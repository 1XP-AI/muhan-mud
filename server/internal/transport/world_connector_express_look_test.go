package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func connectorExpressionLookFixture(t *testing.T) *connectorCommandStore {
	t.Helper()
	state := world.State{
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
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return &connectorCommandStore{state: raw}
}

func connectorThreeWorldConnections(t *testing.T, store *connectorCommandStore, worldID string) (*WorldConnector, *worldConnection, *worldConnection, *worldConnection) {
	t.Helper()
	connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: worldID, Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 3})
	if err != nil {
		t.Fatal(err)
	}
	connections := make([]*worldConnection, 0, 3)
	for _, id := range []string{"a", "b", "c"} {
		lease, acquireErr := connector.owners.Acquire(id)
		if acquireErr != nil {
			t.Fatal(acquireErr)
		}
		if admitErr := connector.owners.Admit(lease, func() error { return nil }); admitErr != nil {
			t.Fatal(admitErr)
		}
		connections = append(connections, &worldConnection{game: connector, lease: lease, ready: true, events: make(chan string, 8)})
	}
	connector.mu.Lock()
	for _, connection := range connections {
		connector.connections[connection] = struct{}{}
	}
	connector.mu.Unlock()
	return connector, connections[0], connections[1], connections[2]
}

func TestWorldConnectorSubmitDispatchesExpressAndFanOutsCommittedRoomEvent(t *testing.T) {
	store := connectorExpressionLookFixture(t)
	_, actor, target, observer := connectorThreeWorldConnections(t, store, "express-world")
	text, err := actor.Submit(context.Background(), "표현 hello")
	if err != nil || text != "예. 좋습니다.\r\n" {
		t.Fatalf("actor response=%q err=%v", text, err)
	}
	for name, connection := range map[string]*worldConnection{"target": target, "observer": observer} {
		select {
		case event := <-connection.events:
			if event != "\n:Alice님이 hello.\r\n" {
				t.Fatalf("%s event=%q", name, event)
			}
		default:
			t.Fatalf("%s expression event missing", name)
		}
	}
	select {
	case event := <-actor.events:
		t.Fatalf("actor received own expression event=%q", event)
	default:
	}
}

func TestWorldConnectorSubmitDispatchesLookAtTargetWithRecipientProjection(t *testing.T) {
	store := connectorExpressionLookFixture(t)
	_, actor, target, observer := connectorThreeWorldConnections(t, store, "look-at-world")
	text, err := actor.Submit(context.Background(), "보아 Bob")
	if err != nil || !strings.Contains(text, "Bob님을 봅니다") {
		t.Fatalf("actor response=%q err=%v", text, err)
	}
	select {
	case event := <-target.events:
		if event != "\nAlice님이 당신을 봅니다.\r\n" {
			t.Fatalf("target event=%q", event)
		}
	default:
		t.Fatal("target look-at event missing")
	}
	select {
	case event := <-observer.events:
		if event != "\nAlice님이 Bob님을 봅니다.\r\n" {
			t.Fatalf("observer event=%q", event)
		}
	default:
		t.Fatal("observer look-at event missing")
	}
	select {
	case event := <-actor.events:
		t.Fatalf("actor received own look-at event=%q", event)
	default:
	}
}
