package world

import (
	"reflect"
	"strings"
	"testing"
)

func meditateStateFixture(class byte) State {
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{1: {
			Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}},
			PlayerIDs: []string{"alice"},
		}},
		Players: map[string]PlayerState{
			"alice": {Body: LegacyMonster{
				Name: "Alice", Type: 0, RoomID: 1, Class: class, Level: 4,
				Stats: [5]byte{10, 10, 10, 10, 15},
			}, Online: true},
		},
	}
}

func TestMeditateSourceSlotsChanceAndInterval(t *testing.T) {
	if MeditateFlag != 54 || MeditateTimerIndex != 39 {
		t.Fatalf("source slots changed: flag=%d timer=%d", MeditateFlag, MeditateTimerIndex)
	}
	body := LegacyMonster{Class: ClericClass, Level: 4, Stats: [5]byte{10, 10, 10, 10, 15}}
	chance, err := MeditateChance(body)
	if err != nil || chance != 21 {
		t.Fatalf("chance=%d err=%v", chance, err)
	}
	proposal, err := meditateStateFixture(ClericClass).PlanMeditate("alice", 1000, func(int, int) int { return 1 })
	if err != nil || proposal.Interval != 150 {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	if _, err := MeditateChance(LegacyMonster{Class: 13, Stats: [5]byte{10, 10, 10, 10, 15}}); err == nil {
		t.Fatal("unknown class accepted")
	}
	if _, err := MeditateChance(LegacyMonster{Class: ClericClass, Stats: [5]byte{10, 10, 10, 10, 64}}); err == nil {
		t.Fatal("out-of-range piety accepted")
	}
}

func TestPlanAndApplyMeditateSuccessUsesCanonicalOnlinePlayer(t *testing.T) {
	before := meditateStateFixture(ClericClass)
	calls := 0
	proposal, err := before.PlanMeditate("alice", 1000, func(low, high int) int {
		calls++
		if low != 1 || high != 100 {
			t.Fatalf("random range=%d..%d", low, high)
		}
		return 1
	})
	if err != nil || calls != 1 || !proposal.Authorized || !proposal.Attempted || !proposal.Succeeded || proposal.Chance != 21 || proposal.Interval != 150 {
		t.Fatalf("proposal=%+v err=%v calls=%d", proposal, err, calls)
	}
	beforeActor := before.Players["alice"].Body
	if beforeActor.Stats[3] != 10 || flag(beforeActor.Flags[:], MeditateFlag) {
		t.Fatal("planning mutated canonical player")
	}
	next, result, err := before.ApplyMeditate(proposal)
	if err != nil {
		t.Fatal(err)
	}
	actor := next.Players["alice"].Body
	if !result.Succeeded || !result.Changed || !result.Broadcast || !result.Active || result.Event == nil || actor.Stats[3] != 13 || !flag(actor.Flags[:], MeditateFlag) || actor.Timers[MeditateTimerIndex] != (LegacyTimer{LastTime: 1000, Interval: 150}) {
		t.Fatalf("actor=%+v result=%+v", actor, result)
	}
	if result.Event.ExcludeActorID != "alice" || !strings.Contains(result.Event.Text, "자리에 앉아 참선을 행합니다") {
		t.Fatalf("event=%+v", result.Event)
	}
	event, ok, err := next.RoomMeditateEvent("alice", true)
	if err != nil || !ok || event.Succeeded != result.Succeeded || event.Text != result.Event.Text {
		t.Fatalf("event=%+v ok=%v err=%v result=%+v", event, ok, err, result)
	}
}

func TestPlanAndApplyMeditateFailureOnlyWritesShortCooldown(t *testing.T) {
	before := meditateStateFixture(PaladinClass)
	actor := before.Players["alice"]
	actor.Body.Timers[MeditateTimerIndex] = LegacyTimer{LastTime: 0, Interval: 7}
	before.Players["alice"] = actor
	proposal, err := before.PlanMeditate("alice", 1000, func(int, int) int { return 100 })
	if err != nil || proposal.Succeeded || !proposal.Attempted || !proposal.Broadcast || !proposal.TimerWrite || proposal.FailureLastTime != 410 || proposal.Interval != 7 {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	next, result, err := before.ApplyMeditate(proposal)
	if err != nil {
		t.Fatal(err)
	}
	nextActor := next.Players["alice"].Body
	beforeActor := before.Players["alice"].Body
	if result.Succeeded || !result.Attempted || !result.Broadcast || nextActor.Stats[3] != beforeActor.Stats[3] || flag(nextActor.Flags[:], MeditateFlag) || nextActor.Timers[MeditateTimerIndex] != (LegacyTimer{LastTime: 410, Interval: 7}) {
		t.Fatalf("before=%+v next=%+v result=%+v", beforeActor, nextActor, result)
	}
}

func TestMeditateGatesOverflowAndStaleProposalFailClosed(t *testing.T) {
	unauthorized := meditateStateFixture(4) // FIGHTER
	proposal, err := unauthorized.PlanMeditate("alice", 1000, func(int, int) int { t.Fatal("unauthorized consumed RNG"); return 1 })
	if err != nil || proposal.Authorized || proposal.Response != meditateUnauthorizedResponse() {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	next, result, err := unauthorized.ApplyMeditate(proposal)
	if err != nil || !reflect.DeepEqual(next, unauthorized) || result.Authorized || result.Broadcast {
		t.Fatalf("next=%+v result=%+v err=%v", next, result, err)
	}

	active := meditateStateFixture(ClericClass)
	actor := active.Players["alice"]
	actor.Body.Flags[MeditateFlag/8] |= 1 << (MeditateFlag % 8)
	active.Players["alice"] = actor
	proposal, err = active.PlanMeditate("alice", 1000, func(int, int) int { t.Fatal("active consumed RNG"); return 1 })
	if err != nil || !proposal.Active || !proposal.AlreadyActive {
		t.Fatalf("active proposal=%+v err=%v", proposal, err)
	}
	next, result, err = active.ApplyMeditate(proposal)
	if err != nil || !reflect.DeepEqual(next, active) || !result.Active || result.Broadcast {
		t.Fatalf("active next=%+v result=%+v err=%v", next, result, err)
	}

	cooldown := meditateStateFixture(ClericClass)
	actor = cooldown.Players["alice"]
	actor.Body.Timers[MeditateTimerIndex] = LegacyTimer{LastTime: 900, Interval: 300}
	cooldown.Players["alice"] = actor
	proposal, err = cooldown.PlanMeditate("alice", 1000, func(int, int) int { t.Fatal("cooldown consumed RNG"); return 1 })
	if err != nil || !proposal.Cooldown || proposal.WaitSeconds != 600 {
		t.Fatalf("cooldown proposal=%+v err=%v", proposal, err)
	}
	next, result, err = cooldown.ApplyMeditate(proposal)
	if err != nil || !reflect.DeepEqual(next, cooldown) || !result.Cooldown || result.Broadcast {
		t.Fatalf("cooldown next=%+v result=%+v err=%v", next, result, err)
	}

	stale := meditateStateFixture(ClericClass)
	proposal, err = stale.PlanMeditate("alice", 1000, func(int, int) int { return 1 })
	if err != nil {
		t.Fatal(err)
	}
	actor = stale.Players["alice"]
	actor.Body.Stats[4]++
	stale.Players["alice"] = actor
	if _, _, err := stale.ApplyMeditate(proposal); err == nil {
		t.Fatal("stale actor proposal accepted")
	}

	for _, intelligence := range []byte{125, 255} {
		overflow := meditateStateFixture(ClericClass)
		actor = overflow.Players["alice"]
		actor.Body.Stats[3] = intelligence
		overflow.Players["alice"] = actor
		if _, err := overflow.PlanMeditate("alice", 1000, func(int, int) int { return 1 }); err == nil {
			t.Fatalf("intelligence overflow accepted: %d", intelligence)
		}
	}
	if _, err := meditateStateFixture(ClericClass).PlanMeditate("alice", 1000, func(int, int) int { return 0 }); err == nil {
		t.Fatal("invalid random result accepted")
	}
}
