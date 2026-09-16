package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

// npcAggressiveTargetTick retains the complete command boundary while a
// durable write has an uncertain outcome. In particular, request is retained
// as bytes so a retry cannot silently bind a different JSON request even when
// the clock has advanced.
type npcAggressiveTargetTick struct {
	slot                         int64
	now                          int32
	commandID                    string
	request                      json.RawMessage
	maintenanceReadyAttackTimers map[string]world.LegacyTimer
}

type npcAggressiveTargetTickRequest struct {
	Kind string `json:"kind"`
	Slot int64  `json:"slot"`
	Now  int32  `json:"now"`
}

type npcAggressiveTargetTickState struct {
	lastSlot int64
	pending  *npcAggressiveTargetTick
}

// npcAggressiveTargetMaintenanceBoundary carries only the durable
// maintenance facts needed to reproduce C's same-pass timer boundary. The
// original maintenance receipt remains the authority; this in-memory view is
// retained with a pending acquisition request and rebuilt from that receipt
// after a connector restart.
type npcAggressiveTargetMaintenanceBoundary struct {
	now               int32
	readyAttackTimers map[string]world.LegacyTimer
}

// WorldConnector's struct is intentionally not widened by this phase. A
// connector-local map matches the existing combat tick compatibility seam and
// keeps the durable state outside the persisted world snapshot.
var npcAggressiveTargetTickStates sync.Map // map[*WorldConnector]*npcAggressiveTargetTickState

func npcAggressiveTargetTickStateFor(g *WorldConnector) *npcAggressiveTargetTickState {
	value, _ := npcAggressiveTargetTickStates.LoadOrStore(g, &npcAggressiveTargetTickState{lastSlot: -1})
	return value.(*npcAggressiveTargetTickState)
}

func npcAggressiveTargetIntervalSeconds(interval time.Duration) (int64, error) {
	if interval <= 0 || interval%time.Second != 0 {
		return 0, errors.New("NPC aggressive target interval must be a positive whole number of seconds")
	}
	seconds := int64(interval / time.Second)
	if seconds < 1 {
		return 0, errors.New("NPC aggressive target interval must be at least one second")
	}
	return seconds, nil
}

func npcAggressiveTargetCommandID(slot int64) string {
	return fmt.Sprintf("npc-aggressive-target-%d", slot)
}

func marshalNPCAggressiveTargetRequest(slot int64, now int32) (json.RawMessage, error) {
	if slot < 0 || now < 0 {
		return nil, errors.New("invalid NPC aggressive target slot or time")
	}
	request, err := json.Marshal(npcAggressiveTargetTickRequest{
		Kind: "npc-aggressive-target-phase",
		Slot: slot,
		Now:  now,
	})
	if err != nil {
		return nil, err
	}
	return request, nil
}

func decodeNPCAggressiveTargetRequest(raw json.RawMessage) (npcAggressiveTargetTickRequest, error) {
	var request npcAggressiveTargetTickRequest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return npcAggressiveTargetTickRequest{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return npcAggressiveTargetTickRequest{}, errors.New("trailing NPC aggressive target request data")
		}
		return npcAggressiveTargetTickRequest{}, err
	}
	if request.Kind != "npc-aggressive-target-phase" || request.Slot < 0 || request.Now < 0 {
		return npcAggressiveTargetTickRequest{}, errors.New("invalid NPC aggressive target request")
	}
	return request, nil
}

func npcAggressiveTargetMaintenanceBoundaryFromReceipt(receipt storage.WorldReceipt) (*npcAggressiveTargetMaintenanceBoundary, error) {
	raw := bytes.TrimSpace(receipt.Response)
	if len(raw) == 0 {
		// A cadence that was already suppressed has no maintenance response to
		// carry. The pending acquisition state, when present, already retains
		// its own boundary; otherwise the strict path remains appropriate.
		return nil, nil
	}
	if raw[0] != '{' {
		return nil, errors.New("invalid NPC maintenance receipt response object")
	}
	var result world.NPCMaintenanceResult
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("invalid NPC maintenance receipt response: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("trailing NPC maintenance receipt response data")
		}
		return nil, fmt.Errorf("invalid NPC maintenance receipt response: %w", err)
	}
	if result.Now < 0 || result.ActiveNPCIDs == nil {
		return nil, errors.New("invalid NPC maintenance receipt time")
	}
	boundary := &npcAggressiveTargetMaintenanceBoundary{
		now:               result.Now,
		readyAttackTimers: map[string]world.LegacyTimer{},
	}
	for _, action := range result.Actions {
		if action.NPCID == "" {
			return nil, errors.New("NPC maintenance receipt has an empty NPC identity")
		}
		if action.Wandered && !action.RemovedFromActive {
			return nil, fmt.Errorf("NPC maintenance receipt has an invalid wander action for %q", action.NPCID)
		}
		if !action.AttackReady {
			if action.AttackInterval != 0 {
				return nil, fmt.Errorf("NPC maintenance receipt has an attack interval without readiness for %q", action.NPCID)
			}
			continue
		}
		if action.AttackInterval != 2 && action.AttackInterval != 3 {
			return nil, fmt.Errorf("NPC maintenance receipt has an invalid attack interval for %q", action.NPCID)
		}
		if action.RemovedFromActive {
			if !action.Wandered {
				return nil, fmt.Errorf("NPC maintenance receipt removed a ready NPC without wandering %q", action.NPCID)
			}
			continue
		}
		if _, exists := boundary.readyAttackTimers[action.NPCID]; exists {
			return nil, fmt.Errorf("NPC maintenance receipt repeated ready NPC %q", action.NPCID)
		}
		boundary.readyAttackTimers[action.NPCID] = world.LegacyTimer{LastTime: result.Now, Interval: action.AttackInterval}
	}
	return boundary, nil
}

func cloneNPCAggressiveTargetReadyAttackTimers(boundary *npcAggressiveTargetMaintenanceBoundary) map[string]world.LegacyTimer {
	if boundary == nil || len(boundary.readyAttackTimers) == 0 {
		return nil
	}
	cloned := make(map[string]world.LegacyTimer, len(boundary.readyAttackTimers))
	for npcID, timer := range boundary.readyAttackTimers {
		cloned[npcID] = timer
	}
	return cloned
}

func prepareNPCAggressiveTargetPostMaintenanceState(state world.State, now int32, readyAttackTimers map[string]world.LegacyTimer) (world.State, map[string]world.LegacyTimer, error) {
	if len(readyAttackTimers) == 0 {
		return state, nil, nil
	}
	ids := make([]string, 0, len(readyAttackTimers))
	for npcID := range readyAttackTimers {
		ids = append(ids, npcID)
	}
	sort.Strings(ids)
	originals := make(map[string]world.LegacyTimer, len(ids))
	applyOverride := true
	for _, npcID := range ids {
		expected := readyAttackTimers[npcID]
		if expected.LastTime < 0 || (expected.Interval != 2 && expected.Interval != 3) {
			return world.State{}, nil, fmt.Errorf("NPC maintenance timer boundary is invalid for %q", npcID)
		}
		if !npcAggressiveTargetActiveNPC(state.ActiveNPCIDs, npcID) {
			return world.State{}, nil, fmt.Errorf("NPC maintenance ready identity is not active: %q", npcID)
		}
		npc, ok := state.NPCs[npcID]
		if !ok || npc.Body.Type != world.TurnMonsterType {
			return world.State{}, nil, fmt.Errorf("NPC maintenance ready identity is unresolved: %q", npcID)
		}
		current := npc.Body.Timers[world.TurnAttackTimerIndex]
		if expected.LastTime != now || current.LastTime != expected.LastTime || current.Interval != expected.Interval {
			// The maintenance prefix is only an override for the exact target
			// slot it produced. If a later maintenance pass has already changed
			// the timer, keep the pending command but let the strict planner
			// decide against the old command timestamp instead of rejecting the
			// retry forever.
			applyOverride = false
		}
		originals[npcID] = current
	}
	if !applyOverride {
		return state, nil, nil
	}
	for _, npcID := range ids {
		npc := state.NPCs[npcID]
		current := originals[npcID]
		// C's maintenance prefix has already admitted this NPC to the same
		// update_active pass. Make only that prefix's timer due for the world
		// planner, which still enforces strict readiness on every other path.
		current.LastTime = now - current.Interval
		npc.Body.Timers[world.TurnAttackTimerIndex] = current
		state.NPCs[npcID] = npc
	}
	return state, originals, nil
}

func restoreNPCAggressiveTargetPostMaintenanceState(state world.State, result world.NPCAggressiveTargetResult, now int32, originals map[string]world.LegacyTimer) (world.State, world.NPCAggressiveTargetResult, error) {
	seen := make(map[string]bool, len(originals))
	for index := range result.Actions {
		action := &result.Actions[index]
		original, wasReady := originals[action.NPCID]
		if !wasReady {
			continue
		}
		seen[action.NPCID] = true
		npc, ok := state.NPCs[action.NPCID]
		if !ok {
			return world.State{}, world.NPCAggressiveTargetResult{}, fmt.Errorf("NPC maintenance ready identity disappeared during acquisition: %q", action.NPCID)
		}
		if action.TimerWrite {
			expectedAfter := original
			expectedAfter.LastTime = now
			expectedAfter.Interval = 0
			if npc.Body.Timers[world.TurnAttackTimerIndex] != expectedAfter || action.AttackTimerAfter != expectedAfter {
				return world.State{}, world.NPCAggressiveTargetResult{}, fmt.Errorf("NPC acquisition timer transition changed for %q", action.NPCID)
			}
			// Plan ran against a due view of the timer. The durable result must
			// expose the real maintenance-before/acquisition-after transition.
			action.AttackTimerBefore = original
			action.AttackTimerAfter = expectedAfter
			continue
		}
		npc.Body.Timers[world.TurnAttackTimerIndex] = original
		state.NPCs[action.NPCID] = npc
	}
	for npcID := range originals {
		if !seen[npcID] {
			return world.State{}, world.NPCAggressiveTargetResult{}, fmt.Errorf("NPC maintenance ready identity missing from acquisition: %q", npcID)
		}
	}
	if err := state.Validate(); err != nil {
		return world.State{}, world.NPCAggressiveTargetResult{}, err
	}
	return state, result, nil
}

func npcAggressiveTargetActiveNPC(active []string, npcID string) bool {
	for _, id := range active {
		if id == npcID {
			return true
		}
	}
	return false
}

// RunNPCAggressiveTargetTick executes the post-combat target-acquisition
// phase for one deterministic cadence slot. It samples the clock only while
// creating a new pending boundary; retrying an uncertain commit reuses the
// exact slot, timestamp, command ID, and request without another RNG draw.
// Direct/standalone callers retain the strict world readiness gate.
func (g *WorldConnector) RunNPCAggressiveTargetTick(ctx context.Context, interval time.Duration) (storage.WorldReceipt, bool, error) {
	return g.runNPCAggressiveTargetTick(ctx, interval, nil)
}

// RunNPCAggressiveTargetTickAfterMaintenance is the ordered scheduler seam.
// C's update_active advances LT_ATTCK during maintenance and then reaches the
// aggressive branch in the same pass, so target acquisition must not re-gate
// those exact NPCs on the timer that maintenance just advanced. The direct
// phase API deliberately does not use this seam and remains strict.
func (g *WorldConnector) RunNPCAggressiveTargetTickAfterMaintenance(ctx context.Context, interval time.Duration, maintenance storage.WorldReceipt) (storage.WorldReceipt, bool, error) {
	if g == nil {
		return storage.WorldReceipt{}, false, errors.New("nil NPC aggressive target connector")
	}
	boundary, err := npcAggressiveTargetMaintenanceBoundaryFromReceipt(maintenance)
	if err != nil {
		return storage.WorldReceipt{}, false, err
	}
	return g.runNPCAggressiveTargetTick(ctx, interval, boundary)
}

func (g *WorldConnector) runNPCAggressiveTargetTick(ctx context.Context, interval time.Duration, boundary *npcAggressiveTargetMaintenanceBoundary) (storage.WorldReceipt, bool, error) {
	if g == nil {
		return storage.WorldReceipt{}, false, errors.New("nil NPC aggressive target connector")
	}
	if ctx == nil {
		return storage.WorldReceipt{}, false, errors.New("nil NPC aggressive target tick context")
	}
	seconds, err := npcAggressiveTargetIntervalSeconds(interval)
	if err != nil {
		return storage.WorldReceipt{}, false, err
	}
	g.tickMu.Lock()
	defer g.tickMu.Unlock()
	if err := ctx.Err(); err != nil {
		return storage.WorldReceipt{}, false, err
	}
	if g.config.Clock == nil {
		return storage.WorldReceipt{}, false, errors.New("NPC aggressive target connector clock is unavailable")
	}

	state := npcAggressiveTargetTickStateFor(g)
	pending := state.pending
	if pending == nil {
		now, _ := g.config.Clock()
		if now < 0 {
			return storage.WorldReceipt{}, false, errors.New("NPC aggressive target clock is outside canonical range")
		}
		slot := int64(now) / seconds
		if slot <= state.lastSlot {
			return storage.WorldReceipt{}, false, nil
		}
		slotNow := slot * seconds
		if slotNow < 0 || slotNow > 2147483647 {
			return storage.WorldReceipt{}, false, errors.New("NPC aggressive target slot timestamp overflow")
		}
		maintenanceBoundary := boundary
		if maintenanceBoundary != nil && maintenanceBoundary.now != int32(slotNow) {
			// The maintenance receipt may only bypass strict readiness for
			// the same authoritative target slot/time. A phase that crossed a
			// wall-clock boundary must use the ordinary planner gate.
			maintenanceBoundary = nil
		}
		request, err := marshalNPCAggressiveTargetRequest(slot, int32(slotNow))
		if err != nil {
			return storage.WorldReceipt{}, false, err
		}
		pending = &npcAggressiveTargetTick{
			slot:                         slot,
			now:                          int32(slotNow),
			commandID:                    npcAggressiveTargetCommandID(slot),
			request:                      append(json.RawMessage(nil), request...),
			maintenanceReadyAttackTimers: cloneNPCAggressiveTargetReadyAttackTimers(maintenanceBoundary),
		}
		state.pending = pending
	}
	if len(pending.request) == 0 {
		return storage.WorldReceipt{}, true, errors.New("NPC aggressive target pending request is unavailable")
	}

	receipt, err := g.runNPCAggressiveTargetPhaseAt(ctx, pending.commandID, pending.request, pending.maintenanceReadyAttackTimers)
	if err != nil {
		// Do not release pending: an error from CommitWorldCommand can have an
		// unknown outcome, and retry must use the identical command boundary.
		return storage.WorldReceipt{}, true, err
	}
	state.lastSlot = pending.slot
	state.pending = nil
	return receipt, true, nil
}

// RunNPCAggressiveTargetScheduler owns this phase on a standalone wall-clock
// cadence for hosts that do not use NPCWorldScheduler. The ordered production
// host uses the optional scheduler seam instead.
func (g *WorldConnector) RunNPCAggressiveTargetScheduler(ctx context.Context, interval time.Duration) error {
	if g == nil {
		return errors.New("nil NPC aggressive target scheduler connector")
	}
	if ctx == nil {
		return errors.New("nil NPC aggressive target scheduler context")
	}
	if _, err := npcAggressiveTargetIntervalSeconds(interval); err != nil {
		return err
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		_, _, err := g.RunNPCAggressiveTargetTick(ctx, interval)
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

// RunNPCAggressiveTargetPhase runs a caller-bound command directly. Normal
// ordered execution should use RunNPCAggressiveTargetTick so the connector
// owns slot/clock/pending retry binding.
func (g *WorldConnector) RunNPCAggressiveTargetPhase(ctx context.Context, commandID string, slot int64, now int32) (storage.WorldReceipt, error) {
	request, err := marshalNPCAggressiveTargetRequest(slot, now)
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return g.runNPCAggressiveTargetPhaseAt(ctx, commandID, request, nil)
}

func (g *WorldConnector) runNPCAggressiveTargetPhaseAt(ctx context.Context, commandID string, request json.RawMessage, maintenanceReadyAttackTimers map[string]world.LegacyTimer) (storage.WorldReceipt, error) {
	if g == nil {
		return storage.WorldReceipt{}, errors.New("nil NPC aggressive target connector")
	}
	if ctx == nil {
		return storage.WorldReceipt{}, errors.New("nil NPC aggressive target phase context")
	}
	if commandID == "" {
		return storage.WorldReceipt{}, errors.New("missing NPC aggressive target phase command ID")
	}
	parsed, err := decodeNPCAggressiveTargetRequest(request)
	if err != nil {
		return storage.WorldReceipt{}, err
	}

	g.commandMu.Lock()
	receipt, err := engine.Execute(ctx, g.config.Store, g.config.WorldID, commandID, request, func(raw json.RawMessage) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		planningState, originalAttackTimers, err := prepareNPCAggressiveTargetPostMaintenanceState(state, parsed.Now, maintenanceReadyAttackTimers)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := planningState.PlanNPCAggressiveTargetAcquisition(parsed.Now, g.config.Roll)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := planningState.ApplyNPCAggressiveTargetAcquisition(proposal)
		if err != nil {
			return nil, nil, err
		}
		if len(originalAttackTimers) != 0 {
			next, result, err = restoreNPCAggressiveTargetPostMaintenanceState(next, result, parsed.Now, originalAttackTimers)
			if err != nil {
				return nil, nil, err
			}
		}
		saved, err := json.Marshal(next)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		return saved, response, err
	})
	g.commandMu.Unlock()
	if err != nil {
		return receipt, err
	}
	if receipt.Replayed {
		return receipt, nil
	}

	// The command receipt is durable before any socket is touched. A snapshot
	// read that races shutdown or recovery simply suppresses this best-effort
	// projection; replay remains fan-out free by the receipt guard above.
	var result world.NPCAggressiveTargetResult
	if err := json.Unmarshal(receipt.Response, &result); err != nil {
		return receipt, nil
	}
	after, ok := g.snapshot(ctx)
	if !ok {
		return receipt, nil
	}
	for _, event := range result.Events {
		g.publishNPCAggressiveTarget(after, event)
	}
	return receipt, nil
}

// publishNPCAggressiveTarget delivers the source-composed private target
// line first, then the room observer line to the current room occupants except
// the selected target. It accepts only the canonical event identities from
// the committed state and never mutates that state.
func (g *WorldConnector) publishNPCAggressiveTarget(after world.State, event world.NPCAggressiveTargetEvent) {
	if g == nil || event.NPCID == "" || event.NPCName == "" || event.TargetID == "" || event.TargetName == "" || event.ExcludeTargetID != event.TargetID || event.TargetText == "" || event.RoomText == "" {
		return
	}
	npc, ok := after.NPCs[event.NPCID]
	if !ok || npc.Body.Type != 1 || npc.Body.RoomID != event.RoomID || npc.Body.Name != event.NPCName {
		return
	}
	target, ok := after.Players[event.TargetID]
	if !ok || !target.Online || target.Body.RoomID != event.RoomID || target.Body.Name != event.TargetName {
		return
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	connections := make([]*worldConnection, 0, len(g.connections))
	for connection := range g.connections {
		connections = append(connections, connection)
	}
	sort.SliceStable(connections, func(i, j int) bool {
		return connections[i].lease.ActorID < connections[j].lease.ActorID
	})

	// The selected player receives the private line before any room observer
	// receives the public line for this event.
	for _, connection := range connections {
		if connection.lease.ActorID != event.TargetID || connection.events == nil {
			continue
		}
		select {
		case connection.events <- event.TargetText:
		default:
		}
	}
	for _, connection := range connections {
		actorID := connection.lease.ActorID
		if actorID == event.ExcludeTargetID || connection.events == nil {
			continue
		}
		player, ok := after.Players[actorID]
		if !ok || !player.Online || player.Body.RoomID != event.RoomID {
			continue
		}
		select {
		case connection.events <- event.RoomText:
		default:
		}
	}
}
