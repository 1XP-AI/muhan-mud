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
	if err != nil || result.Transfer.ArrivalTrap == nil || !result.Transfer.ArrivalTrap.Poisoned || next.Players["a"].Body.HPCurrent != 26 || next.Players["a"].Body.RoomID != 2 || result.ArrivalTrapEvent == nil || result.ArrivalTrapEvent.RoomID != 2 || result.ArrivalTrapEvent.RoomText != "\nAlice이 숨겨진 독화살에 맞았습니다.\r\n" {
		t.Fatalf("next=%+v result=%+v err=%v", next, result, err)
	}
	if next.Players["a"].Body.Flags[24/8]&(1<<(24%8)) != 0 {
		t.Fatal("PPREPA survived arrival")
	}
	if s.Players["a"].Body.HPCurrent != actor.Body.HPCurrent {
		t.Fatal("input mutated")
	}
}

func TestDirectionalStepAvoidedAndTraplessArrivalHaveNoTrapProjection(t *testing.T) {
	s, in := canonicalTransferFixture()
	destination := s.Rooms[2]
	destination.Resource.Trap = TrapDart
	s.Rooms[2] = destination
	actor := s.Players["a"]
	actor.Body.Stats[1] = 10
	s.Players["a"] = actor
	calls := 0
	next, result, err := s.DirectionalStep(in, nil, func(low, high int) int {
		calls++
		if low != 1 || high != 100 {
			t.Fatalf("avoided trap consumed unexpected roll %d..%d", low, high)
		}
		return 1
	}, nil)
	if err != nil || result.Transfer.ArrivalTrap == nil || !result.Transfer.ArrivalTrap.Avoided || result.ArrivalTrapEvent != nil || calls != 1 || next.Players["a"].Body.RoomID != 2 {
		t.Fatalf("avoided result=%+v next=%+v err=%v calls=%d", result, next, err, calls)
	}

	s, in = canonicalTransferFixture()
	next, result, err = s.DirectionalStep(in, nil, nil, nil)
	if err != nil || result.Transfer.ArrivalTrap == nil || result.Transfer.ArrivalTrap.Triggered || result.ArrivalTrapEvent != nil || next.Players["a"].Body.RoomID != 2 {
		t.Fatalf("trapless result=%+v next=%+v err=%v", result, next, err)
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
	if err != nil || result.Death != nil || result.Transfer.ArrivalTrap == nil || next.Players["a"].Body.RoomID != 3 || next.Players["a"].Body.HPCurrent != 28 || result.ArrivalTrapEvent == nil || result.ArrivalTrapEvent.RoomID != 2 || result.ArrivalTrapEvent.ActorText != "당신은 구덩이에 빠졌습니다!\n당신은 2점의 피해를 입었습니다.\n" {
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

func trapMovementFixture(trap byte) (State, TransferInput) {
	s, in := canonicalTransferFixture()
	r := s.Rooms[2]
	r.Resource.Trap = trap
	s.Rooms[2] = r
	actor := s.Players["a"]
	actor.Body.Stats[1] = 1
	actor.Body.Stats[3] = 1
	actor.Body.HPMax = 30
	actor.Body.MPCurrent = 8
	actor.Body.MPMax = 10
	actor.Body.Class = 4
	actor.Body.Level = 1
	s.Players["a"] = actor
	return s, in
}

func arrivedTrapFixture(trap byte) State {
	s, _ := trapMovementFixture(trap)
	r1 := s.Rooms[1]
	r1.PlayerIDs = nil
	s.Rooms[1] = r1
	r2 := s.Rooms[2]
	r2.PlayerIDs = []string{"a"}
	s.Rooms[2] = r2
	actor := s.Players["a"]
	actor.Body.RoomID = 2
	s.Players["a"] = actor
	return s
}

func trapTriggerRoll(t *testing.T, values ...int) func(int, int) int {
	t.Helper()
	return func(low, high int) int {
		t.Helper()
		if len(values) == 0 {
			t.Fatalf("unexpected extra trap roll %d..%d", low, high)
		}
		if low != 1 {
			t.Fatalf("unexpected roll %d..%d", low, high)
		}
		value := values[0]
		values = values[1:]
		return value
	}
}

func TestDirectionalStepAppliesArrivalBlockDamage(t *testing.T) {
	s, in := trapMovementFixture(TrapBlock)
	next, result, err := s.DirectionalStep(in, nil, trapTriggerRoll(t, 100), nil)
	if err != nil || result.Death != nil || result.Transfer.ArrivalTrap == nil || !result.Transfer.ArrivalTrap.Triggered || result.Transfer.ArrivalTrap.Damage != 10 || next.Players["a"].Body.HPCurrent != 20 || next.Players["a"].Body.RoomID != 2 {
		t.Fatalf("next=%+v result=%+v err=%v", next.Players["a"].Body, result, err)
	}
	if s.Players["a"].Body.HPCurrent != 30 {
		t.Fatal("input mutated")
	}
}

func TestDirectionalStepAppliesArrivalMPDamage(t *testing.T) {
	s, in := trapMovementFixture(TrapMPDam)
	next, result, err := s.DirectionalStep(in, nil, trapTriggerRoll(t, 100, 3), nil)
	if err != nil || result.Transfer.ArrivalTrap == nil || result.Transfer.ArrivalTrap.MPDamage != 5 || result.Transfer.ArrivalTrap.Damage != 3 || next.Players["a"].Body.MPCurrent != 3 || next.Players["a"].Body.HPCurrent != 27 || next.Players["a"].Body.RoomID != 2 {
		t.Fatalf("next=%+v result=%+v err=%v", next.Players["a"].Body, result, err)
	}
}

func TestDirectionalStepAppliesArrivalSpellLoss(t *testing.T) {
	s, in := trapMovementFixture(TrapRMSpl)
	actor := s.Players["a"]
	for _, index := range trapSpellTimers {
		actor.Body.Timers[index] = LegacyTimer{Interval: 99, LastTime: 7}
	}
	actor.Body.Timers[3] = LegacyTimer{Interval: 11, LastTime: 4}
	s.Players["a"] = actor
	next, result, err := s.DirectionalStep(in, nil, trapTriggerRoll(t, 100), nil)
	if err != nil || result.Transfer.ArrivalTrap == nil || !result.Transfer.ArrivalTrap.ClearSpells {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	got := next.Players["a"].Body
	for _, index := range trapSpellTimers {
		if got.Timers[index].Interval != 0 || got.Timers[index].LastTime != 7 {
			t.Fatalf("spell timer %d=%+v", index, got.Timers[index])
		}
	}
	if got.Timers[3] != (LegacyTimer{Interval: 11, LastTime: 4}) {
		t.Fatalf("unrelated timer mutated: %+v", got.Timers[3])
	}
}

func TestDirectionalStepAppliesArrivalNakedItemLoss(t *testing.T) {
	s, in := trapMovementFixture(TrapNaked)
	actor := s.Players["a"]
	cursed := Item{Object: LegacyObject{Name: "저주반지", Armor: 5}}
	armor := Item{Object: LegacyObject{Name: "갑옷", Armor: 10}}
	pack := Item{Object: LegacyObject{Name: "가방"}}
	setObjectFlag(&cursed.Object.Flags, objectCursedFlag, true)
	setObjectFlag(&cursed.Object.Flags, objectWearsFlag, true)
	setObjectFlag(&armor.Object.Flags, objectWearsFlag, true)
	actor.Items = &ItemCollection{
		Items:     map[string]Item{"ring": cursed, "armor": armor, "pack": pack},
		Inventory: []string{"pack"},
		Ready:     [20]string{0: "armor", 8: "ring"},
	}
	s.Players["a"] = actor
	next, result, err := s.DirectionalStep(in, nil, trapTriggerRoll(t, 100), nil)
	if err != nil || result.Transfer.ArrivalTrap == nil || !result.Transfer.ArrivalTrap.LoseItems {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	got := next.Players["a"]
	if len(got.Items.Inventory) != 0 || got.Items.Ready[0] != "" || got.Items.Ready[8] != "ring" || len(got.Items.Items) != 1 || got.Body.HPCurrent != 30 || got.Body.RoomID != 2 {
		t.Fatalf("naked items=%+v body=%+v", got.Items, got.Body)
	}
	stats, err := got.Items.CombatStats(got.Body)
	if err != nil || got.Body.Armor != byte(stats.Armor) || got.Body.Thaco != byte(stats.Thaco) {
		t.Fatalf("armor/thaco not recomputed: body=%+v stats=%+v err=%v", got.Body, stats, err)
	}
	if len(s.Players["a"].Items.Inventory) != 1 || s.Players["a"].Items.Ready[0] != "armor" {
		t.Fatal("input mutated")
	}
	if next.Rooms[2].Items.Items["floor"].Object.Name != "검" {
		t.Fatal("floor items dissolved")
	}
}

func TestApplyArrivalTrapAppliesPlannedBlockMPSpellAndNaked(t *testing.T) {
	s := arrivedTrapFixture(TrapBlock)
	effect, err := PlanArrivalTrap(s.Rooms[2].Resource, ArrivalTrapInput{HP: 30, HPMax: 30, MP: 8, MPMax: 10, Dexterity: 1}, trapTriggerRoll(t, 100))
	if err != nil {
		t.Fatal(err)
	}
	next, err := s.ApplyArrivalTrap("a", effect)
	if err != nil || next.Players["a"].Body.HPCurrent != 20 || next.Players["a"].Body.MPCurrent != 8 {
		t.Fatalf("block apply next=%+v err=%v", next.Players["a"].Body, err)
	}

	s = arrivedTrapFixture(TrapMPDam)
	effect, err = PlanArrivalTrap(s.Rooms[2].Resource, ArrivalTrapInput{HP: 30, HPMax: 30, MP: 8, MPMax: 10, Intelligence: 1}, trapTriggerRoll(t, 100, 4))
	if err != nil {
		t.Fatal(err)
	}
	next, err = s.ApplyArrivalTrap("a", effect)
	if err != nil || next.Players["a"].Body.HPCurrent != 26 || next.Players["a"].Body.MPCurrent != 3 {
		t.Fatalf("mp apply next=%+v err=%v", next.Players["a"].Body, err)
	}

	s = arrivedTrapFixture(TrapRMSpl)
	actor := s.Players["a"]
	actor.Body.Timers[1] = LegacyTimer{Interval: 40}
	s.Players["a"] = actor
	effect, err = PlanArrivalTrap(s.Rooms[2].Resource, ArrivalTrapInput{HP: 30, HPMax: 30, MP: 8, MPMax: 10, Intelligence: 1}, trapTriggerRoll(t, 100))
	if err != nil {
		t.Fatal(err)
	}
	next, err = s.ApplyArrivalTrap("a", effect)
	if err != nil || next.Players["a"].Body.Timers[1].Interval != 0 || next.Players["a"].Body.HPCurrent != 30 {
		t.Fatalf("spell apply next=%+v err=%v", next.Players["a"].Body, err)
	}

	s = arrivedTrapFixture(TrapNaked)
	actor = s.Players["a"]
	actor.Items = &ItemCollection{Items: map[string]Item{"pack": {Object: LegacyObject{Name: "가방"}}}, Inventory: []string{"pack"}}
	s.Players["a"] = actor
	effect, err = PlanArrivalTrap(s.Rooms[2].Resource, ArrivalTrapInput{HP: 30, HPMax: 30, Dexterity: 1}, trapTriggerRoll(t, 100))
	if err != nil {
		t.Fatal(err)
	}
	next, err = s.ApplyArrivalTrap("a", effect)
	if err != nil || len(next.Players["a"].Items.Inventory) != 0 || len(next.Players["a"].Items.Items) != 0 || next.Players["a"].Body.HPCurrent != 30 {
		t.Fatalf("naked apply items=%+v err=%v", next.Players["a"].Items, err)
	}
}

func TestApplyArrivalTrapSameCommandDoesNotRerollOrRestack(t *testing.T) {
	for _, trap := range []byte{TrapBlock, TrapMPDam, TrapRMSpl, TrapNaked} {
		s := arrivedTrapFixture(trap)
		actor := s.Players["a"]
		actor.Body.Timers[2] = LegacyTimer{Interval: 15, LastTime: 3}
		actor.Items = &ItemCollection{Items: map[string]Item{"pack": {Object: LegacyObject{Name: "가방"}}}, Inventory: []string{"pack"}}
		s.Players["a"] = actor
		rolls := []int{100, 2}
		calls := 0
		effect, err := PlanArrivalTrap(s.Rooms[2].Resource, ArrivalTrapInput{
			HP: 30, HPMax: 30, MP: 8, MPMax: 10, Dexterity: 1, Intelligence: 1,
		}, func(low, high int) int {
			calls++
			if low != 1 || len(rolls) == 0 {
				t.Fatalf("trap %d unexpected roll %d..%d", trap, low, high)
			}
			value := rolls[0]
			rolls = rolls[1:]
			return value
		})
		if err != nil {
			t.Fatalf("trap %d plan err=%v", trap, err)
		}
		plannedCalls := calls
		first, err := s.ApplyArrivalTrap("a", effect)
		if err != nil || calls != plannedCalls {
			t.Fatalf("trap %d apply rerolled: calls=%d planned=%d err=%v", trap, calls, plannedCalls, err)
		}
		replay, err := first.ApplyArrivalTrap("a", effect)
		if err != nil || calls != plannedCalls {
			t.Fatalf("trap %d replay rerolled: calls=%d err=%v", trap, calls, err)
		}
		a, b := first.Players["a"].Body, replay.Players["a"].Body
		if a.HPCurrent != b.HPCurrent || a.MPCurrent != b.MPCurrent || a.Timers[2].Interval != b.Timers[2].Interval {
			t.Fatalf("trap %d replay restacked vitals first=%+v replay=%+v", trap, a, b)
		}
		if trap == TrapNaked && (len(replay.Players["a"].Items.Inventory) != 0 || len(first.Players["a"].Items.Items) != len(replay.Players["a"].Items.Items)) {
			t.Fatalf("trap naked replay restacked items first=%+v replay=%+v", first.Players["a"].Items, replay.Players["a"].Items)
		}
		if trap == TrapBlock && (a.HPCurrent != 20 || b.HPCurrent != 20) {
			t.Fatalf("block replay hp=%d/%d", a.HPCurrent, b.HPCurrent)
		}
		if trap == TrapMPDam && (a.HPCurrent != 28 || a.MPCurrent != 3 || b.HPCurrent != 28) {
			t.Fatalf("mpdam replay body=%+v", a)
		}
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
	if err != nil || result.Alarm == nil || len(result.Alarm.MovedNPCIDs) != 1 || next.NPCs["guard-id"].Body.RoomID != 2 || result.ArrivalTrapEvent == nil || result.ArrivalTrapEvent.Trap != TrapAlarm {
		t.Fatalf("result=%+v next=%+v err=%v", result, next, err)
	}
}
