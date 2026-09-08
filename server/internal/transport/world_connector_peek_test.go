package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func connectorPeekFixture(t *testing.T) *connectorCommandStore {
	t.Helper()
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}},
			PlayerIDs: []string{"a", "b", "c"},
			Items:     &world.ItemCollection{Items: map[string]world.Item{}},
		}},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1, Type: 0, Class: 8, Level: 4}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
			"b": {Body: world.LegacyMonster{Name: "Bob", RoomID: 1, Type: 0, Class: 4, Level: 4}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{
				"sword": {Object: world.LegacyObject{Name: "검"}},
			}, Inventory: []string{"sword"}}},
			"c": {Body: world.LegacyMonster{Name: "Carol", RoomID: 1, Type: 0, Class: 4}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return &connectorCommandStore{state: raw}
}

func TestWorldConnectorSubmitDispatchesPeekResponseAndAlertEvents(t *testing.T) {
	store := connectorPeekFixture(t)
	connector, actor, target, observer := connectorThreeWorldConnections(t, store, "peek-world")
	rolls := []int{1, 100}
	connector.config.Roll = func(int, int) int {
		value := rolls[0]
		rolls = rolls[1:]
		return value
	}
	text, err := actor.Submit(context.Background(), "엿봐 Bob")
	if err != nil || !strings.Contains(text, "검") {
		t.Fatalf("peek response=%q err=%v", text, err)
	}
	select {
	case event := <-target.events:
		if !strings.Contains(event, "Alice") || !strings.Contains(event, "엿봅니다") {
			t.Fatalf("target peek event=%q", event)
		}
	default:
		t.Fatal("target peek event missing")
	}
	select {
	case event := <-observer.events:
		if !strings.Contains(event, "Alice") || !strings.Contains(event, "Bob") {
			t.Fatalf("observer peek event=%q", event)
		}
	default:
		t.Fatal("observer peek event missing")
	}
	select {
	case event := <-actor.events:
		t.Fatalf("actor received own peek event=%q", event)
	default:
	}
	if store.commits != 1 {
		t.Fatalf("commits=%d", store.commits)
	}
}
