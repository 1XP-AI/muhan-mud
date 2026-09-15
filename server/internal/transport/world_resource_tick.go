package transport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

// roomResourceTick is the exact request retained while a room-resource
// command has an unknown durable outcome. The retry must reuse both the slot
// and command ID; recomputing the wall-clock sample could otherwise produce a
// second refresh for the same interval.
type roomResourceTick struct {
	slot      int64
	now       int32
	commandID string
}

type roomResourcePhaseSummary struct {
	Now        int32   `json:"now"`
	Refreshed  []int16 `json:"refreshed_rooms,omitempty"`
	NPCPending []int16 `json:"npc_pending_rooms,omitempty"`
	Unmigrated []int16 `json:"unmigrated_rooms,omitempty"`
}

func roomResourceIntervalSeconds(interval time.Duration) (int64, error) {
	if interval <= 0 || interval%time.Second != 0 {
		return 0, errors.New("room resource interval must be a positive whole number of seconds")
	}
	seconds := int64(interval / time.Second)
	if seconds < 1 {
		return 0, errors.New("room resource interval must be at least one second")
	}
	return seconds, nil
}

func roomResourceCommandID(slot int64) string {
	return fmt.Sprintf("room-resources-%d", slot)
}

// RunRoomResourceTick executes the currently admitted room-resource phase:
// canonical floor object respawn and automatic door refresh. A due permanent
// NPC is reported in the receipt and left untouched for the identity-aware NPC
// scheduler; it is never silently converted into an anonymous monster.
//
// Noncanonical rooms are explicitly listed as unmigrated and skipped. This
// lets a world containing a reviewed legacy-resource room continue serving
// while its canonical item graph is migrated, without mixing two owners.
func (g *WorldConnector) RunRoomResourceTick(ctx context.Context, interval time.Duration) (storage.WorldReceipt, bool, error) {
	if ctx == nil {
		return storage.WorldReceipt{}, false, errors.New("nil room resource tick context")
	}
	seconds, err := roomResourceIntervalSeconds(interval)
	if err != nil {
		return storage.WorldReceipt{}, false, err
	}
	g.tickMu.Lock()
	defer g.tickMu.Unlock()
	if err := ctx.Err(); err != nil {
		return storage.WorldReceipt{}, false, err
	}
	now, _ := g.config.Clock()
	pending := g.pendingRoomResource
	if pending == nil {
		slot := int64(now) / seconds
		if slot <= g.lastRoomResourceSlot {
			return storage.WorldReceipt{}, false, nil
		}
		slotNow := slot * seconds
		if slotNow < -2147483648 || slotNow > 2147483647 {
			return storage.WorldReceipt{}, false, errors.New("room resource slot timestamp overflow")
		}
		pending = &roomResourceTick{slot: slot, now: int32(slotNow), commandID: roomResourceCommandID(slot)}
		g.pendingRoomResource = pending
	}
	receipt, err := g.runRoomResourcePhaseAt(ctx, pending.commandID, pending.now)
	if err != nil {
		return storage.WorldReceipt{}, true, err
	}
	g.lastRoomResourceSlot = pending.slot
	g.pendingRoomResource = nil
	return receipt, true, nil
}

// RunRoomResourceScheduler owns the admitted room-resource phase on a
// wall-clock cadence. It deliberately does not create NPC identities; a
// transient database or writer error retains the exact pending request for
// the next cadence.
func (g *WorldConnector) RunRoomResourceScheduler(ctx context.Context, interval time.Duration) error {
	if ctx == nil {
		return errors.New("nil room resource scheduler context")
	}
	if _, err := roomResourceIntervalSeconds(interval); err != nil {
		return err
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		_, _, err := g.RunRoomResourceTick(ctx, interval)
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

func (g *WorldConnector) runRoomResourcePhaseAt(ctx context.Context, commandID string, now int32) (storage.WorldReceipt, error) {
	if commandID == "" {
		return storage.WorldReceipt{}, errors.New("missing room resource phase command ID")
	}
	request, err := json.Marshal(struct {
		Kind string `json:"kind"`
		Now  int32  `json:"now"`
	}{Kind: "room-resource-phase", Now: now})
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
		next, summary, err := planRoomResourcePhase(state, now, g.config.Catalog, g.config.Roll, g.config.Allocate)
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

func planRoomResourcePhase(state world.State, now int32, catalog world.SpawnCatalog, roll func(int, int) int, allocate func() (string, error)) (world.State, roomResourcePhaseSummary, error) {
	if err := state.Validate(); err != nil {
		return world.State{}, roomResourcePhaseSummary{}, err
	}
	next := state
	summary := roomResourcePhaseSummary{Now: now}
	roomIDs := make([]int, 0, len(next.Rooms))
	for id := range next.Rooms {
		roomIDs = append(roomIDs, int(id))
	}
	sort.Ints(roomIDs)
	for _, key := range roomIDs {
		roomID := int16(key)
		room := next.Rooms[roomID]
		if room.Items == nil || len(room.Resource.Objects) != 0 {
			summary.Unmigrated = append(summary.Unmigrated, roomID)
			continue
		}
		delta, err := next.PlanRoomResourceRefresh(roomID, catalog, now, roll, allocate)
		if errors.Is(err, world.ErrRoomResourceRefreshNPC) {
			summary.NPCPending = append(summary.NPCPending, roomID)
			continue
		}
		if err != nil {
			return world.State{}, roomResourcePhaseSummary{}, err
		}
		candidate, err := next.ApplyRoomResourceRefresh(delta)
		if err != nil {
			return world.State{}, roomResourcePhaseSummary{}, err
		}
		if !reflect.DeepEqual(candidate, next) {
			summary.Refreshed = append(summary.Refreshed, roomID)
		}
		next = candidate
	}
	return next, summary, nil
}

var _ engine.CommandStore = (*storage.Postgres)(nil)
