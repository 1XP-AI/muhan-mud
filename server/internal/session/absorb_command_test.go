package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func absorbCommandFixture(t *testing.T) []byte {
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
				Name: "도술사", Type: world.AbsorbPlayerType, Class: world.AbsorbMageClass,
				Level: 20, RoomID: 1, Stats: [5]byte{10, 10, 10, 20, 10},
				HPMax: 100, HPCurrent: 90, MPMax: 100, MPCurrent: 17,
			}, Online: true},
		},
		NPCs: map[string]world.NPCState{
			"wolf": {Body: world.LegacyMonster{
				Name: "늑대", Type: world.AbsorbMonsterType, Class: 4, Level: 4,
				RoomID: 1, HPMax: 500, HPCurrent: 500,
			}, Enemies: []world.NPCEnemy{}},
		},
		ActiveNPCIDs: []string{"wolf"},
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func admitAbsorbActor(t *testing.T, owners *Ownership) SessionLease {
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

func TestParseAbsorbLineAdmitsOnlyExactAliasAndTargetToken(t *testing.T) {
	for _, line := range []string{"흡성대법 늑대", " 흡성대법 늑대 "} {
		command, ok := ParseAbsorbLine(line)
		if !ok || command.Target != "늑대" || command.Occurrence != 1 {
			t.Fatalf("ParseAbsorbLine(%q)=%+v ok=%v", line, command, ok)
		}
	}
	for _, line := range []string{"흡성대법", "흡성대법 늑대 extra", "흡 성대법 늑대", "흡성대법 늑 대", "흡성대법 늑대\n", "absorb 늑대"} {
		if IsAbsorbLine(line) {
			t.Fatalf("unsupported absorb line accepted: %q", line)
		}
	}
}

func TestExecuteAbsorbPersistsAndReplaysWithoutReroll(t *testing.T) {
	store := &departureStore{state: absorbCommandFixture(t)}
	var owners Ownership
	lease := admitAbsorbActor(t, &owners)
	calls := 0
	first, err := owners.ExecuteAbsorbLine(context.Background(), store, "world", "absorb-1", lease, "흡성대법 늑대", 1000, func(low, high int) int {
		calls++
		if low != 1 || high != 100 {
			t.Fatalf("chance range=%d..%d", low, high)
		}
		return 1
	})
	if err != nil || first.Replayed || store.commits != 1 || calls != 1 {
		t.Fatalf("first=%+v err=%v commits=%d calls=%d", first, err, store.commits, calls)
	}
	var result world.AbsorbResult
	if err := json.Unmarshal(first.Response, &result); err != nil || result.Action != "absorb" || !result.Authorized || !result.TargetFound || !result.Succeeded || result.Damage != 230 || result.ActorHP != 320 || !result.CombatApplied || result.Event == nil || !strings.Contains(result.Response, "흡성대법") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["alice"].Body.HPCurrent != 320 || saved.NPCs["wolf"].Body.HPCurrent != 270 || len(saved.NPCs["wolf"].Enemies) != 1 || saved.NPCs["wolf"].Enemies[0].Damage != 230 {
		t.Fatalf("saved actor=%+v npc=%+v", saved.Players["alice"].Body, saved.NPCs["wolf"])
	}
	replay, err := owners.ExecuteAbsorbLine(context.Background(), store, "world", "absorb-1", lease, "흡성대법 늑대", 9999, func(int, int) int {
		t.Fatal("absorb replay rerolled")
		return 100
	})
	if err != nil || !replay.Replayed || store.commits != 1 || calls != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d calls=%d", replay, err, store.commits, calls)
	}
}

func TestExecuteAbsorbFailsClosedBeforeReceiptForMalformedAndLethalContinuation(t *testing.T) {
	store := &departureStore{state: absorbCommandFixture(t)}
	var owners Ownership
	lease := admitAbsorbActor(t, &owners)
	if _, err := owners.ExecuteAbsorbLine(context.Background(), store, "world", "absorb-bad", lease, "흡성대법 늑대 extra", 1000, func(int, int) int { return 1 }); !errors.Is(err, ErrUnsupportedAbsorbLine) || store.commits != 0 {
		t.Fatalf("malformed line reached receipt: err=%v commits=%d", err, store.commits)
	}
	s, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	npc := s.NPCs["wolf"]
	npc.Body.HPCurrent = 100
	s.NPCs["wolf"] = npc
	store.state, err = json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	if _, err := owners.ExecuteAbsorbLine(context.Background(), store, "world", "absorb-lethal", lease, "흡성대법 늑대", 1000, func(int, int) int { calls++; return 1 }); !errors.Is(err, world.ErrAbsorbDeathTransitionPending) || store.commits != 0 || calls != 0 {
		t.Fatalf("lethal absorb was admitted: err=%v commits=%d calls=%d", err, store.commits, calls)
	}
}
