package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestExecuteHideLinePersistsFailedOutcomeAndReplaysWithoutReroll(t *testing.T) {
	store := &departureStore{state: hideCommandFixture(t)}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}

	first, err := owners.ExecuteHideLine(context.Background(), store, "w", "hide-fail-1", lease, "숨겨", 100, func(_, _ int) int { return 100 })
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.HideResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if !result.Broadcast || result.Succeeded || result.Response != "당신은 애써 숨어보려고 합니다." {
		t.Fatalf("failed result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	actor := saved.Players["a"].Body
	if actor.Timers[world.HideTimerIndex].LastTime != 100 || hideFlagSet(actor.Flags[:], 1) {
		t.Fatalf("saved failed hide actor=%+v", actor)
	}

	replay, err := owners.ExecuteHideLine(context.Background(), store, "w", "hide-fail-1", lease, "숨겨", 100, func(int, int) int { t.Fatal("hide RNG replayed"); return 1 })
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
	if !strings.Contains(string(replay.Response), "애써") {
		t.Fatalf("replay response=%q", replay.Response)
	}
}

func hideFlagSet(flags []byte, index uint) bool {
	return flags[index/8]&(1<<(index%8)) != 0
}
