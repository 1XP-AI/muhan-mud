package transport

import (
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/session"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestMovementEventsPreserveCommittedActorFollowerAndNPCOrder(t *testing.T) {
	before := world.State{
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1}},
			"b": {Body: world.LegacyMonster{Name: "Bob", RoomID: 1}},
		},
		NPCs: map[string]world.NPCState{
			"guard": {Body: world.LegacyMonster{Name: "Guard", Type: 1, RoomID: 1}},
		},
	}
	after := world.State{
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 2}},
			"b": {Body: world.LegacyMonster{Name: "Bob", RoomID: 2}, FollowingID: "a"},
		},
		NPCs: map[string]world.NPCState{
			"guard": {Body: world.LegacyMonster{Name: "Guard", Type: 1, RoomID: 2}, FollowingPlayerID: "a"},
		},
	}
	events := movementEvents(before, after, "a")
	if len(events) != 4 {
		t.Fatalf("events=%+v", events)
	}
	wantRooms := []int16{1, 2, 2, 2}
	wantExcludes := []string{"a", "a", "b", ""}
	for i, event := range events {
		if event.RoomID != wantRooms[i] || event.ExcludeActorID != wantExcludes[i] {
			t.Fatalf("event[%d]=%+v", i, event)
		}
	}
	if events[0].Text == events[1].Text || events[2].Text == "" || events[3].Text == "" {
		t.Fatalf("event text=%+v", events)
	}
}

func TestWorldConnectorPublishesEventsWithoutBlockingCommand(t *testing.T) {
	events := make(chan string, 4)
	connection := &worldConnection{lease: session.SessionLease{ActorID: "b"}, events: events}
	g := &WorldConnector{connections: map[*worldConnection]struct{}{connection: {}}}
	before := world.State{Players: map[string]world.PlayerState{
		"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1}},
		"b": {Body: world.LegacyMonster{Name: "Bob", RoomID: 2}, Online: true},
	}}
	after := world.State{Players: map[string]world.PlayerState{
		"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 2}, Online: true},
		"b": {Body: world.LegacyMonster{Name: "Bob", RoomID: 2}, Online: true},
	}}
	g.publishMovement(before, after, "a")
	select {
	case text := <-events:
		if text == "" {
			t.Fatal("empty room event")
		}
	default:
		t.Fatal("room event was not queued")
	}
}

func TestWorldConnectorPublishesSayToOtherPlayersInRoom(t *testing.T) {
	events := make(chan string, 1)
	connection := &worldConnection{lease: session.SessionLease{ActorID: "b"}, events: events}
	g := &WorldConnector{connections: map[*worldConnection]struct{}{connection: {}}}
	after := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}}, PlayerIDs: []string{"a", "b"}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
			"b": {Body: world.LegacyMonster{Name: "Bob", RoomID: 1}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
	}
	g.publishSay(after, "a", "안녕하세요")
	select {
	case text := <-events:
		if text != "\nAlice님이 \"안녕하세요\"라고 말합니다.\r\n" {
			t.Fatalf("say event=%q", text)
		}
	default:
		t.Fatal("say event was not queued")
	}
}
