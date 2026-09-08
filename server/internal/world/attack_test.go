package world

import (
	"reflect"
	"testing"
)

func npcAttackFixture(t *testing.T) State {
	t.Helper()
	s := stateFixture()
	p := s.Players["a"]
	p.Body = LegacyMonster{Name: "Alice", RoomID: 1, Type: 0, Class: 4, Level: 1, Stats: [5]byte{10, 10, 10, 10, 10}, HPMax: 100, HPCurrent: 40, DiceCount: 1, DiceSides: 5}
	p.Items = &ItemCollection{Items: map[string]Item{}}
	s.Players["a"] = p
	r := s.Rooms[1]
	r.NPCIDs = []string{"wolf-id"}
	s.Rooms[1] = r
	s.NPCs = map[string]NPCState{"wolf-id": {Body: LegacyMonster{Name: "늑대", RoomID: 1, Type: 1, Class: 4, Level: 1, Armor: 100, HPMax: 50, HPCurrent: 50, DiceCount: 1, DiceSides: 4}, Enemies: []NPCEnemy{}}}
	return s
}

func TestSelectNPCInRoomUsesExactAuthoritativeName(t *testing.T) {
	s := npcAttackFixture(t)
	if got, err := s.SelectNPCInRoom("a", "늑대"); err != nil || got != "wolf-id" {
		t.Fatalf("target=%q err=%v", got, err)
	}
	if _, err := s.SelectNPCInRoom("a", "늑"); err == nil {
		t.Fatal("prefix target accepted")
	}
}

func TestPlanNPCMeleeAttackPersistsHitAndEnemyRelation(t *testing.T) {
	s := npcAttackFixture(t)
	p := s.Players["a"]
	p.Body.Flags[0] |= 1<<1 | 1<<2
	s.Players["a"] = p
	original := s
	next, result, err := s.PlanNPCMeleeAttack("a", "wolf-id", func(_, hi int) int { return hi })
	if err != nil {
		t.Fatal(err)
	}
	if !result.Hit || result.Damage != 5 || result.TargetHP != 45 || result.Critical || next.NPCs["wolf-id"].Body.HPCurrent != 45 {
		t.Fatalf("result=%+v npc=%+v", result, next.NPCs["wolf-id"])
	}
	if len(next.NPCs["wolf-id"].Enemies) != 1 || next.NPCs["wolf-id"].Enemies[0].Target != (EntityRef{Kind: "player", ID: "a"}) || next.NPCs["wolf-id"].Enemies[0].Damage != 5 {
		t.Fatalf("enemy=%+v", next.NPCs["wolf-id"].Enemies)
	}
	if next.Players["a"].Body.Flags[0]&(1<<1|1<<2) != 0 {
		t.Fatal("attack did not reveal actor")
	}
	if !reflect.DeepEqual(s, original) || s.NPCs["wolf-id"].Body.HPCurrent != 50 {
		t.Fatal("attack mutated input")
	}
}

func TestPlanNPCMeleeAttackMissStillRecordsEnemyWithoutDamage(t *testing.T) {
	s := npcAttackFixture(t)
	next, result, err := s.PlanNPCMeleeAttack("a", "wolf-id", func(lo, _ int) int { return lo })
	if err != nil || result.Hit || result.Damage != 0 || result.TargetHP != 50 || next.NPCs["wolf-id"].Body.HPCurrent != 50 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if len(next.NPCs["wolf-id"].Enemies) != 1 {
		t.Fatal("miss did not establish NPC hostility")
	}
}

func TestPlanNPCMeleeAttackCommitsLethalNPCDeathAtomically(t *testing.T) {
	s := npcAttackFixture(t)
	npc := s.NPCs["wolf-id"]
	npc.Body.HPCurrent = 1
	npc.Body.Experience = 100
	npc.Body.Alignment = -20
	s.NPCs["wolf-id"] = npc
	r := s.Rooms[1]
	r.NPCIDs = []string{"wolf-id", "guard-id"}
	s.Rooms[1] = r
	s.NPCs["guard-id"] = NPCState{
		Body:    LegacyMonster{Name: "경비", Type: 1, RoomID: 1, HPMax: 10, HPCurrent: 10},
		Enemies: []NPCEnemy{{Target: EntityRef{Kind: "npc", ID: "wolf-id"}}},
	}
	s.ActiveNPCIDs = []string{"wolf-id", "guard-id"}
	next, result, err := s.PlanNPCMeleeAttack("a", "wolf-id", func(_, hi int) int { return hi })
	if err != nil || !result.Killed || result.ExperienceAward != 2 || result.TargetHP != 0 {
		t.Fatalf("lethal attack result=%+v err=%v", result, err)
	}
	if _, exists := next.NPCs["wolf-id"]; exists || len(next.Rooms[1].NPCIDs) != 1 || next.Rooms[1].NPCIDs[0] != "guard-id" || !reflect.DeepEqual(next.ActiveNPCIDs, []string{"guard-id"}) {
		t.Fatalf("dead NPC remained in canonical indexes: rooms=%+v active=%+v npcs=%+v", next.Rooms[1].NPCIDs, next.ActiveNPCIDs, next.NPCs)
	}
	if next.Players["a"].Body.Experience != 2 || next.Players["a"].Body.Alignment != 4 {
		t.Fatalf("killer reward not applied: %+v", next.Players["a"].Body)
	}
	if len(next.NPCs["guard-id"].Enemies) != 0 {
		t.Fatalf("dead NPC enemy reference remained: %+v", next.NPCs["guard-id"].Enemies)
	}
	if s.NPCs["wolf-id"].Body.HPCurrent != 1 {
		t.Fatal("attack mutated input snapshot")
	}
}

func attackWeaponFixture(t *testing.T, shots int16) State {
	t.Helper()
	s := npcAttackFixture(t)
	p := s.Players["a"]
	p.Items = &ItemCollection{
		Items: map[string]Item{
			"sword": {Object: LegacyObject{
				Name:         "검",
				Type:         0,
				Wear:         20,
				DiceCount:    1,
				DiceSides:    4,
				ShotsMax:     5,
				ShotsCurrent: shots,
			}},
		},
		Ready: [20]string{19: "sword"},
	}
	s.Players["a"] = p
	return s
}

func TestPlanNPCMeleeAttackMovesAlreadyBrokenWeaponBeforeSwing(t *testing.T) {
	s := attackWeaponFixture(t, 0)
	next, result, err := s.PlanNPCMeleeAttack("a", "wolf-id", func(_, _ int) int {
		t.Fatal("broken weapon must not consume a swing roll")
		return 0
	})
	if err != nil || !result.WeaponBroken || result.Hit || result.Damage != 0 || result.TargetHP != 50 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if next.Players["a"].Items.Ready[19] != "" || !reflect.DeepEqual(next.Players["a"].Items.Inventory, []string{"sword"}) {
		t.Fatalf("broken weapon ownership=%+v", next.Players["a"].Items)
	}
	if next.NPCs["wolf-id"].Body.HPCurrent != 50 || len(next.NPCs["wolf-id"].Enemies) != 1 {
		t.Fatalf("broken weapon changed target=%+v", next.NPCs["wolf-id"])
	}
	if err := next.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestPlanNPCMeleeAttackDropsWeaponOnNonCriticalHit(t *testing.T) {
	s := attackWeaponFixture(t, 5)
	roll := func(lo, hi int) int {
		switch {
		case lo == 1 && hi == 30:
			return hi // hit
		case lo == 1 && hi == 4:
			return hi // damage
		case lo == 1 && hi == 100:
			return 100 // no critical, then the drop roll is replaced below
		default:
			return hi
		}
	}
	// The two 1..100 calls are critical and drop. Keep their order explicit so
	// this fixture also detects accidental RNG consumption changes.
	calls := 0
	roll = func(lo, hi int) int {
		if lo == 1 && hi == 30 {
			return hi
		}
		if lo == 1 && hi == 4 {
			return hi
		}
		if lo == 1 && hi == 100 {
			calls++
			if calls == 1 {
				return 100 // not critical
			}
			return 1 // drop at p=0
		}
		t.Fatalf("unexpected random request %d..%d", lo, hi)
		return 0
	}
	next, result, err := s.PlanNPCMeleeAttack("a", "wolf-id", roll)
	if err != nil || !result.Hit || !result.WeaponDropped || result.Damage != 0 || result.TargetHP != 50 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if next.Players["a"].Items.Ready[19] != "" || !reflect.DeepEqual(next.Players["a"].Items.Inventory, []string{"sword"}) {
		t.Fatalf("dropped weapon ownership=%+v", next.Players["a"].Items)
	}
	if next.NPCs["wolf-id"].Body.HPCurrent != 50 {
		t.Fatalf("drop changed target HP=%d", next.NPCs["wolf-id"].Body.HPCurrent)
	}
	if err := next.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestPlanNPCMeleeAttackAwardsProficiencyAndConsumesWeaponShot(t *testing.T) {
	s := attackWeaponFixture(t, 5)
	target := s.NPCs["wolf-id"]
	target.Body.Experience = 100
	target.Body.HPMax = 100
	s.NPCs["wolf-id"] = target
	calls := 0
	roll := func(lo, hi int) int {
		switch {
		case lo == 1 && hi == 30:
			return hi
		case lo == 1 && hi == 4:
			return hi
		case lo == 1 && hi == 100:
			calls++
			if calls == 1 {
				return 100 // not critical
			}
			return 100 // no weapon drop
		case lo == 0 && hi == 3:
			return 0 // consume one shot
		default:
			t.Fatalf("unexpected random request %d..%d", lo, hi)
			return 0
		}
	}
	next, result, err := s.PlanNPCMeleeAttack("a", "wolf-id", roll)
	if err != nil || result.Damage != 4 || result.ProficiencyAward != 4 || result.WeaponBroken || result.WeaponDropped {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if got := next.Players["a"].Body.Proficiency[0]; got != 4 {
		t.Fatalf("proficiency=%d", got)
	}
	if got := next.Players["a"].Items.Items["sword"].Object.ShotsCurrent; got != 4 {
		t.Fatalf("shots=%d", got)
	}
	if next.Players["a"].Items.Ready[19] != "sword" || len(next.Players["a"].Items.Inventory) != 0 {
		t.Fatalf("weapon ownership=%+v", next.Players["a"].Items)
	}
}

func TestPlanNPCMeleeAttackShattersCriticalWeaponAndRemovesSubtree(t *testing.T) {
	s := attackWeaponFixture(t, 5)
	p := s.Players["a"]
	weapon := p.Items.Items["sword"]
	weapon.Object.Flags[objectAlwaysCriticalFlag/8] |= 1 << (objectAlwaysCriticalFlag % 8)
	weapon.Contents = []string{"gem"}
	p.Items.Items["sword"] = weapon
	p.Items.Items["gem"] = Item{Object: LegacyObject{Name: "보석"}}
	s.Players["a"] = p
	calls := 0
	roll := func(lo, hi int) int {
		switch {
		case lo == 1 && hi == 30:
			return hi
		case lo == 1 && hi == 4:
			return hi
		case lo == 1 && hi == 100:
			calls++
			return 100 // the always-critical flag controls the branch
		case lo == 3 && hi == 6:
			return hi
		default:
			t.Fatalf("unexpected random request %d..%d", lo, hi)
			return 0
		}
	}
	next, result, err := s.PlanNPCMeleeAttack("a", "wolf-id", roll)
	if err != nil || !result.Critical || !result.WeaponBroken || result.Damage != 24 || result.TargetHP != 26 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, ok := next.Players["a"].Items.Items["sword"]; ok {
		t.Fatal("shattered weapon root remained")
	}
	if _, ok := next.Players["a"].Items.Items["gem"]; ok {
		t.Fatal("shattered weapon child remained")
	}
	if next.Players["a"].Items.Ready[19] != "" || len(next.Players["a"].Items.Inventory) != 0 {
		t.Fatalf("shattered ownership=%+v", next.Players["a"].Items)
	}
	if result.ProficiencyAward != 0 {
		t.Fatalf("shattered weapon awarded proficiency=%d", result.ProficiencyAward)
	}
}

func TestPlanNPCMeleeAttackPowerDamageAddsDeterministicExtraSwing(t *testing.T) {
	s := npcAttackFixture(t)
	p := s.Players["a"]
	p.Body.Class = 10
	p.Body.Level = 128
	p.Body.Flags[playerPowerDamageFlag/8] |= 1 << (playerPowerDamageFlag % 8)
	s.Players["a"] = p
	calls := 0
	roll := func(lo, hi int) int {
		calls++
		switch {
		case lo == 0 && hi == 3:
			return 0 // (level-97)/10 + 0 > 2
		case lo == 1 && hi == 4:
			return 2 // no third swing
		case lo == 1 && hi == 30, lo == 1 && hi == 5:
			return hi
		case lo == 1 && hi == 100:
			return 100
		default:
			t.Fatalf("unexpected random request %d..%d", lo, hi)
			return 0
		}
	}
	next, result, err := s.PlanNPCMeleeAttack("a", "wolf-id", roll)
	if err != nil || result.SwingCount != 2 || !result.Hit || result.Damage != 10 || result.TargetHP != 40 || result.Critical || result.Killed {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, calls)
	}
	if got := next.NPCs["wolf-id"].Enemies[0].Damage; got != 10 {
		t.Fatalf("enemy damage=%d", got)
	}
}

func TestPlanNPCMeleeAttackPowerDamageStopsAfterLethalSwing(t *testing.T) {
	s := npcAttackFixture(t)
	p := s.Players["a"]
	p.Body.Class = 10
	p.Body.Level = 128
	p.Body.Flags[playerPowerDamageFlag/8] |= 1 << (playerPowerDamageFlag % 8)
	s.Players["a"] = p
	npc := s.NPCs["wolf-id"]
	npc.Body.HPCurrent = 1
	npc.Body.Experience = 100
	s.NPCs["wolf-id"] = npc
	calls := 0
	roll := func(lo, hi int) int {
		calls++
		switch {
		case lo == 0 && hi == 3:
			return 0
		case lo == 1 && hi == 4:
			return 2
		case lo == 1 && hi == 30, lo == 1 && hi == 5:
			return hi
		case lo == 1 && hi == 100:
			return 100
		default:
			t.Fatalf("unexpected random request %d..%d", lo, hi)
			return 0
		}
	}
	next, result, err := s.PlanNPCMeleeAttack("a", "wolf-id", roll)
	if err != nil || result.SwingCount != 1 || !result.Killed || result.TargetHP != 0 || calls != 5 {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, calls)
	}
	if _, ok := next.NPCs["wolf-id"]; ok {
		t.Fatal("lethal first swing did not stop the multi-swing sequence")
	}
}
