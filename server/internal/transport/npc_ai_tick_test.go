package transport

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

type npcAITickSchedulerFake struct {
	calls     []string
	intervals []time.Duration
}

func (r *npcAITickSchedulerFake) RunNPCMaintenanceTick(_ context.Context, interval time.Duration) (storage.WorldReceipt, bool, error) {
	r.calls = append(r.calls, "maintenance")
	r.intervals = append(r.intervals, interval)
	return storage.WorldReceipt{Revision: 1}, true, nil
}

func (r *npcAITickSchedulerFake) RunNPCResourceTick(_ context.Context, interval time.Duration) (storage.WorldReceipt, bool, error) {
	r.calls = append(r.calls, "resource")
	r.intervals = append(r.intervals, interval)
	return storage.WorldReceipt{Revision: 2}, true, nil
}

func (r *npcAITickSchedulerFake) RunNPCCombatTick(_ context.Context, interval time.Duration) (storage.WorldReceipt, bool, error) {
	r.calls = append(r.calls, "combat")
	r.intervals = append(r.intervals, interval)
	return storage.WorldReceipt{Revision: 3}, true, nil
}

func (r *npcAITickSchedulerFake) RunNPCAggressiveTargetTick(_ context.Context, interval time.Duration) (storage.WorldReceipt, bool, error) {
	r.calls = append(r.calls, "aggressive-target")
	r.intervals = append(r.intervals, interval)
	return storage.WorldReceipt{Revision: 4}, true, nil
}

func TestNPCWorldSchedulerRunsOptionalAggressiveTargetAfterCombat(t *testing.T) {
	runner := &npcAITickSchedulerFake{}
	scheduler, err := NewNPCWorldScheduler(runner, time.Second, 20*time.Second, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	result, ran, err := scheduler.RunOnce(context.Background())
	if err != nil || !ran {
		t.Fatalf("result=%+v ran=%v err=%v", result, ran, err)
	}
	if got, want := runner.calls, []string{"maintenance", "resource", "combat", "aggressive-target"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("phase order=%v want=%v", got, want)
	}
	if got, want := runner.intervals, []time.Duration{time.Second, 20 * time.Second, 2 * time.Second, 2 * time.Second}; !reflect.DeepEqual(got, want) {
		t.Fatalf("phase intervals=%v want=%v", got, want)
	}
	if result.AggressiveTarget.Revision != 4 || !result.AggressiveTargetRan {
		t.Fatalf("aggressive target result=%+v", result)
	}
}

func TestNPCWorldSchedulerKeepsThreePhaseRunnerClosedToOptionalPhase(t *testing.T) {
	runner := newNPCWorldSchedulerFake()
	scheduler, err := NewNPCWorldScheduler(runner, time.Second, 20*time.Second, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	result, ran, err := scheduler.RunOnce(context.Background())
	if err != nil || !ran {
		t.Fatalf("result=%+v ran=%v err=%v", result, ran, err)
	}
	if len(runner.calls) != 3 || result.AggressiveTargetRan || result.AggressiveTarget.Revision != 0 {
		t.Fatalf("legacy runner was widened: calls=%v result=%+v", runner.calls, result)
	}
}

func TestNPCWorldSchedulerAcquiresAggressiveTargetAfterMaintenance(t *testing.T) {
	state := npcAITickState(t, 1)
	store := newNPCAITickStore(t, state)
	now := int32(100)
	connector := newNPCAITickConnector(t, store, func() (int32, int) { return now, 12 }, func(low, _ int) int {
		return low
	})
	scheduler, err := NewNPCWorldScheduler(connector, time.Second, 20*time.Second, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	first, ran, err := scheduler.RunOnce(context.Background())
	if err != nil || !ran {
		t.Fatalf("first scheduler pass=%+v ran=%v err=%v", first, ran, err)
	}
	firstCombat := decodeNPCAggressiveCombatSummary(t, first.Combat.Response)
	if len(firstCombat.Attacks) != 0 || len(firstCombat.Skipped) != 1 {
		t.Fatalf("first scheduler combat=%+v", firstCombat)
	}
	firstAcquisition := decodeNPCAggressiveTargetResult(t, first.AggressiveTarget.Response)
	if len(firstAcquisition.Events) != 1 || firstAcquisition.Events[0].NPCID != "wolf-1" || firstAcquisition.Events[0].TargetID != "target" {
		t.Fatalf("first scheduler acquisition=%+v", firstAcquisition)
	}
	if got := npcAITickStoredState(t, store).NPCs["wolf-1"].Body.Timers[world.TurnAttackTimerIndex]; got.LastTime != now || got.Interval != 0 {
		t.Fatalf("first scheduler acquisition timer=%+v", got)
	}

	now++
	second, ran, err := scheduler.RunOnce(context.Background())
	if err != nil || !ran {
		t.Fatalf("second scheduler pass=%+v ran=%v err=%v", second, ran, err)
	}
	secondCombat := decodeNPCAggressiveCombatSummary(t, second.Combat.Response)
	if len(secondCombat.Attacks) != 1 || secondCombat.Attacks[0].NPCID != "wolf-1" || secondCombat.Attacks[0].PlayerID != "target" {
		t.Fatalf("second scheduler combat=%+v", secondCombat)
	}
	secondAcquisition := decodeNPCAggressiveTargetResult(t, second.AggressiveTarget.Response)
	if !secondAcquisition.NoOp || secondAcquisition.Changed || len(secondAcquisition.Events) != 0 || len(secondAcquisition.Actions) != 1 || secondAcquisition.Actions[0].Status != "existing-enemy" {
		t.Fatalf("second scheduler acquisition=%+v", secondAcquisition)
	}
}

func TestNPCAggressiveTargetAfterMaintenanceDoesNotNormalizeCrossSecondTimer(t *testing.T) {
	state := npcAITickState(t, 1)
	store := newNPCAITickStore(t, state)
	now := int32(100)
	connector := newNPCAITickConnector(t, store, func() (int32, int) { return now, 12 }, func(low, _ int) int {
		return low
	})

	maintenance, ran, err := connector.RunNPCMaintenanceTick(context.Background(), time.Second)
	if err != nil || !ran {
		t.Fatalf("maintenance=%+v ran=%v err=%v", maintenance, ran, err)
	}
	now = 101
	acquisition, ran, err := connector.RunNPCAggressiveTargetTickAfterMaintenance(context.Background(), time.Second, maintenance)
	if err != nil || !ran || acquisition.Replayed {
		t.Fatalf("cross-second acquisition=%+v ran=%v err=%v", acquisition, ran, err)
	}
	result := decodeNPCAggressiveTargetResult(t, acquisition.Response)
	if result.Now != 101 || !result.NoOp || result.Changed || len(result.Events) != 0 || len(result.Actions) != 1 || result.Actions[0].Status != "not-ready" {
		t.Fatalf("cross-second result=%+v", result)
	}
	if got := npcAITickStoredState(t, store).NPCs["wolf-1"].Body.Timers[world.TurnAttackTimerIndex]; got.LastTime != 100 || got.Interval != 3 {
		t.Fatalf("cross-second timer=%+v", got)
	}
}

func TestNPCWorldSchedulerDoesNotOverrideMaintenanceTimerAcrossUnequalCadence(t *testing.T) {
	state := npcAITickState(t, 1)
	store := newNPCAITickStore(t, state)
	now := int32(101)
	connector := newNPCAITickConnector(t, store, func() (int32, int) { return now, 12 }, func(low, _ int) int {
		return low
	})
	scheduler, err := NewNPCWorldScheduler(connector, 2*time.Second, 4*time.Second, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	result, ran, err := scheduler.RunOnce(context.Background())
	if err != nil || !ran {
		t.Fatalf("scheduler pass=%+v ran=%v err=%v", result, ran, err)
	}
	acquisition := decodeNPCAggressiveTargetResult(t, result.AggressiveTarget.Response)
	if len(acquisition.Events) != 0 || acquisition.Changed || !acquisition.NoOp || len(acquisition.Actions) != 1 || acquisition.Actions[0].Status != "not-ready" {
		t.Fatalf("cross-cadence acquisition=%+v", acquisition)
	}
	if got := npcAITickStoredState(t, store).NPCs["wolf-1"].Body.Timers[world.TurnAttackTimerIndex]; got.LastTime != 100 || got.Interval != 3 {
		t.Fatalf("cross-cadence timer=%+v", got)
	}
}

func TestNPCAggressiveTargetCannotSwingUntilNextPass(t *testing.T) {
	state := npcAITickState(t, 1)
	store := newNPCAITickStore(t, state)
	now := int32(100)
	rollCalls := 0
	connector := newNPCAITickConnector(t, store, func() (int32, int) { return now, 12 }, func(low, _ int) int {
		rollCalls++
		return low
	})

	before, err := connector.RunNPCCombatPhase(context.Background(), "npc-combat-before-acquisition", 1, now)
	if err != nil || before.Replayed {
		t.Fatalf("before combat receipt=%+v err=%v", before, err)
	}
	beforeSummary := decodeNPCAggressiveCombatSummary(t, before.Response)
	if len(beforeSummary.Attacks) != 0 || len(beforeSummary.Skipped) != 1 {
		t.Fatalf("before acquisition combat=%+v", beforeSummary)
	}

	acquired, err := connector.RunNPCAggressiveTargetPhase(context.Background(), "npc-aggressive-target-direct", 1, now)
	if err != nil || acquired.Replayed {
		t.Fatalf("acquisition receipt=%+v err=%v", acquired, err)
	}
	acquisition := decodeNPCAggressiveTargetResult(t, acquired.Response)
	if len(acquisition.Events) != 1 || acquisition.Events[0].NPCID != "wolf-1" || acquisition.Events[0].TargetID != "target" {
		t.Fatalf("acquisition=%+v", acquisition)
	}
	if got := npcAITickStoredState(t, store).NPCs["wolf-1"].Enemies; len(got) != 1 || got[0].Target != (world.EntityRef{Kind: "player", ID: "target"}) {
		t.Fatalf("acquired enemies=%+v", got)
	}

	now++
	after, err := connector.RunNPCCombatPhase(context.Background(), "npc-combat-next-pass", 2, now)
	if err != nil || after.Replayed {
		t.Fatalf("next combat receipt=%+v err=%v", after, err)
	}
	afterSummary := decodeNPCAggressiveCombatSummary(t, after.Response)
	if len(afterSummary.Attacks) != 1 || afterSummary.Attacks[0].NPCID != "wolf-1" || afterSummary.Attacks[0].PlayerID != "target" {
		t.Fatalf("next-pass combat=%+v", afterSummary)
	}
	if rollCalls == 0 {
		t.Fatal("acquisition/combat did not use the injected deterministic RNG")
	}
}

func TestRunNPCAggressiveTargetPhaseKeepsStrictReadinessAcrossSecond(t *testing.T) {
	state := npcAITickState(t, 1)
	npc := state.NPCs["wolf-1"]
	npc.Body.Timers[world.TurnAttackTimerIndex] = world.LegacyTimer{LastTime: 100, Interval: 2}
	state.NPCs["wolf-1"] = npc
	store := newNPCAITickStore(t, state)
	connector := newNPCAITickConnector(t, store, func() (int32, int) { return 101, 12 }, func(int, int) int {
		t.Fatal("strict direct path consumed RNG")
		return 0
	})

	receipt, err := connector.RunNPCAggressiveTargetPhase(context.Background(), "npc-aggressive-target-strict-cross-second", 101, 101)
	if err != nil || receipt.Replayed {
		t.Fatalf("strict direct receipt=%+v err=%v", receipt, err)
	}
	result := decodeNPCAggressiveTargetResult(t, receipt.Response)
	if result.Now != 101 || !result.NoOp || result.Changed || len(result.Events) != 0 || len(result.Actions) != 1 || result.Actions[0].Status != "not-ready" {
		t.Fatalf("strict direct result=%+v", result)
	}
	if got := npcAITickStoredState(t, store).NPCs["wolf-1"].Body.Timers[world.TurnAttackTimerIndex]; got.LastTime != 100 || got.Interval != 2 {
		t.Fatalf("strict direct timer=%+v", got)
	}
}

func TestNPCAggressiveTargetTickRetainsExactPendingRequestAcrossRetry(t *testing.T) {
	state := npcAITickState(t, 1)
	store := newNPCAITickStore(t, state)
	store.failCommitOnce = true
	now := int32(100)
	clockCalls := 0
	connector := newNPCAITickConnector(t, store, func() (int32, int) {
		clockCalls++
		return now, 12
	}, func(low, _ int) int { return low })

	first, ran, err := connector.RunNPCAggressiveTargetTick(context.Background(), time.Second)
	if err == nil || !ran || first.Replayed {
		t.Fatalf("uncertain first receipt=%+v ran=%v err=%v", first, ran, err)
	}
	if store.commitAttempts != 1 || store.commits != 0 {
		t.Fatalf("first commit attempts=%d commits=%d", store.commitAttempts, store.commits)
	}
	if len(store.commands) != 1 || len(store.requests) != 1 {
		t.Fatalf("first durable boundary commands=%v requests=%v", store.commands, store.requests)
	}

	now = 500
	second, ran, err := connector.RunNPCAggressiveTargetTick(context.Background(), time.Second)
	if err != nil || !ran || second.Replayed {
		t.Fatalf("retry receipt=%+v ran=%v err=%v", second, ran, err)
	}
	if clockCalls != 1 {
		t.Fatalf("retry resampled clock %d times", clockCalls)
	}
	if store.commitAttempts != 2 || store.commits != 1 {
		t.Fatalf("retry commit attempts=%d commits=%d", store.commitAttempts, store.commits)
	}
	if store.commands[0] != store.commands[1] || !bytes.Equal(store.requests[0], store.requests[1]) {
		t.Fatalf("retry changed durable boundary commands=%v requests=%q", store.commands, store.requests)
	}
	var request npcAggressiveTargetTickRequest
	if err := json.Unmarshal(store.requests[0], &request); err != nil {
		t.Fatal(err)
	}
	if request.Kind != "npc-aggressive-target-phase" || request.Slot != 100 || request.Now != 100 {
		t.Fatalf("request=%+v", request)
	}
}

func TestNPCAggressiveTargetPendingRetryFallsBackToStrictAfterMaintenanceAdvance(t *testing.T) {
	state := npcAITickState(t, 1)
	store := newNPCAITickStore(t, state)
	now := int32(100)
	connector := newNPCAITickConnector(t, store, func() (int32, int) { return now, 12 }, func(low, _ int) int {
		return low
	})

	maintenance, ran, err := connector.RunNPCMaintenanceTick(context.Background(), time.Second)
	if err != nil || !ran {
		t.Fatalf("maintenance=%+v ran=%v err=%v", maintenance, ran, err)
	}
	store.failCommitOnce = true
	first, ran, err := connector.RunNPCAggressiveTargetTickAfterMaintenance(context.Background(), time.Second, maintenance)
	if err == nil || !ran || first.Replayed {
		t.Fatalf("uncertain acquisition=%+v ran=%v err=%v", first, ran, err)
	}

	now = 103
	advancedMaintenance, ran, err := connector.RunNPCMaintenanceTick(context.Background(), time.Second)
	if err != nil || !ran {
		t.Fatalf("advanced maintenance=%+v ran=%v err=%v", advancedMaintenance, ran, err)
	}
	second, ran, err := connector.RunNPCAggressiveTargetTickAfterMaintenance(context.Background(), time.Second, advancedMaintenance)
	if err != nil || !ran || second.Replayed {
		t.Fatalf("pending retry=%+v ran=%v err=%v", second, ran, err)
	}
	result := decodeNPCAggressiveTargetResult(t, second.Response)
	if result.Now != 100 || !result.NoOp || result.Changed || len(result.Events) != 0 || len(result.Actions) != 1 || result.Actions[0].Status != "not-ready" {
		t.Fatalf("pending retry result=%+v", result)
	}
	if got := npcAITickStoredState(t, store).NPCs["wolf-1"].Body.Timers[world.TurnAttackTimerIndex]; got.LastTime != 103 || got.Interval != 3 {
		t.Fatalf("pending retry timer=%+v", got)
	}
	if got, want := store.commands, []string{"npc-maintenance-100", "npc-aggressive-target-100", "npc-maintenance-103", "npc-aggressive-target-100"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("pending retry commands=%v want=%v", got, want)
	}
}

func TestNPCAggressiveTargetPendingRetryReconcilesPostCombatActiveListDrift(t *testing.T) {
	state := npcCombatLethalTickFixture(t)
	source := state.Rooms[1]
	source.PlayerIDs = []string{"player-b"}
	source.NPCIDs = []string{"npc-b"}
	state.Rooms[1] = source

	observer := state.Players["player-a"]
	observer.Body.RoomID = 2
	state.Players["player-a"] = observer
	state.Rooms[2] = world.RoomState{
		Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2, Name: "관찰실"}},
		PlayerIDs: []string{"player-a"},
		NPCIDs:    []string{"npc-a"},
	}
	acquisitionNPC := state.NPCs["npc-a"]
	acquisitionNPC.Body.RoomID = 2
	acquisitionNPC.Body.Flags[6/8] |= 1 << (6 % 8) // MAGGRE
	acquisitionNPC.Enemies = []world.NPCEnemy{}
	state.NPCs["npc-a"] = acquisitionNPC
	state.ActiveNPCIDs = []string{"npc-b", "npc-a"}
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}

	store := newNPCAITickStore(t, state)
	now := int32(100)
	connector := newNPCAITickConnector(t, store, func() (int32, int) { return now, 12 }, func(_ int, high int) int {
		return high
	})

	maintenance, ran, err := connector.RunNPCMaintenanceTick(context.Background(), time.Second)
	if err != nil || !ran {
		t.Fatalf("maintenance=%+v ran=%v err=%v", maintenance, ran, err)
	}
	var maintenanceResult world.NPCMaintenanceResult
	if err := json.Unmarshal(maintenance.Response, &maintenanceResult); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(maintenanceResult.ActiveNPCIDs, []string{"npc-b", "npc-a"}) || len(maintenanceResult.Actions) != 2 || !maintenanceResult.Actions[0].AttackReady || !maintenanceResult.Actions[1].AttackReady {
		t.Fatalf("maintenance result=%+v", maintenanceResult)
	}

	combat, err := connector.RunNPCCombatPhase(context.Background(), "npc-combat-last-player", 100, 100)
	if err != nil || combat.Replayed {
		t.Fatalf("combat=%+v err=%v", combat, err)
	}
	combatSummary := decodeNPCAggressiveCombatSummary(t, combat.Response)
	if len(combatSummary.Deaths) != 1 || !combatSummary.Attacks[0].Lethal {
		t.Fatalf("combat summary=%+v", combatSummary)
	}
	postCombat := npcAITickStoredState(t, store)
	if !reflect.DeepEqual(postCombat.ActiveNPCIDs, []string{"npc-a"}) || postCombat.Players["player-b"].Body.RoomID != 1008 {
		t.Fatalf("post-combat state active=%v player=%+v", postCombat.ActiveNPCIDs, postCombat.Players["player-b"])
	}

	store.failCommitOnce = true
	first, ran, err := connector.RunNPCAggressiveTargetTickAfterMaintenance(context.Background(), time.Second, maintenance)
	if err == nil || !ran || first.Replayed {
		t.Fatalf("uncertain acquisition=%+v ran=%v err=%v", first, ran, err)
	}
	if store.commitAttempts != 3 || store.commits != 2 {
		t.Fatalf("uncertain acquisition did not reach commit attempts=%d commits=%d", store.commitAttempts, store.commits)
	}

	now = 101
	advancedMaintenance, ran, err := connector.RunNPCMaintenanceTick(context.Background(), time.Second)
	if err != nil || !ran {
		t.Fatalf("advanced maintenance=%+v ran=%v err=%v", advancedMaintenance, ran, err)
	}
	advancedState := npcAITickStoredState(t, store)
	if !reflect.DeepEqual(advancedState.ActiveNPCIDs, []string{"npc-a"}) {
		t.Fatalf("advanced maintenance active=%v", advancedState.ActiveNPCIDs)
	}

	retry, ran, err := connector.RunNPCAggressiveTargetTickAfterMaintenance(context.Background(), time.Second, advancedMaintenance)
	if err != nil || !ran || retry.Replayed {
		t.Fatalf("pending retry=%+v ran=%v err=%v", retry, ran, err)
	}
	retryResult := decodeNPCAggressiveTargetResult(t, retry.Response)
	if retryResult.Now != 100 || !retryResult.Changed || len(retryResult.Events) != 1 || len(retryResult.Actions) != 1 || retryResult.Actions[0].NPCID != "npc-a" || retryResult.Actions[0].Status != "target-acquired" || retryResult.Events[0].TargetID != "player-a" {
		t.Fatalf("pending retry result=%+v", retryResult)
	}
	if len(store.requests) != 5 || !bytes.Equal(store.requests[2], store.requests[4]) {
		t.Fatalf("pending request changed across retry requests=%q", store.requests)
	}

	later, ran, err := connector.RunNPCAggressiveTargetTick(context.Background(), time.Second)
	if err != nil || !ran || later.Replayed {
		t.Fatalf("later acquisition=%+v ran=%v err=%v", later, ran, err)
	}
	laterResult := decodeNPCAggressiveTargetResult(t, later.Response)
	if laterResult.Now != 101 || !laterResult.NoOp || laterResult.Changed || len(laterResult.Actions) != 1 || laterResult.Actions[0].NPCID != "npc-a" || laterResult.Actions[0].Status != "existing-enemy" {
		t.Fatalf("later acquisition result=%+v", laterResult)
	}
	if got, want := store.commands, []string{"npc-maintenance-100", "npc-combat-last-player", "npc-aggressive-target-100", "npc-maintenance-101", "npc-aggressive-target-100", "npc-aggressive-target-101"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("phase commands=%v want=%v", got, want)
	}
}

func TestNPCAggressiveTargetTickReplaysWithoutRNGOrFanout(t *testing.T) {
	state := npcAITickState(t, 1)
	store := newNPCAITickStore(t, state)
	now := int32(100)
	rollCalls := 0
	roll := func(low, _ int) int {
		rollCalls++
		return low
	}
	firstConnector := newNPCAITickConnector(t, store, func() (int32, int) { return now, 12 }, roll)
	firstConnections := installNPCAITickConnections(t, firstConnector, "target", "observer", "away")
	first, ran, err := firstConnector.RunNPCAggressiveTargetTick(context.Background(), time.Second)
	if err != nil || !ran || first.Replayed {
		t.Fatalf("first receipt=%+v ran=%v err=%v", first, ran, err)
	}
	if rollCalls != 1 || store.commits != 1 {
		t.Fatalf("first RNG/commit calls=%d/%d", rollCalls, store.commits)
	}
	if got := <-firstConnections["target"].events; got == "" {
		t.Fatal("first target event empty")
	}
	if got := <-firstConnections["observer"].events; got == "" {
		t.Fatal("first observer event empty")
	}

	secondConnector := newNPCAITickConnector(t, store, func() (int32, int) { return now, 12 }, roll)
	secondConnections := installNPCAITickConnections(t, secondConnector, "target", "observer", "away")
	second, ran, err := secondConnector.RunNPCAggressiveTargetTick(context.Background(), time.Second)
	if err != nil || !ran || !second.Replayed {
		t.Fatalf("replay receipt=%+v ran=%v err=%v", second, ran, err)
	}
	if rollCalls != 1 || store.commits != 1 {
		t.Fatalf("replay repeated RNG/commit calls=%d/%d", rollCalls, store.commits)
	}
	for id, connection := range secondConnections {
		select {
		case event := <-connection.events:
			t.Fatalf("replay delivered duplicate to %s: %q", id, event)
		default:
		}
	}
}

func TestNPCAggressiveTargetFanoutCommitsFirstAndPreservesOrderAndExclusion(t *testing.T) {
	state := npcAITickState(t, 2)
	store := newNPCAITickStore(t, state)
	connector := newNPCAITickConnector(t, store, func() (int32, int) { return 100, 12 }, func(low, _ int) int { return low })
	connections := installNPCAITickConnections(t, connector, "target", "observer", "away")

	receipt, ran, err := connector.RunNPCAggressiveTargetTick(context.Background(), time.Second)
	if err != nil || !ran || receipt.Replayed {
		t.Fatalf("receipt=%+v ran=%v err=%v", receipt, ran, err)
	}
	if store.commits != 1 || store.commitAttempts != 1 {
		t.Fatalf("commit-before-fanout attempts=%d commits=%d", store.commitAttempts, store.commits)
	}
	result := decodeNPCAggressiveTargetResult(t, receipt.Response)
	if len(result.Events) != 2 {
		t.Fatalf("events=%+v", result.Events)
	}
	for i, event := range result.Events {
		if event.RoomID != 1 || event.ExcludeTargetID != event.TargetID || event.TargetID != "target" {
			t.Fatalf("event[%d]=%+v", i, event)
		}
		if got := <-connections["target"].events; got != event.TargetText {
			t.Fatalf("target event[%d]=%q want=%q", i, got, event.TargetText)
		}
		if got := <-connections["observer"].events; got != event.RoomText {
			t.Fatalf("observer event[%d]=%q want=%q", i, got, event.RoomText)
		}
	}
	for id, connection := range connections {
		select {
		case event := <-connection.events:
			t.Fatalf("unexpected extra event for %s: %q", id, event)
		default:
		}
	}
}

func TestNPCAggressiveTargetTickFailsClosedForNilOrInvalidWorldState(t *testing.T) {
	validWithoutCanonicalNPCs := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "빈 방"}}},
		},
		Players: map[string]world.PlayerState{},
	}
	validRaw, err := json.Marshal(validWithoutCanonicalNPCs)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		raw  json.RawMessage
		want string
	}{
		{name: "nil snapshot", raw: nil, want: ""},
		{name: "null snapshot", raw: json.RawMessage(`null`), want: "null"},
		{name: "canonical NPCs unresolved", raw: validRaw, want: "valid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &npcAITickStore{state: append(json.RawMessage(nil), tc.raw...), receipts: map[string]npcAITickStoredReceipt{}}
			connector := newNPCAITickConnector(t, store, func() (int32, int) { return 100, 12 }, func(int, int) int {
				t.Fatal("invalid state consumed RNG")
				return 0
			})
			before := append(json.RawMessage(nil), store.state...)
			receipt, ran, err := connector.RunNPCAggressiveTargetTick(context.Background(), time.Second)
			if err == nil || !ran || !reflect.DeepEqual(receipt, storage.WorldReceipt{}) {
				t.Fatalf("accepted invalid state receipt=%+v ran=%v err=%v", receipt, ran, err)
			}
			if store.commits != 0 || !bytes.Equal(store.state, before) {
				t.Fatalf("invalid state partially committed commits=%d before=%q after=%q", store.commits, before, store.state)
			}
		})
	}
}

type npcAITickStoredReceipt struct {
	request json.RawMessage
	receipt storage.WorldReceipt
}

type npcAITickStore struct {
	mu sync.Mutex

	state    json.RawMessage
	revision int64
	receipts map[string]npcAITickStoredReceipt

	commands       []string
	requests       []json.RawMessage
	commitAttempts int
	commits        int
	failCommitOnce bool
}

func newNPCAITickStore(t *testing.T, state world.State) *npcAITickStore {
	t.Helper()
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return &npcAITickStore{state: raw, receipts: map[string]npcAITickStoredReceipt{}}
}

func (s *npcAITickStore) ReadWorldReceipt(_ context.Context, _ string, commandID string, request json.RawMessage) (storage.WorldReceipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, ok := s.receipts[commandID]
	if !ok {
		return storage.WorldReceipt{}, sql.ErrNoRows
	}
	if !bytes.Equal(stored.request, request) {
		return storage.WorldReceipt{}, storage.ErrCommandConflict
	}
	receipt := stored.receipt
	receipt.Response = append(json.RawMessage(nil), receipt.Response...)
	receipt.Replayed = true
	return receipt, nil
}

func (s *npcAITickStore) LoadWorld(_ context.Context, _ string) (storage.WorldSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return storage.WorldSnapshot{Revision: s.revision, State: append(json.RawMessage(nil), s.state...)}, nil
}

func (s *npcAITickStore) CommitWorldCommand(_ context.Context, _ string, commandID string, request json.RawMessage, expected int64, state, response json.RawMessage) (storage.WorldReceipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commitAttempts++
	s.commands = append(s.commands, commandID)
	s.requests = append(s.requests, append(json.RawMessage(nil), request...))
	if s.failCommitOnce {
		s.failCommitOnce = false
		return storage.WorldReceipt{}, errors.New("uncertain NPC aggressive target commit")
	}
	if expected != s.revision {
		return storage.WorldReceipt{}, storage.ErrWorldConflict
	}
	s.revision++
	s.state = append(json.RawMessage(nil), state...)
	receipt := storage.WorldReceipt{Revision: s.revision, Response: append(json.RawMessage(nil), response...)}
	s.receipts[commandID] = npcAITickStoredReceipt{
		request: append(json.RawMessage(nil), request...),
		receipt: receipt,
	}
	s.commits++
	return receipt, nil
}

func npcAITickStoredState(t *testing.T, store *npcAITickStore) world.State {
	t.Helper()
	store.mu.Lock()
	raw := append(json.RawMessage(nil), store.state...)
	store.mu.Unlock()
	state, err := world.DecodeState(raw)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func newNPCAITickConnector(t *testing.T, store *npcAITickStore, clock func() (int32, int), roll func(int, int) int) *WorldConnector {
	t.Helper()
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store:       store,
		WorldID:     "npc-aggressive-target-test-world",
		Clock:       clock,
		MaxSessions: 3,
		Roll:        roll,
	})
	if err != nil {
		t.Fatal(err)
	}
	return connector
}

func installNPCAITickConnections(t *testing.T, connector *WorldConnector, ids ...string) map[string]*worldConnection {
	t.Helper()
	connections := make(map[string]*worldConnection, len(ids))
	for _, id := range ids {
		lease, err := connector.owners.Acquire(id)
		if err != nil {
			t.Fatal(err)
		}
		if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
			t.Fatal(err)
		}
		connection := &worldConnection{game: connector, lease: lease, ready: true, events: make(chan string, 8)}
		connections[id] = connection
		connector.mu.Lock()
		connector.connections[connection] = struct{}{}
		connector.mu.Unlock()
	}
	return connections
}

func npcAITickState(t *testing.T, npcCount int) world.State {
	t.Helper()
	room := world.RoomState{
		Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "AI 방"}},
		PlayerIDs: []string{"target", "observer"},
	}
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: room,
			2: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2, Name: "먼 방"}},
				PlayerIDs: []string{"away"},
			},
		},
		Players: map[string]world.PlayerState{
			"target":   {Body: world.LegacyMonster{Name: "대상", Type: 0, RoomID: 1, Level: 8, HPMax: 100, HPCurrent: 100, Stats: [5]byte{10, 10, 10, 10, 24}}, Online: true},
			"observer": {Body: world.LegacyMonster{Name: "관전자", Type: 0, RoomID: 1, Level: 8, HPMax: 100, HPCurrent: 100, Stats: [5]byte{10, 10, 10, 10, 24}}, Online: true},
			"away":     {Body: world.LegacyMonster{Name: "먼사람", Type: 0, RoomID: 2, Level: 8, HPMax: 100, HPCurrent: 100, Stats: [5]byte{10, 10, 10, 10, 24}}, Online: true},
		},
		NPCs:         map[string]world.NPCState{},
		ActiveNPCIDs: []string{},
	}
	for i := 1; i <= npcCount; i++ {
		id := "wolf-" + string(rune('0'+i))
		name := "늑대" + string(rune('0'+i))
		body := world.LegacyMonster{
			Name: name, Type: 1, Class: 4, RoomID: 1, Level: 8,
			HPMax: 100, HPCurrent: 100, Thaco: 20, DiceCount: 1, DiceSides: 1,
			Stats: [5]byte{10, 10, 10, 10, 10},
		}
		body.Flags[6/8] |= 1 << (6 % 8) // MAGGRE
		state.NPCs[id] = world.NPCState{Body: body, Enemies: []world.NPCEnemy{}}
		room := state.Rooms[1]
		room.NPCIDs = append(room.NPCIDs, id)
		state.Rooms[1] = room
		state.ActiveNPCIDs = append(state.ActiveNPCIDs, id)
	}
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	return state
}

func decodeNPCAggressiveTargetResult(t *testing.T, raw json.RawMessage) world.NPCAggressiveTargetResult {
	t.Helper()
	var result world.NPCAggressiveTargetResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func decodeNPCAggressiveCombatSummary(t *testing.T, raw json.RawMessage) NPCCombatTickSummary {
	t.Helper()
	var summary NPCCombatTickSummary
	if err := json.Unmarshal(raw, &summary); err != nil {
		t.Fatal(err)
	}
	return summary
}
