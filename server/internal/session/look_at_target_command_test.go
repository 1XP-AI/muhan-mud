package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func lookAtTargetCommandFixture(t *testing.T, hidden, silent bool) []byte {
	t.Helper()
	s, err := world.DecodeState(followCommandFixture())
	if err != nil {
		t.Fatal(err)
	}
	p := s.Players["a"]
	if hidden {
		p.Body.Flags[1/8] |= 1 << (1 % 8) // PHIDDN
	}
	if silent {
		p.Body.Flags[44/8] |= 1 << (44 % 8) // PSILNC
	}
	s.Players["a"] = p
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestParseLookAtTargetLineAcceptsOnlyExactExplicitTarget(t *testing.T) {
	command, ok := ParseLookAtTargetLine("  보아  Bob  ")
	if !ok || command.Target != "Bob" || command.Occurrence != 0 {
		t.Fatalf("command=%+v ok=%v", command, ok)
	}
	quoted, ok := ParseLookAtTargetLine(`보아 "Bob"`)
	if !ok || quoted.Target != "Bob" {
		t.Fatalf("quoted command=%+v ok=%v", quoted, ok)
	}
	occurrence, ok := ParseLookAtTargetLine("보아 Gob 2")
	if !ok || occurrence.Target != "Gob" || occurrence.Occurrence != 2 {
		t.Fatalf("occurrence command=%+v ok=%v", occurrence, ok)
	}
	for _, line := range []string{"", "보아", "봐 Bob", "보다 Bob", "조사 Bob", "보아 Bob extra", "보아 Bob\textra", "보아 \"Bob extra\"", "보아 Bob\n", "보아 Bob\x00", "보아 Bob 0", "보아 Bob -1", "보아 Bob +1", "보아 Bob nope", "보아 Bob 1 extra"} {
		if _, ok := ParseLookAtTargetLine(line); ok {
			t.Fatalf("unsupported look-at line accepted: %q", line)
		}
	}
}

func TestExecuteLookAtTargetLinePrefixOccurrenceUsesCanonicalCreatureOrderAndReplays(t *testing.T) {
	s, err := world.DecodeState(lookAtTargetCommandFixture(t, false, false))
	if err != nil {
		t.Fatal(err)
	}
	r := s.Rooms[1]
	r.NPCIDs = []string{"npc-key", "npc-name"}
	s.Rooms[1] = r
	s.NPCs = map[string]world.NPCState{
		"npc-key":  {Body: world.LegacyMonster{Name: "Guard", Keys: [3]string{"goblin"}, Type: 1, RoomID: 1}},
		"npc-name": {Body: world.LegacyMonster{Name: "Goblin", Type: 1, RoomID: 1}},
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: raw}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}

	first, err := owners.ExecuteLookAtTargetLine(context.Background(), store, "w", "look-at-occurrence", lease, "보아 gob 2")
	if err != nil || first.Replayed || store.commits != 1 || !strings.Contains(string(first.Response), "Goblin") {
		t.Fatalf("first=%s err=%v commits=%d", first.Response, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	event, ok, err := saved.RoomLookAtTargetEvent("a", "npc", "npc-name")
	if err != nil || !ok || !strings.Contains(event.Text, "Goblin") {
		t.Fatalf("event=%+v ok=%v err=%v", event, ok, err)
	}
	replay, err := owners.ExecuteLookAtTargetLine(context.Background(), store, "w", "look-at-occurrence", lease, "보아 gob 2")
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteLookAtTargetLineRejectsPrefixOccurrenceWithoutCanonicalCreatureReceipt(t *testing.T) {
	store := &departureStore{state: canonicalLookAtCommandFixture(t)}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := owners.ExecuteLookAtTargetLine(context.Background(), store, "w", "look-at-occurrence-bad", lease, "보아 검 1"); err == nil || store.commits != 0 {
		t.Fatalf("unsupported occurrence reached receipt: err=%v commits=%d", err, store.commits)
	}
}

func TestExecuteLookAtTargetLinePersistsPHIDDNRevealAndReplays(t *testing.T) {
	initial := lookAtTargetCommandFixture(t, true, false)
	store := &departureStore{state: initial}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}

	first, err := owners.ExecuteLookAtTargetLine(context.Background(), store, "w", "look-at-1", lease, "보아 Bob")
	if err != nil || first.Replayed || store.commits != 1 || string(first.Response) != `"당신은 Bob님을 봅니다.\r\n"` {
		t.Fatalf("first=%s err=%v commits=%d", first.Response, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	savedActor := saved.Players["a"]
	if err != nil || lookAtTargetFlag(savedActor.Body.Flags[:], 1) {
		t.Fatalf("saved=%+v err=%v", saved.Players["a"], err)
	}
	event, ok, err := saved.RoomLookAtTargetEvent("a", "player", "b")
	if err != nil || !ok || !strings.Contains(event.Text, "Alice님이 Bob님을 봅니다.") || event.TargetText != "\nAlice님이 당신을 봅니다.\r\n" {
		t.Fatalf("event=%+v ok=%v err=%v", event, ok, err)
	}

	replay, err := owners.ExecuteLookAtTargetLine(context.Background(), store, "w", "look-at-1", lease, "보아 Bob")
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteLookAtTargetLineSilenceRunsAfterPHIDDNOrdering(t *testing.T) {
	initial := lookAtTargetCommandFixture(t, true, true)
	store := &departureStore{state: initial}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteLookAtTargetLine(context.Background(), store, "w", "look-at-silent", lease, "보아 missing")
	if err != nil || store.commits != 1 || string(first.Response) != `"한마디도 할수 없습니다!\r\n"` {
		t.Fatalf("first=%s err=%v commits=%d", first.Response, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	savedActor := saved.Players["a"]
	if err != nil || lookAtTargetFlag(savedActor.Body.Flags[:], 1) || !lookAtTargetFlag(savedActor.Body.Flags[:], 44) {
		t.Fatalf("silence state=%+v err=%v", saved.Players["a"], err)
	}
}

func TestExecuteLookAtTargetLineFailsClosedWithoutReceiptForUnsupportedTarget(t *testing.T) {
	for _, line := range []string{"보아", "보아 Nobody", "보아 늑", "보아 Carol", "봐 Bob"} {
		store := &departureStore{state: lookAtTargetCommandFixture(t, false, false)}
		var owners Ownership
		lease, _ := owners.Acquire("a")
		if err := owners.Admit(lease, func() error { return nil }); err != nil {
			t.Fatal(err)
		}
		if _, err := owners.ExecuteLookAtTargetLine(context.Background(), store, "w", "look-at-bad", lease, line); err == nil || store.commits != 0 {
			t.Fatalf("line=%q err=%v commits=%d", line, err, store.commits)
		}
	}
}

func canonicalLookAtCommandFixture(t *testing.T) []byte {
	t.Helper()
	s, err := world.DecodeState(lookAtTargetCommandFixture(t, false, false))
	if err != nil {
		t.Fatal(err)
	}
	room := s.Rooms[1]
	room.Items = &world.ItemCollection{
		Items: map[string]world.Item{
			"floor-sword": {Object: world.LegacyObject{Name: "검", Description: "빛나는 검."}},
		},
		Inventory: []string{"floor-sword"},
	}
	room.Resource.Exits = []world.LegacyExit{{Name: "동", Destination: 2}}
	s.Rooms[1] = room
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestExecuteLookAtTargetLinePersistsCanonicalObjectAndExitAndReplays(t *testing.T) {
	for _, tc := range []struct {
		name, kind, id string
	}{
		{"검", "object", "floor-sword"},
		{"동", "exit", "exit:1:0"},
	} {
		store := &departureStore{state: canonicalLookAtCommandFixture(t)}
		var owners Ownership
		lease, err := owners.Acquire("a")
		if err != nil {
			t.Fatal(err)
		}
		if err := owners.Admit(lease, func() error { return nil }); err != nil {
			t.Fatal(err)
		}
		commandID := "look-at-" + tc.kind
		first, err := owners.ExecuteLookAtTargetLine(context.Background(), store, "w", commandID, lease, "보아 "+tc.name)
		if err != nil || first.Replayed || store.commits != 1 || !strings.Contains(string(first.Response), tc.name) {
			t.Fatalf("target=%q first=%s err=%v commits=%d", tc.name, first.Response, err, store.commits)
		}
		saved, err := world.DecodeState(store.state)
		if err != nil {
			t.Fatal(err)
		}
		event, ok, err := saved.RoomLookAtTargetEvent("a", tc.kind, tc.id)
		if err != nil || !ok || !strings.Contains(event.Text, tc.name) || event.TargetText != "" {
			t.Fatalf("target=%q event=%+v ok=%v err=%v", tc.name, event, ok, err)
		}
		replay, err := owners.ExecuteLookAtTargetLine(context.Background(), store, "w", commandID, lease, "보아 "+tc.name)
		if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
			t.Fatalf("target=%q replay=%+v err=%v commits=%d", tc.name, replay, err, store.commits)
		}
	}
}

func lookAtTargetFlag(flags []byte, bit uint) bool {
	return flags[bit/8]&(1<<(bit%8)) != 0
}
