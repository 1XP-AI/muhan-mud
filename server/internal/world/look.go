package world

import (
	"errors"
	"fmt"
	"strings"
)

// command2.c:look target branch. Bare 봐/보다/조사 stay on CurrentScene,
// including display_rom first_enm combat notices. An exit token is find_ext
// then, if open, display_rom of the destination. After find_ext misses,
// find_obj walks ply first_obj, then ready[], then room first_obj.
// Exact 나 is self-inspect (거울) then PLAYER PMARRI marriage
// ("%s는 %s님과 결혼한 기혼자입니다." with PMALES 그/그녀 and key[2][1]
// spouse name), PLAYER standing ("%s는 %s서 있습니다." with PMALES
// 그/그녀 and description), PKNOWA glow, first_mon HP bands, consider,
// and equip_list. Else find_crt(first_mon) inspects with the same PKNOWA
// glow (viewer PKNOWA && target alignment!=0; alignment<0 붉은 else
// 푸른; MMALES 그/그녀), HP bands (hpcur vs hpmax 9/10,8/10,6/10,4/10,
// 2/10, MMALES 그/그녀), is_enm_crt angry + first_enm combat
// (MMALES 그/그녀; viewer name → "당신과 싸우고"; else raw %S%j
// 2=과/와), consider, and equip_list. Then upcase str[1][0]
// and find_crt(first_ply) inspects with the same PMARRI marriage then
// PLAYER standing then PKNOWA glow (viewer PKNOWA && alignment in
// [-100,100]; always 푸른 광채; MMALES 그/그녀) then hpcur < hpmax*3/10
// 가벼운 상처 (PMALES 그/그녀; not first_mon 9/10–2/10 bands) then
// equip_list without is_enm_crt/first_enm. Self/first_mon/first_ply inspect fans out C
// broadcast_rom on first commit: self and first_mon name-prefix
// "\n%M%j 거울을 들고 자신을 바라 봅니다.", else first_mon/first_ply
// "\n%M%j %M%j 봅니다." (%M is io.c crt_str per occupant: INV from
// viewer PDINVI; PINVIS without PDINVI is 누군가; PDINVI+PINVIS may
// add (*); Type==0 then 님. %j 1=이/가, 3=을/를 on the rendered name).
// The actor still receives inspect text and is excluded from the
// room event. Unmigrated room occupants fail closed. board/special_obj
// remain fail-closed. Unmigrated ready[], nil Items, hpmax<=0 on
// self/first_mon/first_ply, nil NPC Enemies on first_mon inspect,
// unmigrated first_enm names, PMARRI without a canonical m-prefix spouse,
// or empty/unmigrated self or first_ply description fail closed.
const (
	lookClosedExitFlag      = 3  // XCLOSD
	lookDetectInvisibleFlag = 21 // PDINVI
	lookRoomNoFamilyFlag    = 38 // RONFML
	lookRoomNoMarryFlag     = 40 // RONMAR
	lookBlindFlag           = 42 // PBLIND
	lookMaleFlag            = 12 // MMALES/PMALES
	lookKnowAlignmentFlag   = 33 // PKNOWA

	lookObjectSharp       = 0  // SHARP
	lookObjectThrust      = 1  // THRUST
	lookObjectBlunt       = 2  // BLUNT
	lookObjectPole        = 3  // POLE
	lookObjectMissile     = 4  // MISSILE
	lookObjectArmor       = 5  // ARMOR
	lookObjectWand        = 8  // WAND
	lookObjectKey         = 11 // KEY
	lookObjectLightSource = 12 // LIGHTSOURCE

	LookBlindResponse         = "당신은 눈이 멀어 있습니다!\r\n"
	LookClosedResponse        = "그 출구는 닫혀 있습니다.\r\n"
	LookNoMapResponse         = "지도가 없습니다.\r\n"
	LookHiddenResponse        = "그 방은 볼 수가 없습니다.\r\n"
	LookObjectPlainResponse   = "특별한 점이 없습니다.\r\n"
	LookCreaturePlainResponse = "특별한 것은 보이지 않습니다.\r\n"
	LookSelfMirrorResponse    = "당신은 거울을 들고 자신을 봅니다.\r\n"
	LookObjectBrokenResponse  = "그것은 부서져 버렸거나 다 써버렸습니다.\r\n"
	LookObjectWornResponse    = "그것은 곧 부서질것 같습니다.\r\n"
)

var (
	ErrLookActorAbsent              = errors.New("online look actor absent")
	ErrLookExitUnresolved           = errors.New("look exit unresolved")
	ErrLookDestinationUnresolved    = errors.New("look destination unresolved")
	ErrLookStaleProposal            = errors.New("stale or invalid look proposal")
	ErrLookInvalidProposal          = errors.New("invalid look proposal")
	ErrLookObjectCreatureUnmigrated = errors.New("look object or creature inspection unmigrated")
	errLookCreatureAbsent           = errors.New("look creature absent")
)

const (
	lookModeHere     = "here"
	lookModeBlind    = "blind"
	lookModeClosed   = "closed"
	lookModeNoMap    = "nomap"
	lookModeHidden   = "hidden"
	lookModePeek     = "peek"
	lookModeObject   = "object"
	lookModeCreature = "creature"
	lookModeSelf     = "self"
	lookModePlayer   = "player"
)

// LookProposal is one command2.c:look candidate against one snapshot.
// Peeking a destination or inspecting an inventory, ready, room
// object/creature, self (나), or first_ply does not move the actor.
// check_exits remains the room-entry RefreshDoors path; look() itself
// does not re-close doors.
type LookProposal struct {
	ActorID       string
	RoomID        int16
	Hour          int
	Prefix        string
	Occurrence    int
	ExitIndex     int
	ExitName      string
	DestRoomID    int16
	TargetKind    string
	TargetID      string
	TargetName    string
	Response      string
	Mode          string
	Changed       bool
	Broadcast     bool
	BroadcastText string
}

// LookResult is the durable receipt projection for 봐/보다/조사.
type LookResult struct {
	Response      string `json:"response"`
	Mode          string `json:"mode,omitempty"`
	ExitName      string `json:"exit_name,omitempty"`
	DestID        int16  `json:"dest_id,omitempty"`
	TargetKind    string `json:"target_kind,omitempty"`
	TargetID      string `json:"target_id,omitempty"`
	TargetName    string `json:"target_name,omitempty"`
	Broadcast     bool   `json:"broadcast,omitempty"`
	BroadcastText string `json:"broadcast_text,omitempty"`
}

// LookInspectEvent is command2.c:look's inspect broadcast_rom projection.
// The actor already has the durable inspect response, so transport excludes
// ExcludeActorID. Replay never publishes this event. Text is the INV|DMF
// snapshot used for stale checks; TextFor renders C io.c %M / crt_str for
// one occupant (PINVIS without PDINVI → 누군가, PDINVI+PINVIS → (*)).
type LookInspectEvent struct {
	RoomID         int16
	ActorID        string
	ExcludeActorID string
	Text           string
	Mode           string
	Actor          LegacyMonster
	Target         LegacyMonster
}

// TextFor is C print()+crt_str for one occupant. PINVIS without the
// viewer's PDINVI renders 누군가; PDINVI seeing PINVIS may add (*).
func (e LookInspectEvent) TextFor(viewer LegacyMonster) string {
	return lookInspectBroadcastTextFor(e.Actor, e.Mode, e.Target, viewer)
}

func (s State) PlanLook(actorID, prefix string, occurrence int, hour int) (LookProposal, error) {
	if err := s.Validate(); err != nil {
		return LookProposal{}, err
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Body.Type != 0 || actor.Body.Name == "" {
		return LookProposal{}, ErrLookActorAbsent
	}
	if _, ok := s.Rooms[actor.Body.RoomID]; !ok {
		return LookProposal{}, ErrLookActorAbsent
	}
	proposal := LookProposal{ActorID: actorID, RoomID: actor.Body.RoomID, Hour: hour, Prefix: prefix, Occurrence: occurrence, ExitIndex: -1}
	if prefix == "" {
		if occurrence != 0 {
			return LookProposal{}, ErrLookExitUnresolved
		}
		scene, err := s.CurrentScene(actorID, hour)
		if err != nil {
			return LookProposal{}, err
		}
		proposal.Mode = lookModeHere
		proposal.Response = scene
		return proposal, nil
	}
	if occurrence < 1 {
		return LookProposal{}, ErrLookExitUnresolved
	}
	if flag(actor.Body.Flags[:], lookBlindFlag) {
		proposal.Mode = lookModeBlind
		proposal.Response = LookBlindResponse
		return proposal, nil
	}
	room := s.Rooms[actor.Body.RoomID]
	index := SelectExit(room.Resource.Exits, prefix, occurrence, flag(actor.Body.Flags[:], lookDetectInvisibleFlag))
	if index < 0 {
		object, err := s.selectLookObject(actor, room, prefix, occurrence)
		if err == nil {
			if !validLookAtName(object.Object.Name) {
				return LookProposal{}, ErrLookObjectCreatureUnmigrated
			}
			proposal.Mode = lookModeObject
			proposal.TargetKind = "object"
			proposal.TargetID = object.ID
			proposal.TargetName = object.Object.Name
			proposal.Response = lookObjectResponse(object.Object)
			return proposal, nil
		}
		if !errors.Is(err, ErrCanonicalRoomObjectNotFound) {
			return LookProposal{}, err
		}
		if prefix == "나" {
			if !validLookAtName(actor.Body.Name) {
				return LookProposal{}, ErrLookObjectCreatureUnmigrated
			}
			response, err := lookSelfInspectResponse(s, actorID, actor)
			if err != nil {
				return LookProposal{}, err
			}
			proposal.Mode = lookModeSelf
			proposal.TargetKind = "player"
			proposal.TargetID = actorID
			proposal.TargetName = actor.Body.Name
			proposal.Response = response
			return s.attachLookInspectBroadcast(proposal, room, actor.Body, actor.Body)
		}
		creature, err := s.selectLookRoomCreature(actor.Body, room, prefix, occurrence)
		if err == nil {
			if !validLookAtName(creature.Name) {
				return LookProposal{}, ErrLookObjectCreatureUnmigrated
			}
			response, err := lookCreatureInspectResponse(s, actorID, actor.Body, creature.Body, s.NPCs[creature.ID])
			if err != nil {
				return LookProposal{}, err
			}
			proposal.Mode = lookModeCreature
			proposal.TargetKind = creature.Kind
			proposal.TargetID = creature.ID
			proposal.TargetName = creature.Name
			proposal.Response = response
			return s.attachLookInspectBroadcast(proposal, room, actor.Body, creature.Body)
		}
		if !errors.Is(err, errLookCreatureAbsent) {
			return LookProposal{}, err
		}
		player, err := s.selectLookRoomPlayer(actor.Body, room, prefix, occurrence)
		if err == nil {
			if !validLookAtName(player.Name) {
				return LookProposal{}, ErrLookObjectCreatureUnmigrated
			}
			response, err := lookPlayerInspectResponse(actor.Body, player, s.Players[player.ID].Items)
			if err != nil {
				return LookProposal{}, err
			}
			proposal.Mode = lookModePlayer
			proposal.TargetKind = player.Kind
			proposal.TargetID = player.ID
			proposal.TargetName = player.Name
			proposal.Response = response
			return s.attachLookInspectBroadcast(proposal, room, actor.Body, player.Body)
		}
		if !errors.Is(err, errLookCreatureAbsent) {
			return LookProposal{}, err
		}
		return LookProposal{}, fmt.Errorf("%w: %w", ErrLookExitUnresolved, ErrLookObjectCreatureUnmigrated)
	}
	exit := room.Resource.Exits[index]
	if !validLookAtName(exit.Name) {
		return LookProposal{}, ErrLookExitUnresolved
	}
	proposal.ExitIndex = index
	proposal.ExitName = exit.Name
	proposal.DestRoomID = exit.Destination
	if flag(exit.Flags[:], lookClosedExitFlag) {
		proposal.Mode = lookModeClosed
		proposal.Response = LookClosedResponse
		return proposal, nil
	}
	dest, ok := s.Rooms[exit.Destination]
	if !ok {
		return LookProposal{}, ErrLookDestinationUnresolved
	}
	if dest.Resource.ID == actor.Body.RoomID {
		proposal.Mode = lookModeNoMap
		proposal.Response = LookNoMapResponse
		return proposal, nil
	}
	if flag(dest.Resource.Flags[:], lookRoomNoMarryFlag) || flag(dest.Resource.Flags[:], lookRoomNoFamilyFlag) {
		proposal.Mode = lookModeHidden
		proposal.Response = LookHiddenResponse
		return proposal, nil
	}
	scene, err := s.SceneAt(actorID, dest.Resource.ID, hour)
	if err != nil {
		return LookProposal{}, err
	}
	proposal.Mode = lookModePeek
	proposal.Response = scene
	return proposal, nil
}

func (s State) ApplyLook(proposal LookProposal) (State, LookResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, LookResult{}, err
	}
	if proposal.ActorID == "" || proposal.Response == "" || proposal.Changed {
		return State{}, LookResult{}, ErrLookInvalidProposal
	}
	fresh, err := s.PlanLook(proposal.ActorID, proposal.Prefix, proposal.Occurrence, proposal.Hour)
	if err != nil {
		return State{}, LookResult{}, fmt.Errorf("%w: %v", ErrLookStaleProposal, err)
	}
	if fresh.Mode != proposal.Mode || fresh.Response != proposal.Response || fresh.RoomID != proposal.RoomID || fresh.ExitIndex != proposal.ExitIndex || fresh.ExitName != proposal.ExitName || fresh.DestRoomID != proposal.DestRoomID || fresh.TargetKind != proposal.TargetKind || fresh.TargetID != proposal.TargetID || fresh.TargetName != proposal.TargetName || fresh.Broadcast != proposal.Broadcast || fresh.BroadcastText != proposal.BroadcastText {
		return State{}, LookResult{}, ErrLookStaleProposal
	}
	return s, LookResult{Response: proposal.Response, Mode: proposal.Mode, ExitName: proposal.ExitName, DestID: proposal.DestRoomID, TargetKind: proposal.TargetKind, TargetID: proposal.TargetID, TargetName: proposal.TargetName, Broadcast: proposal.Broadcast, BroadcastText: proposal.BroadcastText}, nil
}

// RoomLookInspectEvent derives command2.c:look inspect broadcast_rom from a
// committed snapshot. Bare/peek/object looks return ok=false. Unmigrated
// room occupants fail closed so transport cannot skip a ghost identity.
func (s State) RoomLookInspectEvent(actorID, prefix string, occurrence, hour int) (LookInspectEvent, bool, error) {
	proposal, err := s.PlanLook(actorID, prefix, occurrence, hour)
	if err != nil {
		return LookInspectEvent{}, false, err
	}
	if !proposal.Broadcast || proposal.BroadcastText == "" {
		return LookInspectEvent{}, false, nil
	}
	actor, target, err := s.lookInspectBroadcastBodies(proposal)
	if err != nil {
		return LookInspectEvent{}, false, err
	}
	return LookInspectEvent{RoomID: proposal.RoomID, ActorID: actorID, ExcludeActorID: actorID, Text: proposal.BroadcastText, Mode: proposal.Mode, Actor: actor, Target: target}, true, nil
}

func (s State) lookInspectBroadcastBodies(proposal LookProposal) (LegacyMonster, LegacyMonster, error) {
	actor, ok := s.Players[proposal.ActorID]
	if !ok || !actor.Online {
		return LegacyMonster{}, LegacyMonster{}, ErrLookActorAbsent
	}
	switch proposal.Mode {
	case lookModeSelf:
		return actor.Body, actor.Body, nil
	case lookModeCreature:
		npc, ok := s.NPCs[proposal.TargetID]
		if !ok {
			return LegacyMonster{}, LegacyMonster{}, ErrLookObjectCreatureUnmigrated
		}
		return actor.Body, npc.Body, nil
	case lookModePlayer:
		player, ok := s.Players[proposal.TargetID]
		if !ok || !player.Online {
			return LegacyMonster{}, LegacyMonster{}, ErrLookObjectCreatureUnmigrated
		}
		return actor.Body, player.Body, nil
	default:
		return actor.Body, LegacyMonster{}, nil
	}
}

func (s State) attachLookInspectBroadcast(proposal LookProposal, room RoomState, actor, target LegacyMonster) (LookProposal, error) {
	if err := s.lookInspectOccupants(room); err != nil {
		return LookProposal{}, err
	}
	proposal.Broadcast = true
	proposal.BroadcastText = lookInspectBroadcastText(actor, proposal.Mode, target)
	return proposal, nil
}

func (s State) lookInspectOccupants(room RoomState) error {
	for _, id := range room.PlayerIDs {
		if id == "" {
			return fmt.Errorf("%w: unresolved look occupant", ErrLookObjectCreatureUnmigrated)
		}
		player, exists := s.Players[id]
		if !exists || !player.Online || player.Body.Type != 0 || player.Body.RoomID != room.Resource.ID {
			return fmt.Errorf("%w: unresolved look occupant", ErrLookObjectCreatureUnmigrated)
		}
		if player.Body.Name == "" || !validLookAtName(player.Body.Name) {
			return fmt.Errorf("%w: unmigrated look occupant", ErrLookObjectCreatureUnmigrated)
		}
	}
	return nil
}

const (
	lookBroadcastINV = 2 // io.c INV from viewer PDINVI
	lookBroadcastDMF = 8 // io.c DMF from viewer class==DM
)

func lookInspectBroadcastText(actor LegacyMonster, mode string, target LegacyMonster) string {
	return lookInspectBroadcastTextFor(actor, mode, target, lookBroadcastOmniscientViewer())
}

func lookInspectBroadcastTextFor(actor LegacyMonster, mode string, target, viewer LegacyMonster) string {
	if mode == lookModeSelf || (mode == lookModeCreature && target.Name != "" && strings.HasPrefix(actor.Name, target.Name)) {
		return lookBroadcastSelfText(actor, viewer)
	}
	return lookBroadcastInspectText(actor, target, viewer)
}

func lookBroadcastOmniscientViewer() LegacyMonster {
	var viewer LegacyMonster
	viewer.Class = playerDMClass
	viewer.Flags[playerDetectFlag/8] |= 1 << (playerDetectFlag % 8)
	return viewer
}

func lookBroadcastViewerFlags(viewer LegacyMonster) int {
	flags := 0
	if flag(viewer.Flags[:], playerDetectFlag) {
		flags |= lookBroadcastINV
	}
	if viewer.Class == playerDMClass {
		flags |= lookBroadcastDMF
	}
	return flags
}

// lookBroadcastName is misc.c:crt_str for a non-CAP %M. Monsters keep the
// raw name. Players: ((PINVIS||PDMINV) && !INV) || (PDMINV && !DMF) → 누군가;
// else name, optional (*), then 님.
func lookBroadcastName(body LegacyMonster, viewerFlags int) string {
	if body.Type != 0 {
		return body.Name
	}
	pinvis := flag(body.Flags[:], playerInvisibleFlag)
	pdminv := flag(body.Flags[:], playerDMInvisibleFlag)
	if ((pinvis || pdminv) && viewerFlags&lookBroadcastINV == 0) || (pdminv && viewerFlags&lookBroadcastDMF == 0) {
		return "누군가"
	}
	name := body.Name
	if pinvis {
		name += "(*)"
	}
	return name + "님"
}

func lookBroadcastSubjectParticle(name string) string {
	if hasFinalHangul(name) {
		return "이"
	}
	return "가"
}

func lookBroadcastSelfText(actor, viewer LegacyMonster) string {
	name := lookBroadcastName(actor, lookBroadcastViewerFlags(viewer))
	return "\n" + name + lookBroadcastSubjectParticle(name) + " 거울을 들고 자신을 바라 봅니다.\r\n"
}

func lookBroadcastInspectText(actor, target, viewer LegacyMonster) string {
	flags := lookBroadcastViewerFlags(viewer)
	actorName := lookBroadcastName(actor, flags)
	targetName := lookBroadcastName(target, flags)
	return "\n" + actorName + lookBroadcastSubjectParticle(actorName) + " " + targetName + lookAtObjectParticle(targetName) + " 봅니다.\r\n"
}

func lookObjectPrefixMatch(object LegacyObject, prefix string) bool {
	if prefix == "" || !validLookAtName(prefix) {
		return false
	}
	if equalFoldPrefix(object.Name, prefix) {
		return true
	}
	for _, key := range object.Keys {
		if key != "" && equalFoldPrefix(key, prefix) {
			return true
		}
	}
	return false
}

func (s State) selectLookObject(actor PlayerState, room RoomState, prefix string, occurrence int) (CanonicalRoomObject, error) {
	if occurrence < 1 || prefix == "" {
		return CanonicalRoomObject{}, ErrCanonicalRoomObjectNotFound
	}
	detect := flag(actor.Body.Flags[:], lookDetectInvisibleFlag)
	if lookLegacyObjectsVisible(actor.Body.Inventory, prefix, detect, true) {
		return CanonicalRoomObject{}, fmt.Errorf("%w: %w", ErrLookObjectCreatureUnmigrated, ErrCanonicalRoomObjectUnavailable)
	}
	if actor.Items != nil {
		if err := actor.Items.Validate(); err != nil {
			return CanonicalRoomObject{}, fmt.Errorf("%w: %v", ErrLookObjectCreatureUnmigrated, err)
		}
		object, err := selectLookObjectIDs(actor.Items, actor.Items.Inventory, prefix, occurrence, detect, true)
		if err == nil || !errors.Is(err, ErrCanonicalRoomObjectNotFound) {
			return object, err
		}
		object, err = selectLookReadyObject(actor.Items, prefix, occurrence)
		if err == nil || !errors.Is(err, ErrCanonicalRoomObjectNotFound) {
			return object, err
		}
	}
	return s.selectLookRoomObject(actor.Body, room, prefix, occurrence)
}

func lookLegacyObjectsVisible(objects []LegacyObject, prefix string, detect, skipInvisible bool) bool {
	for _, object := range objects {
		if !lookObjectPrefixMatch(object, prefix) {
			continue
		}
		if skipInvisible && flag(object.Flags[:], objectInvisibleFlag) && !detect {
			continue
		}
		return true
	}
	return false
}

func lookObjectCandidate(id string, item Item) (CanonicalRoomObject, error) {
	if item.Object.Special != 0 || item.Object.Name == "" || !validLookAtName(item.Object.Name) {
		return CanonicalRoomObject{}, ErrLookObjectCreatureUnmigrated
	}
	return CanonicalRoomObject{ID: id, Object: item.Object}, nil
}

func selectLookObjectIDs(items *ItemCollection, ids []string, prefix string, occurrence int, detect, skipInvisible bool) (CanonicalRoomObject, error) {
	remaining := occurrence
	for _, id := range ids {
		item, ok := items.Items[id]
		if !ok || id == "" {
			return CanonicalRoomObject{}, fmt.Errorf("%w: missing look object root", ErrLookObjectCreatureUnmigrated)
		}
		if !lookObjectPrefixMatch(item.Object, prefix) {
			continue
		}
		if skipInvisible && flag(item.Object.Flags[:], objectInvisibleFlag) && !detect {
			continue
		}
		remaining--
		if remaining != 0 {
			continue
		}
		return lookObjectCandidate(id, item)
	}
	return CanonicalRoomObject{}, ErrCanonicalRoomObjectNotFound
}

func selectLookReadyObject(items *ItemCollection, prefix string, occurrence int) (CanonicalRoomObject, error) {
	remaining := occurrence
	for _, id := range items.Ready {
		if id == "" {
			continue
		}
		item, ok := items.Items[id]
		if !ok {
			return CanonicalRoomObject{}, fmt.Errorf("%w: missing look ready object", ErrLookObjectCreatureUnmigrated)
		}
		if !lookObjectPrefixMatch(item.Object, prefix) {
			continue
		}
		remaining--
		if remaining != 0 {
			continue
		}
		return lookObjectCandidate(id, item)
	}
	return CanonicalRoomObject{}, ErrCanonicalRoomObjectNotFound
}

func (s State) selectLookRoomObject(actor LegacyMonster, room RoomState, prefix string, occurrence int) (CanonicalRoomObject, error) {
	if occurrence < 1 || prefix == "" {
		return CanonicalRoomObject{}, ErrCanonicalRoomObjectNotFound
	}
	detect := flag(actor.Flags[:], lookDetectInvisibleFlag)
	if room.Items == nil {
		for _, object := range room.Resource.Objects {
			if !lookObjectPrefixMatch(object, prefix) {
				continue
			}
			if flag(object.Flags[:], objectInvisibleFlag) && !detect {
				continue
			}
			return CanonicalRoomObject{}, fmt.Errorf("%w: %w", ErrLookObjectCreatureUnmigrated, ErrCanonicalRoomObjectUnavailable)
		}
		return CanonicalRoomObject{}, ErrCanonicalRoomObjectNotFound
	}
	if err := room.Items.Validate(); err != nil {
		return CanonicalRoomObject{}, fmt.Errorf("%w: %v", ErrLookObjectCreatureUnmigrated, err)
	}
	remaining := occurrence
	for _, id := range room.Items.Inventory {
		item, ok := room.Items.Items[id]
		if !ok || id == "" {
			return CanonicalRoomObject{}, fmt.Errorf("%w: missing room object root", ErrLookObjectCreatureUnmigrated)
		}
		if !lookObjectPrefixMatch(item.Object, prefix) {
			continue
		}
		if flag(item.Object.Flags[:], objectInvisibleFlag) && !detect {
			continue
		}
		remaining--
		if remaining != 0 {
			continue
		}
		if item.Object.Special != 0 || item.Object.Name == "" || !validLookAtName(item.Object.Name) {
			return CanonicalRoomObject{}, ErrLookObjectCreatureUnmigrated
		}
		return CanonicalRoomObject{ID: id, Object: item.Object}, nil
	}
	return CanonicalRoomObject{}, ErrCanonicalRoomObjectNotFound
}

func (s State) selectLookRoomCreature(actor LegacyMonster, room RoomState, prefix string, occurrence int) (lookAtTarget, error) {
	if occurrence < 1 || prefix == "" {
		return lookAtTarget{}, errLookCreatureAbsent
	}
	if s.NPCs == nil {
		remaining := occurrence
		for _, monster := range room.Resource.Monsters {
			if !legacyCreaturePrefixMatch(monster, prefix) {
				continue
			}
			if !lookAtVisible(actor, monster) {
				continue
			}
			remaining--
			if remaining == 0 {
				return lookAtTarget{}, fmt.Errorf("%w: legacy room monster", ErrLookObjectCreatureUnmigrated)
			}
		}
		return lookAtTarget{}, errLookCreatureAbsent
	}
	remaining := occurrence
	for _, id := range room.NPCIDs {
		npc, exists := s.NPCs[id]
		if !exists || id == "" || npc.Body.Type != 1 || npc.Body.RoomID != room.Resource.ID {
			return lookAtTarget{}, fmt.Errorf("%w: unresolved NPC look identity", ErrLookObjectCreatureUnmigrated)
		}
		if !legacyCreaturePrefixMatch(npc.Body, prefix) || !lookAtVisible(actor, npc.Body) {
			continue
		}
		remaining--
		if remaining != 0 {
			continue
		}
		if npc.Body.Name == "" || !validLookAtName(npc.Body.Name) {
			return lookAtTarget{}, ErrLookObjectCreatureUnmigrated
		}
		return lookAtTarget{Kind: "npc", ID: id, Name: npc.Body.Name, Body: npc.Body}, nil
	}
	return lookAtTarget{}, errLookCreatureAbsent
}

func lookFirstPlyPrefix(prefix string) string {
	if prefix == "" {
		return prefix
	}
	first := prefix[0]
	if first >= 'a' && first <= 'z' {
		return string(first-'a'+'A') + prefix[1:]
	}
	return prefix
}

func (s State) selectLookRoomPlayer(actor LegacyMonster, room RoomState, prefix string, occurrence int) (lookAtTarget, error) {
	if occurrence < 1 || prefix == "" {
		return lookAtTarget{}, errLookCreatureAbsent
	}
	prefix = lookFirstPlyPrefix(prefix)
	remaining := occurrence
	for _, id := range room.PlayerIDs {
		player, exists := s.Players[id]
		if !exists || id == "" || !player.Online || player.Body.Type != 0 || player.Body.RoomID != room.Resource.ID {
			return lookAtTarget{}, fmt.Errorf("%w: unresolved player look identity", ErrLookObjectCreatureUnmigrated)
		}
		if !legacyCreaturePrefixMatch(player.Body, prefix) || !lookAtVisible(actor, player.Body) {
			continue
		}
		remaining--
		if remaining != 0 {
			continue
		}
		if player.Body.Name == "" || !validLookAtName(player.Body.Name) {
			return lookAtTarget{}, ErrLookObjectCreatureUnmigrated
		}
		return lookAtTarget{Kind: "player", ID: id, Name: player.Body.Name, Body: player.Body}, nil
	}
	return lookAtTarget{}, errLookCreatureAbsent
}

func lookObjectResponse(object LegacyObject) string {
	var b strings.Builder
	if desc := strings.TrimRight(object.Description, "\r\n"); desc != "" {
		b.WriteString(desc)
		b.WriteString("\r\n")
	} else {
		b.WriteString(LookObjectPlainResponse)
	}
	name := strings.TrimRight(object.Name, " ")
	if object.Type <= lookObjectMissile {
		b.WriteString(name)
		b.WriteString(lookTopicParticle(name))
		b.WriteByte(' ')
		switch object.Type {
		case lookObjectSharp:
			b.WriteString("매우 날카로운 '도'입니다.\r\n")
		case lookObjectThrust:
			b.WriteString("매우 공격적인 '검'입니다.\r\n")
		case lookObjectBlunt:
			b.WriteString("매우 위력적인 '봉'입니다.\r\n")
		case lookObjectPole:
			b.WriteString("날이 바짝 선 '창'입니다.\r\n")
		case lookObjectMissile:
			b.WriteString("매우 강력하게 보이는 '궁'입니다.\r\n")
		}
	}
	if object.Type <= lookObjectMissile || object.Type == lookObjectArmor || object.Type == lookObjectLightSource || object.Type == lookObjectWand || object.Type == lookObjectKey {
		if object.ShotsCurrent < 1 {
			b.WriteString(LookObjectBrokenResponse)
		} else if object.ShotsMax != 0 && object.ShotsCurrent <= object.ShotsMax/10 {
			b.WriteString(LookObjectWornResponse)
		}
	}
	return b.String()
}

func lookCreatureResponse(actor, target LegacyMonster) string {
	if target.Name != "" && strings.HasPrefix(actor.Name, target.Name) {
		return LookSelfMirrorResponse
	}
	var b strings.Builder
	b.WriteString("당신은 ")
	b.WriteString(target.Name)
	b.WriteString(lookAtObjectParticle(target.Name))
	b.WriteString(" 봅니다.\r\n")
	if desc := strings.TrimRight(target.Description, "\r\n"); desc != "" {
		b.WriteString(desc)
		b.WriteString("\r\n")
	} else {
		b.WriteString(LookCreaturePlainResponse)
	}
	return b.String()
}

func lookPlayerResponse(actor LegacyMonster, target lookAtTarget) string {
	rendered := lookAtRenderedTargetName(actor, target)
	return "당신은 " + rendered + lookAtObjectParticle(rendered) + " 봅니다.\r\n"
}

// lookEquipListSlots shares command3.c:equip_list order/labels with PlayerEquipment.
var lookEquipListSlots = equipListSlots

func lookSelfInspectResponse(s State, actorID string, actor PlayerState) (string, error) {
	marriage, err := lookMarriageLine(actor.Body)
	if err != nil {
		return "", err
	}
	standing, err := lookPlayerStandingLine(actor.Body)
	if err != nil {
		return "", err
	}
	band, err := lookHPBandLine(actor.Body)
	if err != nil {
		return "", err
	}
	enm, err := s.lookPlayerEnemyLines(actorID, actor.Body)
	if err != nil {
		return "", err
	}
	equip, err := lookEquipList(actor.Items)
	if err != nil {
		return "", err
	}
	return LookSelfMirrorResponse + marriage + standing + lookKnowAlignmentLine(actor.Body, actor.Body) + band + enm + lookConsiderLine(actor.Body, actor.Body) + equip, nil
}

func lookCreatureInspectResponse(s State, viewerID string, actor, target LegacyMonster, npc NPCState) (string, error) {
	band, err := lookHPBandLine(target)
	if err != nil {
		return "", err
	}
	enm, err := s.lookNPCEnemyLines(viewerID, actor.Name, target, npc.Enemies)
	if err != nil {
		return "", err
	}
	equip, err := lookEquipList(npc.Items)
	if err != nil {
		return "", err
	}
	return lookCreatureResponse(actor, target) + lookKnowAlignmentLine(actor, target) + band + enm + lookConsiderLine(actor, target) + equip, nil
}

// command2.c:219-230 first_mon/self after HP bands. first_ply omits these.
// Nil Enemies is unresolved, not peaceful.
func (s State) lookNPCEnemyLines(viewerID, viewerName string, target LegacyMonster, enemies []NPCEnemy) (string, error) {
	if enemies == nil {
		return "", fmt.Errorf("%w: NPC enemies", ErrLookObjectCreatureUnmigrated)
	}
	angry := false
	for _, enemy := range enemies {
		match, err := s.lookEnemyIsViewer(viewerID, viewerName, enemy.Target)
		if err != nil {
			return "", err
		}
		if match {
			angry = true
			break
		}
	}
	if len(enemies) == 0 {
		return lookFormatEnemyLines(target, angry, false, false, ""), nil
	}
	first := enemies[0]
	name, err := s.lookEnemyDisplayName(first.Target)
	if err != nil {
		return "", err
	}
	fightingYou, err := s.lookEnemyIsViewer(viewerID, viewerName, first.Target)
	if err != nil {
		return "", err
	}
	return lookFormatEnemyLines(target, angry, true, fightingYou, name), nil
}

func (s State) lookPlayerEnemyLines(viewerID string, target LegacyMonster) (string, error) {
	enemies := s.Players[viewerID].PlayerEnemies
	angry := false
	for _, id := range enemies {
		match, err := s.lookEnemyIsViewer(viewerID, target.Name, EntityRef{Kind: "player", ID: id})
		if err != nil {
			return "", err
		}
		if match {
			angry = true
			break
		}
	}
	if len(enemies) == 0 {
		return lookFormatEnemyLines(target, angry, false, false, ""), nil
	}
	name, err := s.lookEnemyDisplayName(EntityRef{Kind: "player", ID: enemies[0]})
	if err != nil {
		return "", err
	}
	fightingYou, err := s.lookEnemyIsViewer(viewerID, target.Name, EntityRef{Kind: "player", ID: enemies[0]})
	if err != nil {
		return "", err
	}
	return lookFormatEnemyLines(target, angry, true, fightingYou, name), nil
}

func (s State) lookEnemyIsViewer(viewerID, viewerName string, ref EntityRef) (bool, error) {
	if ref.Kind == "player" && ref.ID == viewerID {
		return true, nil
	}
	name, err := s.lookEnemyDisplayName(ref)
	if err != nil {
		return false, err
	}
	return name == viewerName, nil
}

func (s State) lookEnemyDisplayName(ref EntityRef) (string, error) {
	switch ref.Kind {
	case "player":
		player, ok := s.Players[ref.ID]
		if !ok || player.Body.Name == "" || !validLookAtName(player.Body.Name) {
			return "", fmt.Errorf("%w: unmigrated enemy name", ErrLookObjectCreatureUnmigrated)
		}
		return player.Body.Name, nil
	case "npc":
		npc, ok := s.NPCs[ref.ID]
		if !ok || npc.Body.Name == "" || !validLookAtName(npc.Body.Name) {
			return "", fmt.Errorf("%w: unmigrated enemy name", ErrLookObjectCreatureUnmigrated)
		}
		return npc.Body.Name, nil
	default:
		return "", fmt.Errorf("%w: unmigrated enemy kind", ErrLookObjectCreatureUnmigrated)
	}
}

func lookFormatEnemyLines(target LegacyMonster, angry, hasFirst, fightingYou bool, firstName string) string {
	he := lookHe(target)
	var b strings.Builder
	if angry {
		b.WriteString(he)
		b.WriteString("는 당신에게 매우 화가 난것 같습니다.\r\n")
	}
	if !hasFirst {
		return b.String()
	}
	b.WriteString(he)
	b.WriteString("는 ")
	if fightingYou {
		b.WriteString("당신과 싸우고 있습니다.\r\n")
		return b.String()
	}
	b.WriteString(firstName)
	if hasFinalHangul(firstName) {
		b.WriteString("과")
	} else {
		b.WriteString("와")
	}
	b.WriteString(" 싸우고 있습니다.\r\n")
	return b.String()
}

// command2.c:196-203 first_mon/self: viewer PKNOWA and target alignment!=0.
func lookKnowAlignmentLine(viewer, target LegacyMonster) string {
	if !flag(viewer.Flags[:], lookKnowAlignmentFlag) || target.Alignment == 0 {
		return ""
	}
	color := "푸른 광채"
	if target.Alignment < 0 {
		color = "붉은 광채"
	}
	return lookHe(target) + "에게서 " + color + "가 뻗어 나오고 있습니다.\r\n"
}

func lookHe(target LegacyMonster) string {
	if flag(target.Flags[:], lookMaleFlag) {
		return "그"
	}
	return "그녀"
}

// command2.c:204-218 first_mon/self hpcur vs hpmax exclusive bands.
// hpmax<=0 is unmigrated and fail-closed; C integer division uses int.
func lookHPBandLine(target LegacyMonster) (string, error) {
	if target.HPMax <= 0 {
		return "", fmt.Errorf("%w: unmigrated hpmax", ErrLookObjectCreatureUnmigrated)
	}
	hpcur := int(target.HPCurrent)
	hpmax := int(target.HPMax)
	he := lookHe(target)
	var b strings.Builder
	if hpcur < (hpmax*9)/10 && hpcur > (hpmax*8)/10 {
		b.WriteString(he)
		b.WriteString("는 가벼운 상처를 입었습니다.\r\n")
	}
	if hpcur < (hpmax*8)/10 && hpcur > (hpmax*6)/10 {
		b.WriteString(he)
		b.WriteString("는 여러군데 상처를 입었습니다.\r\n")
	}
	if hpcur < (hpmax*6)/10 && hpcur > (hpmax*4)/10 {
		b.WriteString(he)
		b.WriteString("는 많은 상처를 입었습니다.\r\n")
	}
	if hpcur < (hpmax*4)/10 && hpcur > (hpmax*2)/10 {
		b.WriteString(he)
		b.WriteString("는 심각한 상처를 입었습니다.\r\n")
	}
	if hpcur < (hpmax*2)/10 {
		b.WriteString(he)
		b.WriteString("는 죽기 직전입니다.\r\n")
	}
	return b.String(), nil
}

// command2.c:181-185 self / 243-246 first_ply: PLAYER && PMARRI.
func lookMarriageLine(target LegacyMonster) (string, error) {
	if !flag(target.Flags[:], MarriageActiveFlag) {
		return "", nil
	}
	spouse, ok := marriageSpouseName(target)
	if !ok {
		return "", fmt.Errorf("%w: unmigrated spouse", ErrLookObjectCreatureUnmigrated)
	}
	return lookHe(target) + "는 " + spouse + "님과 결혼한 기혼자입니다.\r\n", nil
}

func lookPlayerInspectResponse(actor LegacyMonster, target lookAtTarget, items *ItemCollection) (string, error) {
	marriage, err := lookMarriageLine(target.Body)
	if err != nil {
		return "", err
	}
	standing, err := lookPlayerStandingLine(target.Body)
	if err != nil {
		return "", err
	}
	hp, err := lookPlayerHPLine(target.Body)
	if err != nil {
		return "", err
	}
	equip, err := lookEquipList(items)
	if err != nil {
		return "", err
	}
	return lookPlayerResponse(actor, target) + marriage + standing + lookPlayerKnowAlignmentLine(actor, target.Body) + hp + equip, nil
}

// command2.c:257-264 first_ply: viewer PKNOWA && alignment in [-100,100].
// Inner alignment<-100 붉은 is unreachable on this gate, so this path
// always prints 푸른 광채. MMALES 그/그녀.
func lookPlayerKnowAlignmentLine(viewer, target LegacyMonster) string {
	if !flag(viewer.Flags[:], lookKnowAlignmentFlag) || target.Alignment < -100 || target.Alignment >= 101 {
		return ""
	}
	return lookHe(target) + "에게서 푸른 광채가 뻗어 나오고 있습니다.\r\n"
}

// command2.c:265-267 first_ply: hpcur < hpmax*3/10 only.
// hpmax<=0 is unmigrated and fail-closed; C integer division uses int.
func lookPlayerHPLine(target LegacyMonster) (string, error) {
	if target.HPMax <= 0 {
		return "", fmt.Errorf("%w: unmigrated hpmax", ErrLookObjectCreatureUnmigrated)
	}
	if int(target.HPCurrent) < (int(target.HPMax)*3)/10 {
		return lookHe(target) + "는 가벼운 상처를 입었습니다.\r\n", nil
	}
	return "", nil
}

// command2.c:192-195 self / 253-255 first_ply PLAYER: "%s는 %s서 있습니다."
// Empty or non-canonical description is unmigrated and fail-closed;
// C would still print with an empty field.
func lookPlayerStandingLine(target LegacyMonster) (string, error) {
	canonical, err := canonicalPlayerDescription(target.Description)
	if err != nil || canonical == "" || canonical != target.Description {
		return "", fmt.Errorf("%w: unmigrated player description", ErrLookObjectCreatureUnmigrated)
	}
	return lookHe(target) + "는 " + target.Description + "서 있습니다.\r\n", nil
}

func lookConsiderLine(actor, target LegacyMonster) string {
	he := lookHe(target)
	diff := int(actor.Level)/4 - int(target.Level)/4
	if diff > 4 {
		diff = 4
	}
	if diff < -4 {
		diff = -4
	}
	switch diff {
	case 0:
		return he + "는 당신과 꼭 맞는 상대입니다!\r\n"
	case 1:
		return he + "는 별 무리없이 이길 수 있습니다.\r\n"
	case -1:
		return he + "는 운이 좋으면 이길 수 있습니다..\r\n"
	case 2:
		return he + "는 별로 힘 안들이고 이길수 있습니다.\r\n"
	case -2:
		return he + "는 상대하기 힘들겠는데요?\r\n"
	case 3:
		return he + "는 손쉽게 상대할수 있습니다.\r\n"
	case -3:
		return "당신은 " + he + "에게 쨉도 안됩니다.\r\n"
	case 4:
		return he + "는 한방에 보낼수 있습니다.\r\n"
	case -4:
		return he + "는 보자마자 도망가는것이 좋을겁니다.\r\n"
	default:
		return he + "는 당신과 꼭 맞는 상대입니다!\r\n"
	}
}

func lookEquipList(items *ItemCollection) (string, error) {
	if items == nil {
		return "", fmt.Errorf("%w: nil ready items", ErrLookObjectCreatureUnmigrated)
	}
	if err := items.Validate(); err != nil {
		return "", fmt.Errorf("%w: %v", ErrLookObjectCreatureUnmigrated, err)
	}
	var b strings.Builder
	for _, slot := range lookEquipListSlots {
		id := items.Ready[slot.index]
		if id == "" {
			continue
		}
		item, ok := items.Items[id]
		if !ok {
			return "", fmt.Errorf("%w: missing look ready object", ErrLookObjectCreatureUnmigrated)
		}
		if item.Object.Name == "" || !validLookAtName(item.Object.Name) {
			return "", ErrLookObjectCreatureUnmigrated
		}
		b.WriteString(slot.label)
		b.WriteString("  ")
		b.WriteString(item.Object.Name)
		b.WriteString("\r\n")
	}
	return b.String(), nil
}

func lookTopicParticle(name string) string {
	if hasFinalHangul(name) {
		return "은"
	}
	return "는"
}
