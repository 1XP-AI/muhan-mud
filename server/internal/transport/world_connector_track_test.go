package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func connectorTrackFixture(t *testing.T) *connectorCommandStore {
	t.Helper()
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}, Track: "동"},
			PlayerIDs: []string{"a", "b", "c"},
			Items:     &world.ItemCollection{Items: map[string]world.Item{}},
		}},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1, Type: 0, Class: 7, Level: 4, Stats: [5]byte{10, 15, 10, 10, 10}}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
			"b": {Body: world.LegacyMonster{Name: "Bob", RoomID: 1, Type: 0, Class: 4}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
			"c": {Body: world.LegacyMonster{Name: "Carol", RoomID: 1, Type: 0, Class: 4}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
	}
	p := state.Players["a"]
	p.Body.Flags[1/8] |= 1 << (1 % 8)
	state.Players["a"] = p
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return &connectorCommandStore{state: raw}
}

func TestWorldConnectorSubmitDispatchesTrackResponseAndCommittedRoomEvent(t *testing.T) {
	store := connectorTrackFixture(t)
	connector, actor, target, observer := connectorThreeWorldConnections(t, store, "track-world")
	connector.config.Roll = func(_, _ int) int { return 1 }
	text, err := actor.Submit(context.Background(), "추적")
	if err != nil || !strings.Contains(text, "동쪽") {
		t.Fatalf("track response=%q err=%v", text, err)
	}
	for name, connection := range map[string]*worldConnection{"target": target, "observer": observer} {
		select {
		case event := <-connection.events:
			if !strings.Contains(event, "Alice") || !strings.Contains(event, "흔적") {
				t.Fatalf("%s track event=%q", name, event)
			}
		default:
			t.Fatalf("%s track event missing", name)
		}
	}
	select {
	case event := <-actor.events:
		t.Fatalf("actor received own track event=%q", event)
	default:
	}
	if store.commits != 1 {
		t.Fatalf("commits=%d", store.commits)
	}
}
