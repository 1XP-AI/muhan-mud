package world

import (
	"errors"
	"fmt"
	"reflect"
	"unicode/utf8"
)

const (
	dmFamilyCaretakerClass       = 10 // CARETAKER; process_cmd * gate
	dmMoonstoneObjectID    int16 = 640
	dmMoonstoneRoomMax           = 8999 // RMAX-1
	dmMoonstoneMaxAttempts       = 10000
	dmInvasionCount              = 10
	dmInvasionRoomLo       int16 = 3601
	dmInvasionRoomHi       int16 = 3630
	dmInvasionMonsterLo    int16 = 265
	dmInvasionMonsterHi    int16 = 299

	DMMoonstoneUnknownResponse = "\"*떨어져라\": 이런 명령어는 없네요."
	DMInvasionUnknownResponse  = "\"*침공\": 이런 명령어는 없네요."
	DMInvasionBroadcast1       = "\n무적존을 강탈하려고 드레니아 몹이 침공했습니다."
	DMInvasionBroadcast2       = "\n라작이 부하들에게 외칩니다. \"일반은 건드리지 말아아라\""
)

var (
	ErrDMFamilyActorAbsent     = errors.New("online canonical DM family actor absent")
	ErrDMFamilyCatalog         = errors.New("DM family spawn catalog unavailable")
	ErrDMFamilyRandom          = errors.New("DM family random source unavailable")
	ErrDMFamilyAllocator       = errors.New("DM family identity allocator unavailable")
	ErrDMFamilyFloorUnresolved = errors.New("DM family room floor unresolved")
	ErrDMFamilyNPCUnresolved   = errors.New("DM family NPC state unresolved")
	ErrDMFamilyRoomExhausted   = errors.New("DM family room search exhausted")
	ErrDMFamilyStaleProposal   = errors.New("stale DM family proposal")
	ErrDMFamilyInvalidProposal = errors.New("invalid DM family proposal")
)

type DMFamilyAction string

const (
	DMFamilyMoonstone DMFamilyAction = "moonstone"
	DMFamilyInvasion  DMFamilyAction = "invasion"
)

type DMFamilyEvent struct {
	AllPlayers bool   `json:"all_players,omitempty"`
	ActorID    string `json:"actor_id,omitempty"`
	Text       string `json:"text"`
}

type DMFamilyResult struct {
	Action   DMFamilyAction  `json:"action"`
	ActorID  string          `json:"actor_id"`
	RoomID   int16           `json:"room_id,omitempty"`
	RoomName string          `json:"room_name,omitempty"`
	ItemID   string          `json:"item_id,omitempty"`
	NPCIDs   []string        `json:"npc_ids,omitempty"`
	Response string          `json:"response"`
	Changed  bool            `json:"changed"`
	Events   []DMFamilyEvent `json:"events,omitempty"`
}

type DMFamilyProposal struct {
	Action   DMFamilyAction
	ActorID  string
	RoomID   int16
	RoomName string
	ItemID   string
	NPCIDs   []string
	Response string
	Changed  bool
	Events   []DMFamilyEvent

	before          State
	expectedActor   PlayerState
	afterItems      *ItemCollection
	afterNPCs       map[string]NPCState
	afterActive     []string
	afterRoomNPCIDs map[int16][]string
}

func DMMoonstoneBroadcast(roomName string) string {
	return fmt.Sprintf("\n%s에 초인의 돌이 떨어졌습니다.", roomName)
}

func dmFamilyCanStar(class byte) bool {
	return class >= dmFamilyCaretakerClass
}

func dmFamilyActor(s State, actorID string) (PlayerState, error) {
	if err := s.Validate(); err != nil {
		return PlayerState{}, err
	}
	actor, ok := s.Players[actorID]
	if actorID == "" || !ok || !actor.Online || actor.Body.Type != 0 {
		return PlayerState{}, ErrDMFamilyActorAbsent
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok || room.Resource.ID != actor.Body.RoomID || !containsString(room.PlayerIDs, actorID) {
		return PlayerState{}, ErrDMFamilyActorAbsent
	}
	return actor, nil
}

func dmFamilyUnknown(action DMFamilyAction, actorID, response string, snapshot State, actor PlayerState) DMFamilyProposal {
	return DMFamilyProposal{
		Action: action, ActorID: actorID, Response: response, Changed: false,
		before: snapshot, expectedActor: actor,
	}
}

func dmFamilyRoll(roll func(int, int) int, lo, hi int) (int, error) {
	if roll == nil || lo > hi {
		return 0, ErrDMFamilyRandom
	}
	n := roll(lo, hi)
	if n < lo || n > hi {
		return 0, fmt.Errorf("%w: %d outside %d..%d", ErrDMFamilyRandom, n, lo, hi)
	}
	return n, nil
}

func cloneDMFamilyEnemies(in []NPCEnemy) []NPCEnemy {
	if in == nil {
		return nil
	}
	out := make([]NPCEnemy, len(in))
	copy(out, in)
	return out
}

func insertFloorItem(dest ItemCollection, id string, item Item) (ItemCollection, error) {
	next := dest.clone()
	if next.Items == nil {
		next.Items = map[string]Item{}
	}
	if _, exists := next.Items[id]; exists || id == "" {
		return ItemCollection{}, fmt.Errorf("%w: duplicate item", ErrDMFamilyInvalidProposal)
	}
	next.Items[id] = item
	object := item.Object
	at := len(next.Inventory)
	for i, existing := range next.Inventory {
		other := next.Items[existing].Object
		if other.Name > object.Name || (other.Name == object.Name && int8(other.Adjustment) > int8(object.Adjustment)) {
			at = i
			break
		}
	}
	next.Inventory = append(next.Inventory, "")
	copy(next.Inventory[at+1:], next.Inventory[at:])
	next.Inventory[at] = id
	if err := next.Validate(); err != nil {
		return ItemCollection{}, err
	}
	return next, nil
}

// PlanDMMoonstone is dm5.c:dm_moonstone (`*떨어져라`).
func (s State) PlanDMMoonstone(actorID string, catalog SpawnCatalog, roll func(int, int) int, allocate func() (string, error)) (DMFamilyProposal, error) {
	actor, err := dmFamilyActor(s, actorID)
	if err != nil {
		return DMFamilyProposal{}, err
	}
	snapshot := s.clone()
	if !dmFamilyCanStar(actor.Body.Class) {
		return dmFamilyUnknown(DMFamilyMoonstone, actorID, DMMoonstoneUnknownResponse, snapshot, actor), nil
	}
	if catalog == nil {
		return DMFamilyProposal{}, ErrDMFamilyCatalog
	}
	if allocate == nil {
		return DMFamilyProposal{}, ErrDMFamilyAllocator
	}
	var room RoomState
	var roomID int16
	found := false
	for attempt := 0; attempt < dmMoonstoneMaxAttempts; attempt++ {
		n, err := dmFamilyRoll(roll, 1, dmMoonstoneRoomMax)
		if err != nil {
			return DMFamilyProposal{}, err
		}
		id := int16(n)
		candidate, ok := s.Rooms[id]
		if !ok || candidate.Items == nil || flag(candidate.Resource.Flags[:], roomNoTeleportFlag) {
			continue
		}
		if utf8.RuneCountInString(candidate.Resource.Name) < 2 && len(candidate.Resource.Name) < 2 {
			return dmFamilyUnknown(DMFamilyMoonstone, actorID, "", snapshot, actor), nil
		}
		room, roomID, found = candidate, id, true
		break
	}
	if !found {
		return DMFamilyProposal{}, ErrDMFamilyRoomExhausted
	}
	object, err := catalog.Object(dmMoonstoneObjectID)
	if err != nil {
		return DMFamilyProposal{}, fmt.Errorf("%w: %v", ErrDMFamilyCatalog, err)
	}
	bonus, err := dmFamilyRoll(roll, 1, 20)
	if err != nil {
		return DMFamilyProposal{}, err
	}
	if int(object.ShotsMax)+bonus > 32767 {
		return DMFamilyProposal{}, ErrDMFamilyInvalidProposal
	}
	object.ShotsMax += int16(bonus)
	object.ShotsCurrent = object.ShotsMax
	spawned, err := ImportItems([]LegacyObject{object}, allocate)
	if err != nil || len(spawned.Inventory) != 1 {
		return DMFamilyProposal{}, fmt.Errorf("%w: %v", ErrDMFamilyFloorUnresolved, err)
	}
	itemID := spawned.Inventory[0]
	nextItems, err := insertFloorItem(*room.Items, itemID, spawned.Items[itemID])
	if err != nil {
		return DMFamilyProposal{}, err
	}
	text := DMMoonstoneBroadcast(room.Resource.Name)
	return DMFamilyProposal{
		Action: DMFamilyMoonstone, ActorID: actorID, RoomID: roomID, RoomName: room.Resource.Name,
		ItemID: itemID, Response: text, Changed: true,
		Events: []DMFamilyEvent{{ActorID: actorID, Text: text}},
		before: snapshot, expectedActor: actor, afterItems: &nextItems,
	}, nil
}

// PlanDMInvasion is dm5.c:dm_monster (`*침공`).
func (s State) PlanDMInvasion(actorID string, catalog SpawnCatalog, roll func(int, int) int, allocate func() (string, error)) (DMFamilyProposal, error) {
	actor, err := dmFamilyActor(s, actorID)
	if err != nil {
		return DMFamilyProposal{}, err
	}
	snapshot := s.clone()
	if !dmFamilyCanStar(actor.Body.Class) {
		return dmFamilyUnknown(DMFamilyInvasion, actorID, DMInvasionUnknownResponse, snapshot, actor), nil
	}
	if catalog == nil {
		return DMFamilyProposal{}, ErrDMFamilyCatalog
	}
	if allocate == nil {
		return DMFamilyProposal{}, ErrDMFamilyAllocator
	}
	if s.NPCs == nil || s.ActiveNPCIDs == nil {
		return DMFamilyProposal{}, ErrDMFamilyNPCUnresolved
	}
	npcs := make(map[string]NPCState, len(s.NPCs)+dmInvasionCount)
	for id, npc := range s.NPCs {
		copied := npc
		copied.Body = cloneNPCBody(npc.Body)
		copied.Enemies = cloneDMFamilyEnemies(npc.Enemies)
		npcs[id] = copied
	}
	ids := make([]string, 0, dmInvasionCount)
	active := cloneNPCResourceIDs(s.ActiveNPCIDs)
	roomNPCIDs := make(map[int16][]string)
	for i := 0; i < dmInvasionCount; i++ {
		roomN, err := dmFamilyRoll(roll, int(dmInvasionRoomLo), int(dmInvasionRoomHi))
		if err != nil {
			return DMFamilyProposal{}, err
		}
		monN, err := dmFamilyRoll(roll, int(dmInvasionMonsterLo), int(dmInvasionMonsterHi))
		if err != nil {
			return DMFamilyProposal{}, err
		}
		roomID := int16(roomN)
		room, ok := s.Rooms[roomID]
		if !ok {
			return DMFamilyProposal{}, fmt.Errorf("%w: room %d", ErrDMFamilyFloorUnresolved, roomID)
		}
		template, err := catalog.Monster(int16(monN))
		if err != nil {
			return DMFamilyProposal{}, fmt.Errorf("%w: %v", ErrDMFamilyCatalog, err)
		}
		id, err := allocate()
		if err != nil || id == "" {
			return DMFamilyProposal{}, ErrDMFamilyAllocator
		}
		if _, exists := npcs[id]; exists {
			return DMFamilyProposal{}, ErrDMFamilyAllocator
		}
		body := cloneNPCBody(template)
		body.Type = 1
		body.RoomID = roomID
		npcs[id] = NPCState{Body: body, Enemies: []NPCEnemy{}}
		ids = append(ids, id)
		current, staged := roomNPCIDs[roomID]
		if !staged {
			current = cloneNPCResourceIDs(room.NPCIDs)
		}
		inserted, insertErr := insertNPCResourceID(current, id, s.NPCs, npcs)
		if insertErr != nil {
			return DMFamilyProposal{}, fmt.Errorf("%w: %v", ErrDMFamilyFloorUnresolved, insertErr)
		}
		roomNPCIDs[roomID] = inserted
		if len(room.PlayerIDs) != 0 {
			active = prependNPCResourceID(active, id)
		}
	}
	return DMFamilyProposal{
		Action: DMFamilyInvasion, ActorID: actorID, NPCIDs: ids,
		Response: DMInvasionBroadcast1 + DMInvasionBroadcast2, Changed: true,
		Events: []DMFamilyEvent{
			{ActorID: actorID, Text: DMInvasionBroadcast1},
			{ActorID: actorID, Text: DMInvasionBroadcast2},
		},
		before: snapshot, expectedActor: actor, afterNPCs: npcs, afterActive: active, afterRoomNPCIDs: roomNPCIDs,
	}, nil
}

func dmFamilyResult(p DMFamilyProposal) DMFamilyResult {
	return DMFamilyResult{
		Action: p.Action, ActorID: p.ActorID, RoomID: p.RoomID, RoomName: p.RoomName,
		ItemID: p.ItemID, NPCIDs: append([]string(nil), p.NPCIDs...),
		Response: p.Response, Changed: p.Changed,
		Events: append([]DMFamilyEvent(nil), p.Events...),
	}
}

func dmFamilyProposalMatches(a, b DMFamilyProposal) bool {
	return a.Action == b.Action && a.ActorID == b.ActorID && a.RoomID == b.RoomID && a.RoomName == b.RoomName &&
		a.ItemID == b.ItemID && reflect.DeepEqual(a.NPCIDs, b.NPCIDs) && a.Response == b.Response &&
		a.Changed == b.Changed && reflect.DeepEqual(a.Events, b.Events)
}

// ApplyDMFamily applies a moonstone drop or invasion. Unchanged class gates
// are receipts without Apply.
func (s State) ApplyDMFamily(proposal DMFamilyProposal) (State, DMFamilyResult, error) {
	if proposal.ActorID == "" || proposal.before.Version == 0 || !reflect.DeepEqual(s, proposal.before) {
		return State{}, DMFamilyResult{}, ErrDMFamilyStaleProposal
	}
	if !proposal.Changed {
		return State{}, DMFamilyResult{}, ErrDMFamilyInvalidProposal
	}
	next := s.clone()
	switch proposal.Action {
	case DMFamilyMoonstone:
		if proposal.afterItems == nil || proposal.RoomID == 0 {
			return State{}, DMFamilyResult{}, ErrDMFamilyInvalidProposal
		}
		room := next.Rooms[proposal.RoomID]
		items := proposal.afterItems.clone()
		room.Items = &items
		next.Rooms[proposal.RoomID] = room
	case DMFamilyInvasion:
		if proposal.afterNPCs == nil || next.NPCs == nil || proposal.afterActive == nil || proposal.afterRoomNPCIDs == nil {
			return State{}, DMFamilyResult{}, ErrDMFamilyInvalidProposal
		}
		next.NPCs = make(map[string]NPCState, len(proposal.afterNPCs))
		for id, npc := range proposal.afterNPCs {
			copied := npc
			copied.Body = cloneNPCBody(npc.Body)
			copied.Enemies = cloneDMFamilyEnemies(npc.Enemies)
			next.NPCs[id] = copied
		}
		next.ActiveNPCIDs = cloneNPCResourceIDs(proposal.afterActive)
		for _, id := range proposal.NPCIDs {
			if _, ok := next.NPCs[id]; !ok {
				return State{}, DMFamilyResult{}, ErrDMFamilyInvalidProposal
			}
		}
		for roomID, npcIDs := range proposal.afterRoomNPCIDs {
			room, ok := next.Rooms[roomID]
			if !ok {
				return State{}, DMFamilyResult{}, ErrDMFamilyFloorUnresolved
			}
			room.NPCIDs = cloneNPCResourceIDs(npcIDs)
			next.Rooms[roomID] = room
		}
	default:
		return State{}, DMFamilyResult{}, ErrDMFamilyInvalidProposal
	}
	if err := next.Validate(); err != nil {
		return State{}, DMFamilyResult{}, err
	}
	return next, dmFamilyResult(proposal), nil
}
