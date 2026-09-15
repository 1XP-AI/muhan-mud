package transport

import (
	"context"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestWorldConnectorSubmitDispatchesBackstabAndProjectsRoomEvent(t *testing.T) {
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}},
			PlayerIDs: []string{"actor", "observer"},
			NPCIDs:    []string{"goblin"},
		}},
		Players: map[string]world.PlayerState{
			"actor": {
				Body:   world.LegacyMonster{Name: "Alice", Type: 0, Class: 8, Level: 10, RoomID: 1, HPMax: 100, HPCurrent: 100, Thaco: 10},
				Online: true,
				Items: &world.ItemCollection{Items: map[string]world.Item{
					"knife": {Object: world.LegacyObject{Name: "단검", Type: 0, ShotsCurrent: 1, DiceCount: 1, DiceSides: 4}},
				}, Ready: [20]string{19: "knife"}},
			},
			"observer": {Body: world.LegacyMonster{Name: "Observer", Type: 0, RoomID: 1}, Online: true},
		},
		NPCs: map[string]world.NPCState{
			"goblin": {Body: world.LegacyMonster{Name: "고블린", Type: 1, RoomID: 1, HPMax: 100, HPCurrent: 100, Experience: 100}, Enemies: []world.NPCEnemy{}},
		},
	}
	// PHIDDN is required for the normal hidden backstab threshold.
	actor := state.Players["actor"]
	actor.Body.Flags[1/8] |= 1 << (1 % 8)
	state.Players["actor"] = actor
	store := &connectorCommandStore{}
	connector, connections := boundedLaneConnection(t, store, state, "actor", "observer")
	connector.config.Roll = func(_, high int) int { return high }

	output, err := connections[0].Submit(context.Background(), "기습 고블린")
	if err != nil || !strings.Contains(output, "옆구리") || store.commits != 1 {
		t.Fatalf("backstab output=%q err=%v commits=%d", output, err, store.commits)
	}
	select {
	case event := <-connections[1].events:
		if !strings.Contains(event, "Alice") || !strings.Contains(event, "옆구리") {
			t.Fatalf("backstab observer event=%q", event)
		}
	default:
		t.Fatal("backstab observer event missing")
	}
	select {
	case event := <-connections[0].events:
		t.Fatalf("backstab actor received duplicate event=%q", event)
	default:
	}
}

func TestWorldConnectorSubmitDispatchesDrinkAndProjectsRoomEvent(t *testing.T) {
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}},
			PlayerIDs: []string{"actor", "observer"},
			Items:     &world.ItemCollection{Items: map[string]world.Item{}},
		}},
		Players: map[string]world.PlayerState{
			"actor": {
				Body:   world.LegacyMonster{Name: "Alice", Type: 0, Class: 4, Level: 10, RoomID: 1, HPMax: 30, HPCurrent: 10},
				Online: true,
				Items: &world.ItemCollection{Items: map[string]world.Item{
					"potion": {Object: world.LegacyObject{Name: "회복약", Type: world.DrinkPotionType, MagicPower: 1, ShotsCurrent: 1, ShotsMax: 1}},
				}, Inventory: []string{"potion"}},
			},
			"observer": {Body: world.LegacyMonster{Name: "Observer", Type: 0, RoomID: 1}, Online: true},
		},
	}
	store := &connectorCommandStore{}
	_, connections := boundedLaneConnection(t, store, state, "actor", "observer")

	output, err := connections[0].Submit(context.Background(), "먹어 회복약")
	if err != nil || !strings.Contains(output, "먹었습니다") || store.commits != 1 {
		t.Fatalf("drink output=%q err=%v commits=%d", output, err, store.commits)
	}
	select {
	case event := <-connections[1].events:
		if !strings.Contains(event, "Alice") || !strings.Contains(event, "회복약") {
			t.Fatalf("drink observer event=%q", event)
		}
	default:
		t.Fatal("drink observer event missing")
	}
	select {
	case event := <-connections[0].events:
		t.Fatalf("drink actor received duplicate event=%q", event)
	default:
	}
}
