package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func peekCommandFixture(t *testing.T) []byte {
	t.Helper()
	s, err := world.DecodeState(attackCommandFixture())
	if err != nil {
		t.Fatal(err)
	}
	actor := s.Players["a"]
	actor.Body.Class = 8
	actor.Body.Level = 4
	s.Players["a"] = actor
	target := world.PlayerState{Body: world.LegacyMonster{Name: "Bob", RoomID: 1, Type: 0, Class: 4, Level: 4}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{
		"sword": {Object: world.LegacyObject{Name: "검"}},
	}, Inventory: []string{"sword"}}}
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

func TestParsePeekLineAndCommandClassification(t *testing.T) {
	parsed, err := ParseCommand("엿봐 Bob")
	if err != nil || parsed.Kind != CommandPeek || len(parsed.Tokens) != 2 {
		t.Fatalf("parsed=%+v err=%v", parsed, err)
	}
	if command, ok := ParsePeekLine(" 엿봐 Bob "); !ok || command.Target != "Bob" {
		t.Fatalf("command=%+v ok=%v", command, ok)
	}
	for _, line := range []string{"엿봐", "엿봐 Bob Carol", "엿봐\n", "엿봐 Bob\x00", "peek Bob"} {
		if _, ok := ParsePeekLine(line); ok {
			t.Fatalf("unsupported peek accepted: %q", line)
		}
	}
}

func TestExecutePeekLinePersistsResponseAndReplaysWithoutReroll(t *testing.T) {
	store := &departureStore{state: peekCommandFixture(t)}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	rolls := []int{1, 100}
	roll := func(int, int) int {
		value := rolls[0]
		rolls = rolls[1:]
		return value
	}
	first, err := owners.ExecutePeekLine(context.Background(), store, "w", "peek-1", lease, "엿봐 Bob", 100, roll)
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.PeekResult
	if err := json.Unmarshal(first.Response, &result); err != nil || !result.Succeeded || !result.Alert || !strings.Contains(result.Response, "검") || result.TargetID != "b" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	replay, err := owners.ExecutePeekLine(context.Background(), store, "w", "peek-1", lease, "엿봐 Bob", 100, func(int, int) int { t.Fatal("peek RNG replayed"); return 0 })
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecutePeekLineRejectsUnsupportedBeforeReceipt(t *testing.T) {
	store := &departureStore{state: peekCommandFixture(t)}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	_ = owners.Admit(lease, func() error { return nil })
	if _, err := owners.ExecutePeekLine(context.Background(), store, "w", "peek-bad", lease, "엿봐 Bob Carol", 100, func(int, int) int { return 1 }); err == nil || store.commits != 0 {
		t.Fatalf("unsupported peek reached receipt: err=%v commits=%d", err, store.commits)
	}
}
