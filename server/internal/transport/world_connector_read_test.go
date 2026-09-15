package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestWorldConnectorSubmitDispatchesReadTimeWithoutMutatingWorld(t *testing.T) {
	initial := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
			PlayerIDs: []string{"a"},
			Items:     &world.ItemCollection{Items: map[string]world.Item{}},
		}},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	wallClock := time.Date(2026, time.January, 2, 15, 4, 5, 0, time.FixedZone("PST", -8*60*60))
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "read-world", Clock: func() (int32, int) { return 100, 0 },
		WallClock: func() time.Time { return wallClock }, MaxSessions: 1,
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
	text, err := connection.Submit(context.Background(), "시간")
	if err != nil || !strings.Contains(text, "현재 시간: 오전 12시.") || !strings.Contains(text, "실제 시간: Fri Jan  2 15:04:05 2026 (PST).") {
		t.Fatalf("time text=%q err=%v", text, err)
	}
	saved, commits := store.snapshot()
	if commits != 1 || string(saved) != string(raw) {
		t.Fatalf("read command changed world commits=%d", commits)
	}
}
