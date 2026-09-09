package session

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func boardCommandFixture(t *testing.T) []byte {
	t.Helper()
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}},
			PlayerIDs: []string{"alice"},
			Items: &world.ItemCollection{Items: map[string]world.Item{
				"board-object": {Object: world.LegacyObject{Name: "게시판", Type: 100, Special: world.BoardSpecial}},
			}, Inventory: []string{"board-object"}},
		}},
		Players: map[string]world.PlayerState{
			"alice": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1}, Online: true},
			"bob":   {Body: world.LegacyMonster{Name: "Bob", RoomID: 1}, Online: false},
		},
		Boards: &world.BoardState{Boards: map[int]world.Board{100: {Posts: []world.BoardPost{
			{Number: 1, Author: "Bob", Title: "공지", Body: "본문\n", CreatedAt: time.Date(2026, 9, 9, 1, 2, 3, 0, time.UTC)},
		}}}},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func admitBoardOwner(t *testing.T) (*Ownership, SessionLease) {
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

func TestExecuteBoardLinePersistsReadAndDeleteWithReplayBoundary(t *testing.T) {
	store := &departureStore{state: boardCommandFixture(t)}
	owners, lease := admitBoardOwner(t)
	initial := append([]byte(nil), store.state...)
	list, err := owners.ExecuteBoardLine(context.Background(), store, "w", "board-list-1", lease, "게시판")
	if err != nil || list.Replayed || store.commits != 1 || !bytes.Equal(store.state, initial) {
		t.Fatalf("list=%+v err=%v commits=%d stateChanged=%v", list, err, store.commits, !bytes.Equal(store.state, initial))
	}
	if !strings.Contains(string(list.Response), "공지") || !strings.Contains(string(list.Response), "게시판 100") {
		t.Fatalf("list response=%s", list.Response)
	}

	read, err := owners.ExecuteBoardLine(context.Background(), store, "w", "board-read-1", lease, "읽어 게시판 1")
	if err != nil || read.Replayed || store.commits != 2 || !strings.Contains(string(read.Response), "본문") {
		t.Fatalf("read=%+v err=%v commits=%d", read, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Boards == nil || saved.Boards.Boards[100].Posts[0].ReadCount != 1 {
		t.Fatalf("saved boards=%+v err=%v", saved.Boards, err)
	}
	replay, err := owners.ExecuteBoardLine(context.Background(), store, "w", "board-read-1", lease, "읽어 게시판 1")
	if err != nil || !replay.Replayed || store.commits != 2 || !bytes.Equal(replay.Response, read.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}

	// The actor is not the author, so delete is rejected before a receipt.
	if _, err := owners.ExecuteBoardLine(context.Background(), store, "w", "board-delete-1", lease, "글삭제 게시판 1"); err == nil || store.commits != 2 {
		t.Fatalf("unauthorized delete err=%v commits=%d", err, store.commits)
	}
}
