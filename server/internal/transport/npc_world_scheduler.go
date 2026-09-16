package transport

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
)

// NPCWorldTickRunner is the process-level boundary for one ordered NPC
// cadence. Each method owns its durable slot, command ID, pending retry, and
// duplicate suppression. The scheduler only composes those durable phase
// boundaries; it never invents a second command ID or a second clock sample.
//
// A runner must be safe to call again after an earlier phase has committed.
// In particular, if resource returns an uncertain error after maintenance has
// committed, the next cadence calls maintenance again (which must suppress its
// completed slot) and then retries resource's retained request before combat.
type NPCWorldTickRunner interface {
	RunNPCMaintenanceTick(context.Context, time.Duration) (storage.WorldReceipt, bool, error)
	RunNPCResourceTick(context.Context, time.Duration) (storage.WorldReceipt, bool, error)
	RunNPCCombatTick(context.Context, time.Duration) (storage.WorldReceipt, bool, error)
}

// NPCAggressiveTargetTickRunner is an optional post-combat phase seam. It is
// deliberately separate from NPCWorldTickRunner so existing hosts and test
// doubles that implement the original three-phase contract remain valid. A
// production WorldConnector implements this narrow interface and therefore
// receives the acquisition phase after combat.
type NPCAggressiveTargetTickRunner interface {
	RunNPCAggressiveTargetTick(context.Context, time.Duration) (storage.WorldReceipt, bool, error)
}

// NPCWorldTickResult contains the receipt and admission result from each
// ordered phase. A result returned with an error contains all phases that
// completed before the failing phase; the failing phase's receipt is empty
// unless its runner returned one alongside the error.
type NPCWorldTickResult struct {
	Maintenance         storage.WorldReceipt
	Resource            storage.WorldReceipt
	Combat              storage.WorldReceipt
	AggressiveTarget    storage.WorldReceipt
	MaintenanceRan      bool
	ResourceRan         bool
	CombatRan           bool
	AggressiveTargetRan bool
}

var (
	// ErrNPCWorldSchedulerAlreadyRunning means that a second worker or manual
	// RunOnce attempted to claim the ordered execution boundary.
	ErrNPCWorldSchedulerAlreadyRunning = errors.New("NPC world scheduler is already running")
	// ErrNPCWorldSchedulerClosed means that Shutdown or Stop sealed this
	// scheduler and a new worker cannot be started.
	ErrNPCWorldSchedulerClosed = errors.New("NPC world scheduler is shut down")
)

// NPCWorldScheduler owns one worker and the phase-specific cadences for the
// three required NPC phases plus an optional post-combat acquisition phase.
// Every wake-up is executed strictly as maintenance, resource, combat, then
// acquisition when the runner implements NPCAggressiveTargetTickRunner. A
// phase error stops that cadence immediately; the next wake-up retries the
// phase through the runner's retained durable request.
//
// The scheduler is single-use after Shutdown. Construct a new scheduler for a
// later lifecycle; the phase runners' durable receipts make replacement safe
// across process restart or worker replacement.
type NPCWorldScheduler struct {
	runner              NPCWorldTickRunner
	maintenanceInterval time.Duration
	resourceInterval    time.Duration
	combatInterval      time.Duration
	cadence             time.Duration

	mu      sync.Mutex
	active  bool
	running bool
	closed  bool
	cancel  context.CancelFunc
	done    chan struct{}
}

// NewNPCWorldScheduler validates the phase-specific cadences. The existing
// process defaults intentionally use different intervals (maintenance/combat
// are commonly one second while resource is twenty seconds), so each Tick
// receives its own interval. The worker wakes at the shortest cadence and
// relies on each durable phase to suppress slots that are not due yet.
//
// The intervals are required in maintenance, resource, combat order so the
// process host cannot accidentally route a phase through another cadence.
func NewNPCWorldScheduler(runner NPCWorldTickRunner, maintenanceInterval, resourceInterval, combatInterval time.Duration) (*NPCWorldScheduler, error) {
	if runner == nil {
		return nil, errors.New("nil NPC world scheduler runner")
	}
	intervals := []time.Duration{maintenanceInterval, resourceInterval, combatInterval}
	for _, interval := range intervals {
		if _, err := npcWorldSchedulerIntervalSeconds(interval); err != nil {
			return nil, err
		}
	}
	cadence := intervals[0]
	for _, interval := range intervals[1:] {
		if interval < cadence {
			cadence = interval
		}
	}
	return &NPCWorldScheduler{
		runner:              runner,
		maintenanceInterval: intervals[0],
		resourceInterval:    intervals[1],
		combatInterval:      intervals[2],
		cadence:             cadence,
	}, nil
}

func npcWorldSchedulerIntervalSeconds(interval time.Duration) (int64, error) {
	if interval <= 0 || interval%time.Second != 0 {
		return 0, errors.New("NPC world scheduler interval must be a positive whole number of seconds")
	}
	seconds := int64(interval / time.Second)
	if seconds < 1 {
		return 0, errors.New("NPC world scheduler interval must be at least one second")
	}
	return seconds, nil
}

// Interval returns the worker wake-up cadence, which is the shortest phase
// interval. It is retained as a convenient process-log value; phase-specific
// values are available from MaintenanceInterval, ResourceInterval, and
// CombatInterval and are the values passed to the tick APIs.
func (s *NPCWorldScheduler) Interval() time.Duration {
	if s == nil {
		return 0
	}
	return s.cadence
}

// MaintenanceInterval returns the cadence passed to maintenance ticks.
func (s *NPCWorldScheduler) MaintenanceInterval() time.Duration {
	if s == nil {
		return 0
	}
	return s.maintenanceInterval
}

// ResourceInterval returns the cadence passed to resource ticks.
func (s *NPCWorldScheduler) ResourceInterval() time.Duration {
	if s == nil {
		return 0
	}
	return s.resourceInterval
}

// CombatInterval returns the cadence passed to combat ticks.
func (s *NPCWorldScheduler) CombatInterval() time.Duration {
	if s == nil {
		return 0
	}
	return s.combatInterval
}

// RunOnce executes one ordered cadence synchronously. It is useful for tests,
// one-shot hosts, and deterministic startup probes. It claims the same worker
// boundary as Start/Run, so it cannot overlap a background worker or another
// RunOnce call. Shutdown cancels an in-flight RunOnce as well as a background
// worker.
func (s *NPCWorldScheduler) RunOnce(ctx context.Context) (NPCWorldTickResult, bool, error) {
	if s == nil {
		return NPCWorldTickResult{}, false, errors.New("nil NPC world scheduler")
	}
	if ctx == nil {
		return NPCWorldTickResult{}, false, errors.New("nil NPC world scheduler context")
	}
	owned, cancel, done, err := s.begin(ctx, false)
	if err != nil {
		return NPCWorldTickResult{}, false, err
	}
	defer s.finish(cancel, done)
	return s.runCadence(owned)
}

// Run owns the ordered cadence synchronously until its context is cancelled
// or Shutdown is called from another goroutine. Transient phase errors are
// deliberately retried on a later cadence; the runner retains the exact
// pending command and request for that retry.
func (s *NPCWorldScheduler) Run(ctx context.Context) error {
	if s == nil {
		return errors.New("nil NPC world scheduler")
	}
	if ctx == nil {
		return errors.New("nil NPC world scheduler context")
	}
	owned, cancel, done, err := s.begin(ctx, true)
	if err != nil {
		return err
	}
	defer s.finish(cancel, done)
	return s.loop(owned)
}

// Start starts the ordered cadence asynchronously. Use Wait or Shutdown to
// observe completion. Calling Start while any worker owns the boundary is
// rejected so two process workers cannot interleave phases.
func (s *NPCWorldScheduler) Start(ctx context.Context) error {
	if s == nil {
		return errors.New("nil NPC world scheduler")
	}
	if ctx == nil {
		return errors.New("nil NPC world scheduler context")
	}
	owned, cancel, done, err := s.begin(ctx, true)
	if err != nil {
		return err
	}
	go func() {
		defer s.finish(cancel, done)
		_ = s.loop(owned)
	}()
	return nil
}

// Wait waits for the currently active worker or RunOnce. It is a no-op before
// the scheduler has ever started.
func (s *NPCWorldScheduler) Wait(ctx context.Context) error {
	if s == nil {
		return errors.New("nil NPC world scheduler")
	}
	if ctx == nil {
		return errors.New("nil NPC world scheduler context")
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

// Running reports whether a background cadence worker currently owns the
// scheduler. A synchronous RunOnce is active but is intentionally not
// reported as Running, matching the distinction between a worker lifecycle
// and a one-shot call.
func (s *NPCWorldScheduler) Running() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// Shutdown seals the scheduler, cancels its active worker, and waits for the
// phase currently in progress to return. A timeout does not discard a
// pending durable request; a later Shutdown/Wait can finish the barrier.
func (s *NPCWorldScheduler) Shutdown(ctx context.Context) error {
	if s == nil {
		return errors.New("nil NPC world scheduler")
	}
	if ctx == nil {
		return errors.New("nil NPC world scheduler shutdown context")
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
func (s *NPCWorldScheduler) Stop() {
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

// Close is an alias for Shutdown for hosts using io.Closer-shaped lifecycle
// code.
func (s *NPCWorldScheduler) Close(ctx context.Context) error {
	return s.Shutdown(ctx)
}

func (s *NPCWorldScheduler) begin(parent context.Context, worker bool) (context.Context, context.CancelFunc, chan struct{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, nil, nil, ErrNPCWorldSchedulerClosed
	}
	if s.active {
		return nil, nil, nil, ErrNPCWorldSchedulerAlreadyRunning
	}
	owned, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	s.active = true
	s.running = worker
	s.cancel = cancel
	s.done = done
	return owned, cancel, done, nil
}

func (s *NPCWorldScheduler) finish(cancel context.CancelFunc, done chan struct{}) {
	cancel()
	s.mu.Lock()
	s.active = false
	s.running = false
	s.cancel = nil
	// Keep the closed channel available to callers that call Wait after the
	// worker exits.
	s.done = done
	s.mu.Unlock()
	close(done)
}

func (s *NPCWorldScheduler) loop(ctx context.Context) error {
	ticker := time.NewTicker(s.cadence)
	defer ticker.Stop()
	for {
		_, _, err := s.runCadence(ctx)
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

func (s *NPCWorldScheduler) runCadence(ctx context.Context) (NPCWorldTickResult, bool, error) {
	var result NPCWorldTickResult
	if err := ctx.Err(); err != nil {
		return result, false, err
	}

	var err error
	result.Maintenance, result.MaintenanceRan, err = s.runner.RunNPCMaintenanceTick(ctx, s.maintenanceInterval)
	if err != nil {
		return result, result.MaintenanceRan, err
	}
	if err := ctx.Err(); err != nil {
		return result, result.MaintenanceRan, err
	}

	result.Resource, result.ResourceRan, err = s.runner.RunNPCResourceTick(ctx, s.resourceInterval)
	if err != nil {
		return result, result.MaintenanceRan || result.ResourceRan, err
	}
	if err := ctx.Err(); err != nil {
		return result, result.MaintenanceRan || result.ResourceRan, err
	}

	result.Combat, result.CombatRan, err = s.runner.RunNPCCombatTick(ctx, s.combatInterval)
	if err != nil {
		return result, result.MaintenanceRan || result.ResourceRan || result.CombatRan, err
	}
	if err := ctx.Err(); err != nil {
		return result, result.MaintenanceRan || result.ResourceRan || result.CombatRan, err
	}
	postCombat, ok := s.runner.(NPCAggressiveTargetTickRunner)
	if !ok {
		return result, result.MaintenanceRan || result.ResourceRan || result.CombatRan, nil
	}
	result.AggressiveTarget, result.AggressiveTargetRan, err = postCombat.RunNPCAggressiveTargetTick(ctx, s.combatInterval)
	if err != nil {
		return result, result.MaintenanceRan || result.ResourceRan || result.CombatRan || result.AggressiveTargetRan, err
	}
	return result, result.MaintenanceRan || result.ResourceRan || result.CombatRan || result.AggressiveTargetRan, nil
}
