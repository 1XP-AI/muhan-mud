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

	"github.com/1XP-Inc/muhan-mud/server/internal/session"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

// npcScavengeTick retains the complete durable command boundary while a
// commit has an uncertain outcome. A retry reuses the exact request bytes,
// slot, timestamp, and command ID instead of sampling a new wall-clock value.
type npcScavengeTick struct {
	slot      int64
	now       int32
	commandID string
	request   json.RawMessage
}

type npcScavengeTickState struct {
	lastSlot int64
	pending  *npcScavengeTick
}

// WorldConnector is intentionally not widened by this unintegrated phase.
// Connector-local state mirrors the existing combat tick seam and keeps the
// scheduler/list ownership in its later integration PR.
var npcScavengeTickStates sync.Map // map[*WorldConnector]*npcScavengeTickState

func npcScavengeTickStateFor(g *WorldConnector) *npcScavengeTickState {
	value, _ := npcScavengeTickStates.LoadOrStore(g, &npcScavengeTickState{lastSlot: -1})
	return value.(*npcScavengeTickState)
}

type npcScavengeTickRequest struct {
	Kind string `json:"kind"`
	Slot int64  `json:"slot"`
	Now  int32  `json:"now"`
}

func npcScavengeIntervalSeconds(interval time.Duration) (int64, error) {
	if interval <= 0 || interval%time.Second != 0 {
		return 0, errors.New("NPC scavenge interval must be a positive whole number of seconds")
	}
	seconds := int64(interval / time.Second)
	if seconds < 1 {
		return 0, errors.New("NPC scavenge interval must be at least one second")
	}
	return seconds, nil
}

func npcScavengeCommandID(slot int64) string {
	return fmt.Sprintf("npc-scavenge-%d", slot)
}

func marshalNPCScavengeRequest(slot int64, now int32) (json.RawMessage, error) {
	if slot < 0 || now < 0 {
		return nil, errors.New("invalid NPC scavenge slot or time")
	}
	request, err := json.Marshal(npcScavengeTickRequest{
		Kind: "npc-scavenge-phase",
		Slot: slot,
		Now:  now,
	})
	if err != nil {
		return nil, err
	}
	return request, nil
}

func decodeNPCScavengeRequest(raw json.RawMessage) (npcScavengeTickRequest, error) {
	var request npcScavengeTickRequest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return npcScavengeTickRequest{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return npcScavengeTickRequest{}, errors.New("trailing NPC scavenge request data")
		}
		return npcScavengeTickRequest{}, err
	}
	if request.Kind != "npc-scavenge-phase" || request.Slot < 0 || request.Now < 0 {
		return npcScavengeTickRequest{}, errors.New("invalid NPC scavenge request")
	}
	return request, nil
}

// RunNPCScavengeTick executes the bounded ordered MSCAVE phase for the next
// deterministic cadence slot. It deliberately has no scheduler wrapper: a
// later integration slice owns ordering relative to maintenance/combat.
// ran is false when this connector has already completed the current slot;
// an uncertain durable attempt returns ran=true and retains its exact pending
// request for the next call.
func (g *WorldConnector) RunNPCScavengeTick(ctx context.Context, interval time.Duration) (storage.WorldReceipt, bool, error) {
	if g == nil {
		return storage.WorldReceipt{}, false, errors.New("nil NPC scavenge connector")
	}
	if ctx == nil {
		return storage.WorldReceipt{}, false, errors.New("nil NPC scavenge tick context")
	}
	seconds, err := npcScavengeIntervalSeconds(interval)
	if err != nil {
		return storage.WorldReceipt{}, false, err
	}
	g.tickMu.Lock()
	defer g.tickMu.Unlock()
	if err := ctx.Err(); err != nil {
		return storage.WorldReceipt{}, false, err
	}
	if g.config.Clock == nil {
		return storage.WorldReceipt{}, false, errors.New("NPC scavenge connector clock is unavailable")
	}
	state := npcScavengeTickStateFor(g)
	pending := state.pending
	if pending == nil {
		now, _ := g.config.Clock()
		if now < 0 {
			return storage.WorldReceipt{}, false, errors.New("NPC scavenge clock returned a negative timestamp")
		}
		slot := int64(now) / seconds
		if slot <= state.lastSlot {
			return storage.WorldReceipt{}, false, nil
		}
		slotNow := slot * seconds
		if slotNow < -2147483648 || slotNow > 2147483647 {
			return storage.WorldReceipt{}, false, errors.New("NPC scavenge slot timestamp overflow")
		}
		request, err := marshalNPCScavengeRequest(slot, int32(slotNow))
		if err != nil {
			return storage.WorldReceipt{}, false, err
		}
		pending = &npcScavengeTick{
			slot: slot, now: int32(slotNow), commandID: npcScavengeCommandID(slot),
			request: append(json.RawMessage(nil), request...),
		}
		state.pending = pending
	}
	receipt, err := g.runNPCScavengePhaseAt(ctx, pending.commandID, pending.request)
	if err != nil {
		return storage.WorldReceipt{}, true, err
	}
	state.lastSlot = pending.slot
	state.pending = nil
	return receipt, true, nil
}

// RunNPCScavengePhase executes a caller-bound phase command directly. Hosts
// that already own cadence may use it, while RunNPCScavengeTick owns the
// normal slot/pending-retry binding. It is still deliberately independent of
// the NPC world scheduler.
func (g *WorldConnector) RunNPCScavengePhase(ctx context.Context, commandID string, slot int64, now int32) (storage.WorldReceipt, error) {
	request, err := marshalNPCScavengeRequest(slot, now)
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return g.runNPCScavengePhaseAt(ctx, commandID, request)
}

func (g *WorldConnector) runNPCScavengePhaseAt(ctx context.Context, commandID string, request json.RawMessage) (storage.WorldReceipt, error) {
	if g == nil {
		return storage.WorldReceipt{}, errors.New("nil NPC scavenge connector")
	}
	if ctx == nil {
		return storage.WorldReceipt{}, errors.New("nil NPC scavenge phase context")
	}
	if commandID == "" {
		return storage.WorldReceipt{}, errors.New("missing NPC scavenge phase command ID")
	}
	parsed, err := decodeNPCScavengeRequest(request)
	if err != nil {
		return storage.WorldReceipt{}, err
	}

	g.commandMu.Lock()
	receipt, err := session.ExecuteNPCScavengeReceipt(ctx, g.config.Store, g.config.WorldID, commandID, session.NPCScavengeOptions{
		Slot: parsed.Slot,
		Now:  parsed.Now,
		Roll: g.config.Roll,
	})
	g.commandMu.Unlock()
	if err != nil {
		return receipt, err
	}
	if receipt.Replayed {
		return receipt, nil
	}

	// The durable receipt is committed before this best-effort socket
	// projection. Snapshot/fan-out failure never turns a committed command into
	// a retry, and the receipt replay guard prevents duplicate delivery.
	var summary world.NPCScavengeTickSummary
	if err := json.Unmarshal(receipt.Response, &summary); err != nil {
		return receipt, nil
	}
	after, ok := g.snapshot(ctx)
	if !ok {
		return receipt, nil
	}
	for _, event := range summary.Events {
		g.publishNPCScavenge(after, event)
	}
	return receipt, nil
}

// publishNPCScavenge delivers the single public room line from a successful
// C MSCAVE transfer to every current online player in the committed room.
// Identity, room, selected-root, and canonical text checks reject a tampered
// receipt projection without mutating world state.
func (g *WorldConnector) publishNPCScavenge(after world.State, event world.NPCScavengeEvent) {
	if g == nil || event.NPCID == "" || event.NPCName == "" || event.ItemID == "" || event.Text == "" || event.ExcludeActorID != "" {
		return
	}
	npc, ok := after.NPCs[event.NPCID]
	if !ok || npc.Body.Type != 1 || npc.Body.RoomID != event.RoomID || npc.Body.Name != event.NPCName || npc.Items == nil {
		return
	}
	room, ok := after.Rooms[event.RoomID]
	if !ok || !containsTransportNPCID(room.NPCIDs, event.NPCID) {
		return
	}
	item, ok := npc.Items.Items[event.ItemID]
	if !ok || item.Object.Name != event.ItemName || !containsTransportItemID(npc.Items.Inventory, event.ItemID) {
		return
	}
	if event.Text != world.NPCScavengeRoomText(event.NPCName, event.ItemName) {
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
	for _, connection := range connections {
		if connection.events == nil {
			continue
		}
		player, ok := after.Players[connection.lease.ActorID]
		if !ok || !player.Online || player.Body.RoomID != event.RoomID {
			continue
		}
		select {
		case connection.events <- event.Text:
		default:
		}
	}
}

func containsTransportNPCID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func containsTransportItemID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
