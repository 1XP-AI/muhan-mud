package world

import (
	"fmt"
	"reflect"
)

// These are the raw creature and player flag numbers from src/mtype.h.  The
// reducer deliberately keeps them on LegacyMonster instead of introducing a
// second, partially authoritative NPC flag model.
const (
	npcAIHiddenPlayerFlag      uint = 1  // PHIDDN
	npcAIInvisiblePlayerFlag   uint = 2  // PINVIS / MINVIS
	npcAIAggressiveFlag        uint = 6  // MAGGRE
	npcAIDMInvisiblePlayerFlag uint = 10 // PDMINV
	npcAIDetectInvisibleFlag   uint = 21 // MDINVI
	npcAIGoodAggressiveFlag    uint = 39 // MGAGGR
	npcAIEvilAggressiveFlag    uint = 40 // MEAGGR
	npcAIAttackTimerIndex           = 3  // LT_ATTCK
)

const (
	npcAINoPlayers      = "no-players"
	npcAIExistingEnemy  = "existing-enemy"
	npcAINotAggressive  = "not-aggressive"
	npcAINotReady       = "not-ready"
	npcAINoTarget       = "no-target"
	npcAIEscaped        = "escaped"
	npcAITargetAcquired = "target-acquired"
)

// NPCAggressiveTargetAction is one deterministic active-NPC decision.  The
// action list is in the exact ActiveNPCIDs order; target selection walks the
// referenced room's PlayerIDs order.  SelectionRoll and EscapeRoll are
// source-bound RNG draws, so Apply can verify and replay a decision without
// calling a random source again.
type NPCAggressiveTargetAction struct {
	NPCID             string                    `json:"npc_id"`
	RoomID            int16                     `json:"room_id"`
	Status            string                    `json:"status"`
	TargetID          string                    `json:"target_id,omitempty"`
	TargetName        string                    `json:"target_name,omitempty"`
	TotalWeight       int                       `json:"total_weight,omitempty"`
	SelectionRoll     int                       `json:"selection_roll,omitempty"`
	TargetWeight      int                       `json:"target_weight,omitempty"`
	EscapeRoll        int                       `json:"escape_roll,omitempty"`
	Escaped           bool                      `json:"escaped,omitempty"`
	TimerWrite        bool                      `json:"timer_write,omitempty"`
	AttackTimerBefore LegacyTimer               `json:"attack_timer_before"`
	AttackTimerAfter  LegacyTimer               `json:"attack_timer_after"`
	EnemyAdded        bool                      `json:"enemy_added,omitempty"`
	Event             *NPCAggressiveTargetEvent `json:"event,omitempty"`
}

// NPCAggressiveTargetEvent is the durable actor/room projection of the two
// update.c:638-645 outputs. TargetText is delivered only to TargetID; RoomText
// is delivered to the other occupants of RoomID. The identity and exclusion
// fields keep future tick composition transport-neutral and replay-safe.
type NPCAggressiveTargetEvent struct {
	RoomID          int16  `json:"room_id"`
	NPCID           string `json:"npc_id"`
	NPCName         string `json:"npc_name"`
	TargetID        string `json:"target_id"`
	TargetName      string `json:"target_name"`
	ExcludeTargetID string `json:"exclude_target_id"`
	TargetText      string `json:"target_text"`
	RoomText        string `json:"room_text"`
}

// NPCAggressiveTargetAcquisitionProposal is a complete-snapshot candidate
// for the update.c:619-647 aggressive branch.  The private before snapshot is
// the apply fence: callers can carry a decision, but cannot manufacture a
// target or partial timer/enemy update against another world revision.
type NPCAggressiveTargetAcquisitionProposal struct {
	Now     int32
	Actions []NPCAggressiveTargetAction
	Changed bool
	NoOp    bool

	before     State
	plannedNow int32
}

// NPCAggressiveTargetResult is the durable, transport-neutral receipt for a
// target-acquisition pass. Event projections carry the source-composed actor
// and room text; a later scheduler owns fan-out and socket delivery.
type NPCAggressiveTargetResult struct {
	Now     int32                       `json:"now"`
	Actions []NPCAggressiveTargetAction `json:"actions,omitempty"`
	Events  []NPCAggressiveTargetEvent  `json:"events,omitempty"`
	Changed bool                        `json:"changed"`
	NoOp    bool                        `json:"no_op,omitempty"`
}

// Descriptive aliases keep the proposal/result names usable by callers that
// refer to this bounded seam as NPC aggro rather than target acquisition.
type NPCAggressiveTargetProposal = NPCAggressiveTargetAcquisitionProposal
type NPCAggroProposal = NPCAggressiveTargetAcquisitionProposal
type NPCAggroResult = NPCAggressiveTargetResult

type npcAIAggroMode uint8

const (
	npcAIAllPiety npcAIAggroMode = iota + 1
	npcAIGoodPiety
	npcAIEvilPiety
)

// PlanNPCAggressiveTargetAcquisition ports only the deterministic decision
// seam at src/update.c:619-647.  It assumes the maintenance/timer prefix has
// already run; a successful decision writes LT_ATTCK=(now,0), records a new
// player enemy identity, and performs no transport side effect.
//
// C's list walks are preserved exactly: active NPC order is authoritative,
// each room's PlayerIDs order is authoritative, and low_piety_alg's second
// pass intentionally omits its first-pass level filter because that is the
// legacy source behavior.
func (s State) PlanNPCAggressiveTargetAcquisition(now int32, roll func(int, int) int) (NPCAggressiveTargetAcquisitionProposal, error) {
	if err := s.Validate(); err != nil {
		return NPCAggressiveTargetAcquisitionProposal{}, err
	}
	if now < 0 {
		return NPCAggressiveTargetAcquisitionProposal{}, fmt.Errorf("NPC AI time outside canonical range")
	}
	if s.NPCs == nil {
		return NPCAggressiveTargetAcquisitionProposal{}, fmt.Errorf("NPC AI requires canonical NPC state")
	}
	if s.ActiveNPCIDs == nil {
		return NPCAggressiveTargetAcquisitionProposal{}, fmt.Errorf("NPC AI active order unresolved")
	}
	proposal := NPCAggressiveTargetAcquisitionProposal{Now: now, before: s.clone(), plannedNow: now}
	for _, npcID := range s.ActiveNPCIDs {
		action, err := planNPCAIAggressiveTargetAction(s, npcID, now, roll)
		if err != nil {
			return NPCAggressiveTargetAcquisitionProposal{}, err
		}
		proposal.Actions = append(proposal.Actions, action)
		proposal.Changed = proposal.Changed || action.TimerWrite
	}
	proposal.NoOp = !proposal.Changed
	return proposal, nil
}

// ApplyNPCAggressiveTargetAcquisition validates and atomically applies a
// previously planned pass. It never consumes RNG. A stale or tampered action
// returns State{} and cannot leak a timer or enemy-list mutation.
func (s State) ApplyNPCAggressiveTargetAcquisition(proposal NPCAggressiveTargetAcquisitionProposal) (State, NPCAggressiveTargetResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, NPCAggressiveTargetResult{}, err
	}
	if proposal.Now < 0 || proposal.Now != proposal.plannedNow || proposal.before.Version != 1 || !reflect.DeepEqual(s, proposal.before) {
		return State{}, NPCAggressiveTargetResult{}, fmt.Errorf("stale or invalid NPC AI proposal")
	}
	if s.NPCs == nil || s.ActiveNPCIDs == nil {
		return State{}, NPCAggressiveTargetResult{}, fmt.Errorf("NPC AI canonical order unavailable")
	}

	var expected []NPCAggressiveTargetAction
	changed := false
	for _, npcID := range s.ActiveNPCIDs {
		action, err := replayNPCAIAggressiveTargetAction(s, npcID, proposal.Now, proposal.Actions, len(expected))
		if err != nil {
			return State{}, NPCAggressiveTargetResult{}, err
		}
		expected = append(expected, action)
		changed = changed || action.TimerWrite
	}
	if !reflect.DeepEqual(proposal.Actions, expected) {
		return State{}, NPCAggressiveTargetResult{}, fmt.Errorf("tampered NPC AI actions")
	}
	if proposal.Changed != changed || proposal.NoOp == changed {
		return State{}, NPCAggressiveTargetResult{}, fmt.Errorf("tampered NPC AI change marker")
	}

	next := s.clone()
	for _, action := range expected {
		if err := applyNPCAIAggressiveTargetAction(&next, action); err != nil {
			return State{}, NPCAggressiveTargetResult{}, err
		}
	}
	if err := next.Validate(); err != nil {
		return State{}, NPCAggressiveTargetResult{}, err
	}
	result := NPCAggressiveTargetResult{
		Now:     proposal.Now,
		Actions: cloneNPCAIActions(expected),
		Events:  cloneNPCAIEvents(expected),
		Changed: changed,
		NoOp:    !changed,
	}
	return next, result, nil
}

// ApplyNPCAggressiveTargetAcquisitionProposal is a descriptive alias for
// callers that name the value a proposal rather than a pass.
func (s State) ApplyNPCAggressiveTargetAcquisitionProposal(proposal NPCAggressiveTargetAcquisitionProposal) (State, NPCAggressiveTargetResult, error) {
	return s.ApplyNPCAggressiveTargetAcquisition(proposal)
}

func planNPCAIAggressiveTargetAction(s State, npcID string, now int32, roll func(int, int) int) (NPCAggressiveTargetAction, error) {
	action, npc, room, mode, eligible, err := npcAIAggressiveTargetPrefix(s, npcID, now)
	if err != nil || !eligible {
		return action, err
	}
	detectInvisible := flag(npc.Body.Flags[:], npcAIDetectInvisibleFlag)
	total := npcAITotalWeight(room, s.Players, npc.Body, mode, detectInvisible, true)
	action.TotalWeight = total
	if total == 0 {
		action.Status = npcAINoTarget
		return action, nil
	}
	selectionRoll, err := randomIn(roll, 1, total)
	if err != nil {
		return NPCAggressiveTargetAction{}, err
	}
	targetID, targetName, targetWeight, err := npcAIChooseTarget(room, s.Players, npc.Body, mode, detectInvisible, selectionRoll)
	if err != nil {
		return NPCAggressiveTargetAction{}, err
	}
	action.TargetID = targetID
	action.TargetName = targetName
	action.SelectionRoll = selectionRoll
	action.TargetWeight = targetWeight

	if npcAIDexterity(s.Players[targetID].Body) > npcAIDexterity(npc.Body) {
		escapeRoll, err := randomIn(roll, 1, 10)
		if err != nil {
			return NPCAggressiveTargetAction{}, err
		}
		action.EscapeRoll = escapeRoll
		if escapeRoll < 4 {
			action.Status = npcAIEscaped
			action.Escaped = true
			return action, nil
		}
	}

	action.Status = npcAITargetAcquired
	action.TimerWrite = true
	action.AttackTimerBefore = npc.Body.Timers[npcAIAttackTimerIndex]
	action.AttackTimerAfter = action.AttackTimerBefore
	action.AttackTimerAfter.LastTime = now
	action.AttackTimerAfter.Interval = 0
	action.EnemyAdded = !npcAIEnemyExists(npc.Enemies, EntityRef{Kind: "player", ID: targetID})
	action.Event = npcAIAggressiveTargetEvent(npcID, npc.Body, targetID, s.Players[targetID].Body)
	return action, nil
}

// replayNPCAIAggressiveTargetAction reconstructs one action from public
// recorded choices. The index argument lets a malformed proposal be rejected
// before any slice access and keeps the helper independent of caller shape.
func replayNPCAIAggressiveTargetAction(s State, npcID string, now int32, actions []NPCAggressiveTargetAction, index int) (NPCAggressiveTargetAction, error) {
	if index < 0 || index >= len(actions) {
		return NPCAggressiveTargetAction{}, fmt.Errorf("NPC AI action count mismatch")
	}
	recorded := actions[index]
	action, npc, room, mode, eligible, err := npcAIAggressiveTargetPrefix(s, npcID, now)
	if err != nil || !eligible {
		if err != nil {
			return NPCAggressiveTargetAction{}, err
		}
		if !reflect.DeepEqual(recorded, action) {
			return NPCAggressiveTargetAction{}, fmt.Errorf("tampered NPC AI no-op action")
		}
		return action, nil
	}
	detectInvisible := flag(npc.Body.Flags[:], npcAIDetectInvisibleFlag)
	total := npcAITotalWeight(room, s.Players, npc.Body, mode, detectInvisible, true)
	action.TotalWeight = total
	if total == 0 {
		action.Status = npcAINoTarget
		if !reflect.DeepEqual(recorded, action) {
			return NPCAggressiveTargetAction{}, fmt.Errorf("tampered NPC AI no-target action")
		}
		return action, nil
	}
	if recorded.SelectionRoll < 1 || recorded.SelectionRoll > total {
		return NPCAggressiveTargetAction{}, fmt.Errorf("NPC AI selection roll outside total")
	}
	targetID, targetName, targetWeight, err := npcAIChooseTarget(room, s.Players, npc.Body, mode, detectInvisible, recorded.SelectionRoll)
	if err != nil {
		return NPCAggressiveTargetAction{}, err
	}
	action.TargetID = targetID
	action.TargetName = targetName
	action.SelectionRoll = recorded.SelectionRoll
	action.TargetWeight = targetWeight

	if npcAIDexterity(s.Players[targetID].Body) > npcAIDexterity(npc.Body) {
		if recorded.EscapeRoll < 1 || recorded.EscapeRoll > 10 {
			return NPCAggressiveTargetAction{}, fmt.Errorf("NPC AI escape roll outside 1..10")
		}
		action.EscapeRoll = recorded.EscapeRoll
		if recorded.EscapeRoll < 4 {
			action.Status = npcAIEscaped
			action.Escaped = true
			if !reflect.DeepEqual(recorded, action) {
				return NPCAggressiveTargetAction{}, fmt.Errorf("tampered NPC AI escape action")
			}
			return action, nil
		}
	} else if recorded.EscapeRoll != 0 {
		return NPCAggressiveTargetAction{}, fmt.Errorf("NPC AI escape roll consumed without dexterity edge")
	}

	action.Status = npcAITargetAcquired
	action.TimerWrite = true
	action.AttackTimerBefore = npc.Body.Timers[npcAIAttackTimerIndex]
	action.AttackTimerAfter = action.AttackTimerBefore
	action.AttackTimerAfter.LastTime = now
	action.AttackTimerAfter.Interval = 0
	action.EnemyAdded = !npcAIEnemyExists(npc.Enemies, EntityRef{Kind: "player", ID: targetID})
	action.Event = npcAIAggressiveTargetEvent(npcID, npc.Body, targetID, s.Players[targetID].Body)
	if !reflect.DeepEqual(recorded, action) {
		return NPCAggressiveTargetAction{}, fmt.Errorf("tampered NPC AI target action")
	}
	return action, nil
}

func npcAIAggressiveTargetPrefix(s State, npcID string, now int32) (NPCAggressiveTargetAction, NPCState, RoomState, npcAIAggroMode, bool, error) {
	npc, ok := s.NPCs[npcID]
	if !ok || npcID == "" || npc.Body.Type != 1 {
		return NPCAggressiveTargetAction{}, NPCState{}, RoomState{}, 0, false, fmt.Errorf("unknown active NPC %q", npcID)
	}
	room, ok := s.Rooms[npc.Body.RoomID]
	if !ok || !containsString(room.NPCIDs, npcID) {
		return NPCAggressiveTargetAction{}, NPCState{}, RoomState{}, 0, false, fmt.Errorf("active NPC %q room membership absent", npcID)
	}
	action := NPCAggressiveTargetAction{NPCID: npcID, RoomID: npc.Body.RoomID}
	if !npcAIAttackReady(npc.Body.Timers[npcAIAttackTimerIndex], now) {
		action.Status = npcAINotReady
		return action, npc, room, 0, false, nil
	}
	// update_active removes an NPC from first_active before it reads the
	// room's enemy list when no player occupies the room. This target-only seam
	// records that as a pure no-op and intentionally does not infer peace from a
	// nil enemy slice in an empty room.
	if len(room.PlayerIDs) == 0 {
		action.Status = npcAINoPlayers
		return action, npc, room, 0, false, nil
	}
	if npc.Enemies == nil {
		return NPCAggressiveTargetAction{}, NPCState{}, RoomState{}, 0, false, fmt.Errorf("NPC %q enemy relations unresolved", npcID)
	}
	if len(npc.Enemies) != 0 {
		action.Status = npcAIExistingEnemy
		return action, npc, room, 0, false, nil
	}
	mode, aggressive := npcAIAggroModeForBody(npc.Body)
	if !aggressive {
		action.Status = npcAINotAggressive
		return action, npc, room, 0, false, nil
	}
	return action, npc, room, mode, true, nil
}

func npcAIAggroModeForBody(body LegacyMonster) (npcAIAggroMode, bool) {
	// update.c checks MAGGRE first, then passes MGAGGR ? -1 : 1, so a body
	// carrying multiple aggro bits has MAGGRE precedence and MGAGGR precedence
	// over MEAGGR.
	switch {
	case flag(body.Flags[:], npcAIAggressiveFlag):
		return npcAIAllPiety, true
	case flag(body.Flags[:], npcAIGoodAggressiveFlag):
		return npcAIGoodPiety, true
	case flag(body.Flags[:], npcAIEvilAggressiveFlag):
		return npcAIEvilPiety, true
	default:
		return 0, false
	}
}

func npcAIAttackReady(timer LegacyTimer, now int32) bool {
	// update.c expands LT(crt_ptr, LT_ATTCK) as ltime + interval and skips
	// only when that deadline is strictly greater than t. Equality is ready.
	return int64(timer.LastTime)+int64(timer.Interval) <= int64(now)
}

// NPCAggressiveTargetActorText is the direct notification printed to the
// selected player by update.c:638.
func NPCAggressiveTargetActorText(npcName string) string {
	return fmt.Sprintf("\n%s%s 당신을 공격합니다.\n", npcName, legacySubjectParticle(npcName))
}

// NPCAggressiveTargetRoomText is the room notification broadcast by
// update.c:643-645, excluding the selected player from the room fan-out.
func NPCAggressiveTargetRoomText(npcName, targetName string) string {
	targetDisplay := targetName + "님"
	return fmt.Sprintf("\n%s%s %s%s 공격합니다.\n", npcName, legacySubjectParticle(npcName), targetDisplay, valueObjectParticle(targetDisplay))
}

func npcAIAggressiveTargetEvent(npcID string, npc LegacyMonster, targetID string, target LegacyMonster) *NPCAggressiveTargetEvent {
	return &NPCAggressiveTargetEvent{
		RoomID:          npc.RoomID,
		NPCID:           npcID,
		NPCName:         npc.Name,
		TargetID:        targetID,
		TargetName:      target.Name,
		ExcludeTargetID: targetID,
		TargetText:      NPCAggressiveTargetActorText(npc.Name),
		RoomText:        NPCAggressiveTargetRoomText(npc.Name, target.Name),
	}
}

func npcAIAggroVisible(player LegacyMonster, detectInvisible bool) bool {
	if flag(player.Flags[:], npcAIHiddenPlayerFlag) || flag(player.Flags[:], npcAIDMInvisiblePlayerFlag) {
		return false
	}
	return !flag(player.Flags[:], npcAIInvisiblePlayerFlag) || detectInvisible
}

func npcAIAggroAlignmentAllowed(player LegacyMonster, mode npcAIAggroMode) bool {
	switch mode {
	case npcAIGoodPiety:
		return player.Alignment >= 100
	case npcAIEvilPiety:
		return player.Alignment <= -100
	default:
		return true
	}
}

func npcAIAggroLevelAllowed(npc, player LegacyMonster) bool {
	return npcAILevelBand(player.Level) >= npcAILevelBand(npc.Level)
}

func npcAILevelBand(level byte) int {
	return (int(level) + 3) / 4
}

func npcAIDexterity(body LegacyMonster) int {
	return int(int8(body.Stats[1]))
}

func npcAIPietyWeight(body LegacyMonster, mode npcAIAggroMode) int {
	base := 25
	if mode != npcAIAllPiety {
		base = 30
	}
	weight := base - int(int8(body.Stats[4]))
	if weight < 1 {
		return 1
	}
	return weight
}

func npcAIAggroEligible(npc, player LegacyMonster, mode npcAIAggroMode, detectInvisible, firstPass bool) bool {
	if !npcAIAggroVisible(player, detectInvisible) || !npcAIAggroAlignmentAllowed(player, mode) {
		return false
	}
	// low_piety_alg's second C pass accidentally omits the level predicate.
	return !firstPass || mode == npcAIAllPiety || npcAIAggroLevelAllowed(npc, player)
}

func npcAITotalWeight(room RoomState, players map[string]PlayerState, npc LegacyMonster, mode npcAIAggroMode, detectInvisible, firstPass bool) int {
	total := 0
	for _, playerID := range room.PlayerIDs {
		player := players[playerID]
		if !npcAIAggroEligible(npc, player.Body, mode, detectInvisible, firstPass) {
			continue
		}
		total += npcAIPietyWeight(player.Body, mode)
	}
	return total
}

func npcAIChooseTarget(room RoomState, players map[string]PlayerState, npc LegacyMonster, mode npcAIAggroMode, detectInvisible bool, selectionRoll int) (string, string, int, error) {
	total := npcAITotalWeight(room, players, npc, mode, detectInvisible, true)
	if total == 0 || selectionRoll < 1 || selectionRoll > total {
		return "", "", 0, fmt.Errorf("NPC AI selection roll outside total")
	}
	cumulative := 0
	for _, playerID := range room.PlayerIDs {
		player := players[playerID]
		// This deliberately uses firstPass=false: it mirrors low_piety_alg's
		// second loop, including its omitted level check.
		if !npcAIAggroEligible(npc, player.Body, mode, detectInvisible, false) {
			continue
		}
		weight := npcAIPietyWeight(player.Body, mode)
		cumulative += weight
		if cumulative >= selectionRoll {
			return playerID, player.Body.Name, weight, nil
		}
	}
	return "", "", 0, fmt.Errorf("NPC AI selection target absent")
}

func npcAIEnemyExists(enemies []NPCEnemy, target EntityRef) bool {
	for _, enemy := range enemies {
		if enemy.Target == target {
			return true
		}
	}
	return false
}

func applyNPCAIAggressiveTargetAction(s *State, action NPCAggressiveTargetAction) error {
	if s == nil || action.Status != npcAITargetAcquired || !action.TimerWrite {
		return nil
	}
	npc, ok := s.NPCs[action.NPCID]
	if !ok || npc.Body.RoomID != action.RoomID || npc.Body.Type != 1 {
		return fmt.Errorf("NPC AI target identity changed for %q", action.NPCID)
	}
	if npc.Body.Timers[npcAIAttackTimerIndex] != action.AttackTimerBefore {
		return fmt.Errorf("NPC AI attack timer changed for %q", action.NPCID)
	}
	npc.Body.Timers[npcAIAttackTimerIndex] = action.AttackTimerAfter
	target := EntityRef{Kind: "player", ID: action.TargetID}
	if action.EnemyAdded {
		if npcAIEnemyExists(npc.Enemies, target) {
			return fmt.Errorf("NPC AI duplicate enemy for %q", action.NPCID)
		}
		npc.Enemies = append(append([]NPCEnemy{}, npc.Enemies...), NPCEnemy{Target: target, Damage: 0})
	} else if !npcAIEnemyExists(npc.Enemies, target) {
		return fmt.Errorf("NPC AI expected existing enemy for %q", action.NPCID)
	}
	s.NPCs[action.NPCID] = npc
	return nil
}

func cloneNPCAIActions(in []NPCAggressiveTargetAction) []NPCAggressiveTargetAction {
	if in == nil {
		return nil
	}
	out := make([]NPCAggressiveTargetAction, len(in))
	copy(out, in)
	for i := range out {
		if in[i].Event != nil {
			event := *in[i].Event
			out[i].Event = &event
		}
	}
	return out
}

func cloneNPCAIEvents(actions []NPCAggressiveTargetAction) []NPCAggressiveTargetEvent {
	var events []NPCAggressiveTargetEvent
	for _, action := range actions {
		if action.Event != nil {
			events = append(events, *action.Event)
		}
	}
	return events
}
