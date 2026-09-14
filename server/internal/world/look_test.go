package world

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func lookExitFixture() State {
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{
					ID: 1, Name: "광장",
					Exits: []LegacyExit{
						{Name: "동", Destination: 2},
						{Name: "동굴", Destination: 3},
						{Name: "비밀", Destination: 4, Flags: [4]byte{1}},
						{Name: "숨은길", Destination: 4, Flags: [4]byte{2}},
					},
				}},
				PlayerIDs: []string{"a"},
				Items:     &ItemCollection{Items: map[string]Item{}},
			},
			2: {
				Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 2, Name: "동쪽방"}},
				Items:    &ItemCollection{Items: map[string]Item{}},
			},
			3: {
				Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 3, Name: "동굴방"}},
				Items:    &ItemCollection{Items: map[string]Item{}},
			},
			4: {
				Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 4, Name: "비밀방"}},
				Items:    &ItemCollection{Items: map[string]Item{}},
			},
		},
		Players: map[string]PlayerState{
			"a": {Body: LegacyMonster{Name: "Alice", RoomID: 1, HPMax: 100, HPCurrent: 100}, Online: true, Items: &ItemCollection{Items: map[string]Item{}}},
		},
	}
}

func TestPlanLookBareStillRendersActorRoom(t *testing.T) {
	s := lookExitFixture()
	original := s
	proposal, err := s.PlanLook("a", "", 0, 12)
	if err != nil || proposal.Mode != lookModeHere || !strings.Contains(proposal.Response, "광장") || strings.Contains(proposal.Response, "동쪽방") {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	next, result, err := s.ApplyLook(proposal)
	if err != nil || result.Response != proposal.Response || !reflect.DeepEqual(next, s) || !reflect.DeepEqual(s, original) {
		t.Fatalf("apply mutated look: result=%+v err=%v", result, err)
	}
}

func TestPlanLookPeeksDestinationWithoutMoving(t *testing.T) {
	s := lookExitFixture()
	proposal, err := s.PlanLook("a", "동", 1, 12)
	if err != nil || proposal.Mode != lookModePeek || proposal.ExitName != "동" || proposal.DestRoomID != 2 || s.Players["a"].Body.RoomID != 1 {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	if !strings.Contains(proposal.Response, "동쪽방") || strings.Contains(proposal.Response, "== 광장 ==") {
		t.Fatalf("peeked current room: %q", proposal.Response)
	}
	next, result, err := s.ApplyLook(proposal)
	if err != nil || next.Players["a"].Body.RoomID != 1 || result.DestID != 2 {
		t.Fatalf("look-through moved actor: result=%+v err=%v", result, err)
	}
}

func TestPlanLookPrefixOccurrenceUsesFindExtOrder(t *testing.T) {
	s := lookExitFixture()
	first, err := s.PlanLook("a", "동", 1, 12)
	if err != nil || first.ExitName != "동" {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := s.PlanLook("a", "동", 2, 12)
	if err != nil || second.ExitName != "동굴" || !strings.Contains(second.Response, "동굴방") {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	if _, err := s.PlanLook("a", "동", 3, 12); !errors.Is(err, ErrLookExitUnresolved) {
		t.Fatalf("missing occurrence: %v", err)
	}
}

func TestPlanLookClosedExitDoesNotPeek(t *testing.T) {
	s := lookExitFixture()
	room := s.Rooms[1]
	room.Resource.Exits[0].Flags[0] |= 1 << lookClosedExitFlag
	s.Rooms[1] = room
	proposal, err := s.PlanLook("a", "동", 1, 12)
	if err != nil || proposal.Mode != lookModeClosed || proposal.Response != LookClosedResponse || strings.Contains(proposal.Response, "동쪽방") {
		t.Fatalf("closed=%+v err=%v", proposal, err)
	}
}

func TestPlanLookFailsClosedWhenDestinationIsUnmigrated(t *testing.T) {
	s := lookExitFixture()
	room := s.Rooms[1]
	room.Resource.Exits[0].Destination = 99
	s.Rooms[1] = room
	if _, err := s.PlanLook("a", "동", 1, 12); !errors.Is(err, ErrLookDestinationUnresolved) {
		t.Fatalf("missing dest: %v", err)
	}
}

func TestPlanLookSameRoomDestinationIsNoMap(t *testing.T) {
	s := lookExitFixture()
	room := s.Rooms[1]
	room.Resource.Exits[0].Destination = 1
	s.Rooms[1] = room
	proposal, err := s.PlanLook("a", "동", 1, 12)
	if err != nil || proposal.Mode != lookModeNoMap || proposal.Response != LookNoMapResponse {
		t.Fatalf("nomap=%+v err=%v", proposal, err)
	}
}

func TestPlanLookHiddenMarriageOrFamilyRoom(t *testing.T) {
	s := lookExitFixture()
	dest := s.Rooms[2]
	dest.Resource.Flags[lookRoomNoMarryFlag/8] |= 1 << (lookRoomNoMarryFlag % 8)
	s.Rooms[2] = dest
	proposal, err := s.PlanLook("a", "동", 1, 12)
	if err != nil || proposal.Mode != lookModeHidden || proposal.Response != LookHiddenResponse {
		t.Fatalf("ronmar=%+v err=%v", proposal, err)
	}
	s = lookExitFixture()
	dest = s.Rooms[2]
	dest.Resource.Flags[lookRoomNoFamilyFlag/8] |= 1 << (lookRoomNoFamilyFlag % 8)
	s.Rooms[2] = dest
	proposal, err = s.PlanLook("a", "동", 1, 12)
	if err != nil || proposal.Mode != lookModeHidden {
		t.Fatalf("ronfml=%+v err=%v", proposal, err)
	}
}

func TestPlanLookBlindTargetDoesNotResolveExit(t *testing.T) {
	s := lookExitFixture()
	actor := s.Players["a"]
	actor.Body.Flags[lookBlindFlag/8] |= 1 << (lookBlindFlag % 8)
	s.Players["a"] = actor
	proposal, err := s.PlanLook("a", "없는출구", 1, 12)
	if err != nil || proposal.Mode != lookModeBlind || proposal.Response != LookBlindResponse {
		t.Fatalf("blind=%+v err=%v", proposal, err)
	}
}

func TestPlanLookSecretExitIsPeekableByName(t *testing.T) {
	s := lookExitFixture()
	proposal, err := s.PlanLook("a", "비밀", 1, 12)
	if err != nil || proposal.Mode != lookModePeek || !strings.Contains(proposal.Response, "비밀방") {
		t.Fatalf("secret find_ext=%+v err=%v", proposal, err)
	}
	if _, err := s.PlanLook("a", "숨은길", 1, 12); !errors.Is(err, ErrLookExitUnresolved) {
		t.Fatalf("invisible exit without PDINVI: %v", err)
	}
	actor := s.Players["a"]
	actor.Body.Flags[lookDetectInvisibleFlag/8] |= 1 << (lookDetectInvisibleFlag % 8)
	s.Players["a"] = actor
	proposal, err = s.PlanLook("a", "숨은길", 1, 12)
	if err != nil || proposal.Mode != lookModePeek || !strings.Contains(proposal.Response, "비밀방") {
		t.Fatalf("detected invisible=%+v err=%v", proposal, err)
	}
}

func TestPlanLookCreatureOrObjectTargetFailsClosed(t *testing.T) {
	s := lookExitFixture()
	if _, err := s.PlanLook("a", "늑대", 1, 12); !errors.Is(err, ErrLookExitUnresolved) || !errors.Is(err, ErrLookObjectCreatureUnmigrated) {
		t.Fatalf("creature look not fail-closed: %v", err)
	}
	if _, err := s.PlanLook("a", "검", 1, 12); !errors.Is(err, ErrLookExitUnresolved) || !errors.Is(err, ErrLookObjectCreatureUnmigrated) {
		t.Fatalf("missing object look not fail-closed: %v", err)
	}
}

func lookObjectCreatureFixture() State {
	s := lookExitFixture()
	room := s.Rooms[1]
	room.Items = &ItemCollection{
		Items: map[string]Item{
			"floor-sword": {Object: LegacyObject{Name: "검", Description: "빛나는 검.", Type: 13, Keys: [3]string{"칼"}}},
			"floor-staff": {Object: LegacyObject{Name: "지팡이", Type: 13, Keys: [3]string{"막대"}}},
			"floor-blade": {Object: LegacyObject{Name: "검날", Description: "무딘 검날.", Type: 1, ShotsCurrent: 20, ShotsMax: 20}},
		},
		Inventory: []string{"floor-sword", "floor-staff", "floor-blade"},
	}
	room.NPCIDs = []string{"npc-wolf", "npc-goblin"}
	s.Rooms[1] = room
	s.NPCs = map[string]NPCState{
		"npc-wolf":   {Body: LegacyMonster{Name: "늑대", Description: "회색 늑대다.", Type: 1, RoomID: 1, HPMax: 100, HPCurrent: 100, Keys: [3]string{"wolf"}}, Items: lookEmptyItems(), Enemies: []NPCEnemy{}},
		"npc-goblin": {Body: LegacyMonster{Name: "고블린", Type: 1, RoomID: 1, HPMax: 100, HPCurrent: 100, Keys: [3]string{"goblin"}}, Items: lookEmptyItems(), Enemies: []NPCEnemy{}},
	}
	return s
}

func TestPlanLookInspectsRoomObjectWithoutMoving(t *testing.T) {
	s := lookObjectCreatureFixture()
	original := s
	proposal, err := s.PlanLook("a", "검", 1, 12)
	if err != nil || proposal.Mode != lookModeObject || proposal.TargetID != "floor-sword" || proposal.TargetName != "검" || s.Players["a"].Body.RoomID != 1 {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	if proposal.Response != "빛나는 검.\r\n" || proposal.Changed {
		t.Fatalf("object response=%q", proposal.Response)
	}
	next, result, err := s.ApplyLook(proposal)
	if err != nil || result.Response != proposal.Response || result.TargetKind != "object" || next.Players["a"].Body.RoomID != 1 || !reflect.DeepEqual(s, original) {
		t.Fatalf("apply mutated look: result=%+v err=%v", result, err)
	}
}

func TestPlanLookInspectsRoomCreatureWithoutMoving(t *testing.T) {
	s := lookObjectCreatureFixture()
	proposal, err := s.PlanLook("a", "늑대", 1, 12)
	if err != nil || proposal.Mode != lookModeCreature || proposal.TargetID != "npc-wolf" || proposal.TargetName != "늑대" {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	if proposal.Response != "당신은 늑대를 봅니다.\r\n회색 늑대다.\r\n그녀는 당신과 꼭 맞는 상대입니다!\r\n" || proposal.Changed {
		t.Fatalf("creature response=%q", proposal.Response)
	}
	next, result, err := s.ApplyLook(proposal)
	if err != nil || next.Players["a"].Body.RoomID != 1 || result.TargetKind != "npc" {
		t.Fatalf("creature look moved actor: result=%+v err=%v", result, err)
	}
	plain, err := s.PlanLook("a", "고블린", 1, 12)
	if err != nil || plain.Response != "당신은 고블린을 봅니다.\r\n"+LookCreaturePlainResponse+"그녀는 당신과 꼭 맞는 상대입니다!\r\n" {
		t.Fatalf("plain creature=%+v err=%v", plain, err)
	}
}

func TestPlanLookObjectBeatsCreatureOnSharedPrefix(t *testing.T) {
	s := lookObjectCreatureFixture()
	room := s.Rooms[1]
	room.Items.Items["floor-wolf-hide"] = Item{Object: LegacyObject{Name: "늑대가죽", Type: 13}}
	room.Items.Inventory = append([]string{"floor-wolf-hide"}, room.Items.Inventory...)
	s.Rooms[1] = room
	proposal, err := s.PlanLook("a", "늑대", 1, 12)
	if err != nil || proposal.Mode != lookModeObject || proposal.TargetID != "floor-wolf-hide" {
		t.Fatalf("object should win find_obj before find_crt: %+v err=%v", proposal, err)
	}
}

func TestPlanLookObjectPrefixOccurrenceUsesFindObjOrder(t *testing.T) {
	s := lookObjectCreatureFixture()
	first, err := s.PlanLook("a", "검", 1, 12)
	if err != nil || first.TargetID != "floor-sword" {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := s.PlanLook("a", "검", 2, 12)
	if err != nil || second.TargetID != "floor-blade" || !strings.Contains(second.Response, "무딘 검날.") || !strings.Contains(second.Response, "검날은 매우 공격적인 '검'입니다.") {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	key, err := s.PlanLook("a", "칼", 1, 12)
	if err != nil || key.TargetID != "floor-sword" {
		t.Fatalf("key prefix=%+v err=%v", key, err)
	}
	if _, err := s.PlanLook("a", "검", 3, 12); !errors.Is(err, ErrLookObjectCreatureUnmigrated) {
		t.Fatalf("missing object occurrence: %v", err)
	}
}

func TestPlanLookCreaturePrefixOccurrenceUsesFindCrtOrder(t *testing.T) {
	s := lookObjectCreatureFixture()
	first, err := s.PlanLook("a", "g", 1, 12)
	if err != nil || first.TargetID != "npc-goblin" {
		t.Fatalf("first crt=%+v err=%v", first, err)
	}
	wolf := s.NPCs["npc-wolf"]
	wolf.Body.Name = "늑대"
	wolf.Body.Keys = [3]string{"gob"}
	s.NPCs["npc-wolf"] = wolf
	first, err = s.PlanLook("a", "gob", 1, 12)
	if err != nil || first.TargetID != "npc-wolf" {
		t.Fatalf("key first=%+v err=%v", first, err)
	}
	second, err := s.PlanLook("a", "gob", 2, 12)
	if err != nil || second.TargetID != "npc-goblin" {
		t.Fatalf("key second=%+v err=%v", second, err)
	}
	if _, err := s.PlanLook("a", "gob", 3, 12); !errors.Is(err, ErrLookObjectCreatureUnmigrated) {
		t.Fatalf("missing creature occurrence: %v", err)
	}
}

func TestPlanLookInvisibleObjectAndCreatureRequireDetect(t *testing.T) {
	s := lookObjectCreatureFixture()
	item := s.Rooms[1].Items.Items["floor-sword"]
	item.Object.Flags[objectInvisibleFlag/8] |= 1 << (objectInvisibleFlag % 8)
	s.Rooms[1].Items.Items["floor-sword"] = item
	if _, err := s.PlanLook("a", "검", 1, 12); err != nil {
		t.Fatalf("visible 검날 should remain selectable: %v", err)
	}
	room := s.Rooms[1]
	delete(room.Items.Items, "floor-blade")
	room.Items.Inventory = []string{"floor-sword", "floor-staff"}
	s.Rooms[1] = room
	if _, err := s.PlanLook("a", "검", 1, 12); !errors.Is(err, ErrLookObjectCreatureUnmigrated) {
		t.Fatalf("invisible object without PDINVI: %v", err)
	}
	actor := s.Players["a"]
	actor.Body.Flags[lookDetectInvisibleFlag/8] |= 1 << (lookDetectInvisibleFlag % 8)
	s.Players["a"] = actor
	proposal, err := s.PlanLook("a", "검", 1, 12)
	if err != nil || proposal.TargetID != "floor-sword" {
		t.Fatalf("detected object=%+v err=%v", proposal, err)
	}

	s = lookObjectCreatureFixture()
	wolf := s.NPCs["npc-wolf"]
	wolf.Body.Flags[playerInvisibleFlag/8] |= 1 << (playerInvisibleFlag % 8)
	s.NPCs["npc-wolf"] = wolf
	if _, err := s.PlanLook("a", "늑대", 1, 12); !errors.Is(err, ErrLookObjectCreatureUnmigrated) {
		t.Fatalf("invisible creature without PDINVI: %v", err)
	}
	actor = s.Players["a"]
	actor.Body.Flags[lookDetectInvisibleFlag/8] |= 1 << (lookDetectInvisibleFlag % 8)
	s.Players["a"] = actor
	proposal, err = s.PlanLook("a", "늑대", 1, 12)
	if err != nil || proposal.TargetID != "npc-wolf" {
		t.Fatalf("detected creature=%+v err=%v", proposal, err)
	}
}

func TestPlanLookUnmigratedLegacyObjectOrMonsterFailsClosed(t *testing.T) {
	s := lookExitFixture()
	room := s.Rooms[1]
	room.Items = nil
	room.Resource.Objects = []LegacyObject{{Name: "유물"}}
	s.Rooms[1] = room
	if _, err := s.PlanLook("a", "유물", 1, 12); !errors.Is(err, ErrLookObjectCreatureUnmigrated) || !errors.Is(err, ErrCanonicalRoomObjectUnavailable) {
		t.Fatalf("legacy floor object: %v", err)
	}

	s = lookExitFixture()
	room = s.Rooms[1]
	room.Resource.Monsters = []LegacyMonster{{Name: "늑대", Type: 1}}
	s.Rooms[1] = room
	if _, err := s.PlanLook("a", "늑대", 1, 12); !errors.Is(err, ErrLookObjectCreatureUnmigrated) {
		t.Fatalf("legacy monster: %v", err)
	}
}

func TestPlanLookSpecialObjectFailsClosed(t *testing.T) {
	s := lookObjectCreatureFixture()
	item := s.Rooms[1].Items.Items["floor-sword"]
	item.Object.Special = 1
	s.Rooms[1].Items.Items["floor-sword"] = item
	if _, err := s.PlanLook("a", "검", 1, 12); !errors.Is(err, ErrLookObjectCreatureUnmigrated) {
		t.Fatalf("special object accepted: %v", err)
	}
}

func TestPlanLookInspectsInventoryObjectWithoutMoving(t *testing.T) {
	s := lookObjectCreatureFixture()
	original := s
	actor := s.Players["a"]
	actor.Items = &ItemCollection{
		Items:     map[string]Item{"inv-gem": {Object: LegacyObject{Name: "보석", Description: "품은 보석.", Type: 13}}},
		Inventory: []string{"inv-gem"},
	}
	s.Players["a"] = actor
	proposal, err := s.PlanLook("a", "보석", 1, 12)
	if err != nil || proposal.Mode != lookModeObject || proposal.TargetID != "inv-gem" || proposal.TargetName != "보석" || s.Players["a"].Body.RoomID != 1 {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	if proposal.Response != "품은 보석.\r\n" || proposal.Changed {
		t.Fatalf("inventory response=%q", proposal.Response)
	}
	next, result, err := s.ApplyLook(proposal)
	if err != nil || result.TargetID != "inv-gem" || next.Players["a"].Body.RoomID != 1 || !reflect.DeepEqual(s, original) {
		t.Fatalf("apply mutated inventory look: result=%+v err=%v", result, err)
	}
}

func TestPlanLookInventoryBeatsReadyAndRoomOnSharedPrefix(t *testing.T) {
	s := lookObjectCreatureFixture()
	actor := s.Players["a"]
	actor.Items = &ItemCollection{
		Items: map[string]Item{
			"inv-sword":  {Object: LegacyObject{Name: "검", Description: "품은 검.", Type: 13}},
			"wear-sword": {Object: LegacyObject{Name: "검", Description: "찬 검.", Type: 13}},
		},
		Inventory: []string{"inv-sword"},
		Ready:     [20]string{19: "wear-sword"},
	}
	s.Players["a"] = actor
	proposal, err := s.PlanLook("a", "검", 1, 12)
	if err != nil || proposal.TargetID != "inv-sword" || !strings.Contains(proposal.Response, "품은 검.") {
		t.Fatalf("inventory should win find_obj before ready and room: %+v err=%v", proposal, err)
	}
}

func TestPlanLookReadyBeatsRoomOnSharedPrefix(t *testing.T) {
	s := lookObjectCreatureFixture()
	actor := s.Players["a"]
	actor.Items = &ItemCollection{
		Items: map[string]Item{
			"wear-sword": {Object: LegacyObject{Name: "검", Description: "찬 검.", Type: 13}},
		},
		Ready: [20]string{19: "wear-sword"},
	}
	s.Players["a"] = actor
	proposal, err := s.PlanLook("a", "검", 1, 12)
	if err != nil || proposal.TargetID != "wear-sword" || !strings.Contains(proposal.Response, "찬 검.") {
		t.Fatalf("ready should win find_obj before room: %+v err=%v", proposal, err)
	}
	key, err := s.PlanLook("a", "칼", 1, 12)
	if err != nil || key.TargetID != "floor-sword" {
		t.Fatalf("ready miss should still reach room keys: %+v err=%v", key, err)
	}
}

func TestPlanLookReadyOccurrenceUsesWearOrder(t *testing.T) {
	s := lookObjectCreatureFixture()
	actor := s.Players["a"]
	actor.Items = &ItemCollection{
		Items: map[string]Item{
			"ring-1": {Object: LegacyObject{Name: "반지", Description: "첫 반지.", Type: 13}},
			"ring-2": {Object: LegacyObject{Name: "반지", Description: "둘째 반지.", Type: 13}},
		},
		Ready: [20]string{8: "ring-1", 9: "ring-2"},
	}
	s.Players["a"] = actor
	first, err := s.PlanLook("a", "반지", 1, 12)
	if err != nil || first.TargetID != "ring-1" {
		t.Fatalf("first ready=%+v err=%v", first, err)
	}
	second, err := s.PlanLook("a", "반지", 2, 12)
	if err != nil || second.TargetID != "ring-2" || !strings.Contains(second.Response, "둘째 반지.") {
		t.Fatalf("second ready=%+v err=%v", second, err)
	}
}

func TestPlanLookCarriedOccurrenceDoesNotSpillToLaterLists(t *testing.T) {
	s := lookObjectCreatureFixture()
	room := s.Rooms[1]
	room.Items.Items["floor-dagger"] = Item{Object: LegacyObject{Name: "단검", Description: "바닥 단검.", Type: 13}}
	room.Items.Inventory = append(room.Items.Inventory, "floor-dagger")
	s.Rooms[1] = room
	actor := s.Players["a"]
	actor.Items = &ItemCollection{
		Items: map[string]Item{
			"inv-dagger":  {Object: LegacyObject{Name: "단검", Description: "품은 단검.", Type: 13}},
			"wear-dagger": {Object: LegacyObject{Name: "단검", Description: "찬 단검.", Type: 13}},
		},
		Inventory: []string{"inv-dagger"},
		Ready:     [20]string{19: "wear-dagger"},
	}
	s.Players["a"] = actor
	first, err := s.PlanLook("a", "단검", 1, 12)
	if err != nil || first.TargetID != "inv-dagger" {
		t.Fatalf("first list=%+v err=%v", first, err)
	}
	if _, err := s.PlanLook("a", "단검", 2, 12); !errors.Is(err, ErrLookObjectCreatureUnmigrated) {
		t.Fatalf("occurrence 2 must not spill inventory/ready/room lists: %v", err)
	}
}

func TestPlanLookInvisibleInventoryRequiresDetectButReadyDoesNot(t *testing.T) {
	s := lookObjectCreatureFixture()
	inv := Item{Object: LegacyObject{Name: "검", Description: "숨은 검.", Type: 13}}
	inv.Object.Flags[objectInvisibleFlag/8] |= 1 << (objectInvisibleFlag % 8)
	wear := Item{Object: LegacyObject{Name: "지팡이", Description: "찬 지팡이.", Type: 13}}
	wear.Object.Flags[objectInvisibleFlag/8] |= 1 << (objectInvisibleFlag % 8)
	actor := s.Players["a"]
	actor.Items = &ItemCollection{
		Items:     map[string]Item{"inv-sword": inv, "wear-staff": wear},
		Inventory: []string{"inv-sword"},
		Ready:     [20]string{19: "wear-staff"},
	}
	s.Players["a"] = actor
	floor, err := s.PlanLook("a", "검", 1, 12)
	if err != nil || floor.TargetID != "floor-sword" {
		t.Fatalf("invisible inventory without PDINVI should fall through to room: %+v err=%v", floor, err)
	}
	ready, err := s.PlanLook("a", "지팡이", 1, 12)
	if err != nil || ready.TargetID != "wear-staff" || !strings.Contains(ready.Response, "찬 지팡이.") {
		t.Fatalf("ready EQUAL does not skip OINVIS: %+v err=%v", ready, err)
	}
	actor.Body.Flags[lookDetectInvisibleFlag/8] |= 1 << (lookDetectInvisibleFlag % 8)
	s.Players["a"] = actor
	detected, err := s.PlanLook("a", "검", 1, 12)
	if err != nil || detected.TargetID != "inv-sword" {
		t.Fatalf("detected inventory=%+v err=%v", detected, err)
	}
}

func TestPlanLookUnmigratedPlayerInventoryFailsClosed(t *testing.T) {
	s := lookObjectCreatureFixture()
	actor := s.Players["a"]
	actor.Items = nil
	actor.Body.Inventory = []LegacyObject{{Name: "보석"}}
	s.Players["a"] = actor
	if _, err := s.PlanLook("a", "보석", 1, 12); !errors.Is(err, ErrLookObjectCreatureUnmigrated) || !errors.Is(err, ErrCanonicalRoomObjectUnavailable) {
		t.Fatalf("legacy player inventory: %v", err)
	}
}

func TestApplyLookRejectsStaleInventoryObject(t *testing.T) {
	s := lookObjectCreatureFixture()
	actor := s.Players["a"]
	actor.Items = &ItemCollection{
		Items:     map[string]Item{"inv-gem": {Object: LegacyObject{Name: "보석", Description: "품은 보석.", Type: 13}}},
		Inventory: []string{"inv-gem"},
	}
	s.Players["a"] = actor
	proposal, err := s.PlanLook("a", "보석", 1, 12)
	if err != nil {
		t.Fatal(err)
	}
	item := s.Players["a"].Items.Items["inv-gem"]
	item.Object.Description = "바뀐 보석."
	s.Players["a"].Items.Items["inv-gem"] = item
	if _, _, err := s.ApplyLook(proposal); !errors.Is(err, ErrLookStaleProposal) {
		t.Fatalf("stale inventory applied: %v", err)
	}
}

func TestApplyLookRejectsStaleObjectOrCreature(t *testing.T) {
	s := lookObjectCreatureFixture()
	proposal, err := s.PlanLook("a", "검", 1, 12)
	if err != nil {
		t.Fatal(err)
	}
	item := s.Rooms[1].Items.Items["floor-sword"]
	item.Object.Description = "바뀐 검."
	s.Rooms[1].Items.Items["floor-sword"] = item
	if _, _, err := s.ApplyLook(proposal); !errors.Is(err, ErrLookStaleProposal) {
		t.Fatalf("stale object applied: %v", err)
	}

	s = lookObjectCreatureFixture()
	proposal, err = s.PlanLook("a", "늑대", 1, 12)
	if err != nil {
		t.Fatal(err)
	}
	wolf := s.NPCs["npc-wolf"]
	wolf.Body.Description = "바뀐 늑대."
	s.NPCs["npc-wolf"] = wolf
	if _, _, err := s.ApplyLook(proposal); !errors.Is(err, ErrLookStaleProposal) {
		t.Fatalf("stale creature applied: %v", err)
	}
}

func TestApplyLookRejectsStaleDestination(t *testing.T) {
	s := lookExitFixture()
	proposal, err := s.PlanLook("a", "동", 1, 12)
	if err != nil {
		t.Fatal(err)
	}
	dest := s.Rooms[2]
	dest.Resource.Name = "바뀐방"
	s.Rooms[2] = dest
	if _, _, err := s.ApplyLook(proposal); !errors.Is(err, ErrLookStaleProposal) {
		t.Fatalf("stale dest applied: %v", err)
	}
}

func lookCombatNoticeFixture() State {
	s := lookExitFixture()
	room := s.Rooms[1]
	room.PlayerIDs = []string{"a", "b"}
	room.NPCIDs = []string{"npc-wolf"}
	s.Rooms[1] = room
	s.Players["b"] = PlayerState{Body: LegacyMonster{Name: "Bob", RoomID: 1}, Online: true, Items: &ItemCollection{Items: map[string]Item{}}}
	s.NPCs = map[string]NPCState{
		"npc-wolf": {Body: LegacyMonster{Name: "늑대", Description: "회색 늑대다.", Type: 1, RoomID: 1}, Enemies: []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "a"}, Damage: 0}}},
	}
	return s
}

func TestPlanLookBareRendersDisplayRomCombatNoticeWithoutMoving(t *testing.T) {
	s := lookCombatNoticeFixture()
	original := s
	proposal, err := s.PlanLook("a", "", 0, 12)
	if err != nil || proposal.Mode != lookModeHere || s.Players["a"].Body.RoomID != 1 {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	if !strings.Contains(proposal.Response, "늑대가 당신과 싸우고 있습니다.\n") || !strings.Contains(proposal.Response, "== 광장 ==") {
		t.Fatalf("bare look missing combat notice: %q", proposal.Response)
	}
	next, result, err := s.ApplyLook(proposal)
	if err != nil || result.Response != proposal.Response || next.Players["a"].Body.RoomID != 1 || !reflect.DeepEqual(s, original) {
		t.Fatalf("apply mutated combat look: result=%+v err=%v", result, err)
	}
	replay, err := s.PlanLook("a", "", 0, 12)
	if err != nil || replay.Response != proposal.Response {
		t.Fatalf("replay rerendered: %q err=%v", replay.Response, err)
	}
}

func TestPlanLookPeekRendersDestinationCombatNoticeWithoutMoving(t *testing.T) {
	s := lookCombatNoticeFixture()
	dest := s.Rooms[2]
	dest.PlayerIDs = []string{"c"}
	dest.NPCIDs = []string{"npc-guard"}
	s.Rooms[2] = dest
	s.Players["c"] = PlayerState{Body: LegacyMonster{Name: "Carol", RoomID: 2}, Online: true, Items: &ItemCollection{Items: map[string]Item{}}}
	s.NPCs["npc-guard"] = NPCState{Body: LegacyMonster{Name: "경비", Description: "경비가 서 있습니다.", Type: 1, RoomID: 2}, Enemies: []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "c"}, Damage: 0}}}
	proposal, err := s.PlanLook("a", "동", 1, 12)
	if err != nil || proposal.Mode != lookModePeek || s.Players["a"].Body.RoomID != 1 {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	if !strings.Contains(proposal.Response, "동쪽방") || !strings.Contains(proposal.Response, "경비가 Carol님과 싸우고 있습니다.\n") {
		t.Fatalf("peek missing dest combat: %q", proposal.Response)
	}
	if strings.Contains(proposal.Response, "늑대가 당신과") || strings.Contains(proposal.Response, "== 광장 ==") {
		t.Fatalf("peeked source combat: %q", proposal.Response)
	}
}

func TestPlanLookBareFailsClosedWhenCombatUnmigrated(t *testing.T) {
	s := lookCombatNoticeFixture()
	wolf := s.NPCs["npc-wolf"]
	wolf.Enemies = nil
	s.NPCs["npc-wolf"] = wolf
	if _, err := s.PlanLook("a", "", 0, 12); !errors.Is(err, ErrRoomCombatUnmigrated) {
		t.Fatalf("nil enemies: %v", err)
	}
	s = lookExitFixture()
	room := s.Rooms[2]
	room.Resource.Monsters = []LegacyMonster{{Name: "늑대", Type: 1}}
	s.Rooms[2] = room
	if _, err := s.PlanLook("a", "동", 1, 12); !errors.Is(err, ErrRoomCombatUnmigrated) {
		t.Fatalf("legacy dest monsters: %v", err)
	}
}

const lookPlayerStandingDesc = "바르게 "

func lookPlayerStandingWant(male bool) string {
	if male {
		return "그는 바르게 서 있습니다.\r\n"
	}
	return "그녀는 바르게 서 있습니다.\r\n"
}

func lookSelfPlyFixture() State {
	s := lookExitFixture()
	alice := s.Players["a"]
	alice.Body.Description = lookPlayerStandingDesc
	s.Players["a"] = alice
	room := s.Rooms[1]
	room.PlayerIDs = []string{"a", "b", "c"}
	s.Rooms[1] = room
	s.Players["b"] = PlayerState{Body: LegacyMonster{Name: "Bob", Description: lookPlayerStandingDesc, RoomID: 1, Keys: [3]string{"bobby"}, HPMax: 100, HPCurrent: 100}, Online: true, Items: &ItemCollection{Items: map[string]Item{}}}
	s.Players["c"] = PlayerState{Body: LegacyMonster{Name: "Carol", Description: lookPlayerStandingDesc, RoomID: 1, HPMax: 100, HPCurrent: 100}, Online: true, Items: &ItemCollection{Items: map[string]Item{}}}
	return s
}

func TestPlanLookInspectsSelfWithoutMoving(t *testing.T) {
	s := lookSelfPlyFixture()
	original := s
	for _, occurrence := range []int{1, 2} {
		proposal, err := s.PlanLook("a", "나", occurrence, 12)
		if err != nil || proposal.Mode != lookModeSelf || proposal.TargetKind != "player" || proposal.TargetID != "a" || proposal.TargetName != "Alice" || s.Players["a"].Body.RoomID != 1 {
			t.Fatalf("occurrence=%d proposal=%+v err=%v", occurrence, proposal, err)
		}
		want := LookSelfMirrorResponse + lookPlayerStandingWant(false) + "그녀는 당신과 꼭 맞는 상대입니다!\r\n"
		if proposal.Response != want || proposal.Changed {
			t.Fatalf("self response=%q changed=%v", proposal.Response, proposal.Changed)
		}
		next, result, err := s.ApplyLook(proposal)
		if err != nil || result.Response != want || result.TargetID != "a" || next.Players["a"].Body.RoomID != 1 || !reflect.DeepEqual(s, original) {
			t.Fatalf("apply mutated self look: result=%+v err=%v", result, err)
		}
	}
}

func TestPlanLookInspectsFirstPlyWithoutMoving(t *testing.T) {
	s := lookSelfPlyFixture()
	original := s
	proposal, err := s.PlanLook("a", "Bob", 1, 12)
	if err != nil || proposal.Mode != lookModePlayer || proposal.TargetKind != "player" || proposal.TargetID != "b" || proposal.TargetName != "Bob" || s.Players["a"].Body.RoomID != 1 {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	if proposal.Response != "당신은 Bob님을 봅니다.\r\n"+lookPlayerStandingWant(false) || proposal.Changed {
		t.Fatalf("player response=%q", proposal.Response)
	}
	next, result, err := s.ApplyLook(proposal)
	if err != nil || result.TargetID != "b" || next.Players["a"].Body.RoomID != 1 || next.Players["b"].Body.RoomID != 1 || !reflect.DeepEqual(s, original) {
		t.Fatalf("apply mutated first_ply look: result=%+v err=%v", result, err)
	}
	lower, err := s.PlanLook("a", "bob", 1, 12)
	if err != nil || lower.TargetID != "b" || lower.Response != proposal.Response {
		t.Fatalf("upcased first_ply=%+v err=%v", lower, err)
	}
	key, err := s.PlanLook("a", "bobby", 1, 12)
	if err != nil || key.TargetID != "b" {
		t.Fatalf("first_ply key=%+v err=%v", key, err)
	}
}

func TestPlanLookOwnNameIsFirstPlyInspectNotMirror(t *testing.T) {
	s := lookSelfPlyFixture()
	proposal, err := s.PlanLook("a", "Alice", 1, 12)
	if err != nil || proposal.Mode != lookModePlayer || proposal.TargetID != "a" || proposal.Response != "당신은 Alice님을 봅니다.\r\n"+lookPlayerStandingWant(false) {
		t.Fatalf("own-name first_ply=%+v err=%v", proposal, err)
	}
}

func TestPlanLookNaBeatsFirstMon(t *testing.T) {
	s := lookSelfPlyFixture()
	room := s.Rooms[1]
	room.NPCIDs = []string{"npc-na"}
	s.Rooms[1] = room
	s.NPCs = map[string]NPCState{
		"npc-na": {Body: LegacyMonster{Name: "나", Description: "나라는 몬스터.", Type: 1, RoomID: 1}},
	}
	proposal, err := s.PlanLook("a", "나", 1, 12)
	if err != nil || proposal.Mode != lookModeSelf || proposal.TargetID != "a" || proposal.Response != LookSelfMirrorResponse+lookPlayerStandingWant(false)+"그녀는 당신과 꼭 맞는 상대입니다!\r\n" {
		t.Fatalf("나 must skip first_mon: %+v err=%v", proposal, err)
	}
}

func TestPlanLookInventoryObjectNamedNaBeatsSelf(t *testing.T) {
	s := lookSelfPlyFixture()
	actor := s.Players["a"]
	actor.Items = &ItemCollection{
		Items:     map[string]Item{"inv-na": {Object: LegacyObject{Name: "나", Description: "나라는 물건.", Type: 13}}},
		Inventory: []string{"inv-na"},
	}
	s.Players["a"] = actor
	proposal, err := s.PlanLook("a", "나", 1, 12)
	if err != nil || proposal.Mode != lookModeObject || proposal.TargetID != "inv-na" || !strings.Contains(proposal.Response, "나라는 물건.") {
		t.Fatalf("find_obj 나 should win before self: %+v err=%v", proposal, err)
	}
}

func TestPlanLookFirstMonBeatsFirstPly(t *testing.T) {
	s := lookSelfPlyFixture()
	room := s.Rooms[1]
	room.NPCIDs = []string{"npc-bob"}
	s.Rooms[1] = room
	s.NPCs = map[string]NPCState{
		"npc-bob": {Body: LegacyMonster{Name: "Bob", Description: "가짜 밥.", Type: 1, RoomID: 1, HPMax: 100, HPCurrent: 100}, Items: lookEmptyItems(), Enemies: []NPCEnemy{}},
	}
	proposal, err := s.PlanLook("a", "Bob", 1, 12)
	if err != nil || proposal.Mode != lookModeCreature || proposal.TargetKind != "npc" || proposal.TargetID != "npc-bob" {
		t.Fatalf("first_mon should win before first_ply: %+v err=%v", proposal, err)
	}
}

func TestPlanLookFirstPlyOccurrenceUsesRoomOrder(t *testing.T) {
	s := lookSelfPlyFixture()
	s.Players["b"] = PlayerState{Body: LegacyMonster{Name: "Bobby", Description: lookPlayerStandingDesc, RoomID: 1, HPMax: 100, HPCurrent: 100}, Online: true, Items: &ItemCollection{Items: map[string]Item{}}}
	s.Players["c"] = PlayerState{Body: LegacyMonster{Name: "Bob", Description: lookPlayerStandingDesc, RoomID: 1, HPMax: 100, HPCurrent: 100}, Online: true, Items: &ItemCollection{Items: map[string]Item{}}}
	first, err := s.PlanLook("a", "Bo", 1, 12)
	if err != nil || first.TargetID != "b" || first.TargetName != "Bobby" {
		t.Fatalf("first first_ply=%+v err=%v", first, err)
	}
	second, err := s.PlanLook("a", "Bo", 2, 12)
	if err != nil || second.TargetID != "c" || second.TargetName != "Bob" {
		t.Fatalf("second first_ply=%+v err=%v", second, err)
	}
	if _, err := s.PlanLook("a", "Bo", 3, 12); !errors.Is(err, ErrLookObjectCreatureUnmigrated) {
		t.Fatalf("missing first_ply occurrence: %v", err)
	}
}

func TestPlanLookInvisiblePlayerRequiresDetect(t *testing.T) {
	s := lookSelfPlyFixture()
	bob := s.Players["b"]
	bob.Body.Flags[playerInvisibleFlag/8] |= 1 << (playerInvisibleFlag % 8)
	s.Players["b"] = bob
	if _, err := s.PlanLook("a", "Bob", 1, 12); !errors.Is(err, ErrLookObjectCreatureUnmigrated) {
		t.Fatalf("invisible first_ply without PDINVI: %v", err)
	}
	actor := s.Players["a"]
	actor.Body.Flags[lookDetectInvisibleFlag/8] |= 1 << (lookDetectInvisibleFlag % 8)
	s.Players["a"] = actor
	proposal, err := s.PlanLook("a", "Bob", 1, 12)
	if err != nil || proposal.TargetID != "b" || !strings.Contains(proposal.Response, "Bob") {
		t.Fatalf("detected first_ply=%+v err=%v", proposal, err)
	}
}

func TestPlanLookUnmigratedPlayerFailsClosed(t *testing.T) {
	s := lookSelfPlyFixture()
	bob := s.Players["b"]
	bob.Body.Name = "Bob "
	s.Players["b"] = bob
	if _, err := s.PlanLook("a", "Bob", 1, 12); !errors.Is(err, ErrLookObjectCreatureUnmigrated) {
		t.Fatalf("unmigrated first_ply name: %v", err)
	}

	s = lookSelfPlyFixture()
	if _, err := s.PlanLook("a", "유령", 1, 12); !errors.Is(err, ErrLookExitUnresolved) || !errors.Is(err, ErrLookObjectCreatureUnmigrated) {
		t.Fatalf("missing first_ply: %v", err)
	}
}

func TestApplyLookRejectsStalePlayer(t *testing.T) {
	s := lookSelfPlyFixture()
	proposal, err := s.PlanLook("a", "Bob", 1, 12)
	if err != nil {
		t.Fatal(err)
	}
	bob := s.Players["b"]
	bob.Body.Name = "Bobby"
	s.Players["b"] = bob
	if _, _, err := s.ApplyLook(proposal); !errors.Is(err, ErrLookStaleProposal) {
		t.Fatalf("stale first_ply applied: %v", err)
	}
}

func lookEmptyItems() *ItemCollection {
	return &ItemCollection{Items: map[string]Item{}}
}

func lookEquippedItems() *ItemCollection {
	return &ItemCollection{
		Items: map[string]Item{
			"helm":  {Object: LegacyObject{Name: "투구"}},
			"armor": {Object: LegacyObject{Name: "갑옷"}},
			"ring":  {Object: LegacyObject{Name: "반지"}},
			"sword": {Object: LegacyObject{Name: "검"}},
		},
		Ready: [20]string{0: "armor", 6: "helm", 8: "ring", 19: "sword"},
	}
}

const lookEquipListWant = "[ 머리 ]  투구\r\n[  몸  ]  갑옷\r\n[손가락]  반지\r\n[ 무기 ]  검\r\n"

func lookSetInvisible(body *LegacyMonster) {
	body.Flags[playerInvisibleFlag/8] |= 1 << (playerInvisibleFlag % 8)
}

func lookSetDetectInvisible(body *LegacyMonster) {
	body.Flags[lookDetectInvisibleFlag/8] |= 1 << (lookDetectInvisibleFlag % 8)
}

func lookSetMale(body *LegacyMonster) {
	body.Flags[12/8] |= 1 << (12 % 8)
}

func lookSetKnowAlignment(body *LegacyMonster) {
	body.Flags[33/8] |= 1 << (33 % 8)
}

func lookSetHP(body *LegacyMonster, hpcur, hpmax int16) {
	body.HPCurrent = hpcur
	body.HPMax = hpmax
}

func lookSetMarried(body *LegacyMonster, spouse string, male bool) {
	marriageSetFlag(&body.Flags, MarriageActiveFlag, true)
	if male {
		lookSetMale(body)
	}
	body.Keys[MarriageSpouseKeyIndex] = MarriageSpouseKeyPrefix + spouse
}

func TestPlanLookInspectsSelfAppendsConsiderAndEquipList(t *testing.T) {
	s := lookSelfPlyFixture()
	actor := s.Players["a"]
	actor.Items = lookEquippedItems()
	s.Players["a"] = actor
	original := s
	proposal, err := s.PlanLook("a", "나", 1, 12)
	if err != nil || proposal.Mode != lookModeSelf || proposal.TargetID != "a" || proposal.Changed || s.Players["a"].Body.RoomID != 1 {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	want := LookSelfMirrorResponse + lookPlayerStandingWant(false) + "그녀는 당신과 꼭 맞는 상대입니다!\r\n" + lookEquipListWant
	if proposal.Response != want {
		t.Fatalf("self consider/equip=%q want %q", proposal.Response, want)
	}
	next, result, err := s.ApplyLook(proposal)
	if err != nil || result.Response != want || next.Players["a"].Body.RoomID != 1 || !reflect.DeepEqual(s, original) {
		t.Fatalf("apply mutated self consider: result=%+v err=%v", result, err)
	}
}

func TestPlanLookInspectsCreatureAppendsConsiderAndEquipList(t *testing.T) {
	s := lookObjectCreatureFixture()
	wolf := s.NPCs["npc-wolf"]
	wolf.Items = lookEquippedItems()
	s.NPCs["npc-wolf"] = wolf
	original := s
	proposal, err := s.PlanLook("a", "늑대", 1, 12)
	if err != nil || proposal.Mode != lookModeCreature || proposal.TargetID != "npc-wolf" || proposal.Changed || s.Players["a"].Body.RoomID != 1 {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	want := "당신은 늑대를 봅니다.\r\n회색 늑대다.\r\n그녀는 당신과 꼭 맞는 상대입니다!\r\n" + lookEquipListWant
	if proposal.Response != want {
		t.Fatalf("creature consider/equip=%q want %q", proposal.Response, want)
	}
	next, result, err := s.ApplyLook(proposal)
	if err != nil || result.Response != want || next.Players["a"].Body.RoomID != 1 || !reflect.DeepEqual(s, original) {
		t.Fatalf("apply mutated creature consider: result=%+v err=%v", result, err)
	}
}

func TestPlanLookInspectsFirstPlyAppendsEquipListWithoutConsider(t *testing.T) {
	s := lookSelfPlyFixture()
	actor := s.Players["a"]
	actor.Body.Level = 20
	s.Players["a"] = actor
	bob := s.Players["b"]
	bob.Body.Level = 1
	bob.Items = lookEquippedItems()
	s.Players["b"] = bob
	original := s
	proposal, err := s.PlanLook("a", "Bob", 1, 12)
	if err != nil || proposal.Mode != lookModePlayer || proposal.TargetID != "b" || s.Players["a"].Body.RoomID != 1 {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	want := "당신은 Bob님을 봅니다.\r\n" + lookPlayerStandingWant(false) + lookEquipListWant
	if proposal.Response != want {
		t.Fatalf("first_ply equip=%q want %q", proposal.Response, want)
	}
	if strings.Contains(proposal.Response, "꼭 맞는") || strings.Contains(proposal.Response, "한방에") || strings.Contains(proposal.Response, "쨉도") {
		t.Fatalf("first_ply must not consider: %q", proposal.Response)
	}
	next, result, err := s.ApplyLook(proposal)
	if err != nil || result.Response != want || next.Players["a"].Body.RoomID != 1 || next.Players["b"].Body.RoomID != 1 || !reflect.DeepEqual(s, original) {
		t.Fatalf("apply mutated first_ply equip: result=%+v err=%v", result, err)
	}
}

func TestPlanLookConsiderDiffUsesLevelQuartersClamped(t *testing.T) {
	for _, tt := range []struct {
		actor, target byte
		male          bool
		want          string
	}{
		{4, 4, false, "그녀는 당신과 꼭 맞는 상대입니다!\r\n"},
		{8, 4, true, "그는 별 무리없이 이길 수 있습니다.\r\n"},
		{4, 8, false, "그녀는 운이 좋으면 이길 수 있습니다..\r\n"},
		{12, 4, false, "그녀는 별로 힘 안들이고 이길수 있습니다.\r\n"},
		{4, 12, true, "그는 상대하기 힘들겠는데요?\r\n"},
		{16, 4, false, "그녀는 손쉽게 상대할수 있습니다.\r\n"},
		{4, 16, false, "당신은 그녀에게 쨉도 안됩니다.\r\n"},
		{20, 4, true, "그는 한방에 보낼수 있습니다.\r\n"},
		{4, 20, false, "그녀는 보자마자 도망가는것이 좋을겁니다.\r\n"},
		{24, 4, false, "그녀는 한방에 보낼수 있습니다.\r\n"},
		{0, 255, false, "그녀는 보자마자 도망가는것이 좋을겁니다.\r\n"},
	} {
		s := lookExitFixture()
		actor := s.Players["a"]
		actor.Body.Level = tt.actor
		s.Players["a"] = actor
		room := s.Rooms[1]
		room.NPCIDs = []string{"npc-wolf"}
		s.Rooms[1] = room
		body := LegacyMonster{Name: "늑대", Type: 1, RoomID: 1, Level: tt.target, HPMax: 100, HPCurrent: 100}
		if tt.male {
			lookSetMale(&body)
		}
		s.NPCs = map[string]NPCState{"npc-wolf": {Body: body, Items: lookEmptyItems(), Enemies: []NPCEnemy{}}}
		proposal, err := s.PlanLook("a", "늑대", 1, 12)
		if err != nil || s.Players["a"].Body.RoomID != 1 {
			t.Fatalf("actor=%d target=%d proposal=%+v err=%v", tt.actor, tt.target, proposal, err)
		}
		if !strings.HasSuffix(proposal.Response, tt.want) || strings.Contains(proposal.Response, "[") {
			t.Fatalf("actor=%d target=%d response=%q want suffix %q", tt.actor, tt.target, proposal.Response, tt.want)
		}
	}
}

func TestPlanLookNilItemsFailsClosedOnInspect(t *testing.T) {
	s := lookObjectCreatureFixture()
	wolf := s.NPCs["npc-wolf"]
	wolf.Items = nil
	s.NPCs["npc-wolf"] = wolf
	if _, err := s.PlanLook("a", "늑대", 1, 12); !errors.Is(err, ErrLookObjectCreatureUnmigrated) {
		t.Fatalf("nil NPC items: %v", err)
	}

	s = lookSelfPlyFixture()
	actor := s.Players["a"]
	actor.Items = nil
	s.Players["a"] = actor
	if _, err := s.PlanLook("a", "나", 1, 12); !errors.Is(err, ErrLookObjectCreatureUnmigrated) {
		t.Fatalf("nil self items: %v", err)
	}

	s = lookSelfPlyFixture()
	bob := s.Players["b"]
	bob.Items = nil
	s.Players["b"] = bob
	if _, err := s.PlanLook("a", "Bob", 1, 12); !errors.Is(err, ErrLookObjectCreatureUnmigrated) {
		t.Fatalf("nil first_ply items: %v", err)
	}
}

func TestPlanLookUnmigratedReadyNameFailsClosed(t *testing.T) {
	s := lookSelfPlyFixture()
	bob := s.Players["b"]
	bob.Items = &ItemCollection{
		Items: map[string]Item{"helm": {Object: LegacyObject{Name: "투구 "}}},
		Ready: [20]string{6: "helm"},
	}
	s.Players["b"] = bob
	if _, err := s.PlanLook("a", "Bob", 1, 12); !errors.Is(err, ErrLookObjectCreatureUnmigrated) {
		t.Fatalf("unmigrated ready name: %v", err)
	}
}

func TestApplyLookRejectsStaleEquipList(t *testing.T) {
	s := lookSelfPlyFixture()
	bob := s.Players["b"]
	bob.Items = lookEquippedItems()
	s.Players["b"] = bob
	proposal, err := s.PlanLook("a", "Bob", 1, 12)
	if err != nil {
		t.Fatal(err)
	}
	bob = s.Players["b"]
	bob.Items = lookEmptyItems()
	s.Players["b"] = bob
	if _, _, err := s.ApplyLook(proposal); !errors.Is(err, ErrLookStaleProposal) {
		t.Fatalf("stale equip applied: %v", err)
	}
}

func TestPlanLookInspectsSelfAppendsHPBandBeforeConsider(t *testing.T) {
	s := lookSelfPlyFixture()
	actor := s.Players["a"]
	lookSetHP(&actor.Body, 89, 100)
	actor.Items = lookEquippedItems()
	s.Players["a"] = actor
	original := s
	proposal, err := s.PlanLook("a", "나", 1, 12)
	if err != nil || proposal.Mode != lookModeSelf || proposal.Changed || s.Players["a"].Body.RoomID != 1 {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	want := LookSelfMirrorResponse + lookPlayerStandingWant(false) + "그녀는 가벼운 상처를 입었습니다.\r\n그녀는 당신과 꼭 맞는 상대입니다!\r\n" + lookEquipListWant
	if proposal.Response != want {
		t.Fatalf("self hp band=%q want %q", proposal.Response, want)
	}
	next, result, err := s.ApplyLook(proposal)
	if err != nil || result.Response != want || next.Players["a"].Body.RoomID != 1 || !reflect.DeepEqual(s, original) {
		t.Fatalf("apply mutated self hp: result=%+v err=%v", result, err)
	}
}

func TestPlanLookInspectsCreatureAppendsCHealthBands(t *testing.T) {
	for _, tt := range []struct {
		hpcur, hpmax int16
		male         bool
		band         string
	}{
		{100, 100, false, ""},
		{90, 100, false, ""},
		{89, 100, false, "그녀는 가벼운 상처를 입었습니다.\r\n"},
		{81, 100, true, "그는 가벼운 상처를 입었습니다.\r\n"},
		{80, 100, false, ""},
		{79, 100, false, "그녀는 여러군데 상처를 입었습니다.\r\n"},
		{61, 100, false, "그녀는 여러군데 상처를 입었습니다.\r\n"},
		{60, 100, false, ""},
		{59, 100, false, "그녀는 많은 상처를 입었습니다.\r\n"},
		{41, 100, false, "그녀는 많은 상처를 입었습니다.\r\n"},
		{40, 100, false, ""},
		{39, 100, false, "그녀는 심각한 상처를 입었습니다.\r\n"},
		{21, 100, false, "그녀는 심각한 상처를 입었습니다.\r\n"},
		{20, 100, false, ""},
		{19, 100, false, "그녀는 죽기 직전입니다.\r\n"},
		{0, 100, true, "그는 죽기 직전입니다.\r\n"},
		{-1, 100, false, "그녀는 죽기 직전입니다.\r\n"},
		{7, 10, false, "그녀는 여러군데 상처를 입었습니다.\r\n"},
		{9, 10, false, ""},
		{8, 10, false, ""},
		{1, 10, false, "그녀는 죽기 직전입니다.\r\n"},
	} {
		s := lookObjectCreatureFixture()
		wolf := s.NPCs["npc-wolf"]
		lookSetHP(&wolf.Body, tt.hpcur, tt.hpmax)
		if tt.male {
			lookSetMale(&wolf.Body)
		}
		s.NPCs["npc-wolf"] = wolf
		original := s
		proposal, err := s.PlanLook("a", "늑대", 1, 12)
		if err != nil || proposal.Mode != lookModeCreature || proposal.Changed || s.Players["a"].Body.RoomID != 1 {
			t.Fatalf("hpcur=%d hpmax=%d proposal=%+v err=%v", tt.hpcur, tt.hpmax, proposal, err)
		}
		he := "그녀"
		if tt.male {
			he = "그"
		}
		want := "당신은 늑대를 봅니다.\r\n회색 늑대다.\r\n" + tt.band + he + "는 당신과 꼭 맞는 상대입니다!\r\n"
		if proposal.Response != want {
			t.Fatalf("hpcur=%d hpmax=%d response=%q want %q", tt.hpcur, tt.hpmax, proposal.Response, want)
		}
		next, result, err := s.ApplyLook(proposal)
		if err != nil || result.Response != want || next.Players["a"].Body.RoomID != 1 || !reflect.DeepEqual(s, original) {
			t.Fatalf("hpcur=%d hpmax=%d apply mutated: result=%+v err=%v", tt.hpcur, tt.hpmax, result, err)
		}
	}
}

func TestPlanLookInspectsFirstPlyAppendsThreeTenthsHP(t *testing.T) {
	for _, tt := range []struct {
		hpcur, hpmax int16
		male         bool
		band         string
	}{
		{100, 100, false, ""},
		{30, 100, false, ""},
		{29, 100, false, "그녀는 가벼운 상처를 입었습니다.\r\n"},
		{10, 100, true, "그는 가벼운 상처를 입었습니다.\r\n"},
		{0, 100, false, "그녀는 가벼운 상처를 입었습니다.\r\n"},
		{89, 100, false, ""},
		{50, 100, false, ""},
		{19, 100, true, "그는 가벼운 상처를 입었습니다.\r\n"},
		{3, 10, false, ""},
		{2, 10, false, "그녀는 가벼운 상처를 입었습니다.\r\n"},
	} {
		s := lookSelfPlyFixture()
		bob := s.Players["b"]
		lookSetHP(&bob.Body, tt.hpcur, tt.hpmax)
		if tt.male {
			lookSetMale(&bob.Body)
		}
		bob.Items = lookEquippedItems()
		s.Players["b"] = bob
		original := s
		proposal, err := s.PlanLook("a", "Bob", 1, 12)
		if err != nil || proposal.Mode != lookModePlayer || proposal.Changed || s.Players["a"].Body.RoomID != 1 {
			t.Fatalf("hpcur=%d hpmax=%d proposal=%+v err=%v", tt.hpcur, tt.hpmax, proposal, err)
		}
		want := "당신은 Bob님을 봅니다.\r\n" + lookPlayerStandingWant(tt.male) + tt.band + lookEquipListWant
		if proposal.Response != want {
			t.Fatalf("hpcur=%d hpmax=%d response=%q want %q", tt.hpcur, tt.hpmax, proposal.Response, want)
		}
		if strings.Contains(proposal.Response, "여러군데") || strings.Contains(proposal.Response, "많은 상처") || strings.Contains(proposal.Response, "심각한") || strings.Contains(proposal.Response, "죽기 직전") {
			t.Fatalf("first_ply must not emit first_mon HP bands: %q", proposal.Response)
		}
		next, result, err := s.ApplyLook(proposal)
		if err != nil || result.Response != want || next.Players["a"].Body.RoomID != 1 || next.Players["b"].Body.RoomID != 1 || !reflect.DeepEqual(s, original) {
			t.Fatalf("hpcur=%d hpmax=%d apply mutated: result=%+v err=%v", tt.hpcur, tt.hpmax, result, err)
		}
	}
}

func TestPlanLookInspectsFirstPlyAppendsMarriageBeforeEquip(t *testing.T) {
	s := lookSelfPlyFixture()
	bob := s.Players["b"]
	lookSetHP(&bob.Body, 100, 100)
	lookSetMarried(&bob.Body, "Carol", true)
	bob.Items = lookEquippedItems()
	s.Players["b"] = bob
	original := s
	proposal, err := s.PlanLook("a", "Bob", 1, 12)
	if err != nil || proposal.Mode != lookModePlayer || proposal.TargetID != "b" || proposal.Changed || s.Players["a"].Body.RoomID != 1 {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	want := "당신은 Bob님을 봅니다.\r\n그는 Carol님과 결혼한 기혼자입니다.\r\n" + lookPlayerStandingWant(true) + lookEquipListWant
	if proposal.Response != want {
		t.Fatalf("first_ply marriage=%q want %q", proposal.Response, want)
	}
	if strings.Contains(proposal.Response, "상처") || strings.Contains(proposal.Response, "죽기 직전") || strings.Contains(proposal.Response, "꼭 맞는") || strings.Contains(proposal.Response, "광채") {
		t.Fatalf("first_ply must not emit first_mon HP/consider/PKNOWA: %q", proposal.Response)
	}
	next, result, err := s.ApplyLook(proposal)
	if err != nil || result.Response != want || next.Players["a"].Body.RoomID != 1 || next.Players["b"].Body.RoomID != 1 || !reflect.DeepEqual(s, original) {
		t.Fatalf("apply mutated first_ply marriage: result=%+v err=%v", result, err)
	}
}

func TestPlanLookInspectsFirstPlyMarriageUsesPMALES(t *testing.T) {
	s := lookSelfPlyFixture()
	bob := s.Players["b"]
	lookSetMarried(&bob.Body, "Carol", false)
	s.Players["b"] = bob
	proposal, err := s.PlanLook("a", "Bob", 1, 12)
	if err != nil || s.Players["a"].Body.RoomID != 1 {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	want := "당신은 Bob님을 봅니다.\r\n그녀는 Carol님과 결혼한 기혼자입니다.\r\n" + lookPlayerStandingWant(false)
	if proposal.Response != want {
		t.Fatalf("female first_ply marriage=%q want %q", proposal.Response, want)
	}
}

func TestPlanLookFirstPlyPendingMarriageDoesNotPrintLine(t *testing.T) {
	s := lookSelfPlyFixture()
	bob := s.Players["b"]
	marriageSetFlag(&bob.Body.Flags, MarriagePendingFlag, true)
	bob.Body.Keys[MarriageSpouseKeyIndex] = MarriageSpouseKeyPrefix + "Carol"
	s.Players["b"] = bob
	proposal, err := s.PlanLook("a", "Bob", 1, 12)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Response != "당신은 Bob님을 봅니다.\r\n"+lookPlayerStandingWant(false) || strings.Contains(proposal.Response, "기혼자") {
		t.Fatalf("pending marriage printed: %q", proposal.Response)
	}
}

func TestPlanLookFirstPlyMarriedUnmigratedSpouseFailsClosed(t *testing.T) {
	s := lookSelfPlyFixture()
	bob := s.Players["b"]
	marriageSetFlag(&bob.Body.Flags, MarriageActiveFlag, true)
	s.Players["b"] = bob
	if _, err := s.PlanLook("a", "Bob", 1, 12); !errors.Is(err, ErrLookObjectCreatureUnmigrated) {
		t.Fatalf("PMARRI without spouse: %v", err)
	}

	s = lookSelfPlyFixture()
	bob = s.Players["b"]
	marriageSetFlag(&bob.Body.Flags, MarriageActiveFlag, true)
	bob.Body.Keys[MarriageSpouseKeyIndex] = "Carol"
	s.Players["b"] = bob
	if _, err := s.PlanLook("a", "Bob", 1, 12); !errors.Is(err, ErrLookObjectCreatureUnmigrated) {
		t.Fatalf("PMARRI without m-prefix: %v", err)
	}
}

func TestApplyLookRejectsStaleFirstPlyMarriage(t *testing.T) {
	s := lookSelfPlyFixture()
	bob := s.Players["b"]
	lookSetMarried(&bob.Body, "Carol", true)
	s.Players["b"] = bob
	proposal, err := s.PlanLook("a", "Bob", 1, 12)
	if err != nil {
		t.Fatal(err)
	}
	bob = s.Players["b"]
	bob.Body.Keys[MarriageSpouseKeyIndex] = MarriageSpouseKeyPrefix + "Dave"
	s.Players["b"] = bob
	if _, _, err := s.ApplyLook(proposal); !errors.Is(err, ErrLookStaleProposal) {
		t.Fatalf("stale marriage applied: %v", err)
	}
}

func TestPlanLookInspectsFirstPlyAppendsStandingDescriptionBeforeEquip(t *testing.T) {
	s := lookSelfPlyFixture()
	bob := s.Players["b"]
	lookSetMarried(&bob.Body, "Carol", true)
	bob.Items = lookEquippedItems()
	s.Players["b"] = bob
	original := s
	proposal, err := s.PlanLook("a", "Bob", 1, 12)
	if err != nil || proposal.Mode != lookModePlayer || proposal.TargetID != "b" || proposal.Changed || s.Players["a"].Body.RoomID != 1 {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	want := "당신은 Bob님을 봅니다.\r\n그는 Carol님과 결혼한 기혼자입니다.\r\n" + lookPlayerStandingWant(true) + lookEquipListWant
	if proposal.Response != want {
		t.Fatalf("first_ply standing=%q want %q", proposal.Response, want)
	}
	if strings.Contains(proposal.Response, LookCreaturePlainResponse) || strings.Contains(proposal.Response, "상처") || strings.Contains(proposal.Response, "죽기 직전") || strings.Contains(proposal.Response, "꼭 맞는") || strings.Contains(proposal.Response, "광채") {
		t.Fatalf("first_ply must not emit creature fallback/HP/consider/PKNOWA: %q", proposal.Response)
	}
	next, result, err := s.ApplyLook(proposal)
	if err != nil || result.Response != want || next.Players["a"].Body.RoomID != 1 || next.Players["b"].Body.RoomID != 1 || !reflect.DeepEqual(s, original) {
		t.Fatalf("apply mutated first_ply standing: result=%+v err=%v", result, err)
	}
}

func TestPlanLookInspectsFirstPlyStandingUsesPMALES(t *testing.T) {
	s := lookSelfPlyFixture()
	proposal, err := s.PlanLook("a", "Bob", 1, 12)
	if err != nil || s.Players["a"].Body.RoomID != 1 {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	want := "당신은 Bob님을 봅니다.\r\n" + lookPlayerStandingWant(false)
	if proposal.Response != want {
		t.Fatalf("female first_ply standing=%q want %q", proposal.Response, want)
	}

	bob := s.Players["b"]
	lookSetMale(&bob.Body)
	s.Players["b"] = bob
	proposal, err = s.PlanLook("a", "Bob", 1, 12)
	if err != nil {
		t.Fatal(err)
	}
	want = "당신은 Bob님을 봅니다.\r\n" + lookPlayerStandingWant(true)
	if proposal.Response != want {
		t.Fatalf("male first_ply standing=%q want %q", proposal.Response, want)
	}
}

func TestPlanLookFirstPlyEmptyOrUnmigratedDescriptionFailsClosed(t *testing.T) {
	for _, desc := range []string{"", "바르게", "바르게  ", "bad\ntext", string([]byte{0xff}), strings.Repeat("a", MaxDescriptionBytes+1)} {
		s := lookSelfPlyFixture()
		bob := s.Players["b"]
		bob.Body.Description = desc
		s.Players["b"] = bob
		if _, err := s.PlanLook("a", "Bob", 1, 12); !errors.Is(err, ErrLookObjectCreatureUnmigrated) {
			t.Fatalf("unmigrated description %q: %v", desc, err)
		}
	}
}

func TestApplyLookRejectsStaleFirstPlyDescription(t *testing.T) {
	s := lookSelfPlyFixture()
	proposal, err := s.PlanLook("a", "Bob", 1, 12)
	if err != nil {
		t.Fatal(err)
	}
	bob := s.Players["b"]
	bob.Body.Description = "기대어 "
	s.Players["b"] = bob
	if _, _, err := s.ApplyLook(proposal); !errors.Is(err, ErrLookStaleProposal) {
		t.Fatalf("stale description applied: %v", err)
	}
}

func TestPlanLookInspectsSelfAppendsStandingDescriptionAfterMirror(t *testing.T) {
	s := lookSelfPlyFixture()
	actor := s.Players["a"]
	lookSetKnowAlignment(&actor.Body)
	actor.Body.Alignment = -50
	lookSetHP(&actor.Body, 89, 100)
	actor.Items = lookEquippedItems()
	s.Players["a"] = actor
	original := s
	proposal, err := s.PlanLook("a", "나", 1, 12)
	if err != nil || proposal.Mode != lookModeSelf || proposal.TargetID != "a" || proposal.Changed || s.Players["a"].Body.RoomID != 1 {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	want := LookSelfMirrorResponse + lookPlayerStandingWant(false) + "그녀에게서 붉은 광채가 뻗어 나오고 있습니다.\r\n그녀는 가벼운 상처를 입었습니다.\r\n그녀는 당신과 꼭 맞는 상대입니다!\r\n" + lookEquipListWant
	if proposal.Response != want {
		t.Fatalf("self standing=%q want %q", proposal.Response, want)
	}
	if strings.Contains(proposal.Response, LookCreaturePlainResponse) || strings.Contains(proposal.Response, "그는 서 있습니다") {
		t.Fatalf("self must not emit creature fallback or empty C print: %q", proposal.Response)
	}
	next, result, err := s.ApplyLook(proposal)
	if err != nil || result.Response != want || next.Players["a"].Body.RoomID != 1 || !reflect.DeepEqual(s, original) {
		t.Fatalf("apply mutated self standing: result=%+v err=%v", result, err)
	}
}

func TestPlanLookInspectsSelfStandingUsesPMALES(t *testing.T) {
	s := lookSelfPlyFixture()
	proposal, err := s.PlanLook("a", "나", 1, 12)
	if err != nil || s.Players["a"].Body.RoomID != 1 {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	want := LookSelfMirrorResponse + lookPlayerStandingWant(false) + "그녀는 당신과 꼭 맞는 상대입니다!\r\n"
	if proposal.Response != want {
		t.Fatalf("female self standing=%q want %q", proposal.Response, want)
	}

	actor := s.Players["a"]
	lookSetMale(&actor.Body)
	s.Players["a"] = actor
	proposal, err = s.PlanLook("a", "나", 1, 12)
	if err != nil {
		t.Fatal(err)
	}
	want = LookSelfMirrorResponse + lookPlayerStandingWant(true) + "그는 당신과 꼭 맞는 상대입니다!\r\n"
	if proposal.Response != want {
		t.Fatalf("male self standing=%q want %q", proposal.Response, want)
	}
}

func TestPlanLookSelfEmptyOrUnmigratedDescriptionFailsClosed(t *testing.T) {
	for _, desc := range []string{"", "바르게", "바르게  ", "bad\ntext", string([]byte{0xff}), strings.Repeat("a", MaxDescriptionBytes+1)} {
		s := lookSelfPlyFixture()
		actor := s.Players["a"]
		actor.Body.Description = desc
		s.Players["a"] = actor
		if _, err := s.PlanLook("a", "나", 1, 12); !errors.Is(err, ErrLookObjectCreatureUnmigrated) {
			t.Fatalf("unmigrated self description %q: %v", desc, err)
		}
	}
}

func TestApplyLookRejectsStaleSelfDescription(t *testing.T) {
	s := lookSelfPlyFixture()
	proposal, err := s.PlanLook("a", "나", 1, 12)
	if err != nil {
		t.Fatal(err)
	}
	actor := s.Players["a"]
	actor.Body.Description = "기대어 "
	s.Players["a"] = actor
	if _, _, err := s.ApplyLook(proposal); !errors.Is(err, ErrLookStaleProposal) {
		t.Fatalf("stale self description applied: %v", err)
	}
}

func TestPlanLookInspectsSelfAppendsMarriageAfterMirror(t *testing.T) {
	s := lookSelfPlyFixture()
	actor := s.Players["a"]
	lookSetKnowAlignment(&actor.Body)
	actor.Body.Alignment = -50
	lookSetHP(&actor.Body, 89, 100)
	lookSetMarried(&actor.Body, "Carol", true)
	actor.Items = lookEquippedItems()
	s.Players["a"] = actor
	original := s
	proposal, err := s.PlanLook("a", "나", 1, 12)
	if err != nil || proposal.Mode != lookModeSelf || proposal.TargetID != "a" || proposal.Changed || s.Players["a"].Body.RoomID != 1 {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	want := LookSelfMirrorResponse + "그는 Carol님과 결혼한 기혼자입니다.\r\n" + lookPlayerStandingWant(true) + "그에게서 붉은 광채가 뻗어 나오고 있습니다.\r\n그는 가벼운 상처를 입었습니다.\r\n그는 당신과 꼭 맞는 상대입니다!\r\n" + lookEquipListWant
	if proposal.Response != want {
		t.Fatalf("self marriage=%q want %q", proposal.Response, want)
	}
	next, result, err := s.ApplyLook(proposal)
	if err != nil || result.Response != want || next.Players["a"].Body.RoomID != 1 || !reflect.DeepEqual(s, original) {
		t.Fatalf("apply mutated self marriage: result=%+v err=%v", result, err)
	}
}

func TestPlanLookInspectsSelfMarriageUsesPMALES(t *testing.T) {
	s := lookSelfPlyFixture()
	actor := s.Players["a"]
	lookSetMarried(&actor.Body, "Carol", false)
	s.Players["a"] = actor
	proposal, err := s.PlanLook("a", "나", 1, 12)
	if err != nil || s.Players["a"].Body.RoomID != 1 {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	want := LookSelfMirrorResponse + "그녀는 Carol님과 결혼한 기혼자입니다.\r\n" + lookPlayerStandingWant(false) + "그녀는 당신과 꼭 맞는 상대입니다!\r\n"
	if proposal.Response != want {
		t.Fatalf("female self marriage=%q want %q", proposal.Response, want)
	}
}

func TestPlanLookSelfPendingMarriageDoesNotPrintLine(t *testing.T) {
	s := lookSelfPlyFixture()
	actor := s.Players["a"]
	marriageSetFlag(&actor.Body.Flags, MarriagePendingFlag, true)
	actor.Body.Keys[MarriageSpouseKeyIndex] = MarriageSpouseKeyPrefix + "Carol"
	s.Players["a"] = actor
	proposal, err := s.PlanLook("a", "나", 1, 12)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Response != LookSelfMirrorResponse+lookPlayerStandingWant(false)+"그녀는 당신과 꼭 맞는 상대입니다!\r\n" || strings.Contains(proposal.Response, "기혼자") {
		t.Fatalf("pending marriage printed: %q", proposal.Response)
	}
}

func TestPlanLookSelfMarriedUnmigratedSpouseFailsClosed(t *testing.T) {
	s := lookSelfPlyFixture()
	actor := s.Players["a"]
	marriageSetFlag(&actor.Body.Flags, MarriageActiveFlag, true)
	s.Players["a"] = actor
	if _, err := s.PlanLook("a", "나", 1, 12); !errors.Is(err, ErrLookObjectCreatureUnmigrated) {
		t.Fatalf("PMARRI without spouse: %v", err)
	}

	s = lookSelfPlyFixture()
	actor = s.Players["a"]
	marriageSetFlag(&actor.Body.Flags, MarriageActiveFlag, true)
	actor.Body.Keys[MarriageSpouseKeyIndex] = "Carol"
	s.Players["a"] = actor
	if _, err := s.PlanLook("a", "나", 1, 12); !errors.Is(err, ErrLookObjectCreatureUnmigrated) {
		t.Fatalf("PMARRI without m-prefix: %v", err)
	}
}

func TestApplyLookRejectsStaleSelfMarriage(t *testing.T) {
	s := lookSelfPlyFixture()
	actor := s.Players["a"]
	lookSetMarried(&actor.Body, "Carol", true)
	s.Players["a"] = actor
	proposal, err := s.PlanLook("a", "나", 1, 12)
	if err != nil {
		t.Fatal(err)
	}
	actor = s.Players["a"]
	actor.Body.Keys[MarriageSpouseKeyIndex] = MarriageSpouseKeyPrefix + "Dave"
	s.Players["a"] = actor
	if _, _, err := s.ApplyLook(proposal); !errors.Is(err, ErrLookStaleProposal) {
		t.Fatalf("stale self marriage applied: %v", err)
	}
}

func TestPlanLookHPMaxNonPositiveFailsClosed(t *testing.T) {
	s := lookSelfPlyFixture()
	actor := s.Players["a"]
	lookSetHP(&actor.Body, 10, 0)
	s.Players["a"] = actor
	if _, err := s.PlanLook("a", "나", 1, 12); !errors.Is(err, ErrLookObjectCreatureUnmigrated) {
		t.Fatalf("self hpmax=0: %v", err)
	}

	s = lookSelfPlyFixture()
	actor = s.Players["a"]
	lookSetHP(&actor.Body, 10, -5)
	s.Players["a"] = actor
	if _, err := s.PlanLook("a", "나", 1, 12); !errors.Is(err, ErrLookObjectCreatureUnmigrated) {
		t.Fatalf("self hpmax<0: %v", err)
	}

	s = lookObjectCreatureFixture()
	wolf := s.NPCs["npc-wolf"]
	lookSetHP(&wolf.Body, 10, 0)
	s.NPCs["npc-wolf"] = wolf
	if _, err := s.PlanLook("a", "늑대", 1, 12); !errors.Is(err, ErrLookObjectCreatureUnmigrated) {
		t.Fatalf("creature hpmax=0: %v", err)
	}

	s = lookSelfPlyFixture()
	bob := s.Players["b"]
	lookSetHP(&bob.Body, 10, 0)
	s.Players["b"] = bob
	if _, err := s.PlanLook("a", "Bob", 1, 12); !errors.Is(err, ErrLookObjectCreatureUnmigrated) {
		t.Fatalf("first_ply hpmax=0: %v", err)
	}

	s = lookSelfPlyFixture()
	bob = s.Players["b"]
	lookSetHP(&bob.Body, 10, -5)
	s.Players["b"] = bob
	if _, err := s.PlanLook("a", "Bob", 1, 12); !errors.Is(err, ErrLookObjectCreatureUnmigrated) {
		t.Fatalf("first_ply hpmax<0: %v", err)
	}
}

func TestApplyLookRejectsStaleHPBand(t *testing.T) {
	s := lookObjectCreatureFixture()
	wolf := s.NPCs["npc-wolf"]
	lookSetHP(&wolf.Body, 89, 100)
	s.NPCs["npc-wolf"] = wolf
	proposal, err := s.PlanLook("a", "늑대", 1, 12)
	if err != nil {
		t.Fatal(err)
	}
	wolf = s.NPCs["npc-wolf"]
	lookSetHP(&wolf.Body, 19, 100)
	s.NPCs["npc-wolf"] = wolf
	if _, _, err := s.ApplyLook(proposal); !errors.Is(err, ErrLookStaleProposal) {
		t.Fatalf("stale hp band applied: %v", err)
	}
}

func TestPlanLookInspectsSelfAppendsPKNOWAGlowBeforeHPBand(t *testing.T) {
	s := lookSelfPlyFixture()
	actor := s.Players["a"]
	lookSetKnowAlignment(&actor.Body)
	actor.Body.Alignment = -50
	lookSetHP(&actor.Body, 89, 100)
	actor.Items = lookEquippedItems()
	s.Players["a"] = actor
	original := s
	proposal, err := s.PlanLook("a", "나", 1, 12)
	if err != nil || proposal.Mode != lookModeSelf || proposal.Changed || s.Players["a"].Body.RoomID != 1 {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	want := LookSelfMirrorResponse + lookPlayerStandingWant(false) + "그녀에게서 붉은 광채가 뻗어 나오고 있습니다.\r\n그녀는 가벼운 상처를 입었습니다.\r\n그녀는 당신과 꼭 맞는 상대입니다!\r\n" + lookEquipListWant
	if proposal.Response != want {
		t.Fatalf("self pknowa=%q want %q", proposal.Response, want)
	}
	next, result, err := s.ApplyLook(proposal)
	if err != nil || result.Response != want || next.Players["a"].Body.RoomID != 1 || !reflect.DeepEqual(s, original) {
		t.Fatalf("apply mutated self pknowa: result=%+v err=%v", result, err)
	}
}

func TestPlanLookInspectsCreatureAppendsPKNOWAGlow(t *testing.T) {
	for _, tt := range []struct {
		align int16
		male  bool
		hpcur int16
		glow  string
		band  string
	}{
		{-1, false, 100, "그녀에게서 붉은 광채가 뻗어 나오고 있습니다.\r\n", ""},
		{1, true, 50, "그에게서 푸른 광채가 뻗어 나오고 있습니다.\r\n", "그는 많은 상처를 입었습니다.\r\n"},
		{-100, true, 100, "그에게서 붉은 광채가 뻗어 나오고 있습니다.\r\n", ""},
		{100, false, 100, "그녀에게서 푸른 광채가 뻗어 나오고 있습니다.\r\n", ""},
	} {
		s := lookObjectCreatureFixture()
		actor := s.Players["a"]
		lookSetKnowAlignment(&actor.Body)
		s.Players["a"] = actor
		wolf := s.NPCs["npc-wolf"]
		wolf.Body.Alignment = tt.align
		lookSetHP(&wolf.Body, tt.hpcur, 100)
		if tt.male {
			lookSetMale(&wolf.Body)
		}
		s.NPCs["npc-wolf"] = wolf
		original := s
		proposal, err := s.PlanLook("a", "늑대", 1, 12)
		if err != nil || proposal.Mode != lookModeCreature || proposal.Changed || s.Players["a"].Body.RoomID != 1 {
			t.Fatalf("align=%d male=%v proposal=%+v err=%v", tt.align, tt.male, proposal, err)
		}
		he := "그녀"
		if tt.male {
			he = "그"
		}
		want := "당신은 늑대를 봅니다.\r\n회색 늑대다.\r\n" + tt.glow + tt.band + he + "는 당신과 꼭 맞는 상대입니다!\r\n"
		if proposal.Response != want {
			t.Fatalf("align=%d male=%v response=%q want %q", tt.align, tt.male, proposal.Response, want)
		}
		next, result, err := s.ApplyLook(proposal)
		if err != nil || result.Response != want || next.Players["a"].Body.RoomID != 1 || !reflect.DeepEqual(s, original) {
			t.Fatalf("align=%d apply mutated: result=%+v err=%v", tt.align, result, err)
		}
	}
}

func TestPlanLookPKNOWASkippedWithoutFlagOrZeroAlignment(t *testing.T) {
	s := lookObjectCreatureFixture()
	wolf := s.NPCs["npc-wolf"]
	wolf.Body.Alignment = -40
	s.NPCs["npc-wolf"] = wolf
	proposal, err := s.PlanLook("a", "늑대", 1, 12)
	if err != nil || s.Players["a"].Body.RoomID != 1 {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	if strings.Contains(proposal.Response, "광채") {
		t.Fatalf("glow without PKNOWA: %q", proposal.Response)
	}

	s = lookObjectCreatureFixture()
	actor := s.Players["a"]
	lookSetKnowAlignment(&actor.Body)
	s.Players["a"] = actor
	proposal, err = s.PlanLook("a", "늑대", 1, 12)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(proposal.Response, "광채") {
		t.Fatalf("glow with alignment 0: %q", proposal.Response)
	}

	s = lookSelfPlyFixture()
	actor = s.Players["a"]
	lookSetKnowAlignment(&actor.Body)
	s.Players["a"] = actor
	proposal, err = s.PlanLook("a", "나", 1, 12)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(proposal.Response, "광채") {
		t.Fatalf("self glow with alignment 0: %q", proposal.Response)
	}
}

func TestPlanLookInspectsFirstPlyAppendsPKNOWABlueGlow(t *testing.T) {
	for _, tt := range []struct {
		align int16
		male  bool
		glow  bool
	}{
		{-100, true, true},
		{-50, false, true},
		{0, false, true},
		{50, true, true},
		{100, false, true},
		{101, false, false},
		{-101, true, false},
	} {
		s := lookSelfPlyFixture()
		actor := s.Players["a"]
		lookSetKnowAlignment(&actor.Body)
		s.Players["a"] = actor
		bob := s.Players["b"]
		bob.Body.Alignment = tt.align
		if tt.male {
			lookSetMale(&bob.Body)
		}
		s.Players["b"] = bob
		original := s
		proposal, err := s.PlanLook("a", "Bob", 1, 12)
		if err != nil || proposal.Mode != lookModePlayer || proposal.Changed || s.Players["a"].Body.RoomID != 1 {
			t.Fatalf("align=%d male=%v proposal=%+v err=%v", tt.align, tt.male, proposal, err)
		}
		he := "그녀"
		if tt.male {
			he = "그"
		}
		want := "당신은 Bob님을 봅니다.\r\n" + lookPlayerStandingWant(tt.male)
		if tt.glow {
			want += he + "에게서 푸른 광채가 뻗어 나오고 있습니다.\r\n"
		}
		if proposal.Response != want {
			t.Fatalf("align=%d male=%v response=%q want %q", tt.align, tt.male, proposal.Response, want)
		}
		if strings.Contains(proposal.Response, "붉은") {
			t.Fatalf("first_ply must not invent 붉은: %q", proposal.Response)
		}
		next, result, err := s.ApplyLook(proposal)
		if err != nil || result.Response != want || next.Players["a"].Body.RoomID != 1 || next.Players["b"].Body.RoomID != 1 || !reflect.DeepEqual(s, original) {
			t.Fatalf("align=%d apply mutated: result=%+v err=%v", tt.align, result, err)
		}
	}
}

func TestPlanLookFirstPlyPKNOWASkippedWithoutFlag(t *testing.T) {
	s := lookSelfPlyFixture()
	bob := s.Players["b"]
	bob.Body.Alignment = 50
	lookSetMale(&bob.Body)
	s.Players["b"] = bob
	proposal, err := s.PlanLook("a", "Bob", 1, 12)
	if err != nil || s.Players["a"].Body.RoomID != 1 {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	if proposal.Response != "당신은 Bob님을 봅니다.\r\n"+lookPlayerStandingWant(true) || strings.Contains(proposal.Response, "광채") {
		t.Fatalf("glow without PKNOWA: %q", proposal.Response)
	}
}

func TestPlanLookInspectsFirstPlyAppendsPKNOWAThenThreeTenthsHPBeforeEquip(t *testing.T) {
	s := lookSelfPlyFixture()
	actor := s.Players["a"]
	lookSetKnowAlignment(&actor.Body)
	s.Players["a"] = actor
	bob := s.Players["b"]
	bob.Body.Alignment = -50
	lookSetHP(&bob.Body, 10, 100)
	lookSetMarried(&bob.Body, "Carol", true)
	bob.Items = lookEquippedItems()
	s.Players["b"] = bob
	original := s
	proposal, err := s.PlanLook("a", "Bob", 1, 12)
	if err != nil || proposal.Mode != lookModePlayer || proposal.TargetID != "b" || proposal.Changed || s.Players["a"].Body.RoomID != 1 {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	want := "당신은 Bob님을 봅니다.\r\n그는 Carol님과 결혼한 기혼자입니다.\r\n" + lookPlayerStandingWant(true) + "그에게서 푸른 광채가 뻗어 나오고 있습니다.\r\n그는 가벼운 상처를 입었습니다.\r\n" + lookEquipListWant
	if proposal.Response != want {
		t.Fatalf("first_ply pknowa/hp=%q want %q", proposal.Response, want)
	}
	if strings.Contains(proposal.Response, "붉은") || strings.Contains(proposal.Response, "죽기 직전") || strings.Contains(proposal.Response, "꼭 맞는") {
		t.Fatalf("first_ply leaked first_mon glow/HP/consider: %q", proposal.Response)
	}
	next, result, err := s.ApplyLook(proposal)
	if err != nil || result.Response != want || next.Players["a"].Body.RoomID != 1 || next.Players["b"].Body.RoomID != 1 || !reflect.DeepEqual(s, original) {
		t.Fatalf("apply mutated first_ply pknowa/hp: result=%+v err=%v", result, err)
	}
}

func TestApplyLookRejectsStaleFirstPlyThreeTenthsHP(t *testing.T) {
	s := lookSelfPlyFixture()
	bob := s.Players["b"]
	lookSetHP(&bob.Body, 10, 100)
	s.Players["b"] = bob
	proposal, err := s.PlanLook("a", "Bob", 1, 12)
	if err != nil {
		t.Fatal(err)
	}
	bob = s.Players["b"]
	lookSetHP(&bob.Body, 100, 100)
	s.Players["b"] = bob
	if _, _, err := s.ApplyLook(proposal); !errors.Is(err, ErrLookStaleProposal) {
		t.Fatalf("stale first_ply hp applied: %v", err)
	}
}

func TestApplyLookRejectsStaleFirstPlyPKNOWAGlow(t *testing.T) {
	s := lookSelfPlyFixture()
	actor := s.Players["a"]
	lookSetKnowAlignment(&actor.Body)
	s.Players["a"] = actor
	bob := s.Players["b"]
	bob.Body.Alignment = -20
	s.Players["b"] = bob
	proposal, err := s.PlanLook("a", "Bob", 1, 12)
	if err != nil {
		t.Fatal(err)
	}
	bob = s.Players["b"]
	bob.Body.Alignment = 101
	s.Players["b"] = bob
	if _, _, err := s.ApplyLook(proposal); !errors.Is(err, ErrLookStaleProposal) {
		t.Fatalf("stale first_ply pknowa applied: %v", err)
	}
}

func TestApplyLookRejectsStalePKNOWAGlow(t *testing.T) {
	s := lookObjectCreatureFixture()
	actor := s.Players["a"]
	lookSetKnowAlignment(&actor.Body)
	s.Players["a"] = actor
	wolf := s.NPCs["npc-wolf"]
	wolf.Body.Alignment = -20
	s.NPCs["npc-wolf"] = wolf
	proposal, err := s.PlanLook("a", "늑대", 1, 12)
	if err != nil {
		t.Fatal(err)
	}
	wolf = s.NPCs["npc-wolf"]
	wolf.Body.Alignment = 20
	s.NPCs["npc-wolf"] = wolf
	if _, _, err := s.ApplyLook(proposal); !errors.Is(err, ErrLookStaleProposal) {
		t.Fatalf("stale pknowa applied: %v", err)
	}
}

const (
	lookSelfRoomBroadcast     = "\nAlice님이 거울을 들고 자신을 바라 봅니다.\r\n"
	lookCreatureRoomBroadcast = "\nAlice님이 늑대를 봅니다.\r\n"
	lookPlayerRoomBroadcast   = "\nAlice님이 Bob님을 봅니다.\r\n"
)

func TestPlanLookInspectsSelfFansOutMirrorBroadcast(t *testing.T) {
	s := lookSelfPlyFixture()
	original := s
	proposal, err := s.PlanLook("a", "나", 1, 12)
	if err != nil || proposal.Mode != lookModeSelf || !proposal.Broadcast || proposal.BroadcastText != lookSelfRoomBroadcast || proposal.Changed {
		t.Fatalf("self broadcast=%+v err=%v", proposal, err)
	}
	if strings.Contains(proposal.Response, "바라 봅니다") {
		t.Fatalf("actor inspect reused room broadcast: %q", proposal.Response)
	}
	next, result, err := s.ApplyLook(proposal)
	if err != nil || result.BroadcastText != lookSelfRoomBroadcast || !result.Broadcast || next.Players["a"].Body.RoomID != 1 || !reflect.DeepEqual(s, original) {
		t.Fatalf("apply mutated self broadcast: result=%+v err=%v", result, err)
	}
	event, ok, err := s.RoomLookInspectEvent("a", "나", 1, 12)
	if err != nil || !ok || event.RoomID != 1 || event.ExcludeActorID != "a" || event.Text != lookSelfRoomBroadcast {
		t.Fatalf("event=%+v ok=%v err=%v", event, ok, err)
	}
}

func TestPlanLookInspectsCreatureAndFirstPlyFansOutRoomBroadcast(t *testing.T) {
	s := lookSelfPlyFixture()
	room := s.Rooms[1]
	room.NPCIDs = []string{"npc-wolf"}
	s.Rooms[1] = room
	s.NPCs = map[string]NPCState{
		"npc-wolf": {Body: LegacyMonster{Name: "늑대", Description: "회색 늑대다.", Type: 1, RoomID: 1, HPMax: 100, HPCurrent: 100}, Items: lookEmptyItems(), Enemies: []NPCEnemy{}},
	}
	creature, err := s.PlanLook("a", "늑대", 1, 12)
	if err != nil || creature.Mode != lookModeCreature || creature.BroadcastText != lookCreatureRoomBroadcast {
		t.Fatalf("creature broadcast=%+v err=%v", creature, err)
	}
	player, err := s.PlanLook("a", "Bob", 1, 12)
	if err != nil || player.Mode != lookModePlayer || player.BroadcastText != lookPlayerRoomBroadcast {
		t.Fatalf("first_ply broadcast=%+v err=%v", player, err)
	}
	event, ok, err := s.RoomLookInspectEvent("a", "Bob", 1, 12)
	if err != nil || !ok || event.Text != lookPlayerRoomBroadcast || event.ExcludeActorID != "a" {
		t.Fatalf("first_ply event=%+v ok=%v err=%v", event, ok, err)
	}
}

func TestPlanLookFirstMonNamePrefixFansOutSelfBroadcast(t *testing.T) {
	s := lookSelfPlyFixture()
	room := s.Rooms[1]
	room.NPCIDs = []string{"npc-ali"}
	s.Rooms[1] = room
	s.NPCs = map[string]NPCState{
		"npc-ali": {Body: LegacyMonster{Name: "Ali", Description: "짧은 이름.", Type: 1, RoomID: 1, HPMax: 100, HPCurrent: 100}, Items: lookEmptyItems(), Enemies: []NPCEnemy{}},
	}
	proposal, err := s.PlanLook("a", "Ali", 1, 12)
	if err != nil || proposal.Mode != lookModeCreature || proposal.BroadcastText != lookSelfRoomBroadcast {
		t.Fatalf("prefix-self broadcast=%+v err=%v", proposal, err)
	}
}

func TestPlanLookObjectExitAndBareDoNotBroadcast(t *testing.T) {
	s := lookObjectCreatureFixture()
	object, err := s.PlanLook("a", "검", 1, 12)
	if err != nil || object.Mode != lookModeObject || object.Broadcast || object.BroadcastText != "" {
		t.Fatalf("object broadcast=%+v err=%v", object, err)
	}
	s = lookExitFixture()
	peek, err := s.PlanLook("a", "동", 1, 12)
	if err != nil || peek.Mode != lookModePeek || peek.Broadcast || peek.BroadcastText != "" {
		t.Fatalf("peek broadcast=%+v err=%v", peek, err)
	}
	bare, err := s.PlanLook("a", "", 0, 12)
	if err != nil || bare.Mode != lookModeHere || bare.Broadcast || bare.BroadcastText != "" {
		t.Fatalf("bare broadcast=%+v err=%v", bare, err)
	}
	if _, ok, err := s.RoomLookInspectEvent("a", "동", 1, 12); err != nil || ok {
		t.Fatalf("peek inspect event ok=%v err=%v", ok, err)
	}
}

func TestPlanLookInspectFailsClosedForUnmigratedRoomOccupant(t *testing.T) {
	s := lookSelfPlyFixture()
	carol := s.Players["c"]
	carol.Body.Name = "Carol "
	s.Players["c"] = carol
	if _, err := s.PlanLook("a", "나", 1, 12); !errors.Is(err, ErrLookObjectCreatureUnmigrated) {
		t.Fatalf("unmigrated occupant self: %v", err)
	}
	if _, ok, err := s.RoomLookInspectEvent("a", "나", 1, 12); ok || !errors.Is(err, ErrLookObjectCreatureUnmigrated) {
		t.Fatalf("unmigrated occupant event ok=%v err=%v", ok, err)
	}

	s = lookSelfPlyFixture()
	carol = s.Players["c"]
	carol.Body.Name = "Carol "
	s.Players["c"] = carol
	if _, err := s.PlanLook("a", "Bob", 1, 12); !errors.Is(err, ErrLookObjectCreatureUnmigrated) {
		t.Fatalf("unmigrated occupant first_ply: %v", err)
	}

	s = lookObjectCreatureFixture()
	room := s.Rooms[1]
	room.PlayerIDs = []string{"a", "b"}
	s.Rooms[1] = room
	s.Players["b"] = PlayerState{Body: LegacyMonster{Name: "Bob ", RoomID: 1}, Online: true, Items: &ItemCollection{Items: map[string]Item{}}}
	if _, err := s.PlanLook("a", "늑대", 1, 12); !errors.Is(err, ErrLookObjectCreatureUnmigrated) {
		t.Fatalf("unmigrated occupant creature: %v", err)
	}
}

func TestApplyLookRejectsStaleInspectBroadcast(t *testing.T) {
	s := lookSelfPlyFixture()
	proposal, err := s.PlanLook("a", "Bob", 1, 12)
	if err != nil {
		t.Fatal(err)
	}
	alice := s.Players["a"]
	alice.Body.Name = "Alicia"
	s.Players["a"] = alice
	if _, _, err := s.ApplyLook(proposal); !errors.Is(err, ErrLookStaleProposal) {
		t.Fatalf("stale actor broadcast applied: %v", err)
	}
}

func lookPINVISActorFixture() State {
	s := lookSelfPlyFixture()
	alice := s.Players["a"]
	lookSetInvisible(&alice.Body)
	s.Players["a"] = alice
	carol := s.Players["c"]
	lookSetDetectInvisible(&carol.Body)
	s.Players["c"] = carol
	room := s.Rooms[1]
	room.NPCIDs = []string{"npc-wolf"}
	s.Rooms[1] = room
	s.NPCs = map[string]NPCState{
		"npc-wolf": {Body: LegacyMonster{Name: "늑대", Description: "회색 늑대다.", Type: 1, RoomID: 1, HPMax: 100, HPCurrent: 100}, Items: lookEmptyItems(), Enemies: []NPCEnemy{}},
	}
	return s
}

func TestPlanLookInspectBroadcastHidesPINVISFromNonPDINVIViewers(t *testing.T) {
	s := lookPINVISActorFixture()
	const (
		hiddenSelf       = "\n누군가가 거울을 들고 자신을 바라 봅니다.\r\n"
		detectedSelf     = "\nAlice(*)님이 거울을 들고 자신을 바라 봅니다.\r\n"
		hiddenCreature   = "\n누군가가 늑대를 봅니다.\r\n"
		detectedCreature = "\nAlice(*)님이 늑대를 봅니다.\r\n"
		hiddenPlayer     = "\n누군가가 Bob님을 봅니다.\r\n"
		detectedPlayer   = "\nAlice(*)님이 Bob님을 봅니다.\r\n"
	)
	for _, tt := range []struct {
		prefix, hidden, detected string
	}{
		{"나", hiddenSelf, detectedSelf},
		{"늑대", hiddenCreature, detectedCreature},
		{"Bob", hiddenPlayer, detectedPlayer},
	} {
		proposal, err := s.PlanLook("a", tt.prefix, 1, 12)
		if err != nil || !proposal.Broadcast || strings.Contains(proposal.Response, "바라 봅니다") {
			t.Fatalf("%s proposal=%+v err=%v", tt.prefix, proposal, err)
		}
		if strings.Contains(proposal.BroadcastText, "Alice") && !strings.Contains(proposal.BroadcastText, "(*)") {
			t.Fatalf("%s snapshot leaked PINVIS without (*): %q", tt.prefix, proposal.BroadcastText)
		}
		event, ok, err := s.RoomLookInspectEvent("a", tt.prefix, 1, 12)
		if err != nil || !ok || event.ExcludeActorID != "a" {
			t.Fatalf("%s event=%+v ok=%v err=%v", tt.prefix, event, ok, err)
		}
		if got := event.TextFor(s.Players["b"].Body); got != tt.hidden || strings.Contains(got, "Alice") {
			t.Fatalf("%s bob=%q want %q", tt.prefix, got, tt.hidden)
		}
		if got := event.TextFor(s.Players["c"].Body); got != tt.detected {
			t.Fatalf("%s carol=%q want %q", tt.prefix, got, tt.detected)
		}
		next, result, err := s.ApplyLook(proposal)
		if err != nil || next.Players["a"].Body.RoomID != 1 || !result.Broadcast {
			t.Fatalf("%s apply result=%+v err=%v", tt.prefix, result, err)
		}
	}
}

func TestPlanLookInspectBroadcastHidesPINVISTargetFromNonPDINVIOccupants(t *testing.T) {
	s := lookSelfPlyFixture()
	alice := s.Players["a"]
	lookSetDetectInvisible(&alice.Body)
	s.Players["a"] = alice
	bob := s.Players["b"]
	lookSetInvisible(&bob.Body)
	s.Players["b"] = bob
	carol := s.Players["c"]
	lookSetDetectInvisible(&carol.Body)
	s.Players["c"] = carol
	event, ok, err := s.RoomLookInspectEvent("a", "Bob", 1, 12)
	if err != nil || !ok {
		t.Fatalf("event=%+v ok=%v err=%v", event, ok, err)
	}
	if got := event.TextFor(s.Players["b"].Body); got != "\nAlice님이 누군가를 봅니다.\r\n" || strings.Contains(got, "Bob") {
		t.Fatalf("bob target leak=%q", got)
	}
	if got := event.TextFor(s.Players["c"].Body); got != "\nAlice님이 Bob(*)님을 봅니다.\r\n" {
		t.Fatalf("carol detected target=%q", got)
	}
}

func TestApplyLookRejectsStalePINVISInspectBroadcast(t *testing.T) {
	s := lookSelfPlyFixture()
	proposal, err := s.PlanLook("a", "나", 1, 12)
	if err != nil {
		t.Fatal(err)
	}
	alice := s.Players["a"]
	lookSetInvisible(&alice.Body)
	s.Players["a"] = alice
	if _, _, err := s.ApplyLook(proposal); !errors.Is(err, ErrLookStaleProposal) {
		t.Fatalf("stale PINVIS broadcast applied: %v", err)
	}
}

func TestPlanLookInspectsCreatureAppendsAngryAndFightingYou(t *testing.T) {
	s := lookObjectCreatureFixture()
	wolf := s.NPCs["npc-wolf"]
	wolf.Enemies = []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "a"}, Damage: 0}}
	wolf.Items = lookEquippedItems()
	lookSetHP(&wolf.Body, 89, 100)
	s.NPCs["npc-wolf"] = wolf
	original := s
	proposal, err := s.PlanLook("a", "늑대", 1, 12)
	if err != nil || proposal.Mode != lookModeCreature || proposal.Changed || s.Players["a"].Body.RoomID != 1 {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	want := "당신은 늑대를 봅니다.\r\n회색 늑대다.\r\n그녀는 가벼운 상처를 입었습니다.\r\n그녀는 당신에게 매우 화가 난것 같습니다.\r\n그녀는 당신과 싸우고 있습니다.\r\n그녀는 당신과 꼭 맞는 상대입니다!\r\n" + lookEquipListWant
	if proposal.Response != want {
		t.Fatalf("creature enm=%q want %q", proposal.Response, want)
	}
	next, result, err := s.ApplyLook(proposal)
	if err != nil || result.Response != want || next.Players["a"].Body.RoomID != 1 || !reflect.DeepEqual(s, original) {
		t.Fatalf("apply mutated creature enm: result=%+v err=%v", result, err)
	}
}

func TestPlanLookInspectsCreatureAppendsFightingOtherWithParticles(t *testing.T) {
	for _, tt := range []struct {
		name, particle string
		male, angry    bool
		enemies        []NPCEnemy
	}{
		{"Bob", "와", false, false, []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "b"}, Damage: 0}}},
		{"강민", "과", true, false, []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "b"}, Damage: 0}}},
		{"Bob", "와", false, true, []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "b"}, Damage: 0}, {Target: EntityRef{Kind: "player", ID: "a"}, Damage: 1}}},
	} {
		s := lookObjectCreatureFixture()
		room := s.Rooms[1]
		room.PlayerIDs = []string{"a", "b"}
		s.Rooms[1] = room
		s.Players["b"] = PlayerState{Body: LegacyMonster{Name: tt.name, RoomID: 1, HPMax: 100, HPCurrent: 100}, Online: true, Items: lookEmptyItems()}
		wolf := s.NPCs["npc-wolf"]
		wolf.Enemies = tt.enemies
		if tt.male {
			lookSetMale(&wolf.Body)
		}
		s.NPCs["npc-wolf"] = wolf
		original := s
		proposal, err := s.PlanLook("a", "늑대", 1, 12)
		if err != nil || proposal.Mode != lookModeCreature || proposal.Changed || s.Players["a"].Body.RoomID != 1 {
			t.Fatalf("name=%s proposal=%+v err=%v", tt.name, proposal, err)
		}
		he := "그녀"
		if tt.male {
			he = "그"
		}
		angry := ""
		if tt.angry {
			angry = he + "는 당신에게 매우 화가 난것 같습니다.\r\n"
		}
		want := "당신은 늑대를 봅니다.\r\n회색 늑대다.\r\n" + angry + he + "는 " + tt.name + tt.particle + " 싸우고 있습니다.\r\n" + he + "는 당신과 꼭 맞는 상대입니다!\r\n"
		if proposal.Response != want {
			t.Fatalf("name=%s angry=%v response=%q want %q", tt.name, tt.angry, proposal.Response, want)
		}
		if strings.Contains(proposal.Response, "당신과 싸우고") {
			t.Fatalf("name=%s used viewer combat line: %q", tt.name, proposal.Response)
		}
		next, result, err := s.ApplyLook(proposal)
		if err != nil || result.Response != want || next.Players["a"].Body.RoomID != 1 || !reflect.DeepEqual(s, original) {
			t.Fatalf("name=%s apply mutated: result=%+v err=%v", tt.name, result, err)
		}
	}
}

func TestPlanLookInspectsCreatureAppendsFightingNPCEnemy(t *testing.T) {
	s := lookObjectCreatureFixture()
	wolf := s.NPCs["npc-wolf"]
	wolf.Enemies = []NPCEnemy{{Target: EntityRef{Kind: "npc", ID: "npc-goblin"}, Damage: 0}}
	s.NPCs["npc-wolf"] = wolf
	proposal, err := s.PlanLook("a", "늑대", 1, 12)
	if err != nil || s.Players["a"].Body.RoomID != 1 {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	if !strings.Contains(proposal.Response, "그녀는 고블린과 싸우고 있습니다.\r\n") || strings.Contains(proposal.Response, "화가") {
		t.Fatalf("npc first_enm=%q", proposal.Response)
	}
}

func TestPlanLookInspectsCreatureNilEnemiesFailsClosed(t *testing.T) {
	s := lookObjectCreatureFixture()
	wolf := s.NPCs["npc-wolf"]
	wolf.Enemies = nil
	s.NPCs["npc-wolf"] = wolf
	if _, err := s.PlanLook("a", "늑대", 1, 12); !errors.Is(err, ErrLookObjectCreatureUnmigrated) {
		t.Fatalf("nil Enemies: %v", err)
	}
	if s.Players["a"].Body.RoomID != 1 {
		t.Fatal("nil Enemies moved actor")
	}
}

func TestPlanLookInspectsCreatureUnmigratedEnemyNameFailsClosed(t *testing.T) {
	s := lookObjectCreatureFixture()
	goblin := s.NPCs["npc-goblin"]
	goblin.Body.Name = "고블린 "
	s.NPCs["npc-goblin"] = goblin
	wolf := s.NPCs["npc-wolf"]
	wolf.Enemies = []NPCEnemy{{Target: EntityRef{Kind: "npc", ID: "npc-goblin"}, Damage: 0}}
	s.NPCs["npc-wolf"] = wolf
	if _, err := s.PlanLook("a", "늑대", 1, 12); !errors.Is(err, ErrLookObjectCreatureUnmigrated) {
		t.Fatalf("unmigrated first_enm name: %v", err)
	}
}

func TestPlanLookInspectsSelfAppendsAngryAndFightingYou(t *testing.T) {
	s := lookSelfPlyFixture()
	actor := s.Players["a"]
	actor.PlayerEnemies = []string{"a"}
	actor.Items = lookEquippedItems()
	lookSetHP(&actor.Body, 89, 100)
	s.Players["a"] = actor
	original := s
	proposal, err := s.PlanLook("a", "나", 1, 12)
	if err != nil || proposal.Mode != lookModeSelf || proposal.Changed || s.Players["a"].Body.RoomID != 1 {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	want := LookSelfMirrorResponse + lookPlayerStandingWant(false) + "그녀는 가벼운 상처를 입었습니다.\r\n그녀는 당신에게 매우 화가 난것 같습니다.\r\n그녀는 당신과 싸우고 있습니다.\r\n그녀는 당신과 꼭 맞는 상대입니다!\r\n" + lookEquipListWant
	if proposal.Response != want {
		t.Fatalf("self enm=%q want %q", proposal.Response, want)
	}
	next, result, err := s.ApplyLook(proposal)
	if err != nil || result.Response != want || next.Players["a"].Body.RoomID != 1 || !reflect.DeepEqual(s, original) {
		t.Fatalf("apply mutated self enm: result=%+v err=%v", result, err)
	}
}

func TestPlanLookInspectsSelfAppendsFightingOther(t *testing.T) {
	s := lookSelfPlyFixture()
	actor := s.Players["a"]
	actor.PlayerEnemies = []string{"b"}
	lookSetMale(&actor.Body)
	s.Players["a"] = actor
	original := s
	proposal, err := s.PlanLook("a", "나", 1, 12)
	if err != nil || proposal.Mode != lookModeSelf || proposal.Changed || s.Players["a"].Body.RoomID != 1 {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	want := LookSelfMirrorResponse + lookPlayerStandingWant(true) + "그는 Bob와 싸우고 있습니다.\r\n그는 당신과 꼭 맞는 상대입니다!\r\n"
	if proposal.Response != want {
		t.Fatalf("self first_enm other=%q want %q", proposal.Response, want)
	}
	if strings.Contains(proposal.Response, "화가") {
		t.Fatalf("self fighting other should not be is_enm_crt: %q", proposal.Response)
	}
	next, result, err := s.ApplyLook(proposal)
	if err != nil || result.Response != want || next.Players["a"].Body.RoomID != 1 || !reflect.DeepEqual(s, original) {
		t.Fatalf("apply mutated self other enm: result=%+v err=%v", result, err)
	}
}

func TestPlanLookInspectsFirstPlyDoesNotAppendEnemyLines(t *testing.T) {
	s := lookSelfPlyFixture()
	bob := s.Players["b"]
	bob.PlayerEnemies = []string{"a", "b"}
	bob.Items = lookEquippedItems()
	s.Players["b"] = bob
	proposal, err := s.PlanLook("a", "Bob", 1, 12)
	if err != nil || proposal.Mode != lookModePlayer || s.Players["a"].Body.RoomID != 1 {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	if strings.Contains(proposal.Response, "화가") || strings.Contains(proposal.Response, "싸우고") {
		t.Fatalf("first_ply must not print first_enm: %q", proposal.Response)
	}
	want := "당신은 Bob님을 봅니다.\r\n" + lookPlayerStandingWant(false) + lookEquipListWant
	if proposal.Response != want {
		t.Fatalf("first_ply enm leak=%q want %q", proposal.Response, want)
	}
}

func TestApplyLookRejectsStaleCreatureEnemies(t *testing.T) {
	s := lookObjectCreatureFixture()
	wolf := s.NPCs["npc-wolf"]
	wolf.Enemies = []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "a"}, Damage: 0}}
	s.NPCs["npc-wolf"] = wolf
	proposal, err := s.PlanLook("a", "늑대", 1, 12)
	if err != nil {
		t.Fatal(err)
	}
	wolf = s.NPCs["npc-wolf"]
	wolf.Enemies = []NPCEnemy{}
	s.NPCs["npc-wolf"] = wolf
	if _, _, err := s.ApplyLook(proposal); !errors.Is(err, ErrLookStaleProposal) {
		t.Fatalf("stale enm applied: %v", err)
	}
}
