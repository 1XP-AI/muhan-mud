package world

import (
	"errors"
	"math"
	"reflect"
	"testing"
)

func absorbTestFlag(body *LegacyMonster, bit int) {
	body.Flags[bit/8] |= 1 << (bit % 8)
}

func absorbStateFixture(actorClass byte) State {
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
					Name: "도술사", Type: absorbPlayerType, Class: actorClass, Level: 20,
					RoomID: 1, Stats: [5]byte{10, 10, 10, 20, 10},
					HPMax: 100, HPCurrent: 90, MPMax: 100, MPCurrent: 17,
					Timers: [45]LegacyTimer{{Misc: 1}, {}, {}, {Misc: 2}, {}, {}, {}, {}, {}, {}, {}, {}, {}, {}, {}, {Misc: 3}},
				},
				Online: true,
			},
		},
		NPCs: map[string]NPCState{
			"wolf-1": {Body: LegacyMonster{Name: "늑대", Type: absorbMonsterType, Class: 4, Level: 4, RoomID: 1, HPMax: 500, HPCurrent: 500}, Enemies: []NPCEnemy{}},
			"wolf-2": {Body: LegacyMonster{Name: "늑대", Type: absorbMonsterType, Class: 4, Level: 4, RoomID: 1, HPMax: 500, HPCurrent: 500}, Enemies: []NPCEnemy{}},
		},
		ActiveNPCIDs: []string{"wolf-1", "wolf-2"},
	}
}

func TestPlanApplyAbsorbPreservesSourceChanceDamageAndUncappedHealing(t *testing.T) {
	s := absorbStateFixture(absorbMageClass)
	actor := s.Players["actor"]
	absorbTestFlag(&actor.Body, absorbActorInvisibleFlag)
	s.Players["actor"] = actor
	rolls := 0
	p, err := s.PlanAbsorbWithOccurrence("actor", "늑대", 2, 1000, func(low, high int) int {
		rolls++
		if low != 1 || high != 100 {
			t.Fatalf("roll range=%d..%d", low, high)
		}
		return 1
	})
	if err != nil {
		t.Fatal(err)
	}
	if rolls != 1 || !p.Authorized || !p.TargetFound || p.TargetID != "wolf-2" || p.Chance != 80 || p.Roll != 1 || !p.Succeeded || !p.DamageApplied || p.Damage != 230 || p.Absorbed != 230 || p.TargetHPBefore != 500 || p.TargetHP != 270 || p.ActorHPBefore != 90 || p.ActorHP != 320 || p.MPAfter != 17 || !p.EnemyAdded {
		t.Fatalf("proposal=%+v rolls=%d", p, rolls)
	}
	next, result, err := s.ApplyAbsorb(p)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "absorb" || !result.Succeeded || !result.CombatApplied || !result.ClearInvisible || !result.ActorTimerWritten || !result.EnemyAdded || !result.Broadcast || result.Event == nil || result.TargetID != "wolf-2" || result.TargetOccurrence != 2 {
		t.Fatalf("result=%+v", result)
	}
	updated := next.Players["actor"].Body
	if updated.HPCurrent != 320 || updated.HPCurrent <= updated.HPMax || flag(updated.Flags[:], absorbActorInvisibleFlag) || updated.Timers[absorbCooldownTimerIndex] != (LegacyTimer{LastTime: 1000, Interval: absorbCooldownSeconds, Misc: 3}) || updated.Timers[absorbAttackTimerIndex].LastTime != 1000 || updated.Timers[absorbAttackTimerIndex].Misc != 2 {
		t.Fatalf("actor after absorb=%+v", updated)
	}
	target := next.NPCs["wolf-2"]
	if target.Body.HPCurrent != 270 || len(target.Enemies) != 1 || target.Enemies[0].Target != (EntityRef{Kind: "player", ID: "actor"}) || target.Enemies[0].Damage != 230 {
		t.Fatalf("target after absorb=%+v", target)
	}
	if next.NPCs["wolf-1"].Body.HPCurrent != 500 {
		t.Fatal("occurrence selected the wrong NPC")
	}
}

func TestAbsorbMissStillCommitsEnemyAndCooldownButNoVitals(t *testing.T) {
	s := absorbStateFixture(absorbMageClass)
	p, err := s.PlanAbsorb("actor", "늑대", 1000, func(int, int) int { return 100 })
	if err != nil || p.Succeeded || !p.Attempted || !p.EnemyAdded || p.Damage != 0 || p.ActorHP != 90 {
		t.Fatalf("miss proposal=%+v err=%v", p, err)
	}
	next, result, err := s.ApplyAbsorb(p)
	if err != nil || result.Succeeded || !result.Attempted || !result.Changed || result.DamageApplied || result.EnemyAdded != true {
		t.Fatalf("miss result=%+v err=%v", result, err)
	}
	if next.Players["actor"].Body.HPCurrent != 90 || next.NPCs["wolf-1"].Body.HPCurrent != 500 || next.Players["actor"].Body.Timers[absorbCooldownTimerIndex].Interval != absorbCooldownSeconds || len(next.NPCs["wolf-1"].Enemies) != 1 {
		t.Fatalf("miss state=%+v", next)
	}
}

func TestAbsorbUndeadDrainsMPWithoutHPDamage(t *testing.T) {
	s := absorbStateFixture(absorbMageClass)
	npc := s.NPCs["wolf-1"]
	absorbTestFlag(&npc.Body, absorbTargetUndeadFlag)
	s.NPCs["wolf-1"] = npc
	p, err := s.PlanAbsorb("actor", "늑대", 1000, func(int, int) int { return 1 })
	if err != nil || !p.Succeeded || !p.Undead || p.Damage != 0 || p.TargetHP != 0 || p.MPAfter != 0 || !p.MPChanged {
		t.Fatalf("undead proposal=%+v err=%v", p, err)
	}
	next, result, err := s.ApplyAbsorb(p)
	if err != nil || !result.Undead || !result.MPChanged || next.Players["actor"].Body.MPCurrent != 0 || next.Players["actor"].Body.HPCurrent != 90 || next.NPCs["wolf-1"].Body.HPCurrent != 500 || len(next.NPCs["wolf-1"].Enemies) != 1 {
		t.Fatalf("undead result=%+v state=%+v err=%v", result, next, err)
	}
}

func TestAbsorbGatesRevealCooldownNoHarmAndVisibilityBeforeRNG(t *testing.T) {
	s := absorbStateFixture(absorbMageClass)
	npc := s.NPCs["wolf-1"]
	npc.Body.Class = absorbCaretakerClass
	absorbTestFlag(&npc.Body, absorbTargetDMInvisible)
	s.NPCs["wolf-1"] = npc
	npc = s.NPCs["wolf-2"]
	absorbTestFlag(&npc.Body, absorbTargetDMInvisible-8)
	s.NPCs["wolf-2"] = npc
	if p, err := s.PlanAbsorb("actor", "늑대", 1000, func(int, int) int { t.Fatal("hidden target consumed RNG"); return 1 }); err != nil || !p.NoOp || p.TargetFound {
		t.Fatalf("hidden proposal=%+v err=%v", p, err)
	}
	actor := s.Players["actor"]
	absorbTestFlag(&actor.Body, absorbActorDetectFlag)
	actor.Body.Timers[absorbCooldownTimerIndex] = LegacyTimer{LastTime: 995, Interval: 20, Misc: 7}
	absorbTestFlag(&actor.Body, absorbActorInvisibleFlag)
	s.Players["actor"] = actor
	p, err := s.PlanAbsorb("actor", "늑대", 1000, func(int, int) int { t.Fatal("cooldown consumed RNG"); return 1 })
	if err != nil || !p.Cooldown || !p.NoOp || !p.TargetFound || !p.ClearInvisible || p.TargetID != "wolf-2" || p.WaitSeconds != 15 {
		t.Fatalf("cooldown proposal=%+v err=%v", p, err)
	}
	next, result, err := s.ApplyAbsorb(p)
	updatedActor := next.Players["actor"].Body
	if err != nil || !result.Cooldown || !result.Changed || flag(updatedActor.Flags[:], absorbActorInvisibleFlag) || updatedActor.Timers[absorbCooldownTimerIndex].Misc != 7 {
		t.Fatalf("cooldown result=%+v state=%+v err=%v", result, next, err)
	}

	s = absorbStateFixture(absorbMageClass)
	npc = s.NPCs["wolf-1"]
	absorbTestFlag(&npc.Body, absorbTargetNoHarmFlag)
	s.NPCs["wolf-1"] = npc
	actor = s.Players["actor"]
	absorbTestFlag(&actor.Body, absorbActorInvisibleFlag)
	s.Players["actor"] = actor
	p, err = s.PlanAbsorb("actor", "늑대", 1000, func(int, int) int { t.Fatal("protected target consumed RNG"); return 1 })
	if err != nil || !p.Rejected || !p.NoOp || !p.ClearInvisible || p.EnemyAdded {
		t.Fatalf("protected proposal=%+v err=%v", p, err)
	}
	next, result, err = s.ApplyAbsorb(p)
	updatedActor = next.Players["actor"].Body
	if err != nil || !result.Rejected || !result.Changed || flag(updatedActor.Flags[:], absorbActorInvisibleFlag) || len(next.NPCs["wolf-1"].Enemies) != 0 {
		t.Fatalf("protected result=%+v state=%+v err=%v", result, next, err)
	}
}

func TestAbsorbPermissionAndSelectorsFailBeforeRandomness(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*State)
		query  string
		want   error
	}{
		{name: "unauthorized", mutate: func(s *State) {
			s.Players["actor"] = func() PlayerState { p := s.Players["actor"]; p.Body.Class = 4; return p }()
		}, query: "늑대", want: nil},
		{name: "unresolved NPC relations", mutate: func(s *State) { npc := s.NPCs["wolf-1"]; npc.Enemies = nil; s.NPCs["wolf-1"] = npc }, query: "늑대", want: ErrAbsorbCombatSideEffectPending},
		{name: "lethal continuation", mutate: func(s *State) { npc := s.NPCs["wolf-1"]; npc.Body.HPCurrent = 100; s.NPCs["wolf-1"] = npc }, query: "늑대", want: ErrAbsorbDeathTransitionPending},
		{name: "enemy damage overflow", mutate: func(s *State) {
			npc := s.NPCs["wolf-1"]
			npc.Enemies = []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "actor"}, Damage: math.MaxInt32}}
			s.NPCs["wolf-1"] = npc
		}, query: "늑대", want: ErrAbsorbCombatSideEffectPending},
		{name: "flee continuation", mutate: func(s *State) {
			npc := s.NPCs["wolf-1"]
			npc.Body.HPMax = 500
			npc.Body.HPCurrent = 260
			absorbTestFlag(&npc.Body, absorbTargetFleerFlag)
			s.NPCs["wolf-1"] = npc
		}, query: "늑대", want: ErrAbsorbCombatSideEffectPending},
		{name: "healed HP overflow", mutate: func(s *State) { p := s.Players["actor"]; p.Body.HPCurrent = 32760; s.Players["actor"] = p }, query: "늑대", want: ErrAbsorbHPOverflow},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := absorbStateFixture(absorbMageClass)
			tc.mutate(&s)
			before := s.clone()
			calls := 0
			p, err := s.PlanAbsorb("actor", tc.query, 1000, func(int, int) int { calls++; return 1 })
			if tc.want == nil {
				if err != nil || p.Authorized || !p.NoOp || p.TargetFound || calls != 0 {
					t.Fatalf("permission proposal=%+v err=%v calls=%d", p, err, calls)
				}
			} else if err == nil || !errors.Is(err, tc.want) || calls != 0 || !reflect.DeepEqual(s, before) {
				t.Fatalf("err=%v calls=%d mutated=%v", err, calls, !reflect.DeepEqual(s, before))
			}
		})
	}
}

func TestAbsorbAllowsFollowerFleeFlagOnlyWhenDMFollowSuppressesContinuation(t *testing.T) {
	s := absorbStateFixture(absorbMageClass)
	npc := s.NPCs["wolf-1"]
	npc.Body.HPCurrent = 260
	absorbTestFlag(&npc.Body, absorbTargetFleerFlag)
	absorbTestFlag(&npc.Body, absorbTargetFollowDMFlag)
	s.NPCs["wolf-1"] = npc
	p, err := s.PlanAbsorb("actor", "늑대", 1000, func(int, int) int { return 1 })
	if err != nil || !p.Succeeded || p.TargetHP != 30 {
		t.Fatalf("follower flee proposal=%+v err=%v", p, err)
	}
	if _, _, err := s.ApplyAbsorb(p); err != nil {
		t.Fatal(err)
	}
}
