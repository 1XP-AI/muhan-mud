package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func searchCommandFixture(t *testing.T) []byte {
	t.Helper()
	s, err := world.DecodeState(attackCommandFixture())
	if err != nil {
		t.Fatal(err)
	}
	actor := s.Players["a"]
	actor.Body.Stats[4] = 15
	s.Players["a"] = actor
	target := world.PlayerState{Body: world.LegacyMonster{Name: "Bob", RoomID: 1, Type: 0, Class: 4, Level: 1}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}}
	target.Body.Flags[1/8] |= 1 << (1 % 8)
	s.Players["b"] = target
	room := s.Rooms[1]
	room.PlayerIDs = append(room.PlayerIDs, "b")
	s.Rooms[1] = room
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestParseSearchLineAndCommandClassification(t *testing.T) {
	for _, line := range []string{"검색", " 찾아 "} {
		if _, ok := ParseSearchLine(line); !ok {
			t.Fatalf("search alias rejected: %q", line)
		}
		parsed, err := ParseCommand(line)
		if err != nil || parsed.Kind != CommandSearch || len(parsed.Tokens) != 1 {
			t.Fatalf("parsed=%+v err=%v", parsed, err)
		}
	}
	for _, line := range []string{"", "검색 Bob", "찾아\n", "검색\x00", "search"} {
		if _, ok := ParseSearchLine(line); ok {
			t.Fatalf("unsupported search accepted: %q", line)
		}
	}
}

func TestExecuteSearchLinePersistsResponseEnvelopeAndReplaysWithoutReroll(t *testing.T) {
	store := &departureStore{state: searchCommandFixture(t)}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteSearchLine(context.Background(), store, "w", "search-1", lease, "검색", 100, func(_, _ int) int { return 1 })
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.SearchResult
	if err := json.Unmarshal(first.Response, &result); err != nil || !result.Broadcast || len(result.Targets) != 1 || result.Targets[0].ID != "b" || !strings.Contains(result.Response, "Bob") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Body.Flags[0]&(1<<1) != 0 || saved.Players["a"].Body.Timers[7].LastTime != 100 {
		t.Fatalf("saved=%+v err=%v", saved.Players["a"], err)
	}
	replay, err := owners.ExecuteSearchLine(context.Background(), store, "w", "search-1", lease, "검색", 100, func(int, int) int { t.Fatal("search RNG replayed"); return 0 })
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteSearchLineRejectsUnsupportedBeforeReceipt(t *testing.T) {
	store := &departureStore{state: searchCommandFixture(t)}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	_ = owners.Admit(lease, func() error { return nil })
	if _, err := owners.ExecuteSearchLine(context.Background(), store, "w", "search-bad", lease, "검색 Bob", 100, func(int, int) int { return 1 }); err == nil || store.commits != 0 {
		t.Fatalf("unsupported search reached receipt: err=%v commits=%d", err, store.commits)
	}
}
