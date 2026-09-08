package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func connectorDoorFixture(t *testing.T) *connectorCommandStore {
	t.Helper()
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}},
			PlayerIDs: []string{"a", "b"},
			Items:     &world.ItemCollection{Items: map[string]world.Item{}},
		}},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1, Type: 0}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
			"b": {Body: world.LegacyMonster{Name: "Bob", RoomID: 1, Type: 0}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
	}
	room := state.Rooms[1]
	room.Resource.Exits = []world.LegacyExit{{Name: "북", Flags: [4]byte{40}}}
	state.Rooms[1] = room
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return &connectorCommandStore{state: raw}
}

func TestWorldConnectorSubmitDispatchesDoorResponseAndRoomEvent(t *testing.T) {
	store := connectorDoorFixture(t)
	_, actor, observer, _ := connectorThreeWorldConnections(t, store, "door-world")
	text, err := actor.Submit(context.Background(), "열어 북")
	if err != nil || !strings.Contains(text, "북쪽 출구를 열었습니다") || store.commits != 1 {
		t.Fatalf("door response=%q err=%v commits=%d", text, err, store.commits)
	}
	select {
	case event := <-observer.events:
		if !strings.Contains(event, "Alice") || !strings.Contains(event, "출구를 열었습니다") {
			t.Fatalf("door event=%q", event)
		}
	default:
		t.Fatal("door room event missing")
	}
	select {
	case event := <-actor.events:
		t.Fatalf("actor received own door event=%q", event)
	default:
	}
}
