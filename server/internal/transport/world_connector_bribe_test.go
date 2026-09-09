package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func bribeConnectorStore(t *testing.T) *connectorCommandStore {
	t.Helper()
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
			PlayerIDs: []string{"a", "b"},
			NPCIDs:    []string{"guard"},
		}},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 1, Gold: 500}, Online: true},
			"b": {Body: world.LegacyMonster{Name: "Bob", Type: 0, RoomID: 1}, Online: true},
		},
		NPCs: map[string]world.NPCState{
			"guard": {Body: world.LegacyMonster{Name: "Guard", Type: 1, RoomID: 1, Level: 4, Gold: 10}, Enemies: []world.NPCEnemy{}},
		},
		ActiveNPCIDs: []string{"guard"},
	}
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return &connectorCommandStore{state: raw}
}

func bribeConnectorConnections(t *testing.T, store *connectorCommandStore) (*WorldConnector, *worldConnection, *worldConnection) {
	t.Helper()
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store:       store,
		WorldID:     "bribe-world",
		Clock:       func() (int32, int) { return 100, 12 },
		MaxSessions: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	connections := make([]*worldConnection, 0, 2)
	for _, id := range []string{"a", "b"} {
		lease, err := connector.owners.Acquire(id)
		if err != nil {
			t.Fatal(err)
		}
		if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
			t.Fatal(err)
		}
		connections = append(connections, &worldConnection{game: connector, lease: lease, ready: true, events: make(chan string, 8)})
	}
	connector.mu.Lock()
	for _, connection := range connections {
		connector.connections[connection] = struct{}{}
	}
	connector.mu.Unlock()
	return connector, connections[0], connections[1]
}

func TestWorldConnectorSubmitDispatchesBribeAndPersistsGold(t *testing.T) {
	store := bribeConnectorStore(t)
	_, actor, observer := bribeConnectorConnections(t, store)
	output, err := actor.Submit(context.Background(), "Guard 100냥 뇌물")
	if err != nil || !strings.Contains(output, "뇌물을") || !strings.Contains(output, "꿈쩍도") {
		t.Fatalf("output=%q err=%v", output, err)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["a"].Body.Gold != 400 || saved.NPCs["guard"].Body.Gold != 110 {
		t.Fatalf("gold state=%+v npc=%+v", saved.Players["a"].Body, saved.NPCs["guard"].Body)
	}
	select {
	case event := <-observer.events:
		if !strings.Contains(event, "Alice") || !strings.Contains(event, "Guard") {
			t.Fatalf("observer event=%q", event)
		}
	default:
		t.Fatal("bribe room event missing")
	}
	select {
	case event := <-actor.events:
		t.Fatalf("actor received own bribe event=%q", event)
	default:
	}
	if store.commits != 1 {
		t.Fatalf("commits=%d", store.commits)
	}
}
