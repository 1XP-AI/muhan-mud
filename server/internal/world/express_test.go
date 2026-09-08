package world

import (
	"reflect"
	"strings"
	"testing"
)

func expressWorldFixture() State {
	s := stateFixture()
	p := s.Players["a"]
	p.Body.Name = "Alice"
	p.Body.Flags[playerHiddenStateFlag/8] |= 1 << (playerHiddenStateFlag % 8)
	s.Players["a"] = p
	return s
}

func TestPlanExpressClearsHiddenAndDerivesFixedRoomEventAfterCommit(t *testing.T) {
	s := expressWorldFixture()
	original := s
	next, result, err := s.PlanExpress("a", "hello %s")
	if err != nil {
		t.Fatal(err)
	}
	if result.Response != "예. 좋습니다.\r\n" || !result.Broadcast {
		t.Fatalf("result=%+v", result)
	}
	if !reflect.DeepEqual(s, original) {
		t.Fatal("planning mutated the input snapshot")
	}
	if next.Players["a"].Body.Flags[playerHiddenStateFlag/8]&(1<<(playerHiddenStateFlag%8)) != 0 {
		t.Fatal("successful 표현 did not clear PHIDDN")
	}

	event, ok, err := next.RoomExpressEvent("a", "hello %s")
	if err != nil || !ok {
		t.Fatalf("event=%+v ok=%v err=%v", event, ok, err)
	}
	if event.RoomID != 1 || event.ActorID != "a" || event.ExcludeActorID != "a" || event.ActorName != "Alice" || event.Text != "\n:Alice님이 hello %s.\r\n" {
		t.Fatalf("event=%+v", event)
	}
	// The result has no event field and therefore cannot accidentally become a
	// persisted arbitrary-event payload in the command receipt.
}

func TestPlanExpressEmptyAndSilentArePureNoOps(t *testing.T) {
	for _, text := range []string{"", "   "} {
		s := expressWorldFixture()
		next, result, err := s.PlanExpress("a", text)
		if err != nil || !reflect.DeepEqual(next, s) || result.Response != "무슨말을 표현하시려구요?\r\n" || result.Broadcast {
			t.Fatalf("empty text=%q next=%+v result=%+v err=%v", text, next, result, err)
		}
		if _, ok, err := next.RoomExpressEvent("a", text); err != nil || ok {
			t.Fatalf("empty event text=%q ok=%v err=%v", text, ok, err)
		}
	}

	s := expressWorldFixture()
	p := s.Players["a"]
	p.Body.Flags[playerSilentStateFlag/8] |= 1 << (playerSilentStateFlag % 8)
	s.Players["a"] = p
	next, result, err := s.PlanExpress("a", "hello")
	if err != nil || !reflect.DeepEqual(next, s) || result.Response != "당신은 지금당장 그것을 할 수 없습니다.\r\n" || result.Broadcast {
		t.Fatalf("silent next=%+v result=%+v err=%v", next, result, err)
	}
	if next.Players["a"].Body.Flags[playerHiddenStateFlag/8]&(1<<(playerHiddenStateFlag%8)) == 0 || next.Players["a"].Body.Flags[playerSilentStateFlag/8]&(1<<(playerSilentStateFlag%8)) == 0 {
		t.Fatal("silent 표현 changed hidden/silent state")
	}
	if _, ok, err := next.RoomExpressEvent("a", "hello"); err != nil || ok {
		t.Fatalf("silent event ok=%v err=%v", ok, err)
	}
}

func TestPlanExpressSupportsPLECHOWithoutClientFormatting(t *testing.T) {
	s := expressWorldFixture()
	p := s.Players["a"]
	p.Body.Flags[playerLocalEchoFlag/8] |= 1 << (playerLocalEchoFlag % 8)
	s.Players["a"] = p
	next, result, err := s.PlanExpress("a", "hello %n")
	if err != nil || result.Response != ":Alice님이 hello %n.\r\n" || !result.Broadcast {
		t.Fatalf("next=%+v result=%+v err=%v", next, result, err)
	}
	if next.Players["a"].Body.Flags[playerHiddenStateFlag/8]&(1<<(playerHiddenStateFlag%8)) != 0 {
		t.Fatal("PLECHO expression did not clear PHIDDN")
	}
	if event, ok, err := next.RoomExpressEvent("a", "hello %n"); err != nil || !ok || event.Text != "\n:Alice님이 hello %n.\r\n" {
		t.Fatalf("event=%+v ok=%v err=%v", event, ok, err)
	}
}

func TestPlanExpressRejectsInvalidOrOversizedPayloadWithoutPartialState(t *testing.T) {
	for _, text := range []string{
		string([]byte{0xff}),
		strings.Repeat("a", MaxExpressTextBytes+1),
		"line\nfeed",
		"escape\x1b[31m",
	} {
		s := expressWorldFixture()
		got, _, err := s.PlanExpress("a", text)
		if err == nil || !reflect.DeepEqual(got, State{}) {
			t.Fatalf("payload=%q got=%+v err=%v", text, got, err)
		}
		if s.Players["a"].Body.Flags[playerHiddenStateFlag/8]&(1<<(playerHiddenStateFlag%8)) == 0 {
			t.Fatal("rejected expression mutated the input snapshot")
		}
		if _, ok, eventErr := s.RoomExpressEvent("a", text); eventErr == nil || ok {
			t.Fatalf("rejected event payload=%q ok=%v err=%v", text, ok, eventErr)
		}
	}
}

func TestValidateExpressTextUsesUTF8ByteBound(t *testing.T) {
	if err := ValidateExpressText(strings.Repeat("가", MaxExpressTextBytes/3)); err != nil {
		t.Fatal(err)
	}
	if err := ValidateExpressText(strings.Repeat("a", MaxExpressTextBytes)); err != nil {
		t.Fatal(err)
	}
	if err := ValidateExpressText(strings.Repeat("a", MaxExpressTextBytes+1)); err == nil {
		t.Fatal("oversized expression accepted")
	}
}
