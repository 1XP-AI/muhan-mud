package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestWorldConnectorSubmitDispatchesYellAndPublishesCommittedRoomEvent(t *testing.T) {
	initial := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Exits: []world.LegacyExit{{Destination: 2}}}}, PlayerIDs: []string{"a", "b"}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
			2: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2}}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
			"b": {Body: world.LegacyMonster{Name: "Bob", RoomID: 1}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "yell-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 2})
	if err != nil {
		t.Fatal(err)
	}
	leaseA, err := connector.owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	leaseB, err := connector.owners.Acquire("b")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(leaseA, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(leaseB, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	a := &worldConnection{game: connector, lease: leaseA, ready: true, events: make(chan string, 4)}
	b := &worldConnection{game: connector, lease: leaseB, ready: true, events: make(chan string, 4)}
	connector.mu.Lock()
	connector.connections[a] = struct{}{}
	connector.connections[b] = struct{}{}
	connector.mu.Unlock()
	text, err := a.Submit(context.Background(), "외쳐 안녕")
	if err != nil || !strings.Contains(text, "좋습니다") {
		t.Fatalf("yell text=%q err=%v", text, err)
	}
	select {
	case event := <-b.events:
		if !strings.Contains(event, "Alice님") {
			t.Fatalf("same-room event=%q", event)
		}
	default:
		t.Fatal("same-room yell event missing")
	}
	if store.commits != 1 {
		t.Fatalf("commits=%d", store.commits)
	}
	select {
	case <-a.events:
		t.Fatal("yeller received own event")
	default:
	}
}
