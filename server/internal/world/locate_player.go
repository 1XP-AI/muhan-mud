package world

// locate_player.go is magic7.c:locate_player (SLOCAT / 천리안) on the existing
// CommandCast path. C gates stay fail-closed: learned bit, MP 15, a third
// token, find_who, PDMINV/PINVIS/PDINVI, DM class, PMARRI, then spell_fail
// (which still consumes MP). Vision and target-notify rolls are recorded on
// the proposal so ApplyCast/replay never consult the host RNG.

import (
	"fmt"
	"math"
	"reflect"
	"unicode"
	"unicode/utf8"
)

const (
	locatePlayerCost          int16 = 15
	locateInvisibleFlag             = 2  // PINVIS
	locateDMInvisibleFlag           = 10 // PDMINV
	locateDetectInvisibleFlag       = 21 // PDINVI
	locateMarriedFlag               = 60 // PMARRI
	locateMageClass                 = 5  // MAGE
	locateSubDMClass                = 11 // SUB_DM
	locateDMClass                   = 12 // DM
	locateChanceCap                 = 85
	locateLightFlag                 = 17 // PLIGHT
	locateLightObjectFlag           = 11
	locateLightObjectType           = 12
)

const (
	locateUnlearnedResponse = "당신은 아직 그런 주문을 터득하지 못했습니다.\r\n"
	locateManaResponse      = "당신의 도력이 부족합니다.\r\n"
	locatePromptResponse    = "누구와 연결합니까?\r\n"
	locateMissingResponse   = "그런 사람은 존재하지 않습니다.\r\n"
	locateDMResponse        = "그 사람의 정신력이 너무 높아 투시를 할 수 없습니다.\r\n"
	locateMarriedResponse   = "그 사람의 사생활은 엿볼 수가 없습니다.\r\n"
	locateUnlinkedResponse  = "당신의 정신은 연결될수 없습니다.\r\n"
)

// IsLocateCastSpell reports whether the token uniquely resolves to SLOCAT.
func IsLocateCastSpell(name string) bool {
	spec, err := castSpellSpecFor(name)
	return err == nil && spec.Index == castLocateSpell && spec.healKind == castHealLocate
}

func locateQueryOK(name string) bool {
	if name == "" || !utf8.ValidString(name) {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

func locateLookupName(name string) string {
	canonical := []byte(name)
	for i, b := range canonical {
		if b >= 'A' && b <= 'Z' {
			canonical[i] = b + ('a' - 'A')
		}
	}
	if len(canonical) > 0 && canonical[0] >= 'a' && canonical[0] <= 'z' {
		canonical[0] -= 'a' - 'A'
	}
	return string(canonical)
}

type locateResolved struct {
	targetID string
	target   PlayerState
	room     RoomState
	scene    string
}

func (s State) locateFindWho(query string) (string, PlayerState, error) {
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
		return "", PlayerState{}, fmt.Errorf("%w: locate target name is ambiguous", ErrCastSpellUnavailable)
	}
	return matches[0], s.Players[matches[0]], nil
}

func locateHiddenFrom(actor, target PlayerState) bool {
	if flag(target.Body.Flags[:], locateDMInvisibleFlag) {
		return true
	}
	return flag(target.Body.Flags[:], locateInvisibleFlag) && !flag(actor.Body.Flags[:], locateDetectInvisibleFlag)
}

func (s State) locatePreflight(actor PlayerState, actorID, targetQuery string, hour int) (string, locateResolved, error) {
	if !legacyInfoFlag(actor.Body.Spells[:], uint(castLocateSpell)) {
		return locateUnlearnedResponse, locateResolved{}, nil
	}
	if actor.Body.MPCurrent < locatePlayerCost {
		return locateManaResponse, locateResolved{}, nil
	}
	if targetQuery == "" {
		return locatePromptResponse, locateResolved{}, nil
	}
	targetID, target, err := s.locateFindWho(targetQuery)
	if err != nil {
		return "", locateResolved{}, err
	}
	if targetID == "" || locateHiddenFrom(actor, target) {
		return locateMissingResponse, locateResolved{}, nil
	}
	if target.Body.Class == locateDMClass && actor.Body.Class != locateDMClass {
		return locateDMResponse, locateResolved{}, nil
	}
	if flag(target.Body.Flags[:], locateMarriedFlag) {
		return locateMarriedResponse, locateResolved{}, nil
	}
	if target.Body.Stats[3] > 63 || target.Body.Class > maxLegacyClass {
		return "", locateResolved{}, fmt.Errorf("%w: locate target stats/class outside legacy range", ErrCastSpellUnavailable)
	}
	room, ok := s.Rooms[target.Body.RoomID]
	if !ok {
		return "", locateResolved{}, fmt.Errorf("%w: locate target room is not migrated", ErrCastSpellUnavailable)
	}
	scene, err := s.locateScene(actorID, target.Body.RoomID, hour)
	if err != nil {
		return "", locateResolved{}, err
	}
	return "", locateResolved{targetID: targetID, target: target, room: room, scene: scene}, nil
}

func locateHasLight(player PlayerState) (bool, error) {
	if flag(player.Body.Flags[:], locateLightFlag) {
		return true, nil
	}
	if player.Items == nil {
		return false, fmt.Errorf("%w: locate viewer equipment not migrated", ErrCastSpellUnavailable)
	}
	for _, id := range player.Items.Ready {
		if id == "" {
			continue
		}
		object := player.Items.Items[id].Object
		if flag(object.Flags[:], locateLightObjectFlag) && (object.Type != locateLightObjectType || object.ShotsCurrent > 0) {
			return true, nil
		}
	}
	return false, nil
}

func (s State) locateScene(viewerID string, roomID int16, hour int) (string, error) {
	viewer, ok := s.Players[viewerID]
	if !ok || !viewer.Online {
		return "", fmt.Errorf("%w: locate viewer absent", ErrCastSpellUnavailable)
	}
	room, ok := s.Rooms[roomID]
	if !ok {
		return "", fmt.Errorf("%w: locate target room is not migrated", ErrCastSpellUnavailable)
	}
	resource, err := s.ProjectRoom(roomID)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrCastSpellUnavailable, err)
	}
	room.Resource = resource
	if room.Items != nil {
		objects, err := room.Items.LegacyInventory()
		if err != nil {
			return "", fmt.Errorf("%w: %v", ErrCastSpellUnavailable, err)
		}
		room.Resource.Objects = objects
	}
	flags := viewer.Body.Flags[:]
	base := ViewOptions{Hour: hour, Race: viewer.Body.Race, Class: viewer.Body.Class, Blind: flag(flags, castBlindFlag)}
	light, otherLight := false, false
	if !base.Blind && !roomVisible(room.Resource, base) {
		var unresolved error
		ids := append([]string{viewerID}, room.PlayerIDs...)
		for i, id := range ids {
			if i > 0 && id == viewerID {
				continue
			}
			occupant, present := s.Players[id]
			if !present {
				continue
			}
			lit, lightErr := locateHasLight(occupant)
			if lightErr != nil {
				unresolved = lightErr
				continue
			}
			if lit {
				light, otherLight = id == viewerID, id != viewerID
				break
			}
		}
		if !light && !otherLight && unresolved != nil {
			return "", unresolved
		}
	}
	views, err := s.RoomPlayers(roomID)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrCastSpellUnavailable, err)
	}
	options := SceneOptions{
		ViewOptions: ViewOptions{
			Hour: hour, Race: viewer.Body.Race, Class: viewer.Body.Class, Blind: flag(flags, castBlindFlag),
			HasLight: light, OtherPlayerLight: otherLight, HideName: flag(flags, 6), HideShort: flag(flags, 5),
			HideLong: flag(flags, 4), ExitDiagram: flag(flags, 7), DetectInvisible: flag(flags, locateDetectInvisibleFlag),
		},
		ViewerID: viewerID, KnowAlignment: flag(flags, 33), DetectMagic: flag(flags, 20), PlayerDescriptions: flag(flags, 63),
	}
	return RenderRoomScene(room.Resource, views, options), nil
}

func locateLevelBand(level byte) int {
	return (int(level) + 3) / 4
}

func locateCapChance(chance int) int {
	if chance > locateChanceCap {
		return locateChanceCap
	}
	return chance
}

func locateVisionChance(actor, target LegacyMonster) int {
	chance := 50 + (locateLevelBand(actor.Level)-locateLevelBand(target.Level))*5 +
		(legacyStatBonus[actor.Stats[3]]-legacyStatBonus[target.Stats[3]])*5
	if actor.Class == locateMageClass {
		chance += 5
	}
	return locateCapChance(chance)
}

func locateNotifyChance(actor, target LegacyMonster, linked bool) int {
	if linked {
		chance := 60 + (locateLevelBand(target.Level)-locateLevelBand(actor.Level))*5 +
			(legacyStatBonus[target.Stats[3]]-legacyStatBonus[actor.Stats[3]])*5
		if target.Class == locateMageClass {
			chance += 5
		}
		return locateCapChance(chance)
	}
	return 65 + (locateLevelBand(target.Level)-locateLevelBand(actor.Level))*5 +
		(legacyStatBonus[target.Stats[3]]-legacyStatBonus[actor.Stats[3]])*5
}

func locateFocusResponse(name string) string {
	return fmt.Sprintf("당신의 마음을 %s에게 집중했습니다.\r\n", name)
}

func locateRoomText(name string) string {
	return fmt.Sprintf("\n%s이 천리안 주문을 외웠습니다.\r\n", name)
}

func locateNotifyLinkedText(name string) string {
	return fmt.Sprintf("\n%s이 당신의 눈으로 주위를 보고 있습니다.\r\n", name)
}

func locateNotifyMissText(name string) string {
	return fmt.Sprintf("\n%s이 당신의 눈으로 보려합니다.\r\n", name)
}

func locateSpellFailed(chance, roll int) bool {
	if chance == math.MaxInt {
		return false
	}
	return roll > chance
}

func (s State) planLocateCast(p CastProposal, actor PlayerState, room RoomState, spec castSpellSpec, options CastOptions) (CastProposal, error) {
	if options.Hour < 0 {
		return CastProposal{}, fmt.Errorf("cast hour must be nonnegative")
	}
	p.Hour = options.Hour
	p.TargetName = options.Target
	noop, resolved, err := s.locatePreflight(actor, p.ActorID, options.Target, options.Hour)
	if err != nil {
		return CastProposal{}, err
	}
	if noop != "" {
		p.Response = noop
		return p, nil
	}
	p.TargetID = resolved.targetID
	p.expectedTarget = cloneDrinkActor(resolved.target)
	p.expectedTargetSet = true
	p.targetRoomID = resolved.target.Body.RoomID
	p.expectedTargetRoomFlags = resolved.room.Resource.Flags

	chance, err := castSpellChance(actor.Body)
	if err != nil {
		return CastProposal{}, err
	}
	roll, err := castRoll(options, 1, 100, nil)
	if err != nil {
		return CastProposal{}, err
	}
	p.Attempted, p.Chance, p.Roll, p.Changed = true, chance, roll, true
	p.afterBody = actor.Body
	if locateSpellFailed(chance, roll) {
		p.SpellFailed = true
		p.Response = castResponse(spec, true, "")
		p.afterBody, p.MPDelta, err = castBodyAfter(actor.Body, room, spec, CastOptions{}, nil, false, true)
		if err != nil {
			return CastProposal{}, err
		}
		return p, nil
	}

	effectRolls := make([]int, 0, 2)
	linked := false
	if resolved.target.Body.Class < locateSubDMClass {
		locateRoll, rollErr := castRoll(options, 1, 100, &effectRolls)
		if rollErr != nil {
			return CastProposal{}, rollErr
		}
		linked = locateRoll < locateVisionChance(actor.Body, resolved.target.Body)
	}
	notifyRoll, err := castRoll(options, 1, 100, &effectRolls)
	if err != nil {
		return CastProposal{}, err
	}
	p.EffectRolls = effectRolls
	p.Succeeded = true
	p.LocateLinked = linked
	p.SpellInterval = castSpellInterval(actor.Body.Class)
	p.afterBody, p.HPDelta, err = castBodyAfter(actor.Body, room, spec, options, nil, false, false)
	if err != nil {
		return CastProposal{}, err
	}
	p.afterBody.Timers[castSpellTimerIndex] = LegacyTimer{LastTime: options.Now, Interval: p.SpellInterval}
	p.MPDelta = int32(p.afterBody.MPCurrent) - int32(actor.Body.MPCurrent)
	p.Broadcast = true
	p.RoomText = locateRoomText(actor.Body.Name)
	p.Response = locateFocusResponse(resolved.target.Body.Name)
	if linked {
		p.Response += resolved.scene
	} else {
		p.Response += locateUnlinkedResponse
	}
	if notifyRoll < locateNotifyChance(actor.Body, resolved.target.Body, linked) {
		if linked {
			p.TargetText = locateNotifyLinkedText(actor.Body.Name)
		} else {
			p.TargetText = locateNotifyMissText(actor.Body.Name)
		}
	}
	return p, nil
}

func (s State) applyLocateCast(p CastProposal, actor PlayerState, room RoomState, spec castSpellSpec) (State, CastResult, error) {
	if p.Hour < 0 || p.Cost != locatePlayerCost || p.HealKind != castHealLocate || p.SpellIndex != castLocateSpell {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}
	if !p.Attempted {
		if p.Broadcast || p.Succeeded || p.SpellFailed || p.EffectRolls != nil || p.HPDelta != 0 || p.MPDelta != 0 || p.SpellInterval != 0 || p.TimedFlag != 0 || p.TimedInterval != 0 || p.CursedItemsCleared != 0 || p.afterItemsSet || p.LocateLinked || p.TargetID != "" || p.TargetText != "" || p.expectedTargetSet || p.RoomText != "" {
			return State{}, CastResult{}, ErrCastInvalidProposal
		}
		noop, _, err := s.locatePreflight(actor, p.ActorID, p.TargetName, p.Hour)
		if err != nil || noop == "" || p.Response != noop {
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

	noop, resolved, err := s.locatePreflight(actor, p.ActorID, p.TargetName, p.Hour)
	if err != nil || noop != "" || resolved.targetID != p.TargetID || !p.expectedTargetSet {
		return State{}, CastResult{}, ErrCastStaleProposal
	}
	if !reflect.DeepEqual(resolved.target, p.expectedTarget) || p.targetRoomID != resolved.target.Body.RoomID || resolved.room.Resource.Flags != p.expectedTargetRoomFlags {
		return State{}, CastResult{}, ErrCastStaleProposal
	}
	if !p.Changed || p.Broadcast != p.Succeeded || p.Succeeded == p.SpellFailed || p.TimedFlag != 0 || p.TimedInterval != 0 || p.CursedItemsCleared != 0 || p.afterItemsSet || p.HPDelta != 0 {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}
	if p.HiddenCleared != flag(actor.Body.Flags[:], castHiddenFlag) {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}
	chance, err := castSpellChance(actor.Body)
	if err != nil || chance != p.Chance || p.Roll < 1 || p.Roll > 100 {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}
	if locateSpellFailed(chance, p.Roll) {
		if !p.SpellFailed || p.LocateLinked || p.TargetText != "" || p.EffectRolls != nil || p.SpellInterval != 0 || p.Broadcast || p.RoomText != "" || p.Response != castResponse(spec, true, "") {
			return State{}, CastResult{}, ErrCastInvalidProposal
		}
		after, mpDelta, err := castBodyAfter(actor.Body, room, spec, CastOptions{}, nil, false, true)
		if err != nil || mpDelta != p.MPDelta || !reflect.DeepEqual(after, p.afterBody) {
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

	at := 0
	linked := false
	if resolved.target.Body.Class < locateSubDMClass {
		locateRoll, rollErr := castReplayRoll(p.EffectRolls, &at, 1, 100)
		if rollErr != nil {
			return State{}, CastResult{}, ErrCastInvalidProposal
		}
		linked = locateRoll < locateVisionChance(actor.Body, resolved.target.Body)
	}
	notifyRoll, err := castReplayRoll(p.EffectRolls, &at, 1, 100)
	if err != nil || at != len(p.EffectRolls) {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}
	if p.LocateLinked != linked || p.SpellInterval != castSpellInterval(actor.Body.Class) || p.RoomText != locateRoomText(actor.Body.Name) {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}
	wantResponse := locateFocusResponse(resolved.target.Body.Name)
	if linked {
		wantResponse += resolved.scene
	} else {
		wantResponse += locateUnlinkedResponse
	}
	if p.Response != wantResponse {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}
	wantNotify := ""
	if notifyRoll < locateNotifyChance(actor.Body, resolved.target.Body, linked) {
		if linked {
			wantNotify = locateNotifyLinkedText(actor.Body.Name)
		} else {
			wantNotify = locateNotifyMissText(actor.Body.Name)
		}
	}
	if p.TargetText != wantNotify {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}
	after, hpDelta, err := castBodyAfter(actor.Body, room, spec, CastOptions{}, nil, false, false)
	if err != nil || hpDelta != p.HPDelta {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}
	after.Timers[castSpellTimerIndex] = LegacyTimer{LastTime: p.Now, Interval: p.SpellInterval}
	if int32(after.MPCurrent)-int32(actor.Body.MPCurrent) != p.MPDelta || !reflect.DeepEqual(after, p.afterBody) {
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
