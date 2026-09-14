package world

// summon.go is magic5.c:summon (SSUMMO / 소환) on the existing CommandCast path.
// C gates stay fail-closed: class MP 50/100, learned bit, mrand(1,100)<51 (always
// -50 MP), then find_who, CAST MP debit, destination occupancy/family/level/
// PNOSUM, source RNOLEA. ApplyCast/replay never consult the host RNG.

import (
	"fmt"
	"reflect"
)

const (
	castSummonSpell             = 17 // SSUMMO / 소환
	summonNormalCost      int16 = 50
	summonStaffCost       int16 = 100
	summonFailCost        int16 = 50
	summonFailBelow             = 51
	summonCaretakerClass        = 10 // CARETAKER
	summonDMInvisibleFlag       = 10 // PDMINV
	summonNoTeleportFlag        = 12 // RNOTEL
	summonOnePlayerFlag         = 14 // RONEPL
	summonTwoPlayerFlag         = 15 // RTWOPL
	summonThreePlayerFlag       = 16 // RTHREE
	summonNoLeaveFlag           = 28 // RNOLEA
	summonNoSummonFlag          = 34 // PNOSUM
	summonFamilyRoomFlag        = 37 // RFAMIL
	summonOneFamilyFlag         = 38 // RONFML
	summonFamilyFlag            = 55 // PFAMIL
	summonFamilyDaily           = 9  // DL_EXPND
)

const (
	summonUnlearnedResponse  = "당신은 아직 그런 주술을 터득하지 못했습니다.\r\n"
	summonManaResponse       = "당신의 도력이 부족합니다.\r\n"
	summonFailResponse       = "소환에 실패를 하였습니다.\r\n"
	summonSelfResponse       = "자신을 소환하다뇨?.\r\n"
	summonMissingResponse    = "그런 사람을 못 찾습니다.\r\n"
	summonSuckedResponse     = "주문이 공중으로 빨려듭니다.\r\n"
	summonFamilyResponse     = "그사람은 패거리 가입자가 아닙니다.\r\n"
	summonOnlyFamilyResponse = "그사람은 이곳에 올수 없습니다.\r\n"
	summonLevelResponse      = "주문이 실패했습니다.\r\n"
	summonRefuseResponse     = "주문이 실패했습니다.\r\n상대가 소환 거부 중입니다.\r\n"
)

type summonResolved struct {
	targetID   string
	target     PlayerState
	sourceRoom RoomState
}

// IsSummonCastSpell reports whether the token uniquely resolves to SSUMMO.
func IsSummonCastSpell(name string) bool {
	spec, err := castSpellSpecFor(name)
	return err == nil && spec.Index == castSummonSpell && spec.healKind == castHealSummon
}

// IsTargetedCastSpell admits the three-token `주문 <spell> <name>` forms that
// already have a migrated targeted reducer (천리안, 소환).
func IsTargetedCastSpell(name string) bool {
	return IsLocateCastSpell(name) || IsSummonCastSpell(name)
}

func summonManaCost(class byte) int16 {
	if class == castInvincible || class == summonCaretakerClass {
		return summonStaffCost
	}
	return summonNormalCost
}

func summonAttemptFailed(roll int) bool {
	return roll < summonFailBelow
}

func summonCasterText(name string) string {
	return fmt.Sprintf("\n당신은 %s을 소환하기 위해 주문을 외웁니다.\r\n주문을 마치자 짙은 안개가 끼더니 갑자기 사라지면서 \n그가 나타났습니다.\r\n", name)
}

func summonTargetText(name, scene string) string {
	return fmt.Sprintf("\n당신주위에 짙은 안개가 끼더니 알 수 없는 힘에 이끌려 어디론가 날라갑니다.\r\n안개가 걷히자 %s이 당신앞에 서 있습니다.\r\n", name) + scene
}

func summonRoomText(actorName, targetName string) string {
	return fmt.Sprintf("\n%s이 소환주문을 외우자 짙은 안개가 깔리더니 갑자기 %s이 나타났습니다.\r\n", actorName, targetName)
}

func summonLeaveText(name string) string {
	return fmt.Sprintf("\n소환 주문이 %s을 찾지 못합니다.\r\n", name)
}

func summonApplyBody(body LegacyMonster, mpCost int16, now, interval int32, success bool) (LegacyMonster, int32) {
	after := body
	setSettingFlag(&after, castHiddenFlag, false)
	after.MPCurrent -= mpCost
	if success {
		after.Timers[castSpellTimerIndex] = LegacyTimer{LastTime: now, Interval: interval}
	}
	return after, int32(after.MPCurrent) - int32(body.MPCurrent)
}

func (s State) summonFindWho(actorID, query string) (string, PlayerState, error) {
	if !locateQueryOK(query) {
		return "", PlayerState{}, nil
	}
	want := locateLookupName(query)
	matches := make([]string, 0, 1)
	for id, player := range s.Players {
		if id == "" || !player.Online || player.Body.Type != 0 || player.Body.Name == "" {
			continue
		}
		if player.Body.Name != want {
			continue
		}
		matches = append(matches, id)
	}
	if len(matches) == 0 {
		return "", PlayerState{}, nil
	}
	if len(matches) != 1 {
		return "", PlayerState{}, fmt.Errorf("%w: summon target name is ambiguous", ErrCastSpellUnavailable)
	}
	id := matches[0]
	if id == actorID {
		return "", PlayerState{}, nil
	}
	return id, s.Players[id], nil
}

func summonCountVis(s State, room RoomState) int {
	n := 0
	for _, id := range room.PlayerIDs {
		player, ok := s.Players[id]
		if !ok || !player.Online {
			continue
		}
		if flag(player.Body.Flags[:], summonDMInvisibleFlag) {
			continue
		}
		n++
	}
	return n
}

func summonRefreshPending(room LegacyRoom, now int32) bool {
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

func summonIDsEqual(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}

func summonInsertPlayerID(s State, ids []string, id string) []string {
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

func (s State) summonPlanOccupancy(destID, sourceID int16, targetID string) (srcIDs, dstIDs []string, beenHere int32, deactivate bool, err error) {
	dest, ok := s.Rooms[destID]
	if !ok {
		return nil, nil, 0, false, fmt.Errorf("%w: summon destination room is not migrated", ErrCastSpellUnavailable)
	}
	source, ok := s.Rooms[sourceID]
	if !ok {
		return nil, nil, 0, false, fmt.Errorf("%w: summon source room is not migrated", ErrCastSpellUnavailable)
	}
	if dest.Resource.BeenHere == 2147483647 {
		return nil, nil, 0, false, fmt.Errorf("%w: summon visit counter overflow", ErrCastSpellUnavailable)
	}
	if destID == sourceID {
		ids, found := removeString(dest.PlayerIDs, targetID)
		if !found {
			return nil, nil, 0, false, fmt.Errorf("%w: summon target occupancy is not migrated", ErrCastSpellUnavailable)
		}
		ids = summonInsertPlayerID(s, ids, targetID)
		return ids, ids, dest.Resource.BeenHere + 1, false, nil
	}
	src, found := removeString(source.PlayerIDs, targetID)
	if !found {
		return nil, nil, 0, false, fmt.Errorf("%w: summon target occupancy is not migrated", ErrCastSpellUnavailable)
	}
	if containsString(dest.PlayerIDs, targetID) {
		return nil, nil, 0, false, fmt.Errorf("%w: summon target occupies both rooms", ErrCastSpellUnavailable)
	}
	if len(src) == 0 && len(source.NPCIDs) > 0 && s.ActiveNPCIDs == nil {
		return nil, nil, 0, false, fmt.Errorf("%w: summon source NPC active list is not migrated", ErrCastSpellUnavailable)
	}
	dst := summonInsertPlayerID(s, dest.PlayerIDs, targetID)
	return src, dst, dest.Resource.BeenHere + 1, len(src) == 0 && len(source.NPCIDs) > 0, nil
}

func (s State) summonArrivalScene(targetID string, destID, sourceID int16, srcIDs, dstIDs []string, beenHere int32, hour int) (string, error) {
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
	target := next.Players[targetID]
	target.Body.RoomID = destID
	next.Players[targetID] = target
	scene, err := next.locateScene(targetID, destID, hour)
	if err != nil {
		return "", err
	}
	return scene, nil
}

func summonRoomGate(dest RoomState, target PlayerState, vis int) string {
	flags := dest.Resource.Flags[:]
	if flag(flags, summonNoTeleportFlag) ||
		(flag(flags, summonOnePlayerFlag) && vis > 0) ||
		(flag(flags, summonTwoPlayerFlag) && vis > 1) ||
		(flag(flags, summonThreePlayerFlag) && vis > 2) {
		return summonSuckedResponse
	}
	if flag(flags, summonFamilyRoomFlag) && !flag(target.Body.Flags[:], summonFamilyFlag) {
		return summonFamilyResponse
	}
	if flag(flags, summonOneFamilyFlag) && int16(target.Body.Daily[summonFamilyDaily].Max) != dest.Resource.Special {
		return summonOnlyFamilyResponse
	}
	if dest.Resource.LowLevel > target.Body.Level ||
		(target.Body.Level > dest.Resource.HighLevel && dest.Resource.HighLevel != 0) ||
		flag(target.Body.Flags[:], summonNoSummonFlag) {
		if flag(target.Body.Flags[:], summonNoSummonFlag) {
			return summonRefuseResponse
		}
		return summonLevelResponse
	}
	return ""
}

func (s State) planSummonCast(p CastProposal, actor PlayerState, room RoomState, spec castSpellSpec, options CastOptions) (CastProposal, error) {
	if spec.Index != castSummonSpell || spec.healKind != castHealSummon {
		return CastProposal{}, ErrCastInvalidProposal
	}
	p.TargetName = options.Target
	p.Cost = summonManaCost(actor.Body.Class)
	if !legacyInfoFlag(actor.Body.Spells[:], uint(castSummonSpell)) {
		p.Response = summonUnlearnedResponse
		return p, nil
	}
	if actor.Body.MPCurrent < p.Cost {
		p.Response = summonManaResponse
		return p, nil
	}
	roll, err := castRoll(options, 1, 100, nil)
	if err != nil {
		return CastProposal{}, err
	}
	p.Attempted, p.Chance, p.Roll = true, summonFailBelow-1, roll
	if summonAttemptFailed(roll) {
		p.SpellFailed = true
		p.Changed = true
		p.Response = summonFailResponse
		p.afterBody, p.MPDelta = summonApplyBody(actor.Body, summonFailCost, 0, 0, false)
		return p, nil
	}
	if options.Target == "" {
		p.Response = summonSelfResponse
		return p, nil
	}
	targetID, target, err := s.summonFindWho(p.ActorID, options.Target)
	if err != nil {
		return CastProposal{}, err
	}
	if targetID == "" || flag(target.Body.Flags[:], summonDMInvisibleFlag) {
		p.Response = summonMissingResponse
		return p, nil
	}
	if target.Body.Stats[3] > 63 || target.Body.Class > maxLegacyClass {
		return CastProposal{}, fmt.Errorf("%w: summon target stats/class outside legacy range", ErrCastSpellUnavailable)
	}
	sourceRoom, ok := s.Rooms[target.Body.RoomID]
	if !ok {
		return CastProposal{}, fmt.Errorf("%w: summon target room is not migrated", ErrCastSpellUnavailable)
	}
	if !containsString(sourceRoom.PlayerIDs, targetID) {
		return CastProposal{}, fmt.Errorf("%w: summon target occupancy is not migrated", ErrCastSpellUnavailable)
	}
	if summonRefreshPending(room.Resource, options.Now) {
		return CastProposal{}, fmt.Errorf("%w: summon destination spawn refresh is not migrated", ErrCastSpellUnavailable)
	}
	p.TargetID = targetID
	p.expectedTarget = cloneDrinkActor(target)
	p.expectedTargetSet = true
	p.sourceRoomID = target.Body.RoomID
	p.targetRoomID = target.Body.RoomID
	p.expectedTargetRoomFlags = sourceRoom.Resource.Flags
	p.expectedSourceIDs = append([]string(nil), sourceRoom.PlayerIDs...)
	p.expectedDestIDs = append([]string(nil), room.PlayerIDs...)
	p.Changed = true
	charged, mpDelta := summonApplyBody(actor.Body, p.Cost, 0, 0, false)
	if flag(sourceRoom.Resource.Flags[:], summonNoLeaveFlag) {
		p.afterBody, p.MPDelta = charged, mpDelta
		p.Response = summonLeaveText(target.Body.Name)
		return p, nil
	}
	if gate := summonRoomGate(room, target, summonCountVis(s, room)); gate != "" {
		p.afterBody, p.MPDelta = charged, mpDelta
		p.Response = gate
		return p, nil
	}
	srcIDs, dstIDs, beenHere, deactivate, err := s.summonPlanOccupancy(p.RoomID, p.sourceRoomID, targetID)
	if err != nil {
		return CastProposal{}, err
	}
	scene, err := s.summonArrivalScene(targetID, p.RoomID, p.sourceRoomID, srcIDs, dstIDs, beenHere, options.Hour)
	if err != nil {
		return CastProposal{}, err
	}
	interval := castSpellInterval(actor.Body.Class)
	p.afterBody, p.MPDelta = summonApplyBody(actor.Body, p.Cost, options.Now, interval, true)
	p.Succeeded = true
	p.Broadcast = true
	p.SpellInterval = interval
	p.afterSourceIDs = srcIDs
	p.afterDestIDs = dstIDs
	p.afterDestBeenHere = beenHere
	p.deactivateSource = deactivate
	if deactivate {
		probe := s.clone()
		source := probe.Rooms[p.sourceRoomID]
		source.PlayerIDs = append([]string(nil), srcIDs...)
		probe.Rooms[p.sourceRoomID] = source
		probe.deactivateRoomNPCs(p.sourceRoomID)
		p.afterActiveNPCIDs = append([]string{}, probe.ActiveNPCIDs...)
		p.afterActiveSet = true
	}
	p.RoomText = summonRoomText(actor.Body.Name, target.Body.Name)
	p.Response = summonCasterText(target.Body.Name)
	p.TargetText = summonTargetText(actor.Body.Name, scene)
	return p, nil
}

func summonNoopFieldsInvalid(p CastProposal) bool {
	return p.Broadcast || p.Succeeded || p.SpellFailed || p.EffectRolls != nil || p.HPDelta != 0 || p.MPDelta != 0 || p.SpellInterval != 0 || p.TimedFlag != 0 || p.TimedInterval != 0 || p.CursedItemsCleared != 0 || p.afterItemsSet || p.LocateLinked || p.TargetText != "" || p.RoomText != "" || p.deactivateSource || p.afterActiveSet || p.afterDestBeenHere != 0 || p.afterSourceIDs != nil || p.afterDestIDs != nil
}

func (s State) applySummonCast(p CastProposal, actor PlayerState, room RoomState, spec castSpellSpec) (State, CastResult, error) {
	if p.Hour < 0 || spec.healKind != castHealSummon || spec.Index != castSummonSpell || p.HealKind != castHealSummon || p.SpellIndex != castSummonSpell || p.Cost != summonManaCost(actor.Body.Class) {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}
	if !p.Attempted {
		if summonNoopFieldsInvalid(p) || p.TargetID != "" || p.expectedTargetSet || p.Chance != 0 || p.Roll != 0 {
			return State{}, CastResult{}, ErrCastInvalidProposal
		}
		if !legacyInfoFlag(actor.Body.Spells[:], uint(castSummonSpell)) {
			if p.Response != summonUnlearnedResponse {
				return State{}, CastResult{}, ErrCastInvalidProposal
			}
		} else if actor.Body.MPCurrent < p.Cost {
			if p.Response != summonManaResponse {
				return State{}, CastResult{}, ErrCastInvalidProposal
			}
		} else {
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

	if p.Chance != summonFailBelow-1 || p.Roll < 1 || p.Roll > 100 || p.EffectRolls != nil || p.HPDelta != 0 || p.TimedFlag != 0 || p.TimedInterval != 0 || p.CursedItemsCleared != 0 || p.afterItemsSet || p.LocateLinked {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}
	if p.HiddenCleared != flag(actor.Body.Flags[:], castHiddenFlag) {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}
	if summonAttemptFailed(p.Roll) {
		if !p.SpellFailed || p.Succeeded || p.Broadcast || p.TargetID != "" || p.TargetText != "" || p.RoomText != "" || p.SpellInterval != 0 || p.Response != summonFailResponse || p.expectedTargetSet {
			return State{}, CastResult{}, ErrCastInvalidProposal
		}
		after, mpDelta := summonApplyBody(actor.Body, summonFailCost, 0, 0, false)
		if mpDelta != p.MPDelta || !reflect.DeepEqual(after, p.afterBody) {
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

	if p.SpellFailed {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}

	if !p.Succeeded {
		if p.Broadcast || p.TargetText != "" || p.RoomText != "" || p.SpellInterval != 0 || p.deactivateSource || p.afterActiveSet || p.afterDestBeenHere != 0 || p.afterSourceIDs != nil || p.afterDestIDs != nil {
			return State{}, CastResult{}, ErrCastInvalidProposal
		}
		if p.TargetID == "" {
			if p.MPDelta != 0 || p.expectedTargetSet {
				return State{}, CastResult{}, ErrCastInvalidProposal
			}
			want := summonSelfResponse
			if p.TargetName != "" {
				want = summonMissingResponse
			}
			if p.Response != want {
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
		if !p.expectedTargetSet || !p.Changed {
			return State{}, CastResult{}, ErrCastInvalidProposal
		}
		target, ok := s.Players[p.TargetID]
		if !ok || !p.expectedTargetSet || !reflect.DeepEqual(target, p.expectedTarget) {
			return State{}, CastResult{}, ErrCastStaleProposal
		}
		sourceRoom, ok := s.Rooms[p.sourceRoomID]
		if !ok || p.sourceRoomID != target.Body.RoomID || sourceRoom.Resource.Flags != p.expectedTargetRoomFlags {
			return State{}, CastResult{}, ErrCastStaleProposal
		}
		if !summonIDsEqual(sourceRoom.PlayerIDs, p.expectedSourceIDs) || !summonIDsEqual(room.PlayerIDs, p.expectedDestIDs) {
			return State{}, CastResult{}, ErrCastStaleProposal
		}
		after, mpDelta := summonApplyBody(actor.Body, p.Cost, 0, 0, false)
		if mpDelta != p.MPDelta || !reflect.DeepEqual(after, p.afterBody) {
			return State{}, CastResult{}, ErrCastInvalidProposal
		}
		want := ""
		if flag(sourceRoom.Resource.Flags[:], summonNoLeaveFlag) {
			want = summonLeaveText(target.Body.Name)
		} else {
			want = summonRoomGate(room, target, summonCountVis(s, room))
		}
		if want == "" || p.Response != want {
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

	if !p.Changed || !p.Broadcast || p.TargetID == "" || !p.expectedTargetSet || p.SpellInterval != castSpellInterval(actor.Body.Class) {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}
	target, ok := s.Players[p.TargetID]
	if !ok || !reflect.DeepEqual(target, p.expectedTarget) {
		return State{}, CastResult{}, ErrCastStaleProposal
	}
	sourceRoom, ok := s.Rooms[p.sourceRoomID]
	if !ok || p.sourceRoomID != target.Body.RoomID || sourceRoom.Resource.Flags != p.expectedTargetRoomFlags {
		return State{}, CastResult{}, ErrCastStaleProposal
	}
	if !summonIDsEqual(sourceRoom.PlayerIDs, p.expectedSourceIDs) || !summonIDsEqual(room.PlayerIDs, p.expectedDestIDs) {
		return State{}, CastResult{}, ErrCastStaleProposal
	}
	if flag(sourceRoom.Resource.Flags[:], summonNoLeaveFlag) || summonRoomGate(room, target, summonCountVis(s, room)) != "" {
		return State{}, CastResult{}, ErrCastStaleProposal
	}
	if summonRefreshPending(room.Resource, p.Now) {
		return State{}, CastResult{}, ErrCastStaleProposal
	}
	srcIDs, dstIDs, beenHere, deactivate, err := s.summonPlanOccupancy(p.RoomID, p.sourceRoomID, p.TargetID)
	if err != nil || deactivate != p.deactivateSource || beenHere != p.afterDestBeenHere {
		return State{}, CastResult{}, ErrCastStaleProposal
	}
	if !summonIDsEqual(srcIDs, p.afterSourceIDs) || !summonIDsEqual(dstIDs, p.afterDestIDs) {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}
	scene, err := s.summonArrivalScene(p.TargetID, p.RoomID, p.sourceRoomID, srcIDs, dstIDs, beenHere, p.Hour)
	if err != nil {
		return State{}, CastResult{}, ErrCastStaleProposal
	}
	if p.RoomText != summonRoomText(actor.Body.Name, target.Body.Name) || p.Response != summonCasterText(target.Body.Name) || p.TargetText != summonTargetText(actor.Body.Name, scene) {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}
	after, mpDelta := summonApplyBody(actor.Body, p.Cost, p.Now, p.SpellInterval, true)
	if mpDelta != p.MPDelta || !reflect.DeepEqual(after, p.afterBody) {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}
	next := s.clone()
	nextActor := next.Players[p.ActorID]
	nextActor.Body = after
	next.Players[p.ActorID] = nextActor
	dest := next.Rooms[p.RoomID]
	dest.PlayerIDs = append([]string(nil), dstIDs...)
	dest.Resource.BeenHere = beenHere
	next.Rooms[p.RoomID] = dest
	if p.sourceRoomID != p.RoomID {
		source := next.Rooms[p.sourceRoomID]
		source.PlayerIDs = append([]string(nil), srcIDs...)
		next.Rooms[p.sourceRoomID] = source
	}
	nextTarget := next.Players[p.TargetID]
	nextTarget.Body.RoomID = p.RoomID
	next.Players[p.TargetID] = nextTarget
	if deactivate {
		next.deactivateRoomNPCs(p.sourceRoomID)
		if !p.afterActiveSet || !summonIDsEqual(next.ActiveNPCIDs, p.afterActiveNPCIDs) {
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
