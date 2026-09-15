package session

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func powerAccuracyCommandFixture(t *testing.T, class byte, weapon bool) []byte {
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
				Stats: [5]byte{10, 15}, Thaco: 20,
			}, Online: true},
		},
	}
	if weapon {
		actor := s.Players["alice"]
		actor.Items = &world.ItemCollection{Items: map[string]world.Item{
			"blade": {Object: world.LegacyObject{Name: "검", Type: 1}},
		}}
		actor.Items.Ready[world.WieldSlotIndex] = "blade"
		s.Players["alice"] = actor
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func admitPowerAccuracyCommandActor(t *testing.T, owners *Ownership) SessionLease {
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

func TestParsePowerAccuracyLinesAdmitOnlyExactBareAliases(t *testing.T) {
	for _, line := range []string{"기공집결", " 기공집결 ", "살기충전", " 살기충전 "} {
		command, ok := ParsePowerAccuracyLine(line)
		if !ok || command.Alias == "" {
			t.Fatalf("ParsePowerAccuracyLine(%q)=%+v ok=%v", line, command, ok)
		}
	}
	for _, line := range []string{"기공집결 extra", "살기충전 extra", "기 공집결", "기공집결\n", "살기충전\x00", "power"} {
		if _, ok := ParsePowerAccuracyLine(line); ok {
			t.Fatalf("unsupported power/accurate line accepted: %q", line)
		}
	}
	if command, ok := ParsePowerLine("기공집결"); !ok || command.Alias != "기공집결" || !IsPowerLine(" 기공집결 ") {
		t.Fatalf("power command=%+v ok=%v", command, ok)
	}
	if command, ok := ParseAccurateLine("살기충전"); !ok || command.Alias != "살기충전" || !IsAccurateLine(" 살기충전 ") {
		t.Fatalf("accurate command=%+v ok=%v", command, ok)
	}
}

func TestExecutePowerLinePersistsTypedEventAndReplaysWithoutReroll(t *testing.T) {
	store := &departureStore{state: powerAccuracyCommandFixture(t, world.PowerClass, false)}
	var owners Ownership
	lease := admitPowerAccuracyCommandActor(t, &owners)
	calls := 0
	first, err := owners.ExecutePowerLine(context.Background(), store, "w", "power-1", lease, "기공집결", 1000, func(low, high int) int {
		calls++
		if low != 1 || high != 100 {
			t.Fatalf("roll range=%d..%d", low, high)
		}
		return 1
	})
	if err != nil || first.Replayed || store.commits != 1 || calls != 1 {
		t.Fatalf("first=%+v err=%v commits=%d calls=%d", first, err, store.commits, calls)
	}
	var result world.PowerResult
	if err := json.Unmarshal(first.Response, &result); err != nil || result.Kind != world.PowerAbility || !result.Succeeded || !result.Changed || !result.Broadcast || result.Event == nil || result.Event.ExcludeActorID != "alice" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["alice"].Body.Stats[0] != 13 || !flagForTest(saved.Players["alice"].Body.Flags, world.PowerFlag) {
		t.Fatalf("saved=%+v err=%v", saved.Players["alice"], err)
	}
	replay, err := owners.ExecutePowerLine(context.Background(), store, "w", "power-1", lease, "기공집결", 1000, func(int, int) int {
		t.Fatal("power replay rerolled")
		return 100
	})
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteAccurateLineWeaponGateAndReplay(t *testing.T) {
	noWeaponStore := &departureStore{state: powerAccuracyCommandFixture(t, world.AccurateThiefClass, false)}
	var noWeaponOwners Ownership
	noWeaponLease := admitPowerAccuracyCommandActor(t, &noWeaponOwners)
	noWeapon, err := noWeaponOwners.ExecuteAccurateLineWithOptions(context.Background(), noWeaponStore, "w", "accurate-no-weapon", noWeaponLease, "살기충전", worldAccurateOptions(1000, func(int, int) int {
		t.Fatal("weapon gate consumed RNG")
		return 1
	}))
	if err != nil || noWeapon.Replayed {
		t.Fatalf("no-weapon receipt=%+v err=%v", noWeapon, err)
	}
	var noWeaponResult world.AccurateResult
	if err := json.Unmarshal(noWeapon.Response, &noWeaponResult); err != nil || noWeaponResult.Changed || noWeaponResult.WeaponReady || noWeaponResult.Response == "" {
		t.Fatalf("no-weapon result=%+v err=%v", noWeaponResult, err)
	}

	store := &departureStore{state: powerAccuracyCommandFixture(t, world.AccurateThiefClass, true)}
	var owners Ownership
	lease := admitPowerAccuracyCommandActor(t, &owners)
	first, err := owners.ExecuteAccurateLine(context.Background(), store, "w", "accurate-1", lease, "살기충전", 1000, func(int, int) int { return 1 })
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.AccurateResult
	if err := json.Unmarshal(first.Response, &result); err != nil || !result.Succeeded || result.ThacoDelta != -3 || result.Event == nil {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["alice"].Body.Thaco != 17 || !flagForTest(saved.Players["alice"].Body.Flags, world.AccurateFlag) {
		t.Fatalf("saved=%+v err=%v", saved.Players["alice"], err)
	}
	replay, err := owners.ExecuteAccurateLine(context.Background(), store, "w", "accurate-1", lease, "살기충전", 1000, func(int, int) int {
		t.Fatal("accurate replay rerolled")
		return 100
	})
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecutePowerAccuracyRejectsMalformedBeforeReceipt(t *testing.T) {
	store := &departureStore{state: powerAccuracyCommandFixture(t, world.PowerClass, false)}
	var owners Ownership
	lease := admitPowerAccuracyCommandActor(t, &owners)
	if _, err := owners.ExecutePowerAccuracyLine(context.Background(), store, "w", "bad-power-accuracy", lease, "기공집결 extra", PowerAccuracyOptions{Now: 1000, Roll: func(int, int) int { return 1 }}); err == nil || store.commits != 0 {
		t.Fatalf("malformed command reached receipt: err=%v commits=%d", err, store.commits)
	}
}

func worldAccurateOptions(now int32, roll func(int, int) int) AccurateOptions {
	return AccurateOptions{Now: now, Roll: roll}
}

func flagForTest(bits [8]byte, index int) bool {
	return bits[index/8]&(1<<uint(index%8)) != 0
}
