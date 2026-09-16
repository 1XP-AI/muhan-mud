package world

// know_alignment.go is the player-target SKNOWA / 선악감지 branch from
// magic6.c.  The self-target form remains in cast.go; this file owns only the
// explicit same-room player target and its receipt-bound projection.

import (
	"fmt"
	"math"
	"reflect"
)

const (
	knowAlignmentMissingResponse   = "그런 사람이 존재하지 않습니다.\r\n"
	knowAlignmentManaResponse      = "당신의 도력이 부족합니다.\r\n"
	knowAlignmentUnlearnedResponse = "당신은 아직 그 주술을 터득하지 못했습니다.\r\n"
	knowAlignmentCaretakerClass    = 10
	knowAlignmentDMInvisibleFlag   = 10 // PDMINV
	knowAlignmentInvisibleFlag     = 2  // MINVIS/PINVIS
	knowAlignmentDetectFlag        = 21 // PDINVI
)

func knowAlignmentGate(actor PlayerState) string {
	if actor.Body.MPCurrent < 6 {
		return knowAlignmentManaResponse
	}
	if !legacyInfoFlag(actor.Body.Spells[:], uint(castKnowAlignmentSpell)) {
		return knowAlignmentUnlearnedResponse
	}
	return ""
}

// knowAlignmentTargetVisible mirrors creature.c:find_crt.  A caretaker or
// higher PDMINV identity is always skipped; ordinary MINVIS is visible only
// to a caster with PDINVI.
func knowAlignmentTargetVisible(actor, target LegacyMonster) bool {
	if target.Class >= knowAlignmentCaretakerClass && flag(target.Flags[:], knowAlignmentDMInvisibleFlag) {
		return false
	}
	return flag(actor.Flags[:], knowAlignmentDetectFlag) || !flag(target.Flags[:], knowAlignmentInvisibleFlag)
}

// knowAlignmentFindTarget resolves exactly the authoritative first_ply order
// represented by RoomState.PlayerIDs.  Every listed identity is checked even
// after a match, so malformed room membership can never be bypassed by an
// earlier occurrence or by consulting the unordered Players map.
func (s State) knowAlignmentFindTarget(actor PlayerState, room RoomState, query string, occurrence int) (string, PlayerState, error) {
	if occurrence < 1 || occurrence > math.MaxInt32 {
		return "", PlayerState{}, fmt.Errorf("invalid know-alignment target occurrence")
	}
	if !validRoomPlayerTargetName(query) {
		return "", PlayerState{}, nil
	}
	var found int
	var selectedID string
	var selected PlayerState
	for _, id := range room.PlayerIDs {
		candidate, ok := s.Players[id]
		if id == "" || !ok || !candidate.Online || candidate.Body.Type != 0 || candidate.Body.Name == "" || candidate.Body.RoomID != room.Resource.ID {
			return "", PlayerState{}, fmt.Errorf("%w: know-alignment target identity is not authoritative", ErrCastSpellUnavailable)
		}
		if !legacyCreaturePrefixMatch(candidate.Body, query) || !knowAlignmentTargetVisible(actor.Body, candidate.Body) {
			continue
		}
		found++
		if found == occurrence {
			selectedID, selected = id, candidate
		}
	}
	if selectedID == "" {
		return "", PlayerState{}, nil
	}
	return selectedID, selected, nil
}

func knowAlignmentTargetOccurrence(target string, occurrence int) (int, error) {
	if target == "" {
		if occurrence != 0 && occurrence != 1 {
			return 0, fmt.Errorf("invalid know-alignment target occurrence")
		}
		return 0, nil
	}
	if occurrence == 0 {
		return 1, nil
	}
	if occurrence < 1 || occurrence > math.MaxInt32 {
		return 0, fmt.Errorf("invalid know-alignment target occurrence")
	}
	return occurrence, nil
}

func knowAlignmentIDsEqual(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}

func knowAlignmentApplyBody(body *LegacyMonster, now, interval int32) error {
	if body == nil || now < 0 || interval < 0 || castKnowAlignmentFlag >= uint(len(body.Flags)*8) || castKnowAlignmentTimer < 0 || castKnowAlignmentTimer >= len(body.Timers) {
		return ErrCastSpellUnavailable
	}
	setSettingFlag(body, castKnowAlignmentFlag, true)
	body.Timers[castKnowAlignmentTimer] = LegacyTimer{LastTime: now, Interval: interval}
	return nil
}

func knowAlignmentRoomText(actorName, targetName string) string {
	return fmt.Sprintf("\n%s이 %s에게 선악감지 주문을 외웁니다.\r\n그는 선악을 감지할 수 있는 식별력이 높아졌습니다.\r\n", actorName, targetName)
}

func knowAlignmentTargetText(actorName string) string {
	return fmt.Sprintf("\n%s이 당신에게 선악감지 주문을 외웁니다.\r\n당신은 선악을 감지할 수 있는 식별력이 높아졌습니다.\r\n", actorName)
}

func knowAlignmentCasterText(targetName string) string {
	return fmt.Sprintf("당신은 %s에게 선악감지 주문을 외웁니다.\r\n", targetName)
}

func (s State) planKnowAlignmentCast(p CastProposal, actor PlayerState, room RoomState, spec castSpellSpec, options CastOptions) (CastProposal, error) {
	if spec.Index != castKnowAlignmentSpell || spec.healKind != castHealTimed || spec.Cost != 6 {
		return CastProposal{}, ErrCastInvalidProposal
	}
	p.Cost = spec.Cost
	p.TargetName = options.Target
	occurrence, err := knowAlignmentTargetOccurrence(options.Target, options.Occurrence)
	if err != nil {
		return CastProposal{}, err
	}
	// Keep the command form and its lookup query receipt-bound. ApplyCast must
	// not infer targeted-vs-self form from TargetName, which is an exported
	// field on the in-memory proposal.
	p.knowAlignmentTargeted = true
	p.knowAlignmentTargetName = options.Target
	p.knowAlignmentTargetOccurrence = occurrence
	p.TargetOccurrence = occurrence
	p.expectedSourceIDs = append([]string(nil), room.PlayerIDs...)
	if msg := knowAlignmentGate(actor); msg != "" {
		p.Response = msg
		return p, nil
	}
	targetID, target, err := s.knowAlignmentFindTarget(actor, room, options.Target, occurrence)
	if err != nil {
		return CastProposal{}, err
	}
	if targetID == "" {
		p.Response = knowAlignmentMissingResponse
		return p, nil
	}

	p.Attempted = true
	p.Chance = 100
	p.Roll = 0
	p.Changed = true
	p.Succeeded = true
	p.Broadcast = true
	p.SpellInterval = castSpellInterval(actor.Body.Class)
	p.TimedFlag = castKnowAlignmentFlag
	p.TimedInterval, err = castTimedIntervalWithRoll(actor.Body, room, spec, 0)
	if err != nil {
		return CastProposal{}, err
	}
	p.TargetID = targetID
	p.expectedTarget = cloneDrinkActor(target)
	p.expectedTargetSet = true
	p.knowAlignmentTargetID = targetID
	p.knowAlignmentExpectedTarget = cloneDrinkActor(target)
	p.targetRoomID = room.Resource.ID
	p.afterBody, p.HPDelta, err = castBodyAfter(actor.Body, room, spec, options, nil, false, false)
	if err != nil {
		return CastProposal{}, err
	}
	p.afterBody.Timers[castSpellTimerIndex] = LegacyTimer{LastTime: options.Now, Interval: p.SpellInterval}
	p.afterTarget = cloneDrinkActor(target)
	if targetID == p.ActorID {
		if err := knowAlignmentApplyBody(&p.afterBody, options.Now, p.TimedInterval); err != nil {
			return CastProposal{}, err
		}
		p.afterTarget = cloneDrinkActor(actor)
		p.afterTarget.Body = p.afterBody
	} else {
		if err := knowAlignmentApplyBody(&p.afterTarget.Body, options.Now, p.TimedInterval); err != nil {
			return CastProposal{}, err
		}
	}
	p.MPDelta = int32(p.afterBody.MPCurrent) - int32(actor.Body.MPCurrent)
	p.RoomText = knowAlignmentRoomText(actor.Body.Name, target.Body.Name)
	p.TargetText = knowAlignmentTargetText(actor.Body.Name)
	p.Response = knowAlignmentCasterText(target.Body.Name)
	return p, nil
}

// knowAlignmentTargetProposalPresent identifies any explicit know-alignment
// target material, including the private form/snapshot binding. A generic
// self-cast has none of these fields, so target material cannot be erased from
// the exported fields to reach the generic reducer.
func knowAlignmentTargetProposalPresent(p CastProposal) bool {
	return p.knowAlignmentTargeted || p.TargetName != "" || p.TargetID != "" || p.TargetOccurrence != 0 || p.TargetText != "" || p.expectedTargetSet || !reflect.DeepEqual(p.expectedTarget, PlayerState{}) || !reflect.DeepEqual(p.afterTarget, PlayerState{}) || p.knowAlignmentTargetID != "" || p.knowAlignmentTargetName != "" || p.knowAlignmentTargetOccurrence != 0 || !reflect.DeepEqual(p.knowAlignmentExpectedTarget, PlayerState{})
}

func knowAlignmentTargetBindingValid(p CastProposal) bool {
	if !p.knowAlignmentTargeted || p.TargetName == "" || p.TargetName != p.knowAlignmentTargetName || p.TargetOccurrence != p.knowAlignmentTargetOccurrence {
		return false
	}
	if p.knowAlignmentTargetID == "" {
		return p.TargetID == "" && !p.expectedTargetSet && reflect.DeepEqual(p.expectedTarget, PlayerState{}) && reflect.DeepEqual(p.knowAlignmentExpectedTarget, PlayerState{})
	}
	return p.TargetID == p.knowAlignmentTargetID && p.expectedTargetSet && reflect.DeepEqual(p.expectedTarget, p.knowAlignmentExpectedTarget)
}

func knowAlignmentTargetNoopFieldsInvalid(p CastProposal) bool {
	return p.Broadcast || p.Succeeded || p.SpellFailed || p.EffectRolls != nil || p.HPDelta != 0 || p.MPDelta != 0 || p.SpellInterval != 0 || p.TimedFlag != 0 || p.TimedInterval != 0 || p.CursedItemsCleared != 0 || p.afterItemsSet || p.TargetID != "" || p.expectedTargetSet || p.TargetText != "" || p.RoomText != "" || p.TargetOccurrence < 1 || p.targetRoomID != 0 || p.LocateLinked || p.sourceRoomID != 0 || p.afterSourceIDs != nil || p.afterDestIDs != nil || p.afterDestBeenHere != 0 || p.deactivateSource || p.afterActiveSet || !reflect.DeepEqual(p.afterTarget, PlayerState{})
}

func (s State) applyKnowAlignmentCast(p CastProposal, actor PlayerState, room RoomState, spec castSpellSpec) (State, CastResult, error) {
	if !knowAlignmentTargetBindingValid(p) || p.TargetOccurrence < 1 || p.TargetOccurrence > math.MaxInt32 || p.Cost != spec.Cost || spec.Index != castKnowAlignmentSpell || spec.healKind != castHealTimed || p.HealKind != castHealTimed || p.SpellIndex != castKnowAlignmentSpell {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}
	if !knowAlignmentIDsEqual(room.PlayerIDs, p.expectedSourceIDs) {
		return State{}, CastResult{}, ErrCastStaleProposal
	}
	if !p.Attempted {
		if knowAlignmentTargetNoopFieldsInvalid(p) || p.Chance != 0 || p.Roll != 0 {
			return State{}, CastResult{}, ErrCastInvalidProposal
		}
		gate := knowAlignmentGate(actor)
		if p.Response == knowAlignmentMissingResponse {
			if gate != "" {
				return State{}, CastResult{}, ErrCastInvalidProposal
			}
			targetID, _, err := s.knowAlignmentFindTarget(actor, room, p.TargetName, p.TargetOccurrence)
			if err != nil || targetID != "" {
				return State{}, CastResult{}, ErrCastStaleProposal
			}
		} else if gate == "" || p.Response != gate {
			return State{}, CastResult{}, ErrCastInvalidProposal
		}
		if p.HiddenCleared != flag(actor.Body.Flags[:], castHiddenFlag) {
			return State{}, CastResult{}, ErrCastInvalidProposal
		}
		if p.HiddenCleared {
			if !p.Changed {
				return State{}, CastResult{}, ErrCastInvalidProposal
			}
			after := actor.Body
			setSettingFlag(&after, castHiddenFlag, false)
			if !reflect.DeepEqual(after, p.afterBody) {
				return State{}, CastResult{}, ErrCastInvalidProposal
			}
			next := s.clone()
			nextActor := next.Players[p.ActorID]
			nextActor.Body = after
			next.Players[p.ActorID] = nextActor
			if err := next.Validate(); err != nil {
				return State{}, CastResult{}, err
			}
			return next, castResult(p, nextActor), nil
		}
		if p.Changed || !reflect.DeepEqual(p.afterBody, LegacyMonster{}) {
			return State{}, CastResult{}, ErrCastInvalidProposal
		}
		return s.clone(), castResult(p, actor), nil
	}

	if !p.Changed || !p.Succeeded || p.SpellFailed || !p.Broadcast || p.Chance != 100 || p.Roll != 0 || p.EffectRolls != nil || p.HPDelta != 0 || p.DailyUsed || p.CursedItemsCleared != 0 || p.afterItemsSet || p.TargetID == "" || !p.expectedTargetSet || p.TargetText == "" || p.RoomText == "" || p.TimedFlag != castKnowAlignmentFlag || p.TimedInterval < 0 || p.targetRoomID != room.Resource.ID || p.LocateLinked || p.sourceRoomID != 0 || p.afterSourceIDs != nil || p.afterDestIDs != nil || p.expectedDestIDs != nil || p.afterDestBeenHere != 0 || p.deactivateSource || p.afterActiveSet {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}
	if p.HiddenCleared != flag(actor.Body.Flags[:], castHiddenFlag) {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}
	if knowAlignmentGate(actor) != "" {
		return State{}, CastResult{}, ErrCastStaleProposal
	}
	if !containsString(room.PlayerIDs, p.ActorID) {
		return State{}, CastResult{}, ErrCastStaleProposal
	}
	target, ok := s.Players[p.TargetID]
	if !ok || !target.Online || target.Body.Type != 0 || target.Body.Name == "" || target.Body.RoomID != room.Resource.ID || !containsString(room.PlayerIDs, p.TargetID) || !knowAlignmentTargetVisible(actor.Body, target.Body) || !reflect.DeepEqual(target, p.expectedTarget) {
		return State{}, CastResult{}, ErrCastStaleProposal
	}
	resolvedID, resolved, err := s.knowAlignmentFindTarget(actor, room, p.TargetName, p.TargetOccurrence)
	if err != nil || resolvedID != p.TargetID || !reflect.DeepEqual(resolved, target) {
		return State{}, CastResult{}, ErrCastStaleProposal
	}
	interval, err := castTimedIntervalWithRoll(actor.Body, room, spec, 0)
	if err != nil || p.TimedInterval != interval || p.SpellInterval != castSpellInterval(actor.Body.Class) {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}
	if p.Response != knowAlignmentCasterText(target.Body.Name) || p.RoomText != knowAlignmentRoomText(actor.Body.Name, target.Body.Name) || p.TargetText != knowAlignmentTargetText(actor.Body.Name) || p.MPDelta != -6 {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}
	after, hpDelta, err := castBodyAfter(actor.Body, room, spec, CastOptions{}, nil, false, false)
	if err != nil || hpDelta != 0 {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}
	after.Timers[castSpellTimerIndex] = LegacyTimer{LastTime: p.Now, Interval: p.SpellInterval}
	afterTarget := cloneDrinkActor(target)
	if p.TargetID == p.ActorID {
		if err := knowAlignmentApplyBody(&after, p.Now, p.TimedInterval); err != nil {
			return State{}, CastResult{}, ErrCastInvalidProposal
		}
		afterTarget = cloneDrinkActor(actor)
		afterTarget.Body = after
	} else {
		if err := knowAlignmentApplyBody(&afterTarget.Body, p.Now, p.TimedInterval); err != nil {
			return State{}, CastResult{}, ErrCastInvalidProposal
		}
	}
	if int32(after.MPCurrent)-int32(actor.Body.MPCurrent) != p.MPDelta || !reflect.DeepEqual(after, p.afterBody) || !reflect.DeepEqual(afterTarget, p.afterTarget) {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}
	next := s.clone()
	nextActor := next.Players[p.ActorID]
	nextActor.Body = after
	next.Players[p.ActorID] = nextActor
	if p.TargetID != p.ActorID {
		nextTarget := next.Players[p.TargetID]
		nextTarget.Body = afterTarget.Body
		next.Players[p.TargetID] = nextTarget
	}
	if err := next.Validate(); err != nil {
		return State{}, CastResult{}, err
	}
	return next, castResult(p, nextActor), nil
}
