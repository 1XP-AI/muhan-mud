package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func giveConnectorState(withItem bool) world.State {
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
			PlayerIDs: []string{"actor", "target", "observer"},
		}},
		Players: map[string]world.PlayerState{
			"actor": {
				Body:   world.LegacyMonster{Name: "앨리스", Type: 0, Class: 1, RoomID: 1, Stats: [5]byte{10, 20, 10, 10, 10}, Gold: 100},
				Online: true,
			},
			"target": {
				Body:   world.LegacyMonster{Name: "밥", Type: 0, Class: 1, RoomID: 1, Stats: [5]byte{10, 10, 10, 10, 10}, Gold: 1},
				Online: true,
			},
			"observer": {
				Body:   world.LegacyMonster{Name: "찰리", Type: 0, Class: 1, RoomID: 1, Stats: [5]byte{10, 10, 10, 10, 10}},
				Online: true,
			},
		},
	}
	if withItem {
		actor := state.Players["actor"]
		actor.Items = &world.ItemCollection{
			Items:     map[string]world.Item{"sword-1": {Object: world.LegacyObject{Name: "검", Weight: 1}}},
			Inventory: []string{"sword-1"},
		}
		target := state.Players["target"]
		target.Items = &world.ItemCollection{Items: map[string]world.Item{}}
		state.Players["actor"] = actor
		state.Players["target"] = target
	}
	return state
}

func TestWorldConnectorSubmitDispatchesGiveItemAndPrivateFanout(t *testing.T) {
	store := &connectorCommandStore{}
	_, connections := boundedLaneConnection(t, store, giveConnectorState(true), "actor", "target", "observer")

	output, err := connections[0].Submit(context.Background(), "검 밥 줘")
	if err != nil || !strings.Contains(output, "밥님에게 검") || store.commits != 1 {
		t.Fatalf("give item output=%q err=%v commits=%d", output, err, store.commits)
	}
	select {
	case event := <-connections[1].events:
		if !strings.Contains(event, "앨리스") || !strings.Contains(event, "검") || !strings.Contains(event, "줍니다") {
			t.Fatalf("give target event=%q", event)
		}
	default:
		t.Fatal("give target event missing")
	}
	select {
	case event := <-connections[2].events:
		if !strings.Contains(event, "앨리스") || !strings.Contains(event, "밥") || !strings.Contains(event, "검") {
			t.Fatalf("give observer event=%q", event)
		}
	default:
		t.Fatal("give observer event missing")
	}
	select {
	case event := <-connections[0].events:
		t.Fatalf("give actor received duplicate event=%q", event)
	default:
	}

	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := saved.Players["actor"].Items.Items["sword-1"]; ok {
		t.Fatal("actor still owns transferred item")
	}
	if _, ok := saved.Players["target"].Items.Items["sword-1"]; !ok {
		t.Fatal("target did not receive transferred item")
	}
}

func TestWorldConnectorSubmitDispatchesGiveMoneyAndPersistsAtomicBalances(t *testing.T) {
	store := &connectorCommandStore{}
	_, connections := boundedLaneConnection(t, store, giveConnectorState(false), "actor", "target", "observer")

	output, err := connections[0].Submit(context.Background(), "10냥 밥 줘")
	if err != nil || !strings.Contains(output, "10냥") || store.commits != 1 {
		t.Fatalf("give money output=%q err=%v commits=%d", output, err, store.commits)
	}
	select {
	case event := <-connections[1].events:
		if !strings.Contains(event, "10냥") || !strings.Contains(event, "앨리스") {
			t.Fatalf("give money target event=%q", event)
		}
	default:
		t.Fatal("give money target event missing")
	}
	select {
	case event := <-connections[2].events:
		if !strings.Contains(event, "10냥") || !strings.Contains(event, "밥") {
			t.Fatalf("give money observer event=%q", event)
		}
	default:
		t.Fatal("give money observer event missing")
	}

	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor"].Body.Gold != 90 || saved.Players["target"].Body.Gold != 11 {
		t.Fatalf("balances actor=%d target=%d", saved.Players["actor"].Body.Gold, saved.Players["target"].Body.Gold)
	}
}

func poisonConnectorState(hp int16) world.State {
	return world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "전장"}},
			PlayerIDs: []string{"actor", "observer"},
			NPCIDs:    []string{"wolf"},
		}},
		Players: map[string]world.PlayerState{
			"actor": {
				Body: world.LegacyMonster{
					Name: "자객", Type: world.PoisonPlayerType, Class: world.PoisonAssassinClass,
					Level: 20, RoomID: 1, Stats: [5]byte{10, 20, 10, 10, 10}, HPMax: 100, HPCurrent: 100,
				},
				Online: true,
			},
			"observer": {
				Body:   world.LegacyMonster{Name: "관전자", Type: 0, Class: 1, Level: 1, RoomID: 1, HPMax: 100, HPCurrent: 100},
				Online: true,
			},
		},
		NPCs: map[string]world.NPCState{
			"wolf": {
				Body:    world.LegacyMonster{Name: "늑대", Type: world.PoisonMonsterType, Class: 4, Level: 4, RoomID: 1, HPMax: 99, HPCurrent: hp},
				Enemies: []world.NPCEnemy{},
			},
		},
	}
}

func TestWorldConnectorSubmitDispatchesPoisonAndPublishesRoomEvent(t *testing.T) {
	store := &connectorCommandStore{}
	connector, connections := boundedLaneConnection(t, store, poisonConnectorState(99), "actor", "observer")
	connector.config.Roll = func(low, _ int) int { return low }

	output, err := connections[0].Submit(context.Background(), "독살포 늑대")
	if err != nil || !strings.Contains(output, "중독") || store.commits != 1 {
		t.Fatalf("poison output=%q err=%v commits=%d", output, err, store.commits)
	}
	select {
	case event := <-connections[1].events:
		if !strings.Contains(event, "자객") || !strings.Contains(event, "늑대") || !strings.Contains(event, "독") {
			t.Fatalf("poison observer event=%q", event)
		}
	default:
		t.Fatal("poison observer event missing")
	}

	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.NPCs["wolf"].Body.HPCurrent != 66 || len(saved.NPCs["wolf"].Enemies) != 1 {
		t.Fatalf("poison target=%+v", saved.NPCs["wolf"])
	}
}

func TestWorldConnectorKeepsSessionUsableWhenPoisonDeathIsPending(t *testing.T) {
	store := &connectorCommandStore{}
	_, connections := boundedLaneConnection(t, store, poisonConnectorState(20), "actor")

	output, err := connections[0].Submit(context.Background(), "독살포 늑대")
	if err != nil || output != "아직 구현되지 않은 명령입니다.\r\n" || store.commits != 0 {
		t.Fatalf("poison pending output=%q err=%v commits=%d", output, err, store.commits)
	}
	if !connections[0].ready {
		t.Fatal("pending poison sealed the session")
	}
}

func TestWorldConnectorSubmitRendersEnemyStatusWithoutRoomBroadcast(t *testing.T) {
	state := poisonConnectorState(50)
	store := &connectorCommandStore{}
	_, connections := boundedLaneConnection(t, store, state, "actor", "observer")

	output, err := connections[0].Submit(context.Background(), "상태 늑대")
	if err != nil || output != "늑대 : [========       ]\r\n" || store.commits != 1 {
		t.Fatalf("enemy status output=%q err=%v commits=%d", output, err, store.commits)
	}
	select {
	case event := <-connections[1].events:
		t.Fatalf("enemy status leaked room event=%q", event)
	default:
	}
}

func TestWorldConnectorKeepsSessionUsableWhenEnemyStatusTargetIsMissing(t *testing.T) {
	store := &connectorCommandStore{}
	_, connections := boundedLaneConnection(t, store, poisonConnectorState(50), "actor")

	output, err := connections[0].Submit(context.Background(), "상태 없는괴물")
	if err != nil || output != "아직 구현되지 않은 명령입니다.\r\n" || store.commits != 0 {
		t.Fatalf("enemy status missing output=%q err=%v commits=%d", output, err, store.commits)
	}
	if !connections[0].ready {
		t.Fatal("missing enemy status target sealed the session")
	}
}

func TestWorldConnectorNinthLaneFixtureIsJSONRoundTrippable(t *testing.T) {
	for _, state := range []world.State{giveConnectorState(true), giveConnectorState(false), poisonConnectorState(99)} {
		raw, err := json.Marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := world.DecodeState(raw); err != nil {
			t.Fatalf("fixture decode: %v", err)
		}
	}
}
