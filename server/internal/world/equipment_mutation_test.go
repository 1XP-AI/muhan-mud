package world

import (
	"reflect"
	"strings"
	"testing"
)

func equipmentMutationFixture(t *testing.T) State {
	t.Helper()
	s := npcAttackFixture(t)
	p := s.Players["a"]
	p.Items = &ItemCollection{Items: map[string]Item{
		"armor": {Object: LegacyObject{Name: "갑옷", Armor: 2, Wear: 1, ShotsMax: 1, ShotsCurrent: 1}},
		"ring":  {Object: LegacyObject{Name: "반지", Wear: 9, ShotsMax: 1, ShotsCurrent: 1}},
		"torch": {Object: LegacyObject{Name: "횃불", Type: 0, Wear: 17, ShotsMax: 1, ShotsCurrent: 1}},
		"sword": {Object: LegacyObject{Name: "검", Type: 0, Wear: 20, DiceCount: 1, DiceSides: 4, ShotsMax: 1, ShotsCurrent: 1}},
	}, Inventory: []string{"armor", "ring", "torch", "sword"}}
	p.Body.Class = 4
	p.Body.Level = 1
	p.Body.Stats[0] = 10
	p.Body.Stats[1] = 10
	s.Players["a"] = p
	return s
}

func TestEquipmentMutationMovesCanonicalRootsAndRecomputesStats(t *testing.T) {
	s := equipmentMutationFixture(t)
	original := s
	next, result, err := s.ReadyItem("a", "갑옷", 1, EquipmentWear)
	if err != nil || result.Action != "wear" || result.Slot != equipmentBody || result.ItemName != "갑옷" {
		t.Fatalf("wear result=%+v err=%v", result, err)
	}
	if next.Players["a"].Items.Ready[equipmentBody-1] != "armor" || containsID(next.Players["a"].Items.Inventory, "armor") {
		t.Fatalf("wear ownership=%+v", next.Players["a"].Items)
	}
	armor := next.Players["a"].Items.Items["armor"]
	if !flag(armor.Object.Flags[:], objectWearsFlag) || next.Players["a"].Body.Armor == s.Players["a"].Body.Armor {
		t.Fatalf("wear flags/stats=%+v body=%+v", next.Players["a"].Items.Items["armor"], next.Players["a"].Body)
	}

	next, result, err = next.ReadyItem("a", "횃불", 1, EquipmentHold)
	if err != nil || result.Action != "hold" || result.Slot != equipmentHeld {
		t.Fatalf("hold result=%+v err=%v", result, err)
	}
	torch := next.Players["a"].Items.Items["torch"]
	if next.Players["a"].Items.Ready[equipmentHeld-1] != "torch" || !flag(torch.Object.Flags[:], objectHeldFlag) {
		t.Fatalf("hold ownership/flags=%+v", next.Players["a"].Items)
	}

	next, result, err = next.ReadyItem("a", "검", 1, EquipmentWield)
	if err != nil || result.Action != "wield" || result.Slot != equipmentWield {
		t.Fatalf("wield result=%+v err=%v", result, err)
	}
	if next.Players["a"].Items.Ready[equipmentWield-1] != "sword" || containsID(next.Players["a"].Items.Inventory, "sword") {
		t.Fatalf("wield ownership=%+v", next.Players["a"].Items)
	}

	next, result, err = next.RemoveItem("a", "갑옷", 1)
	if err != nil || result.Action != "remove" || result.Slot != equipmentBody {
		t.Fatalf("remove result=%+v err=%v", result, err)
	}
	armor = next.Players["a"].Items.Items["armor"]
	if next.Players["a"].Items.Ready[equipmentBody-1] != "" || !containsID(next.Players["a"].Items.Inventory, "armor") || flag(armor.Object.Flags[:], objectWearsFlag) {
		t.Fatalf("remove ownership/flags=%+v", next.Players["a"].Items)
	}
	if err := next.Validate(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s, original) || s.Players["a"].Items.Ready != ([20]string{}) {
		t.Fatal("mutation changed source snapshot")
	}
}

func TestEquipmentMutationUsesFirstEmptyNeckAndFingerSlots(t *testing.T) {
	s := equipmentMutationFixture(t)
	p := s.Players["a"]
	delete(p.Items.Items, "torch")
	delete(p.Items.Items, "sword")
	p.Items.Inventory = []string{"armor", "ring"}
	p.Items.Items["neck-a"] = Item{Object: LegacyObject{Name: "목걸이", Wear: 4, ShotsMax: 1, ShotsCurrent: 1}}
	p.Items.Items["neck-b"] = Item{Object: LegacyObject{Name: "목걸이", Wear: 4, ShotsMax: 1, ShotsCurrent: 1}}
	p.Items.Items["ring-2"] = Item{Object: LegacyObject{Name: "반지", Wear: 9, ShotsMax: 1, ShotsCurrent: 1}}
	p.Items.Inventory = append(p.Items.Inventory, "neck-a", "neck-b", "ring-2")
	s.Players["a"] = p

	next, first, err := s.ReadyItem("a", "목걸이", 1, EquipmentWear)
	if err != nil || first.Slot != equipmentNeck1 {
		t.Fatalf("first neck=%+v err=%v", first, err)
	}
	next, second, err := next.ReadyItem("a", "목걸이", 1, EquipmentWear)
	if err != nil || second.Slot != equipmentNeck2 {
		t.Fatalf("second neck=%+v err=%v", second, err)
	}
	next, first, err = next.ReadyItem("a", "반지", 1, EquipmentWear)
	if err != nil || first.Slot != equipmentFinger1 {
		t.Fatalf("first finger=%+v err=%v", first, err)
	}
	next, second, err = next.ReadyItem("a", "반지", 1, EquipmentWear)
	if err != nil || second.Slot != equipmentFinger1+1 {
		t.Fatalf("second finger=%+v err=%v", second, err)
	}
}

func TestEquipmentSelectorsMatchLegacyEqualPrefixesOnceInOrderAndVisibility(t *testing.T) {
	makeInventoryState := func(detect bool) State {
		s := equipmentMutationFixture(t)
		p := s.Players["a"]
		invisible := [8]byte{}
		invisible[objectInvisibleFlag/8] |= 1 << (objectInvisibleFlag % 8)
		p.Items = &ItemCollection{
			Items: map[string]Item{
				"display":  {Object: LegacyObject{Name: "NeedleDisplay", Keys: [3]string{"needle-display", "", ""}, Wear: equipmentBody, ShotsMax: 1, ShotsCurrent: 1}},
				"key0":     {Object: LegacyObject{Name: "ZeroLabel", Keys: [3]string{"needle-zero", "", ""}, Wear: equipmentArms, ShotsMax: 1, ShotsCurrent: 1}},
				"key1":     {Object: LegacyObject{Name: "OneLabel", Keys: [3]string{"", "needle-one", ""}, Wear: equipmentLegs, ShotsMax: 1, ShotsCurrent: 1}},
				"key2":     {Object: LegacyObject{Name: "TwoLabel", Keys: [3]string{"", "", "needle-two"}, Wear: equipmentHead, ShotsMax: 1, ShotsCurrent: 1}},
				"mixed":    {Object: LegacyObject{Name: "NeedleMixed", Keys: [3]string{"needle-mixed", "needle-alias", "needle-third"}, Wear: equipmentFeet, ShotsMax: 1, ShotsCurrent: 1}},
				"hidden":   {Object: LegacyObject{Name: "HiddenNeedle", Keys: [3]string{"needle-hidden", "", ""}, Wear: equipmentFace, ShotsMax: 1, ShotsCurrent: 1, Flags: invisible}},
				"equipped": {Object: LegacyObject{Name: "Equipped", Keys: [3]string{"equipped-only", "", ""}, Wear: equipmentWield, ShotsMax: 1, ShotsCurrent: 1}},
			},
			Inventory: []string{"display", "key0", "key1", "key2", "mixed", "hidden"},
			Ready:     [20]string{equipmentWield - 1: "equipped"},
		}
		if detect {
			p.Body.Flags[playerDetectInvisibleFlag/8] |= 1 << (playerDetectInvisibleFlag % 8)
		}
		s.Players["a"] = p
		return s
	}

	for _, tc := range []struct {
		selector string
		wantID   string
	}{
		{selector: "NEEDLED", wantID: "display"},
		{selector: "needle-z", wantID: "key0"},
		{selector: "NEEDLE-O", wantID: "key1"},
		{selector: "needle-t", wantID: "key2"},
	} {
		next, result, err := makeInventoryState(false).ReadyItem("a", tc.selector, 1, EquipmentWear)
		if err != nil || next.Players["a"].Items.Ready[result.Slot-1] != tc.wantID {
			t.Fatalf("selector=%q result=%+v err=%v items=%+v", tc.selector, result, err, next.Players["a"].Items)
		}
	}

	// The mixed root matches by display name and all three keys, but C's
	// find_obj contributes one occurrence for the canonical root.
	next, result, err := makeInventoryState(false).ReadyItem("a", "NeEdLe", 5, EquipmentWear)
	if err != nil || result.Slot != equipmentFeet || next.Players["a"].Items.Ready[equipmentFeet-1] != "mixed" {
		t.Fatalf("mixed inventory occurrence result=%+v err=%v items=%+v", result, err, next.Players["a"].Items)
	}
	if _, _, err := makeInventoryState(false).ReadyItem("a", "needle", 6, EquipmentWear); err == nil {
		t.Fatal("invisible inventory root counted without PDINVI")
	}
	next, result, err = makeInventoryState(true).ReadyItem("a", "needle", 6, EquipmentWear)
	if err != nil || result.Slot != equipmentFace || next.Players["a"].Items.Ready[equipmentFace-1] != "hidden" {
		t.Fatalf("detected invisible inventory result=%+v err=%v items=%+v", result, err, next.Players["a"].Items)
	}
	if _, _, err := makeInventoryState(false).ReadyItem("a", "equipped", 1, EquipmentWear); err == nil {
		t.Fatal("Ready root selected by inventory-only equipment command")
	}
	if _, _, err := makeInventoryState(false).RemoveItem("a", "needle", 1); err == nil {
		t.Fatal("Inventory root selected by ready-only equipment command")
	}

	makeReadyState := func(detect bool) State {
		s := equipmentMutationFixture(t)
		p := s.Players["a"]
		invisible := [8]byte{}
		invisible[objectInvisibleFlag/8] |= 1 << (objectInvisibleFlag % 8)
		p.Items = &ItemCollection{
			Items: map[string]Item{
				"hidden-ready": {Object: LegacyObject{Name: "HiddenReady", Keys: [3]string{"needle-hidden-ready", "", ""}, Wear: equipmentBody, ShotsMax: 1, ShotsCurrent: 1, Flags: invisible}},
				"ready-key0":   {Object: LegacyObject{Name: "ReadyZero", Keys: [3]string{"needle-ready-zero", "", ""}, Wear: equipmentArms, ShotsMax: 1, ShotsCurrent: 1}},
				"ready-key1":   {Object: LegacyObject{Name: "ReadyOne", Keys: [3]string{"", "needle-ready-one", ""}, Wear: equipmentLegs, ShotsMax: 1, ShotsCurrent: 1}},
				"ready-key2":   {Object: LegacyObject{Name: "ReadyTwo", Keys: [3]string{"", "", "needle-ready-two"}, Wear: equipmentHead, ShotsMax: 1, ShotsCurrent: 1}},
				"ready-mixed":  {Object: LegacyObject{Name: "NeedleReadyMixed", Keys: [3]string{"needle-ready-mixed", "needle-ready-alias", ""}, Wear: equipmentFeet, ShotsMax: 1, ShotsCurrent: 1}},
			},
			Ready: [20]string{"hidden-ready", "ready-key0", "ready-key1", "ready-key2", "ready-mixed"},
		}
		if detect {
			p.Body.Flags[playerDetectInvisibleFlag/8] |= 1 << (playerDetectInvisibleFlag % 8)
		}
		s.Players["a"] = p
		return s
	}

	next, result, err = makeReadyState(false).RemoveItem("a", "NEEDLE-READY-Z", 1)
	if err != nil || result.Slot != 2 || !containsID(next.Players["a"].Items.Inventory, "ready-key0") {
		t.Fatalf("ready key prefix result=%+v err=%v items=%+v", result, err, next.Players["a"].Items)
	}
	next, result, err = makeReadyState(false).RemoveItem("a", "needle-ready-o", 1)
	if err != nil || result.Slot != 3 || !containsID(next.Players["a"].Items.Inventory, "ready-key1") {
		t.Fatalf("ready key[1] prefix result=%+v err=%v items=%+v", result, err, next.Players["a"].Items)
	}
	next, result, err = makeReadyState(false).RemoveItem("a", "NEEDLE-READY-T", 1)
	if err != nil || result.Slot != 4 || !containsID(next.Players["a"].Items.Inventory, "ready-key2") {
		t.Fatalf("ready key[2] prefix result=%+v err=%v items=%+v", result, err, next.Players["a"].Items)
	}
	next, result, err = makeReadyState(false).RemoveItem("a", "needle-ready", 4)
	if err != nil || result.Slot != 5 || !containsID(next.Players["a"].Items.Inventory, "ready-mixed") {
		t.Fatalf("ready mixed occurrence result=%+v err=%v items=%+v", result, err, next.Players["a"].Items)
	}
	next, result, err = makeReadyState(false).RemoveItem("a", "needle", 1)
	if err != nil || result.Slot != 2 || !containsID(next.Players["a"].Items.Inventory, "ready-key0") {
		t.Fatalf("hidden ready visibility result=%+v err=%v items=%+v", result, err, next.Players["a"].Items)
	}
	next, result, err = makeReadyState(true).RemoveItem("a", "needle", 1)
	if err != nil || result.Slot != 1 || !containsID(next.Players["a"].Items.Inventory, "hidden-ready") {
		t.Fatalf("detected hidden ready result=%+v err=%v items=%+v", result, err, next.Players["a"].Items)
	}
}

func TestEquipmentMutationAllSkipsIneligibleAndRemovesNonCursed(t *testing.T) {
	s := equipmentMutationFixture(t)
	next, result, err := s.WearAllItems("a")
	if err != nil || result.Action != "wear-all" || result.Count != 2 || next.Players["a"].Items.Ready[equipmentBody-1] != "armor" || next.Players["a"].Items.Ready[equipmentFinger1-1] != "ring" {
		t.Fatalf("wear all result=%+v items=%+v err=%v", result, next.Players["a"].Items, err)
	}
	next, result, err = next.RemoveAllItems("a")
	if err != nil || result.Action != "remove-all" || result.Count != 2 || len(next.Players["a"].Items.Inventory) != 4 {
		t.Fatalf("remove all result=%+v items=%+v err=%v", result, next.Players["a"].Items, err)
	}
	if err := next.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestEquipmentMutationRejectsUnsafeOrCursedTransitionsWithoutPartialState(t *testing.T) {
	s := equipmentMutationFixture(t)
	p := s.Players["a"]
	bad := p.Items.Items["armor"]
	bad.Object.ShotsCurrent = 0
	p.Items.Items["armor"] = bad
	cursed := p.Items.Items["ring"]
	cursed.Object.Flags[objectCursedFlag/8] |= 1 << (objectCursedFlag % 8)
	p.Items.Items["ring"] = cursed
	s.Players["a"] = p

	if next, _, err := s.ReadyItem("a", "갑옷", 1, EquipmentWear); err == nil || !reflect.DeepEqual(next, State{}) {
		t.Fatalf("broken item partially equipped: next=%+v err=%v", next, err)
	}

	// Use a fresh valid snapshot to test cursed removal, avoiding any state
	// produced by the failed transition above.
	valid := equipmentMutationFixture(t)
	p = valid.Players["a"]
	item := p.Items.Items["armor"]
	item.Object.Flags[objectCursedFlag/8] |= 1 << (objectCursedFlag % 8)
	p.Items.Items["armor"] = item
	valid.Players["a"] = p
	ready, _, err := valid.ReadyItem("a", "갑옷", 1, EquipmentWear)
	if err != nil {
		t.Fatal(err)
	}
	if next, _, err := ready.RemoveItem("a", "갑옷", 1); err == nil || !strings.Contains(err.Error(), "저주") || !reflect.DeepEqual(next, State{}) {
		t.Fatalf("cursed item removed: next=%+v err=%v", next, err)
	}
	if valid.Players["a"].Items.Ready != ([20]string{}) {
		t.Fatal("failed remove changed source")
	}
}

func containsID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
