package transport

import (
	"context"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestWorldConnectorSubmitDispatchesUseAndPublishesEvent(t *testing.T) {
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "약방"}},
			PlayerIDs: []string{"actor", "observer"},
			Items:     &world.ItemCollection{Items: map[string]world.Item{}},
		}},
		Players: map[string]world.PlayerState{
			"actor": {
				Body:   world.LegacyMonster{Name: "Alice", Type: 0, Class: 4, RoomID: 1, Level: 10, HPMax: 30, HPCurrent: 10},
				Online: true,
				Items: &world.ItemCollection{Items: map[string]world.Item{
					"potion": {Object: world.LegacyObject{Name: "회복약", Type: world.DrinkPotionType, MagicPower: 1, ShotsCurrent: 1}},
				}, Inventory: []string{"potion"}},
			},
			"observer": {Body: world.LegacyMonster{Name: "Bob", Type: 0, Class: 4, RoomID: 1, Level: 1, HPMax: 30, HPCurrent: 30}, Online: true},
		},
	}
	store := &connectorCommandStore{}
	connector, connections := boundedLaneConnection(t, store, state, "actor", "observer")
	connector.config.Roll = func(_, _ int) int { return 5 }
	output, err := connections[0].Submit(context.Background(), "사용 회복약")
	if err != nil || !strings.Contains(output, "회복약") || store.commits != 1 {
		t.Fatalf("use output=%q err=%v commits=%d", output, err, store.commits)
	}
	select {
	case event := <-connections[1].events:
		if !strings.Contains(event, "Alice") || !strings.Contains(event, "회복약") {
			t.Fatalf("use observer event=%q", event)
		}
	default:
		t.Fatal("use observer event missing")
	}
	select {
	case event := <-connections[0].events:
		t.Fatalf("use actor received duplicate event=%q", event)
	default:
	}
}

func TestWorldConnectorSubmitDispatchesChangeClass(t *testing.T) {
	flags := [8]byte{}
	flags[world.ChangeClassRoomFlag/8] |= 1 << (world.ChangeClassRoomFlag % 8)
	// Destination class 5 is encoded by the first class bit after RTRAIN; the
	// source folds RTRAIN+1 through +3 in increasing order.
	flags[(world.ChangeClassRoomFlag+1)/8] |= 1 << ((world.ChangeClassRoomFlag + 1) % 8)
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "전직소", Flags: flags}},
			PlayerIDs: []string{"actor"},
		}},
		Players: map[string]world.PlayerState{"actor": {
			Body:   world.LegacyMonster{Name: "Alice", Type: 0, Class: 4, RoomID: 1, Level: 2, Experience: 100000},
			Online: true,
		}},
	}
	store := &connectorCommandStore{}
	_, connections := boundedLaneConnection(t, store, state, "actor")
	output, err := connections[0].Submit(context.Background(), "직업전환 예")
	if err != nil || !strings.Contains(output, "직업이 전환") || store.commits != 1 {
		t.Fatalf("change-class output=%q err=%v commits=%d", output, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["actor"].Body.Class != 5 || saved.Players["actor"].Body.Experience != 0 {
		t.Fatalf("change-class state=%+v err=%v", saved.Players["actor"].Body, err)
	}
}
