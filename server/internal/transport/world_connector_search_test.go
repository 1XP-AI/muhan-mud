package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func connectorSearchFixture(t *testing.T) *connectorCommandStore {
	t.Helper()
	a := world.LegacyMonster{Name: "Alice", RoomID: 1, Type: 0, Class: 4, Level: 1, Stats: [5]byte{10, 10, 10, 10, 15}}
	b := world.LegacyMonster{Name: "Bob", RoomID: 1, Type: 0, Class: 4, Level: 1}
	b.Flags[1/8] |= 1 << (1 % 8)
	c := world.LegacyMonster{Name: "Carol", RoomID: 1, Type: 0, Class: 4, Level: 1}
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Exits: []world.LegacyExit{{Name: "비밀문", Flags: [4]byte{1}}}}},
			PlayerIDs: []string{"a", "b", "c"},
			Items: &world.ItemCollection{
				Items:     map[string]world.Item{"hidden-root": {Object: world.LegacyObject{Name: "숨은상자", Flags: [8]byte{0: 1 << 1}}}},
				Inventory: []string{"hidden-root"},
			},
		}},
		Players: map[string]world.PlayerState{
			"a": {Body: a, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
			"b": {Body: b, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
			"c": {Body: c, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return &connectorCommandStore{state: raw}
}

func TestWorldConnectorSubmitDispatchesSearchResponseAndCommittedRoomEvents(t *testing.T) {
	store := connectorSearchFixture(t)
	connector, actor, target, observer := connectorThreeWorldConnections(t, store, "search-world")
	connector.config.Roll = func(_, _ int) int { return 1 }
	text, err := actor.Submit(context.Background(), "검색")
	if err != nil || !strings.Contains(text, "Bob") || !strings.Contains(text, "비밀문") || !strings.Contains(text, "숨은상자") {
		t.Fatalf("search response=%q err=%v", text, err)
	}
	for name, connection := range map[string]*worldConnection{"target": target, "observer": observer} {
		for i := 0; i < 2; i++ {
			select {
			case event := <-connection.events:
				if i == 0 && !strings.Contains(event, "Alice") {
					t.Fatalf("%s search action event=%q", name, event)
				}
				if i == 1 && !strings.Contains(event, "발견") {
					t.Fatalf("%s search found event=%q", name, event)
				}
			default:
				t.Fatalf("%s search event %d missing", name, i)
			}
		}
	}
	select {
	case event := <-actor.events:
		t.Fatalf("actor received own search event=%q", event)
	default:
	}
	if store.commits != 1 {
		t.Fatalf("commits=%d", store.commits)
	}
}
