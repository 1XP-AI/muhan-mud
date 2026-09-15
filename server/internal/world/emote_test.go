package world

import (
	"reflect"
	"strings"
	"testing"
)

func emoteFixture() State {
	s := stateFixture()
	actor := s.Players["a"]
	actor.Body.Name = "Alice"
	actor.Body.Flags[playerHiddenStateFlag/8] |= 1 << (playerHiddenStateFlag % 8)
	s.Players["a"] = actor
	s.Players["b"] = PlayerState{
		Body:   LegacyMonster{Name: "Bob", RoomID: 1, Type: 0, HPMax: 100, HPCurrent: 100},
		Online: true,
	}
	room := s.Rooms[1]
	room.PlayerIDs = append(room.PlayerIDs, "b")
	s.Rooms[1] = room
	return s
}

func TestPlanEmoteAdmitsRequestedAliasesAsDeterministicRoomReceipts(t *testing.T) {
	for _, alias := range EmoteAliases() {
		s := emoteFixture()
		original := s
		next, result, err := s.PlanEmote("a", alias, "")
		if err != nil {
			t.Fatalf("alias=%q err=%v", alias, err)
		}
		if result.Response == "" || !result.Broadcast || result.Event == nil {
			t.Fatalf("alias=%q result=%+v", alias, result)
		}
		if result.Event.RoomID != 1 || result.Event.ActorID != "a" || result.Event.ExcludeActorID != "a" || result.Event.TargetID != "" || !strings.Contains(result.Event.Text, "Alice님") {
			t.Fatalf("alias=%q event=%+v", alias, result.Event)
		}
		nextPlayer := next.Players["a"]
		if flag(nextPlayer.Body.Flags[:], playerHiddenStateFlag) {
			t.Fatalf("alias=%q did not clear PHIDDN", alias)
		}
		originalPlayer := original.Players["a"]
		if !flag(originalPlayer.Body.Flags[:], playerHiddenStateFlag) {
			t.Fatalf("alias=%q mutated input", alias)
		}
	}
	if IsEmoteAlias("보아") {
		t.Fatal("target-inspection alias unexpectedly admitted")
	}
}

func TestPlanEmoteBuildsTargetAndRecipientProjections(t *testing.T) {
	s := emoteFixture()
	next, result, err := s.PlanEmote("a", "노려봐", "Bob")
	if err != nil {
		t.Fatal(err)
	}
	if result.TargetID != "b" || result.Event == nil || result.Event.TargetID != "b" || result.Event.TargetName != "Bob" {
		t.Fatalf("result=%+v", result)
	}
	if result.Event.ExcludeActorID != "a" || result.Event.ExcludeTargetID != "b" {
		t.Fatalf("event exclusions=%+v", result.Event)
	}
	if !strings.Contains(result.Response, "Bob님을") || !strings.Contains(result.Event.TargetText, "Alice님이") || !strings.Contains(result.Event.Text, "Alice님이 Bob님을") {
		t.Fatalf("response=%q target=%q room=%q", result.Response, result.Event.TargetText, result.Event.Text)
	}
	if next.Players["a"].Body.RoomID != 1 || next.Players["b"].Body.RoomID != 1 {
		t.Fatalf("emote moved a player: %+v", next.Players)
	}

	// src/io.c selects Josa[2] for OUTj2's 악수 branch. The rendered player
	// name ends in 님, so the deterministic source-compatible result is 과.
	_, handshake, err := s.PlanEmote("a", "악수", "Bob")
	if err != nil || handshake.Event == nil || !strings.Contains(handshake.Response, "Bob님과") || !strings.Contains(handshake.Event.Text, "Bob님과") {
		t.Fatalf("handshake=%+v err=%v", handshake, err)
	}

	committedEvent, ok, err := next.RoomEmoteEvent("a", "노려봐", "b")
	if err != nil || !ok || !reflect.DeepEqual(committedEvent, *result.Event) {
		t.Fatalf("committed event=%+v ok=%v err=%v want=%+v", committedEvent, ok, err, *result.Event)
	}
}

func TestPlanEmoteClearsHiddenBeforeSilenceAndSuppressesEvents(t *testing.T) {
	s := emoteFixture()
	p := s.Players["a"]
	p.Body.Flags[playerSilentStateFlag/8] |= 1 << (playerSilentStateFlag % 8)
	s.Players["a"] = p
	next, result, err := s.PlanEmote("a", "미소", "Bob")
	if err != nil || result.Broadcast || result.Event != nil || result.Response != "한마디도 할수 없습니다!\r\n" {
		t.Fatalf("next=%+v result=%+v err=%v", next.Players["a"], result, err)
	}
	nextPlayer := next.Players["a"]
	if flag(nextPlayer.Body.Flags[:], playerHiddenStateFlag) || !flag(nextPlayer.Body.Flags[:], playerSilentStateFlag) {
		t.Fatalf("hidden/silence ordering lost: %+v", next.Players["a"].Body.Flags)
	}
	if _, ok, err := next.RoomEmoteEvent("a", "미소", "b"); err != nil || ok {
		t.Fatalf("silent event ok=%v err=%v", ok, err)
	}
}

func TestPlanEmoteRejectsUnprovenTargetsWithoutStateChange(t *testing.T) {
	cases := []struct {
		alias, target string
	}{
		{alias: "감정표현", target: "Bob"},
		{alias: "생각", target: "Bob"},
		{alias: "부끄러", target: "Bob"},
		{alias: "미소", target: "Nobody"},
	}
	for _, tc := range cases {
		s := emoteFixture()
		got, _, err := s.PlanEmote("a", tc.alias, tc.target)
		if err == nil || !reflect.DeepEqual(got, State{}) {
			t.Fatalf("alias=%q target=%q got=%+v err=%v", tc.alias, tc.target, got, err)
		}
		inputPlayer := s.Players["a"]
		if !flag(inputPlayer.Body.Flags[:], playerHiddenStateFlag) {
			t.Fatalf("failed alias=%q changed input state", tc.alias)
		}
	}
	invisible := emoteFixture()
	target := invisible.Players["b"]
	target.Body.Flags[playerInvisibleFlag/8] |= 1 << (playerInvisibleFlag % 8)
	invisible.Players["b"] = target
	if got, _, err := invisible.PlanEmote("a", "미소", "Bob"); err == nil || !reflect.DeepEqual(got, State{}) {
		t.Fatalf("undetected invisible target accepted: got=%+v err=%v", got, err)
	}

	if _, _, err := emoteFixture().PlanEmote("a", "없는표현", ""); err == nil {
		t.Fatal("unsupported alias accepted")
	}
}
