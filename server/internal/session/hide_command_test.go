package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func hideCommandFixture(t *testing.T) []byte {
	t.Helper()
	s, err := world.DecodeState(attackCommandFixture())
	if err != nil {
		t.Fatal(err)
	}
	actor := s.Players["a"]
	actor.Body.Class = 4
	actor.Body.Level = 4
	actor.Body.Stats[1] = 15
	s.Players["a"] = actor
	r := s.Rooms[1]
	s.Rooms[1] = r
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestParseHideLineAndCommandClassification(t *testing.T) {
	for _, line := range []string{"숨겨", " 숨어 "} {
		if _, ok := ParseHideLine(line); !ok {
			t.Fatalf("hide alias rejected: %q", line)
		}
		parsed, err := ParseCommand(line)
		if err != nil || parsed.Kind != CommandHide || len(parsed.Tokens) != 1 {
			t.Fatalf("parsed=%+v err=%v", parsed, err)
		}
	}
	for _, line := range []string{"숨겨 검", "숨겨\n", "숨겨\x00", "hide"} {
		if _, ok := ParseHideLine(line); ok {
			t.Fatalf("unsupported hide accepted: %q", line)
		}
	}
}

func TestExecuteHideLinePersistsResponseAndReplaysWithoutReroll(t *testing.T) {
	store := &departureStore{state: hideCommandFixture(t)}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteHideLine(context.Background(), store, "w", "hide-1", lease, "숨겨", 100, func(_, _ int) int { return 1 })
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.HideResult
	if err := json.Unmarshal(first.Response, &result); err != nil || !result.Broadcast || !result.Succeeded || !strings.Contains(result.Response, "성공적으로") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Body.Timers[14].LastTime != 100 || saved.Players["a"].Body.Flags[0]&(1<<1) == 0 {
		t.Fatalf("saved=%+v err=%v", saved.Players["a"], err)
	}
	replay, err := owners.ExecuteHideLine(context.Background(), store, "w", "hide-1", lease, "숨겨", 100, func(int, int) int { t.Fatal("hide RNG replayed"); return 0 })
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteHideLineRejectsUnsupportedBeforeReceipt(t *testing.T) {
	store := &departureStore{state: hideCommandFixture(t)}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	_ = owners.Admit(lease, func() error { return nil })
	if _, err := owners.ExecuteHideLine(context.Background(), store, "w", "hide-bad", lease, "숨겨 검", 100, func(int, int) int { return 1 }); err == nil || store.commits != 0 {
		t.Fatalf("unsupported hide reached receipt: err=%v commits=%d", err, store.commits)
	}
}
