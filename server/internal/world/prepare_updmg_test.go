package world

import (
	"reflect"
	"testing"
)

func prepareUpDmgState(body LegacyMonster) State {
	if body.Name == "" {
		body.Name = "Alice"
	}
	body.Type = 0
	body.RoomID = 1
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{1: {
			Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}},
			PlayerIDs: []string{"alice"},
		}},
		Players: map[string]PlayerState{
			"alice": {Body: body, Online: true},
		},
	}
}

func TestPreparePlanApplyPreservesSourceOrderingAndBlindClear(t *testing.T) {
	state := prepareUpDmgState(LegacyMonster{Class: 4, HPMax: 100, HPCurrent: 70, MPMax: 80, MPCurrent: 60})
	proposal, err := state.PlanPrepare("alice", 100)
	if err != nil {
		t.Fatal(err)
	}
	if !proposal.Activate || proposal.Cooldown || proposal.Interval != 15 || !proposal.Broadcast {
		t.Fatalf("proposal=%+v", proposal)
	}
	next, result, err := state.ApplyPrepare(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || !result.Broadcast || !result.Active || result.Event == nil {
		t.Fatalf("result=%+v", result)
	}
	beforeActor := state.Players["alice"].Body
	if flag(beforeActor.Flags[:], prepareFlag) {
		t.Fatal("planning mutated source")
	}
	actor := next.Players["alice"].Body
	if !flag(actor.Flags[:], prepareFlag) || actor.Timers[prepareTimerIndex] != (LegacyTimer{LastTime: 100, Interval: 15}) {
		t.Fatalf("actor=%+v", actor)
	}
	if result.Event.RoomID != 1 || result.Event.ActorID != "alice" || result.Event.ExcludeActorID != "alice" {
		t.Fatalf("event=%+v", result.Event)
	}

	blindBody := LegacyMonster{Class: 4}
	blindBody.Flags[playerBlindFlag/8] |= 1 << (playerBlindFlag % 8)
	blind := prepareUpDmgState(blindBody)
	blindProposal, err := blind.PlanPrepare("alice", 100)
	if err != nil {
		t.Fatal(err)
	}
	blindNext, blindResult, err := blind.ApplyPrepare(blindProposal)
	if err != nil {
		t.Fatal(err)
	}
	blindActor := blindNext.Players["alice"].Body
	if !blindResult.Changed || !blindResult.Broadcast || blindResult.Event == nil || flag(blindActor.Flags[:], prepareFlag) {
		t.Fatalf("blind next=%+v result=%+v", blindNext.Players["alice"], blindResult)
	}
	if blindNext.Players["alice"].Body.Timers[prepareTimerIndex] != (LegacyTimer{LastTime: 100, Interval: 15}) {
		t.Fatalf("blind timer=%+v", blindNext.Players["alice"].Body.Timers[prepareTimerIndex])
	}
}

func TestPrepareActiveAndCooldownAreDeterministicNoOps(t *testing.T) {
	activeBody := LegacyMonster{}
	activeBody.Flags[prepareFlag/8] |= 1 << (prepareFlag % 8)
	active := prepareUpDmgState(activeBody)
	p, err := active.PlanPrepare("alice", 100)
	if err != nil || !p.AlreadyActive || p.Broadcast || p.Activate {
		t.Fatalf("active proposal=%+v err=%v", p, err)
	}
	next, result, err := active.ApplyPrepare(p)
	if err != nil || result.Changed || result.Broadcast || !result.Active || !reflect.DeepEqual(next, active) {
		t.Fatalf("active next=%+v result=%+v err=%v", next, result, err)
	}

	cooldown := prepareUpDmgState(LegacyMonster{Timers: [45]LegacyTimer{prepareTimerIndex: {LastTime: 100, Interval: 15}}})
	p, err = cooldown.PlanPrepare("alice", 105)
	if err != nil || !p.Cooldown || p.WaitSeconds != 10 || p.Broadcast || p.Activate {
		t.Fatalf("cooldown proposal=%+v err=%v", p, err)
	}
	next, result, err = cooldown.ApplyPrepare(p)
	if err != nil || result.Changed || result.Broadcast || !reflect.DeepEqual(next, cooldown) {
		t.Fatalf("cooldown next=%+v result=%+v err=%v", next, result, err)
	}
}

func TestUpDmgSuccessFailureAndCooldownPreserveAtomicSourceFields(t *testing.T) {
	initial := prepareUpDmgState(LegacyMonster{
		Class: 9, Level: 4, Stats: [5]byte{10, 15}, DicePlus: 3,
		HPMax: 100, HPCurrent: 40, MPMax: 80, MPCurrent: 20,
		Timers: [45]LegacyTimer{upDmgTimerIndex: {LastTime: 0, Interval: 7}},
	})
	success, err := initial.PlanUpDmg("alice", 1200, func(low, high int) int {
		if low != 1 || high != 100 {
			t.Fatalf("roll range=%d..%d", low, high)
		}
		return 1
	})
	if err != nil || !success.Succeeded || success.Chance != 21 || !success.Broadcast {
		t.Fatalf("success proposal=%+v err=%v", success, err)
	}
	next, result, err := initial.ApplyUpDmg(success)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Succeeded || !result.Changed || !result.Broadcast || result.Event == nil {
		t.Fatalf("success result=%+v", result)
	}
	actor := next.Players["alice"].Body
	if !flag(actor.Flags[:], upDmgFlag) || actor.DicePlus != 8 || actor.HPMax != 200 || actor.MPMax != 180 || actor.HPCurrent != 200 || actor.MPCurrent != 180 || actor.Timers[upDmgTimerIndex] != (LegacyTimer{LastTime: 1200, Interval: 120}) {
		t.Fatalf("success actor=%+v", actor)
	}
	initialActor := initial.Players["alice"].Body
	if flag(initialActor.Flags[:], upDmgFlag) || initialActor.DicePlus != 3 {
		t.Fatal("planning mutated source")
	}

	failureState := prepareUpDmgState(LegacyMonster{
		Class: 9, Level: 4, Stats: [5]byte{10, 15}, DicePlus: 3,
		HPMax: 100, HPCurrent: 40, MPMax: 80, MPCurrent: 20,
		Timers: [45]LegacyTimer{upDmgTimerIndex: {LastTime: 0, Interval: 7}},
	})
	failure, err := failureState.PlanUpDmg("alice", 1200, func(int, int) int { return 100 })
	if err != nil || failure.Succeeded || !failure.Broadcast || failure.Cooldown {
		t.Fatalf("failure proposal=%+v err=%v", failure, err)
	}
	failureNext, failureResult, err := failureState.ApplyUpDmg(failure)
	if err != nil {
		t.Fatal(err)
	}
	failureActor := failureNext.Players["alice"].Body
	if failureResult.Succeeded || !failureResult.Changed || failureActor.DicePlus != 3 || failureActor.HPMax != 100 || failureActor.MPMax != 80 || failureActor.Timers[upDmgTimerIndex] != (LegacyTimer{LastTime: 240, Interval: 7}) {
		t.Fatalf("failure actor=%+v result=%+v", failureActor, failureResult)
	}

	cooldownState := prepareUpDmgState(LegacyMonster{
		Class: 9, Stats: [5]byte{10, 15}, Timers: [45]LegacyTimer{upDmgTimerIndex: {LastTime: 100, Interval: 7}},
	})
	cooldown, err := cooldownState.PlanUpDmg("alice", 105, func(int, int) int { t.Fatal("cooldown consumed RNG"); return 1 })
	if err != nil || !cooldown.Cooldown || cooldown.WaitSeconds != 1195 || cooldown.Broadcast {
		t.Fatalf("cooldown proposal=%+v err=%v", cooldown, err)
	}
	cooldownNext, cooldownResult, err := cooldownState.ApplyUpDmg(cooldown)
	if err != nil || cooldownResult.Changed || cooldownResult.Broadcast || !reflect.DeepEqual(cooldownNext, cooldownState) {
		t.Fatalf("cooldown next=%+v result=%+v err=%v", cooldownNext, cooldownResult, err)
	}
}

func TestUpDmgAuthorizationAndNumericBoundariesFailClosed(t *testing.T) {
	ordinary := prepareUpDmgState(LegacyMonster{Class: 4})
	p, err := ordinary.PlanUpDmg("alice", 100, func(int, int) int { t.Fatal("ordinary consumed RNG"); return 1 })
	if err != nil || p.Authorized || p.Broadcast {
		t.Fatalf("ordinary proposal=%+v err=%v", p, err)
	}
	next, result, err := ordinary.ApplyUpDmg(p)
	if err != nil || result.Changed || !reflect.DeepEqual(next, ordinary) {
		t.Fatalf("ordinary next=%+v result=%+v err=%v", next, result, err)
	}

	for _, tc := range []struct {
		name string
		body LegacyMonster
		now  int32
		roll int
	}{
		{"dex overflow", LegacyMonster{Class: 9, Stats: [5]byte{10, 64}}, 1200, 1},
		{"negative timer", LegacyMonster{Class: 9, Stats: [5]byte{10, 15}, Timers: [45]LegacyTimer{upDmgTimerIndex: {LastTime: -1}}}, 1200, 100},
		{"dice overflow", LegacyMonster{Class: 9, Stats: [5]byte{10, 15}, DicePlus: 32767}, 1200, 1},
		{"hp overflow", LegacyMonster{Class: 9, Stats: [5]byte{10, 15}, HPMax: 32767}, 1200, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := prepareUpDmgState(tc.body)
			if _, err := s.PlanUpDmg("alice", tc.now, func(int, int) int { return tc.roll }); err == nil {
				t.Fatal("invalid numeric boundary accepted")
			}
		})
	}

	if _, err := ordinary.PlanPrepare("alice", -1); err == nil {
		t.Fatal("negative prepare clock accepted")
	}
	stale := prepareUpDmgState(LegacyMonster{Class: 9, Stats: [5]byte{10, 15}})
	proposal, err := stale.PlanUpDmg("alice", 1200, func(int, int) int { return 1 })
	if err != nil {
		t.Fatal(err)
	}
	changed := stale.Players["alice"]
	changed.Body.Level++
	stale.Players["alice"] = changed
	if _, _, err := stale.ApplyUpDmg(proposal); err == nil {
		t.Fatal("stale up_dmg proposal accepted")
	}
}
