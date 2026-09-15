package world

import (
	"errors"
	"reflect"
	"testing"
)

func useTestState(object Item, floor bool) State {
	items := &ItemCollection{Items: map[string]Item{}}
	if !floor {
		items.Items["item"] = object
		items.Inventory = []string{"item"}
	}
	roomItems := &ItemCollection{Items: map[string]Item{}}
	if floor {
		roomItems.Items["item"] = object
		roomItems.Inventory = []string{"item"}
	}
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{1: {
			Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}},
			Items:     roomItems,
			PlayerIDs: []string{"a"},
		}},
		Players: map[string]PlayerState{
			"a": {
				Body:   LegacyMonster{Name: "Alice", Type: 0, Class: 4, RoomID: 1, Level: 20, HPMax: 30, HPCurrent: 10},
				Online: true,
				Items:  items,
			},
		},
	}
}

func TestSelectUseRootMatchesCEqualPrefixesOnceWithIndependentFloorFallback(t *testing.T) {
	floorOne := LegacyObject{Name: "Floor One", Keys: [3]string{"floor-one", "gate-stone", ""}}
	floorTwo := LegacyObject{Name: "Floor Two", Keys: [3]string{"floor-two", "", ""}}
	floorTwo.Flags[useFloorFlag/8] |= 1 << (useFloorFlag % 8)
	actor := PlayerState{Items: &ItemCollection{
		Items: map[string]Item{
			"display":   {Object: LegacyObject{Name: "Healing Flask"}},
			"key0":      {Object: LegacyObject{Name: "zero-label", Keys: [3]string{"alpha-use", "", ""}}},
			"key1":      {Object: LegacyObject{Name: "one-label", Keys: [3]string{"", "beta-use", ""}}},
			"key2":      {Object: LegacyObject{Name: "two-label", Keys: [3]string{"", "", "gamma-use"}}},
			"duplicate": {Object: LegacyObject{Name: "Healing Root", Keys: [3]string{"healing-key0", "HEALING-KEY1", "healing-key2"}}},
			"followup":  {Object: LegacyObject{Name: "Healing Followup"}},
			"cross":     {Object: LegacyObject{Name: "cross inventory", Keys: [3]string{"floor-alias", "", ""}}},
		},
		Inventory: []string{"display", "key0", "key1", "key2", "duplicate", "followup", "cross"},
	}}
	room := RoomState{Items: &ItemCollection{
		Items:     map[string]Item{"floor-one": {Object: floorOne}, "floor-two": {Object: floorTwo}},
		Inventory: []string{"floor-one", "floor-two"},
	}}

	for _, tc := range []struct {
		name     string
		selector string
		wantID   string
		wantLoc  UseLocation
		wantOcc  int
	}{
		{name: "display name prefix", selector: "HEAL", wantID: "display", wantLoc: UseInventoryRoot, wantOcc: 1},
		{name: "key zero prefix", selector: "ALPHA", wantID: "key0", wantLoc: UseInventoryRoot, wantOcc: 1},
		{name: "key one prefix", selector: "BETA", wantID: "key1", wantLoc: UseInventoryRoot, wantOcc: 1},
		{name: "key two prefix", selector: "GaMmA", wantID: "key2", wantLoc: UseInventoryRoot, wantOcc: 1},
		{name: "duplicate fields count one root", selector: "HEAL", wantID: "duplicate", wantLoc: UseInventoryRoot, wantOcc: 2},
		{name: "duplicate fields do not consume next root", selector: "HEAL", wantID: "followup", wantLoc: UseInventoryRoot, wantOcc: 3},
		{name: "inventory takes precedence", selector: "FLOOR", wantID: "cross", wantLoc: UseInventoryRoot, wantOcc: 1},
		{name: "floor occurrence is independent", selector: "FLOOR", wantID: "floor-two", wantLoc: UseFloorRoot, wantOcc: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, err := selectUseRoot(actor, room, tc.selector, tc.wantOcc)
			if err != nil || root.id != tc.wantID || root.location != tc.wantLoc || root.occurrence != tc.wantOcc {
				t.Fatalf("selector=%q occurrence=%d root=%+v err=%v", tc.selector, tc.wantOcc, root, err)
			}
		})
	}

	if _, err := selectUseRoot(actor, room, "GATE", 1); !errors.Is(err, ErrUseFloor) {
		t.Fatalf("floor root without OUSEFL was admitted: err=%v", err)
	}
}

func TestPlanApplyUseDelegatesEquipmentAndClearsHiddenAtomically(t *testing.T) {
	object := LegacyObject{Name: "검", Type: useSharpType, Wear: equipmentWield, ShotsMax: 1, ShotsCurrent: 1, DiceCount: 1, DiceSides: 4}
	s := useTestState(Item{Object: object}, false)
	actor := s.Players["a"]
	setSettingFlag(&actor.Body, playerHiddenStateFlag, true)
	s.Players["a"] = actor
	before := s
	p, err := s.PlanUse("a", "검", 1, UseOptions{Now: 10})
	if err != nil || p.Result.ItemID != "item" || p.Result.Slot != equipmentWield {
		t.Fatalf("proposal=%+v err=%v", p, err)
	}
	inputActor := s.Players["a"]
	if !flag(inputActor.Body.Flags[:], playerHiddenStateFlag) || !reflect.DeepEqual(s, before) {
		t.Fatal("planning changed source snapshot")
	}
	next, result, err := s.ApplyUse(p)
	nextActor := next.Players["a"]
	if err != nil || !result.Broadcast || result.Event == nil || flag(nextActor.Body.Flags[:], playerHiddenStateFlag) || nextActor.Items.Ready[equipmentWield-1] != "item" {
		t.Fatalf("next=%+v result=%+v err=%v", next, result, err)
	}
	if _, _, err := next.ApplyUse(p); err == nil {
		t.Fatal("stale use proposal was accepted")
	}
}

func TestPlanApplyUseDelegatesPotionAndDoesNotRerollApply(t *testing.T) {
	object := LegacyObject{Name: "회복약", Type: usePotionType, MagicPower: 1, ShotsMax: 1, ShotsCurrent: 1}
	s := useTestState(Item{Object: object}, false)
	rolls := 0
	p, err := s.PlanUse("a", "회복약", 1, UseOptions{Now: 10, Roll: func(low, high int) int {
		rolls++
		if low != 1 || high != 6 {
			t.Fatalf("roll range %d..%d", low, high)
		}
		return 5
	}})
	if err != nil || rolls != 1 {
		t.Fatalf("proposal=%+v err=%v rolls=%d", p, err, rolls)
	}
	next, result, err := s.ApplyUse(p)
	if err != nil || rolls != 1 || !result.Consumed || next.Players["a"].Body.HPCurrent != 15 || len(next.Players["a"].Items.Items) != 0 {
		t.Fatalf("next=%+v result=%+v err=%v rolls=%d", next, result, err, rolls)
	}
}

func TestUseUnsupportedAndFloorAuthorityAreFailClosed(t *testing.T) {
	for _, object := range []LegacyObject{
		{Name: "주문서", Type: useScrollType, ShotsCurrent: 1},
		{Name: "지팡이", Type: useWandType, ShotsCurrent: 1},
		{Name: "열쇠", Type: useKeyType, ShotsCurrent: 1},
		{Name: "전쟁물건", Type: useSharpType, Wear: equipmentWield, ShotsCurrent: 1, Special: useWarSpecial},
	} {
		s := useTestState(Item{Object: object}, false)
		before := s
		_, err := s.PlanUse("a", object.Name, 1, UseOptions{})
		var unsupported *UseUnsupportedError
		if !errors.As(err, &unsupported) || !errors.Is(err, ErrUseUnsupported) || !reflect.DeepEqual(s, before) {
			t.Fatalf("object=%+v err=%v source changed=%v", object, err, !reflect.DeepEqual(s, before))
		}
	}
	floor := useTestState(Item{Object: LegacyObject{Name: "검", Type: useSharpType, Wear: equipmentWield, ShotsCurrent: 1, DiceCount: 1, DiceSides: 4}}, true)
	before := floor
	if _, err := floor.PlanUse("a", "검", 1, UseOptions{}); !errors.Is(err, ErrUseFloor) || !reflect.DeepEqual(floor, before) {
		t.Fatalf("missing OUSEFL err=%v source changed=%v", err, !reflect.DeepEqual(floor, before))
	}
	item := floor.Rooms[1].Items.Items["item"]
	item.Object.Flags[useFloorFlag/8] |= 1 << (useFloorFlag % 8)
	floor.Rooms[1].Items.Items["item"] = item
	p, err := floor.PlanUse("a", "검", 1, UseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	next, result, err := floor.ApplyUse(p)
	if err != nil || result.Location != UseFloorRoot || next.Players["a"].Items.Ready[equipmentWield-1] != "item" || len(next.Rooms[1].Items.Inventory) != 0 {
		t.Fatalf("floor next=%+v result=%+v err=%v", next, result, err)
	}
}
