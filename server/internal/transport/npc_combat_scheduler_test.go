package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
)

func TestNewNPCCombatSchedulerValidatesRunnerAndCadence(t *testing.T) {
	if _, err := NewNPCCombatScheduler(nil, time.Second); err == nil {
		t.Fatal("nil runner accepted")
	}
	runner := &blockingNPCCombatSchedulerRunner{}
	for _, interval := range []time.Duration{0, -time.Second, 500 * time.Millisecond} {
		if _, err := NewNPCCombatScheduler(runner, interval); err == nil {
			t.Fatalf("interval %s accepted", interval)
		}
	}
	scheduler, err := NewNPCCombatScheduler(runner, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if scheduler.Interval() != 2*time.Second {
		t.Fatalf("interval=%s", scheduler.Interval())
	}
}

func TestNPCCombatSchedulerDelegatesDurableSlotAndSuppressesDuplicate(t *testing.T) {
	state := npcCombatTickFixture(t)
	now := int32(103)
	store := &npcCombatTickStore{state: encodeNPCCombatTickState(t, state)}
	connector := newNPCCombatTickConnector(t, store, &now)
	scheduler, err := NewNPCCombatScheduler(connector, 20*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	first, ran, err := scheduler.RunOnce(context.Background())
	if err != nil || !ran || first.Replayed {
		t.Fatalf("first=%+v ran=%v err=%v", first, ran, err)
	}
	if store.command != "npc-combat-5" || store.commits != 1 {
		t.Fatalf("command=%q commits=%d", store.command, store.commits)
	}
	var request npcCombatTickRequest
	if err := json.Unmarshal(store.request, &request); err != nil {
		t.Fatal(err)
	}
	if request.Kind != "npc-combat-phase" || request.Slot != 5 || request.Now != 100 {
		t.Fatalf("request=%+v", request)
	}

	duplicate, ran, err := scheduler.RunOnce(context.Background())
	if err != nil || ran || duplicate.Response != nil || store.commitAttempts != 1 {
		t.Fatalf("duplicate=%+v ran=%v attempts=%d err=%v", duplicate, ran, store.commitAttempts, err)
	}
}

func TestNPCCombatSchedulerRetainsExactPendingRequestAcrossRetry(t *testing.T) {
	state := npcCombatTickFixture(t)
	now := int32(101)
	store := &npcCombatTickStore{state: encodeNPCCombatTickState(t, state), failOnce: true}
	connector := newNPCCombatTickConnector(t, store, &now)
	scheduler, err := NewNPCCombatScheduler(connector, 20*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	if _, ran, err := scheduler.RunOnce(context.Background()); err == nil || !ran {
		t.Fatalf("uncertain attempt ran=%v err=%v", ran, err)
	}
	now = 119
	retry, ran, err := scheduler.RunOnce(context.Background())
	if err != nil || !ran || retry.Replayed || store.commits != 1 {
		t.Fatalf("retry=%+v ran=%v commits=%d err=%v", retry, ran, store.commits, err)
	}
	if len(store.commands) != 2 || store.commands[0] != "npc-combat-5" || store.commands[1] != store.commands[0] {
		t.Fatalf("commands=%v", store.commands)
	}
	if len(store.requests) != 2 || !bytes.Equal(store.requests[0], store.requests[1]) {
		t.Fatalf("requests differ: %s / %s", store.requests[0], store.requests[1])
	}
	var request npcCombatTickRequest
	if err := json.Unmarshal(store.requests[1], &request); err != nil {
		t.Fatal(err)
	}
	if request.Slot != 5 || request.Now != 100 {
		t.Fatalf("retry request=%+v", request)
	}
}

type blockingNPCCombatSchedulerRunner struct {
	mu         sync.Mutex
	intervals  []time.Duration
	started    chan struct{}
	cancelled  chan struct{}
	startOnce  sync.Once
	cancelOnce sync.Once
}

func (r *blockingNPCCombatSchedulerRunner) RunNPCCombatTick(ctx context.Context, interval time.Duration) (storage.WorldReceipt, bool, error) {
	r.mu.Lock()
	r.intervals = append(r.intervals, interval)
	r.mu.Unlock()
	r.startOnce.Do(func() { close(r.started) })
	select {
	case <-ctx.Done():
		r.cancelOnce.Do(func() { close(r.cancelled) })
		return storage.WorldReceipt{}, true, ctx.Err()
	}
}

func newBlockingNPCCombatSchedulerRunner() *blockingNPCCombatSchedulerRunner {
	return &blockingNPCCombatSchedulerRunner{
		started:   make(chan struct{}),
		cancelled: make(chan struct{}),
	}
}

func TestNPCCombatSchedulerShutdownCancelsAndWaitsForWorker(t *testing.T) {
	runner := newBlockingNPCCombatSchedulerRunner()
	scheduler, err := NewNPCCombatScheduler(runner, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !scheduler.Running() {
		t.Fatal("scheduler is not running")
	}
	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not start runner")
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := scheduler.Shutdown(shutdownCtx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-runner.cancelled:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not cancel runner")
	}
	if err := scheduler.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if scheduler.Running() {
		t.Fatal("scheduler is still running")
	}
	if err := scheduler.Start(context.Background()); !errors.Is(err, ErrNPCCombatSchedulerClosed) {
		t.Fatalf("restart err=%v", err)
	}
}

func TestNPCCombatSchedulerRejectsConcurrentStartAndHonorsRunCancellation(t *testing.T) {
	runner := newBlockingNPCCombatSchedulerRunner()
	scheduler, err := NewNPCCombatScheduler(runner, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := make(chan error, 1)
	go func() { runDone <- scheduler.Run(ctx) }()
	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not start runner")
	}
	if err := scheduler.Start(context.Background()); !errors.Is(err, ErrNPCCombatSchedulerAlreadyRunning) {
		t.Fatalf("duplicate start err=%v", err)
	}
	if _, _, err := scheduler.RunOnce(context.Background()); !errors.Is(err, ErrNPCCombatSchedulerAlreadyRunning) {
		t.Fatalf("manual tick while running err=%v", err)
	}
	cancel()
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("scheduler did not stop after cancellation")
	}
	if err := scheduler.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestNPCCombatSchedulerStopRequestsCancellationWithoutWaiting(t *testing.T) {
	runner := newBlockingNPCCombatSchedulerRunner()
	scheduler, err := NewNPCCombatScheduler(runner, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not start runner")
	}
	scheduler.Stop()
	select {
	case <-runner.cancelled:
	case <-time.After(time.Second):
		t.Fatal("stop did not cancel runner")
	}
	if err := scheduler.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestNPCCombatSchedulerRunCancellationAfterCommittedReceiptDoesNotDuplicate(t *testing.T) {
	state := npcCombatTickFixture(t)
	store := &npcCombatTickStore{state: encodeNPCCombatTickState(t, state)}
	ctx, cancel := context.WithCancel(context.Background())
	storeWithCancel := &cancelAfterNPCCombatCommitStore{base: store, cancel: cancel}
	now := int32(103)
	connector := newNPCCombatTickConnector(t, store, &now)
	connector.config.Store = storeWithCancel
	scheduler, err := NewNPCCombatScheduler(connector, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if store.commits != 1 || store.command != "npc-combat-0" {
		t.Fatalf("commits=%d command=%q", store.commits, store.command)
	}
	if _, ran, err := scheduler.RunOnce(context.Background()); err != nil || ran {
		t.Fatalf("post-run duplicate ran=%v err=%v", ran, err)
	}
	if err := scheduler.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ran, err := scheduler.RunOnce(context.Background()); !errors.Is(err, ErrNPCCombatSchedulerClosed) || ran {
		t.Fatalf("post-shutdown RunOnce ran=%v err=%v", ran, err)
	}
}

type cancelAfterNPCCombatCommitStore struct {
	base   *npcCombatTickStore
	cancel context.CancelFunc
}

func (s *cancelAfterNPCCombatCommitStore) ReadWorldReceipt(ctx context.Context, worldID, commandID string, request json.RawMessage) (storage.WorldReceipt, error) {
	return s.base.ReadWorldReceipt(ctx, worldID, commandID, request)
}

func (s *cancelAfterNPCCombatCommitStore) LoadWorld(ctx context.Context, worldID string) (storage.WorldSnapshot, error) {
	return s.base.LoadWorld(ctx, worldID)
}

func (s *cancelAfterNPCCombatCommitStore) CommitWorldCommand(ctx context.Context, worldID, commandID string, request json.RawMessage, expected int64, state, response json.RawMessage) (storage.WorldReceipt, error) {
	receipt, err := s.base.CommitWorldCommand(ctx, worldID, commandID, request, expected, state, response)
	if err == nil {
		s.cancel()
	}
	return receipt, err
}
