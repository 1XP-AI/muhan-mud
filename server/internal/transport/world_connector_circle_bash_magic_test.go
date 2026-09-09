package transport

import (
	"context"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func combatCommandConnectorState(withWeapon bool) world.State {
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "전장"}},
			PlayerIDs: []string{"actor", "observer"},
			NPCIDs:    []string{"wolf"},
		}},
		Players: map[string]world.PlayerState{
			"actor": {
				Body: world.LegacyMonster{
					Name: "Alice", Type: 0, Class: world.BashFighterClass, Level: 10,
					RoomID: 1, HPMax: 100, HPCurrent: 100, Thaco: 20,
					Stats: [5]byte{10, 10, 10, 10, 10}, DiceCount: 1, DiceSides: 4,
				},
				Online: true,
			},
			"observer": {
				Body:   world.LegacyMonster{Name: "Bob", Type: 0, Class: world.BashFighterClass, Level: 1, RoomID: 1, HPMax: 100, HPCurrent: 100},
				Online: true,
			},
		},
		NPCs: map[string]world.NPCState{
			"wolf": {
				Body:    world.LegacyMonster{Name: "늑대", Type: 1, Class: world.BashFighterClass, Level: 1, RoomID: 1, HPMax: 100, HPCurrent: 100, Experience: 100, DiceCount: 1, DiceSides: 4},
				Enemies: []world.NPCEnemy{},
			},
		},
	}
	if withWeapon {
		actor := state.Players["actor"]
		actor.Items = &world.ItemCollection{
			Items: map[string]world.Item{
				"sword": {Object: world.LegacyObject{Name: "검", Type: 0, ShotsMax: 5, ShotsCurrent: 5, DiceCount: 1, DiceSides: 4}},
			},
			Ready: [20]string{world.BashWieldSlot: "sword"},
		}
		state.Players["actor"] = actor
	}
	return state
}

func TestWorldConnectorSubmitDispatchesCircleAndPublishesRoomEvent(t *testing.T) {
	store := &connectorCommandStore{}
	connector, connections := boundedLaneConnection(t, store, combatCommandConnectorState(false), "actor", "observer")
	calls := 0
	connector.config.Roll = func(low, high int) int {
		calls++
		if calls == 1 {
			if low != 1 || high != 100 {
				t.Fatalf("circle chance range=%d..%d", low, high)
			}
			return 1
		}
		return low
	}

	output, err := connections[0].Submit(context.Background(), "교란 늑대")
	if err != nil || !strings.Contains(output, "교란시킵니다") || store.commits != 1 {
		t.Fatalf("circle output=%q err=%v commits=%d", output, err, store.commits)
	}
	select {
	case event := <-connections[1].events:
		if !strings.Contains(event, "뱅글뱅글") || !strings.Contains(event, "Alice") {
			t.Fatalf("circle observer event=%q", event)
		}
	default:
		t.Fatal("circle observer event missing")
	}
	select {
	case event := <-connections[0].events:
		t.Fatalf("circle actor received duplicate event=%q", event)
	default:
	}
}

func TestWorldConnectorSubmitDispatchesBashAndPublishesRoomEvent(t *testing.T) {
	store := &connectorCommandStore{}
	connector, connections := boundedLaneConnection(t, store, combatCommandConnectorState(true), "actor", "observer")
	calls := 0
	connector.config.Roll = func(low, high int) int {
		calls++
		switch calls {
		case 1:
			return 1 // chance
		case 2:
			return 20 // hit threshold
		case 3:
			return 6 // befuddle
		case 4:
			return 4 // weapon die
		case 5:
			return 1 // fighter bonus branch
		default:
			t.Fatalf("unexpected bash random call %d (%d..%d)", calls, low, high)
			return low
		}
	}

	output, err := connections[0].Submit(context.Background(), "맹공 늑대")
	if err != nil || !strings.Contains(output, "맹공") || store.commits != 1 {
		t.Fatalf("bash output=%q err=%v commits=%d", output, err, store.commits)
	}
	select {
	case event := <-connections[1].events:
		if !strings.Contains(event, "맹공") || !strings.Contains(event, "Alice") {
			t.Fatalf("bash observer event=%q", event)
		}
	default:
		t.Fatal("bash observer event missing")
	}
}

func TestWorldConnectorKeepsSessionUsableWhenMagicStopCombatIsPending(t *testing.T) {
	state := combatCommandConnectorState(false)
	actor := state.Players["actor"]
	actor.Body.Class = world.MagicStopRangerClass
	state.Players["actor"] = actor
	store := &connectorCommandStore{}
	_, connections := boundedLaneConnection(t, store, state, "actor")

	output, err := connections[0].Submit(context.Background(), "혈도봉쇄 늑대")
	if err != nil || output != "아직 구현되지 않은 명령입니다.\r\n" || store.commits != 0 {
		t.Fatalf("magic_stop output=%q err=%v commits=%d", output, err, store.commits)
	}
	if !connections[0].ready {
		t.Fatal("pending magic_stop sealed the session")
	}
}
