package session

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func marriageSessionFixture(t *testing.T) []byte {
	t.Helper()
	room := world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "결혼식장"}}
	room.Flags[world.MarriageRoomFlag/8] |= 1 << (world.MarriageRoomFlag % 8)
	alice := world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 1}
	alice.Flags[world.MarriageMaleFlag/8] |= 1 << (world.MarriageMaleFlag % 8)
	alice.Timers[world.MarriageHoursTimerIndex].Interval = 7 * 86400
	bob := world.LegacyMonster{Name: "Bob", Type: 0, RoomID: 1}
	bob.Timers[world.MarriageHoursTimerIndex].Interval = 7 * 86400
	s := world.State{
		Version: 1,
		Rooms:   map[int16]world.RoomState{1: {Resource: room, PlayerIDs: []string{"alice", "bob"}}},
		Players: map[string]world.PlayerState{"alice": {Body: alice, Online: true}, "bob": {Body: bob, Online: true}},
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestParseMarriageLineAndCommandClassification(t *testing.T) {
	tests := []struct {
		line   string
		target string
	}{
		{line: "결혼", target: ""},
		{line: "  결혼  ", target: ""},
		{line: "결혼 Bob", target: "Bob"},
		{line: "Bob 결혼", target: "Bob"},
	}
	for _, tt := range tests {
		command, ok := ParseMarriageLine(tt.line)
		if !ok || command.Target != tt.target || !IsMarriageLine(tt.line) {
			t.Fatalf("line=%q command=%+v ok=%t", tt.line, command, ok)
		}
		parsed, err := ParseCommand(tt.line)
		if err != nil || parsed.Kind != CommandMarriage {
			t.Fatalf("line=%q parsed=%+v err=%v", tt.line, parsed, err)
		}
	}
	for _, line := range []string{"결혼 Bob extra", "Bob 결혼 extra", "결혼 결혼", "결혼\n", string([]byte{0xff})} {
		if _, ok := ParseMarriageLine(line); ok || IsMarriageLine(line) {
			t.Fatalf("unsupported marriage line accepted: %q", line)
		}
	}
	if parsed, err := ParseCommand("결혼 Bob extra"); err != nil || parsed.Kind != CommandMarriage {
		t.Fatalf("malformed marriage should reach bounded adapter: parsed=%+v err=%v", parsed, err)
	}
}

func TestExecuteMarriageLinePersistsAndReplaysReceipt(t *testing.T) {
	store := &departureStore{state: marriageSessionFixture(t)}
	var owners Ownership
	lease, err := owners.Acquire("alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteMarriageLine(context.Background(), store, "w", "marriage-1", lease, "결혼 Bob")
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.MarriageResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != world.MarriageRequest || result.TargetID != "bob" || !result.Changed || result.Response == "" || len(result.Events) != 1 {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if !world.PlayerFlagSet(saved.Players["alice"].Body, world.MarriagePendingFlag) {
		t.Fatalf("pending actor=%+v", saved.Players["alice"].Body)
	}
	replay, err := owners.ExecuteMarriageLine(context.Background(), store, "w", "marriage-1", lease, "결혼 Bob")
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteMarriageLineRejectsUnsupportedBeforeReceipt(t *testing.T) {
	store := &departureStore{state: marriageSessionFixture(t)}
	var owners Ownership
	lease, _ := owners.Acquire("alice")
	_ = owners.Admit(lease, func() error { return nil })
	if _, err := owners.ExecuteMarriageLine(context.Background(), store, "w", "marriage-bad", lease, "결혼 Bob extra"); !errors.Is(err, ErrUnsupportedMarriageLine) || store.commits != 0 {
		t.Fatalf("unsupported err=%v commits=%d", err, store.commits)
	}
}
