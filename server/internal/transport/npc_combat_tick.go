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

// NPCCombatTickAttack is the durable projection of one NPC swing.  A lethal
// swing is followed by NPCCombatTickDeath in the same receipt; its order in
// NPCCombatTickSummary.Attacks is still the source-backed action order.
type NPCCombatTickAttack struct {
	NPCID                  string                    `json:"npc_id"`
	PlayerID               string                    `json:"player_id"`
	RoomID                 int16                     `json:"room_id"`
	Hit                    bool                      `json:"hit"`
	Critical               bool                      `json:"critical"`
	Poisoned               bool                      `json:"poisoned"`
	Diseased               bool                      `json:"diseased"`
	Blinded                bool                      `json:"blinded"`
	BreathTriggered        bool                      `json:"breath_triggered,omitempty"`
	BreathType             world.NPCCombatBreathType `json:"breath_type,omitempty"`
	BreathRoll             int                       `json:"breath_roll,omitempty"`
	BreathDiceCount        int                       `json:"breath_dice_count,omitempty"`
	BreathDiceSides        int                       `json:"breath_dice_sides,omitempty"`
	BreathDicePlus         int                       `json:"breath_dice_plus,omitempty"`
	BreathResisted         bool                      `json:"breath_resisted,omitempty"`
	BreathPoisoned         bool                      `json:"breath_poisoned,omitempty"`
	DissolveSucceeded      bool                      `json:"dissolve_succeeded,omitempty"`
	Dissolved              bool                      `json:"dissolved,omitempty"`
	DissolveProtected      bool                      `json:"dissolve_protected,omitempty"`
	DissolveRoll           int                       `json:"dissolve_roll,omitempty"`
	DissolveSelectionRoll  int                       `json:"dissolve_selection_roll,omitempty"`
	DissolveCandidateCount int                       `json:"dissolve_candidate_count,omitempty"`
	DissolveReadySlot      int                       `json:"dissolve_ready_slot,omitempty"`
	DissolveItemID         string                    `json:"dissolve_item_id,omitempty"`
	DissolveItemName       string                    `json:"dissolve_item_name,omitempty"`
	Damage                 int                       `json:"damage"`
	PlayerHP               int                       `json:"player_hp"`
	Lethal                 bool                      `json:"lethal"`
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

// NPCCombatTickFailClosed records a lethal player result when the pure
// PlanNPCCombatTick API is used without a death continuation.  The durable
// transport opts into NPCCombatTickDeath when canonical respawn/equipment
// dependencies are supplied; an unresolved continuation remains an atomic
// command error rather than a partial damage commit.
type NPCCombatTickFailClosed struct {
	NPCID    string `json:"npc_id"`
	PlayerID string `json:"player_id"`
	RoomID   int16  `json:"room_id"`
	Reason   string `json:"reason"`
}

// NPCCombatTickDeath is the durable projection of one successful
// NPC->PLAYER death continuation.  The full equipment graph, XP, room
// membership, enemy cleanup, and room-1008 admission live in the committed
// State; these fields make the same facts available without replaying that
// graph or re-rendering output.
type NPCCombatTickDeath struct {
	NPCID                    string                 `json:"npc_id"`
	PlayerID                 string                 `json:"player_id"`
	SourceRoomID             int16                  `json:"source_room_id"`
	DestinationRoomID        int16                  `json:"destination_room_id"`
	BroadcastDeath           bool                   `json:"broadcast_death"`
	FamilyDefeated           bool                   `json:"family_defeated"`
	DeactivateSourceMonsters bool                   `json:"deactivate_source_monsters"`
	EnemyRemoved             bool                   `json:"enemy_removed"`
	ExperienceBefore         int32                  `json:"experience_before"`
	ExperienceAfter          int32                  `json:"experience_after"`
	DroppedItemCount         int                    `json:"dropped_item_count"`
	Scene                    string                 `json:"scene,omitempty"`
	Events                   []world.FamilyWarEvent `json:"events,omitempty"`
}

// NPCCombatTickSummary is stored in the command receipt.  Every slice is
// appended in deterministic source order; no map iteration contributes to
// the response.
type NPCCombatTickSummary struct {
	Slot              int64                     `json:"slot"`
	Now               int32                     `json:"now"`
	Attacks           []NPCCombatTickAttack     `json:"attacks,omitempty"`
	Skipped           []NPCCombatTickSkip       `json:"skipped,omitempty"`
	FailClosed        []NPCCombatTickFailClosed `json:"fail_closed,omitempty"`
	Deaths            []NPCCombatTickDeath      `json:"deaths,omitempty"`
	StoppedAfterDeath bool                      `json:"stopped_after_death,omitempty"`
}

// NPCCombatTickOptions controls the optional player-death continuation.  The
// zero value preserves the original pure combat-round API and records lethal
// damage as fail-closed.  RunNPCCombatPhase opts in with the connector's
// canonical catalog/allocator so the entire attack+death candidate is saved
// by one receipt.
type NPCCombatTickOptions struct {
	ContinuePlayerDeath bool
	View                world.SceneOptions
	Catalog             world.SpawnCatalog
	FamilyCatalog       world.FamilyCatalog
	Allocate            func() (string, error)
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
	if g.config.Clock == nil {
		return storage.WorldReceipt{}, errors.New("NPC combat connector clock is unavailable")
	}
	request, err := json.Marshal(npcCombatTickRequest{Kind: "npc-combat-phase", Slot: slot, Now: now})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	g.commandMu.Lock()
	receipt, err := engine.Execute(ctx, g.config.Store, g.config.WorldID, commandID, request, func(raw json.RawMessage) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		_, hour := g.config.Clock()
		next, summary, err := PlanNPCCombatTickWithOptions(state, slot, now, g.config.Roll, NPCCombatTickOptions{
			ContinuePlayerDeath: true,
			View:                world.SceneOptions{ViewOptions: world.ViewOptions{Hour: hour}},
			Catalog:             g.config.Catalog,
			FamilyCatalog:       g.config.FamilyCatalog,
			Allocate:            g.config.Allocate,
		})
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
	g.commandMu.Unlock()
	if err != nil {
		return receipt, err
	}
	if !receipt.Replayed {
		var summary NPCCombatTickSummary
		if decodeErr := json.Unmarshal(receipt.Response, &summary); decodeErr == nil {
			if after, ok := g.snapshot(ctx); ok {
				g.publishFamilyDefeat(after, familyDefeatEventsFromDeaths(summary.Deaths))
			}
		}
	}
	return receipt, nil
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
	return PlanNPCCombatTickWithOptions(state, slot, now, roll, NPCCombatTickOptions{})
}

// PlanNPCCombatTickWithOptions is the composable form used by the durable
// transport.  A lethal round is first planned against a high-HP probe using
// the exact recorded RNG values from the real victim attempt; its actual
// damage is then restored before PlanNPCPlayerDeath runs.  Thus the attack
// RNG is consumed exactly once, while the death continuation receives a
// canonical victim with HP below one.
func PlanNPCCombatTickWithOptions(state world.State, slot int64, now int32, roll func(int, int) int, options NPCCombatTickOptions) (world.State, NPCCombatTickSummary, error) {
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

		candidate, attack, lethal, err := planNPCCombatRoundForTick(next, npcID, playerID, roll)
		if err != nil {
			if lethal && !options.ContinuePlayerDeath {
				summary.FailClosed = append(summary.FailClosed, NPCCombatTickFailClosed{
					NPCID: npcID, PlayerID: playerID, RoomID: npc.Body.RoomID,
					Reason: err.Error(),
				})
				summary.StoppedAfterDeath = true
				break
			}
			return world.State{}, NPCCombatTickSummary{}, fmt.Errorf("NPC %q combat plan: %w", npcID, err)
		}
		if !lethal {
			next = candidate
			summary.Attacks = append(summary.Attacks, attack)
			continue
		}

		if !options.ContinuePlayerDeath {
			summary.FailClosed = append(summary.FailClosed, NPCCombatTickFailClosed{
				NPCID: npcID, PlayerID: playerID, RoomID: npc.Body.RoomID,
				Reason: "NPC combat player death continuation pending",
			})
			summary.StoppedAfterDeath = true
			break
		}
		deathState, death, err := planNPCCombatPlayerDeath(next, candidate, npcID, playerID, now, roll, options)
		if err != nil {
			// A death continuation failure (respawn floor, allocator, equipment,
			// or unresolved identity) is an atomic command error, not a reason
			// to continue attacking the next NPC with a partially-dead player.
			return world.State{}, NPCCombatTickSummary{}, fmt.Errorf("NPC %q player death continuation: %w", npcID, err)
		}
		next = deathState
		attack.Lethal = true
		summary.Attacks = append(summary.Attacks, attack)
		summary.Deaths = append(summary.Deaths, death)
		// update_active resets cp to first_active after die().  This command
		// stops here instead of guessing whether a restarted traversal should
		// attack another NPC in the same durable slot.
		summary.StoppedAfterDeath = true
		break
	}
	if err := next.Validate(); err != nil {
		return world.State{}, NPCCombatTickSummary{}, err
	}
	return next, summary, nil
}

type npcCombatRollCall struct {
	low, high int
	value     int
}

// npcCombatRecordingRoll binds every attack random result to the current
// command.  PlanNPCCombatRound returns no proposal for lethal damage, so the
// recorded values are replayed into a high-HP probe instead of calling the
// caller's RNG a second time and changing the attack sequence.
type npcCombatRecordingRoll struct {
	source func(int, int) int
	calls  []npcCombatRollCall
}

func (r *npcCombatRecordingRoll) Roll(low, high int) int {
	value := r.source(low, high)
	r.calls = append(r.calls, npcCombatRollCall{low: low, high: high, value: value})
	return value
}

type npcCombatReplayRoll struct {
	calls []npcCombatRollCall
	index int
	err   error
}

func (r *npcCombatReplayRoll) Roll(low, high int) int {
	if r.index >= len(r.calls) {
		r.err = fmt.Errorf("NPC combat RNG replay requested %d..%d after recorded sequence", low, high)
		return low - 1
	}
	call := r.calls[r.index]
	r.index++
	if call.low != low || call.high != high {
		r.err = fmt.Errorf("NPC combat RNG replay range changed from %d..%d to %d..%d", call.low, call.high, low, high)
		return low - 1
	}
	return call.value
}

func cloneNPCCombatState(state world.State) (world.State, error) {
	raw, err := json.Marshal(state)
	if err != nil {
		return world.State{}, err
	}
	return world.DecodeState(raw)
}

// planNPCCombatRoundForTick preserves the existing non-lethal path exactly.
// On the one lethal error emitted by PlanNPCCombatRound, it replays the same
// random values against a temporary 32767-HP victim to recover the source
// damage and candidate flags, then restores the real victim's post-hit HP.
// The returned candidate is still uncommitted and is safe to hand to the
// player-death planner.
func planNPCCombatRoundForTick(state world.State, npcID, playerID string, roll func(int, int) int) (world.State, NPCCombatTickAttack, bool, error) {
	recorder := &npcCombatRecordingRoll{source: roll}
	proposal, err := state.PlanNPCCombatRound(npcID, playerID, recorder.Roll)
	if err == nil {
		candidate, result, applyErr := state.ApplyNPCCombatRound(proposal)
		if applyErr != nil {
			return world.State{}, NPCCombatTickAttack{}, false, fmt.Errorf("NPC %q combat apply: %w", npcID, applyErr)
		}
		return candidate, NPCCombatTickAttack{
			NPCID: result.NPCID, PlayerID: result.PlayerID, RoomID: result.RoomID,
			Hit: result.Hit, Critical: result.Critical, Poisoned: result.Poisoned,
			Diseased: result.Diseased, Blinded: result.Blinded,
			BreathTriggered: result.BreathTriggered, BreathType: result.BreathType,
			BreathRoll: result.BreathRoll, BreathDiceCount: result.BreathDiceCount,
			BreathDiceSides: result.BreathDiceSides, BreathDicePlus: result.BreathDicePlus,
			BreathResisted: result.BreathResisted, BreathPoisoned: result.BreathPoisoned,
			DissolveSucceeded: result.DissolveSucceeded, Dissolved: result.Dissolved,
			DissolveProtected: result.DissolveProtected, DissolveRoll: result.DissolveRoll,
			DissolveSelectionRoll:  result.DissolveSelectionRoll,
			DissolveCandidateCount: result.DissolveCandidateCount,
			DissolveReadySlot:      result.DissolveReadySlot, DissolveItemID: result.DissolveItemID,
			DissolveItemName: result.DissolveItemName, Damage: result.Damage, PlayerHP: result.PlayerHP,
		}, false, nil
	}
	if !isNPCCombatLethalBoundary(err) {
		return world.State{}, NPCCombatTickAttack{}, false, err
	}

	player, ok := state.Players[playerID]
	if !ok || player.Body.HPCurrent < 1 {
		return world.State{}, NPCCombatTickAttack{}, true, fmt.Errorf("lethal NPC combat victim is not alive")
	}
	probe, cloneErr := cloneNPCCombatState(state)
	if cloneErr != nil {
		return world.State{}, NPCCombatTickAttack{}, true, fmt.Errorf("lethal NPC combat probe clone: %w", cloneErr)
	}
	probePlayer := probe.Players[playerID]
	probePlayer.Body.HPCurrent = 32767
	probe.Players[playerID] = probePlayer
	replay := &npcCombatReplayRoll{calls: recorder.calls}
	probeProposal, probeErr := probe.PlanNPCCombatRound(npcID, playerID, replay.Roll)
	if probeErr != nil {
		return world.State{}, NPCCombatTickAttack{}, true, fmt.Errorf("lethal NPC combat probe: %w", probeErr)
	}
	if replay.err != nil || replay.index != len(recorder.calls) {
		if replay.err != nil {
			return world.State{}, NPCCombatTickAttack{}, true, replay.err
		}
		return world.State{}, NPCCombatTickAttack{}, true, errors.New("NPC combat RNG replay left unused attack values")
	}
	probeCandidate, probeResult, applyErr := probe.ApplyNPCCombatRound(probeProposal)
	if applyErr != nil {
		return world.State{}, NPCCombatTickAttack{}, true, fmt.Errorf("lethal NPC combat probe apply: %w", applyErr)
	}
	if !probeResult.Hit || probeResult.Damage < int(player.Body.HPCurrent) {
		return world.State{}, NPCCombatTickAttack{}, true, errors.New("NPC combat lethal probe did not reproduce lethal damage")
	}
	actualAfter := int(player.Body.HPCurrent) - probeResult.Damage
	if actualAfter >= 1 || actualAfter < -32768 {
		return world.State{}, NPCCombatTickAttack{}, true, errors.New("NPC combat lethal damage outside player range")
	}
	probePlayer = probeCandidate.Players[playerID]
	probePlayer.Body.HPCurrent = int16(actualAfter)
	probeCandidate.Players[playerID] = probePlayer
	if err := probeCandidate.Validate(); err != nil {
		return world.State{}, NPCCombatTickAttack{}, true, fmt.Errorf("lethal NPC combat candidate: %w", err)
	}
	return probeCandidate, NPCCombatTickAttack{
		NPCID: probeResult.NPCID, PlayerID: probeResult.PlayerID, RoomID: probeResult.RoomID,
		Hit: probeResult.Hit, Critical: probeResult.Critical, Poisoned: probeResult.Poisoned,
		Diseased: probeResult.Diseased, Blinded: probeResult.Blinded,
		BreathTriggered: probeResult.BreathTriggered, BreathType: probeResult.BreathType,
		BreathRoll: probeResult.BreathRoll, BreathDiceCount: probeResult.BreathDiceCount,
		BreathDiceSides: probeResult.BreathDiceSides, BreathDicePlus: probeResult.BreathDicePlus,
		BreathResisted: probeResult.BreathResisted, BreathPoisoned: probeResult.BreathPoisoned,
		DissolveSucceeded: probeResult.DissolveSucceeded, Dissolved: probeResult.Dissolved,
		DissolveProtected: probeResult.DissolveProtected, DissolveRoll: probeResult.DissolveRoll,
		DissolveSelectionRoll:  probeResult.DissolveSelectionRoll,
		DissolveCandidateCount: probeResult.DissolveCandidateCount,
		DissolveReadySlot:      probeResult.DissolveReadySlot, DissolveItemID: probeResult.DissolveItemID,
		DissolveItemName: probeResult.DissolveItemName, Damage: probeResult.Damage,
		PlayerHP: actualAfter, Lethal: true,
	}, true, nil
}

func itemCount(items *world.ItemCollection) int {
	if items == nil {
		return 0
	}
	return len(items.Items)
}

func npcCombatEnemyRemoved(before, after world.NPCState, playerID string) bool {
	want := world.EntityRef{Kind: "player", ID: playerID}
	beforeCount, afterCount := 0, 0
	for _, relation := range before.Enemies {
		if relation.Target == want {
			beforeCount++
		}
	}
	for _, relation := range after.Enemies {
		if relation.Target == want {
			afterCount++
		}
	}
	return beforeCount > 0 && afterCount < beforeCount
}

func planNPCCombatPlayerDeath(state, lethalCandidate world.State, npcID, playerID string, now int32, roll func(int, int) int, options NPCCombatTickOptions) (world.State, NPCCombatTickDeath, error) {
	beforePlayer, ok := state.Players[playerID]
	if !ok {
		return world.State{}, NPCCombatTickDeath{}, errors.New("NPC combat death victim absent before attack")
	}
	beforeNPC, ok := state.NPCs[npcID]
	if !ok {
		return world.State{}, NPCCombatTickDeath{}, errors.New("NPC combat death attacker absent before attack")
	}
	roomID := beforeNPC.Body.RoomID
	next, result, err := lethalCandidate.PlanNPCPlayerDeath(npcID, playerID, now, options.View, options.Catalog, roll, options.Allocate, options.FamilyCatalog)
	if err != nil {
		return world.State{}, NPCCombatTickDeath{}, err
	}
	afterPlayer, ok := next.Players[playerID]
	if !ok {
		return world.State{}, NPCCombatTickDeath{}, errors.New("NPC combat death victim absent after continuation")
	}
	afterNPC, ok := next.NPCs[npcID]
	if !ok {
		return world.State{}, NPCCombatTickDeath{}, errors.New("NPC combat death attacker absent after continuation")
	}
	// Count the committed source-floor transfer.  The lethal candidate may
	// already have dissolved an item subtree before the death continuation.
	beforeFloor, ok := lethalCandidate.Rooms[roomID]
	if !ok || beforeFloor.Items == nil {
		return world.State{}, NPCCombatTickDeath{}, errors.New("NPC combat death source floor absent before continuation")
	}
	afterFloor, ok := next.Rooms[roomID]
	if !ok || afterFloor.Items == nil {
		return world.State{}, NPCCombatTickDeath{}, errors.New("NPC combat death source floor absent after continuation")
	}
	return next, NPCCombatTickDeath{
		NPCID:                    result.NPCID,
		PlayerID:                 result.VictimID,
		SourceRoomID:             roomID,
		DestinationRoomID:        result.Entry.Room.ID,
		BroadcastDeath:           result.BroadcastDeath,
		FamilyDefeated:           result.FamilyDefeated,
		DeactivateSourceMonsters: result.DeactivateSourceMonsters,
		EnemyRemoved:             npcCombatEnemyRemoved(beforeNPC, afterNPC, playerID),
		ExperienceBefore:         beforePlayer.Body.Experience,
		ExperienceAfter:          afterPlayer.Body.Experience,
		DroppedItemCount:         itemCount(afterFloor.Items) - itemCount(beforeFloor.Items),
		Scene:                    result.Entry.Scene,
		Events:                   append([]world.FamilyWarEvent(nil), result.Events...),
	}, nil
}

func familyDefeatEventsFromDeaths(deaths []NPCCombatTickDeath) []world.FamilyWarEvent {
	var events []world.FamilyWarEvent
	for _, death := range deaths {
		events = append(events, death.Events...)
	}
	return events
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
