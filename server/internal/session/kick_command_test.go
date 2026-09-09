package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func kickCommandFixture(t *testing.T) []byte {
	t.Helper()
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}},
			PlayerIDs: []string{"alice"},
			NPCIDs:    []string{"wolf"},
		}},
		Players: map[string]world.PlayerState{
			"alice": {Body: world.LegacyMonster{
				Name: "Alice", Type: 0, Class: world.KickBarbarianClass,
				Level: 4, RoomID: 1, Stats: [5]byte{14, 14, 10, 10, 10},
				HPMax: 100, HPCurrent: 100, Thaco: 20,
				DiceCount: 1, DiceSides: 4,
			}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		NPCs: map[string]world.NPCState{
			"wolf": {Body: world.LegacyMonster{
				Name: "늑대", Type: 1, Class: 4, Level: 4, RoomID: 1,
				HPMax: 100, HPCurrent: 100, DiceCount: 1, DiceSides: 4,
			}, Enemies: []world.NPCEnemy{}},
		},
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func admitKickActor(t *testing.T, owners *Ownership) SessionLease {
	t.Helper()
	lease, err := owners.Acquire("alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	return lease
}

func TestParseKickLineAdmitsOnlyExactAliasAndTargetToken(t *testing.T) {
	for _, line := range []string{"차기 늑대", " 차기 늑대 "} {
		command, ok := ParseKickLine(line)
		if !ok || command.Target != "늑대" {
			t.Fatalf("ParseKickLine(%q)=%+v ok=%v", line, command, ok)
		}
	}
	for _, line := range []string{"차기", "차기 늑대 extra", "차 기 늑대", "차기 늑 대", "차기 늑대\n", "kick 늑대", "차기 \x00"} {
		if IsKickLine(line) {
			t.Fatalf("unsupported kick line accepted: %q", line)
		}
	}
	if _, ok := ParseKickLine(string([]byte{'\xff'})); ok {
		t.Fatal("invalid UTF-8 kick line accepted")
	}
}

func kickCommandRoll(t *testing.T, values ...int) func(int, int) int {
	t.Helper()
	index := 0
	return func(low, high int) int {
		if index >= len(values) {
			t.Fatalf("unexpected kick random request %d..%d", low, high)
		}
		value := values[index]
		index++
		if value < low || value > high {
			t.Fatalf("kick random value %d outside %d..%d", value, low, high)
		}
		return value
	}
}

func TestExecuteKickLinePersistsNPCDamageAndReplaysWithoutReroll(t *testing.T) {
	store := &departureStore{state: kickCommandFixture(t)}
	var owners Ownership
	lease := admitKickActor(t, &owners)
	calls := 0
	roll := kickCommandRoll(t, 1, 20, 4)
	first, err := owners.ExecuteKickLine(context.Background(), store, "world", "kick-1", lease, "차기 늑대", 100, func(low, high int) int {
		calls++
		return roll(low, high)
	})
	if err != nil || first.Replayed || first.Revision != 1 || store.commits != 1 || calls != 3 {
		t.Fatalf("first=%+v err=%v commits=%d calls=%d", first, err, store.commits, calls)
	}
	var result world.KickResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != "kick" || !result.Changed || !result.Hit || !result.Succeeded || result.TargetID != "wolf" || result.Damage != 32 || result.EnemyDamage != 32 || result.Interval != 3 || result.ProficiencyAward != 0 || !strings.Contains(result.Response, "발차기로") {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.NPCs["wolf"].Body.HPCurrent != 68 || len(saved.NPCs["wolf"].Enemies) != 1 || saved.NPCs["wolf"].Enemies[0].Damage != 32 || saved.Players["alice"].Body.Timers[world.KickTimerIndex].Interval != 3 {
		t.Fatalf("saved actor=%+v npc=%+v", saved.Players["alice"].Body, saved.NPCs["wolf"])
	}
	replay, err := owners.ExecuteKickLine(context.Background(), store, "world", "kick-1", lease, "차기 늑대", 9999, func(int, int) int {
		t.Fatal("kick replay rerolled")
		return 0
	})
	if err != nil || !replay.Replayed || replay.Revision != first.Revision || store.commits != 1 || calls != 3 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d calls=%d", replay, err, store.commits, calls)
	}
}

func TestExecuteKickLineFailsClosedBeforeReceiptForMalformedAndLethal(t *testing.T) {
	store := &departureStore{state: kickCommandFixture(t)}
	var owners Ownership
	lease := admitKickActor(t, &owners)
	if _, err := owners.ExecuteKickLine(context.Background(), store, "world", "kick-bad", lease, "공격 늑대", 100, func(int, int) int { t.Fatal("malformed kick consumed RNG"); return 0 }); !errors.Is(err, ErrUnsupportedKickLine) || store.commits != 0 {
		t.Fatalf("malformed kick reached receipt: err=%v commits=%d", err, store.commits)
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
	calls := 0
	lethalRoll := kickCommandRoll(t, 4, 20, 4)
	if _, err := owners.ExecuteKickLine(context.Background(), store, "world", "kick-lethal", lease, "차기 늑대", 100, func(low, high int) int { calls++; return lethalRoll(low, high) }); !errors.Is(err, world.ErrKickDeathTransitionPending) || store.commits != 0 || calls != 3 {
		t.Fatalf("lethal kick was admitted: err=%v commits=%d calls=%d", err, store.commits, calls)
	}
	if _, err := world.DecodeState(store.state); err != nil {
		t.Fatal(err)
	}
}
