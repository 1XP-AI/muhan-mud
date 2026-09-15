package transport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

// npcResourceTick is the exact request retained while a NPC-resource command
// has an unknown durable outcome. The next attempt must reuse both values;
// recomputing the slot timestamp would otherwise create a second respawn.
type npcResourceTick struct {
	slot      int64
	now       int32
	commandID string
}

type npcResourcePhaseSummary struct {
	Now           int32    `json:"now"`
	SpawnedRooms  []int16  `json:"spawned_rooms,omitempty"`
	SpawnedNPCIDs []string `json:"spawned_npc_ids,omitempty"`
	Unmigrated    []int16  `json:"unmigrated_rooms,omitempty"`
}

func npcResourceIntervalSeconds(interval time.Duration) (int64, error) {
	if interval <= 0 || interval%time.Second != 0 {
		return 0, errors.New("NPC resource interval must be a positive whole number of seconds")
	}
	seconds := int64(interval / time.Second)
	if seconds < 1 {
		return 0, errors.New("NPC resource interval must be at least one second")
	}
	return seconds, nil
}

func npcResourceCommandID(slot int64) string {
	return fmt.Sprintf("npc-resources-%d", slot)
}

// RunNPCResourceTick executes the identity-aware permanent-NPC phase for the
// next deterministic cadence slot. An unsuccessful durable attempt retains
// the exact slot, timestamp, and command ID for the next call.
func (g *WorldConnector) RunNPCResourceTick(ctx context.Context, interval time.Duration) (storage.WorldReceipt, bool, error) {
	if ctx == nil {
		return storage.WorldReceipt{}, false, errors.New("nil NPC resource tick context")
	}
	seconds, err := npcResourceIntervalSeconds(interval)
	if err != nil {
		return storage.WorldReceipt{}, false, err
	}
	g.tickMu.Lock()
	defer g.tickMu.Unlock()
	if err := ctx.Err(); err != nil {
		return storage.WorldReceipt{}, false, err
	}
	now, _ := g.config.Clock()
	pending := g.pendingNPCResource
	if pending == nil {
		slot := int64(now) / seconds
		if slot <= g.lastNPCResourceSlot {
			return storage.WorldReceipt{}, false, nil
		}
		slotNow := slot * seconds
		if slotNow < -2147483648 || slotNow > 2147483647 {
			return storage.WorldReceipt{}, false, errors.New("NPC resource slot timestamp overflow")
		}
		pending = &npcResourceTick{slot: slot, now: int32(slotNow), commandID: npcResourceCommandID(slot)}
		g.pendingNPCResource = pending
	}
	receipt, err := g.runNPCResourcePhaseAt(ctx, pending.commandID, pending.now)
	if err != nil {
		return storage.WorldReceipt{}, true, err
	}
	g.lastNPCResourceSlot = pending.slot
	g.pendingNPCResource = nil
	return receipt, true, nil
}

// RunNPCResourceScheduler owns the NPC identity phase on a wall-clock cadence.
// It does not generate anonymous identities: noncanonical rooms are recorded
// in the durable summary and skipped, while canonical errors retain the exact
// pending request for retry.
func (g *WorldConnector) RunNPCResourceScheduler(ctx context.Context, interval time.Duration) error {
	if ctx == nil {
		return errors.New("nil NPC resource scheduler context")
	}
	if _, err := npcResourceIntervalSeconds(interval); err != nil {
		return err
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		_, _, err := g.RunNPCResourceTick(ctx, interval)
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

func (g *WorldConnector) runNPCResourcePhaseAt(ctx context.Context, commandID string, now int32) (storage.WorldReceipt, error) {
	if commandID == "" {
		return storage.WorldReceipt{}, errors.New("missing NPC resource phase command ID")
	}
	request, err := json.Marshal(struct {
		Kind string `json:"kind"`
		Now  int32  `json:"now"`
	}{Kind: "npc-resource-phase", Now: now})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	g.commandMu.Lock()
	defer g.commandMu.Unlock()
	return engine.Execute(ctx, g.config.Store, g.config.WorldID, commandID, request, func(raw json.RawMessage) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		next, summary, err := planNPCResourcePhase(state, now, g.config.Catalog, g.config.Roll, g.config.Allocate)
		if err != nil {
			return nil, nil, err
		}
		saved, err := json.Marshal(next)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(summary)
		return saved, response, err
	})
}

func planNPCResourcePhase(state world.State, now int32, catalog world.SpawnCatalog, roll func(int, int) int, allocate func() (string, error)) (world.State, npcResourcePhaseSummary, error) {
	if err := state.Validate(); err != nil {
		return world.State{}, npcResourcePhaseSummary{}, err
	}
	summary := npcResourcePhaseSummary{Now: now}
	next := state
	roomIDs := make([]int, 0, len(next.Rooms))
	for id := range next.Rooms {
		roomIDs = append(roomIDs, int(id))
	}
	sort.Ints(roomIDs)
	for _, key := range roomIDs {
		roomID := int16(key)
		room := next.Rooms[roomID]
		// NPCResourceTick requires both the canonical item floor and the
		// canonical world-wide NPC identity map. Legacy/unmigrated resources
		// are evidence in the receipt, never anonymous spawn candidates.
		if next.NPCs == nil || room.Items == nil || len(room.Resource.Objects) != 0 {
			summary.Unmigrated = append(summary.Unmigrated, roomID)
			continue
		}
		proposal, err := next.PlanNPCResourceTick(roomID, catalog, now, roll, allocate)
		if err != nil {
			return world.State{}, npcResourcePhaseSummary{}, err
		}
		candidate, err := next.ApplyNPCResourceTick(proposal)
		if err != nil {
			return world.State{}, npcResourcePhaseSummary{}, err
		}
		if len(proposal.Spawns) != 0 {
			summary.SpawnedRooms = append(summary.SpawnedRooms, roomID)
			for _, spawn := range proposal.Spawns {
				summary.SpawnedNPCIDs = append(summary.SpawnedNPCIDs, spawn.ID)
			}
		}
		next = candidate
	}
	return next, summary, nil
}
