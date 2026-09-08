package session

import (
	"context"
	"crypto/rand"
	"errors"
	"sync"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
)

type CleanupOperation func(context.Context, string, SessionLease) (storage.WorldReceipt, error)
type PendingCleanup struct {
	Lease     SessionLease
	CommandID string
	LastError error
}

// CleanupQueue owns uncertain closures beyond socket lifetime. It is process
// local: pending work is preserved on Run cancellation, but process death still
// requires fenced cold-start recovery. Resolve must not reenter this queue.
type CleanupQueue struct {
	mu      sync.Mutex
	enqueue sync.Mutex
	drain   chan struct{}
	owners  *Ownership
	resolve CleanupOperation
	pending []PendingCleanup
}

func NewCleanupQueue(owners *Ownership, resolve CleanupOperation) *CleanupQueue {
	return &CleanupQueue{owners: owners, resolve: resolve, drain: make(chan struct{}, 1)}
}

func NewWorldCleanupQueue(owners *Ownership, store engine.CommandStore, worldID string) *CleanupQueue {
	return NewCleanupQueue(owners, func(ctx context.Context, id string, lease SessionLease) (storage.WorldReceipt, error) {
		return owners.CancelWorldAdmission(ctx, store, worldID, id, lease)
	})
}
func (q *CleanupQueue) Enqueue(lease SessionLease) error {
	// Serialize seal+insert without blocking metadata inspection on ownership IO.
	q.enqueue.Lock()
	defer q.enqueue.Unlock()
	q.mu.Lock()
	if q.owners == nil || q.resolve == nil {
		q.mu.Unlock()
		return errors.New("cleanup unavailable")
	}
	for _, p := range q.pending {
		if p.Lease == lease {
			q.mu.Unlock()
			return nil
		}
	}
	q.mu.Unlock()
	if err := q.owners.Seal(lease); err != nil {
		return err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, p := range q.pending {
		if p.Lease == lease {
			return nil
		}
	}
	q.pending = append(q.pending, PendingCleanup{Lease: lease, CommandID: "cleanup-" + rand.Text()})
	return nil
}
func (q *CleanupQueue) Pending() []PendingCleanup {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]PendingCleanup(nil), q.pending...)
}

// Retry bounds each attempt independently and retains every failed record. The
// cancellation-aware gate serializes drains without holding the metadata lock
// during IO. New enqueues are processed in the next batch.
func (q *CleanupQueue) Retry(ctx context.Context, attemptTimeout time.Duration) error {
	if attemptTimeout <= 0 {
		return errors.New("invalid cleanup timeout")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case q.drain <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-q.drain }()
	batch := q.Pending()
	var failures []error
	for _, p := range batch {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(failures, err)...)
		}
		attempt, cancel := context.WithTimeout(ctx, attemptTimeout)
		_, err := q.resolve(attempt, p.CommandID, p.Lease)
		cancel()
		q.mu.Lock()
		for i := range q.pending {
			if q.pending[i].CommandID == p.CommandID {
				if err == nil {
					q.pending = append(q.pending[:i], q.pending[i+1:]...)
				} else {
					q.pending[i].LastError = err
				}
				break
			}
		}
		q.mu.Unlock()
		if err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// Run retries at a fixed bounded cadence; errors remain available in Pending.
// Caller owns the worker context and must inspect/drain Pending at shutdown.
func (q *CleanupQueue) Run(ctx context.Context, interval, attemptTimeout time.Duration) error {
	if interval <= 0 || attemptTimeout <= 0 {
		return errors.New("invalid cleanup schedule")
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		_ = q.Retry(ctx, attemptTimeout)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
