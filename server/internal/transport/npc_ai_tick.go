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
	slot      int64
	now       int32
	commandID string
	request   json.RawMessage
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

// RunNPCAggressiveTargetTick executes the post-combat target-acquisition
// phase for one deterministic cadence slot. It samples the clock only while
// creating a new pending boundary; retrying an uncertain commit reuses the
// exact slot, timestamp, command ID, and request without another RNG draw.
func (g *WorldConnector) RunNPCAggressiveTargetTick(ctx context.Context, interval time.Duration) (storage.WorldReceipt, bool, error) {
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
		request, err := marshalNPCAggressiveTargetRequest(slot, int32(slotNow))
		if err != nil {
			return storage.WorldReceipt{}, false, err
		}
		pending = &npcAggressiveTargetTick{
			slot:      slot,
			now:       int32(slotNow),
			commandID: npcAggressiveTargetCommandID(slot),
			request:   append(json.RawMessage(nil), request...),
		}
		state.pending = pending
	}
	if len(pending.request) == 0 {
		return storage.WorldReceipt{}, true, errors.New("NPC aggressive target pending request is unavailable")
	}

	receipt, err := g.runNPCAggressiveTargetPhaseAt(ctx, pending.commandID, pending.request)
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
	return g.runNPCAggressiveTargetPhaseAt(ctx, commandID, request)
}

func (g *WorldConnector) runNPCAggressiveTargetPhaseAt(ctx context.Context, commandID string, request json.RawMessage) (storage.WorldReceipt, error) {
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
		proposal, err := state.PlanNPCAggressiveTargetAcquisition(parsed.Now, g.config.Roll)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyNPCAggressiveTargetAcquisition(proposal)
		if err != nil {
			return nil, nil, err
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
