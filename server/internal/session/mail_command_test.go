package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func mailCommandFixture(t *testing.T, withMessages bool) []byte {
	t.Helper()
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}}, PlayerIDs: []string{"recipient"}},
			2: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2}}},
		},
		Players: map[string]world.PlayerState{
			"recipient": {Body: world.LegacyMonster{Name: "받는이", RoomID: 1}, Online: true},
			"sender":    {Body: world.LegacyMonster{Name: "보내는이", RoomID: 2}, Online: false},
		},
	}
	state.Rooms[1] = world.RoomState{
		Resource:  state.Rooms[1].Resource,
		PlayerIDs: []string{"recipient"},
	}
	room := state.Rooms[1]
	room.Resource.Flags[world.RoomPostOfficeFlag/8] |= 1 << (world.RoomPostOfficeFlag % 8)
	state.Rooms[1] = room
	if withMessages {
		state.Mailboxes = map[string][]world.MailMessage{"recipient": {
			{ID: "mail-1", SenderID: "sender", Body: "hello\n", Timestamp: time.Date(2026, 9, 9, 1, 2, 3, 0, time.UTC)},
		}}
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func admitMailOwner(t *testing.T) (*Ownership, SessionLease) {
	t.Helper()
	owners := &Ownership{}
	lease, err := owners.Acquire("recipient")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	return owners, lease
}

func TestParseMailLineKeepsComposeExplicitlyUnsupported(t *testing.T) {
	for _, tc := range []struct {
		line   string
		action world.MailAction
		ok     bool
	}{
		{line: "편지받기", action: world.MailRead, ok: true},
		{line: "  편지삭제  ", action: world.MailDelete, ok: true},
		{line: "편지보내기", ok: false},
		{line: "편지받기 누구", ok: false},
		{line: "편지받기\n", ok: false},
		{line: "\xff", ok: false},
	} {
		got, ok := ParseMailLine(tc.line)
		if ok != tc.ok || (ok && got.Action != tc.action) {
			t.Fatalf("ParseMailLine(%q)=(%+v,%v), want action=%q ok=%v", tc.line, got, ok, tc.action, tc.ok)
		}
		if IsMailLine(tc.line) != tc.ok {
			t.Fatalf("IsMailLine(%q)=%v want %v", tc.line, IsMailLine(tc.line), tc.ok)
		}
	}
}

func TestExecuteMailReadPersistsTypedResponseAndReplaysWithoutStateChange(t *testing.T) {
	initial := mailCommandFixture(t, true)
	store := &departureStore{state: initial}
	owners, lease := admitMailOwner(t)
	first, err := owners.ExecuteMailLine(context.Background(), store, "w", "mail-read-1", lease, "편지받기")
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.MailResult
	if err := json.Unmarshal(first.Response, &result); err != nil || result.Action != world.MailRead || result.ActorID != "recipient" || len(result.Messages) != 1 || result.Messages[0].ID != "mail-1" || result.Response == world.MailReadResponseEmpty {
		t.Fatalf("result=%+v raw=%s err=%v", result, first.Response, err)
	}
	if !bytes.Equal(store.state, initial) {
		t.Fatal("read changed canonical state bytes")
	}
	replay, err := owners.ExecuteMailLine(context.Background(), store, "w", "mail-read-1", lease, "편지받기")
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteMailDeleteCommitsOnceAndReplayDoesNotDeleteAgain(t *testing.T) {
	store := &departureStore{state: mailCommandFixture(t, true)}
	owners, lease := admitMailOwner(t)
	first, err := owners.ExecuteMailLine(context.Background(), store, "w", "mail-delete-1", lease, "편지삭제")
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.MailResult
	if err := json.Unmarshal(first.Response, &result); err != nil || result.Action != world.MailDelete || result.Deleted != 1 || !result.Changed || result.Response != world.MailDeleteResponse {
		t.Fatalf("result=%+v raw=%s err=%v", result, first.Response, err)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || len(saved.Mailboxes["recipient"]) != 0 {
		t.Fatalf("saved=%+v err=%v", saved, err)
	}
	replay, err := owners.ExecuteMailLine(context.Background(), store, "w", "mail-delete-1", lease, "편지삭제")
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteMailRequiresPostOfficeAndLeavesUnsupportedUncommitted(t *testing.T) {
	store := &departureStore{state: mailCommandFixture(t, true)}
	state, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	room := state.Rooms[1]
	room.Resource.Flags = [8]byte{}
	state.Rooms[1] = room
	store.state, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	owners, lease := admitMailOwner(t)
	if _, err := owners.ExecuteMailLine(context.Background(), store, "w", "mail-wrong-room", lease, "편지받기"); err == nil || store.commits != 0 {
		t.Fatalf("wrong-room read err=%v commits=%d", err, store.commits)
	}
	if _, err := owners.ExecuteMailLine(context.Background(), store, "w", "mail-send", lease, "편지보내기"); !errors.Is(err, ErrUnsupportedMailLine) || !errors.Is(err, ErrUnsupportedMailSendLine) || store.commits != 0 {
		t.Fatalf("send err=%v commits=%d", err, store.commits)
	}
}

func TestExecuteMailReadNoMailboxHasDeterministicTypedEmptyResult(t *testing.T) {
	store := &departureStore{state: mailCommandFixture(t, false)}
	owners, lease := admitMailOwner(t)
	first, err := owners.ExecuteMailLine(context.Background(), store, "w", "mail-empty", lease, "편지받기")
	if err != nil || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.MailResult
	if err := json.Unmarshal(first.Response, &result); err != nil || result.Response != world.MailReadResponseEmpty || len(result.Messages) != 0 || result.Changed {
		t.Fatalf("result=%+v raw=%s err=%v", result, first.Response, err)
	}
}
