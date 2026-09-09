package world

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func turnStateFixture() State {
	s := State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}},
				PlayerIDs: []string{"cleric"},
				NPCIDs:    []string{"undead"},
			},
		},
		Players: map[string]PlayerState{
			"cleric": {
				Body: LegacyMonster{
					Name: "아린", Type: turnPlayerType, Class: TurnClericClass,
					Level: 10, Stats: [5]byte{10, 10, 10, 10, 10},
					HPMax: 100, HPCurrent: 100, RoomID: 1,
				},
				Online: true,
			},
		},
		NPCs: map[string]NPCState{
			"undead": {
				Body: LegacyMonster{
					Name: "해골", Type: turnMonsterType, Class: 4, Level: 10,
					HPMax: 100, HPCurrent: 100, RoomID: 1,
				},
				Enemies: []NPCEnemy{},
			},
		},
	}
	npc := s.NPCs["undead"]
	setSettingFlag(&npc.Body, turnNPCUndeadFlag, true)
	s.NPCs["undead"] = npc
	return s
}

func turnRollScript(t *testing.T, values ...int) func(int, int) int {
	t.Helper()
	index := 0
	return func(low, high int) int {
		if index >= len(values) {
			t.Fatalf("unexpected turn random request %d..%d", low, high)
		}
		value := values[index]
		index++
		if value < low || value > high {
			t.Fatalf("turn random value %d outside %d..%d", value, low, high)
		}
		return value
	}
}

func TestPlanApplyTurnNonlethalSuccessAddsEnemyDamageAndBroadcast(t *testing.T) {
	s := turnStateFixture()
	actor := s.Players["cleric"]
	actor.Body.Timers[TurnAttackTimerIndex] = LegacyTimer{Interval: 7, Misc: 9}
	s.Players["cleric"] = actor
	original := s.clone()
	proposal, err := s.PlanTurn("cleric", "해골", 100, turnRollScript(t, 1, 1), nil)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.TargetID != "undead" || proposal.TargetKind != TurnTargetNPC || proposal.Chance != 25 || proposal.Roll != 1 || proposal.DisintegrateRoll != 1 || !proposal.Attempted || !proposal.Succeeded || proposal.Disintegrated || proposal.Damage != 50 || proposal.TargetHP != 50 || !proposal.EnemyAdded || proposal.EnemyDamage != 50 {
		t.Fatalf("proposal=%+v", proposal)
	}
	if !reflect.DeepEqual(s, original) {
		t.Fatal("planning mutated the input snapshot")
	}
	next, result, err := s.ApplyTurn(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "turn" || !result.Changed || !result.Broadcast || !result.Attempted || !result.Succeeded || result.Killed || result.Damage != 50 || result.TargetHP != 50 || result.Event == nil || !strings.Contains(result.Response, "50만큼") {
		t.Fatalf("result=%+v", result)
	}
	if next.NPCs["undead"].Body.HPCurrent != 50 || len(next.NPCs["undead"].Enemies) != 1 || next.NPCs["undead"].Enemies[0].Damage != 50 {
		t.Fatalf("next npc=%+v", next.NPCs["undead"])
	}
	if next.Players["cleric"].Body.Timers[TurnCooldownTimerIndex] != (LegacyTimer{LastTime: 100, Interval: TurnCooldownSeconds}) || next.Players["cleric"].Body.Timers[TurnAttackTimerIndex] != (LegacyTimer{LastTime: 100, Interval: 7, Misc: 9}) {
		t.Fatalf("actor timers=%+v", next.Players["cleric"].Body.Timers)
	}
}

func TestTurnDisintegratesAndComposesCanonicalNPCDeath(t *testing.T) {
	s := turnStateFixture()
	npc := s.NPCs["undead"]
	npc.Body.HPMax, npc.Body.HPCurrent, npc.Body.Experience = 1, 1, 40
	s.NPCs["undead"] = npc
	proposal, err := s.PlanTurn("cleric", "해골", 100, turnRollScript(t, 1, 100), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !proposal.Disintegrated || !proposal.Killed || proposal.TargetHP != 0 || proposal.Damage != 1 || proposal.EnemyDamage != 1 || proposal.Death == nil {
		t.Fatalf("proposal=%+v", proposal)
	}
	next, result, err := s.ApplyTurn(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Killed || !result.Disintegrated || result.Death == nil || result.Death.ExperienceAward != 40 {
		t.Fatalf("result=%+v", result)
	}
	if _, ok := next.NPCs["undead"]; ok || next.Players["cleric"].Body.Experience != 40 {
		t.Fatalf("death state=%+v player=%+v", next.NPCs, next.Players["cleric"].Body)
	}
	if err := next.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestTurnCooldownRevealsBeforeWaitingWithoutRandomness(t *testing.T) {
	s := turnStateFixture()
	actor := s.Players["cleric"]
	setSettingFlag(&actor.Body, turnActorInvisibleFlag, true)
	actor.Body.Timers[TurnCooldownTimerIndex] = LegacyTimer{LastTime: 100, Interval: TurnCooldownSeconds}
	s.Players["cleric"] = actor
	p, err := s.PlanTurn("cleric", "해골", 101, func(int, int) int {
		t.Fatal("turn cooldown consumed RNG")
		return 0
	}, nil)
	if err != nil || !p.NoOp || !p.Cooldown || p.WaitSeconds != 29 || !p.ClearInvisible || p.Attempted || p.Response == "" {
		t.Fatalf("proposal=%+v err=%v", p, err)
	}
	next, result, err := s.ApplyTurn(p)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Cooldown || !result.Changed || !result.ClearInvisible || !result.Broadcast || result.Event == nil || turnFlag(next.Players["cleric"].Body, turnActorInvisibleFlag) {
		t.Fatalf("result=%+v next=%+v", result, next.Players["cleric"])
	}
}

func turnFlag(body LegacyMonster, bit uint) bool { return body.Flags[bit/8]&(1<<(bit%8)) != 0 }

func TestTurnPermissionTargetAndUnresolvedCombatFailClosed(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*State)
		want   error
	}{
		{name: "permission", mutate: func(s *State) { p := s.Players["cleric"]; p.Body.Class = 4; s.Players["cleric"] = p }, want: nil},
		{name: "not undead", mutate: func(s *State) {
			n := s.NPCs["undead"]
			n.Body.Flags[turnNPCUndeadFlag/8] &^= 1 << (turnNPCUndeadFlag % 8)
			s.NPCs["undead"] = n
		}, want: nil},
		{name: "enemy unresolved", mutate: func(s *State) { n := s.NPCs["undead"]; n.Enemies = nil; s.NPCs["undead"] = n }, want: ErrTurnCombatSideEffectPending},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := turnStateFixture()
			tc.mutate(&s)
			calls := 0
			_, err := s.PlanTurn("cleric", "해골", 100, func(int, int) int { calls++; return 1 }, nil)
			if tc.want != nil {
				if !errors.Is(err, tc.want) || calls != 0 {
					t.Fatalf("err=%v calls=%d", err, calls)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestTurnNoOpResponsesRemainApplicable(t *testing.T) {
	for _, tc := range []struct {
		name     string
		target   string
		class    byte
		wantText string
		wantAuth bool
	}{
		{name: "prompt", wantText: "누구에게", wantAuth: false},
		{name: "missing", target: "유령", wantText: "그런 괴물", wantAuth: true},
		{name: "unauthorized", target: "해골", class: 4, wantText: "불제자", wantAuth: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := turnStateFixture()
			if tc.class != 0 {
				actor := s.Players["cleric"]
				actor.Body.Class = tc.class
				s.Players["cleric"] = actor
			}
			p, err := s.PlanTurn("cleric", tc.target, 100, func(int, int) int { t.Fatal("no-op consumed RNG"); return 1 }, nil)
			if err != nil || !p.NoOp || p.Authorized != tc.wantAuth || !strings.Contains(p.Response, tc.wantText) {
				t.Fatalf("proposal=%+v err=%v", p, err)
			}
			next, result, err := s.ApplyTurn(p)
			if err != nil || !result.NoOp || !strings.Contains(result.Response, tc.wantText) || !reflect.DeepEqual(next, s) {
				t.Fatalf("next=%+v result=%+v err=%v", next, result, err)
			}
		})
	}
}

func TestTurnRejectsUnsafeDeathBeforeRandomDraw(t *testing.T) {
	s := turnStateFixture()
	npc := s.NPCs["undead"]
	npc.Body.HPCurrent = 1
	npc.Body.Flags[turnNPCPermanentFlag/8] |= 1 << (turnNPCPermanentFlag % 8)
	s.NPCs["undead"] = npc
	calls := 0
	_, err := s.PlanTurn("cleric", "해골", 100, func(int, int) int { calls++; return 1 }, nil)
	if !errors.Is(err, ErrTurnDeathTransitionPending) || calls != 0 {
		t.Fatalf("unsafe death admitted: err=%v calls=%d", err, calls)
	}
}

func TestTurnLethalPreflightDoesNotConsumeHostItemAllocator(t *testing.T) {
	s := turnStateFixture()
	room := s.Rooms[1]
	room.Items = &ItemCollection{Items: map[string]Item{}}
	s.Rooms[1] = room
	npc := s.NPCs["undead"]
	npc.Body.HPMax, npc.Body.HPCurrent, npc.Body.Experience, npc.Body.Gold = 1, 1, 40, 12
	npc.Body.Inventory = []LegacyObject{{Name: "검", Value: 7}}
	s.NPCs["undead"] = npc
	allocated := 0
	p, err := s.PlanTurnWithOptions("cleric", "해골", 100, turnRollScript(t, 1, 100), TurnOptions{
		Allocate: func() (string, error) {
			allocated++
			return fmt.Sprintf("turn-item-%d", allocated), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !p.Killed || allocated != 2 {
		t.Fatalf("proposal=%+v allocated=%d", p, allocated)
	}
	next, result, err := s.ApplyTurn(p)
	if err != nil || !result.Killed || len(next.Rooms[1].Items.Items) != 2 {
		t.Fatalf("result=%+v room=%+v err=%v", result, next.Rooms[1].Items, err)
	}
}
