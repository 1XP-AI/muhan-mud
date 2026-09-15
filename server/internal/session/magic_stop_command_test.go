package session

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func magicStopCommandFixture(t *testing.T) []byte {
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
				Name: "수호자", Type: world.MagicStopPlayerType, Class: world.MagicStopRangerClass,
				Level: 20, RoomID: 1, Stats: [5]byte{10, 20, 10, 10, 10},
				MPMax: 90, MPCurrent: 17,
			}, Online: true},
		},
		NPCs: map[string]world.NPCState{
			"wolf": {Body: world.LegacyMonster{
				Name: "늑대", Keys: [3]string{"wolf", "", ""}, Type: world.MagicStopMonsterType,
				Level: 4, Class: 4, RoomID: 1, HPMax: 100, HPCurrent: 100, MPMax: 50, MPCurrent: 31,
			}},
		},
		ActiveNPCIDs: []string{"wolf"},
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func magicStopCooldownCommandFixture(t *testing.T) []byte {
	t.Helper()
	raw := magicStopCommandFixture(t)
	s, err := world.DecodeState(raw)
	if err != nil {
		t.Fatal(err)
	}
	actor := s.Players["alice"]
	actor.Body.Timers[world.MagicStopCooldownTimerIndex] = world.LegacyTimer{LastTime: 995, Interval: 20}
	s.Players["alice"] = actor
	raw, err = json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func admitMagicStopActor(t *testing.T, owners *Ownership) SessionLease {
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

func TestParseMagicStopLineAdmitsAliasAndOneTargetToken(t *testing.T) {
	for _, tc := range []struct {
		line, target string
		occurrence   int
	}{
		{"혈도봉쇄 늑대", "늑대", 1},
		{" 혈도봉쇄 늑대 ", "늑대", 1},
	} {
		got, ok := ParseMagicStopLine(tc.line)
		if !ok || got.Target != tc.target || got.Occurrence != tc.occurrence {
			t.Fatalf("ParseMagicStopLine(%q)=%+v ok=%v", tc.line, got, ok)
		}
	}
	for _, line := range []string{
		"혈도봉쇄", "혈도봉쇄 늑대 0", "혈도봉쇄 늑대 -1", "혈도봉쇄 늑대 two",
		"혈도봉쇄 늑대 1 extra", "혈도 봉쇄 늑대", "혈도봉쇄 늑 대", "혈도봉쇄 늑대\n",
	} {
		if _, ok := ParseMagicStopLine(line); ok || IsMagicStopLine(line) {
			t.Fatalf("unsupported magic_stop line accepted: %q", line)
		}
	}
}

func TestExecuteMagicStopPersistsReceiptAndReplaysWithoutReroll(t *testing.T) {
	store := &departureStore{state: magicStopCooldownCommandFixture(t)}
	var owners Ownership
	lease := admitMagicStopActor(t, &owners)
	calls := 0
	first, err := owners.ExecuteMagicStopLine(context.Background(), store, "world", "magic-stop-1", lease, "혈도봉쇄 늑대", 1000, func(low, high int) int {
		calls++
		t.Fatalf("cooldown magic_stop consumed RNG: call=%d range=%d..%d", calls, low, high)
		return 1
	})
	if err != nil || first.Replayed || store.commits != 1 || calls != 0 {
		t.Fatalf("first=%+v err=%v commits=%d calls=%d", first, err, store.commits, calls)
	}
	var result world.MagicStopResult
	if err := json.Unmarshal(first.Response, &result); err != nil || result.Action != "magic_stop" || !result.Authorized || !result.TargetFound || !result.Cooldown || result.Changed || result.Broadcast || result.Succeeded || result.CombatApplied || result.CombatDeferred || result.MPBefore != 17 || result.MPAfter != 17 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["alice"].Body.MPCurrent != 17 || saved.Players["alice"].Body.Timers[world.MagicStopCooldownTimerIndex].LastTime != 995 || saved.NPCs["wolf"].Body.HPCurrent != 100 || saved.NPCs["wolf"].Body.MPCurrent != 31 || saved.NPCs["wolf"].Body.Timers[world.MagicStopSpellTimerIndex].Interval != 0 {
		t.Fatalf("saved state=%+v", saved)
	}
	replay, err := owners.ExecuteMagicStopLine(context.Background(), store, "world", "magic-stop-1", lease, "혈도봉쇄 늑대", 9999, func(int, int) int {
		t.Fatal("magic_stop replay rerolled")
		return 100
	})
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteMagicStopDoesNotCommitUnsupportedCombatContinuation(t *testing.T) {
	store := &departureStore{state: magicStopCommandFixture(t)}
	var owners Ownership
	lease := admitMagicStopActor(t, &owners)
	calls := 0
	_, err := owners.ExecuteMagicStopLine(context.Background(), store, "world", "magic-stop-unsupported", lease, "혈도봉쇄 늑대", 1000, func(int, int) int {
		calls++
		return 1
	})
	if err == nil || !errors.Is(err, world.ErrMagicStopCombatSideEffectPending) {
		t.Fatalf("unsupported combat continuation was admitted: err=%v", err)
	}
	if store.commits != 0 || calls != 0 {
		t.Fatalf("unsupported command committed or consumed RNG: commits=%d calls=%d", store.commits, calls)
	}
}

func TestExecuteMagicStopRejectsMalformedBeforeReceipt(t *testing.T) {
	store := &departureStore{state: magicStopCommandFixture(t)}
	var owners Ownership
	lease := admitMagicStopActor(t, &owners)
	if _, err := owners.ExecuteMagicStopLine(context.Background(), store, "world", "magic-stop-bad", lease, "혈도봉쇄 늑대 extra", 1000, func(int, int) int { return 1 }); err == nil || store.commits != 0 {
		t.Fatalf("malformed command reached receipt: err=%v commits=%d", err, store.commits)
	}
}
