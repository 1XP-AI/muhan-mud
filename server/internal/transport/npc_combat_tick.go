package transport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

// npcCombatTick is the exact request retained while a durable combat command
// has an unknown outcome.  A retry must use both the slot boundary and the
// command ID from this value; sampling the wall clock again could create a
// second attack for the same cadence.
type npcCombatTick struct {
	slot      int64
	now       int32
	commandID string
}

// WorldConnector cannot grow fields in this compatibility slice: the
// connector definition is shared by the already-landed player/resource tick
// workers.  Keep combat-only scheduler state out of the persisted world and
// serialize access with WorldConnector.tickMu.  A fresh connector intentionally
// starts with lastSlot=-1; the durable command ID then resolves to a receipt
// replay after a process restart rather than executing the round again.
type npcCombatTickState struct {
	lastSlot int64
	pending  *npcCombatTick
}

var npcCombatTickStates sync.Map // map[*WorldConnector]*npcCombatTickState

// NPCCombatTickAttack is the durable projection of one non-lethal NPC swing.
// Its order in NPCCombatTickSummary.Attacks is the source-backed action order.
type NPCCombatTickAttack struct {
	NPCID    string `json:"npc_id"`
	PlayerID string `json:"player_id"`
	RoomID   int16  `json:"room_id"`
	Hit      bool   `json:"hit"`
	Critical bool   `json:"critical"`
	Damage   int    `json:"damage"`
	PlayerHP int    `json:"player_hp"`
}

// NPCCombatTickSkip records a canonical active NPC for which C would not
// reach attack_crt in this pass (for example, an empty room or a first enemy
// that is no longer a member of that room).  It is kept in the receipt so a
// replay does not need to rediscover a different target.
type NPCCombatTickSkip struct {
	NPCID  string `json:"npc_id"`
	RoomID int16  `json:"room_id"`
	Reason string `json:"reason"`
}

// NPCCombatTickFailClosed records a lethal player result.  The existing
// PlanNPCCombatRound/ApplyNPCCombatRound reducer deliberately rejects player
// death because the player-death continuation is not yet composed here.  The
// tick therefore commits no damage for that swing and records the boundary in
// its durable summary instead of manufacturing a successful lethal result.
type NPCCombatTickFailClosed struct {
	NPCID    string `json:"npc_id"`
	PlayerID string `json:"player_id"`
	RoomID   int16  `json:"room_id"`
	Reason   string `json:"reason"`
}

// NPCCombatTickSummary is stored in the command receipt.  Every slice is
// appended in deterministic source order; no map iteration contributes to
// the response.
type NPCCombatTickSummary struct {
	Slot       int64                     `json:"slot"`
	Now        int32                     `json:"now"`
	Attacks    []NPCCombatTickAttack     `json:"attacks,omitempty"`
	Skipped    []NPCCombatTickSkip       `json:"skipped,omitempty"`
	FailClosed []NPCCombatTickFailClosed `json:"fail_closed,omitempty"`
}

type npcCombatTickRequest struct {
	Kind string `json:"kind"`
	Slot int64  `json:"slot"`
	Now  int32  `json:"now"`
}

func npcCombatIntervalSeconds(interval time.Duration) (int64, error) {
	if interval <= 0 || interval%time.Second != 0 {
		return 0, errors.New("NPC combat interval must be a positive whole number of seconds")
	}
	seconds := int64(interval / time.Second)
	if seconds < 1 {
		return 0, errors.New("NPC combat interval must be at least one second")
	}
	return seconds, nil
}

func npcCombatCommandID(slot int64) string {
	return fmt.Sprintf("npc-combat-%d", slot)
}

func npcCombatTickStateFor(g *WorldConnector) *npcCombatTickState {
	value, _ := npcCombatTickStates.LoadOrStore(g, &npcCombatTickState{lastSlot: -1})
	return value.(*npcCombatTickState)
}

// RunNPCCombatTick executes one deterministic NPC update_active combat slot.
// The bool is false when this connector already completed the current slot.
// On an uncertain durable write it is true and the exact pending request is
// retained for the next call.
func (g *WorldConnector) RunNPCCombatTick(ctx context.Context, interval time.Duration) (storage.WorldReceipt, bool, error) {
	if g == nil {
		return storage.WorldReceipt{}, false, errors.New("nil NPC combat connector")
	}
	if ctx == nil {
		return storage.WorldReceipt{}, false, errors.New("nil NPC combat tick context")
	}
	seconds, err := npcCombatIntervalSeconds(interval)
	if err != nil {
		return storage.WorldReceipt{}, false, err
	}

	g.tickMu.Lock()
	defer g.tickMu.Unlock()
	if err := ctx.Err(); err != nil {
		return storage.WorldReceipt{}, false, err
	}
	if g.config.Clock == nil {
		return storage.WorldReceipt{}, false, errors.New("NPC combat connector clock is unavailable")
	}

	state := npcCombatTickStateFor(g)
	now, _ := g.config.Clock()
	pending := state.pending
	if pending == nil {
		slot := int64(now) / seconds
		if slot <= state.lastSlot {
			return storage.WorldReceipt{}, false, nil
		}
		slotNow := slot * seconds
		if slotNow < -2147483648 || slotNow > 2147483647 {
			return storage.WorldReceipt{}, false, errors.New("NPC combat slot timestamp overflow")
		}
		pending = &npcCombatTick{slot: slot, now: int32(slotNow), commandID: npcCombatCommandID(slot)}
		state.pending = pending
	}

	receipt, err := g.runNPCCombatPhaseAt(ctx, pending.commandID, pending.slot, pending.now)
	if err != nil {
		// Do not release pending: a commit error may have an unknown outcome,
		// and the caller must retry the identical command/request.
		return storage.WorldReceipt{}, true, err
	}
	state.lastSlot = pending.slot
	state.pending = nil
	return receipt, true, nil
}

// RunNPCCombatScheduler owns the combat phase on a wall-clock cadence.  It is
// intentionally not wired into cmd/muhan here; callers must opt into this
// phase only after the canonical NPC graph and active order are admitted.
func (g *WorldConnector) RunNPCCombatScheduler(ctx context.Context, interval time.Duration) error {
	if g == nil {
		return errors.New("nil NPC combat scheduler connector")
	}
	if ctx == nil {
		return errors.New("nil NPC combat scheduler context")
	}
	if _, err := npcCombatIntervalSeconds(interval); err != nil {
		return err
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		_, _, err := g.RunNPCCombatTick(ctx, interval)
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

// RunNPCCombatPhase runs a caller-bound command directly.  It is useful to a
// host that owns its own cadence, while RunNPCCombatTick supplies the stable
// slot/command binding for the normal retry path.
func (g *WorldConnector) RunNPCCombatPhase(ctx context.Context, commandID string, slot int64, now int32) (storage.WorldReceipt, error) {
	if g == nil {
		return storage.WorldReceipt{}, errors.New("nil NPC combat connector")
	}
	return g.runNPCCombatPhaseAt(ctx, commandID, slot, now)
}

func (g *WorldConnector) runNPCCombatPhaseAt(ctx context.Context, commandID string, slot int64, now int32) (storage.WorldReceipt, error) {
	if g == nil {
		return storage.WorldReceipt{}, errors.New("nil NPC combat connector")
	}
	if ctx == nil {
		return storage.WorldReceipt{}, errors.New("nil NPC combat phase context")
	}
	if commandID == "" {
		return storage.WorldReceipt{}, errors.New("missing NPC combat phase command ID")
	}
	request, err := json.Marshal(npcCombatTickRequest{Kind: "npc-combat-phase", Slot: slot, Now: now})
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
		next, summary, err := PlanNPCCombatTick(state, slot, now, g.config.Roll)
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

// PlanNPCCombatTick composes the source-backed one-round reducer for every
// active canonical NPC in one atomic candidate.  The order is deliberate:
// src/update.c:update_active walks C's first_active list, represented by
// State.ActiveNPCIDs; each NPC's room membership is validated against the
// ordered RoomState.NPCIDs; and the first enemy entry is resolved against the
// ordered room PlayerIDs, matching first_enm/find_crt.  Map iteration and
// display-name matching are never used to choose an attacker or target.
//
// src/command5.c:attack_crt clears PHIDDN/PINVIS before rolling the hit and
// damage.  PlanNPCCombatRound owns that exact per-swing transition and the
// C update_active/attack_crt RNG order; this function only composes rounds and
// commits their candidates through one durable command receipt.
func PlanNPCCombatTick(state world.State, slot int64, now int32, roll func(int, int) int) (world.State, NPCCombatTickSummary, error) {
	if err := state.Validate(); err != nil {
		return world.State{}, NPCCombatTickSummary{}, err
	}
	if state.NPCs == nil {
		return world.State{}, NPCCombatTickSummary{}, errors.New("NPC combat requires canonical NPC state")
	}
	if state.ActiveNPCIDs == nil {
		return world.State{}, NPCCombatTickSummary{}, errors.New("NPC combat active order unresolved")
	}
	if roll == nil {
		return world.State{}, NPCCombatTickSummary{}, errors.New("NPC combat requires RNG")
	}
	if slot < 0 {
		return world.State{}, NPCCombatTickSummary{}, errors.New("NPC combat slot must be non-negative")
	}

	next := state
	summary := NPCCombatTickSummary{Slot: slot, Now: now}
	for _, npcID := range state.ActiveNPCIDs {
		npc, ok := next.NPCs[npcID]
		if !ok || npcID == "" {
			return world.State{}, NPCCombatTickSummary{}, fmt.Errorf("unknown active NPC %q", npcID)
		}
		room, ok := next.Rooms[npc.Body.RoomID]
		if !ok || !containsNPCCombatID(room.NPCIDs, npcID) {
			return world.State{}, NPCCombatTickSummary{}, fmt.Errorf("active NPC %q room membership absent", npcID)
		}

		playerID, found, err := npcCombatTarget(next, npcID)
		if err != nil {
			return world.State{}, NPCCombatTickSummary{}, err
		}
		if !found {
			summary.Skipped = append(summary.Skipped, NPCCombatTickSkip{NPCID: npcID, RoomID: npc.Body.RoomID, Reason: "no current first enemy target"})
			continue
		}

		proposal, err := next.PlanNPCCombatRound(npcID, playerID, roll)
		if err != nil {
			// PlanNPCCombatRound intentionally consumes no state on a lethal
			// result.  Preserve that fail-closed boundary in the receipt while
			// allowing other independent NPCs in this slot to be considered.
			if isNPCCombatLethalBoundary(err) {
				summary.FailClosed = append(summary.FailClosed, NPCCombatTickFailClosed{
					NPCID: npcID, PlayerID: playerID, RoomID: npc.Body.RoomID,
					Reason: err.Error(),
				})
				continue
			}
			return world.State{}, NPCCombatTickSummary{}, fmt.Errorf("NPC %q combat plan: %w", npcID, err)
		}
		candidate, result, err := next.ApplyNPCCombatRound(proposal)
		if err != nil {
			return world.State{}, NPCCombatTickSummary{}, fmt.Errorf("NPC %q combat apply: %w", npcID, err)
		}
		next = candidate
		summary.Attacks = append(summary.Attacks, NPCCombatTickAttack{
			NPCID: result.NPCID, PlayerID: result.PlayerID, RoomID: result.RoomID,
			Hit: result.Hit, Critical: result.Critical, Damage: result.Damage,
			PlayerHP: result.PlayerHP,
		})
	}
	if err := next.Validate(); err != nil {
		return world.State{}, NPCCombatTickSummary{}, err
	}
	return next, summary, nil
}

// planNPCCombatPhase is kept as a package-local name for transport tests and
// callers that follow the existing resource-phase naming convention.
func planNPCCombatPhase(state world.State, slot int64, now int32, roll func(int, int) int) (world.State, NPCCombatTickSummary, error) {
	return PlanNPCCombatTick(state, slot, now, roll)
}

func containsNPCCombatID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// npcCombatTarget mirrors update_active's first_enm + find_crt pair.  A
// missing first enemy is a peaceful/no-op pass; a nil relation list is
// unresolved migration state and is rejected rather than guessed as peace.
func npcCombatTarget(state world.State, npcID string) (string, bool, error) {
	npc, ok := state.NPCs[npcID]
	if !ok {
		return "", false, fmt.Errorf("unknown NPC combat identity %q", npcID)
	}
	room, ok := state.Rooms[npc.Body.RoomID]
	if !ok {
		return "", false, fmt.Errorf("NPC combat room absent for %q", npcID)
	}
	if npc.Enemies == nil {
		return "", false, fmt.Errorf("NPC combat enemy relations unresolved for %q", npcID)
	}
	if len(npc.Enemies) == 0 {
		return "", false, nil
	}
	first := npc.Enemies[0]
	if first.Damage < 0 {
		return "", false, fmt.Errorf("NPC combat enemy relation unresolved for %q", npcID)
	}
	if first.Target.Kind != "player" {
		// update_active searches the room's first_ply list for the first
		// enemy name.  NPC-to-NPC relations do not produce a player attack in
		// this reducer and remain untouched for their own future phase.
		return "", false, nil
	}
	for _, playerID := range room.PlayerIDs {
		if playerID != first.Target.ID {
			continue
		}
		player, exists := state.Players[playerID]
		if !exists || !player.Online || player.Body.RoomID != npc.Body.RoomID {
			return "", false, nil
		}
		return playerID, true, nil
	}
	return "", false, nil
}

func isNPCCombatLethalBoundary(err error) bool {
	return err != nil && strings.Contains(err.Error(), "player death continuation pending")
}
