package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func dmActiveSessionFixture(t *testing.T, class byte) []byte {
	t.Helper()
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"actor"},
				NPCIDs:    []string{"room-first", "room-second"},
			},
		},
		Players: map[string]world.PlayerState{
			"actor": {Body: world.LegacyMonster{Name: "운영자", Type: 0, Class: class, RoomID: 1}, Online: true},
		},
		NPCs: map[string]world.NPCState{
			"room-first":  {Body: world.LegacyMonster{Name: "방순서", Type: 1, RoomID: 1}},
			"room-second": {Body: world.LegacyMonster{Name: "활성순서", Type: 1, RoomID: 1}},
		},
		ActiveNPCIDs: []string{"room-second", "room-first"},
	}
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func admitDMActiveSessionOwner(t *testing.T) (*Ownership, SessionLease) {
	t.Helper()
	owners := &Ownership{}
	lease, err := owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	return owners, lease
}

func TestParseDMActiveLineAdmitsOnlyExactAliases(t *testing.T) {
	for _, line := range []string{"*active", "*활성", "  *active  "} {
		command, ok := ParseDMActiveLine(line)
		if !ok || command.Verb != strings.TrimSpace(line) {
			t.Fatalf("ParseDMActiveLine(%q)=%+v ok=%t", line, command, ok)
		}
		parsed, err := ParseCommand(line)
		if err != nil || parsed.Kind != CommandDMActive {
			t.Fatalf("ParseCommand(%q)=%+v err=%v", line, parsed, err)
		}
	}
	for _, line := range []string{
		"*act", "*활", "*active npc", "npc *active", "*active *활성", "*active\n", "*active\x00", "*active\u00a0", string([]byte{0xff}),
	} {
		if _, ok := ParseDMActiveLine(line); ok || IsDMActiveLine(line) {
			t.Fatalf("accepted malformed DM active line %q", line)
		}
		parsed, err := ParseCommand(line)
		if err != nil {
			continue
		}
		if parsed.Kind == CommandDMActive {
			t.Fatalf("ParseCommand(%q) routed malformed line: %+v", line, parsed)
		}
	}
}

func TestExecuteDMActiveLineIsReadOnlyAndReplaysDeterministically(t *testing.T) {
	store := &departureStore{state: dmActiveSessionFixture(t, 12)}
	initial := append([]byte(nil), store.state...)
	owners, lease := admitDMActiveSessionOwner(t)

	first, err := owners.ExecuteDMActiveLine(context.Background(), store, "w", "dm-active-1", lease, "*active")
	if err != nil || first.Replayed || store.commits != 1 || !bytes.Equal(store.state, initial) {
		t.Fatalf("first=%+v err=%v commits=%d stateChanged=%t", first, err, store.commits, !bytes.Equal(store.state, initial))
	}
	var result world.DMActiveResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	wantResponse := "현재 활동중인 괴물\n\n이름:\n   활성순서.\n   방순서.\n"
	if result.Action != world.DMActiveList || result.ActorID != "actor" || result.Verb != "*active" || result.Response != wantResponse || result.Changed {
		t.Fatalf("result=%+v", result)
	}
	if !reflect.DeepEqual(result.NPCIDs, []string{"room-second", "room-first"}) || !reflect.DeepEqual(result.NPCNames, []string{"활성순서", "방순서"}) {
		t.Fatalf("ordered result=%+v", result)
	}

	replay, err := owners.ExecuteDMActiveLine(context.Background(), store, "w", "dm-active-1", lease, "*active")
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) || !bytes.Equal(store.state, initial) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteDMActiveLineUnauthorizedAndUnsupportedAreDeterministic(t *testing.T) {
	store := &departureStore{state: dmActiveSessionFixture(t, 4)}
	initial := append([]byte(nil), store.state...)
	owners, lease := admitDMActiveSessionOwner(t)

	first, err := owners.ExecuteDMActiveLine(context.Background(), store, "w", "dm-active-mortal", lease, "*활성")
	if err != nil || first.Replayed || store.commits != 1 || !bytes.Equal(store.state, initial) {
		t.Fatalf("unauthorized=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.DMActiveResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != world.DMActiveUnknown || result.Response != world.DMActiveUnknownResponse("*활성") || result.Changed {
		t.Fatalf("unauthorized result=%+v", result)
	}
	replay, err := owners.ExecuteDMActiveLine(context.Background(), store, "w", "dm-active-mortal", lease, "*활성")
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("unauthorized replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
	if _, err := owners.ExecuteDMActiveLine(context.Background(), store, "w", "dm-active-bad", lease, "*active npc"); !errors.Is(err, ErrUnsupportedDMActiveLine) || store.commits != 1 {
		t.Fatalf("unsupported err=%v commits=%d", err, store.commits)
	}
}
