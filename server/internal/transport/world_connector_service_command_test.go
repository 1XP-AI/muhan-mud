package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestWorldConnectorSubmitDispatchesRepair(t *testing.T) {
	var flags [8]byte
	flags[world.RepairRoomFlag/8] |= 1 << (world.RepairRoomFlag % 8)
	initial := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{200: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 200, Name: "수리점", Flags: flags}},
			PlayerIDs: []string{"a"},
		}},
		Players: map[string]world.PlayerState{
			"a": {
				Body:   world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 200, Gold: 100},
				Online: true,
				Items: &world.ItemCollection{Items: map[string]world.Item{
					"sword": {Object: world.LegacyObject{Name: "검", Type: 4, Value: 100, ShotsMax: 30}},
				}, Inventory: []string{"sword"}},
			},
		},
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "repair-world", Clock: func() (int32, int) { return 100, 12 },
		Roll: func(low, high int) int {
			if low == 1 && high == 100 {
				return 100
			}
			return high
		}, MaxSessions: 1,
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
	output, err := connection.Submit(context.Background(), "수리 검")
	if err != nil || !strings.Contains(output, "검을 되돌려 줍니다") || store.commits != 1 {
		t.Fatalf("output=%q err=%v commits=%d", output, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Body.Gold != 75 || saved.Players["a"].Items.Items["sword"].Object.ShotsCurrent != 27 {
		t.Fatalf("saved=%+v err=%v", saved.Players["a"], err)
	}
}

func TestWorldConnectorSubmitDispatchesDirectMessageToExactRecipient(t *testing.T) {
	initial := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{200: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 200, Name: "광장"}},
			PlayerIDs: []string{"a", "b"},
		}},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 200}, Online: true},
			"b": {Body: world.LegacyMonster{Name: "Bob", Type: 0, RoomID: 200}, Online: true},
		},
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "direct-message-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 2})
	if err != nil {
		t.Fatal(err)
	}
	connections := make([]*worldConnection, 0, 2)
	for _, id := range []string{"a", "b"} {
		lease, acquireErr := connector.owners.Acquire(id)
		if acquireErr != nil {
			t.Fatal(acquireErr)
		}
		if admitErr := connector.owners.Admit(lease, func() error { return nil }); admitErr != nil {
			t.Fatal(admitErr)
		}
		connections = append(connections, &worldConnection{game: connector, lease: lease, ready: true, events: make(chan string, 4)})
	}
	connector.mu.Lock()
	for _, connection := range connections {
		connector.connections[connection] = struct{}{}
	}
	connector.mu.Unlock()
	output, err := connections[0].Submit(context.Background(), "얘기 Bob 안녕")
	if err != nil || !strings.Contains(output, "Bob님에게") || store.commits != 1 {
		t.Fatalf("output=%q err=%v commits=%d", output, err, store.commits)
	}
	select {
	case event := <-connections[1].events:
		expected := "\nAlice님이 당신에게 \"안녕\"라고 이야기합니다.\r\n"
		if event != expected {
			t.Fatalf("recipient event=%q (% x), want=%q (% x)", event, []byte(event), expected, []byte(expected))
		}
	default:
		t.Fatal("recipient direct-message event missing")
	}
	select {
	case event := <-connections[0].events:
		t.Fatalf("sender received recipient event=%q", event)
	default:
	}
}
