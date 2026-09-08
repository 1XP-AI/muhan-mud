package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestWorldConnectorSubmitDispatchesInfoWithoutMutatingWorld(t *testing.T) {
	initial := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
			PlayerIDs: []string{"a"},
			Items:     &world.ItemCollection{Items: map[string]world.Item{}},
		}},
		Players: map[string]world.PlayerState{
			"a": {
				Body: world.LegacyMonster{
					Name: "Alice", RoomID: 1, Level: 3, Class: 4, Race: 5,
					Stats: [5]byte{11, 12, 13, 14, 15}, HPCurrent: 33, HPMax: 44,
					MPCurrent: 7, MPMax: 8, Experience: 300, Gold: 42,
				},
				Online: true,
				Items: &world.ItemCollection{Items: map[string]world.Item{
					"sword": {Object: world.LegacyObject{Name: "검", Weight: 3}},
				}, Inventory: []string{"sword"}},
			},
		},
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "info-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1})
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
	text, err := connection.Submit(context.Background(), "정보")
	if err != nil || !strings.Contains(text, "[이름] Alice") || !strings.Contains(text, "[직업] 검사") {
		t.Fatalf("info text=%q err=%v", text, err)
	}
	saved, commits := store.snapshot()
	if commits != 1 || string(saved) != string(raw) {
		t.Fatalf("info command changed world commits=%d", commits)
	}
}
