package world

import (
	"reflect"
	"testing"
)

func repairStateFixture() State {
	s := stateFixture()
	room := s.Rooms[1]
	room.Resource.Flags[RoomRepairFlag/8] |= 1 << (RoomRepairFlag % 8)
	s.Rooms[1] = room
	actor := s.Players["a"]
	actor.Body.Gold = 100
	actor.Body.Stats[4] = 0
	actor.Body.Flags[playerHiddenStateFlag/8] |= 1 << (playerHiddenStateFlag % 8)
	actor.Items = &ItemCollection{
		Items: map[string]Item{
			"sword": {Object: LegacyObject{
				Name: "검", Type: repairMissileType, Value: 100,
				ShotsMax: 30, ShotsCurrent: 0, DicePlus: 5,
				Adjustment: 2, Flags: [8]byte{repairEnchantedFlag / 8: 1 << (repairEnchantedFlag % 8)},
			}},
		},
		Inventory: []string{"sword"},
	}
	s.Players["a"] = actor
	return s
}

func TestPlanAndApplyRepairClearsAdjustmentChargesAndRestoresShots(t *testing.T) {
	s := repairStateFixture()
	original := s
	rolls := []struct {
		low, high, value int
	}{
		{1, 100, 100},
		{1, 50, 1},
		{5, 9, 9},
	}
	proposal, err := s.PlanRepair("a", "검", 1, func(low, high int) int {
		if len(rolls) == 0 {
			t.Fatal("repair consumed too many random outcomes")
		}
		outcome := rolls[0]
		rolls = rolls[1:]
		if low != outcome.low || high != outcome.high {
			t.Fatalf("repair random range=%d..%d want=%d..%d", low, high, outcome.low, outcome.high)
		}
		return outcome.value
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rolls) != 0 || proposal.ItemID != "sword" || proposal.Cost != 25 || proposal.Broke || !proposal.AdjustmentCleared || proposal.ShotsRoll != 9 {
		t.Fatalf("proposal=%+v remaining=%v", proposal, rolls)
	}

	next, result, err := s.ApplyRepair(proposal)
	if err != nil {
		t.Fatal(err)
	}
	item := next.Players["a"].Items.Items["sword"]
	if result.GoldBefore != 100 || result.GoldAfter != 75 || result.ShotsCurrent != 9 || item.Object.ShotsCurrent != 9 || item.Object.ShotsMax != 10 || item.Object.DicePlus != 3 || item.Object.Adjustment != 0 || flag(item.Object.Flags[:], repairEnchantedFlag) {
		t.Fatalf("result=%+v item=%+v", result, item)
	}
	nextActor := next.Players["a"]
	if flag(nextActor.Body.Flags[:], playerHiddenStateFlag) {
		t.Fatal("successful repair did not reveal actor")
	}
	if !reflect.DeepEqual(s, original) || s.Players["a"].Items.Items["sword"].Object.ShotsCurrent != 0 {
		t.Fatal("repair mutated the planning snapshot")
	}
	if err := next.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestRepairBreakRefundsAndRemovesWholeDirectInventoryRoot(t *testing.T) {
	s := repairStateFixture()
	actor := s.Players["a"]
	root := actor.Items.Items["sword"]
	root.Contents = []string{"gem"}
	actor.Items.Items["sword"] = root
	actor.Items.Items["gem"] = Item{Object: LegacyObject{Name: "보석"}}
	actor.Items.Inventory = []string{"sword"}
	s.Players["a"] = actor

	rolls := 0
	proposal, err := s.PlanRepair("a", "검", 1, func(low, high int) int {
		rolls++
		if low != 1 || high != 100 {
			t.Fatalf("break path consumed unexpected roll=%d..%d", low, high)
		}
		return 1
	})
	if err != nil || !proposal.Broke || !proposal.Refunded || !proposal.Removed || rolls != 1 {
		t.Fatalf("proposal=%+v err=%v rolls=%d", proposal, err, rolls)
	}
	next, result, err := s.ApplyRepair(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if result.GoldBefore != 100 || result.GoldAfter != 100 || !result.Broke || !result.Refunded || !result.Removed || len(next.Players["a"].Items.Items) != 0 || len(next.Players["a"].Items.Inventory) != 0 {
		t.Fatalf("result=%+v items=%+v", result, next.Players["a"].Items)
	}
	nextActor := next.Players["a"]
	if flag(nextActor.Body.Flags[:], playerHiddenStateFlag) {
		t.Fatal("break path did not reveal actor")
	}
	if err := next.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestRepairRequiresExactPositiveDirectOccurrenceAndAdmissionChecks(t *testing.T) {
	base := repairStateFixture()
	actor := base.Players["a"]
	second := actor.Items.Items["sword"]
	second.Object.Value = 200
	actor.Items.Items["sword-2"] = second
	actor.Items.Inventory = append(actor.Items.Inventory, "sword-2")
	base.Players["a"] = actor
	proposal, err := base.PlanRepair("a", "검", 2, func(low, high int) int {
		if low == 1 && high == 100 {
			return 100
		}
		if low == 1 && high == 50 {
			return 50
		}
		return 9
	})
	if err != nil || proposal.ItemID != "sword-2" || proposal.Cost != 50 {
		t.Fatalf("second occurrence proposal=%+v err=%v", proposal, err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*State)
	}{
		{name: "prefix", mutate: func(s *State) {}},
		{name: "onofix", mutate: func(s *State) {
			p := s.Players["a"]
			item := p.Items.Items["sword"]
			setObjectFlag(&item.Object.Flags, repairNoFixFlag, true)
			p.Items.Items["sword"] = item
			s.Players["a"] = p
		}},
		{name: "unsupported type", mutate: func(s *State) {
			p := s.Players["a"]
			item := p.Items.Items["sword"]
			item.Object.Type = 6
			p.Items.Items["sword"] = item
			s.Players["a"] = p
		}},
		{name: "not damaged", mutate: func(s *State) {
			p := s.Players["a"]
			item := p.Items.Items["sword"]
			item.Object.ShotsCurrent = 4
			p.Items.Items["sword"] = item
			s.Players["a"] = p
		}},
		{name: "insufficient gold", mutate: func(s *State) {
			p := s.Players["a"]
			p.Body.Gold = 1
			s.Players["a"] = p
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := repairStateFixture()
			test.mutate(&s)
			before := s
			name := "검"
			if test.name == "prefix" {
				name = "검 일부"
			}
			if _, err := s.PlanRepair("a", name, 1, func(int, int) int { t.Fatal("invalid repair consumed RNG"); return 1 }); err == nil {
				t.Fatal("invalid repair was admitted")
			}
			if !reflect.DeepEqual(s, before) {
				t.Fatal("invalid repair changed state")
			}
		})
	}
	for _, occurrence := range []int{0, -1} {
		if _, err := base.PlanRepair("a", "검", occurrence, func(int, int) int { t.Fatal("invalid occurrence consumed RNG"); return 1 }); err == nil {
			t.Fatalf("occurrence %d was admitted", occurrence)
		}
	}
}

func TestApplyRepairRejectsStaleOrTamperedOutcomeAtomically(t *testing.T) {
	s := repairStateFixture()
	proposal, err := s.PlanRepair("a", "검", 1, func(low, high int) int {
		if low == 1 && high == 100 {
			return 1
		}
		t.Fatal("break proposal should not consume later outcomes")
		return 0
	})
	if err != nil {
		t.Fatal(err)
	}
	tampered := proposal
	tampered.Broke = false
	tampered.Refunded = false
	tampered.Removed = false
	tampered.Response = "수리가 끝났습니다.\r\n"
	if next, _, err := s.ApplyRepair(tampered); err == nil || !reflect.DeepEqual(next, State{}) {
		t.Fatalf("tampered break result applied: next=%+v err=%v", next, err)
	}

	stale := repairStateFixture()
	staleProposal, err := stale.PlanRepair("a", "검", 1, func(low, high int) int {
		if low == 1 && high == 100 {
			return 100
		}
		if low == 1 && high == 50 {
			return 50
		}
		return 9
	})
	if err != nil {
		t.Fatal(err)
	}
	changed := stale.Players["a"]
	changed.Body.Gold++
	stale.Players["a"] = changed
	if next, _, err := stale.ApplyRepair(staleProposal); err == nil || !reflect.DeepEqual(next, State{}) {
		t.Fatalf("stale repair applied: next=%+v err=%v", next, err)
	}
}
