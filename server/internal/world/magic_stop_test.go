package world

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
)

func magicStopTestState(class byte) State {
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}},
				PlayerIDs: []string{"actor"},
				NPCIDs:    []string{"wolf-1", "wolf-2"},
			},
			2: {Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 2}}},
		},
		Players: map[string]PlayerState{
			"actor": {
				Body: LegacyMonster{
					Name: "수호자", Type: 0, Class: class, Level: 20, RoomID: 1,
					Stats: [5]byte{10, 20, 10, 10, 10}, MPMax: 90, MPCurrent: 17,
					Timers: [45]LegacyTimer{{}, {}, {}, {Interval: 7}},
				},
				Online: true,
			},
		},
		NPCs: map[string]NPCState{
			"wolf-1": {Body: LegacyMonster{Name: "늑대", Keys: [3]string{"wolf", "", ""}, Type: 1, Level: 4, Class: 4, RoomID: 1, HPMax: 100, HPCurrent: 100, MPMax: 50, MPCurrent: 31}, Enemies: []NPCEnemy{}},
			"wolf-2": {Body: LegacyMonster{Name: "늑대", Keys: [3]string{"wolf", "", ""}, Type: 1, Level: 4, Class: 4, RoomID: 1, HPMax: 100, HPCurrent: 100, MPMax: 50, MPCurrent: 31}, Enemies: []NPCEnemy{}},
		},
		ActiveNPCIDs: []string{"wolf-1", "wolf-2"},
	}
}

func TestPlanMagicStopFailsClosedBeforeCombatContinuation(t *testing.T) {
	s := magicStopTestState(MagicStopRangerClass)
	npc := s.NPCs["wolf-2"]
	npc.Enemies = nil
	s.NPCs["wolf-2"] = npc
	before := s.clone()
	calls := 0
	_, err := s.PlanMagicStopWithOccurrence("actor", "늑대", 2, 1000, func(low, high int) int {
		calls++
		t.Fatalf("combat continuation consumed RNG: call=%d range=%d..%d", calls, low, high)
		return 1
	})
	if err == nil || !errors.Is(err, ErrMagicStopCombatSideEffectPending) {
		t.Fatalf("ordinary target was not fail-closed: err=%v", err)
	}
	if calls != 0 || !reflect.DeepEqual(s, before) {
		t.Fatalf("fail-closed plan mutated state or consumed RNG: calls=%d", calls)
	}
}

func TestMagicStopClassVisibilityCooldownAndOccurrenceAreFailClosed(t *testing.T) {
	unauthorized := magicStopTestState(4)
	p, err := unauthorized.PlanMagicStop("actor", "늑대", 1000, func(int, int) int {
		t.Fatal("unauthorized magic_stop consumed RNG")
		return 1
	})
	if err != nil || p.Authorized || !p.NoOp || p.TargetFound || p.Response == "" {
		t.Fatalf("unauthorized proposal=%+v err=%v", p, err)
	}
	next, result, err := unauthorized.ApplyMagicStop(p)
	if err != nil || result.Changed || !reflect.DeepEqual(next, unauthorized) {
		t.Fatalf("unauthorized apply result=%+v err=%v", result, err)
	}

	s := magicStopTestState(MagicStopRangerClass)
	npc := s.NPCs["wolf-1"]
	npc.Body.Flags[MagicStopTargetInvisibleFlag/8] |= 1 << (MagicStopTargetInvisibleFlag % 8)
	s.NPCs["wolf-1"] = npc
	npc = s.NPCs["wolf-2"]
	npc.Body.Flags[MagicStopTargetInvisibleFlag/8] |= 1 << (MagicStopTargetInvisibleFlag % 8)
	s.NPCs["wolf-2"] = npc
	actor := s.Players["actor"]
	actor.Body.Flags[MagicStopActorInvisibleFlag/8] |= 1 << (MagicStopActorInvisibleFlag % 8)
	s.Players["actor"] = actor
	p, err = s.PlanMagicStop("actor", "늑대", 1000, func(int, int) int {
		t.Fatal("invisible target should not consume RNG")
		return 1
	})
	if err != nil || !p.NoOp || p.TargetFound || p.ClearInvisible || p.Response != "그런 괴물은 존재하지 않습니다.\n" {
		t.Fatalf("hidden target proposal=%+v err=%v", p, err)
	}
	if next, result, err := s.ApplyMagicStop(p); err != nil || result.Changed || !reflect.DeepEqual(next, s) {
		t.Fatalf("hidden target apply result=%+v err=%v", result, err)
	}

	npc = s.NPCs["wolf-1"]
	npc.Body.Flags[MagicStopTargetInvisibleFlag/8] &^= 1 << (MagicStopTargetInvisibleFlag % 8)
	s.NPCs["wolf-1"] = npc
	npc = s.NPCs["wolf-2"]
	npc.Body.Flags[MagicStopTargetInvisibleFlag/8] &^= 1 << (MagicStopTargetInvisibleFlag % 8)
	s.NPCs["wolf-2"] = npc
	actor = s.Players["actor"]
	actor.Body.Timers[MagicStopCooldownTimerIndex] = LegacyTimer{LastTime: 995, Interval: 20}
	s.Players["actor"] = actor
	p, err = s.PlanMagicStop("actor", "늑대", 1000, func(int, int) int {
		t.Fatal("cooldown consumed RNG")
		return 1
	})
	if err != nil || !p.Cooldown || !p.NoOp || !p.TargetFound || !p.ClearInvisible || p.WaitSeconds != 15 || p.Broadcast == false {
		t.Fatalf("cooldown proposal=%+v err=%v", p, err)
	}
	next, result, err = s.ApplyMagicStop(p)
	nextActor := next.Players["actor"].Body
	if err != nil || !result.Cooldown || !result.Changed || !result.Broadcast || flag(nextActor.Flags[:], MagicStopActorInvisibleFlag) {
		t.Fatalf("cooldown apply result=%+v err=%v", result, err)
	}
	if next.Players["actor"].Body.Timers[MagicStopCooldownTimerIndex] != actor.Body.Timers[MagicStopCooldownTimerIndex] {
		t.Fatal("cooldown rewrote timer")
	}
}

func TestMagicStopRejectsStaleAndUnresolvedState(t *testing.T) {
	s := magicStopTestState(MagicStopRangerClass)
	actor := s.Players["actor"]
	actor.Body.Timers[MagicStopCooldownTimerIndex] = LegacyTimer{LastTime: 995, Interval: 20}
	s.Players["actor"] = actor
	p, err := s.PlanMagicStop("actor", "늑대", 1000, func(int, int) int { return 100 })
	if err != nil {
		t.Fatal(err)
	}
	changed := s.clone()
	changedActor := changed.Players["actor"]
	changedActor.Body.MPCurrent++
	changed.Players["actor"] = changedActor
	if _, _, err := changed.ApplyMagicStop(p); err == nil {
		t.Fatal("stale magic_stop proposal applied")
	}
	unresolved := State{Version: 1, Rooms: map[int16]RoomState{1: {Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}}, PlayerIDs: []string{"actor"}, NPCIDs: []string{"wolf"}}}, Players: map[string]PlayerState{"actor": {Body: LegacyMonster{Name: "수호자", Type: 0, Class: MagicStopRangerClass, RoomID: 1}, Online: true}}, NPCs: nil}
	if _, err := unresolved.PlanMagicStop("actor", "늑대", 1000, func(int, int) int { return 1 }); err == nil {
		t.Fatal("unresolved canonical NPC state admitted")
	}
}

func magicStopTestRolls(t *testing.T, values ...int) (func(int, int) int, func() [][2]int) {
	t.Helper()
	index := 0
	var calls [][2]int
	return func(low, high int) int {
		calls = append(calls, [2]int{low, high})
		if index >= len(values) {
			t.Fatalf("unexpected magic_stop random request %d..%d", low, high)
		}
		value := values[index]
		index++
		if value < low || value > high {
			t.Fatalf("magic_stop random value %d outside %d..%d", value, low, high)
		}
		return value
	}, func() [][2]int { return append([][2]int(nil), calls...) }
}

func TestPlanApplyMagicStopMissCommitsHostilityRevealAndTimers(t *testing.T) {
	s := magicStopTestState(MagicStopRangerClass)
	actor := s.Players["actor"]
	setSettingFlag(&actor.Body, MagicStopActorInvisibleFlag, true)
	actor.Body.Timers[MagicStopCooldownTimerIndex] = LegacyTimer{LastTime: 700, Interval: 11, Misc: 4}
	actor.Body.Timers[MagicStopAttackTimerIndex] = LegacyTimer{LastTime: 600, Interval: 9, Misc: 5}
	s.Players["actor"] = actor
	npc := s.NPCs["wolf-1"]
	npc.Body.Timers[MagicStopSpellTimerIndex] = LegacyTimer{LastTime: 500, Interval: 8, Misc: 6}
	s.NPCs["wolf-1"] = npc
	original := s.clone()
	roll, calls := magicStopTestRolls(t, 100)

	p, err := s.PlanMagicStop("actor", "늑대", 1000, roll)
	if err != nil {
		t.Fatal(err)
	}
	if p.Chance != 90 || p.Roll != 100 || p.Succeeded || !p.Attempted || !p.TimerWrite || !p.CombatApplied || !p.EnemyAdded || p.SpellTimerWrite || p.DamageRoll != 0 || p.DamageApplied || p.TargetHPBefore != 100 || p.TargetHP != 100 {
		t.Fatalf("miss proposal=%+v", p)
	}
	if got, want := calls(), [][2]int{{1, 100}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("miss random calls=%v want=%v", got, want)
	}
	if !reflect.DeepEqual(s, original) {
		t.Fatal("planning mutated the source snapshot")
	}
	if p.Response != magicStopRevealResponse()+magicStopMissResponse() || p.ExpectedEvent == nil || len(p.ExpectedEvent.Texts) != 2 {
		t.Fatalf("miss projection response=%q event=%+v", p.Response, p.ExpectedEvent)
	}

	next, result, err := s.ApplyMagicStop(p)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || !result.CombatApplied || result.Succeeded || !result.EnemyAdded || result.DamageApplied || result.SpellTimerWritten {
		t.Fatalf("miss result=%+v", result)
	}
	if got := next.Players["actor"].Body; flag(got.Flags[:], MagicStopActorInvisibleFlag) {
		t.Fatal("miss did not reveal the actor")
	}
	if got := next.Players["actor"].Body.Timers[MagicStopCooldownTimerIndex]; got != (LegacyTimer{LastTime: 1000, Interval: 20, Misc: 4}) {
		t.Fatalf("cooldown timer=%+v", got)
	}
	if got := next.Players["actor"].Body.Timers[MagicStopAttackTimerIndex]; got != (LegacyTimer{LastTime: 1000, Interval: 9, Misc: 5}) {
		t.Fatalf("attack timer=%+v", got)
	}
	if got := next.NPCs["wolf-1"].Body.Timers[MagicStopSpellTimerIndex]; got != (LegacyTimer{LastTime: 500, Interval: 8, Misc: 6}) {
		t.Fatalf("miss rewrote spell timer=%+v", got)
	}
	npc = next.NPCs["wolf-1"]
	if len(npc.Enemies) != 1 || npc.Enemies[0] != (NPCEnemy{Target: EntityRef{Kind: "player", ID: "actor"}}) {
		t.Fatalf("miss enemy graph=%+v", npc.Enemies)
	}
	if err := next.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestPlanApplyMagicStopHitWithoutDamageConsumesDurationRolls(t *testing.T) {
	s := magicStopTestState(MagicStopRangerClass)
	actor := s.Players["actor"]
	actor.Body.Timers[MagicStopCooldownTimerIndex] = LegacyTimer{LastTime: 700, Interval: 11, Misc: 4}
	actor.Body.Timers[MagicStopAttackTimerIndex] = LegacyTimer{LastTime: 600, Interval: 9, Misc: 5}
	s.Players["actor"] = actor
	npc := s.NPCs["wolf-1"]
	npc.Body.Timers[MagicStopSpellTimerIndex] = LegacyTimer{LastTime: 500, Interval: 8, Misc: 6}
	s.NPCs["wolf-1"] = npc
	roll, calls := magicStopTestRolls(t, 1, 100, 2, 3)
	p, err := s.PlanMagicStop("actor", "늑대", 1000, roll)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Attempted || !p.Succeeded || p.DamageRoll != 100 || p.DamageApplied || p.Damage != 0 || p.EnemyDamage != 0 || p.TargetHPBefore != 100 || p.TargetHP != 100 || p.SpellInterval != 15 || !p.SpellTimerWrite || !p.CombatApplied || !p.EnemyAdded {
		t.Fatalf("no-damage proposal=%+v", p)
	}
	if got, want := calls(), [][2]int{{1, 100}, {25, 100}, {1, 6}, {1, 6}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("no-damage random calls=%v want=%v", got, want)
	}
	if strings.Contains(p.Response, "피해를 입혔습니다") || len(p.ExpectedEvent.Texts) != 1 {
		t.Fatalf("no-damage output response=%q event=%+v", p.Response, p.ExpectedEvent)
	}

	next, result, err := s.ApplyMagicStop(p)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Succeeded || !result.CombatApplied || result.DamageApplied || result.Damage != 0 || result.EnemyDamage != 0 || result.TargetHP != 100 || !result.SpellTimerWritten {
		t.Fatalf("no-damage result=%+v", result)
	}
	if got := next.NPCs["wolf-1"].Body; got.HPCurrent != 100 || got.MPCurrent != 31 || got.Timers[MagicStopSpellTimerIndex] != (LegacyTimer{LastTime: 1000, Interval: 15, Misc: 6}) {
		t.Fatalf("no-damage target=%+v", got)
	}
	if got := next.NPCs["wolf-1"].Enemies; len(got) != 1 || got[0].Damage != 0 {
		t.Fatalf("no-damage enemies=%+v", got)
	}
	if got := next.Players["actor"].Body; got.MPCurrent != 17 || got.Timers[MagicStopCooldownTimerIndex] != (LegacyTimer{LastTime: 1000, Interval: 20, Misc: 4}) || got.Timers[MagicStopAttackTimerIndex] != (LegacyTimer{LastTime: 1000, Interval: 9, Misc: 5}) {
		t.Fatalf("no-damage actor=%+v", got)
	}
}

func TestPlanApplyMagicStopHitWithDamageUpdatesHPEnemyAndOutput(t *testing.T) {
	s := magicStopTestState(MagicStopRangerClass)
	npc := s.NPCs["wolf-1"]
	npc.Body.Timers[MagicStopSpellTimerIndex] = LegacyTimer{LastTime: 500, Interval: 8, Misc: 6}
	s.NPCs["wolf-1"] = npc
	roll, calls := magicStopTestRolls(t, 1, 25, 6, 6)
	p, err := s.PlanMagicStop("actor", "늑대", 1000, roll)
	if err != nil {
		t.Fatal(err)
	}
	if p.Roll != 1 || p.DamageRoll != 25 || !p.DamageApplied || p.Damage != 50 || p.EnemyDamage != 50 || p.TargetHPBefore != 100 || p.TargetHP != 50 || p.DurationRoll1 != 6 || p.DurationRoll2 != 6 || p.SpellInterval != 18 {
		t.Fatalf("damage proposal=%+v", p)
	}
	if got, want := calls(), [][2]int{{1, 100}, {25, 100}, {1, 6}, {1, 6}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("damage random calls=%v want=%v", got, want)
	}
	if !strings.Contains(p.Response, "50의 피해를 입혔습니다") || p.ExpectedEvent == nil || len(p.ExpectedEvent.Texts) != 2 || !strings.Contains(p.ExpectedEvent.Text, "50의 피해를 입혔습니다") {
		t.Fatalf("damage output response=%q event=%+v", p.Response, p.ExpectedEvent)
	}
	next, result, err := s.ApplyMagicStop(p)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Succeeded || !result.DamageApplied || result.Damage != 50 || result.EnemyDamage != 50 || result.TargetHP != 50 || !result.SpellTimerWritten {
		t.Fatalf("damage result=%+v", result)
	}
	got := next.NPCs["wolf-1"]
	if got.Body.HPCurrent != 50 || got.Body.MPCurrent != 31 || got.Body.Timers[MagicStopSpellTimerIndex] != (LegacyTimer{LastTime: 1000, Interval: 18, Misc: 6}) || len(got.Enemies) != 1 || got.Enemies[0].Damage != 50 {
		t.Fatalf("damage target=%+v", got)
	}
}

func TestMagicStopHitWithOneHPKeepsZeroIntegerDamageNonlethal(t *testing.T) {
	s := magicStopTestState(MagicStopRangerClass)
	npc := s.NPCs["wolf-1"]
	npc.Body.HPMax, npc.Body.HPCurrent = 1, 1
	s.NPCs["wolf-1"] = npc
	p, err := s.PlanMagicStop("actor", "늑대", 1000, func(low, high int) int {
		switch {
		case low == 1 && high == 100:
			return 1
		case low == 25 && high == 100:
			return 25
		default:
			return 1
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !p.DamageApplied || p.Damage != 0 || p.EnemyDamage != 0 || p.TargetHPBefore != 1 || p.TargetHP != 1 || !strings.Contains(p.Response, "0의 피해를 입혔습니다") {
		t.Fatalf("zero-damage proposal=%+v", p)
	}
	next, _, err := s.ApplyMagicStop(p)
	if err != nil {
		t.Fatal(err)
	}
	if next.NPCs["wolf-1"].Body.HPCurrent != 1 || len(next.NPCs["wolf-1"].Enemies) != 1 || next.NPCs["wolf-1"].Enemies[0].Damage != 0 {
		t.Fatalf("zero-damage target=%+v", next.NPCs["wolf-1"])
	}
}

func TestMagicStopResistanceForcesFiveSecondSpellTimer(t *testing.T) {
	for _, flagBit := range []int{MagicStopTargetResistMagic, MagicStopTargetResistBefuddle} {
		t.Run(fmt.Sprintf("flag-%d", flagBit), func(t *testing.T) {
			s := magicStopTestState(MagicStopRangerClass)
			npc := s.NPCs["wolf-1"]
			setSettingFlag(&npc.Body, uint(flagBit), true)
			s.NPCs["wolf-1"] = npc
			p, err := s.PlanMagicStop("actor", "늑대", 1000, func(low, high int) int {
				switch {
				case low == 1 && high == 100:
					return 1
				case low == 25 && high == 100:
					return 100
				default:
					return 1
				}
			})
			if err != nil || p.SpellInterval != 5 || p.DurationRoll1 != 1 || p.DurationRoll2 != 1 {
				t.Fatalf("resistance proposal=%+v err=%v", p, err)
			}
			next, _, err := s.ApplyMagicStop(p)
			if err != nil {
				t.Fatal(err)
			}
			if got := next.NPCs["wolf-1"].Body.Timers[MagicStopSpellTimerIndex]; got.LastTime != 1000 || got.Interval != 5 {
				t.Fatalf("resistance spell timer=%+v", got)
			}
		})
	}
}

func TestMagicStopAccumulatesExistingEnemyWithoutDuplicate(t *testing.T) {
	s := magicStopTestState(MagicStopRangerClass)
	npc := s.NPCs["wolf-1"]
	npc.Enemies = []NPCEnemy{
		{Target: EntityRef{Kind: "npc", ID: "wolf-2"}, Damage: 4},
		{Target: EntityRef{Kind: "player", ID: "actor"}, Damage: 7},
	}
	s.NPCs["wolf-1"] = npc
	p, err := s.PlanMagicStop("actor", "늑대", 1000, func(low, high int) int {
		if low == 1 && high == 100 {
			return 1
		}
		if low == 25 && high == 100 {
			return 25
		}
		return 1
	})
	if err != nil || p.EnemyAdded || p.EnemyDamage != 50 {
		t.Fatalf("accumulation proposal=%+v err=%v", p, err)
	}
	next, _, err := s.ApplyMagicStop(p)
	if err != nil {
		t.Fatal(err)
	}
	got := next.NPCs["wolf-1"].Enemies
	if len(got) != 2 || got[0].Target != (EntityRef{Kind: "npc", ID: "wolf-2"}) || got[0].Damage != 4 || got[1].Target != (EntityRef{Kind: "player", ID: "actor"}) || got[1].Damage != 57 {
		t.Fatalf("accumulated enemies=%+v", got)
	}

	p, err = next.PlanMagicStop("actor", "늑대", 1020, func(low, high int) int {
		if low != 1 || high != 100 {
			t.Fatalf("miss should stop after first roll: %d..%d", low, high)
		}
		return 100
	})
	if err != nil || p.EnemyAdded {
		t.Fatalf("second proposal=%+v err=%v", p, err)
	}
	next, _, err = next.ApplyMagicStop(p)
	if err != nil || len(next.NPCs["wolf-1"].Enemies) != 2 || next.NPCs["wolf-1"].Enemies[1].Damage != 57 {
		t.Fatalf("second accumulation state=%+v err=%v", next.NPCs["wolf-1"].Enemies, err)
	}
}

func TestMagicStopRejectsUnresolvedDeadSummonedLegacyAndOverflowBeforeRNG(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*State)
		want   error
	}{
		{name: "nil enemy list", mutate: func(s *State) {
			npc := s.NPCs["wolf-1"]
			npc.Enemies = nil
			s.NPCs["wolf-1"] = npc
		}, want: ErrMagicStopNPCStateUnresolved},
		{name: "dead target", mutate: func(s *State) {
			npc := s.NPCs["wolf-1"]
			npc.Body.HPCurrent = 0
			s.NPCs["wolf-1"] = npc
		}, want: ErrMagicStopCombatSideEffectPending},
		{name: "summoned target", mutate: func(s *State) {
			npc := s.NPCs["wolf-1"]
			setSettingFlag(&npc.Body, magicStopNPCSummonFlag, true)
			s.NPCs["wolf-1"] = npc
		}, want: ErrMagicStopCombatSideEffectPending},
		{name: "legacy inventory", mutate: func(s *State) {
			npc := s.NPCs["wolf-1"]
			npc.Body.Inventory = []LegacyObject{{Name: "legacy"}}
			s.NPCs["wolf-1"] = npc
		}, want: ErrMagicStopNPCStateUnresolved},
		{name: "negative enemy damage", mutate: func(s *State) {
			npc := s.NPCs["wolf-1"]
			npc.Enemies = []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "actor"}, Damage: -1}}
			s.NPCs["wolf-1"] = npc
		}, want: ErrMagicStopNPCStateUnresolved},
		{name: "legacy class", mutate: func(s *State) {
			npc := s.NPCs["wolf-1"]
			npc.Body.Class = MagicStopMaxLegacyClass + 1
			s.NPCs["wolf-1"] = npc
		}, want: ErrMagicStopNPCStateUnresolved},
		{name: "enemy damage overflow", mutate: func(s *State) {
			npc := s.NPCs["wolf-1"]
			npc.Enemies = []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "actor"}, Damage: math.MaxInt32 - 1}}
			s.NPCs["wolf-1"] = npc
		}, want: ErrMagicStopCombatSideEffectPending},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := magicStopTestState(MagicStopRangerClass)
			tc.mutate(&s)
			before := s.clone()
			calls := 0
			_, err := s.PlanMagicStop("actor", "늑대", 1000, func(int, int) int {
				calls++
				return 1
			})
			if err == nil || !errors.Is(err, tc.want) || calls != 0 || !reflect.DeepEqual(s, before) {
				t.Fatalf("err=%v want=%v calls=%d mutated=%v", err, tc.want, calls, !reflect.DeepEqual(s, before))
			}
		})
	}
}

func TestMagicStopRejectsTamperReplayAndPreservesState(t *testing.T) {
	s := magicStopTestState(MagicStopRangerClass)
	p, err := s.PlanMagicStop("actor", "늑대", 1000, func(low, high int) int {
		switch {
		case low == 1 && high == 100:
			return 1
		case low == 25 && high == 100:
			return 25
		default:
			return 1
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	before := s.clone()
	tampered := p
	tampered.Damage++
	if _, _, err := s.ApplyMagicStop(tampered); err == nil || !reflect.DeepEqual(s, before) {
		t.Fatalf("tampered proposal applied or mutated source: err=%v", err)
	}
	next, _, err := s.ApplyMagicStop(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := next.ApplyMagicStop(p); err == nil {
		t.Fatal("replayed magic_stop proposal applied")
	}
}
