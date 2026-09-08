package world

import (
	"reflect"
	"strings"
	"testing"
)

func peekItems(names ...string) *ItemCollection {
	items := ItemCollection{Items: map[string]Item{}}
	for index, name := range names {
		id := "item-" + string(rune('a'+index))
		items.Items[id] = Item{Object: LegacyObject{Name: name}}
		items.Inventory = append(items.Inventory, id)
	}
	return &items
}

func peekStateFixture(actorClass byte) State {
	actor := LegacyMonster{Name: "Alice", RoomID: 1, Type: 0, Class: actorClass, Level: 4, Stats: [5]byte{10, 10, 10, 10, 10}}
	actor.Flags[peekMaleFlag/8] |= 1 << (peekMaleFlag % 8)
	target := LegacyMonster{Name: "Bob", RoomID: 1, Type: 0, Class: 4, Level: 4}
	target.Flags[peekMaleFlag/8] |= 1 << (peekMaleFlag % 8)
	return State{
		Version: 1,
		Rooms:   map[int16]RoomState{1: {Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}}, PlayerIDs: []string{"alice", "bob"}}},
		Players: map[string]PlayerState{
			"alice": {Body: actor, Online: true, Items: peekItems()},
			"bob":   {Body: target, Online: true, Items: peekItems("검", "망토")},
		},
	}
}

func TestPlanAndApplyPeekSuccessPersistsTimerAndAlertProjection(t *testing.T) {
	s := peekStateFixture(peekThiefClass)
	rolls := []int{1, 100}
	proposal, err := s.PlanPeek("alice", "Bob", 100, func(low, high int) int {
		if low != 1 || high != 100 || len(rolls) == 0 {
			t.Fatalf("unexpected roll range or count: %d..%d rolls=%v", low, high, rolls)
		}
		value := rolls[0]
		rolls = rolls[1:]
		return value
	})
	if err != nil {
		t.Fatal(err)
	}
	if proposal.TargetID != "bob" || proposal.TargetKind != "player" || proposal.Chance != 30 || proposal.AlertChance != 20 || !proposal.Succeeded || !proposal.Alert || !proposal.TimerWrite || proposal.Response == "" {
		t.Fatalf("proposal=%+v", proposal)
	}
	if !strings.Contains(proposal.Response, "검") || proposal.TargetText == "" || proposal.RoomText == "" {
		t.Fatalf("proposal=%+v", proposal)
	}
	next, result, err := s.ApplyPeek(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if result.Response != proposal.Response || !result.Succeeded || !result.Alert || result.TargetID != "bob" || next.Players["alice"].Body.Timers[peekTimerIndex] != (LegacyTimer{LastTime: 100, Interval: 5}) {
		t.Fatalf("next=%+v result=%+v", next.Players["alice"], result)
	}
}

func TestPeekCooldownAndUnauthorizedArePureNoOps(t *testing.T) {
	unauthorized := peekStateFixture(4)
	before := unauthorized
	proposal, err := unauthorized.PlanPeek("alice", "Bob", 100, func(int, int) int { t.Fatal("unauthorized consumed RNG"); return 1 })
	if err != nil || proposal.TargetID != "" || !strings.Contains(proposal.Response, "직업") {
		t.Fatalf("unauthorized proposal=%+v err=%v", proposal, err)
	}
	next, result, err := unauthorized.ApplyPeek(proposal)
	if err != nil || !reflect.DeepEqual(next, before) || result.Response != proposal.Response {
		t.Fatalf("unauthorized next=%+v result=%+v err=%v", next, result, err)
	}

	cooldown := peekStateFixture(peekThiefClass)
	p := cooldown.Players["alice"]
	p.Body.Timers[peekTimerIndex] = LegacyTimer{LastTime: 100, Interval: 5}
	cooldown.Players["alice"] = p
	proposal, err = cooldown.PlanPeek("alice", "Bob", 102, func(int, int) int { t.Fatal("cooldown consumed RNG"); return 1 })
	if err != nil || !proposal.Cooldown || proposal.WaitSeconds != 3 || proposal.TimerWrite {
		t.Fatalf("cooldown proposal=%+v err=%v", proposal, err)
	}
	next, result, err = cooldown.ApplyPeek(proposal)
	if err != nil || !reflect.DeepEqual(next, cooldown) || result.Response != proposal.Response {
		t.Fatalf("cooldown next=%+v result=%+v err=%v", next, result, err)
	}
}

func TestPeekBlockedTargetWritesCooldownButDoesNotRevealItems(t *testing.T) {
	s := peekStateFixture(peekThiefClass)
	target := s.Players["bob"]
	target.Body.Flags[peekCannotStealFlag/8] |= 1 << (peekCannotStealFlag % 8)
	s.Players["bob"] = target
	proposal, err := s.PlanPeek("alice", "Bob", 100, func(int, int) int { t.Fatal("blocked target consumed RNG"); return 1 })
	if err != nil || !proposal.TimerWrite || proposal.Succeeded || !strings.Contains(proposal.Response, "도둑") {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	next, result, err := s.ApplyPeek(proposal)
	if err != nil || result.Succeeded || next.Players["alice"].Body.Timers[peekTimerIndex] != (LegacyTimer{LastTime: 100, Interval: 5}) {
		t.Fatalf("next=%+v result=%+v err=%v", next.Players["alice"], result, err)
	}
}

func TestPeekRejectsInvisibleTargetAndStaleInventory(t *testing.T) {
	s := peekStateFixture(peekThiefClass)
	target := s.Players["bob"]
	target.Body.Flags[peekInvisibleFlag/8] |= 1 << (peekInvisibleFlag % 8)
	s.Players["bob"] = target
	proposal, err := s.PlanPeek("alice", "Bob", 100, func(int, int) int { t.Fatal("invisible target consumed RNG"); return 1 })
	if err != nil || proposal.TargetID != "" || !strings.Contains(proposal.Response, "없어요") {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}

	s = peekStateFixture(peekThiefClass)
	proposal, err = s.PlanPeek("alice", "Bob", 100, func(int, int) int { return 1 })
	if err != nil {
		t.Fatal(err)
	}
	target = s.Players["bob"]
	target.Items = peekItems("다른 물건")
	s.Players["bob"] = target
	if _, _, err := s.ApplyPeek(proposal); err == nil {
		t.Fatal("stale peek inventory accepted")
	}
}
