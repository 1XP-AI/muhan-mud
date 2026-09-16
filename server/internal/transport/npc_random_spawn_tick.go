package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

// npcRandomSpawnTick retains the complete request boundary while a durable
// commit has an uncertain outcome. In particular, retries reuse the exact
// player descriptor order so the producer's C-compatible random stream cannot
// be rebound to a later clock sample or a different traversal order.
type npcRandomSpawnTick struct {
	slot      int64
	now       int32
	commandID string
	request   json.RawMessage
}

type npcRandomSpawnTickRequest struct {
	Kind     string   `json:"kind"`
	Slot     int64    `json:"slot"`
	Now      int32    `json:"now"`
	PlyOrder []string `json:"ply_order"`
}

type npcRandomSpawnPhaseSummary struct {
	Now           int32    `json:"now"`
	SpawnedRooms  []int16  `json:"spawned_rooms,omitempty"`
	SpawnedNPCIDs []string `json:"spawned_npc_ids,omitempty"`
}

func npcRandomSpawnIntervalSeconds(interval time.Duration) (int64, error) {
	if interval <= 0 || interval%time.Second != 0 {
		return 0, errors.New("NPC random spawn interval must be a positive whole number of seconds")
	}
	seconds := int64(interval / time.Second)
	if seconds < 1 {
		return 0, errors.New("NPC random spawn interval must be at least one second")
	}
	return seconds, nil
}

func npcRandomSpawnCommandID(slot int64) string {
	return fmt.Sprintf("npc-random-spawn-%d", slot)
}

func marshalNPCRandomSpawnRequest(slot int64, now int32, plyOrder []string) (json.RawMessage, error) {
	if slot < 0 || now < 0 {
		return nil, errors.New("invalid NPC random spawn slot or time")
	}
	request, err := json.Marshal(npcRandomSpawnTickRequest{
		Kind:     "npc-random-spawn-phase",
		Slot:     slot,
		Now:      now,
		PlyOrder: plyOrder,
	})
	if err != nil {
		return nil, err
	}
	return request, nil
}

func decodeNPCRandomSpawnRequest(raw json.RawMessage) (npcRandomSpawnTickRequest, error) {
	var request npcRandomSpawnTickRequest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return npcRandomSpawnTickRequest{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return npcRandomSpawnTickRequest{}, errors.New("trailing NPC random spawn request data")
		}
		return npcRandomSpawnTickRequest{}, err
	}
	if request.Kind != "npc-random-spawn-phase" || request.Slot < 0 || request.Now < 0 {
		return npcRandomSpawnTickRequest{}, errors.New("invalid NPC random spawn request")
	}
	return request, nil
}

// RunNPCRandomSpawnTick executes one deterministic random-NPC producer slot.
// The optional player order is an immutable snapshot of the caller's C Ply
// descriptor order. A single occupied room may omit it; world.PlanNPCRandomProducer
// rejects an omitted order when multiple occupied rooms would make traversal
// order ambiguous. A retry ignores later order arguments and reuses its exact
// pending request.
func (g *WorldConnector) RunNPCRandomSpawnTick(ctx context.Context, interval time.Duration, orders ...[]string) (storage.WorldReceipt, bool, error) {
	if g == nil {
		return storage.WorldReceipt{}, false, errors.New("nil NPC random spawn connector")
	}
	if ctx == nil {
		return storage.WorldReceipt{}, false, errors.New("nil NPC random spawn tick context")
	}
	seconds, err := npcRandomSpawnIntervalSeconds(interval)
	if err != nil {
		return storage.WorldReceipt{}, false, err
	}
	if len(orders) > 1 {
		return storage.WorldReceipt{}, false, errors.New("NPC random spawn accepts at most one Ply order")
	}
	var requestedOrder []string
	if len(orders) == 1 && orders[0] != nil {
		requestedOrder = make([]string, len(orders[0]))
		copy(requestedOrder, orders[0])
	}

	g.tickMu.Lock()
	defer g.tickMu.Unlock()
	if err := ctx.Err(); err != nil {
		return storage.WorldReceipt{}, false, err
	}
	if g.config.Clock == nil {
		return storage.WorldReceipt{}, false, errors.New("NPC random spawn connector clock is unavailable")
	}

	pending := g.pendingNPCRandomSpawn
	if pending == nil {
		now, _ := g.config.Clock()
		if now < 0 {
			return storage.WorldReceipt{}, false, errors.New("NPC random spawn clock returned a negative timestamp")
		}
		slot := int64(now) / seconds
		if slot <= g.lastNPCRandomSpawnSlot {
			return storage.WorldReceipt{}, false, nil
		}
		slotNow := slot * seconds
		if slotNow < 0 || slotNow > 2147483647 {
			return storage.WorldReceipt{}, false, errors.New("NPC random spawn slot timestamp overflow")
		}
		request, err := marshalNPCRandomSpawnRequest(slot, int32(slotNow), requestedOrder)
		if err != nil {
			return storage.WorldReceipt{}, false, err
		}
		pending = &npcRandomSpawnTick{
			slot: slot, now: int32(slotNow), commandID: npcRandomSpawnCommandID(slot),
			request: append(json.RawMessage(nil), request...),
		}
		g.pendingNPCRandomSpawn = pending
	}
	if len(pending.request) == 0 {
		return storage.WorldReceipt{}, true, errors.New("NPC random spawn pending request is unavailable")
	}

	receipt, err := g.runNPCRandomSpawnPhaseAt(ctx, pending.commandID, pending.request)
	if err != nil {
		// A commit error can have an unknown outcome. Keep the exact pending
		// request until Execute confirms a receipt on a later retry.
		return storage.WorldReceipt{}, true, err
	}
	g.lastNPCRandomSpawnSlot = pending.slot
	g.pendingNPCRandomSpawn = nil
	return receipt, true, nil
}

func (g *WorldConnector) runNPCRandomSpawnPhaseAt(ctx context.Context, commandID string, request json.RawMessage) (storage.WorldReceipt, error) {
	if g == nil {
		return storage.WorldReceipt{}, errors.New("nil NPC random spawn connector")
	}
	if ctx == nil {
		return storage.WorldReceipt{}, errors.New("nil NPC random spawn phase context")
	}
	if commandID == "" {
		return storage.WorldReceipt{}, errors.New("missing NPC random spawn phase command ID")
	}
	parsed, err := decodeNPCRandomSpawnRequest(request)
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
		proposal, err := state.PlanNPCRandomProducer(world.NPCRandomProducerInput{
			Now:      parsed.Now,
			Catalog:  g.config.Catalog,
			Roll:     g.config.Roll,
			Allocate: g.config.Allocate,
			PlyOrder: parsed.PlyOrder,
		})
		if err != nil {
			return nil, nil, err
		}
		next, err := state.ApplyNPCRandomProducer(proposal)
		if err != nil {
			return nil, nil, err
		}
		summary := npcRandomSpawnPhaseSummary{Now: parsed.Now}
		for _, decision := range proposal.Rooms {
			if len(decision.Spawns) == 0 {
				continue
			}
			summary.SpawnedRooms = append(summary.SpawnedRooms, decision.RoomID)
			for _, spawn := range decision.Spawns {
				summary.SpawnedNPCIDs = append(summary.SpawnedNPCIDs, spawn.ID)
			}
		}
		saved, err := json.Marshal(next)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(summary)
		return saved, response, err
	})
}
