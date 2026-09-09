package world

import (
	"errors"
	"math"
	"reflect"
	"testing"
)

func poisonReducerTestState(class byte) State {
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}},
				PlayerIDs: []string{"actor"},
				NPCIDs:    []string{"wolf-1", "wolf-2"},
			},
		},
		Players: map[string]PlayerState{
			"actor": {
				Body: LegacyMonster{
					Name: "자객", Type: PoisonPlayerType, Class: class, Level: 20, RoomID: 1,
					Stats: [5]byte{10, 20, 10, 10, 10}, HPMax: 100, HPCurrent: 100,
					Timers: [45]LegacyTimer{{Misc: 7}, {}, {}, {Misc: 8}, {}, {}, {}, {}, {}, {}, {}, {}, {}, {}, {}, {Misc: 9}},
				},
				Online: true,
			},
		},
		NPCs: map[string]NPCState{
			"wolf-1": {Body: LegacyMonster{Name: "늑대", Keys: [3]string{"wolf", "짐승", ""}, Type: PoisonMonsterType, Level: 4, Class: 4, RoomID: 1, HPMax: 99, HPCurrent: 99}, Enemies: []NPCEnemy{}},
			"wolf-2": {Body: LegacyMonster{Name: "늑대", Keys: [3]string{"wolf2", "짐승2", ""}, Type: PoisonMonsterType, Level: 4, Class: 4, RoomID: 1, HPMax: 99, HPCurrent: 99}, Enemies: []NPCEnemy{}},
		},
		ActiveNPCIDs: []string{"wolf-1", "wolf-2"},
	}
}

func poisonRolls(values ...int) func(int, int) int {
	index := 0
	return func(low, high int) int {
		if index >= len(values) {
			return low
		}
		value := values[index]
		index++
		return value
	}
}

func poisonPlayerHasFlag(s State, id string, bit uint) bool {
	player := s.Players[id]
	return flag(player.Body.Flags[:], bit)
}

func TestPlanApplyPoisonPreservesSourceDrawOrderAndCanonicalState(t *testing.T) {
	s := poisonReducerTestState(PoisonAssassinClass)
	actor := s.Players["actor"]
	actor.Body.Flags[PoisonActorInvisibleFlag/8] |= 1 << (PoisonActorInvisibleFlag % 8)
	s.Players["actor"] = actor
	var ranges [][2]int
	p, err := s.PlanPoisonWithOccurrence("actor", "늑대", 2, 1000, func(low, high int) int {
		ranges = append(ranges, [2]int{low, high})
		return map[[2]int]int{{1, 100}: 1, {10, 100}: 10, {1, 6}: 2}[ranges[len(ranges)-1]]
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := ranges, [][2]int{{1, 100}, {10, 100}, {1, 6}, {1, 6}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("random order=%v want=%v", got, want)
	}
	if !p.TargetFound || p.TargetID != "wolf-2" || p.Chance != 80 || p.Roll != 1 || !p.Succeeded || !p.Poisoned || !p.DamageApplied || p.Damage != 33 || p.TargetHP != 66 || p.EnemyDamage != 33 || p.SpellInterval != 10 {
		t.Fatalf("proposal=%+v", p)
	}
	next, result, err := s.ApplyPoison(p)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || !result.Broadcast || result.TargetID != "wolf-2" || result.TargetOccurrence != 2 || !result.ClearInvisible || result.AttackInterval != 15 || result.CooldownInterval != 15 || result.SpellInterval != 10 {
		t.Fatalf("result=%+v", result)
	}
	if poisonPlayerHasFlag(next, "actor", PoisonActorInvisibleFlag) {
		t.Fatal("actor remained invisible")
	}
	actor = next.Players["actor"]
	if actor.Body.Timers[PoisonCooldownTimerIndex].LastTime != 1000 || actor.Body.Timers[PoisonCooldownTimerIndex].Interval != 15 || actor.Body.Timers[PoisonCooldownTimerIndex].Misc != 9 || actor.Body.Timers[PoisonAttackTimerIndex].LastTime != 1000 || actor.Body.Timers[PoisonAttackTimerIndex].Misc != 8 {
		t.Fatalf("actor timers=%+v", actor.Body.Timers)
	}
	target := next.NPCs["wolf-2"]
	if target.Body.HPCurrent != 66 || !flag(target.Body.Flags[:], PoisonNPCPoisonedFlag) || target.Body.Timers[PoisonSpellTimerIndex].LastTime != 1000 || target.Body.Timers[PoisonSpellTimerIndex].Interval != 10 || len(target.Enemies) != 1 || target.Enemies[0].Target != (EntityRef{Kind: "player", ID: "actor"}) || target.Enemies[0].Damage != 33 {
		t.Fatalf("target=%+v", target)
	}
}

func TestPoisonMissWritesAttackCooldownAndEnemyButNoPoison(t *testing.T) {
	s := poisonReducerTestState(PoisonAssassinClass)
	p, err := s.PlanPoison("actor", "늑대", 1000, poisonRolls(100))
	if err != nil || p.Succeeded || p.Poisoned || p.DamageChanceRoll != 0 || !p.Attempted || p.EnemyAdded {
		// The first NPC has a nonnil empty relation; EnemyAdded is expected below.
		if err != nil {
			t.Fatal(err)
		}
	}
	if p.EnemyAdded != true {
		t.Fatalf("miss proposal=%+v", p)
	}
	next, result, err := s.ApplyPoison(p)
	if err != nil {
		t.Fatal(err)
	}
	if result.Succeeded || result.Poisoned || result.DamageApplied || result.SpellTimerWritten || !result.Attempted || !result.Changed {
		t.Fatalf("miss result=%+v", result)
	}
	target := next.NPCs["wolf-1"]
	if flag(target.Body.Flags[:], PoisonNPCPoisonedFlag) || target.Body.HPCurrent != 99 || target.Body.Timers[PoisonSpellTimerIndex] != (LegacyTimer{}) || len(target.Enemies) != 1 || target.Enemies[0].Damage != 0 {
		t.Fatalf("miss target=%+v", target)
	}
	if next.Players["actor"].Body.Timers[PoisonCooldownTimerIndex].Interval != 15 || next.Players["actor"].Body.Timers[PoisonAttackTimerIndex].LastTime != 1000 {
		t.Fatalf("miss actor timers=%+v", next.Players["actor"].Body.Timers)
	}
}

func TestPoisonSourceGatesVisibilityCooldownNoHarmAndAuthorizationBeforeRNG(t *testing.T) {
	unauthorized := poisonReducerTestState(4)
	if p, err := unauthorized.PlanPoison("actor", "늑대", 1000, func(int, int) int { t.Fatal("unauthorized RNG"); return 1 }); err != nil || p.Authorized || !p.NoOp || p.TargetFound {
		t.Fatalf("unauthorized proposal=%+v err=%v", p, err)
	}

	s := poisonReducerTestState(PoisonAssassinClass)
	target := s.NPCs["wolf-1"]
	target.Body.Flags[PoisonTargetInvisibleFlag/8] |= 1 << (PoisonTargetInvisibleFlag % 8)
	s.NPCs["wolf-1"] = target
	target = s.NPCs["wolf-2"]
	target.Body.Flags[PoisonTargetInvisibleFlag/8] |= 1 << (PoisonTargetInvisibleFlag % 8)
	s.NPCs["wolf-2"] = target
	if p, err := s.PlanPoison("actor", "늑대", 1000, func(int, int) int { t.Fatal("hidden RNG"); return 1 }); err != nil || !p.NoOp || p.TargetFound || p.ClearInvisible {
		t.Fatalf("hidden proposal=%+v err=%v", p, err)
	}
	actor := s.Players["actor"]
	actor.Body.Flags[PoisonActorDetectFlag/8] |= 1 << (PoisonActorDetectFlag % 8)
	actor.Body.Flags[PoisonActorInvisibleFlag/8] |= 1 << (PoisonActorInvisibleFlag % 8)
	actor.Body.Timers[PoisonCooldownTimerIndex] = LegacyTimer{LastTime: 995, Interval: 20, Misc: 4}
	s.Players["actor"] = actor
	p, err := s.PlanPoison("actor", "늑대", 1000, func(int, int) int { t.Fatal("cooldown RNG"); return 1 })
	if err != nil || !p.Cooldown || !p.NoOp || !p.TargetFound || !p.ClearInvisible || p.WaitSeconds != 15 {
		t.Fatalf("cooldown proposal=%+v err=%v", p, err)
	}
	next, result, err := s.ApplyPoison(p)
	if err != nil || !result.Cooldown || !result.Changed || poisonPlayerHasFlag(next, "actor", PoisonActorInvisibleFlag) || next.Players["actor"].Body.Timers[PoisonCooldownTimerIndex].Misc != 4 {
		t.Fatalf("cooldown result=%+v err=%v", result, err)
	}

	s = poisonReducerTestState(PoisonAssassinClass)
	target = s.NPCs["wolf-1"]
	target.Body.Flags[PoisonTargetNoHarmFlag/8] |= 1 << (PoisonTargetNoHarmFlag % 8)
	s.NPCs["wolf-1"] = target
	actor = s.Players["actor"]
	actor.Body.Flags[PoisonActorInvisibleFlag/8] |= 1 << (PoisonActorInvisibleFlag % 8)
	s.Players["actor"] = actor
	p, err = s.PlanPoison("actor", "늑대", 1000, func(int, int) int { t.Fatal("no-harm RNG"); return 1 })
	if err != nil || !p.Rejected || !p.NoOp || !p.ClearInvisible || p.EnemyAdded {
		t.Fatalf("no-harm proposal=%+v err=%v", p, err)
	}
	next, result, err = s.ApplyPoison(p)
	if err != nil || !result.Rejected || !result.Changed || poisonPlayerHasFlag(next, "actor", PoisonActorInvisibleFlag) || len(next.NPCs["wolf-1"].Enemies) != 0 {
		t.Fatalf("no-harm result=%+v err=%v", result, err)
	}
}

func TestPoisonFailsClosedBeforeRNGForUnresolvedLethalAndFleeContinuation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*State)
		want   error
	}{
		{name: "enemy relations", mutate: func(s *State) { npc := s.NPCs["wolf-1"]; npc.Enemies = nil; s.NPCs["wolf-1"] = npc }, want: ErrPoisonCombatSideEffectPending},
		{name: "enemy damage overflow", mutate: func(s *State) {
			npc := s.NPCs["wolf-1"]
			npc.Enemies = []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "actor"}, Damage: math.MaxInt32}}
			s.NPCs["wolf-1"] = npc
		}, want: ErrPoisonCombatSideEffectPending},
		{name: "lethal death", mutate: func(s *State) { npc := s.NPCs["wolf-1"]; npc.Body.HPCurrent = 20; s.NPCs["wolf-1"] = npc }, want: ErrPoisonDeathTransitionPending},
		{name: "flee", mutate: func(s *State) {
			npc := s.NPCs["wolf-1"]
			npc.Body.HPCurrent = 40
			npc.Body.Flags[PoisonNPCFleerFlag/8] |= 1 << (PoisonNPCFleerFlag % 8)
			s.NPCs["wolf-1"] = npc
		}, want: ErrPoisonCombatSideEffectPending},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := poisonReducerTestState(PoisonAssassinClass)
			tc.mutate(&s)
			calls := 0
			before := s.clone()
			_, err := s.PlanPoison("actor", "늑대", 1000, func(int, int) int { calls++; return 1 })
			if err == nil || !errors.Is(err, tc.want) || calls != 0 || !reflect.DeepEqual(s, before) {
				t.Fatalf("err=%v calls=%d mutated=%v", err, calls, !reflect.DeepEqual(s, before))
			}
		})
	}
}

func TestPoisonRequiresExactDisplayNameAndOccurrenceOrder(t *testing.T) {
	s := poisonReducerTestState(PoisonAssassinClass)
	for _, name := range []string{"wolf", "늑", "WOLF"} {
		if p, err := s.PlanPoison("actor", name, 1000, func(int, int) int { t.Fatal("non-exact selector RNG"); return 1 }); err != nil || !p.NoOp || p.TargetFound {
			t.Fatalf("selector=%q proposal=%+v err=%v", name, p, err)
		}
	}
	if _, err := s.PlanPoison("actor", "늑대 ", 1000, func(int, int) int { t.Fatal("invalid selector RNG"); return 1 }); err == nil {
		t.Fatal("whitespace selector was accepted")
	}
	caseInsensitiveState := poisonReducerTestState(PoisonAssassinClass)
	caseInsensitive := caseInsensitiveState.NPCs["wolf-1"]
	caseInsensitive.Body.Name = "Wolf"
	caseInsensitiveState.NPCs["wolf-1"] = caseInsensitive
	p, err := caseInsensitiveState.PlanPoison("actor", "wolf", 1000, poisonRolls(100))
	if err != nil || !p.TargetFound || p.TargetID != "wolf-1" {
		t.Fatalf("case-insensitive exact display name was not resolved: proposal=%+v err=%v", p, err)
	}
	p, err = s.PlanPoisonWithOccurrence("actor", "늑대", 2, 1000, poisonRolls(100))
	if err != nil || p.TargetID != "wolf-2" || p.TargetOccurrence != 2 {
		t.Fatalf("occurrence proposal=%+v err=%v", p, err)
	}
}
