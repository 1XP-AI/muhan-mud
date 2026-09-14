package world

// recall.go is magic5.c:recall (SRECAL / 귀환) on the existing CommandCast path.
// This is the 2-token self-cast only: MP 30, CLERIC or class>=INVINCIBLE, learned
// bit, then teleport the caster to room 1001. Targeted `주문 귀환 <name>` stays
// rejected by ParseCastLine and fail-closed here. Missing destination occupancy
// and unmigrated dest spawn/NPC active lists fail closed so ApplyCast/replay
// never invent a square or consult the host RNG. Distinct from command1.c:return_square.

import (
	"fmt"
	"math"
	"reflect"
)

const (
	castRecallSpell             = 16 // SRECAL / 귀환
	recallCost            int16 = 30
	recallSquareRoom            = int16(1001)
	recallMaleFlag              = 12 // PMALES
	recallDMInvisibleFlag       = 10 // PDMINV
)

const (
	recallManaResponse      = "당신이 도력이 부족합니다.\r\n"
	recallClassResponse     = "불제자만이 이 주술을 사용할 수 있습니다.\r\n"
	recallUnlearnedResponse = "당신은 아직 그런 주문을 터득하지 못했습니다.\r\n"
	recallCasterText        = "귀환 주문을 외웠습니다.\r\n"
)

// IsRecallCastSpell reports whether the token uniquely resolves to SRECAL.
func IsRecallCastSpell(name string) bool {
	spec, err := castSpellSpecFor(name)
	return err == nil && spec.Index == castRecallSpell && spec.healKind == castHealRecall
}

func recallPronoun(body LegacyMonster) string {
	if flag(body.Flags[:], recallMaleFlag) {
		return "그"
	}
	return "그녀"
}

func recallRoomText(name, pronoun string) string {
	return fmt.Sprintf("\n%s이 %s 자신에게 귀환 주문을 외웠습니다.\r\n", name, pronoun)
}

// RecallDestArrivalText is room.c:add_ply_rom's dest broadcast_rom
// ("\n%M%j 도착하였습니다.") after self-recall lands in room 1001.
func RecallDestArrivalText(name string) string {
	return fmt.Sprintf("\n%s이 도착하였습니다.\r\n", name)
}

// RecallDestArrivalVisible is add_ply_rom's PDMINV/PHIDDN skip for dest
// arrival text. Self-recall already clears PHIDDN before landing.
func RecallDestArrivalVisible(body LegacyMonster) bool {
	return !flag(body.Flags[:], recallDMInvisibleFlag) && !flag(body.Flags[:], castHiddenFlag)
}

func recallGate(actor PlayerState) string {
	if actor.Body.MPCurrent < recallCost {
		return recallManaResponse
	}
	if actor.Body.Class != castClericClass && actor.Body.Class < castInvincible {
		return recallClassResponse
	}
	if !legacyInfoFlag(actor.Body.Spells[:], uint(castRecallSpell)) {
		return recallUnlearnedResponse
	}
	return ""
}

func recallRefreshPending(room LegacyRoom, now int32) bool {
	due := func(t LegacyTimer) bool {
		return t.Misc != 0 && int64(t.LastTime)+int64(t.Interval) <= int64(now)
	}
	for _, t := range room.PermanentMonsters {
		if due(t) {
			return true
		}
	}
	for _, t := range room.PermanentObjects {
		if due(t) {
			return true
		}
	}
	return false
}

func recallIDsEqual(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}

func recallInsertPlayerID(s State, ids []string, id string) []string {
	name := s.Players[id].Body.Name
	at := len(ids)
	for i, existing := range ids {
		if s.Players[existing].Body.Name > name {
			at = i
			break
		}
	}
	out := make([]string, 0, len(ids)+1)
	out = append(out, ids[:at]...)
	out = append(out, id)
	out = append(out, ids[at:]...)
	return out
}

func recallNPCActiveUnmigrated(s State, empty bool, room RoomState, kind string) error {
	if empty && len(room.NPCIDs) > 0 && s.ActiveNPCIDs == nil {
		return fmt.Errorf("%w: recall %s NPC active list is not migrated", ErrCastSpellUnavailable, kind)
	}
	return nil
}

func (s State) recallPlanOccupancy(destID, sourceID int16, actorID string) (srcIDs, dstIDs []string, beenHere int32, deactivate, activateDest bool, err error) {
	dest, ok := s.Rooms[destID]
	if !ok || dest.Resource.ID != destID {
		return nil, nil, 0, false, false, fmt.Errorf("%w: recall destination room is not migrated", ErrCastSpellUnavailable)
	}
	source, ok := s.Rooms[sourceID]
	if !ok || source.Resource.ID != sourceID {
		return nil, nil, 0, false, false, fmt.Errorf("%w: recall source room is not migrated", ErrCastSpellUnavailable)
	}
	if dest.Resource.BeenHere == math.MaxInt32 {
		return nil, nil, 0, false, false, fmt.Errorf("%w: recall visit counter overflow", ErrCastSpellUnavailable)
	}
	if destID == sourceID {
		ids, found := removeString(dest.PlayerIDs, actorID)
		if !found {
			return nil, nil, 0, false, false, fmt.Errorf("%w: recall occupancy is not migrated", ErrCastSpellUnavailable)
		}
		if err := recallNPCActiveUnmigrated(s, len(ids) == 0, dest, "destination"); err != nil {
			return nil, nil, 0, false, false, err
		}
		activate := len(ids) == 0 && len(dest.NPCIDs) > 0
		ids = recallInsertPlayerID(s, ids, actorID)
		return ids, ids, dest.Resource.BeenHere + 1, activate, activate, nil
	}
	src, found := removeString(source.PlayerIDs, actorID)
	if !found {
		return nil, nil, 0, false, false, fmt.Errorf("%w: recall occupancy is not migrated", ErrCastSpellUnavailable)
	}
	if containsString(dest.PlayerIDs, actorID) {
		return nil, nil, 0, false, false, fmt.Errorf("%w: recall actor occupies both rooms", ErrCastSpellUnavailable)
	}
	if err := recallNPCActiveUnmigrated(s, len(src) == 0, source, "source"); err != nil {
		return nil, nil, 0, false, false, err
	}
	if err := recallNPCActiveUnmigrated(s, len(dest.PlayerIDs) == 0, dest, "destination"); err != nil {
		return nil, nil, 0, false, false, err
	}
	dst := recallInsertPlayerID(s, dest.PlayerIDs, actorID)
	return src, dst, dest.Resource.BeenHere + 1, len(src) == 0 && len(source.NPCIDs) > 0, len(dest.PlayerIDs) == 0 && len(dest.NPCIDs) > 0, nil
}

func (s *State) recallApplyActive(sourceID int16, deactivate, activateDest bool) {
	if deactivate {
		s.deactivateRoomNPCs(sourceID)
	}
	if activateDest {
		s.activateEntryNPCs(recallSquareRoom, true, nil)
	}
}

func (s State) recallArrivalScene(actorID string, destID, sourceID int16, srcIDs, dstIDs []string, beenHere int32, hour int) (string, error) {
	next := s.clone()
	dest := next.Rooms[destID]
	dest.PlayerIDs = append([]string(nil), dstIDs...)
	dest.Resource.BeenHere = beenHere
	next.Rooms[destID] = dest
	if destID != sourceID {
		source := next.Rooms[sourceID]
		source.PlayerIDs = append([]string(nil), srcIDs...)
		next.Rooms[sourceID] = source
	}
	actor := next.Players[actorID]
	actor.Body.RoomID = destID
	next.Players[actorID] = actor
	scene, err := next.locateScene(actorID, destID, hour)
	if err != nil {
		return "", err
	}
	return scene, nil
}

func recallNoopFieldsInvalid(p CastProposal) bool {
	return p.Broadcast || p.Succeeded || p.SpellFailed || p.EffectRolls != nil || p.HPDelta != 0 || p.MPDelta != 0 || p.SpellInterval != 0 || p.TimedFlag != 0 || p.TimedInterval != 0 || p.CursedItemsCleared != 0 || p.afterItemsSet || p.LocateLinked || p.TargetText != "" || p.RoomText != "" || p.deactivateSource || p.afterActiveSet || p.afterDestBeenHere != 0 || p.afterSourceIDs != nil || p.afterDestIDs != nil
}

func (s State) planRecallCast(p CastProposal, actor PlayerState, room RoomState, spec castSpellSpec, options CastOptions) (CastProposal, error) {
	if spec.Index != castRecallSpell || spec.healKind != castHealRecall {
		return CastProposal{}, ErrCastInvalidProposal
	}
	if options.Target != "" {
		return CastProposal{}, fmt.Errorf("%w: targeted recall is not migrated", ErrCastSpellUnavailable)
	}
	p.Cost = recallCost
	if msg := recallGate(actor); msg != "" {
		p.Response = msg
		return p, nil
	}
	dest, ok := s.Rooms[recallSquareRoom]
	if !ok || dest.Resource.ID != recallSquareRoom {
		return CastProposal{}, fmt.Errorf("%w: recall destination room is not migrated", ErrCastSpellUnavailable)
	}
	if !containsString(room.PlayerIDs, p.ActorID) {
		return CastProposal{}, fmt.Errorf("%w: recall occupancy is not migrated", ErrCastSpellUnavailable)
	}
	if recallRefreshPending(dest.Resource, options.Now) {
		return CastProposal{}, fmt.Errorf("%w: recall destination spawn refresh is not migrated", ErrCastSpellUnavailable)
	}
	srcIDs, dstIDs, beenHere, deactivate, activateDest, err := s.recallPlanOccupancy(recallSquareRoom, p.RoomID, p.ActorID)
	if err != nil {
		return CastProposal{}, err
	}
	scene, err := s.recallArrivalScene(p.ActorID, recallSquareRoom, p.RoomID, srcIDs, dstIDs, beenHere, options.Hour)
	if err != nil {
		return CastProposal{}, err
	}
	interval := castSpellInterval(actor.Body.Class)
	p.afterBody, p.HPDelta, err = castBodyAfter(actor.Body, room, spec, options, nil, false, false)
	if err != nil {
		return CastProposal{}, err
	}
	p.afterBody.Timers[castSpellTimerIndex] = LegacyTimer{LastTime: options.Now, Interval: interval}
	p.afterBody.RoomID = recallSquareRoom
	p.MPDelta = int32(p.afterBody.MPCurrent) - int32(actor.Body.MPCurrent)
	p.Attempted = true
	p.Chance = 100
	p.Changed = true
	p.Succeeded = true
	p.Broadcast = true
	p.SpellInterval = interval
	p.sourceRoomID = p.RoomID
	p.targetRoomID = recallSquareRoom
	p.expectedTargetRoomFlags = dest.Resource.Flags
	p.expectedSourceIDs = append([]string(nil), room.PlayerIDs...)
	p.expectedDestIDs = append([]string(nil), dest.PlayerIDs...)
	p.afterSourceIDs = srcIDs
	p.afterDestIDs = dstIDs
	p.afterDestBeenHere = beenHere
	p.deactivateSource = deactivate
	if deactivate || activateDest {
		probe := s.clone()
		destRoom := probe.Rooms[recallSquareRoom]
		destRoom.PlayerIDs = append([]string(nil), dstIDs...)
		destRoom.Resource.BeenHere = beenHere
		probe.Rooms[recallSquareRoom] = destRoom
		if p.RoomID != recallSquareRoom {
			source := probe.Rooms[p.RoomID]
			source.PlayerIDs = append([]string(nil), srcIDs...)
			probe.Rooms[p.RoomID] = source
		}
		probe.recallApplyActive(p.RoomID, deactivate, activateDest)
		p.afterActiveNPCIDs = append([]string{}, probe.ActiveNPCIDs...)
		p.afterActiveSet = true
	}
	p.RoomText = recallRoomText(actor.Body.Name, recallPronoun(actor.Body))
	p.Response = recallCasterText + scene
	return p, nil
}

func (s State) applyRecallCast(p CastProposal, actor PlayerState, room RoomState, spec castSpellSpec) (State, CastResult, error) {
	if p.Hour < 0 || spec.healKind != castHealRecall || spec.Index != castRecallSpell || p.HealKind != castHealRecall || p.SpellIndex != castRecallSpell || p.Cost != recallCost {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}
	if p.TargetName != "" || p.TargetID != "" || p.expectedTargetSet || p.LocateLinked || p.TargetText != "" {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}
	if !p.Attempted {
		if recallNoopFieldsInvalid(p) || p.Chance != 0 || p.Roll != 0 {
			return State{}, CastResult{}, ErrCastInvalidProposal
		}
		if msg := recallGate(actor); msg == "" || p.Response != msg {
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

	if !p.Changed || !p.Succeeded || p.SpellFailed || !p.Broadcast || p.Chance != 100 || p.Roll != 0 || p.EffectRolls != nil || p.HPDelta != 0 || p.TimedFlag != 0 || p.TimedInterval != 0 || p.CursedItemsCleared != 0 || p.afterItemsSet {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}
	if p.HiddenCleared != flag(actor.Body.Flags[:], castHiddenFlag) {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}
	if recallGate(actor) != "" {
		return State{}, CastResult{}, ErrCastStaleProposal
	}
	dest, ok := s.Rooms[recallSquareRoom]
	if !ok || dest.Resource.ID != recallSquareRoom || dest.Resource.Flags != p.expectedTargetRoomFlags {
		return State{}, CastResult{}, ErrCastStaleProposal
	}
	if p.sourceRoomID != p.RoomID || p.targetRoomID != recallSquareRoom {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}
	if !recallIDsEqual(room.PlayerIDs, p.expectedSourceIDs) || !recallIDsEqual(dest.PlayerIDs, p.expectedDestIDs) {
		return State{}, CastResult{}, ErrCastStaleProposal
	}
	if recallRefreshPending(dest.Resource, p.Now) {
		return State{}, CastResult{}, ErrCastStaleProposal
	}
	if p.SpellInterval != castSpellInterval(actor.Body.Class) {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}
	srcIDs, dstIDs, beenHere, deactivate, activateDest, err := s.recallPlanOccupancy(recallSquareRoom, p.RoomID, p.ActorID)
	if err != nil || deactivate != p.deactivateSource || beenHere != p.afterDestBeenHere {
		return State{}, CastResult{}, ErrCastStaleProposal
	}
	if !recallIDsEqual(srcIDs, p.afterSourceIDs) || !recallIDsEqual(dstIDs, p.afterDestIDs) {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}
	scene, err := s.recallArrivalScene(p.ActorID, recallSquareRoom, p.RoomID, srcIDs, dstIDs, beenHere, p.Hour)
	if err != nil {
		return State{}, CastResult{}, ErrCastStaleProposal
	}
	if p.RoomText != recallRoomText(actor.Body.Name, recallPronoun(actor.Body)) || p.Response != recallCasterText+scene {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}
	after, hpDelta, err := castBodyAfter(actor.Body, room, spec, CastOptions{}, nil, false, false)
	if err != nil || hpDelta != p.HPDelta {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}
	after.Timers[castSpellTimerIndex] = LegacyTimer{LastTime: p.Now, Interval: p.SpellInterval}
	after.RoomID = recallSquareRoom
	if int32(after.MPCurrent)-int32(actor.Body.MPCurrent) != p.MPDelta || !reflect.DeepEqual(after, p.afterBody) {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}
	next := s.clone()
	nextActor := next.Players[p.ActorID]
	nextActor.Body = after
	next.Players[p.ActorID] = nextActor
	dest = next.Rooms[recallSquareRoom]
	dest.PlayerIDs = append([]string(nil), dstIDs...)
	dest.Resource.BeenHere = beenHere
	next.Rooms[recallSquareRoom] = dest
	if p.RoomID != recallSquareRoom {
		source := next.Rooms[p.RoomID]
		source.PlayerIDs = append([]string(nil), srcIDs...)
		next.Rooms[p.RoomID] = source
	}
	if deactivate || activateDest {
		next.recallApplyActive(p.RoomID, deactivate, activateDest)
		if !p.afterActiveSet || !recallIDsEqual(next.ActiveNPCIDs, p.afterActiveNPCIDs) {
			return State{}, CastResult{}, ErrCastInvalidProposal
		}
	} else if p.afterActiveSet {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}
	if err := next.Validate(); err != nil {
		return State{}, CastResult{}, err
	}
	return next, castResult(p, nextActor), nil
}
