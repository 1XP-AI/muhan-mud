package world

import (
	"errors"
	"strings"
	"testing"
)

func compareStateFixture(objects map[string]Item, roots []string, class, level byte) State {
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}},
				PlayerIDs: []string{"actor"},
			},
		},
		Players: map[string]PlayerState{
			"actor": {
				Body:   LegacyMonster{Name: "Alice", Type: 0, RoomID: 1, Class: class, Level: level},
				Online: true,
				Items:  &ItemCollection{Items: objects, Inventory: roots},
			},
		},
	}
}

func TestCompareItemByNameUsesCanonicalRootOccurrenceAndSourceArmorSemantics(t *testing.T) {
	s := compareStateFixture(map[string]Item{
		"armor-body":  {Object: LegacyObject{Name: "갑옷", Type: 5, Armor: 15, Wear: 1}},
		"armor-other": {Object: LegacyObject{Name: "갑옷", Type: 5, Armor: 15, Wear: 2}},
	}, []string{"armor-body", "armor-other"}, 4, 1)

	result, err := s.CompareItemByName("actor", "갑옷", 1)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "compare" || result.ItemID != "armor-body" || result.Kind != CompareArmor || result.Occurrence != 1 {
		t.Fatalf("result=%+v", result)
	}
	if result.CheckAC != 30 || result.RequiredLevel != 30 || result.Universal {
		t.Fatalf("armor body result=%+v", result)
	}
	if !strings.Contains(result.Response, "30 레벨부터 입을 수 있습니다") {
		t.Fatalf("response=%q", result.Response)
	}

	result, err = s.CompareItemByName("actor", "갑옷", 2)
	if err != nil {
		t.Fatal(err)
	}
	if result.ItemID != "armor-other" || result.CheckAC != 75 || result.RequiredLevel != 75 {
		t.Fatalf("armor non-body result=%+v", result)
	}
	if !strings.Contains(result.Response, "75 레벨부터 입을 수 있습니다") {
		t.Fatalf("response=%q", result.Response)
	}
}

func TestCompareItemByNameAppliesSourceWeaponClassAdjustments(t *testing.T) {
	for _, tc := range []struct {
		name       string
		class      byte
		check      int
		required   int
		universal  bool
		classTable CompareClassLevels
	}{
		{name: "fighter", class: 4, check: 25, required: 75},
		{name: "assassin", class: 1, check: 29, required: 87},
		{name: "thief", class: 8, check: 29, required: 87},
		{name: "paladin", class: 6, check: 30, required: 90},
		{name: "ranger", class: 7, check: 30, required: 90},
		{name: "ordinary", class: 3, check: 32, required: 96},
		{name: "universal", class: 4, check: 13, required: 0, universal: true},
		{name: "invincible", class: 9, check: 32, classTable: CompareClassLevels{Fighter: 75, AssassinThief: 87, PaladinRanger: 90, Other: 96}},
	} {
		rawDamage := int16(30)
		if tc.universal {
			rawDamage = 18
		}
		s := compareStateFixture(map[string]Item{
			"weapon": {Object: LegacyObject{Name: "검", Type: 0, DiceCount: 1, DiceSides: rawDamage, DicePlus: 2}},
		}, []string{"weapon"}, tc.class, 1)
		result, err := s.CompareItemByName("actor", "검", 1)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if result.CheckDamage != tc.check || result.RequiredLevel != tc.required || result.Universal != tc.universal {
			t.Fatalf("%s: result=%+v", tc.name, result)
		}
		if tc.classTable != (CompareClassLevels{}) && result.ClassLevels != tc.classTable {
			t.Fatalf("%s: class levels=%+v", tc.name, result.ClassLevels)
		}
	}

	// A low weapon damage value is explicitly available to every class.
	s := compareStateFixture(map[string]Item{
		"weapon": {Object: LegacyObject{Name: "단검", Type: 0, DiceCount: 1, DiceSides: 1, DicePlus: 1}},
	}, []string{"weapon"}, 9, 1)
	result, err := s.CompareItemByName("actor", "단검", 1)
	if err != nil || !result.Universal || result.ClassLevels != (CompareClassLevels{}) {
		t.Fatalf("low invincible weapon result=%+v err=%v", result, err)
	}
}

func TestCompareItemByNameReturnsSourceFailureResultsAndRejectsUnmigratedData(t *testing.T) {
	s := compareStateFixture(map[string]Item{
		"potion": {Object: LegacyObject{Name: "물약", Type: 6}},
	}, []string{"potion"}, 4, 1)
	result, err := s.CompareItemByName("actor", "물약", 1)
	if err != nil || result.Success || result.ErrorCode != CompareUnsupportedItem || result.Response != "무기나 방어구만 가능합니다.\r\n" {
		t.Fatalf("unsupported result=%+v err=%v", result, err)
	}
	result, err = s.CompareItemByName("actor", "없는것", 1)
	if err != nil || result.Success || result.ErrorCode != CompareItemNotFound || result.Response != "당신은 그런 것을 갖고 있지 않습니다.\r\n" {
		t.Fatalf("missing result=%+v err=%v", result, err)
	}

	if _, err := s.CompareItemByName("actor", "물약", 0); !errors.Is(err, ErrCompareInvalidOccurrence) {
		t.Fatalf("zero occurrence err=%v", err)
	}
	if result, err := s.CompareItemByName("actor", "물", 1); err != nil || result.ErrorCode != CompareItemNotFound {
		t.Fatalf("prefix lookup result=%+v err=%v", result, err)
	}

	s.Players["actor"] = PlayerState{Body: LegacyMonster{Name: "Alice", Type: 0, RoomID: 1, Inventory: []LegacyObject{{Name: "검", Type: 0}}}, Online: true}
	if _, err := s.CompareItemByName("actor", "검", 1); !errors.Is(err, ErrCompareCanonicalInventoryRequired) {
		t.Fatalf("legacy inventory err=%v", err)
	}

	s = compareStateFixture(map[string]Item{
		"bag":    {Object: LegacyObject{Name: "가방", Type: 9}, Contents: []string{"nested"}},
		"nested": {Object: LegacyObject{Name: "검", Type: 0}},
	}, []string{"bag"}, 4, 1)
	if result, err := s.CompareItemByName("actor", "검", 1); err != nil || result.ErrorCode != CompareItemNotFound {
		t.Fatalf("nested lookup result=%+v err=%v", result, err)
	}
}

func TestComparePromptMatchesBareSourceCommand(t *testing.T) {
	s := compareStateFixture(map[string]Item{}, nil, 4, 1)
	result, err := s.ComparePrompt("actor")
	if err != nil || result.Success || result.ErrorCode != CompareMissingItem || result.Response != "무엇을 비교하시려고요?\r\n" {
		t.Fatalf("prompt=%+v err=%v", result, err)
	}
}
