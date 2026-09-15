package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func memoCommandFixture(t *testing.T, imported bool, actorOnline bool) []byte {
	t.Helper()
	roomPlayers := []string(nil)
	if actorOnline {
		roomPlayers = []string{"alice"}
	}
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}}, PlayerIDs: roomPlayers},
			2: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2}}},
		},
		Players: map[string]world.PlayerState{
			"alice": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1}, Online: actorOnline},
			"bob":   {Body: world.LegacyMonster{Name: "Bob", RoomID: 2}, Online: false},
		},
	}
	if imported {
		s.Memos = map[string][]world.CharacterMemo{}
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func admitMemoOwner(t *testing.T) (*Ownership, SessionLease) {
	t.Helper()
	owners := &Ownership{}
	lease, err := owners.Acquire("alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	return owners, lease
}

func TestParseMemoLineRetainsBodyAndRejectsUnsafeBounds(t *testing.T) {
	command, ok := ParseMemoLine("  메모 bOB  안녕, Bob!  ")
	if !ok || command.Target != "bOB" || command.Body != "안녕, Bob!" {
		t.Fatalf("command=%+v ok=%v", command, ok)
	}
	for _, line := range []string{
		"메모",
		"메모 Bob",
		"메모 Bob \nnext",
		"메모 Bob \x00",
		"메모 Bob " + strings.Repeat("a", world.MaxMemoBodyBytes+1),
		"메모 Bob/secret hello",
		"\xff",
	} {
		if _, ok := ParseMemoLine(line); ok {
			t.Fatalf("unsafe/invalid memo accepted: %q", line)
		}
	}
	parsed, err := ParseCommand("메모 Bob hello there")
	if err != nil || parsed.Kind != CommandMemo || len(parsed.Tokens) != 4 {
		t.Fatalf("parsed=%+v err=%v", parsed, err)
	}
	malformed, err := ParseCommand("메모 Bob")
	if err != nil || malformed.Kind != CommandMemo {
		t.Fatalf("malformed memo should remain explicit command kind: %+v err=%v", malformed, err)
	}
}

func TestExecuteMemoAppendsToOfflineCanonicalTargetAndReplays(t *testing.T) {
	store := &departureStore{state: memoCommandFixture(t, true, true)}
	owners, lease := admitMemoOwner(t)
	now := time.Date(2026, 9, 10, 3, 4, 5, 0, time.UTC)
	line := "메모 bOB 안녕, 오프라인 친구"
	first, err := owners.ExecuteMemoLineWithOptions(context.Background(), store, "world", "memo-1", lease, line, world.MemoOptions{Now: now})
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.MemoResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != world.MemoAppend || result.ActorID != "alice" || result.TargetID != "bob" || result.TargetName != "Bob" || result.Memo.Body != "안녕, 오프라인 친구" || result.Response != world.MemoResponse || !result.Changed {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || len(saved.Memos["bob"]) != 1 || saved.Memos["bob"][0].SenderName != "Alice" || !saved.Memos["bob"][0].CreatedAt.Equal(now) {
		t.Fatalf("saved=%+v err=%v", saved.Memos, err)
	}
	replay, err := owners.ExecuteMemoLineWithOptions(context.Background(), store, "world", "memo-1", lease, line, world.MemoOptions{Now: now})
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteMemoConvenienceReplayDoesNotSampleReceiptIdentity(t *testing.T) {
	store := &departureStore{state: memoCommandFixture(t, true, true)}
	owners, lease := admitMemoOwner(t)
	line := "메모 Bob hello"
	first, err := owners.ExecuteMemoLine(context.Background(), store, "world", "memo-clock", lease, line)
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	replay, err := owners.ExecuteMemoLine(context.Background(), store, "world", "memo-clock", lease, line)
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteMemoFailsClosedBeforeReceiptForUnresolvedOrOfflineActor(t *testing.T) {
	for _, tc := range []struct {
		name        string
		imported    bool
		actorOnline bool
		want        error
	}{
		{name: "nil pre-migration", imported: false, actorOnline: true, want: world.ErrMemoStateUnresolved},
		{name: "offline actor", imported: true, actorOnline: false, want: world.ErrMemoActorAbsent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &departureStore{state: memoCommandFixture(t, tc.imported, tc.actorOnline)}
			owners, lease := admitMemoOwner(t)
			_, err := owners.ExecuteMemoLineWithOptions(context.Background(), store, "world", "memo-fail", lease, "메모 Bob hello", world.MemoOptions{Now: time.Unix(1, 0).UTC()})
			if !errors.Is(err, tc.want) || store.commits != 0 {
				t.Fatalf("err=%v commits=%d want=%v", err, store.commits, tc.want)
			}
		})
	}
}
