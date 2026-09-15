package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
)

func TestCleanupRetainsIdentityAndReservationUntilConfirmed(t *testing.T) {
	var owners Ownership
	lease, _ := owners.Acquire("a")
	var ids []string
	q := NewCleanupQueue(&owners, func(ctx context.Context, id string, l SessionLease) (storage.WorldReceipt, error) {
		ids = append(ids, id)
		err := owners.Finish(l, func() error {
			if len(ids) == 1 {
				return errors.New("unconfirmed")
			}
			return nil
		})
		return storage.WorldReceipt{}, err
	})
	if err := q.Enqueue(lease); err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(lease); err != nil {
		t.Fatal(err)
	}
	if len(q.Pending()) != 1 {
		t.Fatal("duplicate cleanup")
	}
	if owners.Release(lease) || owners.RunGame(lease, func() error { return nil }) == nil {
		t.Fatal("queued closure not sealed")
	}
	if err := q.Retry(context.Background(), time.Second); err == nil || !owners.Owns(lease) || len(q.Pending()) != 1 {
		t.Fatal("failure discarded")
	}
	if err := q.Retry(context.Background(), time.Second); err != nil || owners.Owns(lease) || len(q.Pending()) != 0 {
		t.Fatalf("cleanup incomplete %v", err)
	}
	if len(ids) != 2 || ids[0] == "" || ids[0] != ids[1] {
		t.Fatal("retry identity changed")
	}
}

func TestCleanupWorkerStopsWithoutDiscardingPending(t *testing.T) {
	var owners Ownership
	lease, _ := owners.Acquire("a")
	q := NewCleanupQueue(&owners, func(context.Context, string, SessionLease) (storage.WorldReceipt, error) {
		t.Fatal("canceled worker performed IO")
		return storage.WorldReceipt{}, nil
	})
	if err := q.Enqueue(lease); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := q.Run(ctx, time.Millisecond, time.Second); !errors.Is(err, context.Canceled) || len(q.Pending()) != 1 || !owners.Owns(lease) {
		t.Fatalf("shutdown lost pending: %v", err)
	}
}

func TestCleanupSlowIOAllowsInspectionAndCanceledDrain(t *testing.T) {
	var owners Ownership
	lease, _ := owners.Acquire("a")
	started, release := make(chan struct{}), make(chan struct{})
	q := NewCleanupQueue(&owners, func(context.Context, string, SessionLease) (storage.WorldReceipt, error) {
		close(started)
		<-release
		return storage.WorldReceipt{}, errors.New("still pending")
	})
	if err := q.Enqueue(lease); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- q.Retry(context.Background(), time.Second) }()
	<-started
	defer func() { close(release); <-done }()
	inspected := make(chan int, 1)
	go func() { inspected <- len(q.Pending()) }()
	select {
	case n := <-inspected:
		if n != 1 {
			t.Fatal("missing in-flight record")
		}
	case <-time.After(time.Second):
		t.Fatal("inspection blocked by IO")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	canceled := make(chan error, 1)
	go func() { canceled <- q.Retry(ctx, time.Second) }()
	select {
	case err := <-canceled:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled drain waited for other IO")
	}
}

func TestCleanupWorkerCancellationInterruptsActiveAttempt(t *testing.T) {
	var owners Ownership
	lease, _ := owners.Acquire("a")
	started := make(chan struct{})
	q := NewCleanupQueue(&owners, func(ctx context.Context, _ string, _ SessionLease) (storage.WorldReceipt, error) {
		close(started)
		<-ctx.Done()
		return storage.WorldReceipt{}, ctx.Err()
	})
	if err := q.Enqueue(lease); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- q.Run(ctx, time.Hour, time.Hour) }()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("worker shutdown blocked")
	}
	if len(q.Pending()) != 1 || q.Pending()[0].LastError == nil || !owners.Owns(lease) {
		t.Fatal("shutdown discarded active cleanup")
	}
}
