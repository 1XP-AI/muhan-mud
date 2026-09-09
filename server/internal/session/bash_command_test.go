package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func bashCommandState(t *testing.T) []byte {
	t.Helper()
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}},
				PlayerIDs: []string{"a"},
				NPCIDs:    []string{"wolf"},
			},
		},
		Players: map[string]world.PlayerState{
			"a": {
				Body: world.LegacyMonster{
					Name: "Alice", Type: 0, Class: world.BashFighterClass, Level: 4, RoomID: 1,
					Stats: [5]byte{14, 14, 10, 10, 10}, HPMax: 100, HPCurrent: 100,
					DiceCount: 1, DiceSides: 4,
				},
				Online: true,
				Items:  &world.ItemCollection{Items: map[string]world.Item{}},
			},
		},
		NPCs: map[string]world.NPCState{
			"wolf": {
				Body: world.LegacyMonster{
					Name: "늑대", Type: 1, Class: world.BashFighterClass, Level: 4, RoomID: 1,
					Stats: [5]byte{10, 10, 10, 10, 10}, HPMax: 100, HPCurrent: 100,
					DiceCount: 1, DiceSides: 4, Experience: 100,
				},
				Enemies: []world.NPCEnemy{},
			},
		},
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func bashCommandRoll(t *testing.T, values ...int) func(int, int) int {
	t.Helper()
	index := 0
	return func(low, high int) int {
		if index >= len(values) {
			t.Fatalf("unexpected bash random request %d..%d", low, high)
		}
		value := values[index]
		index++
		if value < low || value > high {
			t.Fatalf("bash random value %d outside %d..%d", value, low, high)
		}
		return value
	}
}

func TestParseBashLineAdmitsOneExactTargetToken(t *testing.T) {
	for _, line := range []string{"맹공 늑대", "  맹공   늑대  "} {
		command, ok := ParseBashLine(line)
		if !ok || command.Target != "늑대" || !IsBashLine(line) || !IsBashCommandLine(line) {
			t.Fatalf("line=%q command=%+v ok=%v", line, command, ok)
		}
	}
	for _, line := range []string{
		"맹공", "맹공 늑대 하나", "공격 늑대", "맹공 늑 대", "맹공\n늑대", "맹공 \x00",
	} {
		if command, ok := ParseBashCommandLine(line); ok {
			t.Fatalf("unsupported line accepted: %q -> %+v", line, command)
		}
	}
	if _, ok := ParseBashLine(string([]byte{0xff})); ok {
		t.Fatal("invalid UTF-8 line accepted")
	}
}

func TestExecuteBashLinePersistsReceiptAndReplaysWithoutRNG(t *testing.T) {
	store := &departureStore{state: bashCommandState(t)}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}

	first, err := owners.ExecuteBashLine(context.Background(), store, "w", "bash-1", lease, "맹공 늑대", 100, bashCommandRoll(t, 1, 20, 6, 4, 100))
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.BashResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != "bash" || !result.Changed || result.Damage != 36 || result.TargetID != "wolf" || result.TargetKind != world.BashTargetNPC {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.NPCs["wolf"].Body.HPCurrent != 64 || len(saved.NPCs["wolf"].Enemies) != 1 {
		t.Fatalf("saved=%+v err=%v", saved.NPCs["wolf"], err)
	}

	replay, err := owners.ExecuteBashLine(context.Background(), store, "w", "bash-1", lease, "맹공 늑대", 100, func(int, int) int {
		t.Fatal("bash RNG replayed")
		return 0
	})
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteBashLineRejectsUnsupportedAndLethalWithoutCommit(t *testing.T) {
	store := &departureStore{state: bashCommandState(t)}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := owners.ExecuteBashLine(context.Background(), store, "w", "bash-bad", lease, "공격 늑대", 100, func(int, int) int { t.Fatal("unsupported line consumed RNG"); return 0 }); !errors.Is(err, ErrUnsupportedBashLine) || store.commits != 0 {
		t.Fatalf("unsupported err=%v commits=%d", err, store.commits)
	}

	s, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	npc := s.NPCs["wolf"]
	npc.Body.HPCurrent = 1
	s.NPCs["wolf"] = npc
	store.state, err = json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	_, err = owners.ExecuteBashLine(context.Background(), store, "w", "bash-lethal", lease, "맹공 늑대", 100, bashCommandRoll(t, 1, 20, 6, 4, 100))
	if !errors.Is(err, world.ErrBashDeathTransitionPending) || store.commits != 0 {
		t.Fatalf("lethal err=%v commits=%d", err, store.commits)
	}
	if strings.Contains(string(store.state), "target_hp\":0") {
		t.Fatal("lethal reducer exposed a partial state")
	}
}
