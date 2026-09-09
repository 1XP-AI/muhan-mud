package world

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func hideStateFixture(class, level, dex byte) State {
	actor := LegacyMonster{
		Name:   "Alice",
		Type:   0,
		Class:  class,
		Level:  level,
		Stats:  [5]byte{10, dex, 10, 10, 10},
		RoomID: 1,
	}
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{1: {
			Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}},
			PlayerIDs: []string{"alice"},
		}},
		Players: map[string]PlayerState{
			"alice": {Body: actor, Online: true},
		},
	}
}

func hideObjectStateFixture(class, level, dex byte) State {
	s := hideStateFixture(class, level, dex)
	room := s.Rooms[1]
	room.Items = &ItemCollection{Items: map[string]Item{
		"sword-1": {Object: LegacyObject{Name: "검"}, Contents: []string{"nested"}},
		"sword-2": {Object: LegacyObject{Name: "검"}},
		"nested":  {Object: LegacyObject{Name: "보석"}},
	}, Inventory: []string{"sword-1", "sword-2"}}
	s.Rooms[1] = room
	return s
}

func TestHideChancePortsClassFormulaIntervalAndBlindCap(t *testing.T) {
	cases := []struct {
		name     string
		class    byte
		level    byte
		dex      byte
		blind    bool
		want     int
		interval int32
	}{
		{name: "ordinary", class: 4, level: 4, dex: 15, want: 10, interval: 15},
		{name: "thief", class: hideThiefClass, level: 4, dex: 15, want: 14, interval: 5},
		{name: "assassin", class: hideAssassinClass, level: 4, dex: 15, want: 14, interval: 5},
		{name: "ranger", class: hideRangerClass, level: 4, dex: 15, want: 18, interval: 5},
		{name: "caretaker-cap", class: hideCaretakerClass, level: 100, dex: 63, want: 90, interval: 15},
		{name: "ranger-no-cap", class: hideRangerClass, level: 100, dex: 63, want: 276, interval: 5},
		{name: "blind-cap", class: hideRangerClass, level: 100, dex: 63, blind: true, want: 20, interval: 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := LegacyMonster{Class: tc.class, Level: tc.level, Stats: [5]byte{10, tc.dex}}
			if tc.blind {
				body.Flags[hideBlindFlag/8] |= 1 << (hideBlindFlag % 8)
			}
			chance, err := HideChance(body)
			if err != nil || chance != tc.want {
				t.Fatalf("chance=%d err=%v, want %d", chance, err, tc.want)
			}
			interval, err := hideInterval(body)
			if err != nil || interval != tc.interval {
				t.Fatalf("interval=%d err=%v, want %d", interval, err, tc.interval)
			}
		})
	}

	if _, err := HideChance(LegacyMonster{Class: 13, Stats: [5]byte{10, 15}}); err == nil {
		t.Fatal("unknown class accepted")
	}
	if _, err := HideChance(LegacyMonster{Class: 4, Stats: [5]byte{10, 64}}); err == nil {
		t.Fatal("out-of-range dexterity accepted")
	}
}

func TestHideObjectChancePortsSourceFormula(t *testing.T) {
	cases := []struct {
		name  string
		class byte
		level byte
		dex   byte
		want  int
	}{
		{name: "ordinary", class: 4, level: 4, dex: 15, want: 11},
		{name: "thief", class: hideThiefClass, level: 4, dex: 15, want: 20},
		{name: "assassin", class: hideAssassinClass, level: 4, dex: 15, want: 20},
		{name: "ranger", class: hideRangerClass, level: 4, dex: 15, want: 17},
		{name: "ordinary-cap", class: 4, level: 100, dex: 63, want: 90},
		{name: "ranger-no-cap", class: hideRangerClass, level: 100, dex: 63, want: 251},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := HideObjectChance(LegacyMonster{Class: tc.class, Level: tc.level, Stats: [5]byte{10, tc.dex}})
			if err != nil || got != tc.want {
				t.Fatalf("chance=%d err=%v, want %d", got, err, tc.want)
			}
		})
	}
}

func TestPlanAndApplyHideObjectSuccessUsesOccurrenceAndTogglesHiddenFlag(t *testing.T) {
	before := hideObjectStateFixture(hideThiefClass, 4, 15)
	calls := 0
	proposal, err := before.PlanHideObjectWithOccurrence("alice", "검", 2, 100, func(low, high int) int {
		calls++
		if low != 1 || high != 100 {
			t.Fatalf("roll range=%d..%d", low, high)
		}
		return 1
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || !proposal.ObjectBranch || proposal.ObjectID != "sword-2" || proposal.ObjectName != "검" || proposal.ObjectOccurrence != 2 || !proposal.Succeeded || !proposal.Broadcast || proposal.Chance != 20 {
		t.Fatalf("proposal=%+v calls=%d", proposal, calls)
	}
	next, result, err := before.ApplyHide(proposal)
	if err != nil {
		t.Fatal(err)
	}
	object := next.Rooms[1].Items.Items["sword-2"]
	firstObject := next.Rooms[1].Items.Items["sword-1"]
	if !flag(object.Object.Flags[:], objectHiddenFlag) || flag(firstObject.Object.Flags[:], objectHiddenFlag) {
		t.Fatalf("unexpected object flags: %+v", next.Rooms[1].Items.Items)
	}
	if next.Players["alice"].Body.Timers[hideTimerIndex] != (LegacyTimer{LastTime: 100, Interval: 5}) {
		t.Fatalf("timer=%+v", next.Players["alice"].Body.Timers[hideTimerIndex])
	}
	if !result.ObjectBranch || result.ObjectID != "sword-2" || result.RoomText != "\nAlice님이 검 어딘가 숨깁니다.\r\n" {
		t.Fatalf("result=%+v", result)
	}
	event, ok, err := next.RoomHideObjectEvent("alice", "sword-2", result.Succeeded)
	if err != nil || !ok || !event.Succeeded || event.ObjectName != "검" || event.Text != result.RoomText {
		t.Fatalf("event=%+v ok=%v err=%v", event, ok, err)
	}
}

func TestHideObjectFailureClearsHiddenFlagAndStaleProposalIsAtomic(t *testing.T) {
	before := hideObjectStateFixture(4, 4, 15)
	room := before.Rooms[1]
	item := room.Items.Items["sword-1"]
	item.Object.Flags[objectHiddenFlag/8] |= 1 << (objectHiddenFlag % 8)
	room.Items.Items["sword-1"] = item
	before.Rooms[1] = room
	proposal, err := before.PlanHideObject("alice", "검", 100, func(int, int) int { return 100 })
	if err != nil {
		t.Fatal(err)
	}
	next, result, err := before.ApplyHide(proposal)
	if err != nil {
		t.Fatal(err)
	}
	failedObject := next.Rooms[1].Items.Items["sword-1"]
	if result.Succeeded || flag(failedObject.Object.Flags[:], objectHiddenFlag) || next.Players["alice"].Body.Timers[hideTimerIndex].LastTime != 100 {
		t.Fatalf("next=%+v result=%+v", next.Rooms[1].Items.Items["sword-1"], result)
	}

	changed := before.Rooms[1]
	changedItem := changed.Items.Items["sword-1"]
	changedItem.Object.Name = "다른검"
	changed.Items.Items["sword-1"] = changedItem
	changed.Items.Inventory = []string{"sword-1", "sword-2"}
	changedState := before.clone()
	changedState.Rooms[1] = changed
	if _, _, err := changedState.ApplyHide(proposal); err == nil {
		t.Fatal("stale object proposal accepted")
	}
}

func TestHideObjectRejectsONOTAKBeforeRandomAndCooldownSkipsTargetAndRandom(t *testing.T) {
	s := hideObjectStateFixture(4, 4, 15)
	room := s.Rooms[1]
	item := room.Items.Items["sword-1"]
	item.Object.Flags[objectNotTakeFlag/8] |= 1 << (objectNotTakeFlag % 8)
	room.Items.Items["sword-1"] = item
	s.Rooms[1] = room
	if _, err := s.PlanHideObject("alice", "검", 100, func(int, int) int {
		t.Fatal("ONOTAK consumed random")
		return 1
	}); !errors.Is(err, ErrHideObjectCannot) {
		t.Fatalf("ONOTAK err=%v", err)
	}

	s = hideObjectStateFixture(4, 4, 15)
	player := s.Players["alice"]
	player.Body.Timers[hideTimerIndex] = LegacyTimer{LastTime: 100, Interval: 15}
	s.Players["alice"] = player
	proposal, err := s.PlanHideObject("alice", "없는물건", 102, func(int, int) int {
		t.Fatal("cooldown consumed random")
		return 1
	})
	if err != nil || !proposal.Cooldown || proposal.WaitSeconds != 13 || proposal.ObjectBranch || proposal.Broadcast {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
}

func TestPlanAndApplyHideSuccessConsumesOneRollAndProjectsSuccess(t *testing.T) {
	before := hideStateFixture(4, 4, 15)
	calls := 0
	proposal, err := before.PlanHide("alice", 100, func(low, high int) int {
		calls++
		if low != 1 || high != 100 {
			t.Fatalf("roll range=%d..%d", low, high)
		}
		return 1
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || proposal.Cooldown || !proposal.Succeeded || !proposal.Broadcast || proposal.Chance != 10 || proposal.Interval != 15 {
		t.Fatalf("proposal=%+v calls=%d", proposal, calls)
	}
	beforeActor := before.Players["alice"].Body
	if flag(beforeActor.Flags[:], playerHiddenStateFlag) {
		t.Fatal("planning mutated source hidden state")
	}
	next, result, err := before.ApplyHide(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Broadcast || !result.Succeeded || result.Response != proposal.Response {
		t.Fatalf("result=%+v proposal=%+v", result, proposal)
	}
	actor := next.Players["alice"].Body
	if !flag(actor.Flags[:], playerHiddenStateFlag) || actor.Timers[hideTimerIndex] != (LegacyTimer{LastTime: 100, Interval: 15}) {
		t.Fatalf("applied actor=%+v", actor)
	}
	event, ok, err := next.RoomHideEvent("alice", result.Succeeded)
	if err != nil || !ok || !event.Succeeded || event.RoomID != 1 || event.ExcludeActorID != "alice" || event.Text != "\nAlice님이 그림자 사이로 숨었습니다.\r\n" {
		t.Fatalf("event=%+v ok=%v err=%v", event, ok, err)
	}
}

func TestPlanAndApplyHideFailureClearsHiddenAndProjectsFailure(t *testing.T) {
	before := hideStateFixture(hideThiefClass, 4, 15)
	p := before.Players["alice"]
	p.Body.Flags[playerHiddenStateFlag/8] |= 1 << (playerHiddenStateFlag % 8)
	before.Players["alice"] = p
	proposal, err := before.PlanHide("alice", 100, func(int, int) int { return 100 })
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Succeeded || !proposal.Broadcast || proposal.Response != "당신은 애써 숨어보려고 합니다." {
		t.Fatalf("proposal=%+v", proposal)
	}
	next, result, err := before.ApplyHide(proposal)
	if err != nil {
		t.Fatal(err)
	}
	nextActor := next.Players["alice"].Body
	if !result.Broadcast || result.Succeeded || flag(nextActor.Flags[:], playerHiddenStateFlag) {
		t.Fatalf("next=%+v result=%+v", next.Players["alice"], result)
	}
	event, ok, err := next.RoomHideEvent("alice")
	if err != nil || !ok || event.Succeeded || event.Text != "\nAlice님이 애써 숨어보려고 합니다.\r\n" {
		t.Fatalf("event=%+v ok=%v err=%v", event, ok, err)
	}
}

func TestHideCooldownIsPureNoOpAndDoesNotConsumeRandomness(t *testing.T) {
	s := hideStateFixture(hideRangerClass, 4, 15)
	p := s.Players["alice"]
	p.Body.Flags[playerHiddenStateFlag/8] |= 1 << (playerHiddenStateFlag % 8)
	p.Body.Timers[hideTimerIndex] = LegacyTimer{LastTime: 100, Interval: 5}
	s.Players["alice"] = p
	proposal, err := s.PlanHide("alice", 102, func(int, int) int {
		t.Fatal("cooldown consumed random")
		return 1
	})
	if err != nil || !proposal.Cooldown || proposal.Broadcast || proposal.Succeeded || proposal.WaitSeconds != 3 || proposal.Response != "3초동안 기다리세요.\r\n" {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	next, result, err := s.ApplyHide(proposal)
	if err != nil || result.Broadcast || result.Succeeded || result.Response != proposal.Response || !reflect.DeepEqual(next, s) {
		t.Fatalf("next=%+v result=%+v err=%v", next, result, err)
	}
}

func TestHideRejectsInvalidRandomSourceStaleProposalAndObjectTarget(t *testing.T) {
	s := hideStateFixture(4, 4, 15)
	if _, err := s.PlanHide("alice", 100, nil); err == nil {
		t.Fatal("missing random source accepted")
	}
	if _, err := s.PlanHide("alice", 100, func(int, int) int { return 0 }); err == nil {
		t.Fatal("out-of-range random value accepted")
	}
	if _, err := s.PlanHide("alice", 100, func(int, int) int { panic("rng broken") }); err == nil {
		t.Fatal("panicking random source accepted")
	}
	if _, err := s.PlanHideTarget("alice", "검", 100, func(int, int) int { t.Fatal("object path consumed random"); return 1 }); !errors.Is(err, ErrHideObjectUnsupported) {
		t.Fatalf("object target err=%v", err)
	}

	proposal, err := s.PlanHide("alice", 100, func(int, int) int { return 1 })
	if err != nil {
		t.Fatal(err)
	}
	mutated := s.Players["alice"]
	mutated.Body.Stats[1] = 20
	s.Players["alice"] = mutated
	if _, _, err := s.ApplyHide(proposal); err == nil {
		t.Fatal("stale chance proposal accepted")
	}
}

func TestHideRejectsInvalidEventOutcomeArityAndUnsafeActorName(t *testing.T) {
	s := hideStateFixture(4, 4, 15)
	if _, _, err := s.RoomHideEvent("alice", true, false); err == nil {
		t.Fatal("multiple event outcomes accepted")
	}
	p := s.Players["alice"]
	p.Body.Name = "Alice\nMallory"
	s.Players["alice"] = p
	if _, _, err := s.RoomHideEvent("alice"); err == nil {
		t.Fatal("unsafe actor name accepted")
	}
}

func TestHideTargetEmptyUsesBarePath(t *testing.T) {
	s := hideStateFixture(4, 4, 15)
	proposal, err := s.PlanHideTarget("alice", "   ", 100, func(int, int) int { return 1 })
	if err != nil || !proposal.Broadcast || proposal.Succeeded != true {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	if strings.Contains(proposal.Response, "object") {
		t.Fatalf("unexpected object response=%q", proposal.Response)
	}
}
