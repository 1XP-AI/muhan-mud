package world

import (
	"errors"
	"reflect"
	"testing"
)

func readScrollTestState(object LegacyObject, ready bool) State {
	items := &ItemCollection{Items: map[string]Item{"scroll-1": {Object: object}}}
	if ready {
		items.Ready[2] = "scroll-1"
	} else {
		items.Inventory = []string{"scroll-1"}
	}
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}},
				Items:     &ItemCollection{Items: map[string]Item{}},
				PlayerIDs: []string{"alice"},
			},
		},
		Players: map[string]PlayerState{
			"alice": {
				Online: true,
				Body: LegacyMonster{
					Name: "Alice", Type: 0, Class: 5, Level: 10,
					RoomID: 1, HPMax: 30, HPCurrent: 10, MPMax: 20, MPCurrent: 5,
					Stats: [5]byte{10, 10, 10, 10, 10},
				},
				Items: items,
			},
		},
	}
}

func TestPlanApplyReadScrollSuccessConsumesScrollAndStoresDraws(t *testing.T) {
	s := readScrollTestState(LegacyObject{
		Name: "회복부", Type: readScrollType, DiceCount: 1, MagicPower: 1, ShotsCurrent: 1,
	}, false)
	actor := s.Players["alice"]
	setSettingFlag(&actor.Body, readScrollHiddenFlag, true)
	s.Players["alice"] = actor

	var draws []int
	p, err := s.PlanReadScroll("alice", "회복부", 1, ScrollOptions{Now: 100, Roll: func(low, high int) int {
		draws = append(draws, low)
		if len(draws) == 1 {
			if low != 1 || high != 100 {
				t.Fatalf("spell-fail range=%d..%d", low, high)
			}
			return 1
		}
		if low != 1 || high != 6 {
			t.Fatalf("effect range=%d..%d", low, high)
		}
		return 4
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.Rolls, []int{1, 4}) || !p.Consumed || !p.Succeeded || p.SpellFailed || !p.ClearHidden || p.ShotsAfter != 0 {
		t.Fatalf("proposal=%+v", p)
	}
	if s.Players["alice"].Body.HPCurrent != 10 {
		t.Fatal("planning mutated actor")
	}
	next, result, err := s.ApplyReadScroll(p)
	if err != nil {
		t.Fatal(err)
	}
	got := next.Players["alice"]
	if got.Body.HPCurrent != 14 || flag(got.Body.Flags[:], readScrollHiddenFlag) || got.Body.Timers[readScrollTimerIndex] != (LegacyTimer{LastTime: 100, Interval: 3}) {
		t.Fatalf("actor after read=%+v", got.Body)
	}
	if len(got.Items.Items) != 0 || len(got.Items.Inventory) != 0 || result.Response == "" || !result.Broadcast || result.Event == nil || result.HPDelta != 4 {
		t.Fatalf("result=%+v items=%+v", result, got.Items)
	}
	if _, _, err := next.ApplyReadScroll(p); err == nil {
		t.Fatal("expected stale proposal after commit")
	}
}

func TestReadScrollSpellFailureConsumesWithoutEffectRoll(t *testing.T) {
	s := readScrollTestState(LegacyObject{
		Name: "실패부", Type: readScrollType, DiceCount: 1, MagicPower: 1, ShotsCurrent: 1,
	}, false)
	actor := s.Players["alice"]
	actor.Body.Class = 2 // barbarian: low spell-failure chance
	actor.Body.HPCurrent = 10
	setSettingFlag(&actor.Body, readScrollHiddenFlag, true)
	s.Players["alice"] = actor
	rolls := 0
	p, err := s.PlanReadScroll("alice", "실패부", 1, ScrollOptions{Now: 200, Roll: func(low, high int) int {
		rolls++
		if low != 1 || high != 100 {
			t.Fatalf("spell failure consumed an effect roll: %d..%d", low, high)
		}
		return 100
	}})
	if err != nil {
		t.Fatal(err)
	}
	if rolls != 1 || !p.SpellFailed || p.Succeeded || !p.Consumed || !p.ClearHidden || !reflect.DeepEqual(p.Rolls, []int{100}) {
		t.Fatalf("proposal=%+v calls=%d", p, rolls)
	}
	next, result, err := s.ApplyReadScroll(p)
	if err != nil {
		t.Fatal(err)
	}
	got := next.Players["alice"]
	if got.Body.HPCurrent != 10 || flag(got.Body.Flags[:], readScrollHiddenFlag) || got.Body.Timers[readScrollTimerIndex] != (LegacyTimer{LastTime: 200, Interval: 3}) || len(got.Items.Items) != 0 {
		t.Fatalf("failed read mutated wrong state: %+v", got)
	}
	if result.Succeeded || !result.SpellFailed || result.Broadcast || result.Event != nil {
		t.Fatalf("failure result=%+v", result)
	}
}

func TestReadScrollChecksAdmissionBeforeRandomnessAndSupportsReadyRoot(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*State)
		want   error
	}{
		{name: "blind", mutate: func(s *State) {
			p := s.Players["alice"]
			setSettingFlag(&p.Body, readScrollBlindFlag, true)
			s.Players["alice"] = p
		}, want: ErrReadScrollBlind},
		{name: "not-scroll", mutate: func(s *State) {
			p := s.Players["alice"]
			item := p.Items.Items["scroll-1"]
			item.Object.Type = 6
			p.Items.Items["scroll-1"] = item
			s.Players["alice"] = p
		}, want: ErrReadScrollNotScroll},
		{name: "empty", mutate: func(s *State) {
			p := s.Players["alice"]
			item := p.Items.Items["scroll-1"]
			item.Object.ShotsCurrent = 0
			p.Items.Items["scroll-1"] = item
			s.Players["alice"] = p
		}, want: ErrReadScrollEmpty},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := readScrollTestState(LegacyObject{Name: "두루마리", Type: readScrollType, MagicPower: 1, ShotsCurrent: 1}, false)
			tc.mutate(&s)
			called := false
			_, err := s.PlanReadScroll("alice", "두루마리", 1, ScrollOptions{Now: 1, Roll: func(int, int) int {
				called = true
				return 1
			}})
			if !errors.Is(err, tc.want) || called {
				t.Fatalf("err=%v want=%v random=%v", err, tc.want, called)
			}
		})
	}

	s := readScrollTestState(LegacyObject{Name: "장비두루마리", Type: readScrollType, MagicPower: 1, ShotsCurrent: 1}, true)
	p, err := s.PlanReadScroll("alice", "장비두루마리", 1, ScrollOptions{Now: 1, Roll: func(int, int) int { return 1 }})
	if err != nil || p.Location != ScrollReadySlot || p.ReadySlot != 2 {
		t.Fatalf("ready proposal=%+v err=%v", p, err)
	}
}

func TestReadScrollRejectsUnsupportedWithoutConsumptionAndAlignmentMovesToRoom(t *testing.T) {
	s := readScrollTestState(LegacyObject{Name: "공격부", Type: readScrollType, MagicPower: 2, ShotsCurrent: 1}, false)
	before := s.Players["alice"].Items.clone()
	_, err := s.PlanReadScroll("alice", "공격부", 1, ScrollOptions{Now: 1, Roll: func(int, int) int {
		t.Fatal("unsupported spell consumed RNG")
		return 1
	}})
	if !errors.Is(err, ErrReadScrollUnsupported) || !reflect.DeepEqual(*s.Players["alice"].Items, before) {
		t.Fatalf("unsupported err=%v items=%+v", err, s.Players["alice"].Items)
	}

	var flags [8]byte
	flags[readScrollGoodOnlyFlag/8] |= 1 << (readScrollGoodOnlyFlag % 8)
	s = readScrollTestState(LegacyObject{Name: "선부", Type: readScrollType, MagicPower: 1, ShotsCurrent: 1, Flags: flags}, false)
	actor := s.Players["alice"]
	actor.Body.Alignment = -101
	setSettingFlag(&actor.Body, readScrollHiddenFlag, true)
	s.Players["alice"] = actor
	p, err := s.PlanReadScroll("alice", "선부", 1, ScrollOptions{Now: 1})
	if err != nil || !p.MovedToRoom || p.Consumed || p.ClearHidden {
		t.Fatalf("alignment proposal=%+v err=%v", p, err)
	}
	next, result, err := s.ApplyReadScroll(p)
	nextActor := next.Players["alice"]
	if err != nil || !result.MovedToRoom || result.Consumed || len(nextActor.Items.Items) != 0 || len(next.Rooms[1].Items.Items) != 1 || !flag(nextActor.Body.Flags[:], readScrollHiddenFlag) {
		t.Fatalf("alignment result=%+v next=%+v err=%v", result, next, err)
	}
}

func TestApplyReadScrollRejectsReceiptTamperingAndStaleRoom(t *testing.T) {
	s := readScrollTestState(LegacyObject{Name: "두루마리", Type: readScrollType, MagicPower: 1, ShotsCurrent: 1}, false)
	p, err := s.PlanReadScroll("alice", "두루마리", 1, ScrollOptions{Now: 1, Roll: func(int, int) int { return 1 }})
	if err != nil {
		t.Fatal(err)
	}
	tampered := p
	tampered.Response += "변조"
	if _, _, err := s.ApplyReadScroll(tampered); err == nil {
		t.Fatal("tampered receipt accepted")
	}
	changed := s.clone()
	room := changed.Rooms[1]
	room.Resource.Flags[readScrollNoMagicRoomFlag/8] |= 1 << (readScrollNoMagicRoomFlag % 8)
	changed.Rooms[1] = room
	before := changed
	if _, _, err := changed.ApplyReadScroll(p); err == nil {
		t.Fatal("stale room proposal accepted")
	}
	if !reflect.DeepEqual(changed, before) {
		t.Fatal("stale apply mutated state")
	}
}

func TestReadScrollSelectorUsesLegacyEqualVisibilityPrecedenceAndReadyFallback(t *testing.T) {
	scroll := func(object LegacyObject) LegacyObject {
		object.Type = readScrollType
		object.MagicPower = 1
		object.ShotsCurrent = 1
		return object
	}

	hidden := scroll(LegacyObject{Name: "Hidden Selector Scroll", Keys: [3]string{"hidden-prefix", "", ""}})
	hidden.Flags[objectInvisibleFlag/8] |= 1 << (objectInvisibleFlag % 8)

	s := readScrollTestState(scroll(LegacyObject{Name: "unused"}), false)
	actor := s.Players["alice"]
	actor.Items = &ItemCollection{
		Items: map[string]Item{
			"display": {
				Object: scroll(LegacyObject{Name: "Display Prefix Scroll"}),
			},
			"key0": {
				Object: scroll(LegacyObject{Name: "Key Zero Scroll", Keys: [3]string{"slot-zero", "", ""}}),
			},
			"key1": {
				Object: scroll(LegacyObject{Name: "Key One Scroll", Keys: [3]string{"", "slot-one", ""}}),
			},
			"key2": {
				Object: scroll(LegacyObject{Name: "Key Two Scroll", Keys: [3]string{"", "", "slot-two"}}),
			},
			"mixed": {
				Object: scroll(LegacyObject{Name: "Mixed Display", Keys: [3]string{"prefix-mixed", "prefix-mixed-alias", "prefix-mixed-third"}}),
			},
			"mixed-second": {
				Object: scroll(LegacyObject{Name: "Another Mixed Scroll", Keys: [3]string{"prefix-mixed-second", "", ""}}),
			},
			"hidden": {Object: hidden},
			"precedence-inventory": {
				Object: scroll(LegacyObject{Name: "Precedence Inventory"}),
			},
			"precedence-ready": {
				Object: scroll(LegacyObject{Name: "Precedence Ready"}),
			},
			"independent-inventory": {
				Object: scroll(LegacyObject{Name: "Independent Inventory"}),
			},
			"independent-ready-one": {
				Object: scroll(LegacyObject{Name: "Independent Ready One"}),
			},
			"independent-ready-two": {
				Object: scroll(LegacyObject{Name: "Independent Ready Two"}),
			},
		},
		Inventory: []string{
			"display", "key0", "key1", "key2", "mixed", "mixed-second", "hidden",
			"precedence-inventory", "independent-inventory",
		},
		Ready: [20]string{
			3:  "precedence-ready",
			7:  "independent-ready-one",
			11: "independent-ready-two",
		},
	}
	s.Players["alice"] = actor

	plan := func(name string, occurrence int) ScrollProposal {
		t.Helper()
		proposal, err := s.PlanReadScroll("alice", name, occurrence, ScrollOptions{Now: 1, Roll: func(int, int) int { return 1 }})
		if err != nil {
			t.Fatalf("selector %q occurrence %d: %v", name, occurrence, err)
		}
		return proposal
	}

	for _, tc := range []struct {
		name string
		want string
	}{
		{name: "DISPLAY PREFIX", want: "display"},
		{name: "SLOT-ZERO", want: "key0"},
		{name: "SLOT-ONE", want: "key1"},
		{name: "SLOT-TWO", want: "key2"},
	} {
		proposal := plan(tc.name, 1)
		if proposal.ItemID != tc.want || proposal.Location != ScrollInventoryRoot {
			t.Fatalf("selector %q proposal=%+v want item=%q inventory", tc.name, proposal, tc.want)
		}
	}

	mixed := plan("PREFIX-MIXED", 2)
	if mixed.ItemID != "mixed-second" || mixed.Location != ScrollInventoryRoot {
		t.Fatalf("matching multiple fields counted more than once proposal=%+v", mixed)
	}

	if _, err := s.PlanReadScroll("alice", "HIDDEN", 1, ScrollOptions{Now: 1, Roll: func(int, int) int {
		t.Fatal("invisible root consumed RNG without PDINVI")
		return 1
	}}); !errors.Is(err, ErrReadScrollMissingItem) {
		t.Fatalf("invisible root selected without PDINVI: %v", err)
	}
	actor = s.Players["alice"]
	actor.Body.Flags[playerDetectInvisibleFlag/8] |= 1 << (playerDetectInvisibleFlag % 8)
	s.Players["alice"] = actor
	hiddenProposal := plan("hidden", 1)
	if hiddenProposal.ItemID != "hidden" {
		t.Fatalf("PDINVI did not reveal hidden root proposal=%+v", hiddenProposal)
	}

	precedence := plan("precedence", 1)
	if precedence.ItemID != "precedence-inventory" || precedence.Location != ScrollInventoryRoot {
		t.Fatalf("ready root overrode direct Inventory precedence proposal=%+v", precedence)
	}

	independent := plan("independent", 2)
	if independent.ItemID != "independent-ready-two" || independent.Location != ScrollReadySlot || independent.ReadySlot != 11 {
		t.Fatalf("Ready fallback did not reset occurrence independently proposal=%+v", independent)
	}
}
