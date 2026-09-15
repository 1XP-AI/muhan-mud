package transport

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
)

// NPCCombatTickRunner is the small public boundary owned by the connector.
// Keeping the scheduler dependent on this method, rather than on combat
// reducer internals, lets the durable tick keep ownership of slot, timestamp,
// command ID, pending retry, and receipt replay semantics.
type NPCCombatTickRunner interface {
	RunNPCCombatTick(context.Context, time.Duration) (storage.WorldReceipt, bool, error)
}

var (
	// ErrNPCCombatSchedulerAlreadyRunning means that Start or Run was called
	// while this scheduler already owned its worker.
	ErrNPCCombatSchedulerAlreadyRunning = errors.New("NPC combat scheduler is already running")
	// ErrNPCCombatSchedulerClosed means that Shutdown has sealed this scheduler.
	ErrNPCCombatSchedulerClosed = errors.New("NPC combat scheduler is shut down")
)

// NPCCombatScheduler owns the lifecycle of one NPC combat cadence worker.
//
// It deliberately does not calculate combat slots itself. RunOnce delegates to
// WorldConnector.RunNPCCombatTick, whose durable boundary binds the current
// cadence to a fixed slot/now/command ID and retains that request after an
// uncertain commit. Run/Start only provide one worker, cancellation, and
// shutdown coordination around that existing API.
//
// A scheduler is single-use: after Shutdown it cannot be started again. A
// caller that needs a new lifecycle should construct a new scheduler for the
// same connector; the connector's durable receipt makes a replay safe across
// that replacement.
type NPCCombatScheduler struct {
	runner   NPCCombatTickRunner
	interval time.Duration

	mu      sync.Mutex
	running bool
	closed  bool
	cancel  context.CancelFunc
	done    chan struct{}
}

// NewNPCCombatScheduler validates an NPC combat cadence without changing
// WorldConnector configuration or wiring it into the process host.
func NewNPCCombatScheduler(runner NPCCombatTickRunner, interval time.Duration) (*NPCCombatScheduler, error) {
	if runner == nil {
		return nil, errors.New("nil NPC combat scheduler runner")
	}
	if _, err := npcCombatIntervalSeconds(interval); err != nil {
		return nil, err
	}
	return &NPCCombatScheduler{runner: runner, interval: interval}, nil
}

// Interval returns the validated cadence used for every delegated tick.
func (s *NPCCombatScheduler) Interval() time.Duration {
	if s == nil {
		return 0
	}
	return s.interval
}

// RunOnce attempts one durable combat slot. The connector owns duplicate
// suppression: ran is false when the slot has already been completed, while a
// failed attempt leaves the exact pending request in the connector for the
// next RunOnce or cadence iteration.
func (s *NPCCombatScheduler) RunOnce(ctx context.Context) (storage.WorldReceipt, bool, error) {
	if s == nil {
		return storage.WorldReceipt{}, false, errors.New("nil NPC combat scheduler")
	}
	if ctx == nil {
		return storage.WorldReceipt{}, false, errors.New("nil NPC combat scheduler context")
	}
	s.mu.Lock()
	closed := s.closed
	running := s.running
	s.mu.Unlock()
	if closed {
		return storage.WorldReceipt{}, false, ErrNPCCombatSchedulerClosed
	}
	if running {
		return storage.WorldReceipt{}, false, ErrNPCCombatSchedulerAlreadyRunning
	}
	return s.runner.RunNPCCombatTick(ctx, s.interval)
}

// Run owns the cadence synchronously until its context is cancelled or
// Shutdown is called from another goroutine. A transient durable error is not
// returned immediately: the connector retains its pending request and the
// next cadence retries the same command. This matches the existing connector
// scheduler contract and avoids abandoning an unknown commit outcome.
func (s *NPCCombatScheduler) Run(ctx context.Context) error {
	if s == nil {
		return errors.New("nil NPC combat scheduler")
	}
	if ctx == nil {
		return errors.New("nil NPC combat scheduler context")
	}
	owned, cancel, done, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer s.finish(cancel, done)
	return s.loop(owned)
}

// Start starts the cadence asynchronously. Use Wait or Shutdown to observe
// its completion. Calling Start twice is rejected so two workers cannot race
// the same cadence and receipt.
func (s *NPCCombatScheduler) Start(ctx context.Context) error {
	if s == nil {
		return errors.New("nil NPC combat scheduler")
	}
	if ctx == nil {
		return errors.New("nil NPC combat scheduler context")
	}
	owned, cancel, done, err := s.begin(ctx)
	if err != nil {
		return err
	}
	go func() {
		defer s.finish(cancel, done)
		_ = s.loop(owned)
	}()
	return nil
}

// Wait waits for the currently running worker. It is a no-op when the
// scheduler has not been started. Wait never cancels the worker itself.
func (s *NPCCombatScheduler) Wait(ctx context.Context) error {
	if s == nil {
		return errors.New("nil NPC combat scheduler")
	}
	if ctx == nil {
		return errors.New("nil NPC combat scheduler context")
	}
	s.mu.Lock()
	done := s.done
	s.mu.Unlock()
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Running reports whether this scheduler currently owns a cadence worker.
func (s *NPCCombatScheduler) Running() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// Shutdown cancels the cadence worker and waits for it to stop. A timeout
// does not discard the connector's pending request; a later Shutdown call may
// finish waiting, and the exact command remains retryable by a new scheduler.
func (s *NPCCombatScheduler) Shutdown(ctx context.Context) error {
	if s == nil {
		return errors.New("nil NPC combat scheduler")
	}
	if ctx == nil {
		return errors.New("nil NPC combat scheduler shutdown context")
	}
	s.mu.Lock()
	s.closed = true
	cancel := s.cancel
	done := s.done
	s.mu.Unlock()
	if cancel == nil || done == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Stop requests cancellation without waiting. Hosts that need a bounded
// shutdown barrier should call Shutdown or Wait after Stop.
func (s *NPCCombatScheduler) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.closed = true
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Close is an alias for Shutdown for hosts that use io.Closer-shaped worker
// lifecycle code. The context is required so shutdown remains bounded.
func (s *NPCCombatScheduler) Close(ctx context.Context) error {
	return s.Shutdown(ctx)
}

func (s *NPCCombatScheduler) begin(parent context.Context) (context.Context, context.CancelFunc, chan struct{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, nil, nil, ErrNPCCombatSchedulerClosed
	}
	if s.running {
		return nil, nil, nil, ErrNPCCombatSchedulerAlreadyRunning
	}
	owned, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	s.running = true
	s.cancel = cancel
	s.done = done
	return owned, cancel, done, nil
}

func (s *NPCCombatScheduler) finish(cancel context.CancelFunc, done chan struct{}) {
	cancel()
	s.mu.Lock()
	s.running = false
	s.cancel = nil
	// Keep a closed done channel available to Wait after a worker exits.
	s.done = done
	s.mu.Unlock()
	close(done)
}

func (s *NPCCombatScheduler) loop(ctx context.Context) error {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		_, _, err := s.runner.RunNPCCombatTick(ctx, s.interval)
		if err != nil && ctx.Err() != nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
