package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func connectorHideFixture(t *testing.T) *connectorCommandStore {
	t.Helper()
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}},
			PlayerIDs: []string{"a", "b", "c"},
			Items:     &world.ItemCollection{Items: map[string]world.Item{}},
		}},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1, Type: 0, Class: 4, Level: 4, Stats: [5]byte{10, 15, 10, 10, 10}}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
			"b": {Body: world.LegacyMonster{Name: "Bob", RoomID: 1, Type: 0, Class: 4}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
			"c": {Body: world.LegacyMonster{Name: "Carol", RoomID: 1, Type: 0, Class: 4}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return &connectorCommandStore{state: raw}
}

func TestWorldConnectorSubmitDispatchesHideResponseAndCommittedRoomEvent(t *testing.T) {
	store := connectorHideFixture(t)
	connector, actor, target, observer := connectorThreeWorldConnections(t, store, "hide-world")
	connector.config.Roll = func(_, _ int) int { return 1 }
	text, err := actor.Submit(context.Background(), "숨겨")
	if err != nil || !strings.Contains(text, "성공적으로") {
		t.Fatalf("hide response=%q err=%v", text, err)
	}
	for name, connection := range map[string]*worldConnection{"target": target, "observer": observer} {
		select {
		case event := <-connection.events:
			if !strings.Contains(event, "Alice") || !strings.Contains(event, "숨었습니다") {
				t.Fatalf("%s hide event=%q", name, event)
			}
		default:
			t.Fatalf("%s hide event missing", name)
		}
	}
	select {
	case event := <-actor.events:
		t.Fatalf("actor received own hide event=%q", event)
	default:
	}
	if store.commits != 1 {
		t.Fatalf("commits=%d", store.commits)
	}
}
