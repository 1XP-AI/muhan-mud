package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestWorldConnectorIgnoreIsConnectionLocalAndSuppressesDirectMessage(t *testing.T) {
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
	connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "ignore-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 2})
	if err != nil {
		t.Fatal(err)
	}
	connections := make([]*worldConnection, 0, 2)
	for _, actorID := range []string{"a", "b"} {
		lease, acquireErr := connector.owners.Acquire(actorID)
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

	add, err := connections[0].Submit(context.Background(), "듣기거부 bob")
	if err != nil || !strings.Contains(add, "Bob님을") || !strings.Contains(add, "추가") || store.commits != 0 {
		t.Fatalf("ignore add output=%q err=%v commits=%d", add, err, store.commits)
	}
	list, err := connections[0].Submit(context.Background(), "듣기거부")
	if err != nil || list != "듣기 거부된 사용자: Bob.\n" {
		t.Fatalf("ignore list=%q err=%v", list, err)
	}

	blocked, err := connections[1].Submit(context.Background(), "얘기 Alice 안녕")
	if err != nil || !strings.Contains(blocked, "Alice is ignoring you") || store.commits != 0 {
		t.Fatalf("blocked DM output=%q err=%v commits=%d", blocked, err, store.commits)
	}
	select {
	case event := <-connections[1].events:
		t.Fatalf("blocked sender received event=%q", event)
	default:
	}

	remove, err := connections[0].Submit(context.Background(), "듣기거부 Bob")
	if err != nil || !strings.Contains(remove, "삭제") || len(connections[0].ignore.List()) != 0 {
		t.Fatalf("ignore remove output=%q err=%v list=%v", remove, err, connections[0].ignore.List())
	}
	delivered, err := connections[1].Submit(context.Background(), "얘기 Alice 안녕")
	if err != nil || !strings.Contains(delivered, "Alice님에게") || store.commits != 1 {
		t.Fatalf("delivered DM output=%q err=%v commits=%d", delivered, err, store.commits)
	}
	select {
	case event := <-connections[0].events:
		if !strings.Contains(event, "Bob님이") {
			t.Fatalf("recipient event=%q", event)
		}
	default:
		t.Fatal("recipient event missing after unblocking")
	}
}

func TestFindIgnoreTargetRejectsDuplicateCanonicalNames(t *testing.T) {
	state := world.State{Players: map[string]world.PlayerState{
		"a": {Body: world.LegacyMonster{Name: "Bob", Type: 0}, Online: true},
		"b": {Body: world.LegacyMonster{Name: "bob", Type: 0}, Online: true},
	}}
	connection := &worldConnection{}
	if id, name, ok := connection.findIgnoreTarget(state, "Bob"); ok || id != "" || name != "" {
		t.Fatalf("ambiguous target resolved as id=%q name=%q ok=%v", id, name, ok)
	}
}
