package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func npcTalkCommandFixture(t *testing.T) []byte {
	t.Helper()
	s, err := world.DecodeState(followCommandFixture())
	if err != nil {
		t.Fatal(err)
	}
	actor := s.Players["a"]
	actor.Body.Flags[1/8] |= 1 << (1 % 8) // PHIDDN
	s.Players["a"] = actor
	var aggressive [8]byte
	aggressive[26/8] |= 1 << (26 % 8) // MTLKAG
	room := s.Rooms[1]
	room.NPCIDs = []string{"guide-one", "guide-two"}
	s.Rooms[1] = room
	s.NPCs = map[string]world.NPCState{
		"guide-one": {Body: world.LegacyMonster{Name: "Guide", Type: 1, RoomID: 1, Talk: "첫 번째 안내"}, Enemies: []world.NPCEnemy{}},
		"guide-two": {Body: world.LegacyMonster{Name: "Guide", Type: 1, RoomID: 1, Talk: "두 번째 안내", Flags: aggressive}, Enemies: []world.NPCEnemy{}},
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func npcTalkSessionCatalog(t *testing.T, body string) world.TalkCatalog {
	t.Helper()
	catalog, err := world.LoadTalkCatalog(fstest.MapFS{
		"Guide-0": &fstest.MapFile{Data: []byte(body)},
	})
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func TestParseNPCTalkLineAdmitsTargetTopicAndPositiveOccurrence(t *testing.T) {
	tests := []struct {
		line, name, topic string
		occurrence        int
		ok                bool
	}{
		{line: "대화 Guide", name: "Guide", occurrence: 1, ok: true},
		{line: "대화 Guide quest", name: "Guide", topic: "quest", occurrence: 1, ok: true},
		{line: "대화 Guide 2", name: "Guide", occurrence: 2, ok: true},
		{line: "대화 Guide 2 quest", name: "Guide", topic: "quest", occurrence: 2, ok: true},
		{line: `대화 "Guide" "quest key"`, name: "Guide", topic: "quest key", occurrence: 1, ok: true},
	}
	for _, tc := range tests {
		got, ok := ParseNPCTalkLine(tc.line)
		if ok != tc.ok || got.NPCName != tc.name || got.Topic != tc.topic || got.NPCOccurrence != tc.occurrence {
			t.Fatalf("line=%q got=%+v ok=%v want name=%q topic=%q occurrence=%d ok=%v", tc.line, got, ok, tc.name, tc.topic, tc.occurrence, tc.ok)
		}
	}
	for _, line := range []string{"", "대화", "대화 Guide 0", "대화 Guide -1", "대화 Guide +1", "대화 Guide nope 2", "대화 Guide\nquest", "얘기 Guide", "대화 Guide one two"} {
		if _, ok := ParseNPCTalkLine(line); ok {
			t.Fatalf("unsupported NPC talk line accepted: %q", line)
		}
	}
}

func TestExecuteNPCTalkLinePersistsAtomicProjectionAndReplays(t *testing.T) {
	store := &departureStore{state: npcTalkCommandFixture(t)}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteNPCTalkLine(context.Background(), store, "w", "npc-talk-1", lease, "대화 Guide 2")
	if err != nil || first.Replayed || first.Revision != 1 || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.NPCTalkResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.TargetID != "guide-two" || !result.EnemyAdded || result.Event == nil || !strings.Contains(result.Response, "두 번째 안내") || !strings.Contains(result.Event.RoomText, "Alice") {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["a"].Body.Flags[0]&(1<<1) != 0 || len(saved.NPCs["guide-two"].Enemies) != 1 {
		t.Fatalf("saved state=%+v", saved)
	}
	replay, err := owners.ExecuteNPCTalkLine(context.Background(), store, "w", "npc-talk-1", lease, "대화 Guide 2")
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteNPCTalkLineFailsClosedForTopicWithoutCanonicalLoader(t *testing.T) {
	raw := npcTalkCommandFixture(t)
	s, err := world.DecodeState(raw)
	if err != nil {
		t.Fatal(err)
	}
	npc := s.NPCs["guide-one"]
	npc.Body.Flags[23/8] |= 1 << (23 % 8) // MTALKS
	s.NPCs["guide-one"] = npc
	raw, err = json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: raw}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := owners.ExecuteNPCTalkLine(context.Background(), store, "w", "npc-talk-topic", lease, "대화 Guide quest"); !errors.Is(err, world.ErrNPCTalkTopicsUnavailable) || store.commits != 0 {
		t.Fatalf("topic unexpectedly committed: err=%v commits=%d", err, store.commits)
	}
}

func TestExecuteNPCTalkLineWithOptionsUsesInjectedCatalogAndReplays(t *testing.T) {
	raw := npcTalkCommandFixture(t)
	s, err := world.DecodeState(raw)
	if err != nil {
		t.Fatal(err)
	}
	npc := s.NPCs["guide-one"]
	npc.Body.Flags[23/8] |= 1 << (23 % 8) // MTALKS
	s.NPCs["guide-one"] = npc
	raw, err = json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: raw}
	catalog := npcTalkSessionCatalog(t, "quest\ncanonical answer\n")
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteNPCTalkLineWithOptions(context.Background(), store, "w", "npc-talk-catalog", lease, "대화 Guide quest", NPCTalkOptions{Catalog: &catalog})
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.NPCTalkResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Topic != "quest" || result.Event == nil || !strings.Contains(result.Response, "canonical answer") {
		t.Fatalf("catalog result=%+v", result)
	}
	replay, err := owners.ExecuteNPCTalkLineWithOptions(context.Background(), store, "w", "npc-talk-catalog", lease, "대화 Guide quest", NPCTalkOptions{Catalog: &catalog})
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteNPCTalkLineRejectsUnsupportedTopicActionAtomically(t *testing.T) {
	raw := npcTalkCommandFixture(t)
	s, err := world.DecodeState(raw)
	if err != nil {
		t.Fatal(err)
	}
	npc := s.NPCs["guide-one"]
	npc.Body.Flags[23/8] |= 1 << (23 % 8) // MTALKS
	s.NPCs["guide-one"] = npc
	raw, err = json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: raw}
	catalog := npcTalkSessionCatalog(t, "quest ACTION smile PLAYER\ncanonical answer\n")
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := owners.ExecuteNPCTalkLineWithOptions(context.Background(), store, "w", "npc-talk-action", lease, "대화 Guide quest", NPCTalkOptions{Catalog: &catalog}); !errors.Is(err, world.ErrNPCTalkActionUnavailable) {
		t.Fatalf("unsupported action err=%v", err)
	}
	if store.commits != 0 || string(store.state) != string(raw) {
		t.Fatalf("unsupported action mutated durable state: commits=%d", store.commits)
	}
}
