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

func TestValueSelectorMatchesNameAndKeysOnceInInventoryOrder(t *testing.T) {
	var pawnFlags [8]byte
	pawnFlags[RoomPawnFlag/8] |= 1 << (RoomPawnFlag % 8)
	hidden := Item{Object: LegacyObject{Name: "NeedleHidden", Keys: [3]string{"", "needle-hidden", ""}, Value: 60}}
	hidden.Object.Flags[objectInvisibleFlag/8] |= 1 << (objectInvisibleFlag % 8)
	items := &ItemCollection{
		Items: map[string]Item{
			"name":   {Object: LegacyObject{Name: "NeedleName", Value: 10}},
			"key0":   {Object: LegacyObject{Name: "ZeroAlias", Keys: [3]string{"needle-zero", "", ""}, Value: 20}},
			"key1":   {Object: LegacyObject{Name: "OneAlias", Keys: [3]string{"", "needle-one", ""}, Value: 30}},
			"key2":   {Object: LegacyObject{Name: "TwoAlias", Keys: [3]string{"", "", "needle-two"}, Value: 40}},
			"mixed":  {Object: LegacyObject{Name: "NeedleMixed", Keys: [3]string{"needle-mixed", "", ""}, Value: 50}},
			"hidden": hidden,
			"parent": {Object: LegacyObject{Name: "NeedleParent", Value: 70}, Contents: []string{"child"}},
			"child":  {Object: LegacyObject{Name: "NeedleChild", Value: 80}},
			"ready":  {Object: LegacyObject{Name: "ReadyNeedle", Keys: [3]string{"ready-needle", "", ""}, Value: 90}},
			"other":  {Object: LegacyObject{Name: "Other", Keys: [3]string{"unrelated", "", ""}, Value: 100}},
		},
		Inventory: []string{"name", "key0", "key1", "key2", "mixed", "hidden", "parent", "other"},
		Ready:     [20]string{0: "ready"},
	}
	state := valueTestState(pawnFlags, items)
	for _, tc := range []struct {
		name     string
		selector string
		wantID   string
	}{
		{name: "display name prefix is case insensitive", selector: "NEEDLEN", wantID: "name"},
		{name: "key zero prefix", selector: "needle-z", wantID: "key0"},
		{name: "key one prefix", selector: "NEEDLE-O", wantID: "key1"},
		{name: "key two prefix", selector: "needle-t", wantID: "key2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := state.QuoteValueByName("actor", tc.selector, 1)
			if err != nil || result.ItemID != tc.wantID {
				t.Fatalf("selector=%q result=%+v err=%v", tc.selector, result, err)
			}
		})
	}

	// The mixed root matches through both its display name and key[0], but it
	// contributes exactly one occurrence after the four preceding roots.
	result, err := state.QuoteValueByName("actor", "NeEdLe", 5)
	if err != nil || result.ItemID != "mixed" {
		t.Fatalf("mixed occurrence result=%+v err=%v", result, err)
	}
	if _, err := state.QuoteValueByName("actor", "NeEdLe", 6); err == nil {
		t.Fatal("nested/hidden roots unexpectedly counted for ordinary actor")
	}
	if _, err := state.QuoteValueByName("actor", "not-a-key", 1); err == nil {
		t.Fatal("nonmatching key selected")
	}
	if _, err := state.QuoteValueByName("actor", "needle-child", 1); err == nil {
		t.Fatal("nested child selected through direct inventory selector")
	}
	if _, err := state.QuoteValueByName("actor", "ready", 1); err == nil {
		t.Fatal("equipped Ready root selected by inventory selector")
	}

	player := state.Players["actor"]
	player.Body.Flags[playerDetectInvisibleFlag/8] |= 1 << (playerDetectInvisibleFlag % 8)
	state.Players["actor"] = player
	result, err = state.QuoteValueByName("actor", "needle", 6)
	if err != nil || result.ItemID != "hidden" {
		t.Fatalf("PDINVI hidden occurrence result=%+v err=%v", result, err)
	}
}

func TestValueByNameHiddenPrefixNoOpStillClearsPHIDDN(t *testing.T) {
	var pawnFlags [8]byte
	pawnFlags[RoomPawnFlag/8] |= 1 << (RoomPawnFlag % 8)
	hidden := Item{Object: LegacyObject{Name: "NeedleHidden", Keys: [3]string{"needle-key", "", ""}, Value: 60}}
	hidden.Object.Flags[objectInvisibleFlag/8] |= 1 << (objectInvisibleFlag % 8)
	state := valueTestState(pawnFlags, valueTestItems(map[string]Item{"hidden": hidden}, "hidden"))
	hideValueActor(&state)
	next, result, err := state.ValueByName("actor", "NEEDLE", 1)
	if err != nil || result.Action != ValueNotHoldingAction || result.Response != ValueNotHoldingResponse {
		t.Fatalf("ordinary hidden selector result=%+v err=%v", result, err)
	}
	if valueActorHidden(next) {
		t.Fatal("named hidden miss did not clear PHIDDN")
	}
	player := state.Players["actor"]
	player.Body.Flags[playerDetectInvisibleFlag/8] |= 1 << (playerDetectInvisibleFlag % 8)
	state.Players["actor"] = player
	_, result, err = state.ValueByName("actor", "NEEDLE", 1)
	if err != nil || result.Action != ValueQuotedAction || result.ItemID != "hidden" {
		t.Fatalf("PDINVI hidden selector result=%+v err=%v", result, err)
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

func hideValueActor(s *State) {
	actor := s.Players["actor"]
	actor.Body.Flags[playerHiddenStateFlag/8] |= 1 << (playerHiddenStateFlag % 8)
	s.Players["actor"] = actor
}

func valueActorHidden(s State) bool {
	actor := s.Players["actor"]
	return flag(actor.Body.Flags[:], playerHiddenStateFlag)
}

func TestValueByNamePrintsCNotServiceWithoutClearingPHIDDN(t *testing.T) {
	state := valueTestState([8]byte{}, valueTestItems(map[string]Item{
		"sword": {Object: LegacyObject{Name: "검", Value: 100}},
	}, "sword"))
	hideValueActor(&state)
	original := state.clone()
	next, result, err := state.ValueByName("actor", "검", 1)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != ValueNotServiceAction || result.Response != ValueNotServiceResponse || result.RoomID != 1 {
		t.Fatalf("result=%+v", result)
	}
	if !valueActorHidden(next) {
		t.Fatal("not-service cleared PHIDDN")
	}
	if !reflect.DeepEqual(next, original) || !reflect.DeepEqual(state, original) {
		t.Fatal("not-service mutated or diverged from source")
	}
}

func TestValueByNamePrintsCAskWhatWithoutClearingPHIDDN(t *testing.T) {
	var flags [8]byte
	flags[RoomPawnFlag/8] |= 1 << (RoomPawnFlag % 8)
	state := valueTestState(flags, valueTestItems(map[string]Item{
		"sword": {Object: LegacyObject{Name: "검", Value: 100}},
	}, "sword"))
	hideValueActor(&state)
	original := state.clone()
	next, result, err := state.ValueByName("actor", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != ValueAskWhatAction || result.Response != ValueAskWhatResponse || result.RoomID != 1 {
		t.Fatalf("result=%+v", result)
	}
	if !valueActorHidden(next) {
		t.Fatal("ask-what cleared PHIDDN")
	}
	if !reflect.DeepEqual(next, original) || !reflect.DeepEqual(state, original) {
		t.Fatal("ask-what mutated or diverged from source")
	}
}

func TestValueByNamePrintsCNotServiceBeforeAskWhat(t *testing.T) {
	state := valueTestState([8]byte{}, valueTestItems(map[string]Item{
		"sword": {Object: LegacyObject{Name: "검", Value: 100}},
	}, "sword"))
	_, result, err := state.ValueByName("actor", "", 1)
	if err != nil || result.Action != ValueNotServiceAction || result.Response != ValueNotServiceResponse {
		t.Fatalf("bare value outside service room=%+v err=%v", result, err)
	}
}

func TestValueByNamePrintsCNotHoldingAfterClearingPHIDDN(t *testing.T) {
	var flags [8]byte
	flags[RoomPawnFlag/8] |= 1 << (RoomPawnFlag % 8)
	state := valueTestState(flags, valueTestItems(map[string]Item{
		"sword": {Object: LegacyObject{Name: "검", Value: 100}},
	}, "sword"))
	hideValueActor(&state)
	original := state.clone()
	next, result, err := state.ValueByName("actor", "방패", 1)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != ValueNotHoldingAction || result.Response != ValueNotHoldingResponse || result.ItemID != "" {
		t.Fatalf("result=%+v", result)
	}
	if valueActorHidden(next) {
		t.Fatal("not-holding leftover did not F_CLR PHIDDN")
	}
	if !valueActorHidden(original) || !reflect.DeepEqual(state, original) {
		t.Fatal("not-holding mutated source snapshot")
	}
	if !containsString(next.Players["actor"].Items.Inventory, "sword") {
		t.Fatal("not-holding moved inventory")
	}

	_, prefix, err := state.ValueByName("actor", "검술", 1)
	if err != nil || prefix.Response != ValueNotHoldingResponse {
		t.Fatalf("prefix name=%+v err=%v", prefix, err)
	}
	_, occ, err := state.ValueByName("actor", "검", 2)
	if err != nil || occ.Response != ValueNotHoldingResponse {
		t.Fatalf("missing occurrence=%+v err=%v", occ, err)
	}
}

func TestValueByNamePawnCapsRepairQuarterAndClearsPHIDDN(t *testing.T) {
	var pawnFlags [8]byte
	pawnFlags[RoomPawnFlag/8] |= 1 << (RoomPawnFlag % 8)
	pawn := valueTestState(pawnFlags, valueTestItems(map[string]Item{
		"first":  {Object: LegacyObject{Name: "검", Value: 300001}},
		"second": {Object: LegacyObject{Name: "검", Value: 99}},
	}, "first", "second"))
	hideValueActor(&pawn)
	before := pawn.clone()
	next, result, err := pawn.ValueByName("actor", "검", 1)
	if err != nil || result.Action != ValueQuotedAction || result.Mode != ValueModePawn || result.Quote != 100000 || result.ItemID != "first" || !strings.Contains(result.Response, "100000냥") {
		t.Fatalf("pawn result=%+v err=%v", result, err)
	}
	if valueActorHidden(next) {
		t.Fatal("successful pawn value did not F_CLR PHIDDN")
	}
	if !valueActorHidden(before) || !reflect.DeepEqual(pawn, before) {
		t.Fatal("successful pawn value mutated source")
	}
	_, second, err := pawn.ValueByName("actor", "검", 2)
	if err != nil || second.Quote != 49 || second.ItemID != "second" {
		t.Fatalf("pawn occurrence 2=%+v err=%v", second, err)
	}

	var repairFlags [8]byte
	repairFlags[RoomRepairFlag/8] |= 1 << (RoomRepairFlag % 8)
	repair := valueTestState(repairFlags, valueTestItems(map[string]Item{
		"sword": {Object: LegacyObject{Name: "검", Value: 101}},
	}, "sword"))
	hideValueActor(&repair)
	_, repairResult, err := repair.ValueByName("actor", "검", 1)
	if err != nil || repairResult.Mode != ValueModeRepair || repairResult.Quote != 25 || !strings.Contains(repairResult.Response, "25냥") {
		t.Fatalf("repair result=%+v err=%v", repairResult, err)
	}

	bothFlags := pawnFlags
	bothFlags[RoomRepairFlag/8] |= 1 << (RoomRepairFlag % 8)
	both := valueTestState(bothFlags, valueTestItems(map[string]Item{
		"sword": {Object: LegacyObject{Name: "검", Value: 100}},
	}, "sword"))
	_, bothResult, err := both.ValueByName("actor", "검", 1)
	if err != nil || bothResult.Mode != ValueModePawn || bothResult.Quote != 50 {
		t.Fatalf("both-flag result=%+v err=%v", bothResult, err)
	}
}

func TestValueByNameFailClosesUnmigratedItems(t *testing.T) {
	var flags [8]byte
	flags[RoomPawnFlag/8] |= 1 << (RoomPawnFlag % 8)
	state := valueTestState(flags, nil)
	actor := state.Players["actor"]
	actor.Body.Inventory = []LegacyObject{{Name: "legacy", Value: 1}}
	state.Players["actor"] = actor
	original := state.clone()
	next, result, err := state.ValueByName("actor", "legacy", 1)
	if err == nil || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(result, ValueResult{}) || !reflect.DeepEqual(state, original) {
		t.Fatalf("unmigrated items valued: next=%+v result=%+v err=%v", next, result, err)
	}
}
