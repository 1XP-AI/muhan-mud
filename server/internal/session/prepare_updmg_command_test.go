package session

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func prepareUpDmgCommandFixture(t *testing.T, class byte) []byte {
	t.Helper()
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}},
			PlayerIDs: []string{"alice"},
		}},
		Players: map[string]world.PlayerState{
			"alice": {Body: world.LegacyMonster{
				Name: "Alice", Type: 0, RoomID: 1, Class: class, Level: 4,
				Stats: [5]byte{10, 15}, HPMax: 100, HPCurrent: 40, MPMax: 80, MPCurrent: 20,
			}, Online: true},
		},
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestParsePrepareAndUpDmgLinesAdmitOnlyBareAliases(t *testing.T) {
	for _, line := range []string{"경계", " 경계 ", "잠력격발", " 잠력격발 "} {
		if _, ok := ParsePrepareLine(line); line != "잠력격발" && line != " 잠력격발 " && !ok {
			t.Fatalf("prepare parse rejected %q", line)
		}
		if _, ok := ParseUpDmgLine(line); (line == "잠력격발" || line == " 잠력격발 ") && !ok {
			t.Fatalf("up_dmg parse rejected %q", line)
		}
	}
	for _, line := range []string{"경계 extra", "잠력격발 extra", "경계\n", "잠력격발\x00", "경계\"\""} {
		if _, ok := ParsePrepareLine(line); ok {
			t.Fatalf("extra prepare token accepted: %q", line)
		}
		if _, ok := ParseUpDmgLine(line); ok {
			t.Fatalf("extra up_dmg token accepted: %q", line)
		}
	}
}

func TestExecutePrepareLinePersistsEventAndReplaysWithoutMutation(t *testing.T) {
	store := &departureStore{state: prepareUpDmgCommandFixture(t, 4)}
	var owners Ownership
	lease, err := owners.Acquire("alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecutePrepareLine(context.Background(), store, "w", "prepare-1", lease, "경계", 100)
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.PrepareResult
	if err := json.Unmarshal(first.Response, &result); err != nil || !result.Changed || !result.Broadcast || result.Event == nil {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["alice"].Body.Timers[world.PrepareTimerIndex].LastTime != 100 {
		t.Fatalf("saved=%+v err=%v", saved, err)
	}
	replay, err := owners.ExecutePrepareLine(context.Background(), store, "w", "prepare-1", lease, "경계", 100)
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteUpDmgLinePersistsFailureCooldownAndReplaysWithoutReroll(t *testing.T) {
	store := &departureStore{state: prepareUpDmgCommandFixture(t, world.InvincibleClass)}
	var owners Ownership
	lease, err := owners.Acquire("alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteUpDmgLine(context.Background(), store, "w", "up-dmg-1", lease, "잠력격발", 1200, func(int, int) int { return 100 })
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.UpDmgResult
	if err := json.Unmarshal(first.Response, &result); err != nil || result.Succeeded || !result.Changed || !result.Broadcast || result.Roll != 100 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["alice"].Body.Timers[world.UpDmgTimerIndex].LastTime != 240 || saved.Players["alice"].Body.Timers[world.UpDmgTimerIndex].Interval != 0 {
		t.Fatalf("saved=%+v err=%v", saved.Players["alice"], err)
	}
	replay, err := owners.ExecuteUpDmgLine(context.Background(), store, "w", "up-dmg-1", lease, "잠력격발", 1200, func(int, int) int { t.Fatal("replay rerolled up_dmg"); return 1 })
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteUpDmgLineClassGateStillCreatesTypedNoOpReceipt(t *testing.T) {
	store := &departureStore{state: prepareUpDmgCommandFixture(t, 4)}
	var owners Ownership
	lease, _ := owners.Acquire("alice")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteUpDmgLine(context.Background(), store, "w", "up-dmg-class", lease, "잠력격발", 1200, func(int, int) int { t.Fatal("class gate consumed RNG"); return 1 })
	if err != nil || first.Replayed {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	var result world.UpDmgResult
	if err := json.Unmarshal(first.Response, &result); err != nil || result.Authorized || result.Changed || result.Broadcast {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	replay, err := owners.ExecuteUpDmgLine(context.Background(), store, "w", "up-dmg-class", lease, "잠력격발", 1200, func(int, int) int { t.Fatal("class replay consumed RNG"); return 1 })
	if err != nil || !replay.Replayed || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
}
