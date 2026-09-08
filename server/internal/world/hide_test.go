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
