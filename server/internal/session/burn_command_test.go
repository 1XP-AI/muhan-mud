package session

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func burnCommandFixture(t *testing.T) []byte {
	t.Helper()
	actor := world.LegacyMonster{
		Name: "Alice", Type: 0, Class: 4, RoomID: 1, Gold: 10, Experience: 20,
	}
	actor.Flags[1/8] |= 1 << (1 % 8)
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}},
				PlayerIDs: []string{"actor"},
			},
		},
		Players: map[string]world.PlayerState{
			"actor": {
				Body: actor, Online: true,
				Items: &world.ItemCollection{
					Items: map[string]world.Item{
						"coin": {Object: world.LegacyObject{Name: "동전"}},
					},
					Inventory: []string{"coin"},
				},
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

func TestParseBurnLineAdmitsAliasesAndPositiveDirectOccurrence(t *testing.T) {
	tests := []struct {
		line       string
		alias      string
		name       string
		occurrence int
		accepted   bool
	}{
		{line: "태워 동전", alias: "태워", name: "동전", occurrence: 1, accepted: true},
		{line: "소각 동전 2", alias: "소각", name: "동전", occurrence: 2, accepted: true},
		{line: `태워 "긴 검" 3`, alias: "태워", name: "긴 검", occurrence: 3, accepted: true},
		{line: "태워", accepted: false},
		{line: "소각", accepted: false},
		{line: "태워 동전 0", accepted: false},
		{line: "태워 동전 -1", accepted: false},
		{line: "태워 동전 x", accepted: false},
		{line: "태워 동전 1 extra", accepted: false},
		{line: "태워\n동전", accepted: false},
		{line: "burn 동전", accepted: false},
	}
	for _, tc := range tests {
		got, ok := ParseBurnLine(tc.line)
		if ok != tc.accepted || (ok && (got.Alias != tc.alias || got.ItemName != tc.name || got.Occurrence != tc.occurrence)) {
			t.Fatalf("ParseBurnLine(%q)=%+v,%v want %+v,%v", tc.line, got, ok, tc, tc.accepted)
		}
	}
	if !IsBurnLine("소각 동전") {
		t.Fatal("IsBurnLine rejected canonical alias")
	}
}

func TestExecuteBurnLinePersistsTypedReceiptAndReplaysWithoutRandom(t *testing.T) {
	store := &departureStore{state: burnCommandFixture(t)}
	var owners Ownership
	lease, err := owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	calls := 0
	first, err := owners.ExecuteBurnLineWithOptions(context.Background(), store, "w", "burn-1", lease, "태워 동전", BurnOptions{
		Now: 8,
		Roll: func(low, high int) int {
			calls++
			if low != 1 || high != 100 {
				t.Fatalf("roll range=%d..%d", low, high)
			}
			return 1
		},
	})
	if err != nil || first.Replayed || store.commits != 1 || calls != 1 {
		t.Fatalf("first=%+v err=%v commits=%d calls=%d", first, err, store.commits, calls)
	}
	var result world.BurnResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != "burn" || !result.Burned || !result.Jackpot || result.JackpotRoll != 1 || result.GoldAward != 100004 || result.ExperienceAward != 11 || result.Event == nil {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	savedActor := saved.Players["actor"]
	if savedActor.Body.Gold != 100014 || savedActor.Body.Experience != 31 || savedActor.Items == nil || savedActor.Body.Timers[world.BurnTimerIndex] != (world.LegacyTimer{LastTime: 8, Interval: int32(world.BurnCooldown)}) {
		t.Fatalf("saved actor=%+v", savedActor)
	}
	if _, ok := savedActor.Items.Items["coin"]; ok {
		t.Fatal("burned item remained")
	}

	replay, err := owners.ExecuteBurnLineWithOptions(context.Background(), store, "w", "burn-1", lease, "태워 동전", BurnOptions{
		Now: 8,
		Roll: func(int, int) int {
			t.Fatal("burn replay consumed jackpot random")
			return 1
		},
	})
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteBurnLineUsesTwoSecondClockBoundary(t *testing.T) {
	state, err := world.DecodeState(burnCommandFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	actor := state.Players["actor"]
	actor.Body.Timers[world.BurnTimerIndex] = world.LegacyTimer{LastTime: 100, Interval: int32(world.BurnCooldown)}
	state.Players["actor"] = actor
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: raw}
	var owners Ownership
	lease, _ := owners.Acquire("actor")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	cooldown, err := owners.ExecuteBurnLineWithOptions(context.Background(), store, "w", "burn-cooldown", lease, "태워 동전", BurnOptions{
		Now: 101,
		Roll: func(int, int) int {
			t.Fatal("cooldown consumed jackpot random")
			return 1
		},
	})
	if err != nil || cooldown.Replayed || store.commits != 1 {
		t.Fatalf("cooldown=%+v err=%v commits=%d", cooldown, err, store.commits)
	}
	var rejected world.BurnResult
	if err := json.Unmarshal(cooldown.Response, &rejected); err != nil {
		t.Fatal(err)
	}
	if !rejected.Cooldown || rejected.WaitSeconds != 1 || rejected.Changed || rejected.Burned {
		t.Fatalf("rejected=%+v", rejected)
	}

	accepted, err := owners.ExecuteBurnLineWithOptions(context.Background(), store, "w", "burn-boundary", lease, "태워 동전", BurnOptions{Now: 102})
	if err != nil || accepted.Replayed || store.commits != 2 {
		t.Fatalf("accepted=%+v err=%v commits=%d", accepted, err, store.commits)
	}
	var result world.BurnResult
	if err := json.Unmarshal(accepted.Response, &result); err != nil || !result.Burned {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestExecuteBurnLineRejectsMalformedBeforeReceipt(t *testing.T) {
	store := &departureStore{state: burnCommandFixture(t)}
	var owners Ownership
	lease, _ := owners.Acquire("actor")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := owners.ExecuteBurnLineWithOptions(context.Background(), store, "w", "burn-bad", lease, "태워", BurnOptions{
		Now: 100,
		Roll: func(int, int) int {
			t.Fatal("malformed burn consumed jackpot random")
			return 1
		},
	}); err == nil || store.commits != 0 {
		t.Fatalf("malformed burn committed: err=%v commits=%d", err, store.commits)
	}
}
