package world

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func kickTestState(t *testing.T) State {
	t.Helper()
	s := State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"actor"},
				NPCIDs:    []string{"wolf"},
			},
		},
		Players: map[string]PlayerState{
			"actor": {
				Body: LegacyMonster{
					Name: "Alice", Type: 0, Class: KickBarbarianClass, Level: 4, RoomID: 1,
					Stats: [5]byte{14, 14, 10, 10, 10}, HPMax: 100, HPCurrent: 100,
					Thaco: 20, DiceCount: 1, DiceSides: 4, DicePlus: 0,
				},
				Online: true,
				Items:  &ItemCollection{Items: map[string]Item{}},
			},
		},
		NPCs: map[string]NPCState{
			"wolf": {
				Body: LegacyMonster{
					Name: "늑대", Type: 1, Class: KickFighterClassForTest, Level: 4, RoomID: 1,
					Stats: [5]byte{10, 10, 10, 10, 10}, HPMax: 100, HPCurrent: 100,
					Armor: 0, DiceCount: 1, DiceSides: 4, Experience: 100,
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

// The production constants intentionally export only kick-specific classes;
// fighter is the ordinary source NPC class used by the fixture.
const KickFighterClassForTest = 4

func kickPlayerState(t *testing.T) State {
	t.Helper()
	s := kickTestState(t)
	delete(s.NPCs, "wolf")
	s.Rooms[1] = RoomState{Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}}, PlayerIDs: []string{"actor", "target"}}
	s.Players["target"] = PlayerState{Body: LegacyMonster{
		Name: "Bob", Type: 0, Class: 4, Level: 4, RoomID: 1,
		Stats: [5]byte{10, 10, 10, 10, 10}, HPMax: 100, HPCurrent: 100, Armor: 0,
	}, Online: true, Items: &ItemCollection{Items: map[string]Item{}}}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	return s
}

func kickTestFlag(flags *[8]byte, bit int, enabled bool) {
	if enabled {
		flags[bit/8] |= 1 << (bit % 8)
	} else {
		flags[bit/8] &^= 1 << (bit % 8)
	}
}

func kickTestRoll(t *testing.T, values ...int) func(int, int) int {
	t.Helper()
	index := 0
	return func(low, high int) int {
		if index >= len(values) {
			t.Fatalf("unexpected kick random request %d..%d", low, high)
		}
		value := values[index]
		index++
		if value < low || value > high {
			t.Fatalf("kick random value %d outside %d..%d", value, low, high)
		}
		return value
	}
}

func TestPlanApplyKickNPCHitTracksRawDamageAndSourceTimer(t *testing.T) {
	s := kickTestState(t)
	actor := s.Players["actor"]
	kickTestFlag(&actor.Body.Flags, KickPlayerHiddenFlag, true)
	kickTestFlag(&actor.Body.Flags, KickPlayerInvisibleFlag, true)
	s.Players["actor"] = actor
	original := s

	proposal, err := s.PlanKick("actor", "늑대", 100, kickTestRoll(t, 1, 20, 4))
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Chance != 60 || proposal.ChanceRoll != 1 || proposal.HitRoll != 20 || !proposal.Hit || !proposal.Succeeded || proposal.RawDamage != 32 || proposal.Damage != 32 || proposal.EnemyDamage != 32 || proposal.TargetHP != 68 || proposal.Interval != 3 {
		t.Fatalf("proposal=%+v", proposal)
	}
	if proposal.ProficiencyAward != 0 {
		t.Fatalf("kick source does not award proficiency: %+v", proposal)
	}
	if !reflect.DeepEqual(s, original) {
		t.Fatal("planning mutated source snapshot")
	}

	next, result, err := s.ApplyKick(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "kick" || !result.Changed || !result.Broadcast || result.Damage != 32 || result.EnemyDamage != 32 || result.TargetID != "wolf" || result.TargetKind != KickTargetNPC || result.ProficiencyAward != 0 {
		t.Fatalf("result=%+v", result)
	}
	if got := next.NPCs["wolf"]; got.Body.HPCurrent != 68 || len(got.Enemies) != 1 || got.Enemies[0].Target != (EntityRef{Kind: "player", ID: "actor"}) || got.Enemies[0].Damage != 32 {
		t.Fatalf("npc=%+v", got)
	}
	if got := next.Players["actor"].Body; got.Timers[KickTimerIndex] != (LegacyTimer{LastTime: 100, Interval: 3}) || flag(got.Flags[:], KickPlayerHiddenFlag) || flag(got.Flags[:], KickPlayerInvisibleFlag) {
		t.Fatalf("actor=%+v", got)
	}
	if result.Event == nil || result.Event.ExcludeActorID != "actor" || result.Event.TargetID != "wolf" || len(result.Event.Texts) != 2 {
		t.Fatalf("event=%+v", result.Event)
	}
}

func TestPlanApplyKickChanceMissStillAddsNPCEnemyAndKeepsTwoSecondTimer(t *testing.T) {
	s := kickTestState(t)
	proposal, err := s.PlanKick("actor", "늑대", 100, kickTestRoll(t, 100))
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Hit || proposal.Succeeded || !proposal.Attempted || !proposal.EnemyAdded || proposal.Interval != 2 || proposal.Damage != 0 {
		t.Fatalf("proposal=%+v", proposal)
	}
	next, result, err := s.ApplyKick(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed != true || result.Hit || result.EnemyAdded != true || next.NPCs["wolf"].Body.HPCurrent != 100 || len(next.NPCs["wolf"].Enemies) != 1 || next.Players["actor"].Body.Timers[KickTimerIndex].Interval != 2 {
		t.Fatalf("result=%+v next=%+v", result, next)
	}
}

func TestPlanApplyKickHitRollMissDoesNotDamage(t *testing.T) {
	s := kickTestState(t)
	proposal, err := s.PlanKick("actor", "늑대", 100, kickTestRoll(t, 1, 1))
	if err != nil {
		t.Fatal(err)
	}
	if !proposal.Attempted || !proposal.Hit || proposal.Succeeded || proposal.HitRoll != 1 || proposal.ChanceRoll != 1 || proposal.Damage != 0 {
		t.Fatalf("proposal=%+v", proposal)
	}
	next, _, err := s.ApplyKick(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if next.NPCs["wolf"].Body.HPCurrent != 100 || len(next.NPCs["wolf"].Enemies) != 1 {
		t.Fatalf("npc=%+v", next.NPCs["wolf"])
	}
}

func TestPlanApplyKickNPCImmunityCommitsRevealAndCooldownOnly(t *testing.T) {
	for _, tc := range []struct {
		name string
		flag int
		want string
	}{
		{name: "no-harm", flag: 24, want: "해칠 수 없습니다"},
		{name: "magic-only", flag: 20, want: "아무소용"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := kickTestState(t)
			npc := s.NPCs["wolf"]
			kickTestFlag(&npc.Body.Flags, tc.flag, true)
			s.NPCs["wolf"] = npc
			proposal, err := s.PlanKick("actor", "늑대", 100, func(int, int) int { t.Fatal("immunity consumed RNG"); return 0 })
			if err != nil || !proposal.Rejected || proposal.Status == kickStatusReady || proposal.Interval != 2 || !proposal.TimerWrite || !strings.Contains(proposal.Response, tc.want) {
				t.Fatalf("proposal=%+v err=%v", proposal, err)
			}
			next, result, err := s.ApplyKick(proposal)
			if err != nil {
				t.Fatal(err)
			}
			if !result.Changed || next.Players["actor"].Body.Timers[KickTimerIndex] != (LegacyTimer{LastTime: 100, Interval: 2}) || next.NPCs["wolf"].Body.HPCurrent != 100 || len(next.NPCs["wolf"].Enemies) != 0 {
				t.Fatalf("result=%+v next=%+v", result, next)
			}
		})
	}
}

func TestPlanApplyKickEnchantOnlyRequiresAnEnchantedWeapon(t *testing.T) {
	s := kickTestState(t)
	npc := s.NPCs["wolf"]
	kickTestFlag(&npc.Body.Flags, 22, true)
	s.NPCs["wolf"] = npc
	proposal, err := s.PlanKick("actor", "늑대", 100, func(int, int) int { t.Fatal("enchant gate consumed RNG"); return 0 })
	if err != nil || proposal.Status != kickStatusEnchantOnly || !proposal.Rejected {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	if _, _, err := s.ApplyKick(proposal); err != nil {
		t.Fatal(err)
	}

	actor := s.Players["actor"]
	actor.Items = &ItemCollection{Items: map[string]Item{"weapon": {Object: LegacyObject{Name: "검", Adjustment: 1}}}, Ready: [20]string{KickWieldSlot: "weapon"}}
	s.Players["actor"] = actor
	proposal, err = s.PlanKick("actor", "늑대", 100, kickTestRoll(t, 1, 20, 4))
	if err != nil || proposal.Status != kickStatusReady || proposal.WeaponID != "weapon" || proposal.WeaponAdjustment != 1 {
		t.Fatalf("enchanted proposal=%+v err=%v", proposal, err)
	}
	if _, _, err = s.ApplyKick(proposal); err != nil {
		t.Fatal(err)
	}
}

func TestPlanKickPlayerPVPFamilyWarCharmAndVisibilityGates(t *testing.T) {
	base := kickPlayerState(t)
	for _, tc := range []struct {
		name       string
		mutate     func(*State)
		wantStatus string
		wantErr    error
	}{
		{name: "safe-room", mutate: func(s *State) {
			room := s.Rooms[1]
			kickTestFlag(&room.Resource.Flags, KickRoomNoKillFlag, true)
			s.Rooms[1] = room
		}, wantStatus: kickStatusSafeRoom},
		{name: "actor-lawful", mutate: func(s *State) {}, wantStatus: kickStatusActorLawful},
		{name: "target-lawful", mutate: func(s *State) {
			actor := s.Players["actor"]
			kickTestFlag(&actor.Body.Flags, KickPlayerChaosFlag, true)
			s.Players["actor"] = actor
		}, wantStatus: kickStatusTargetLawful},
		{name: "war-unresolved", mutate: func(s *State) {
			actor := s.Players["actor"]
			target := s.Players["target"]
			kickTestFlag(&actor.Body.Flags, KickPlayerFamilyFlag, true)
			kickTestFlag(&target.Body.Flags, KickPlayerFamilyFlag, true)
			s.Players["actor"], s.Players["target"] = actor, target
		}, wantErr: ErrKickWarStateUnresolved},
		{name: "charm-unresolved", mutate: func(s *State) {
			actor := s.Players["actor"]
			target := s.Players["target"]
			kickTestFlag(&actor.Body.Flags, KickPlayerChaosFlag, true)
			kickTestFlag(&target.Body.Flags, KickPlayerChaosFlag, true)
			kickTestFlag(&target.Body.Flags, KickPlayerCharmFlag, true)
			s.Players["actor"], s.Players["target"] = actor, target
		}, wantErr: ErrKickCharmStateUnresolved},
		{name: "invisible-target", mutate: func(s *State) {
			target := s.Players["target"]
			kickTestFlag(&target.Body.Flags, KickPlayerInvisibleFlag, true)
			s.Players["target"] = target
		}, wantStatus: kickStatusNoTarget},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := base
			// The fixture has pointer-free values but maps/slices must be copied
			// before each gate mutation to keep subtests independent.
			raw, err := json.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			s, err = DecodeState(raw)
			if err != nil {
				t.Fatal(err)
			}
			tc.mutate(&s)
			if err := s.Validate(); err != nil {
				t.Fatal(err)
			}
			proposal, err := s.PlanKick("actor", "Bob", 100, func(int, int) int { t.Fatal("PVP gate consumed RNG"); return 0 })
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err=%v want=%v", err, tc.wantErr)
				}
				return
			}
			if err != nil || proposal.Status != tc.wantStatus || !proposal.NoOp || !proposal.Rejected {
				t.Fatalf("proposal=%+v err=%v", proposal, err)
			}
		})
	}

	// A war pair is allowed only when the imported active pair matches both
	// family IDs, and survival/chaos then permits the actual non-lethal hit.
	war := base
	actor := war.Players["actor"]
	target := war.Players["target"]
	kickTestFlag(&actor.Body.Flags, KickPlayerFamilyFlag, true)
	kickTestFlag(&actor.Body.Flags, KickPlayerChaosFlag, true)
	actor.Body.Daily[kickFamilyDaily].Max = 1
	kickTestFlag(&target.Body.Flags, KickPlayerFamilyFlag, true)
	kickTestFlag(&target.Body.Flags, KickPlayerChaosFlag, true)
	target.Body.Daily[kickFamilyDaily].Max = 2
	war.Players["actor"], war.Players["target"] = actor, target
	war.War = &FamilyWar{Active: 0x12}
	proposal, err := war.PlanKick("actor", "Bob", 100, kickTestRoll(t, 1, 20, 1))
	if err != nil || proposal.Status != kickStatusReady {
		t.Fatalf("war proposal=%+v err=%v", proposal, err)
	}
}

func TestPlanKickCooldownAndLethalFailClosed(t *testing.T) {
	s := kickTestState(t)
	actor := s.Players["actor"]
	actor.Body.Timers[KickTimerIndex] = LegacyTimer{LastTime: 100, Interval: 3}
	s.Players["actor"] = actor
	proposal, err := s.PlanKick("actor", "늑대", 101, func(int, int) int { t.Fatal("cooldown consumed RNG"); return 0 })
	if err != nil || !proposal.NoOp || !proposal.Cooldown || proposal.WaitSeconds != 2 || proposal.Status != kickStatusCooldown {
		t.Fatalf("cooldown=%+v err=%v", proposal, err)
	}
	if _, _, err := s.ApplyKick(proposal); err != nil {
		t.Fatal(err)
	}

	s = kickTestState(t)
	npc := s.NPCs["wolf"]
	npc.Body.HPCurrent = 1
	s.NPCs["wolf"] = npc
	original := s
	_, err = s.PlanKick("actor", "늑대", 100, kickTestRoll(t, 1, 20, 4))
	if !errors.Is(err, ErrKickDeathTransitionPending) {
		t.Fatalf("lethal err=%v", err)
	}
	if !reflect.DeepEqual(s, original) {
		t.Fatal("lethal plan exposed a partial state")
	}
}
