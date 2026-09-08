package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/session"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestWorldConnectorSubmitDispatchesEmoteWithTargetAndRoomProjection(t *testing.T) {
	initial := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}}, PlayerIDs: []string{"a", "b", "c"}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
			"b": {Body: world.LegacyMonster{Name: "Bob", RoomID: 1}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
			"c": {Body: world.LegacyMonster{Name: "Carol", RoomID: 1}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "emote-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 3})
	if err != nil {
		t.Fatal(err)
	}
	leases := make([]session.SessionLease, 0, 3)
	for _, id := range []string{"a", "b", "c"} {
		lease, acquireErr := connector.owners.Acquire(id)
		if acquireErr != nil {
			t.Fatal(acquireErr)
		}
		if admitErr := connector.owners.Admit(lease, func() error { return nil }); admitErr != nil {
			t.Fatal(admitErr)
		}
		leases = append(leases, lease)
	}
	a := &worldConnection{game: connector, lease: leases[0], ready: true, events: make(chan string, 4)}
	b := &worldConnection{game: connector, lease: leases[1], ready: true, events: make(chan string, 4)}
	c := &worldConnection{game: connector, lease: leases[2], ready: true, events: make(chan string, 4)}
	connector.mu.Lock()
	connector.connections[a] = struct{}{}
	connector.connections[b] = struct{}{}
	connector.connections[c] = struct{}{}
	connector.mu.Unlock()

	text, err := a.Submit(context.Background(), "미소 Bob")
	if err != nil || !strings.Contains(text, "Bob님에게 미소") {
		t.Fatalf("actor response=%q err=%v", text, err)
	}
	select {
	case event := <-b.events:
		if !strings.Contains(event, "Alice님이 당신에게 미소") {
			t.Fatalf("target event=%q", event)
		}
	default:
		t.Fatal("target emote event missing")
	}
	select {
	case event := <-c.events:
		if !strings.Contains(event, "Alice님이 Bob님에게 미소") {
			t.Fatalf("room event=%q", event)
		}
	default:
		t.Fatal("room emote event missing")
	}
	select {
	case event := <-a.events:
		t.Fatalf("actor received own emote event=%q", event)
	default:
	}
	if store.commits != 1 {
		t.Fatalf("commits=%d", store.commits)
	}
}
