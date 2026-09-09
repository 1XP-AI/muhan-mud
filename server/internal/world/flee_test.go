package world

import (
	"reflect"
	"strings"
	"testing"
)

func fleeStateFixture() State {
	actor := LegacyMonster{
		Name: "Alice", Type: 0, Class: 4, Level: 10, Stats: [5]byte{10, 10, 10, 10, 10},
		HPMax: 100, HPCurrent: 100, RoomID: 1,
	}
	guard := LegacyMonster{Name: "Goblin", Type: 1, Class: 4, Level: 5, RoomID: 1}
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Exits: []LegacyExit{{Name: "북", Destination: 2}}}}, PlayerIDs: []string{"alice"}, NPCIDs: []string{"goblin"}},
			2: {Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 2}}},
		},
		Players:      map[string]PlayerState{"alice": {Body: actor, Online: true}},
		NPCs:         map[string]NPCState{"goblin": {Body: guard, Enemies: []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "alice"}, Damage: 0}}}},
		ActiveNPCIDs: []string{"goblin"},
	}
}

func TestPlanAndApplyFleeSuccessUsesEligibleExitAndMovesAtomically(t *testing.T) {
	before := fleeStateFixture()
	calls := 0
	proposal, err := before.PlanFlee("alice", 100, 12, func(low, high int) int {
		calls++
		if low != 1 || high != 100 {
			t.Fatalf("roll range=%d..%d", low, high)
		}
		return 1
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || proposal.Chance != 65 || proposal.Cooldown || proposal.NoCombat || !proposal.Moved || proposal.ExitName != "북" || !proposal.Broadcast || proposal.Transfer.ArrivalTrap == nil || proposal.Transfer.ArrivalTrap.Trap != 0 {
		t.Fatalf("proposal=%+v calls=%d", proposal, calls)
	}
	if before.Players["alice"].Body.RoomID != 1 {
		t.Fatal("planning mutated actor location")
	}
	next, result, err := before.ApplyFlee(proposal)
	if err != nil {
		t.Fatal(err)
	}
	nextActor := next.Players["alice"].Body
	if nextActor.RoomID != 2 || flag(nextActor.Flags[:], fleeHiddenFlag) || next.Rooms[1].Resource.Track != "북" {
		t.Fatalf("next actor=%+v source=%+v", next.Players["alice"], next.Rooms[1].Resource)
	}
	if next.Rooms[1].PlayerIDs != nil || !reflect.DeepEqual(next.ActiveNPCIDs, []string{}) {
		t.Fatalf("source membership/active NPCs=%+v/%+v", next.Rooms[1].PlayerIDs, next.ActiveNPCIDs)
	}
	if !strings.Contains(result.Response, "줄행랑") || !result.Moved || result.ArrivalTrap == nil || result.ArrivalTrap.Trap != 0 {
		t.Fatalf("result=%+v", result)
	}
	event, ok, err := next.RoomFleeEvent(result)
	if err != nil || !ok || event.SourceRoomID != 1 || event.DestinationRoomID != 2 || event.DestinationText == "" || event.ExcludeActorID != "alice" {
		t.Fatalf("event=%+v ok=%v err=%v", event, ok, err)
	}
}

func TestFleeCooldownAndNoCombatDoNotConsumeRandomness(t *testing.T) {
	cooldown := fleeStateFixture()
	actor := cooldown.Players["alice"]
	actor.Body.Timers[fleeAttackTimer] = LegacyTimer{LastTime: 110, Interval: 10}
	cooldown.Players["alice"] = actor
	p, err := cooldown.PlanFlee("alice", 105, 12, func(int, int) int { t.Fatal("cooldown consumed random"); return 1 }, nil, nil)
	if err != nil || !p.Cooldown || p.Broadcast || p.WaitSeconds != 5 || p.Response != "5초동안 기다리세요.\r\n" {
		t.Fatalf("proposal=%+v err=%v", p, err)
	}
	next, result, err := cooldown.ApplyFlee(p)
	if err != nil || !reflect.DeepEqual(next, cooldown) || result.Broadcast {
		t.Fatalf("next/result=%+v/%+v err=%v", next, result, err)
	}

	peaceful := fleeStateFixture()
	peaceful.NPCs = nil
	peaceful.ActiveNPCIDs = nil
	peaceful.Rooms[1] = RoomState{Resource: peaceful.Rooms[1].Resource, PlayerIDs: []string{"alice"}}
	p, err = peaceful.PlanFlee("alice", 100, 12, func(int, int) int { t.Fatal("peaceful flee consumed random"); return 1 }, nil, nil)
	if err != nil || !p.NoCombat || p.Broadcast || p.Response != "누구에게서 도망가시려구요?\r\n" {
		t.Fatalf("peaceful proposal=%+v err=%v", p, err)
	}
}

func TestFleePreservesFilterOrderAndSkipsUnsupportedExits(t *testing.T) {
	s := fleeStateFixture()
	room := s.Rooms[1]
	room.Resource.Exits = []LegacyExit{
		{Name: "닫힘", Destination: 2, Flags: [4]byte{1 << (fleeClosedExitFlag % 8)}},
		{Name: "비밀", Destination: 2, Flags: [4]byte{1}},
		{Name: "보이지않음", Destination: 2, Flags: [4]byte{1 << fleeInvisibleExitFlag}},
		{Name: "낮길", Destination: 2, Flags: [4]byte{0, 0, 1 << (fleeDayOnlyExitFlag % 8)}},
		{Name: "북", Destination: 2},
	}
	s.Rooms[1] = room
	calls := 0
	p, err := s.PlanFlee("alice", 100, 22, func(int, int) int { calls++; return 1 }, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || p.ExitIndex != 4 || p.ExitName != "북" {
		t.Fatalf("calls=%d proposal=%+v", calls, p)
	}

	actor := s.Players["alice"]
	actor.Body.Flags[fleeDetectInvisibleFlag/8] |= 1 << (fleeDetectInvisibleFlag % 8)
	s.Players["alice"] = actor
	room = s.Rooms[1]
	room.Resource.Exits[2].Flags = [4]byte{1 << fleeInvisibleExitFlag}
	s.Rooms[1] = room
	if p, err := s.PlanFlee("alice", 100, 12, func(int, int) int { return 1 }, nil, nil); err != nil || p.ExitName != "보이지않음" {
		t.Fatalf("detect invisible result=%+v err=%v", p, err)
	}
}

func TestFleeAppliesPaladinLossBeforeDestinationDenial(t *testing.T) {
	s := fleeStateFixture()
	actor := s.Players["alice"]
	actor.Body.Class = fleePaladinClass
	actor.Body.Level = 21
	actor.Body.Experience = 1000
	actor.Body.Proficiency = [5]int32{1024, 1024, 1024, 1024, 1024}
	actor.Body.Stats[1] = 10
	s.Players["alice"] = actor
	destination := s.Rooms[2]
	destination.Resource.LowLevel = 22
	s.Rooms[2] = destination
	p, err := s.PlanFlee("alice", 100, 12, func(int, int) int { return 1 }, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Moved || !p.PaladinPenalty || p.ExperienceLoss != 60 || !strings.Contains(p.Response, "60") {
		t.Fatalf("proposal=%+v", p)
	}
	next, _, err := s.ApplyFlee(p)
	if err != nil || next.Players["alice"].Body.Experience != 940 || next.Players["alice"].Body.RoomID != 1 {
		t.Fatalf("next=%+v err=%v", next.Players["alice"], err)
	}
	event, ok, err := next.RoomFleeEvent(FleeResult{Response: p.Response, Broadcast: true, Moved: false, ActorID: "alice", ActorName: "Alice", SourceRoomID: 1, DestinationRoomID: 2, ExitName: "북"})
	if err != nil || !ok || event.DestinationText == "" {
		t.Fatalf("event=%+v ok=%v err=%v", event, ok, err)
	}
}

func TestFleeAppliesArrivalDartAndRejectsStaleProposal(t *testing.T) {
	s := fleeStateFixture()
	destination := s.Rooms[2]
	destination.Resource.Trap = TrapDart
	s.Rooms[2] = destination
	rolls := []int{1, 100, 1}
	calls := 0
	p, err := s.PlanFlee("alice", 100, 12, func(low, high int) int {
		calls++
		if len(rolls) == 0 {
			t.Fatalf("unexpected random call %d..%d", low, high)
		}
		value := rolls[0]
		rolls = rolls[1:]
		return value
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 || p.Transfer.ArrivalTrap == nil || !p.Transfer.ArrivalTrap.Triggered || !p.Transfer.ArrivalTrap.Poisoned || p.Transfer.ArrivalTrap.Dead {
		t.Fatalf("calls=%d proposal=%+v", calls, p)
	}
	next, result, err := s.ApplyFlee(p)
	if err != nil {
		t.Fatal(err)
	}
	if next.Players["alice"].Body.RoomID != 2 || next.Players["alice"].Body.HPCurrent != 99 || result.ArrivalTrap == nil || result.Death != nil {
		t.Fatalf("next=%+v result=%+v", next.Players["alice"], result)
	}

	changed := s.Players["alice"]
	changed.Body.Stats[1]++
	s.Players["alice"] = changed
	if _, _, err := s.ApplyFlee(p); err == nil {
		t.Fatal("stale flee proposal accepted")
	}
}

func TestFleeAppliesArrivalPitBeforeFinalScene(t *testing.T) {
	s := fleeStateFixture()
	s.Rooms[3] = RoomState{Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 3}}}
	destination := s.Rooms[2]
	destination.Resource.Trap = TrapPit
	destination.Resource.TrapExit = 3
	s.Rooms[2] = destination
	rolls := []int{1, 100, 2}
	calls := 0
	p, err := s.PlanFlee("alice", 100, 12, func(low, high int) int {
		calls++
		if len(rolls) == 0 {
			t.Fatalf("unexpected random call %d..%d", low, high)
		}
		value := rolls[0]
		rolls = rolls[1:]
		return value
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 || p.Transfer.ArrivalTrap == nil || !p.Transfer.ArrivalTrap.Relocate || p.Death != nil {
		t.Fatalf("calls=%d proposal=%+v", calls, p)
	}
	next, result, err := s.ApplyFlee(p)
	if err != nil {
		t.Fatal(err)
	}
	if next.Players["alice"].Body.RoomID != 3 || next.Players["alice"].Body.HPCurrent != 98 || result.DestinationRoomID != 3 {
		t.Fatalf("actor=%+v result=%+v", next.Players["alice"], result)
	}
	if len(next.Rooms[2].PlayerIDs) != 0 || !reflect.DeepEqual(next.Rooms[3].PlayerIDs, []string{"alice"}) {
		t.Fatalf("room membership=%+v/%+v", next.Rooms[2].PlayerIDs, next.Rooms[3].PlayerIDs)
	}
	if !strings.Contains(result.Response, "줄행랑") {
		t.Fatalf("response=%q", result.Response)
	}
}

func TestFleeAppliesArrivalAlarmWithCanonicalReducer(t *testing.T) {
	s := fleeStateFixture()
	destination := s.Rooms[2]
	destination.Resource.Trap = TrapAlarm
	destination.Resource.TrapExit = 1
	s.Rooms[2] = destination
	rolls := []int{1, 100}
	calls := 0
	p, err := s.PlanFlee("alice", 100, 12, func(low, high int) int {
		calls++
		if len(rolls) == 0 {
			t.Fatalf("unexpected random call %d..%d", low, high)
		}
		value := rolls[0]
		rolls = rolls[1:]
		return value
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || p.Transfer.ArrivalTrap == nil || !p.Transfer.ArrivalTrap.Alarm || p.Alarm == nil || p.Death != nil {
		t.Fatalf("calls=%d proposal=%+v", calls, p)
	}
	next, result, err := s.ApplyFlee(p)
	if err != nil {
		t.Fatal(err)
	}
	if next.Players["alice"].Body.RoomID != 2 || result.Alarm == nil || !strings.Contains(result.Response, "경보장치가 울립니다!") {
		t.Fatalf("next=%+v result=%+v", next.Players["alice"], result)
	}
}
