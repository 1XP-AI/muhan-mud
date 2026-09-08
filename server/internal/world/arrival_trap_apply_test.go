package world

import (
	"reflect"
	"testing"
)

type alarmSpawnCatalog struct{}

func (alarmSpawnCatalog) Monster(int16) (LegacyMonster, error) {
	return LegacyMonster{Name: "guard", Type: 1}, nil
}

func (alarmSpawnCatalog) Object(int16) (LegacyObject, error) {
	return LegacyObject{Name: "검"}, nil
}

func TestDirectionalStepAppliesArrivalDartAfterSuccessfulTransfer(t *testing.T) {
	s, in := canonicalTransferFixture()
	r := s.Rooms[2]
	r.Resource.Trap = TrapDart
	s.Rooms[2] = r
	actor := s.Players["a"]
	actor.Body.Stats[1] = 1
	s.Players["a"] = actor
	rolls := []int{100, 4}
	next, result, err := s.DirectionalStep(in, nil, func(low, high int) int {
		if low != 1 || (high != 100 && high != 10) {
			t.Fatalf("unexpected roll %d..%d", low, high)
		}
		value := rolls[0]
		rolls = rolls[1:]
		return value
	}, nil)
	if err != nil || result.Transfer.ArrivalTrap == nil || !result.Transfer.ArrivalTrap.Poisoned || next.Players["a"].Body.HPCurrent != 26 || next.Players["a"].Body.RoomID != 2 {
		t.Fatalf("next=%+v result=%+v err=%v", next, result, err)
	}
	if next.Players["a"].Body.Flags[24/8]&(1<<(24%8)) != 0 {
		t.Fatal("PPREPA survived arrival")
	}
	if s.Players["a"].Body.HPCurrent != actor.Body.HPCurrent {
		t.Fatal("input mutated")
	}
}

func TestDirectionalStepPitRelocatesBeforeNonlethalCompletion(t *testing.T) {
	s, in := canonicalTransferFixture()
	s.Rooms[3] = RoomState{Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 3}}}
	r := s.Rooms[2]
	r.Resource.Trap = TrapPit
	r.Resource.TrapExit = 3
	s.Rooms[2] = r
	actor := s.Players["a"]
	actor.Body.Stats[1] = 1
	s.Players["a"] = actor
	rolls := []int{100, 2}
	next, result, err := s.DirectionalStep(in, nil, func(low, high int) int {
		if low != 1 || (high != 100 && high != 15) {
			t.Fatalf("unexpected roll %d..%d", low, high)
		}
		value := rolls[0]
		rolls = rolls[1:]
		return value
	}, nil)
	if err != nil || result.Death != nil || result.Transfer.ArrivalTrap == nil || next.Players["a"].Body.RoomID != 3 || next.Players["a"].Body.HPCurrent != 28 {
		t.Fatalf("next=%+v result=%+v err=%v", next, result, err)
	}
	if len(next.Rooms[2].PlayerIDs) != 0 || len(next.Rooms[3].PlayerIDs) != 1 || next.Rooms[3].Resource.BeenHere != 1 {
		t.Fatalf("room membership after pit: %+v", next.Rooms)
	}
}

func TestDirectionalStepRejectsUnsupportedAlarmWithoutCandidate(t *testing.T) {
	s, in := canonicalTransferFixture()
	r := s.Rooms[2]
	r.Resource.Trap = TrapAlarm
	s.Rooms[2] = r
	actor := s.Players["a"]
	actor.Body.Stats[1] = 1
	s.Players["a"] = actor
	next, result, err := s.DirectionalStep(in, nil, func(low, high int) int { return 100 }, nil)
	if err == nil || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(result, DirectionalStepResult{}) {
		t.Fatalf("unsupported alarm leaked candidate: next=%+v result=%+v err=%v", next, result, err)
	}
	if s.Players["a"].Body.RoomID != 1 {
		t.Fatal("input mutated")
	}
}

func alarmStateFixture(t *testing.T, sourceHasPlayer bool) State {
	t.Helper()
	s, _ := npcMovementFixture(t)
	// The alarm source is a separate trapexit room, so the NPC chase phase for
	// the player's ordinary move has no opportunity to move this guard first.
	sourceRoom := s.Rooms[1]
	sourceRoom.NPCIDs = nil
	sourceRoom.PlayerIDs = nil
	s.Rooms[1] = sourceRoom
	s.Rooms[3] = RoomState{Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 3}, PermanentMonsters: [10]LegacyTimer{{Misc: 77, LastTime: 1, Interval: 10}}}, NPCIDs: []string{"guard-id"}}
	destination := s.Rooms[2]
	destination.Resource.Trap = TrapAlarm
	destination.Resource.TrapExit = 3
	destination.PlayerIDs = []string{"a"}
	s.Rooms[2] = destination
	actor := s.Players["a"]
	actor.Body.RoomID = 2
	s.Players["a"] = actor
	npc := s.NPCs["guard-id"]
	npc.Body.RoomID = 3
	npc.Body.Flags[npcPermanentFlag/8] |= 1 << (npcPermanentFlag % 8)
	npc.Enemies = []NPCEnemy{}
	npc.PermanentOrigin = &NPCPermanentOrigin{RoomID: 3, Slot: 0}
	s.NPCs["guard-id"] = npc
	s.ActiveNPCIDs = []string{}
	if sourceHasPlayer {
		s.Players["b"] = PlayerState{Body: LegacyMonster{Name: "ByGuard", Type: 0, RoomID: 3}, Online: true}
		r := s.Rooms[3]
		r.PlayerIDs = []string{"b"}
		s.Rooms[3] = r
	}
	return s
}

func TestApplyArrivalAlarmMovesPermanentNPCAndUpdatesOriginTimer(t *testing.T) {
	s := alarmStateFixture(t, false)
	effect := ArrivalTrapResult{Trap: TrapAlarm, Triggered: true, Alarm: true}
	next, result, err := s.ApplyArrivalAlarm("a", effect, 100)
	if err != nil || len(result.MovedNPCIDs) != 1 || result.MovedNPCIDs[0] != "guard-id" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	guard := next.NPCs["guard-id"]
	if guard.Body.RoomID != 2 || flag(guard.Body.Flags[:], npcPermanentFlag) || !flag(guard.Body.Flags[:], npcAggressiveFlag) {
		t.Fatalf("alarm guard=%+v", guard)
	}
	if next.Rooms[3].Resource.PermanentMonsters[0].LastTime != 100 || len(next.Rooms[3].NPCIDs) != 0 || len(next.Rooms[2].NPCIDs) != 1 {
		t.Fatalf("alarm rooms=%+v", next.Rooms)
	}
	if len(next.ActiveNPCIDs) != 1 || next.ActiveNPCIDs[0] != "guard-id" {
		t.Fatalf("alarm active order=%v", next.ActiveNPCIDs)
	}
	if len(s.Rooms[3].NPCIDs) != 1 || s.NPCs["guard-id"].Body.RoomID != 3 {
		t.Fatal("alarm mutated source")
	}
}

func TestApplyArrivalAlarmSpawnsDuePermanentNPCBeforeMoving(t *testing.T) {
	s := alarmStateFixture(t, false)
	source := s.Rooms[3]
	source.NPCIDs = nil
	s.Rooms[3] = source
	delete(s.NPCs, "guard-id")
	allocations := 0
	rolls := 0
	next, result, err := s.ApplyArrivalAlarmWithCatalog("a", ArrivalTrapResult{Trap: TrapAlarm, Triggered: true, Alarm: true}, 100, alarmSpawnCatalog{}, func(low, high int) int {
		rolls++
		if rolls == 1 && (low != 1 || high != 100) {
			t.Fatalf("unexpected alarm spawn chance roll %d..%d", low, high)
		}
		if rolls == 2 && (low != 0 || high != 9) {
			t.Fatalf("unexpected alarm spawn slot roll %d..%d", low, high)
		}
		return low
	}, func() (string, error) {
		allocations++
		return "guard-spawned", nil
	})
	if err != nil || allocations != 1 || len(result.MovedNPCIDs) != 1 || result.MovedNPCIDs[0] != "guard-spawned" {
		t.Fatalf("spawn result=%+v allocations=%d err=%v", result, allocations, err)
	}
	guard := next.NPCs["guard-spawned"]
	if guard.Body.RoomID != 2 || guard.PermanentOrigin == nil || *guard.PermanentOrigin != (NPCPermanentOrigin{RoomID: 3, Slot: 0}) || flag(guard.Body.Flags[:], npcPermanentFlag) || !flag(guard.Body.Flags[:], npcAggressiveFlag) {
		t.Fatalf("spawned alarm guard=%+v", guard)
	}
	if next.Rooms[3].Resource.PermanentMonsters[0].LastTime != 100 || len(next.Rooms[3].NPCIDs) != 0 || !reflect.DeepEqual(next.Rooms[2].NPCIDs, []string{"guard-spawned"}) || !reflect.DeepEqual(next.ActiveNPCIDs, []string{"guard-spawned"}) {
		t.Fatalf("spawned alarm rooms=%+v active=%v", next.Rooms, next.ActiveNPCIDs)
	}
}

func TestApplyArrivalAlarmDoesNotAddActiveWhenTrapexitHasPlayer(t *testing.T) {
	s := alarmStateFixture(t, true)
	next, result, err := s.ApplyArrivalAlarm("a", ArrivalTrapResult{Trap: TrapAlarm, Triggered: true, Alarm: true}, 100)
	if err != nil || len(result.MovedNPCIDs) != 1 || len(next.ActiveNPCIDs) != 0 {
		t.Fatalf("result=%+v active=%v err=%v", result, next.ActiveNPCIDs, err)
	}
}

func TestApplyArrivalAlarmRejectsMissingDueSpawnWithoutCandidate(t *testing.T) {
	s := alarmStateFixture(t, false)
	sourceRoom := s.Rooms[3]
	sourceRoom.NPCIDs = nil
	s.Rooms[3] = sourceRoom
	delete(s.NPCs, "guard-id")
	next, result, err := s.ApplyArrivalAlarm("a", ArrivalTrapResult{Trap: TrapAlarm, Triggered: true, Alarm: true}, 100)
	if err == nil || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(result, ArrivalAlarmResult{}) || len(s.Rooms[3].NPCIDs) != 0 {
		t.Fatalf("missing spawn leaked candidate: next=%+v result=%+v err=%v", next, result, err)
	}
}

func TestApplyArrivalAlarmRejectsUnknownActiveOrderWithoutCandidate(t *testing.T) {
	s := alarmStateFixture(t, false)
	s.ActiveNPCIDs = nil
	next, result, err := s.ApplyArrivalAlarm("a", ArrivalTrapResult{Trap: TrapAlarm, Triggered: true, Alarm: true}, 100)
	if err == nil || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(result, ArrivalAlarmResult{}) {
		t.Fatalf("unknown active order leaked candidate: next=%+v result=%+v err=%v", next, result, err)
	}
}

func TestDirectionalStepRunsAlarmAfterPlayerMovement(t *testing.T) {
	s, in := npcMovementFixture(t)
	sourceRoom := s.Rooms[1]
	sourceRoom.NPCIDs = nil
	s.Rooms[1] = sourceRoom
	s.Rooms[3] = RoomState{Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 3}, PermanentMonsters: [10]LegacyTimer{{Misc: 77, LastTime: 1, Interval: 10}}}, NPCIDs: []string{"guard-id"}}
	destination := s.Rooms[2]
	destination.Resource.Trap = TrapAlarm
	destination.Resource.TrapExit = 3
	s.Rooms[2] = destination
	npc := s.NPCs["guard-id"]
	npc.Body.RoomID = 3
	npc.Body.Flags[npcPermanentFlag/8] |= 1 << (npcPermanentFlag % 8)
	npc.Enemies = []NPCEnemy{}
	npc.PermanentOrigin = &NPCPermanentOrigin{RoomID: 3, Slot: 0}
	s.NPCs["guard-id"] = npc
	s.ActiveNPCIDs = []string{}
	actor := s.Players["a"]
	actor.Body.Stats[1] = 1
	s.Players["a"] = actor
	in.Movement.Now = 100
	next, result, err := s.DirectionalStep(in, nil, func(low, high int) int {
		if low != 1 || high != 100 {
			t.Fatalf("unexpected alarm roll %d..%d", low, high)
		}
		return 100
	}, nil)
	if err != nil || result.Alarm == nil || len(result.Alarm.MovedNPCIDs) != 1 || next.NPCs["guard-id"].Body.RoomID != 2 {
		t.Fatalf("result=%+v next=%+v err=%v", result, next, err)
	}
}
