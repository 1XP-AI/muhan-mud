package world

import (
	"reflect"
	"strings"
	"testing"
)

func rangerPrayStateFixture(class, level, dex, piety byte) State {
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{1: {
			Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}},
			PlayerIDs: []string{"alice"},
		}},
		Players: map[string]PlayerState{
			"alice": {Body: LegacyMonster{
				Name: "Alice", Type: 0, RoomID: 1, Class: class, Level: level,
				Stats: [5]byte{10, dex, 10, 10, piety},
			}, Online: true},
		},
	}
}

func TestRangerPrayChanceAndSourceSlots(t *testing.T) {
	haste, err := HasteChance(LegacyMonster{Class: RangerClass, Level: 4, Stats: [5]byte{10, 15}})
	if err != nil || haste != 21 {
		t.Fatalf("haste chance=%d err=%v", haste, err)
	}
	pray, err := PrayChance(LegacyMonster{Class: ClericClass, Level: 4, Stats: [5]byte{10, 10, 10, 10, 15}})
	if err != nil || pray != 21 {
		t.Fatalf("pray chance=%d err=%v", pray, err)
	}
	if HasteFlag != 19 || HasteTimerIndex != 16 || PrayFlag != 22 || PrayTimerIndex != 19 {
		t.Fatalf("source slots changed: haste=%d/%d pray=%d/%d", HasteFlag, HasteTimerIndex, PrayFlag, PrayTimerIndex)
	}
	if _, err := HasteChance(LegacyMonster{Class: 13, Stats: [5]byte{10, 15}}); err == nil {
		t.Fatal("unknown class accepted")
	}
	if _, err := PrayChance(LegacyMonster{Class: ClericClass, Stats: [5]byte{10, 10, 10, 10, 64}}); err == nil {
		t.Fatal("out-of-range piety accepted")
	}
}

func TestPlanAndApplyHasteSuccessIsAtomic(t *testing.T) {
	before := rangerPrayStateFixture(RangerClass, 4, 15, 15)
	calls := 0
	proposal, err := before.PlanHaste("alice", 1000, func(low, high int) int {
		calls++
		if low != 1 || high != 100 {
			t.Fatalf("random range=%d..%d", low, high)
		}
		return 1
	})
	if err != nil || calls != 1 || !proposal.Authorized || !proposal.Attempted || !proposal.Succeeded || proposal.Chance != 21 || proposal.Interval != 120 || !proposal.Broadcast {
		t.Fatalf("proposal=%+v err=%v calls=%d", proposal, err, calls)
	}
	beforeActor := before.Players["alice"].Body
	if flag(beforeActor.Flags[:], uint(HasteFlag)) || beforeActor.Stats[1] != 15 {
		t.Fatal("planning mutated source")
	}
	next, result, err := before.ApplyHaste(proposal)
	if err != nil {
		t.Fatal(err)
	}
	actor := next.Players["alice"].Body
	if !result.Succeeded || !result.Broadcast || !result.Active || result.Event == nil || result.EventText == "" || !flag(actor.Flags[:], uint(HasteFlag)) || actor.Stats[1] != 30 || actor.Timers[HasteTimerIndex] != (LegacyTimer{LastTime: 1000, Interval: 120}) {
		t.Fatalf("next actor=%+v result=%+v", actor, result)
	}
	event, ok, err := next.RoomHasteEvent("alice", true)
	if err != nil || !ok || event.ExcludeActorID != "alice" || !strings.Contains(event.Text, "활보법을 사용하였습니다") {
		t.Fatalf("event=%+v ok=%v err=%v", event, ok, err)
	}
}

func TestPlanAndApplyPrayFailureOnlyWritesSourceCooldown(t *testing.T) {
	before := rangerPrayStateFixture(ClericClass, 4, 10, 15)
	actor := before.Players["alice"]
	actor.Body.Timers[PrayTimerIndex] = LegacyTimer{LastTime: 0, Interval: 7}
	before.Players["alice"] = actor
	proposal, err := before.PlanPray("alice", 1000, func(int, int) int { return 100 })
	if err != nil || proposal.Succeeded || !proposal.Attempted || !proposal.Broadcast || !proposal.TimerWrite {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	next, result, err := before.ApplyPray(proposal)
	if err != nil {
		t.Fatal(err)
	}
	nextActor := next.Players["alice"].Body
	beforeActor := before.Players["alice"].Body
	if result.Succeeded || !result.Attempted || !result.Broadcast || nextActor.Stats[4] != beforeActor.Stats[4] || flag(nextActor.Flags[:], uint(PrayFlag)) || nextActor.Timers[PrayTimerIndex] != (LegacyTimer{LastTime: 410, Interval: 7}) {
		t.Fatalf("before=%+v next=%+v result=%+v", beforeActor, nextActor, result)
	}
	if nextActor.Timers[PrayTimerIndex].Interval != beforeActor.Timers[PrayTimerIndex].Interval {
		t.Fatal("failed prayer changed interval")
	}
}

func TestRangerPrayUnauthorizedActiveCooldownAndInvalidValuesAreFailClosed(t *testing.T) {
	unauthorized := rangerPrayStateFixture(4, 4, 15, 15)
	before := unauthorized
	proposal, err := unauthorized.PlanHaste("alice", 1000, func(int, int) int { t.Fatal("unauthorized consumed RNG"); return 1 })
	if err != nil || proposal.Authorized || proposal.Response == "" {
		t.Fatalf("unauthorized proposal=%+v err=%v", proposal, err)
	}
	next, result, err := unauthorized.ApplyHaste(proposal)
	if err != nil || !reflect.DeepEqual(next, before) || result.Authorized || result.Broadcast {
		t.Fatalf("unauthorized next=%+v result=%+v err=%v", next, result, err)
	}

	active := rangerPrayStateFixture(RangerClass, 4, 15, 15)
	actor := active.Players["alice"]
	actor.Body.Flags[HasteFlag/8] |= 1 << (HasteFlag % 8)
	active.Players["alice"] = actor
	proposal, err = active.PlanHaste("alice", 1000, func(int, int) int { t.Fatal("active consumed RNG"); return 1 })
	if err != nil || !proposal.Active {
		t.Fatalf("active proposal=%+v err=%v", proposal, err)
	}
	next, result, err = active.ApplyHaste(proposal)
	if err != nil || !reflect.DeepEqual(next, active) || !result.Active || result.Broadcast {
		t.Fatalf("active next=%+v result=%+v err=%v", next, result, err)
	}

	cooldown := rangerPrayStateFixture(ClericClass, 4, 10, 15)
	actor = cooldown.Players["alice"]
	actor.Body.Timers[PrayTimerIndex] = LegacyTimer{LastTime: 900, Interval: 300}
	cooldown.Players["alice"] = actor
	proposal, err = cooldown.PlanPray("alice", 1000, func(int, int) int { t.Fatal("cooldown consumed RNG"); return 1 })
	if err != nil || !proposal.Cooldown || proposal.WaitSeconds != 500 {
		t.Fatalf("cooldown proposal=%+v err=%v", proposal, err)
	}
	next, result, err = cooldown.ApplyPray(proposal)
	if err != nil || !reflect.DeepEqual(next, cooldown) || !result.Cooldown || result.Broadcast {
		t.Fatalf("cooldown next=%+v result=%+v err=%v", next, result, err)
	}

	invalid := rangerPrayStateFixture(RangerClass, 4, 15, 15)
	actor = invalid.Players["alice"]
	actor.Body.Timers[HasteTimerIndex].LastTime = -1
	invalid.Players["alice"] = actor
	if _, err := invalid.PlanHaste("alice", 1000, func(int, int) int { t.Fatal("invalid timer consumed RNG"); return 1 }); err == nil {
		t.Fatal("negative timer accepted")
	}
	if _, err := rangerPrayStateFixture(RangerClass, 4, 64, 15).PlanHaste("alice", 1000, func(int, int) int { return 1 }); err == nil {
		t.Fatal("invalid dexterity accepted")
	}
	if _, err := rangerPrayStateFixture(RangerClass, 4, 15, 15).PlanHaste("alice", 1000, func(int, int) int { return 0 }); err == nil {
		t.Fatal("invalid random value accepted")
	}
}
