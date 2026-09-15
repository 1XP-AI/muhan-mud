package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func doorCommandFixture(t *testing.T) []byte {
	t.Helper()
	s, err := world.DecodeState(attackCommandFixture())
	if err != nil {
		t.Fatal(err)
	}
	room := s.Rooms[1]
	room.Resource.Exits = []world.LegacyExit{{Name: "북", Flags: [4]byte{40}, LastTime: 5}}
	s.Rooms[1] = room
	actor := s.Players["a"]
	actor.Body.Flags[1/8] |= 1 << (1 % 8)
	s.Players["a"] = actor
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestParseDoorLineAndCommandClassification(t *testing.T) {
	for _, line := range []string{"열어 북", "닫아 북", "열어"} {
		command, ok := ParseDoorLine(line)
		if !ok || command.Action == "" {
			t.Fatalf("ParseDoorLine(%q)=%+v ok=%v", line, command, ok)
		}
		parsed, err := ParseCommand(line)
		if err != nil || parsed.Kind != CommandDoor {
			t.Fatalf("ParseCommand(%q)=%+v err=%v", line, parsed, err)
		}
	}
	command, ok := ParseDoorLine("열어 북")
	if !ok || command.Target != "북" {
		t.Fatalf("command=%+v ok=%v", command, ok)
	}
	for _, line := range []string{"열어 북 남", "열어\n", "열어 북\x00", "풀어 북"} {
		if _, ok := ParseDoorLine(line); ok {
			t.Fatalf("unsupported door accepted: %q", line)
		}
	}
}

func TestExecuteDoorLinePersistsResponseAndReplays(t *testing.T) {
	store := &departureStore{state: doorCommandFixture(t)}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteDoorLine(context.Background(), store, "w", "door-1", lease, "열어 북", 10)
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.DoorCommandResult
	if err := json.Unmarshal(first.Response, &result); err != nil || !result.Broadcast || !strings.Contains(result.Response, "북쪽 출구를 열었습니다") || result.ExitName != "북" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	replay, err := owners.ExecuteDoorLine(context.Background(), store, "w", "door-1", lease, "열어 북", 10)
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Rooms[1].Resource.Exits[0].Flags[0]&8 != 0 || saved.Players["a"].Body.Flags[0]&2 != 0 {
		t.Fatalf("saved=%+v err=%v", saved, err)
	}
}

func TestExecuteDoorLineRejectsMalformedBeforeReceipt(t *testing.T) {
	store := &departureStore{state: doorCommandFixture(t)}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	_ = owners.Admit(lease, func() error { return nil })
	if _, err := owners.ExecuteDoorLine(context.Background(), store, "w", "door-bad", lease, "열어 북 남", 10); err == nil || store.commits != 0 {
		t.Fatalf("malformed door reached receipt: err=%v commits=%d", err, store.commits)
	}
}
