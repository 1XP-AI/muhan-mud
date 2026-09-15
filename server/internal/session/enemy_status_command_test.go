package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func enemyStatusCommandFixture(t *testing.T) []byte {
	t.Helper()
	s, err := world.DecodeState(attackCommandFixture())
	if err != nil {
		t.Fatal(err)
	}
	npc := s.NPCs["wolf-id"]
	npc.Body.Name = "늑대"
	npc.Body.HPMax = 100
	npc.Body.HPCurrent = 50
	s.NPCs["wolf-id"] = npc
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestParseEnemyStatusLineAdmitsOnlyExactStatusTargetShape(t *testing.T) {
	command, ok := ParseEnemyStatusLine("  상태  늑대  ")
	if !ok || command.Target != "늑대" {
		t.Fatalf("command=%+v ok=%v", command, ok)
	}
	quoted, ok := ParseEnemyStatusLine(`상태 "늑대"`)
	if !ok || quoted.Target != "늑대" {
		t.Fatalf("quoted=%+v ok=%v", quoted, ok)
	}
	for _, line := range []string{
		"", "상태", "건강 늑대", "적상태 늑대", "상태 늑대 2",
		"상태 늑대 여분", "상태 늑대\n", "상태 늑대\x00", "상태 \"늑대", `상태 "늑 대"`,
	} {
		if _, ok := ParseEnemyStatusLine(line); ok {
			t.Fatalf("unsupported enemy status line accepted: %q", line)
		}
	}
}

func TestExecuteEnemyStatusLinePersistsResponseAndReplays(t *testing.T) {
	initial := enemyStatusCommandFixture(t)
	store := &departureStore{state: initial}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteEnemyStatusLine(context.Background(), store, "w", "enemy-status-1", lease, "상태 늑대")
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var text string
	if err := json.Unmarshal(first.Response, &text); err != nil || text != "늑대 : [========       ]\r\n" {
		t.Fatalf("response=%q err=%v", text, err)
	}
	if string(store.state) != string(initial) {
		t.Fatal("read-only enemy status changed world state")
	}
	replay, err := owners.ExecuteEnemyStatusLine(context.Background(), store, "w", "enemy-status-1", lease, "상태 늑대")
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteEnemyStatusLineBlindGateIsDurableNoOp(t *testing.T) {
	s, err := world.DecodeState(enemyStatusCommandFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	actor := s.Players["a"]
	actor.Body.Flags[42/8] |= 1 << (42 % 8)
	s.Players["a"] = actor
	initial, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: initial}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteEnemyStatusLine(context.Background(), store, "w", "enemy-status-blind", lease, "상태 없는괴물")
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var text string
	if err := json.Unmarshal(first.Response, &text); err != nil || text != world.EnemyStatusBlindResponse {
		t.Fatalf("blind response=%q err=%v", text, err)
	}
	if string(store.state) != string(initial) {
		t.Fatal("blind read-only command changed world state")
	}
}

func TestExecuteEnemyStatusLineRejectsUnsupportedAndUnresolvedTargetsBeforeReceipt(t *testing.T) {
	for _, line := range []string{"상태", "상태 늑", "상태 원격", "상태 없는괴물"} {
		store := &departureStore{state: enemyStatusCommandFixture(t)}
		var owners Ownership
		lease, _ := owners.Acquire("a")
		if err := owners.Admit(lease, func() error { return nil }); err != nil {
			t.Fatal(err)
		}
		_, err := owners.ExecuteEnemyStatusLine(context.Background(), store, "w", "enemy-status-bad", lease, line)
		if err == nil || store.commits != 0 {
			t.Fatalf("line=%q err=%v commits=%d", line, err, store.commits)
		}
	}
}

func TestExecuteEnemyStatusLineFailsClosedWhenCanonicalNPCStateIsUnavailable(t *testing.T) {
	s, err := world.DecodeState(enemyStatusCommandFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	s.NPCs = nil
	r := s.Rooms[1]
	r.NPCIDs = nil
	s.Rooms[1] = r
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: raw}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	_, err = owners.ExecuteEnemyStatusLine(context.Background(), store, "w", "enemy-status-canonical", lease, "상태 늑대")
	if err == nil || !strings.Contains(err.Error(), "canonical NPC") || store.commits != 0 {
		t.Fatalf("canonical boundary err=%v commits=%d", err, store.commits)
	}
}
