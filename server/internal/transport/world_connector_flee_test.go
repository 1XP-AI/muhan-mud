package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/session"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestWorldConnectorSubmitDispatchesFleeAndPublishesCommittedRoomEvents(t *testing.T) {
	initial := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "출발지", Exits: []world.LegacyExit{{Name: "북", Destination: 2}}}}, PlayerIDs: []string{"a", "b"}, NPCIDs: []string{"wolf-id"}},
			2: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2, Name: "도착지"}}, PlayerIDs: []string{"c"}},
		},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1, Type: 0, Class: 4, Level: 10, Stats: [5]byte{10, 10, 10, 10, 10}, HPMax: 100, HPCurrent: 100}, Online: true},
			"b": {Body: world.LegacyMonster{Name: "Bob", RoomID: 1, Type: 0, Class: 4, Level: 10}, Online: true},
			"c": {Body: world.LegacyMonster{Name: "Carol", RoomID: 2, Type: 0, Class: 4, Level: 10}, Online: true},
		},
		NPCs: map[string]world.NPCState{
			"wolf-id": {Body: world.LegacyMonster{Name: "늑대", RoomID: 1, Type: 1, Class: 4, Level: 1}, Enemies: []world.NPCEnemy{{Target: world.EntityRef{Kind: "player", ID: "a"}, Damage: 0}}},
		},
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "flee-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1, Roll: func(_, _ int) int { return 1 }})
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
	actorConnection := &worldConnection{game: connector, lease: lease, events: make(chan string, 4), ready: true}
	sourceObserver := &worldConnection{game: connector, lease: session.SessionLease{ActorID: "b"}, events: make(chan string, 4), ready: true}
	destinationObserver := &worldConnection{game: connector, lease: session.SessionLease{ActorID: "c"}, events: make(chan string, 4), ready: true}
	connector.connections[actorConnection] = struct{}{}
	connector.connections[sourceObserver] = struct{}{}
	connector.connections[destinationObserver] = struct{}{}

	text, err := actorConnection.Submit(context.Background(), "도망")
	if err != nil || !strings.Contains(text, "줄행랑") {
		t.Fatalf("flee text=%q err=%v", text, err)
	}
	stateRaw, commits := store.snapshot()
	saved, err := world.DecodeState(stateRaw)
	if err != nil || commits != 1 || saved.Players["a"].Body.RoomID != 2 || saved.Rooms[1].Resource.Track != "북" {
		t.Fatalf("saved=%+v err=%v commits=%d", saved.Players["a"], err, commits)
	}
	select {
	case message := <-sourceObserver.events:
		if !strings.Contains(message, "도망") || !strings.Contains(message, "북") {
			t.Fatalf("source event=%q", message)
		}
	default:
		t.Fatal("source observer did not receive flee event")
	}
	select {
	case message := <-destinationObserver.events:
		if !strings.Contains(message, "도착") {
			t.Fatalf("destination event=%q", message)
		}
	default:
		t.Fatal("destination observer did not receive flee event")
	}
	select {
	case message := <-actorConnection.events:
		t.Fatalf("actor received its own room event: %q", message)
	default:
	}
}
