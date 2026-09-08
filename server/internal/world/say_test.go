package world

import (
	"reflect"
	"strings"
	"testing"
)

func TestPlanSayClearsHiddenStateAndBuildsRoomEvent(t *testing.T) {
	s := stateFixture()
	p := s.Players["a"]
	p.Body.Name = "Alice"
	p.Body.Flags[playerHiddenStateFlag/8] |= 1 << (playerHiddenStateFlag % 8)
	p.Body.Flags[playerLocalEchoFlag/8] |= 1 << (playerLocalEchoFlag % 8)
	s.Players["a"] = p
	next, result, err := s.PlanSay("a", "안녕하세요")
	nextPlayer := next.Players["a"]
	if err != nil || !result.Broadcast || !strings.Contains(result.Response, "안녕하세요") || flag(nextPlayer.Body.Flags[:], playerHiddenStateFlag) {
		t.Fatalf("next=%+v result=%+v err=%v", next.Players["a"], result, err)
	}
	event, ok, err := next.RoomSayEvent("a", "안녕하세요")
	if err != nil || !ok || event.RoomID != 1 || event.ActorName != "Alice" {
		t.Fatalf("event=%+v ok=%v err=%v", event, ok, err)
	}
	if _, ok, err := next.RoomSayEvent("a", ""); err != nil || ok {
		t.Fatalf("empty event ok=%v err=%v", ok, err)
	}
}

func TestPlanSaySilenceAndEmptyTextDoNotBroadcast(t *testing.T) {
	s := stateFixture()
	p := s.Players["a"]
	p.Body.Flags[playerSilentStateFlag/8] |= 1 << (playerSilentStateFlag % 8)
	s.Players["a"] = p
	next, result, err := s.PlanSay("a", "조용")
	if err != nil || result.Broadcast || !strings.Contains(result.Response, "들리지") || !reflect.DeepEqual(next, s) {
		t.Fatalf("silenced next=%+v result=%+v err=%v", next.Players["a"], result, err)
	}
	_, result, err = s.PlanSay("a", "   ")
	if err != nil || result.Broadcast || !strings.Contains(result.Response, "뭘 말하고") {
		t.Fatalf("empty result=%+v err=%v", result, err)
	}
}
