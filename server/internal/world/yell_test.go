package world

import (
	"strings"
	"testing"
)

func TestPlanYellClearsHiddenAndProjectsCurrentAndAdjacentEvents(t *testing.T) {
	s := State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Exits: []LegacyExit{{Destination: 2}, {Destination: 3}}}}, PlayerIDs: []string{"a"}, Items: &ItemCollection{Items: map[string]Item{}}},
			2: {Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 2}}, Items: &ItemCollection{Items: map[string]Item{}}},
			3: {Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 3}}, Items: &ItemCollection{Items: map[string]Item{}}},
		},
		Players: map[string]PlayerState{"a": {Body: LegacyMonster{Name: "Alice", RoomID: 1}, Online: true, Items: &ItemCollection{Items: map[string]Item{}}}},
	}
	p := s.Players["a"]
	p.Body.Flags[playerHiddenStateFlag/8] |= 1 << (playerHiddenStateFlag % 8)
	s.Players["a"] = p
	next, result, err := s.PlanYell("a", "안녕하세요")
	if err != nil || !result.Broadcast || !strings.Contains(result.Response, "좋습니다") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	nextPlayer := next.Players["a"]
	if flag(nextPlayer.Body.Flags[:], playerHiddenStateFlag) {
		t.Fatal("successful yell did not reveal actor")
	}
	events, err := next.RoomYellEvents("a", "안녕하세요")
	if err != nil || len(events) != 3 {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	if events[0].RoomID != 1 || events[0].ExcludeActorID != "a" || !strings.Contains(events[0].Text, "Alice님") {
		t.Fatalf("current event=%+v", events[0])
	}
	if events[1].RoomID != 2 || events[1].ExcludeActorID != "" || !strings.Contains(events[1].Text, "누군가") {
		t.Fatalf("adjacent event=%+v", events[1])
	}
	if events[2].RoomID != 3 {
		t.Fatalf("second adjacent event=%+v", events[2])
	}
}

func TestPlanYellEmptySilentAndUnknownExitFailClosed(t *testing.T) {
	base := State{
		Version: 1,
		Rooms:   map[int16]RoomState{1: {Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}}, PlayerIDs: []string{"a"}, Items: &ItemCollection{Items: map[string]Item{}}}},
		Players: map[string]PlayerState{"a": {Body: LegacyMonster{Name: "Alice", RoomID: 1}, Online: true, Items: &ItemCollection{Items: map[string]Item{}}}},
	}
	_, empty, err := base.PlanYell("a", "")
	if err != nil || empty.Broadcast || !strings.Contains(empty.Response, "무슨말") {
		t.Fatalf("empty=%+v err=%v", empty, err)
	}
	silent := base.clone()
	p := silent.Players["a"]
	p.Body.Flags[playerSilentStateFlag/8] |= 1 << (playerSilentStateFlag % 8)
	silent.Players["a"] = p
	next, result, err := silent.PlanYell("a", "조용")
	nextPlayer := next.Players["a"]
	if err != nil || result.Broadcast || !strings.Contains(result.Response, "너무 약해서") || !flag(nextPlayer.Body.Flags[:], playerSilentStateFlag) {
		t.Fatalf("silent=%+v result=%+v err=%v", next, result, err)
	}
	broken := base
	r := broken.Rooms[1]
	r.Resource.Exits = []LegacyExit{{Destination: 99}}
	broken.Rooms[1] = r
	if _, err := broken.RoomYellEvents("a", "외침"); err == nil {
		t.Fatal("unknown yell destination accepted")
	}
}
