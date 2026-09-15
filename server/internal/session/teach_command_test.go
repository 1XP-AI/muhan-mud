package session

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func teachCommandFixture(t *testing.T) []byte {
	t.Helper()
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}},
			PlayerIDs: []string{"alice", "bob"},
		}},
		Players: map[string]world.PlayerState{
			"alice": {
				Body: world.LegacyMonster{
					Name: "Alice", Type: 0, Class: world.TeachMageClass,
					Level: 20, RoomID: 1, Spells: [16]byte{1 << 1},
				},
				Online: true,
			},
			"bob": {
				Body:   world.LegacyMonster{Name: "Bob", Type: 0, RoomID: 1},
				Online: true,
			},
		},
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func newTeachOwners(t *testing.T) (*Ownership, SessionLease) {
	t.Helper()
	var owners Ownership
	lease, err := owners.Acquire("alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	return &owners, lease
}

func TestParseTeachLineAndTargetOccurrence(t *testing.T) {
	cases := []struct {
		line       string
		target     string
		occurrence int
		spell      string
	}{
		{line: "가르쳐 Bob 삭풍", target: "Bob", occurrence: 1, spell: "삭풍"},
		{line: ` 가르쳐 "Bob" 2 회복 `, target: "Bob", occurrence: 2, spell: "회복"},
	}
	for _, tc := range cases {
		command, ok := ParseTeachLine(tc.line)
		if !ok || command.Alias != "가르쳐" || command.Target != tc.target || command.Occurrence != tc.occurrence || command.Spell != tc.spell {
			t.Fatalf("line=%q command=%+v ok=%v", tc.line, command, ok)
		}
		if !IsTeachLine(tc.line) {
			t.Fatalf("IsTeachLine rejected %q", tc.line)
		}
	}
	for _, line := range []string{
		"가르쳐", "가르쳐 Bob", "가르쳐 Bob 0 삭풍", "가르쳐 Bob nope 삭풍",
		"가르쳐 Bob 삭풍 추가", "가르쳐 Bob\n삭풍", "teach Bob 삭풍", "가르쳐 Bob ",
	} {
		if _, ok := ParseTeachLine(line); ok {
			t.Fatalf("invalid teach line accepted: %q", line)
		}
	}
}

func TestExecuteTeachLinePersistsGrantAndReplaysWithoutMutation(t *testing.T) {
	store := &departureStore{state: teachCommandFixture(t)}
	owners, lease := newTeachOwners(t)
	first, err := owners.ExecuteTeachLine(context.Background(), store, "w", "teach-1", lease, "가르쳐 Bob 삭풍")
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.TeachResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != "teach" || !result.Changed || !result.Broadcast || result.TargetID != "bob" || result.SpellName != "삭풍" || result.Event == nil {
		t.Fatalf("result=%+v", result)
	}
	if !strings.Contains(result.Response, "삭풍") || result.Event.ExcludeActorID != "alice" || result.Event.ExcludeTargetID != "bob" {
		t.Fatalf("result event=%+v", result.Event)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["bob"].Body.Spells[0]&(1<<1) == 0 {
		t.Fatal("target spell was not persisted")
	}
	replay, err := owners.ExecuteTeachLine(context.Background(), store, "w", "teach-1", lease, "가르쳐 Bob 삭풍")
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteTeachLineRejectsUnsupportedBeforeReceipt(t *testing.T) {
	store := &departureStore{state: teachCommandFixture(t)}
	owners, lease := newTeachOwners(t)
	if _, err := owners.ExecuteTeachLine(context.Background(), store, "w", "teach-bad", lease, "가르쳐 Bob"); err != ErrUnsupportedTeachLine || store.commits != 0 {
		t.Fatalf("err=%v commits=%d", err, store.commits)
	}
}
