package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestWorldConnectorReplyUsesIncomingSenderIdentity(t *testing.T) {
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
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "reply-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 2,
	})
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

	if output, err := connections[0].Submit(context.Background(), "얘기 Bob 안녕"); err != nil || !strings.Contains(output, "Bob님에게") {
		t.Fatalf("initial message output=%q err=%v", output, err)
	}
	select {
	case <-connections[1].events:
	default:
		t.Fatal("incoming message event missing")
	}
	if id, name, ok := connections[1].getReplyTarget(); !ok || id != "a" || name != "Alice" {
		t.Fatalf("reply target=%q/%q ok=%v", id, name, ok)
	}

	output, err := connections[1].Submit(context.Background(), "/ 반가워")
	if err != nil || !strings.Contains(output, "Alice님에게") || store.commits != 2 {
		t.Fatalf("reply output=%q err=%v commits=%d", output, err, store.commits)
	}
	select {
	case event := <-connections[0].events:
		want := "\nBob님이 당신에게 \"반가워\"라고 이야기합니다.\r\n"
		if event != want {
			t.Fatalf("reply event=%q want=%q", event, want)
		}
	default:
		t.Fatal("reply event missing")
	}
	select {
	case event := <-connections[1].events:
		t.Fatalf("reply sender received event=%q", event)
	default:
	}
}

func TestWorldConnectorReplyWithoutIncomingSenderIsNonDurable(t *testing.T) {
	initial := world.State{
		Version: 1,
		Rooms:   map[int16]world.RoomState{200: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 200}}, PlayerIDs: []string{"a"}}},
		Players: map[string]world.PlayerState{"a": {Body: world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 200}, Online: true}},
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "reply-empty-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1})
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
	connection := &worldConnection{game: connector, lease: lease, ready: true, events: make(chan string, 2)}
	connector.mu.Lock()
	connector.connections[connection] = struct{}{}
	connector.mu.Unlock()
	output, err := connection.Submit(context.Background(), "/ hello")
	if err != nil || output != "누구에게 말을 전하시려구요?\r\n" || store.commits != 0 {
		t.Fatalf("output=%q err=%v commits=%d", output, err, store.commits)
	}
}
