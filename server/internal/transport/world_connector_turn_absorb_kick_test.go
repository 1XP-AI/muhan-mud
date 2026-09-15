package transport

import (
	"context"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func connectorCombatState(class byte) world.State {
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
					Name: "Alice", Type: 0, Class: class, Level: 10, RoomID: 1,
					Stats: [5]byte{10, 10, 10, 20, 10}, HPMax: 500, HPCurrent: 100,
					MPMax: 500, MPCurrent: 100, Thaco: 20, DiceCount: 1, DiceSides: 4,
				},
				Online: true,
			},
			"observer": {
				Body:   world.LegacyMonster{Name: "Bob", Type: 0, Class: 4, Level: 1, RoomID: 1, HPMax: 100, HPCurrent: 100},
				Online: true,
			},
		},
		NPCs: map[string]world.NPCState{
			"wolf": {
				Body:    world.LegacyMonster{Name: "늑대", Type: 1, Class: 4, Level: 4, RoomID: 1, HPMax: 500, HPCurrent: 500, Experience: 100, DiceCount: 1, DiceSides: 4},
				Enemies: []world.NPCEnemy{},
			},
		},
	}
	return state
}

func setConnectorFlag(body *world.LegacyMonster, bit int) {
	body.Flags[bit/8] |= 1 << (bit % 8)
}

func TestWorldConnectorSubmitDispatchesTurnAbsorbAndKick(t *testing.T) {
	tests := []struct {
		name       string
		class      byte
		line       string
		want       string
		roll       func(int, int) int
		prepare    func(*world.State)
		checkEvent string
	}{
		{
			name: "turn", class: world.TurnClericClass, line: "방혼술 늑대", want: "방혼술",
			roll: func(_, _ int) int { return 1 },
			prepare: func(state *world.State) {
				npc := state.NPCs["wolf"]
				setConnectorFlag(&npc.Body, world.TurnNPCUndeadFlag)
				state.NPCs["wolf"] = npc
			},
			checkEvent: "Alice",
		},
		{
			name: "absorb", class: world.AbsorbMageClass, line: "흡성대법 늑대", want: "흡성",
			roll: func(_, _ int) int { return 1 }, checkEvent: "Alice",
		},
		{
			name: "kick", class: world.KickBarbarianClass, line: "차기 늑대", want: "발차기",
			roll: func(_, _ int) int { return 100 }, checkEvent: "Alice",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := connectorCombatState(test.class)
			if test.prepare != nil {
				test.prepare(&state)
			}
			store := &connectorCommandStore{}
			connector, connections := boundedLaneConnection(t, store, state, "actor", "observer")
			connector.config.Roll = test.roll
			output, err := connections[0].Submit(context.Background(), test.line)
			if err != nil || !strings.Contains(output, test.want) || store.commits != 1 {
				t.Fatalf("output=%q err=%v commits=%d", output, err, store.commits)
			}
			select {
			case event := <-connections[1].events:
				if !strings.Contains(event, test.checkEvent) {
					t.Fatalf("observer event=%q", event)
				}
			default:
				t.Fatal("observer event missing")
			}
			select {
			case event := <-connections[0].events:
				t.Fatalf("actor received duplicate event=%q", event)
			default:
			}
		})
	}
}
