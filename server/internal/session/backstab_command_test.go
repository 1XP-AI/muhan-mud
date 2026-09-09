package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestParseBackstabLineUsesBoundedTargetForm(t *testing.T) {
	for _, tc := range []struct {
		line   string
		target string
		ok     bool
	}{
		{line: "기습 늑대", target: "늑대", ok: true},
		{line: " 기습 고블린 ", target: "고블린", ok: true},
		{line: "기습", ok: false},
		{line: "기습 늑대 추가", ok: false},
		{line: "기습\n늑대", ok: false},
		{line: "기습 늑대\x00", ok: false},
		{line: "backstab 늑대", ok: false},
		{line: "\xff", ok: false},
	} {
		got, ok := ParseBackstabLine(tc.line)
		if ok != tc.ok || (ok && got.Target != tc.target) {
			t.Fatalf("ParseBackstabLine(%q)=%+v,%v want target=%q,%v", tc.line, got, ok, tc.target, tc.ok)
		}
	}
	if !IsBackstabLine("기습 늑대") {
		t.Fatal("IsBackstabLine rejected canonical form")
	}
}

func backstabCommandFixture(t *testing.T) []byte {
	t.Helper()
	s, err := world.DecodeState(attackCommandFixture())
	if err != nil {
		t.Fatal(err)
	}
	actor := s.Players["a"]
	actor.Body.Class = 8
	actor.Body.Flags[0] |= 1 << 1 // PHIDDN
	actor.Items = &world.ItemCollection{
		Items: map[string]world.Item{
			"knife": {Object: world.LegacyObject{
				Name:         "단검",
				Type:         0,
				ShotsMax:     5,
				ShotsCurrent: 5,
				DiceCount:    1,
				DiceSides:    4,
			}},
		},
		Ready: [20]string{19: "knife"},
	}
	s.Players["a"] = actor
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestExecuteBackstabLineCommitsCanonicalNPCDamageAndReplays(t *testing.T) {
	store := &departureStore{state: backstabCommandFixture(t)}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteBackstabLine(context.Background(), store, "w", "backstab-1", lease, "기습 늑대", 100, func(lo, hi int) int {
		switch {
		case lo == 1 && hi == 20:
			return hi
		case lo == 1 && hi == 4:
			return hi
		case lo == 20 && hi == 35:
			return 29
		default:
			t.Fatalf("unexpected backstab random request %d..%d", lo, hi)
			return 0
		}
	})
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.BackstabResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if !result.Succeeded || !result.Hit || result.Damage != 8 || result.TargetID != "wolf-id" || !strings.Contains(result.Response, "8 만큼의 피해") {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.NPCs["wolf-id"].Body.HPCurrent != 42 || saved.NPCs["wolf-id"].Enemies[0].Damage != 8 || saved.Players["a"].Body.Timers[world.BackstabTimerIndex].LastTime != 100 {
		t.Fatalf("saved actor=%+v npc=%+v", saved.Players["a"].Body, saved.NPCs["wolf-id"])
	}

	replay, err := owners.ExecuteBackstabLine(context.Background(), store, "w", "backstab-1", lease, "기습 늑대", 100, func(int, int) int {
		t.Fatal("backstab RNG replayed")
		return 0
	})
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteBackstabLineRejectsUnsupportedBeforeReceipt(t *testing.T) {
	store := &departureStore{state: backstabCommandFixture(t)}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := owners.ExecuteBackstabLine(context.Background(), store, "w", "backstab-bad", lease, "봐", 100, func(int, int) int { return 1 }); err == nil || store.commits != 0 {
		t.Fatalf("unsupported backstab reached receipt: err=%v commits=%d", err, store.commits)
	}
}
