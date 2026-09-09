package world

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func backstabFixture() State {
	actor := PlayerState{
		Body: LegacyMonster{
			Name:      "Alice",
			RoomID:    1,
			Type:      0,
			Class:     backstabThiefClass,
			Level:     10,
			Stats:     [5]byte{10, 10, 10, 10, 10},
			HPMax:     100,
			HPCurrent: 100,
			Thaco:     10,
		},
		Online: true,
		Items: &ItemCollection{
			Items: map[string]Item{
				"knife": {Object: LegacyObject{
					Name:         "단검",
					Type:         backstabSharpType,
					ShotsMax:     5,
					ShotsCurrent: 5,
					DiceCount:    1,
					DiceSides:    4,
				}},
			},
			Ready: [20]string{backstabWieldSlot: "knife"},
		},
	}
	setSettingFlag(&actor.Body, backstabPlayerHidden, true)

	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}},
				PlayerIDs: []string{"a"},
				NPCIDs:    []string{"goblin"},
			},
		},
		Players: map[string]PlayerState{"a": actor},
		NPCs: map[string]NPCState{
			"goblin": {
				Body: LegacyMonster{
					Name:       "고블린",
					RoomID:     1,
					Type:       1,
					Class:      4,
					HPMax:      100,
					HPCurrent:  100,
					Armor:      0,
					Experience: 100,
				},
				Enemies: []NPCEnemy{},
			},
		},
	}
}

func backstabRollScript(t *testing.T, values ...int) func(int, int) int {
	t.Helper()
	index := 0
	return func(lo, hi int) int {
		if index >= len(values) {
			t.Fatalf("unexpected backstab random request %d..%d", lo, hi)
		}
		value := values[index]
		index++
		if value < lo || value > hi {
			t.Fatalf("backstab random value %d outside %d..%d", value, lo, hi)
		}
		return value
	}
}

func TestPlanApplyBackstabHitUsesCanonicalWeaponAndNPCState(t *testing.T) {
	s := backstabFixture()
	original := s.clone()
	proposal, err := s.PlanBackstab("a", "고블린", 100, backstabRollScript(t, 20, 4, 29))
	if err != nil {
		t.Fatal(err)
	}
	if proposal.TargetID != "goblin" || proposal.TargetKind != BackstabTargetNPC || proposal.Roll != 20 || proposal.Chance != 12 || proposal.Damage != 8 || proposal.TargetHP != 92 || proposal.Interval != 1 || !proposal.Attempted || !proposal.Hit || !proposal.Succeeded {
		t.Fatalf("proposal=%+v", proposal)
	}

	next, result, err := s.ApplyBackstab(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || !result.Broadcast || result.Damage != 8 || result.TargetHP != 92 || result.ProficiencyAward != 8 {
		t.Fatalf("result=%+v", result)
	}
	if next.NPCs["goblin"].Body.HPCurrent != 92 || len(next.NPCs["goblin"].Enemies) != 1 || next.NPCs["goblin"].Enemies[0].Damage != 8 {
		t.Fatalf("npc=%+v", next.NPCs["goblin"])
	}
	if next.Players["a"].Body.Proficiency[backstabSharpType] != 8 {
		t.Fatalf("proficiency=%d", next.Players["a"].Body.Proficiency[backstabSharpType])
	}
	actor := next.Players["a"]
	if flag(actor.Body.Flags[:], backstabPlayerHidden) || actor.Body.Timers[backstabTimerIndex] != (LegacyTimer{LastTime: 100, Interval: 1}) {
		t.Fatalf("actor=%+v", actor.Body)
	}
	if result.Event == nil || len(result.Event.Texts) != 1 || !strings.Contains(result.Event.Texts[0], "옆구리를") || result.Event.TargetText != "" {
		t.Fatalf("event=%+v", result.Event)
	}
	if !reflect.DeepEqual(s, original) {
		t.Fatal("planning mutated the input snapshot")
	}
}

func TestBackstabMissStillRevealsAndRecordsHostility(t *testing.T) {
	s := backstabFixture()
	actor := s.Players["a"]
	setSettingFlag(&actor.Body, backstabPlayerHidden, false)
	s.Players["a"] = actor

	proposal, err := s.PlanBackstab("a", "고블린", 100, backstabRollScript(t, 20))
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Chance != 21 || proposal.Roll != 20 || proposal.Hit || proposal.Succeeded || proposal.Damage != 0 || proposal.Interval != 3 {
		t.Fatalf("proposal=%+v", proposal)
	}
	next, result, err := s.ApplyBackstab(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if result.Hit || result.Succeeded || !strings.Contains(result.Response, "허공을 쳤습니다") {
		t.Fatalf("result=%+v", result)
	}
	if next.NPCs["goblin"].Body.HPCurrent != 100 || len(next.NPCs["goblin"].Enemies) != 1 || next.NPCs["goblin"].Enemies[0].Damage != 0 {
		t.Fatalf("npc=%+v", next.NPCs["goblin"])
	}
	if next.Players["a"].Body.Timers[backstabTimerIndex].Interval != 3 {
		t.Fatalf("timer=%+v", next.Players["a"].Body.Timers[backstabTimerIndex])
	}
}

func TestBackstabBrokenWeaponMovesReadyRootWithoutRolling(t *testing.T) {
	s := backstabFixture()
	weapon := s.Players["a"].Items.Items["knife"]
	weapon.Object.ShotsCurrent = 0
	player := s.Players["a"]
	player.Items.Items["knife"] = weapon
	s.Players["a"] = player

	proposal, err := s.PlanBackstab("a", "고블린", 100, func(int, int) int {
		t.Fatal("a broken weapon must not consume RNG")
		return 0
	})
	if err != nil {
		t.Fatal(err)
	}
	if !proposal.WeaponBroken || proposal.Attempted || proposal.Hit || proposal.Damage != 0 || proposal.Interval != 2 {
		t.Fatalf("proposal=%+v", proposal)
	}
	next, result, err := s.ApplyBackstab(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if !result.WeaponBroken || result.Attempted || next.Players["a"].Items.Ready[backstabWieldSlot] != "" || !reflect.DeepEqual(next.Players["a"].Items.Inventory, []string{"knife"}) {
		t.Fatalf("result=%+v items=%+v", result, next.Players["a"].Items)
	}
	if len(next.NPCs["goblin"].Enemies) != 1 || next.NPCs["goblin"].Body.HPCurrent != 100 {
		t.Fatalf("npc=%+v", next.NPCs["goblin"])
	}
}

func TestBackstabCooldownAndAuthorizationGatesAreNoOps(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*State)
		want   string
	}{
		{name: "cooldown", mutate: func(s *State) {
			p := s.Players["a"]
			p.Body.Timers[backstabTimerIndex] = LegacyTimer{LastTime: 100, Interval: 5}
			s.Players["a"] = p
		}, want: "3초동안 기다리세요"},
		{name: "wrong class", mutate: func(s *State) {
			p := s.Players["a"]
			p.Body.Class = 4
			s.Players["a"] = p
		}, want: "도둑과 자객만"},
		{name: "blind", mutate: func(s *State) {
			p := s.Players["a"]
			setSettingFlag(&p.Body, backstabPlayerBlind, true)
			s.Players["a"] = p
		}, want: "누구를 기습"},
		{name: "no weapon", mutate: func(s *State) {
			p := s.Players["a"]
			p.Items = &ItemCollection{Items: map[string]Item{}}
			s.Players["a"] = p
		}, want: "도나 검종류"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := backstabFixture()
			tc.mutate(&s)
			before := s.clone()
			proposal, err := s.PlanBackstab("a", "고블린", 102, func(int, int) int {
				t.Fatal("a rejected backstab must not consume RNG")
				return 0
			})
			if err != nil {
				t.Fatal(err)
			}
			if !proposal.NoOp || !strings.Contains(proposal.Response, tc.want) {
				t.Fatalf("proposal=%+v", proposal)
			}
			next, result, err := s.ApplyBackstab(proposal)
			if err != nil {
				t.Fatal(err)
			}
			if result.Changed || !reflect.DeepEqual(next, before) {
				t.Fatalf("no-op changed state: result=%+v next=%+v before=%+v", result, next, before)
			}
		})
	}
}

func TestBackstabMagicOnlyNPCCommitsRevealTimerAndEnemy(t *testing.T) {
	s := backstabFixture()
	npc := s.NPCs["goblin"]
	setSettingFlag(&npc.Body, backstabNPCNoMagic, true)
	s.NPCs["goblin"] = npc

	proposal, err := s.PlanBackstab("a", "고블린", 100, func(int, int) int {
		t.Fatal("magic-only backstab must not roll")
		return 0
	})
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Attempted || proposal.Hit || proposal.Damage != 0 || !proposal.EnemyAdded || !strings.Contains(proposal.Response, "아무 소용") {
		t.Fatalf("proposal=%+v", proposal)
	}
	next, result, err := s.ApplyBackstab(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if result.Attempted || result.Hit || len(next.NPCs["goblin"].Enemies) != 1 || next.Players["a"].Body.Timers[backstabTimerIndex].LastTime != 100 {
		t.Fatalf("result=%+v next=%+v", result, next)
	}
}

func TestBackstabCaretakerNPCTakesOneHPButKeepsRawEnemyDamage(t *testing.T) {
	s := backstabFixture()
	npc := s.NPCs["goblin"]
	npc.Body.Class = backstabSubDMClass
	s.NPCs["goblin"] = npc

	proposal, err := s.PlanBackstab("a", "고블린", 100, backstabRollScript(t, 20, 4, 29))
	if err != nil {
		t.Fatal(err)
	}
	if proposal.RawDamage != 8 || proposal.Damage != 1 || proposal.ProficiencyDamage != 8 || proposal.TargetHP != 99 {
		t.Fatalf("proposal=%+v", proposal)
	}
	next, result, err := s.ApplyBackstab(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if result.Damage != 1 || result.ProficiencyAward != 8 || next.NPCs["goblin"].Body.HPCurrent != 99 || next.NPCs["goblin"].Enemies[0].Damage != 8 {
		t.Fatalf("result=%+v npc=%+v", result, next.NPCs["goblin"])
	}
}

func TestBackstabPlayerTargetUsesChaosGateAndPrivateDamageProjection(t *testing.T) {
	s := backstabFixture()
	delete(s.NPCs, "goblin")
	room := s.Rooms[1]
	room.NPCIDs = nil
	room.PlayerIDs = []string{"a", "b"}
	s.Rooms[1] = room
	actor := s.Players["a"]
	setSettingFlag(&actor.Body, backstabPlayerChaos, true)
	s.Players["a"] = actor
	target := PlayerState{Body: LegacyMonster{Name: "Bob", RoomID: 1, Type: 0, HPMax: 100, HPCurrent: 100}, Online: true}
	setSettingFlag(&target.Body, backstabPlayerChaos, true)
	s.Players["b"] = target

	proposal, err := s.PlanBackstab("a", "Bob", 100, backstabRollScript(t, 20, 4, 29))
	if err != nil {
		t.Fatal(err)
	}
	if proposal.TargetKind != BackstabTargetPlayer || proposal.TargetID != "b" || proposal.Damage != 8 || proposal.TargetHP != 92 || proposal.EnemyAdded {
		t.Fatalf("proposal=%+v", proposal)
	}
	next, result, err := s.ApplyBackstab(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if next.Players["b"].Body.HPCurrent != 92 || result.Event == nil || result.Event.TargetKind != BackstabTargetPlayer || !strings.Contains(result.Event.TargetText, "8 만큼의 피해") {
		t.Fatalf("result=%+v target=%+v", result, next.Players["b"])
	}
}

func TestBackstabRejectsLethalSwingBeforeReturningPartialState(t *testing.T) {
	s := backstabFixture()
	npc := s.NPCs["goblin"]
	npc.Body.HPCurrent = 5
	s.NPCs["goblin"] = npc
	original := s.clone()

	_, err := s.PlanBackstab("a", "고블린", 100, backstabRollScript(t, 20, 4, 29))
	if !errors.Is(err, ErrBackstabDeathTransitionPending) {
		t.Fatalf("lethal error=%v", err)
	}
	if !reflect.DeepEqual(s, original) {
		t.Fatal("lethal planning returned or applied a partial state")
	}
}
