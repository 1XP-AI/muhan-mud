package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func stealCommandFixture(t *testing.T) []byte {
	t.Helper()
	s, err := world.DecodeState(attackCommandFixture())
	if err != nil {
		t.Fatal(err)
	}
	actor := s.Players["a"]
	actor.Body.Class = 8
	actor.Body.Level = 20
	actor.Body.Stats[1] = 10
	actor.Items = &world.ItemCollection{Items: map[string]world.Item{}}
	s.Players["a"] = actor
	npc := s.NPCs["wolf-id"]
	npc.Body.Name = "고블린"
	npc.Items = &world.ItemCollection{
		Items: map[string]world.Item{
			"pouch": {Object: world.LegacyObject{Name: "주머니", Type: 1}, Contents: []string{"gem"}},
			"gem":   {Object: world.LegacyObject{Name: "보석", Type: 1}},
		},
		Inventory: []string{"pouch"},
	}
	npc.Body.Inventory = nil
	npc.Enemies = []world.NPCEnemy{}
	s.NPCs["wolf-id"] = npc
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestParseStealLineUsesBoundedItemAndTargetForm(t *testing.T) {
	for _, tc := range []struct {
		line   string
		item   string
		target string
		ok     bool
	}{
		{line: "훔쳐 주머니 고블린", item: "주머니", target: "고블린", ok: true},
		{line: " 훔쳐 보석 경비 ", item: "보석", target: "경비", ok: true},
		{line: "훔쳐 주머니", ok: false},
		{line: "훔쳐 주머니 고블린 추가", ok: false},
		{line: "훔쳐\n주머니 고블린", ok: false},
		{line: "훔쳐 주머니 고블린\x00", ok: false},
		{line: "steal 주머니 고블린", ok: false},
		{line: "\xff", ok: false},
	} {
		got, ok := ParseStealLine(tc.line)
		if ok != tc.ok || (ok && (got.Item != tc.item || got.Target != tc.target)) {
			t.Fatalf("ParseStealLine(%q)=%+v,%v want %+v,%v", tc.line, got, ok, tc, tc.ok)
		}
	}
	if !IsStealLine("훔쳐 주머니 고블린") {
		t.Fatal("IsStealLine rejected canonical form")
	}
}

func TestExecuteStealLinePersistsCanonicalTransferAndReplaysWithoutReroll(t *testing.T) {
	store := &departureStore{state: stealCommandFixture(t)}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteStealLine(context.Background(), store, "w", "steal-1", lease, "훔쳐 주머니 고블린", 100, func(lo, hi int) int {
		if lo != 1 || hi != 100 {
			t.Fatalf("unexpected steal random request %d..%d", lo, hi)
		}
		return 1
	})
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.StealResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if !result.Succeeded || !result.Attempted || result.TargetID != "wolf-id" || result.ItemID != "pouch" || !strings.Contains(result.Response, "훔쳤습니다") {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.NPCs["wolf-id"].Items.Items) != 0 || len(saved.Players["a"].Items.Items) != 2 || saved.Players["a"].Body.Timers[world.StealTimerIndex].LastTime != 100 {
		t.Fatalf("saved actor=%+v npc=%+v", saved.Players["a"].Items, saved.NPCs["wolf-id"].Items)
	}

	replay, err := owners.ExecuteStealLine(context.Background(), store, "w", "steal-1", lease, "훔쳐 주머니 고블린", 100, func(int, int) int {
		t.Fatal("steal RNG replayed")
		return 0
	})
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteStealLineRejectsUnsupportedBeforeReceipt(t *testing.T) {
	store := &departureStore{state: stealCommandFixture(t)}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := owners.ExecuteStealLine(context.Background(), store, "w", "steal-bad", lease, "훔쳐 주머니", 100, func(int, int) int { return 1 }); err == nil || store.commits != 0 {
		t.Fatalf("unsupported steal reached receipt: err=%v commits=%d", err, store.commits)
	}
}
