package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestParseCircleLineUsesBoundedTargetForm(t *testing.T) {
	for _, tc := range []struct {
		line   string
		target string
		ok     bool
	}{
		{line: "교란 늑대", target: "늑대", ok: true},
		{line: " 교란 고블린 ", target: "고블린", ok: true},
		{line: "교란", ok: false},
		{line: "교란 늑대 추가", ok: false},
		{line: "교란\n늑대", ok: false},
		{line: "교란 늑대\x00", ok: false},
		{line: "circle 늑대", ok: false},
		{line: "\xff", ok: false},
	} {
		got, ok := ParseCircleLine(tc.line)
		if ok != tc.ok || (ok && got.Target != tc.target) {
			t.Fatalf("ParseCircleLine(%q)=%+v,%v want target=%q,%v", tc.line, got, ok, tc.target, tc.ok)
		}
	}
	if !IsCircleLine("교란 늑대") {
		t.Fatal("IsCircleLine rejected canonical form")
	}
}

func TestExecuteCircleLineCommitsAndReplaysWithoutReroll(t *testing.T) {
	store := &departureStore{state: attackCommandFixture()}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}

	first, err := owners.ExecuteCircleLine(context.Background(), store, "w", "circle-1", lease, "교란 늑대", 100, func(low, high int) int {
		switch {
		case low == 1 && high == 100:
			return 1
		case low == 6 && high == 12:
			return 7
		default:
			t.Fatalf("unexpected circle random request %d..%d", low, high)
			return 0
		}
	})
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.CircleResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if !result.Succeeded || result.Delay != 7 || result.TargetID != "wolf-id" || !strings.Contains(result.Response, "교란시킵니다") {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.NPCs["wolf-id"].Body.Timers[world.CircleBefuddleTimerIndex].Interval != 7 || len(saved.NPCs["wolf-id"].Enemies) != 1 || saved.Players["a"].Body.Timers[world.CircleTimerIndex].LastTime != 100 {
		t.Fatalf("saved actor=%+v npc=%+v", saved.Players["a"].Body, saved.NPCs["wolf-id"])
	}

	replay, err := owners.ExecuteCircleLine(context.Background(), store, "w", "circle-1", lease, "교란 늑대", 100, func(int, int) int {
		t.Fatal("circle RNG replayed")
		return 0
	})
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteCircleLineRejectsUnsupportedBeforeReceipt(t *testing.T) {
	store := &departureStore{state: attackCommandFixture()}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := owners.ExecuteCircleLine(context.Background(), store, "w", "circle-bad", lease, "봐", 100, func(int, int) int { return 1 }); err != ErrUnsupportedCircleLine || store.commits != 0 {
		t.Fatalf("unsupported circle reached receipt: err=%v commits=%d", err, store.commits)
	}
}
