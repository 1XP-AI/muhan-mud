package transport

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
)

func TestNewNPCWorldSchedulerValidatesRunnerAndCadence(t *testing.T) {
	runner := newNPCWorldSchedulerFake()
	if _, err := NewNPCWorldScheduler(nil, time.Second, 20*time.Second, time.Second); err == nil {
		t.Fatal("nil runner accepted")
	}
	for _, intervals := range [][3]time.Duration{
		{0, 20 * time.Second, time.Second},
		{-time.Second, 20 * time.Second, time.Second},
		{500 * time.Millisecond, 20 * time.Second, time.Second},
	} {
		if _, err := NewNPCWorldScheduler(runner, intervals[0], intervals[1], intervals[2]); err == nil {
			t.Fatalf("intervals %v accepted", intervals)
		}
	}
	scheduler, err := NewNPCWorldScheduler(runner, time.Second, 20*time.Second, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if scheduler.Interval() != time.Second || scheduler.MaintenanceInterval() != time.Second || scheduler.ResourceInterval() != 20*time.Second || scheduler.CombatInterval() != time.Second {
		t.Fatalf("interval=%s", scheduler.Interval())
	}
}

func TestNPCWorldSchedulerRunsDeterministicOrderAndSuppressesDuplicateCadence(t *testing.T) {
	runner := newNPCWorldSchedulerFake()
	scheduler, err := NewNPCWorldScheduler(runner, time.Second, 20*time.Second, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	first, ran, err := scheduler.RunOnce(context.Background())
	if err != nil || !ran {
		t.Fatalf("first=%+v ran=%v err=%v", first, ran, err)
	}
	if got, want := runner.calls, []string{"maintenance", "resource", "combat"}; !equalStrings(got, want) {
		t.Fatalf("first order=%v want=%v", got, want)
	}
	if first.MaintenanceRan != true || first.ScavengeRan || first.ResourceRan != true || first.CombatRan != true || first.Scavenge.Revision != 0 {
		t.Fatalf("first phase results=%+v", first)
	}
	if got, want := runner.attempts, []string{"maintenance-1", "resource-1", "combat-1"}; !equalStrings(got, want) {
		t.Fatalf("first commands=%v want=%v", got, want)
	}

	duplicate, ran, err := scheduler.RunOnce(context.Background())
	if err != nil || ran {
		t.Fatalf("duplicate=%+v ran=%v err=%v", duplicate, ran, err)
	}
	if got, want := runner.calls, []string{
		"maintenance", "resource", "combat",
		"maintenance", "resource", "combat",
	}; !equalStrings(got, want) {
		t.Fatalf("duplicate order=%v want=%v", got, want)
	}
	if got, want := runner.attempts, []string{"maintenance-1", "resource-1", "combat-1"}; !equalStrings(got, want) {
		t.Fatalf("duplicate created commands=%v want=%v", got, want)
	}
	for _, interval := range runner.intervals {
		if interval != time.Second && interval != 20*time.Second {
			t.Fatalf("phase interval=%s", interval)
		}
	}
	if got, want := runner.intervals, []time.Duration{time.Second, 20 * time.Second, time.Second, time.Second, 20 * time.Second, time.Second}; !equalDurations(got, want) {
		t.Fatalf("phase intervals=%v want=%v", got, want)
	}
}

func TestNPCWorldSchedulerRunsOptionalScavengeBetweenMaintenanceAndResource(t *testing.T) {
	runner := newNPCWorldSchedulerScavengeFake()
	scheduler, err := NewNPCWorldScheduler(runner, 2*time.Second, 20*time.Second, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	result, ran, err := scheduler.RunOnce(context.Background())
	if err != nil || !ran {
		t.Fatalf("result=%+v ran=%v err=%v", result, ran, err)
	}
	if got, want := runner.calls, []string{"maintenance", "scavenge", "resource", "combat"}; !equalStrings(got, want) {
		t.Fatalf("phase order=%v want=%v", got, want)
	}
	if got, want := runner.intervals, []time.Duration{2 * time.Second, 3 * time.Second, 20 * time.Second, 3 * time.Second}; !equalDurations(got, want) {
		t.Fatalf("phase intervals=%v want=%v", got, want)
	}
	if result.Maintenance.Revision != 1 || result.Scavenge.Revision != 2 || result.Resource.Revision != 3 || result.Combat.Revision != 4 {
		t.Fatalf("phase receipts=%+v", result)
	}
	if !result.MaintenanceRan || !result.ScavengeRan || !result.ResourceRan || !result.CombatRan {
		t.Fatalf("phase admission=%+v", result)
	}
}

func TestNPCWorldSchedulerRetriesUncertainScavengeBeforeResourceAndCombat(t *testing.T) {
	runner := newNPCWorldSchedulerScavengeFake()
	runner.failScavenge = true
	scheduler, err := NewNPCWorldScheduler(runner, time.Second, 20*time.Second, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	first, ran, err := scheduler.RunOnce(context.Background())
	if err == nil || !ran {
		t.Fatalf("failed cadence=%+v ran=%v err=%v", first, ran, err)
	}
	if !errors.Is(err, errNPCWorldScavengeUncertain) {
		t.Fatalf("failed cadence err=%v", err)
	}
	if got, want := runner.calls, []string{"maintenance", "scavenge"}; !equalStrings(got, want) {
		t.Fatalf("failed order=%v want=%v", got, want)
	}
	if got, want := runner.attempts, []string{"maintenance-1", "scavenge-1"}; !equalStrings(got, want) {
		t.Fatalf("failed commands=%v want=%v", got, want)
	}
	if first.Maintenance.Revision != 1 || !first.MaintenanceRan || first.Scavenge.Revision != 0 || !first.ScavengeRan || first.ResourceRan || first.CombatRan {
		t.Fatalf("failed phase results=%+v", first)
	}

	second, ran, err := scheduler.RunOnce(context.Background())
	if err != nil || !ran {
		t.Fatalf("retry cadence=%+v ran=%v err=%v", second, ran, err)
	}
	if got, want := runner.calls, []string{
		"maintenance", "scavenge",
		"maintenance", "scavenge", "resource", "combat",
	}; !equalStrings(got, want) {
		t.Fatalf("retry order=%v want=%v", got, want)
	}
	if got, want := runner.attempts, []string{
		"maintenance-1", "scavenge-1", "scavenge-1", "resource-1", "combat-1",
	}; !equalStrings(got, want) {
		t.Fatalf("retry commands=%v want=%v", got, want)
	}
	if second.MaintenanceRan || !second.ScavengeRan || !second.ResourceRan || !second.CombatRan {
		t.Fatalf("retry phase admission=%+v", second)
	}
	if second.Maintenance.Revision != 0 || second.Scavenge.Revision != 3 || second.Resource.Revision != 4 || second.Combat.Revision != 5 {
		t.Fatalf("retry phase receipts=%+v", second)
	}
}

func TestNPCWorldSchedulerRetriesMiddlePhaseOnNextCadenceInOrder(t *testing.T) {
	runner := newNPCWorldSchedulerFake()
	runner.failResource = true
	scheduler, err := NewNPCWorldScheduler(runner, time.Second, 20*time.Second, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	first, ran, err := scheduler.RunOnce(context.Background())
	if err == nil || !ran {
		t.Fatalf("failed cadence=%+v ran=%v err=%v", first, ran, err)
	}
	if !errors.Is(err, errNPCWorldResourceUncertain) {
		t.Fatalf("failed cadence err=%v", err)
	}
	if got, want := runner.calls, []string{"maintenance", "resource"}; !equalStrings(got, want) {
		t.Fatalf("failed order=%v want=%v", got, want)
	}
	if got, want := runner.attempts, []string{"maintenance-1", "resource-1"}; !equalStrings(got, want) {
		t.Fatalf("failed commands=%v want=%v", got, want)
	}

	second, ran, err := scheduler.RunOnce(context.Background())
	if err != nil || !ran {
		t.Fatalf("retry cadence=%+v ran=%v err=%v", second, ran, err)
	}
	if got, want := runner.calls, []string{
		"maintenance", "resource",
		"maintenance", "resource", "combat",
	}; !equalStrings(got, want) {
		t.Fatalf("retry order=%v want=%v", got, want)
	}
	// Maintenance's completed command is suppressed, while resource retries
	// its exact pending command before combat is admitted.
	if got, want := runner.attempts, []string{
		"maintenance-1", "resource-1", "resource-1", "combat-1",
	}; !equalStrings(got, want) {
		t.Fatalf("retry commands=%v want=%v", got, want)
	}
	if second.MaintenanceRan || !second.ResourceRan || !second.CombatRan {
		t.Fatalf("retry phase results=%+v", second)
	}
}

func TestNPCWorldSchedulerShutdownCancelsAndWaitsForWorker(t *testing.T) {
	runner := newBlockingNPCWorldSchedulerFake()
	scheduler, err := NewNPCWorldScheduler(runner, time.Hour, time.Hour, time.Hour)
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
		t.Fatal("scheduler did not start maintenance phase")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := scheduler.Shutdown(shutdownCtx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-runner.cancelled:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not cancel phase")
	}
	if err := scheduler.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if scheduler.Running() {
		t.Fatal("scheduler is still running")
	}
	if err := scheduler.Start(context.Background()); !errors.Is(err, ErrNPCWorldSchedulerClosed) {
		t.Fatalf("restart err=%v", err)
	}
}

func TestNPCWorldSchedulerRejectsOverlappingWorkerAndRunOnce(t *testing.T) {
	runner := newBlockingNPCWorldSchedulerFake()
	scheduler, err := NewNPCWorldScheduler(runner, time.Hour, time.Hour, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not start")
	}
	if _, _, err := scheduler.RunOnce(context.Background()); !errors.Is(err, ErrNPCWorldSchedulerAlreadyRunning) {
		t.Fatalf("RunOnce err=%v", err)
	}
	if err := scheduler.Start(context.Background()); !errors.Is(err, ErrNPCWorldSchedulerAlreadyRunning) {
		t.Fatalf("duplicate Start err=%v", err)
	}
	scheduler.Stop()
	if err := scheduler.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

var errNPCWorldResourceUncertain = errors.New("resource durable outcome uncertain")
var errNPCWorldScavengeUncertain = errors.New("scavenge durable outcome uncertain")

type npcWorldSchedulerFake struct {
	mu           sync.Mutex
	calls        []string
	attempts     []string
	intervals    []time.Duration
	phases       map[string]*npcWorldSchedulerFakePhase
	failResource bool
	failScavenge bool
}

type npcWorldSchedulerFakePhase struct {
	commandID string
	completed bool
}

func newNPCWorldSchedulerFake() *npcWorldSchedulerFake {
	return &npcWorldSchedulerFake{phases: map[string]*npcWorldSchedulerFakePhase{
		"maintenance": {},
		"resource":    {},
		"combat":      {},
	}}
}

type npcWorldSchedulerScavengeFake struct {
	*npcWorldSchedulerFake
}

func newNPCWorldSchedulerScavengeFake() *npcWorldSchedulerScavengeFake {
	runner := newNPCWorldSchedulerFake()
	runner.phases["scavenge"] = &npcWorldSchedulerFakePhase{}
	return &npcWorldSchedulerScavengeFake{npcWorldSchedulerFake: runner}
}

func (r *npcWorldSchedulerFake) RunNPCMaintenanceTick(ctx context.Context, interval time.Duration) (storage.WorldReceipt, bool, error) {
	return r.run(ctx, "maintenance", interval)
}

func (r *npcWorldSchedulerFake) RunNPCResourceTick(ctx context.Context, interval time.Duration) (storage.WorldReceipt, bool, error) {
	return r.run(ctx, "resource", interval)
}

func (r *npcWorldSchedulerFake) RunNPCCombatTick(ctx context.Context, interval time.Duration) (storage.WorldReceipt, bool, error) {
	return r.run(ctx, "combat", interval)
}

func (r *npcWorldSchedulerScavengeFake) RunNPCScavengeTick(ctx context.Context, interval time.Duration) (storage.WorldReceipt, bool, error) {
	return r.run(ctx, "scavenge", interval)
}

func (r *npcWorldSchedulerFake) run(ctx context.Context, phase string, interval time.Duration) (storage.WorldReceipt, bool, error) {
	if err := ctx.Err(); err != nil {
		return storage.WorldReceipt{}, false, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, phase)
	r.intervals = append(r.intervals, interval)
	state := r.phases[phase]
	if state.completed {
		return storage.WorldReceipt{}, false, nil
	}
	if state.commandID == "" {
		state.commandID = phase + "-1"
	}
	r.attempts = append(r.attempts, state.commandID)
	if phase == "resource" && r.failResource {
		r.failResource = false
		return storage.WorldReceipt{}, true, errNPCWorldResourceUncertain
	}
	if phase == "scavenge" && r.failScavenge {
		r.failScavenge = false
		return storage.WorldReceipt{}, true, errNPCWorldScavengeUncertain
	}
	state.completed = true
	return storage.WorldReceipt{Revision: int64(len(r.attempts))}, true, nil
}

type blockingNPCWorldSchedulerFake struct {
	started    chan struct{}
	cancelled  chan struct{}
	startOnce  sync.Once
	cancelOnce sync.Once
}

func newBlockingNPCWorldSchedulerFake() *blockingNPCWorldSchedulerFake {
	return &blockingNPCWorldSchedulerFake{
		started:   make(chan struct{}),
		cancelled: make(chan struct{}),
	}
}

func (r *blockingNPCWorldSchedulerFake) RunNPCMaintenanceTick(ctx context.Context, interval time.Duration) (storage.WorldReceipt, bool, error) {
	return r.block(ctx)
}

func (r *blockingNPCWorldSchedulerFake) RunNPCResourceTick(ctx context.Context, interval time.Duration) (storage.WorldReceipt, bool, error) {
	return r.block(ctx)
}

func (r *blockingNPCWorldSchedulerFake) RunNPCCombatTick(ctx context.Context, interval time.Duration) (storage.WorldReceipt, bool, error) {
	return r.block(ctx)
}

func (r *blockingNPCWorldSchedulerFake) block(ctx context.Context) (storage.WorldReceipt, bool, error) {
	r.startOnce.Do(func() { close(r.started) })
	<-ctx.Done()
	r.cancelOnce.Do(func() { close(r.cancelled) })
	return storage.WorldReceipt{}, true, ctx.Err()
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func equalDurations(got, want []time.Duration) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
