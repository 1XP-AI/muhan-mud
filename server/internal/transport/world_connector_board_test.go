package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestWorldConnectorSubmitDispatchesBoardList(t *testing.T) {
	initial := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}},
			PlayerIDs: []string{"a"},
			Items: &world.ItemCollection{Items: map[string]world.Item{
				"board": {Object: world.LegacyObject{Name: "게시판", Type: 100, Special: world.BoardSpecial}},
			}, Inventory: []string{"board"}},
		}},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1}, Online: true},
		},
		Boards: &world.BoardState{Boards: map[int]world.Board{100: {Posts: []world.BoardPost{
			{Number: 1, Author: "Alice", Title: "안내", Body: "내용\n", CreatedAt: time.Date(2026, 9, 9, 1, 2, 3, 0, time.UTC)},
		}}}},
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "board-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	connection := &worldConnection{game: connector, lease: lease, ready: true}
	output, err := connection.Submit(context.Background(), "게시판")
	if err != nil || !strings.Contains(output, "안내") || !strings.Contains(output, "게시판 100") {
		t.Fatalf("output=%q err=%v", output, err)
	}
	if store.commits != 1 {
		t.Fatalf("commits=%d", store.commits)
	}
}
