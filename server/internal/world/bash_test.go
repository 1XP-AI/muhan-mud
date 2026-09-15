package world

import (
	"errors"
	"reflect"
	"testing"
)

func bashTestState(t *testing.T) State {
	t.Helper()
	s := State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"a"},
				NPCIDs:    []string{"wolf"},
			},
		},
		Players: map[string]PlayerState{
			"a": {
				Body: LegacyMonster{
					Name: "Alice", Type: 0, Class: BashFighterClass, Level: 4, RoomID: 1,
					Stats: [5]byte{14, 14, 10, 10, 10}, HPMax: 100, HPCurrent: 100,
					DiceCount: 1, DiceSides: 4,
				},
				Online: true,
				Items:  &ItemCollection{Items: map[string]Item{}},
			},
		},
		NPCs: map[string]NPCState{
			"wolf": {
				Body: LegacyMonster{
					Name: "늑대", Type: 1, Class: BashFighterClass, Level: 4, RoomID: 1,
					Stats: [5]byte{10, 10, 10, 10, 10}, HPMax: 100, HPCurrent: 100, Experience: 100,
					DiceCount: 1, DiceSides: 4,
				},
				Enemies: []NPCEnemy{},
			},
		},
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	return s
}

func bashTestWeapon(s State, shots int16) State {
	player := s.Players["a"]
	player.Items = &ItemCollection{
		Items: map[string]Item{
			"sword": {Object: LegacyObject{
				Name: "검", Type: 0, Wear: 20, ShotsMax: 5, ShotsCurrent: shots,
				DiceCount: 1, DiceSides: 4,
			}},
		},
		Ready: [20]string{BashWieldSlot: "sword"},
	}
	s.Players["a"] = player
	return s
}

func bashTestFlag(flags *[8]byte, bit int, enabled bool) {
	if enabled {
		flags[bit/8] |= 1 << (bit % 8)
	} else {
		flags[bit/8] &^= 1 << (bit % 8)
	}
}

func bashTestRoll(t *testing.T, values ...int) func(int, int) int {
	t.Helper()
	index := 0
	return func(low, high int) int {
		if index >= len(values) {
			t.Fatalf("unexpected bash random request %d..%d", low, high)
		}
		value := values[index]
		index++
		if value < low || value > high {
			t.Fatalf("bash random value %d outside %d..%d", value, low, high)
		}
		return value
	}
}

func TestPlanApplyBashNPCHitKeepsReceiptAndEnemyDamageBounded(t *testing.T) {
	s := bashTestWeapon(bashTestState(t), 5)
	actor := s.Players["a"]
	bashTestFlag(&actor.Body.Flags, BashPlayerHiddenFlag, true)
	bashTestFlag(&actor.Body.Flags, BashPlayerInvisibleFlag, true)
	s.Players["a"] = actor
	original := s

	p, err := s.PlanBash("a", "늑대", 100, bashTestRoll(t, 1, 20, 6, 4, 100))
	if err != nil {
		t.Fatal(err)
	}
	if p.Chance != 55 || p.ChanceRoll != 1 || p.HitRoll != 20 || p.BefuddleInterval != 6 || p.RawDamage != 18 || p.Damage != 25 || p.EnemyDamage != 18 {
		t.Fatalf("proposal=%+v", p)
	}
	if !p.Attempted || !p.Hit || !p.Succeeded || !p.EnemyAdded || p.TargetHP != 75 {
		t.Fatalf("outcome=%+v", p)
	}
	if !reflect.DeepEqual(s, original) {
		t.Fatal("planning mutated source snapshot")
	}

	next, result, err := s.ApplyBash(p)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "bash" || !result.Changed || !result.Broadcast || result.Damage != 25 || result.EnemyDamage != 18 || result.ProficiencyAward != 18 {
		t.Fatalf("result=%+v", result)
	}
	if result.Event == nil || result.Event.ExcludeActorID != "a" || result.Event.TargetID != "wolf" || result.Event.TargetKind != BashTargetNPC {
		t.Fatalf("event=%+v", result.Event)
	}
	npc := next.NPCs["wolf"]
	if npc.Body.HPCurrent != 75 || npc.Body.Timers[BashBefuddleTimerIndex] != (LegacyTimer{LastTime: 100, Interval: 6}) || !flag(npc.Body.Flags[:], bashNPCBefuddledFlag) || len(npc.Enemies) != 1 || npc.Enemies[0].Damage != 18 {
		t.Fatalf("npc=%+v", npc)
	}
	if next.Players["a"].Body.Timers[BashTimerIndex] != (LegacyTimer{LastTime: 100, Interval: 1}) || next.Players["a"].Body.Flags[0]&(1<<BashPlayerHiddenFlag|1<<BashPlayerInvisibleFlag) != 0 || next.Players["a"].Body.Proficiency[0] != 18 {
		t.Fatalf("actor=%+v", next.Players["a"].Body)
	}
	if s.NPCs["wolf"].Body.HPCurrent != 100 {
		t.Fatal("apply mutated source snapshot")
	}
}

func TestPlanApplyBashChanceMissStillStartsNPCHostility(t *testing.T) {
	s := bashTestState(t)
	p, err := s.PlanBash("a", "늑대", 100, bashTestRoll(t, 100))
	if err != nil {
		t.Fatal(err)
	}
	if p.ChanceSucceeded || !p.Attempted || p.Hit || !p.EnemyAdded {
		t.Fatalf("proposal=%+v", p)
	}
	next, result, err := s.ApplyBash(p)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.ChanceRoll != 100 || result.Hit || result.Succeeded || result.Damage != 0 {
		t.Fatalf("result=%+v", result)
	}
	if next.NPCs["wolf"].Body.HPCurrent != 100 || len(next.NPCs["wolf"].Enemies) != 1 || next.NPCs["wolf"].Enemies[0].Damage != 0 {
		t.Fatalf("npc=%+v", next.NPCs["wolf"])
	}
}

func TestPlanApplyBashBrokenWeaponMovesCanonicalRoot(t *testing.T) {
	s := bashTestWeapon(bashTestState(t), 0)
	p, err := s.PlanBash("a", "늑대", 100, bashTestRoll(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	if !p.WeaponBroken || p.Hit || p.Succeeded || !p.EnemyAdded || p.HitRoll != 0 {
		t.Fatalf("proposal=%+v", p)
	}
	next, result, err := s.ApplyBash(p)
	if err != nil {
		t.Fatal(err)
	}
	if !result.WeaponBroken || next.Players["a"].Items.Ready[BashWieldSlot] != "" || !reflect.DeepEqual(next.Players["a"].Items.Inventory, []string{"sword"}) {
		t.Fatalf("weapon state=%+v result=%+v", next.Players["a"].Items, result)
	}
	if len(next.NPCs["wolf"].Enemies) != 1 || next.NPCs["wolf"].Body.HPCurrent != 100 {
		t.Fatalf("npc=%+v", next.NPCs["wolf"])
	}
}

func TestPlanApplyBashPlayerTargetUsesSameRoomCanonicalIdentity(t *testing.T) {
	s := bashTestState(t)
	s.Rooms[1] = RoomState{
		Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}},
		PlayerIDs: []string{"a", "b"},
	}
	delete(s.NPCs, "wolf")
	actor := s.Players["a"]
	bashTestFlag(&actor.Body.Flags, BashPlayerChaosFlag, true)
	s.Players["a"] = actor
	target := PlayerState{Body: LegacyMonster{Name: "Bob", Type: 0, Class: BashFighterClass, Level: 4, RoomID: 1, Stats: [5]byte{10, 10, 10, 10, 10}, HPMax: 100, HPCurrent: 100}, Online: true}
	bashTestFlag(&target.Body.Flags, BashPlayerChaosFlag, true)
	s.Players["b"] = target
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}

	p, err := s.PlanBash("a", "Bob", 100, bashTestRoll(t, 1, 20, 6, 4, 100))
	if err != nil {
		t.Fatal(err)
	}
	if p.TargetKind != BashTargetPlayer || p.TargetID != "b" || p.EnemyAdded || p.Damage != 36 || p.TargetHP != 64 {
		t.Fatalf("proposal=%+v", p)
	}
	next, result, err := s.ApplyBash(p)
	if err != nil {
		t.Fatal(err)
	}
	if next.Players["b"].Body.HPCurrent != 64 || next.Players["b"].Body.Timers[BashBefuddleTimerIndex].Interval != 6 || result.Event == nil || result.Event.ExcludeTargetID != "b" || result.Event.TargetText == "" {
		t.Fatalf("next=%+v result=%+v", next.Players["b"], result)
	}
}

func TestPlanApplyBashGatesAndCooldownAreNoOpsWithoutRNG(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(*State)
		status string
		now    int32
	}{
		{name: "unauthorized", setup: func(s *State) { p := s.Players["a"]; p.Body.Class = 1; s.Players["a"] = p }, status: bashStatusUnauthorized, now: 100},
		{name: "blind", setup: func(s *State) {
			p := s.Players["a"]
			bashTestFlag(&p.Body.Flags, BashPlayerBlindFlag, true)
			s.Players["a"] = p
		}, status: bashStatusBlind, now: 100},
		{name: "cooldown", setup: func(s *State) {
			p := s.Players["a"]
			p.Body.Timers[BashTimerIndex] = LegacyTimer{LastTime: 100, Interval: 3}
			s.Players["a"] = p
		}, status: bashStatusCooldown, now: 102},
		{name: "no target", setup: func(s *State) { r := s.Rooms[1]; r.NPCIDs = nil; s.Rooms[1] = r; delete(s.NPCs, "wolf") }, status: bashStatusNoTarget, now: 100},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := bashTestState(t)
			tc.setup(&s)
			if err := s.Validate(); err != nil {
				t.Fatal(err)
			}
			original := s
			p, err := s.PlanBash("a", "늑대", tc.now, func(int, int) int { t.Fatal("gate consumed RNG"); return 0 })
			if err != nil || !p.NoOp || !p.Rejected || p.Status != tc.status {
				t.Fatalf("proposal=%+v err=%v", p, err)
			}
			next, result, err := s.ApplyBash(p)
			if err != nil || result.Changed || !result.NoOp || result.Status != tc.status || !reflect.DeepEqual(next, original) {
				t.Fatalf("next=%+v result=%+v err=%v", next, result, err)
			}
		})
	}
}

func TestPlanApplyBashNPCPostGatesStillCommitCooldownAndReveal(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*State)
	}{
		{name: "no harm", setup: func(s *State) {
			npc := s.NPCs["wolf"]
			bashTestFlag(&npc.Body.Flags, bashNPCNoHarmFlag, true)
			s.NPCs["wolf"] = npc
		}},
		{name: "magic only", setup: func(s *State) {
			npc := s.NPCs["wolf"]
			bashTestFlag(&npc.Body.Flags, bashNPCNoMagicFlag, true)
			s.NPCs["wolf"] = npc
		}},
		{name: "enchant only", setup: func(s *State) {
			*s = bashTestWeapon(*s, 5)
			npc := s.NPCs["wolf"]
			bashTestFlag(&npc.Body.Flags, bashNPCEnchantOnlyFlag, true)
			s.NPCs["wolf"] = npc
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := bashTestState(t)
			actor := s.Players["a"]
			bashTestFlag(&actor.Body.Flags, BashPlayerHiddenFlag, true)
			bashTestFlag(&actor.Body.Flags, BashPlayerInvisibleFlag, true)
			s.Players["a"] = actor
			tc.setup(&s)
			if err := s.Validate(); err != nil {
				t.Fatal(err)
			}
			p, err := s.PlanBash("a", "늑대", 100, func(int, int) int { t.Fatal("post-gate consumed RNG"); return 0 })
			if err != nil || p.NoOp || !p.Rejected || p.Status == bashStatusReady || !p.TimerWrite || p.Interval != 1 {
				t.Fatalf("proposal=%+v err=%v", p, err)
			}
			next, result, err := s.ApplyBash(p)
			if err != nil {
				t.Fatal(err)
			}
			if !result.Changed || next.Players["a"].Body.Timers[BashTimerIndex] != (LegacyTimer{LastTime: 100, Interval: 1}) || next.Players["a"].Body.Flags[0]&(1<<BashPlayerHiddenFlag|1<<BashPlayerInvisibleFlag) != 0 {
				t.Fatalf("next=%+v result=%+v", next.Players["a"], result)
			}
			if next.NPCs["wolf"].Body.HPCurrent != 100 || len(next.NPCs["wolf"].Enemies) != 0 {
				t.Fatalf("NPC changed after post-gate: %+v", next.NPCs["wolf"])
			}
		})
	}
}

func TestPlanBashFailsClosedForLethalAndUnresolvedRelations(t *testing.T) {
	s := bashTestWeapon(bashTestState(t), 5)
	npc := s.NPCs["wolf"]
	npc.Body.HPCurrent = 1
	s.NPCs["wolf"] = npc
	original := s
	_, err := s.PlanBash("a", "늑대", 100, bashTestRoll(t, 1, 20, 6, 4, 100))
	if !errors.Is(err, ErrBashDeathTransitionPending) || !reflect.DeepEqual(s, original) {
		t.Fatalf("lethal err=%v state changed=%v", err, !reflect.DeepEqual(s, original))
	}

	unresolved := bashTestState(t)
	npc = unresolved.NPCs["wolf"]
	npc.Enemies = nil
	unresolved.NPCs["wolf"] = npc
	if _, err := unresolved.PlanBash("a", "늑대", 100, func(int, int) int { t.Fatal("unresolved relation consumed RNG"); return 0 }); err == nil {
		t.Fatal("accepted unresolved NPC enemy relation")
	}

	players := bashTestState(t)
	r := RoomState{Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}}, PlayerIDs: []string{"a", "b"}}
	players.Rooms[1] = r
	delete(players.NPCs, "wolf")
	a := players.Players["a"]
	b := PlayerState{Body: LegacyMonster{Name: "Bob", Type: 0, Class: BashFighterClass, Level: 4, RoomID: 1, Stats: [5]byte{10, 10, 10, 10, 10}, HPMax: 100, HPCurrent: 100}, Online: true}
	bashTestFlag(&a.Body.Flags, BashPlayerFamilyFlag, true)
	bashTestFlag(&b.Body.Flags, BashPlayerFamilyFlag, true)
	players.Players["a"], players.Players["b"] = a, b
	if err := players.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := players.PlanBash("a", "Bob", 100, func(int, int) int { t.Fatal("war state consumed RNG"); return 0 }); !errors.Is(err, ErrBashWarStateUnresolved) {
		t.Fatalf("family-war err=%v", err)
	}
	bashTestFlag(&b.Body.Flags, BashPlayerFamilyFlag, false)
	bashTestFlag(&a.Body.Flags, BashPlayerFamilyFlag, false)
	bashTestFlag(&b.Body.Flags, BashPlayerCharmFlag, true)
	bashTestFlag(&a.Body.Flags, BashPlayerChaosFlag, true)
	bashTestFlag(&b.Body.Flags, BashPlayerChaosFlag, true)
	players.Players["a"] = a
	players.Players["b"] = b
	if _, err := players.PlanBash("a", "Bob", 100, func(int, int) int { t.Fatal("charm state consumed RNG"); return 0 }); !errors.Is(err, ErrBashCharmStateUnresolved) {
		t.Fatalf("charm err=%v", err)
	}
}

func TestApplyBashRejectsStaleSnapshot(t *testing.T) {
	s := bashTestState(t)
	p, err := s.PlanBash("a", "늑대", 100, bashTestRoll(t, 100))
	if err != nil {
		t.Fatal(err)
	}
	changed := s.clone()
	// A semantically inert but observable snapshot change must still invalidate
	// the proposal; receipts bind the entire canonical snapshot, not a subset.
	room := changed.Rooms[1]
	room.Resource.Name = "다른 방"
	changed.Rooms[1] = room
	if _, _, err := changed.ApplyBash(p); err == nil {
		t.Fatal("stale bash proposal applied")
	}
}
