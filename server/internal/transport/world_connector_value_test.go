package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestWorldConnectorSubmitDispatchesValueAndKeepsSnapshotReadOnly(t *testing.T) {
	var flags [8]byte
	flags[world.RoomPawnFlag/8] |= 1 << (world.RoomPawnFlag % 8)
	initial := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{200: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 200, Name: "전당포", Flags: flags}},
			PlayerIDs: []string{"a"},
		}},
		Players: map[string]world.PlayerState{
			"a": {
				Body:   world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 200},
				Online: true,
				Items: &world.ItemCollection{Items: map[string]world.Item{
					"sword-1": {Object: world.LegacyObject{Name: "검", Value: 100}},
					"sword-2": {Object: world.LegacyObject{Name: "검", Value: 300001}},
				}, Inventory: []string{"sword-1", "sword-2"}},
			},
		},
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "value-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1})
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
	output, err := connection.Submit(context.Background(), "가격 검 2")
	if err != nil || !strings.Contains(output, "100000냥") || store.commits != 1 {
		t.Fatalf("output=%q err=%v commits=%d", output, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Items.Items["sword-2"].Object.Value != 300001 {
		t.Fatalf("saved=%+v err=%v", saved.Players["a"], err)
	}
	unsupported, err := connection.Submit(context.Background(), "가격 검 0")
	if err != nil || !strings.Contains(unsupported, "아직") || store.commits != 1 {
		t.Fatalf("unsupported=%q err=%v commits=%d", unsupported, err, store.commits)
	}
}
