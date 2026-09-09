package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestWorldConnectorSubmitDispatchesSaveReceiptWithoutMutatingState(t *testing.T) {
	initial := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
			PlayerIDs: []string{"a"},
			Items:     &world.ItemCollection{Items: map[string]world.Item{}},
		}},
		Players: map[string]world.PlayerState{
			"a": {
				Body:   world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 1},
				Online: true,
				Items:  &world.ItemCollection{Items: map[string]world.Item{}},
			},
		},
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "save-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1,
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
	output, err := connection.Submit(context.Background(), "저장")
	if err != nil || !strings.Contains(output, "저장하였습니다") {
		t.Fatalf("output=%q err=%v", output, err)
	}
	if store.commits != 1 || string(store.state) != string(raw) {
		t.Fatalf("save changed state commits=%d", store.commits)
	}
	unsupported, err := connection.Submit(context.Background(), "저장 지금")
	if err != nil || !strings.Contains(unsupported, "아직") || store.commits != 1 {
		t.Fatalf("unsupported=%q err=%v commits=%d", unsupported, err, store.commits)
	}
}
