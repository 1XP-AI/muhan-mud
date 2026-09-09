package world

import (
	"reflect"
	"strings"
	"testing"
)

func valueTestState(flags [8]byte, items *ItemCollection) State {
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Flags: flags}},
				PlayerIDs: []string{"actor"},
			},
		},
		Players: map[string]PlayerState{
			"actor": {
				Body:   LegacyMonster{Name: "Alice", Type: 0, RoomID: 1},
				Online: true,
				Items:  items,
			},
		},
	}
}

func valueTestItems(objects map[string]Item, roots ...string) *ItemCollection {
	return &ItemCollection{Items: objects, Inventory: roots}
}

func TestQuoteValueByNamePawnCapsAndDoesNotMutate(t *testing.T) {
	var flags [8]byte
	flags[RoomPawnFlag/8] |= 1 << (RoomPawnFlag % 8)
	state := valueTestState(flags, valueTestItems(map[string]Item{
		"first":  {Object: LegacyObject{Name: "검", Value: 300001}},
		"second": {Object: LegacyObject{Name: "검", Value: 99}},
	}, "first", "second"))
	before := state

	result, err := state.QuoteValueByName("actor", "검", 2)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "value" || result.Mode != ValueModePawn || result.RoomID != 1 || result.ItemID != "second" || result.ItemName != "검" || result.Value != 99 || result.Quote != 49 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.Response == "" || !strings.Contains(result.Response, "49냥") {
		t.Fatalf("unexpected response: %q", result.Response)
	}
	if !reflect.DeepEqual(state, before) {
		t.Fatal("value quote mutated the world")
	}

	result, err = state.QuoteValueByName("actor", "검", 1)
	if err != nil || result.Quote != 100000 {
		t.Fatalf("cap result=%+v err=%v", result, err)
	}
}

func TestQuoteValueByNameRepairUsesQuarterAndExactRootOccurrence(t *testing.T) {
	var flags [8]byte
	flags[RoomRepairFlag/8] |= 1 << (RoomRepairFlag % 8)
	state := valueTestState(flags, valueTestItems(map[string]Item{
		"sword": {Object: LegacyObject{Name: "검", Value: 101}},
		"gem":   {Object: LegacyObject{Name: "보석", Value: 1000}},
	}, "sword", "gem"))

	result, err := state.QuoteValueByName("actor", "검", 1)
	if err != nil || result.Mode != ValueModeRepair || result.Quote != 25 || !strings.Contains(result.Response, "25냥") {
		t.Fatalf("repair result=%+v err=%v", result, err)
	}
	if _, err := state.QuoteValueByName("actor", "검", 0); err == nil {
		t.Fatal("zero occurrence accepted")
	}
	if _, err := state.QuoteValueByName("actor", "검", 2); err == nil {
		t.Fatal("missing occurrence accepted")
	}
	if _, err := state.QuoteValueByName("actor", "검 ", 1); err == nil {
		t.Fatal("non-exact item name accepted")
	}
}

func TestQuoteValueByNameVisibilityRoomAndMigrationBoundaries(t *testing.T) {
	var pawnFlags [8]byte
	pawnFlags[RoomPawnFlag/8] |= 1 << (RoomPawnFlag % 8)
	invisible := Item{Object: LegacyObject{Name: "숨은검", Value: 100}}
	invisible.Object.Flags[objectInvisibleFlag/8] |= 1 << (objectInvisibleFlag % 8)
	state := valueTestState(pawnFlags, valueTestItems(map[string]Item{
		"hidden": invisible,
		"bag":    {Object: LegacyObject{Name: "가방", Value: 100}, Contents: []string{"child"}},
		"child":  {Object: LegacyObject{Name: "보석", Value: 100}},
	}, "hidden", "bag"))

	if _, err := state.QuoteValueByName("actor", "숨은검", 1); err == nil {
		t.Fatal("invisible item visible without detection")
	}
	player := state.Players["actor"]
	player.Body.Flags[playerDetectInvisibleFlag/8] |= 1 << (playerDetectInvisibleFlag % 8)
	state.Players["actor"] = player
	result, err := state.QuoteValueByName("actor", "숨은검", 1)
	if err != nil || result.ItemID != "hidden" {
		t.Fatalf("detected invisible result=%+v err=%v", result, err)
	}
	if _, err := state.QuoteValueByName("actor", "보석", 1); err == nil {
		t.Fatal("nested item selected as direct inventory root")
	}
	if _, err := state.QuoteValueByName("actor", "가방", 1); err == nil {
		t.Fatal("nested container accepted")
	}

	state.Rooms[1] = RoomState{Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}}, PlayerIDs: []string{"actor"}}
	if _, err := state.QuoteValueByName("actor", "숨은검", 1); err == nil {
		t.Fatal("non-valuation room accepted")
	}

	state.Rooms[1] = RoomState{Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Flags: pawnFlags}}, PlayerIDs: []string{"actor"}}
	player = state.Players["actor"]
	player.Items = nil
	player.Body.Inventory = []LegacyObject{{Name: "legacy", Value: 1}}
	state.Players["actor"] = player
	if _, err := state.QuoteValueByName("actor", "legacy", 1); err == nil {
		t.Fatal("unmigrated legacy inventory accepted")
	}
}

func TestQuoteValueByNameRejectsMalformedCanonicalGraphAndNegativeValue(t *testing.T) {
	var flags [8]byte
	flags[RoomPawnFlag/8] |= 1 << (RoomPawnFlag % 8)
	state := valueTestState(flags, valueTestItems(map[string]Item{
		"bad": {Object: LegacyObject{Name: "검", Value: -1}},
	}, "missing"))
	if _, err := state.QuoteValueByName("actor", "검", 1); err == nil {
		t.Fatal("malformed canonical graph accepted")
	}

	state = valueTestState(flags, valueTestItems(map[string]Item{
		"bad": {Object: LegacyObject{Name: "검", Value: -1}},
	}, "bad"))
	if _, err := state.QuoteValueByName("actor", "검", 1); err == nil {
		t.Fatal("negative item value accepted")
	}
}
