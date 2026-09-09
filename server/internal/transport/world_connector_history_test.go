package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestWorldConnectorSubmitExpandsLegacyBangHistory(t *testing.T) {
	const roomID int16 = 200
	initial := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{roomID: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: roomID, Name: "광장"}},
			PlayerIDs: []string{"a"},
		}},
		Players: map[string]world.PlayerState{
			"a": {
				Body:   world.LegacyMonster{Name: "Alice", Type: 0, RoomID: roomID},
				Online: true,
			},
		},
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "history-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1,
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

	first, err := connection.Submit(context.Background(), "시간")
	if err != nil || !strings.Contains(first, "현재 시간") {
		t.Fatalf("first output=%q err=%v", first, err)
	}
	repeated, err := connection.Submit(context.Background(), "!")
	if err != nil || repeated != first {
		t.Fatalf("repeat output=%q first=%q err=%v", repeated, first, err)
	}
	prefixed, err := connection.Submit(context.Background(), "! ")
	if err != nil || !strings.Contains(prefixed, "현재 시간") {
		t.Fatalf("prefixed output=%q err=%v", prefixed, err)
	}
	if store.commits != 3 {
		t.Fatalf("commits=%d want=3", store.commits)
	}
}
