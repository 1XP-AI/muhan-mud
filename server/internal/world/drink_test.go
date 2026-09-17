package world

import (
	"errors"
	"reflect"
	"testing"
)

func drinkTestState(object LegacyObject, ready bool) State {
	items := &ItemCollection{Items: map[string]Item{"potion": {Object: object}}}
	if ready {
		items.Ready[0] = "potion"
	} else {
		items.Inventory = []string{"potion"}
	}
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}}, Items: &ItemCollection{Items: map[string]Item{}}, PlayerIDs: []string{"a"}},
		},
		Players: map[string]PlayerState{
			"a": {Online: true, Body: LegacyMonster{Name: "아무개", Type: 0, RoomID: 1, Level: 20, HPMax: 30, HPCurrent: 10, MPMax: 20, MPCurrent: 5}, Items: items},
		},
	}
}

func TestDrinkSelectorUsesCaseInsensitiveEQUALPrefixesAndIndependentReadyFallback(t *testing.T) {
	actor := drinkTestState(LegacyObject{}, false).Players["a"]
	actor.Items = &ItemCollection{
		Items: map[string]Item{
			"display":         {Object: LegacyObject{Name: "HealingPotion"}},
			"key0":            {Object: LegacyObject{Name: "zero-label", Keys: [3]string{"alpha-potion", "", ""}}},
			"key1":            {Object: LegacyObject{Name: "one-label", Keys: [3]string{"", "beta-potion", ""}}},
			"key2":            {Object: LegacyObject{Name: "two-label", Keys: [3]string{"", "", "gamma-potion"}}},
			"inventory-ready": {Object: LegacyObject{Name: "not-a-potion", Keys: [3]string{"ready-potion", "", ""}}},
			"ready-one":       {Object: LegacyObject{Name: "first-ready", Keys: [3]string{"ready-potion", "", ""}}},
			"ready-two":       {Object: LegacyObject{Name: "second-ready", Keys: [3]string{"ready-potion", "", ""}}},
		},
		Inventory: []string{"display", "key0", "key1", "key2", "inventory-ready"},
		Ready:     [20]string{2: "ready-one", 7: "ready-two"},
	}
	if err := actor.Items.Validate(); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name       string
		selector   string
		occurrence int
		wantID     string
		wantPlace  DrinkLocation
		wantSlot   int
	}{
		{name: "display name prefix", selector: "HEAL", occurrence: 1, wantID: "display", wantPlace: DrinkInventoryRoot, wantSlot: -1},
		{name: "key zero prefix", selector: "ALPHA", occurrence: 1, wantID: "key0", wantPlace: DrinkInventoryRoot, wantSlot: -1},
		{name: "key one prefix", selector: "BETA", occurrence: 1, wantID: "key1", wantPlace: DrinkInventoryRoot, wantSlot: -1},
		{name: "key two prefix", selector: "GAMMA", occurrence: 1, wantID: "key2", wantPlace: DrinkInventoryRoot, wantSlot: -1},
		{name: "inventory precedence", selector: "READY-POTION", occurrence: 1, wantID: "inventory-ready", wantPlace: DrinkInventoryRoot, wantSlot: -1},
		{name: "independent ready occurrence", selector: "READY-POTION", occurrence: 2, wantID: "ready-two", wantPlace: DrinkReadySlot, wantSlot: 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id, _, place, slot, err := selectDrinkRoot(actor, tc.selector, tc.occurrence)
			if err != nil || id != tc.wantID || place != tc.wantPlace || slot != tc.wantSlot {
				t.Fatalf("selector=%q occurrence=%d got id=%q place=%q slot=%d err=%v", tc.selector, tc.occurrence, id, place, slot, err)
			}
		})
	}
}

func TestPlanApplyDrinkVigorConsumesOneChargeAndClearsHidden(t *testing.T) {
	object := LegacyObject{Name: "회복약", Type: drinkPotionType, MagicPower: 1, ShotsCurrent: 2, ShotsMax: 2}
	s := drinkTestState(object, false)
	actor := s.Players["a"]
	setSettingFlag(&actor.Body, 1, true)
	s.Players["a"] = actor
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	rollCalls := 0
	p, err := s.PlanDrink("a", "회복약", 1, DrinkOptions{Now: 100, Roll: func(low, high int) int {
		rollCalls++
		if low != 1 || high != 6 {
			t.Fatalf("roll range %d..%d", low, high)
		}
		return 4
	}})
	if err != nil {
		t.Fatal(err)
	}
	if rollCalls != 1 || p.ShotsAfter != 1 || !p.Consumed || !p.ClearHidden {
		t.Fatalf("proposal=%+v calls=%d", p, rollCalls)
	}
	if s.Players["a"].Body.HPCurrent != 10 {
		t.Fatal("planning mutated source")
	}
	next, result, err := s.ApplyDrink(p)
	if err != nil {
		t.Fatal(err)
	}
	got := next.Players["a"]
	if got.Body.HPCurrent != 14 || flag(got.Body.Flags[:], 1) || got.Items.Items["potion"].Object.ShotsCurrent != 1 {
		t.Fatalf("actor after drink=%+v", got)
	}
	if !result.Broadcast || result.Event == nil || result.HPDelta != 4 || result.SpellName != "회복" {
		t.Fatalf("result=%+v", result)
	}
	if _, _, err := next.ApplyDrink(p); err == nil {
		t.Fatal("expected stale proposal after commit")
	}
}

func TestPlanApplyDrinkHealingClampsAtHPMaxAndReplaysWithoutRNG(t *testing.T) {
	for _, tc := range []struct {
		name       string
		magicPower byte
		hpCurrent  int16
		rolls      []int
		wantRanges [][2]int
		wantDelta  int32
	}{
		{name: "vigor below max", magicPower: 1, hpCurrent: 29, rolls: []int{6}, wantRanges: [][2]int{{1, 6}}, wantDelta: 1},
		{name: "vigor at max", magicPower: 1, hpCurrent: 30, rolls: []int{6}, wantRanges: [][2]int{{1, 6}}, wantDelta: 0},
		{name: "mend below max", magicPower: 19, hpCurrent: 29, rolls: []int{6, 6}, wantRanges: [][2]int{{1, 6}, {1, 6}}, wantDelta: 1},
		{name: "mend at max", magicPower: 19, hpCurrent: 30, rolls: []int{6, 6}, wantRanges: [][2]int{{1, 6}, {1, 6}}, wantDelta: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := drinkTestState(LegacyObject{Name: "회복약", Type: drinkPotionType, MagicPower: tc.magicPower, ShotsCurrent: 1}, false)
			actor := s.Players["a"]
			actor.Body.HPCurrent = tc.hpCurrent
			s.Players["a"] = actor
			var gotRanges [][2]int
			rollCalls := 0
			p, err := s.PlanDrink("a", "회복약", 1, DrinkOptions{Now: 100, Roll: func(low, high int) int {
				gotRanges = append(gotRanges, [2]int{low, high})
				value := tc.rolls[rollCalls]
				rollCalls++
				return value
			}})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(gotRanges, tc.wantRanges) || !reflect.DeepEqual(p.Rolls, tc.rolls) || p.HPDelta != tc.wantDelta {
				t.Fatalf("proposal=%+v calls=%d ranges=%v", p, rollCalls, gotRanges)
			}
			if p.Effect != map[byte]string{1: "vigor", 19: "mend"}[tc.magicPower] || !p.Consumed || p.ShotsAfter != 0 {
				t.Fatalf("healing proposal=%+v", p)
			}

			next, result, err := s.ApplyDrink(p)
			if err != nil {
				t.Fatal(err)
			}
			if rollCalls != len(tc.rolls) {
				t.Fatalf("apply replay consumed another RNG draw: calls=%d", rollCalls)
			}
			got := next.Players["a"]
			if got.Body.HPCurrent != 30 || len(got.Items.Items) != 0 {
				t.Fatalf("actor after healing=%+v items=%+v", got.Body, got.Items)
			}
			if !result.Changed || !result.Consumed || !result.Broadcast || result.HPDelta != tc.wantDelta || result.MPDelta != 0 {
				t.Fatalf("healing result=%+v", result)
			}
		})
	}
}

func TestDrinkRestoreManaAdmitsIndex51ClampsHPAndFillsMPWithThreeDraws(t *testing.T) {
	s := drinkTestState(LegacyObject{Name: "전회복약", Type: drinkPotionType, MagicPower: 52, ShotsCurrent: 1}, false)
	actor := s.Players["a"]
	actor.Body.HPCurrent = 29
	actor.Body.MPCurrent = 5
	s.Players["a"] = actor
	values := []int{7, 8, 59}
	wantRanges := [][2]int{{1, 10}, {1, 10}, {1, 100}}
	var gotRanges [][2]int
	rollCalls := 0
	p, err := s.PlanDrink("a", "전회복약", 1, DrinkOptions{Now: 100, Roll: func(low, high int) int {
		gotRanges = append(gotRanges, [2]int{low, high})
		value := values[rollCalls]
		rollCalls++
		return value
	}})
	if err != nil {
		t.Fatal(err)
	}
	if rollCalls != 3 || !reflect.DeepEqual(gotRanges, wantRanges) || !reflect.DeepEqual(p.Rolls, values) {
		t.Fatalf("restore draws=%d ranges=%v rolls=%v", rollCalls, gotRanges, p.Rolls)
	}
	if p.SpellIndex != 51 || p.SpellName != "전회복" || p.Effect != "restore-mana" || p.HPDelta != 1 || p.MPDelta != 15 {
		t.Fatalf("restore proposal=%+v", p)
	}
	next, result, err := s.ApplyDrink(p)
	if err != nil {
		t.Fatal(err)
	}
	if rollCalls != 3 {
		t.Fatalf("apply replay consumed a second RNG sequence: calls=%d", rollCalls)
	}
	got := next.Players["a"]
	if got.Body.HPCurrent != 30 || got.Body.MPCurrent != 20 || len(got.Items.Items) != 0 || !result.Consumed || !result.Broadcast {
		t.Fatalf("restore result=%+v actor=%+v items=%+v", result, got.Body, got.Items)
	}
}

func TestDrinkRestoreManaConsumesWithoutMPFillAtRoll60(t *testing.T) {
	s := drinkTestState(LegacyObject{Name: "전회복약", Type: drinkPotionType, MagicPower: 52, ShotsCurrent: 1}, false)
	actor := s.Players["a"]
	actor.Body.HPCurrent = 10
	actor.Body.MPCurrent = 5
	s.Players["a"] = actor
	values := []int{1, 2, 60}
	rollCalls := 0
	p, err := s.PlanDrink("a", "전회복약", 1, DrinkOptions{Now: 100, Roll: func(low, high int) int {
		value := values[rollCalls]
		rollCalls++
		return value
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !p.Consumed || p.HPDelta != 3 || p.MPDelta != 0 || !reflect.DeepEqual(p.Rolls, values) {
		t.Fatalf("roll-60 proposal=%+v calls=%d", p, rollCalls)
	}
	next, result, err := s.ApplyDrink(p)
	if err != nil {
		t.Fatal(err)
	}
	got := next.Players["a"]
	if rollCalls != 3 || got.Body.HPCurrent != 13 || got.Body.MPCurrent != 5 || len(got.Items.Items) != 0 || !result.Consumed || !result.Broadcast {
		t.Fatalf("roll-60 result=%+v actor=%+v items=%+v calls=%d", result, got.Body, got.Items, rollCalls)
	}
}

func TestDrinkRejectsUnsupportedSpellBeforeConsuming(t *testing.T) {
	s := drinkTestState(LegacyObject{Name: "삭풍약", Type: drinkPotionType, MagicPower: 2, ShotsCurrent: 1}, false)
	before := s.Players["a"].Items.clone()
	_, err := s.PlanDrink("a", "삭풍약", 1, DrinkOptions{Now: 1})
	if !errors.Is(err, ErrDrinkSpellUnavailable) {
		t.Fatalf("err=%v", err)
	}
	if !reflectItemCollectionEqual(*s.Players["a"].Items, before) {
		t.Fatal("unsupported spell mutated inventory")
	}
}

func reflectItemCollectionEqual(a, b ItemCollection) bool {
	if len(a.Items) != len(b.Items) || len(a.Inventory) != len(b.Inventory) || a.Ready != b.Ready {
		return false
	}
	for id, item := range a.Items {
		if !reflect.DeepEqual(b.Items[id], item) {
			return false
		}
	}
	for i := range a.Inventory {
		if a.Inventory[i] != b.Inventory[i] {
			return false
		}
	}
	return true
}

func TestDrinkSupportsReadySlotAndSpecialChaosPotion(t *testing.T) {
	s := drinkTestState(LegacyObject{Name: "혼돈약", Type: drinkPotionType, ShotsCurrent: 1, Flags: [8]byte{5: 1 << 4}, Special: 2}, true)
	p, err := s.PlanDrink("a", "혼돈약", 1, DrinkOptions{Now: 9})
	if err != nil {
		t.Fatal(err)
	}
	next, result, err := s.ApplyDrink(p)
	if err != nil {
		t.Fatal(err)
	}
	got := next.Players["a"]
	if !result.Special || result.Location != DrinkReadySlot || !flag(got.Body.Flags[:], 28) || got.Items.Ready[0] != "" {
		t.Fatalf("next=%+v result=%+v", next.Players["a"], result)
	}
	if err := next.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestDrinkAlignmentBurnMovesRootToRoom(t *testing.T) {
	var flags [8]byte
	flags[drinkGoodOnlyFlag/8] |= 1 << (drinkGoodOnlyFlag % 8)
	object := LegacyObject{Name: "선약", Type: drinkPotionType, MagicPower: 1, ShotsCurrent: 1, Flags: flags}
	s := drinkTestState(object, false)
	actor := s.Players["a"]
	actor.Body.Alignment = -101
	s.Players["a"] = actor
	p, err := s.PlanDrink("a", "선약", 1, DrinkOptions{Now: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !p.MovedToRoom || p.Consumed {
		t.Fatalf("proposal=%+v", p)
	}
	next, result, err := s.ApplyDrink(p)
	if err != nil {
		t.Fatal(err)
	}
	if result.Consumed || !result.MovedToRoom || len(next.Players["a"].Items.Inventory) != 0 || len(next.Rooms[1].Items.Inventory) != 1 {
		t.Fatalf("next=%+v result=%+v", next, result)
	}
}

func TestDrinkSpecialStatsUsesCanonicalLegacyFlagBits(t *testing.T) {
	object := LegacyObject{Name: "수련약", Type: drinkPotionType, ShotsCurrent: 1, Flags: [8]byte{5: 1 << 4}, Special: 4}
	s := drinkTestState(object, false)
	actor := s.Players["a"]
	actor.Body.Stats = [5]byte{13, 13, 13, 13, 13} // sum 65; base threshold is 62
	actor.Body.Flags[52/8] |= 1 << (52 % 8)        // PPOWER adds three to the threshold
	s.Players["a"] = actor
	p, err := s.PlanDrink("a", "수련약", 1, DrinkOptions{Now: 1})
	if err != nil {
		t.Fatal(err)
	}
	next, _, err := s.ApplyDrink(p)
	if err != nil {
		t.Fatal(err)
	}
	for i, got := range next.Players["a"].Body.Stats {
		if got != 14 {
			t.Fatalf("stat[%d]=%d; canonical PPOWER bit was not applied", i, got)
		}
	}
}

func TestDrinkSpecialMapMovesActorAndConsumesCharge(t *testing.T) {
	s := drinkTestState(LegacyObject{Name: "지도약", Type: drinkPotionType, ShotsCurrent: 1, Value: 2, Flags: [8]byte{5: 1 << 4}, Special: 5}, false)
	s.Rooms[2] = RoomState{Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 2, LowLevel: 1, HighLevel: 50}}}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	p, err := s.PlanDrink("a", "지도약", 1, DrinkOptions{Now: 10})
	if err != nil {
		t.Fatal(err)
	}
	next, result, err := s.ApplyDrink(p)
	if err != nil {
		t.Fatal(err)
	}
	if !result.MovedToRoom || next.Players["a"].Body.RoomID != 2 || len(next.Rooms[1].PlayerIDs) != 0 || len(next.Rooms[2].PlayerIDs) != 1 || len(next.Players["a"].Items.Items) != 0 {
		t.Fatalf("next=%+v result=%+v", next, result)
	}
}
