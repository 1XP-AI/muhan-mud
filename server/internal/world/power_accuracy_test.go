package world

import (
	"reflect"
	"strings"
	"testing"
)

func powerAccuracyState(body LegacyMonster, weapon bool) State {
	body.Name = "Alice"
	body.Type = 0
	body.RoomID = 1
	player := PlayerState{Body: body, Online: true}
	if weapon {
		player.Items = &ItemCollection{
			Items: map[string]Item{
				"blade": {Object: LegacyObject{Name: "검", Type: 1}},
			},
		}
		player.Items.Ready[WieldSlotIndex] = "blade"
	}
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{1: {
			Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}},
			PlayerIDs: []string{"alice"},
		}},
		Players: map[string]PlayerState{"alice": player},
	}
}

func TestPowerAccuracySourceConstantsChanceAndPermissions(t *testing.T) {
	if PowerFlag != 52 || PowerTimerIndex != 37 || AccurateFlag != 53 || AccurateTimerIndex != 38 || WieldSlotIndex != 19 {
		t.Fatalf("source slots changed: power=%d/%d accurate=%d/%d wield=%d", PowerFlag, PowerTimerIndex, AccurateFlag, AccurateTimerIndex, WieldSlotIndex)
	}
	body := LegacyMonster{Class: PowerClass, Level: 4, Stats: [5]byte{10, 15}}
	if chance, err := PowerChance(body); err != nil || chance != 21 {
		t.Fatalf("power chance=%d err=%v", chance, err)
	}
	body.Class = AccurateAssassinClass
	if chance, err := AccurateChance(body); err != nil || chance != 21 {
		t.Fatalf("accurate chance=%d err=%v", chance, err)
	}
	for _, tc := range []struct {
		kind  PowerAccuracyKind
		class byte
		want  bool
	}{
		{PowerAbility, PowerClass, true},
		{PowerAbility, AccurateAssassinClass, false},
		{PowerAbility, PowerInvincibleClass, true},
		{AccurateAbility, AccurateAssassinClass, true},
		{AccurateAbility, AccurateThiefClass, true},
		{AccurateAbility, PowerClass, false},
		{AccurateAbility, PowerInvincibleClass, true},
	} {
		got, err := powerAccuracyAuthorized(tc.kind, tc.class)
		if err != nil || got != tc.want {
			t.Fatalf("authorize(%q,%d)=%v err=%v want=%v", tc.kind, tc.class, got, err, tc.want)
		}
	}
	if _, err := PowerChance(LegacyMonster{Stats: [5]byte{10, 64}}); err == nil {
		t.Fatal("out-of-range dexterity accepted")
	}
	if _, err := powerAccuracyAuthorized(PowerAbility, 13); err == nil {
		t.Fatal("out-of-range class accepted")
	}
}

func TestPlanApplyPowerSuccessAndFailurePreserveSourceOrdering(t *testing.T) {
	initial := powerAccuracyState(LegacyMonster{
		Class: PowerClass, Level: 4, Stats: [5]byte{10, 15},
		Timers: [45]LegacyTimer{PowerTimerIndex: {LastTime: 0, Interval: 7}},
	}, false)
	proposal, err := initial.PlanPower("alice", 1000, func(low, high int) int {
		if low != 1 || high != 100 {
			t.Fatalf("roll range=%d..%d", low, high)
		}
		return 1
	})
	if err != nil || !proposal.Succeeded || proposal.Chance != 21 || proposal.Interval != 120 || proposal.StatDelta != 3 || !proposal.Broadcast {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	initialActor := initial.Players["alice"].Body
	if flag(initialActor.Flags[:], PowerFlag) || initialActor.Stats[0] != 10 {
		t.Fatal("planning mutated canonical source")
	}
	next, result, err := initial.ApplyPower(proposal)
	if err != nil {
		t.Fatal(err)
	}
	actor := next.Players["alice"].Body
	if !result.Succeeded || !result.Changed || !result.Broadcast || result.Event == nil || !result.Active || !flag(actor.Flags[:], PowerFlag) || actor.Stats[0] != 13 || actor.Timers[PowerTimerIndex] != (LegacyTimer{LastTime: 1000, Interval: 120}) {
		t.Fatalf("actor=%+v result=%+v", actor, result)
	}
	if !strings.Contains(result.Event.Text, "가부좌") || result.Event.ExcludeActorID != "alice" {
		t.Fatalf("event=%+v", result.Event)
	}

	failureState := powerAccuracyState(LegacyMonster{
		Class: PowerClass, Level: 4, Stats: [5]byte{10, 15},
		Timers: [45]LegacyTimer{PowerTimerIndex: {LastTime: 0, Interval: 7}},
	}, false)
	failure, err := failureState.PlanPower("alice", 1000, func(int, int) int { return 100 })
	if err != nil || failure.Succeeded || !failure.Attempted || !failure.Broadcast || failure.Interval != 7 {
		t.Fatalf("failure proposal=%+v err=%v", failure, err)
	}
	failureNext, failureResult, err := failureState.ApplyPower(failure)
	if err != nil {
		t.Fatal(err)
	}
	failureActor := failureNext.Players["alice"].Body
	if failureResult.Succeeded || !failureResult.Changed || failureActor.Stats[0] != 10 || flag(failureActor.Flags[:], PowerFlag) || failureActor.Timers[PowerTimerIndex] != (LegacyTimer{LastTime: 410, Interval: 7}) {
		t.Fatalf("failure actor=%+v result=%+v", failureActor, failureResult)
	}
}

func TestPowerGatesCooldownActiveAndStaleProposalFailClosed(t *testing.T) {
	ordinary := powerAccuracyState(LegacyMonster{Class: AccurateAssassinClass, Stats: [5]byte{10, 15}}, false)
	proposal, err := ordinary.PlanPower("alice", 1000, func(int, int) int { t.Fatal("class gate consumed RNG"); return 1 })
	if err != nil || proposal.Authorized || proposal.Broadcast {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	next, result, err := ordinary.ApplyPower(proposal)
	if err != nil || result.Changed || !reflect.DeepEqual(next, ordinary) {
		t.Fatalf("next=%+v result=%+v err=%v", next, result, err)
	}

	active := powerAccuracyState(LegacyMonster{Class: PowerClass, Stats: [5]byte{10, 15}}, false)
	activeActor := active.Players["alice"]
	activeActor.Body.Flags[PowerFlag/8] |= 1 << (PowerFlag % 8)
	active.Players["alice"] = activeActor
	proposal, err = active.PlanPower("alice", 1000, func(int, int) int { t.Fatal("active gate consumed RNG"); return 1 })
	if err != nil || !proposal.Active || proposal.Broadcast {
		t.Fatalf("active proposal=%+v err=%v", proposal, err)
	}
	next, result, err = active.ApplyPower(proposal)
	if err != nil || result.Changed || !result.Active || !reflect.DeepEqual(next, active) {
		t.Fatalf("active next=%+v result=%+v err=%v", next, result, err)
	}

	cooldown := powerAccuracyState(LegacyMonster{Class: PowerClass, Stats: [5]byte{10, 15}, Timers: [45]LegacyTimer{PowerTimerIndex: {LastTime: 900, Interval: 120}}}, false)
	proposal, err = cooldown.PlanPower("alice", 1000, func(int, int) int { t.Fatal("cooldown consumed RNG"); return 1 })
	if err != nil || !proposal.Cooldown || proposal.WaitSeconds != 500 || proposal.Broadcast {
		t.Fatalf("cooldown proposal=%+v err=%v", proposal, err)
	}
	next, result, err = cooldown.ApplyPower(proposal)
	if err != nil || result.Changed || result.Broadcast || !result.Cooldown || !reflect.DeepEqual(next, cooldown) {
		t.Fatalf("cooldown next=%+v result=%+v err=%v", next, result, err)
	}

	stale := powerAccuracyState(LegacyMonster{Class: PowerClass, Level: 4, Stats: [5]byte{10, 15}}, false)
	proposal, err = stale.PlanPower("alice", 1000, func(int, int) int { return 1 })
	if err != nil {
		t.Fatal(err)
	}
	changed := stale.Players["alice"]
	changed.Body.Level++
	stale.Players["alice"] = changed
	if _, _, err := stale.ApplyPower(proposal); err == nil {
		t.Fatal("stale power proposal accepted")
	}
}

func TestPlanApplyAccurateRequiresCanonicalWieldAndChangesThaco(t *testing.T) {
	noWeapon := powerAccuracyState(LegacyMonster{Class: AccurateThiefClass, Level: 4, Stats: [5]byte{10, 15}, Thaco: 20}, false)
	proposal, err := noWeapon.PlanAccurate("alice", 1000, func(int, int) int { t.Fatal("weapon gate consumed RNG"); return 1 })
	if err != nil || proposal.WeaponReady || proposal.Response != powerAccuracyNoWeaponResponse() {
		t.Fatalf("no-weapon proposal=%+v err=%v", proposal, err)
	}
	next, result, err := noWeapon.ApplyAccurate(proposal)
	if err != nil || result.Changed || !reflect.DeepEqual(next, noWeapon) {
		t.Fatalf("no-weapon next=%+v result=%+v err=%v", next, result, err)
	}

	initial := powerAccuracyState(LegacyMonster{
		Class: AccurateThiefClass, Level: 4, Stats: [5]byte{10, 15}, Thaco: 20,
		Timers: [45]LegacyTimer{AccurateTimerIndex: {LastTime: 0, Interval: 11}},
	}, true)
	proposal, err = initial.PlanAccurate("alice", 1000, func(int, int) int { return 1 })
	if err != nil || !proposal.Succeeded || proposal.ThacoDelta != -3 || proposal.Interval != 150 || !proposal.WeaponReady {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	next, result, err = initial.ApplyAccurate(proposal)
	if err != nil {
		t.Fatal(err)
	}
	actor := next.Players["alice"].Body
	if !result.Succeeded || !result.Broadcast || result.Event == nil || !flag(actor.Flags[:], AccurateFlag) || actor.Thaco != 17 || actor.Timers[AccurateTimerIndex] != (LegacyTimer{LastTime: 1000, Interval: 150}) {
		t.Fatalf("actor=%+v result=%+v", actor, result)
	}
	if !strings.Contains(result.Event.Text, "무기에 피를 먹입니다") {
		t.Fatalf("event=%+v", result.Event)
	}
}

func TestPowerAccuracyOverflowAndRandomBoundariesFailClosed(t *testing.T) {
	strengthOverflow := powerAccuracyState(LegacyMonster{Class: PowerClass, Level: 4, Stats: [5]byte{63, 15}}, false)
	if _, err := strengthOverflow.PlanPower("alice", 1000, func(int, int) int { return 1 }); err == nil {
		t.Fatal("strength overflow accepted")
	}
	thacoOverflow := powerAccuracyState(LegacyMonster{Class: AccurateThiefClass, Level: 4, Stats: [5]byte{10, 15}, Thaco: 128}, true)
	if _, err := thacoOverflow.PlanAccurate("alice", 1000, func(int, int) int { return 1 }); err == nil {
		t.Fatal("signed thaco underflow accepted")
	}
	badRoll := powerAccuracyState(LegacyMonster{Class: PowerClass, Level: 4, Stats: [5]byte{10, 15}}, false)
	if _, err := badRoll.PlanPower("alice", 1000, func(int, int) int { return 0 }); err == nil {
		t.Fatal("invalid random value accepted")
	}
	negative := powerAccuracyState(LegacyMonster{Class: PowerClass, Level: 4, Stats: [5]byte{10, 15}}, false)
	if _, err := negative.PlanPower("alice", -1, func(int, int) int { return 1 }); err == nil {
		t.Fatal("negative clock accepted")
	}
}
