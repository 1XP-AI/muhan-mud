package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/session"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func admitThirdLaneActor(t *testing.T, connector *WorldConnector, actorID string) session.SessionLease {
	t.Helper()
	lease, err := connector.owners.Acquire(actorID)
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	return lease
}

func TestWorldConnectorSubmitDispatchesDescriptionAndPlayerLookup(t *testing.T) {
	initial := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"a", "b"},
			},
		},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 1, Class: 4, Race: 5, Level: 3}, Online: true},
			"b": {Body: world.LegacyMonster{Name: "Bob", Type: 0, RoomID: 1, Class: 4, Race: 5, Level: 7}, Online: true},
		},
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "third-lanes", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1})
	if err != nil {
		t.Fatal(err)
	}
	lease := admitThirdLaneActor(t, connector, "a")
	connection := &worldConnection{game: connector, lease: lease, ready: true}

	description, err := connection.Submit(context.Background(), "바르게 서다 묘사")
	if err != nil || !strings.Contains(description, "바르게 서다 서 있습니다") || store.commits != 1 {
		t.Fatalf("description=%q err=%v commits=%d", description, err, store.commits)
	}
	search, err := connection.Submit(context.Background(), "사용자검색 Bob")
	if err != nil || !strings.Contains(search, "사용자: Bob") || store.commits != 2 {
		t.Fatalf("search=%q err=%v commits=%d", search, err, store.commits)
	}
	info, err := connection.Submit(context.Background(), "사용자정보 Bob")
	if err != nil || !strings.Contains(info, "현재 접속 중 입니다") || store.commits != 3 {
		t.Fatalf("info=%q err=%v commits=%d", info, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Body.Description != "바르게 서다 " {
		t.Fatalf("saved=%+v err=%v", saved.Players["a"], err)
	}
}

func TestWorldConnectorSubmitDispatchesReturnSquareAndPublishesCommittedRooms(t *testing.T) {
	initial := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			2:    {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2, Name: "사냥터"}}, PlayerIDs: []string{"a", "b"}},
			1001: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1001, Name: "광장"}}, PlayerIDs: []string{"c"}},
		},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 2, Class: 4, Level: 21, MPCurrent: 9}, Online: true},
			"b": {Body: world.LegacyMonster{Name: "Bob", Type: 0, RoomID: 2, Class: 4}, Online: true},
			"c": {Body: world.LegacyMonster{Name: "Charlie", Type: 0, RoomID: 1001, Class: 4}, Online: true},
		},
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "return-square", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 3})
	if err != nil {
		t.Fatal(err)
	}
	actorLease := admitThirdLaneActor(t, connector, "a")
	actor := &worldConnection{game: connector, lease: actorLease, ready: true, events: make(chan string, 4)}
	sourceObserver := &worldConnection{game: connector, lease: session.SessionLease{ActorID: "b"}, ready: true, events: make(chan string, 4)}
	destinationObserver := &worldConnection{game: connector, lease: session.SessionLease{ActorID: "c"}, ready: true, events: make(chan string, 4)}
	connector.connections[actor] = struct{}{}
	connector.connections[sourceObserver] = struct{}{}
	connector.connections[destinationObserver] = struct{}{}

	output, err := actor.Submit(context.Background(), "귀환")
	if err != nil || !strings.Contains(output, "귀환") || store.commits != 1 {
		t.Fatalf("output=%q err=%v commits=%d", output, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Body.RoomID != world.ReturnSquareSquareRoom || saved.Players["a"].Body.MPCurrent != 0 {
		t.Fatalf("saved=%+v err=%v", saved.Players["a"], err)
	}
	select {
	case message := <-sourceObserver.events:
		if !strings.Contains(message, "갑자기 사라집니다") {
			t.Fatalf("source event=%q", message)
		}
	default:
		t.Fatal("source observer did not receive return event")
	}
	select {
	case message := <-destinationObserver.events:
		if !strings.Contains(message, "자욱한 연기") {
			t.Fatalf("destination event=%q", message)
		}
	default:
		t.Fatal("destination observer did not receive return event")
	}
}
