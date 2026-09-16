package world

import (
	"reflect"
	"strings"
	"testing"
)

func npcCombatRoundFixture(t *testing.T) State {
	t.Helper()
	s := stateFixture()
	player := s.Players["a"]
	player.Body = LegacyMonster{
		Name:   "Alice",
		Type:   0,
		Class:  4,
		Level:  1,
		Stats:  [5]byte{10, 10, 10, 10, 10},
		RoomID: 1,
		// update_active's ordinary NPC damage is mdice(crt) -
		// ((70 - player.armor) / 5), clamped to one.  Armor 70 keeps the
		// source-backed base damage at the deterministic 1d6 result.
		Armor:     70,
		HPMax:     40,
		HPCurrent: 40,
	}
	player.Items = &ItemCollection{Items: map[string]Item{}}
	s.Players["a"] = player
	room := s.Rooms[1]
	room.NPCIDs = []string{"wolf-id"}
	s.Rooms[1] = room
	s.NPCs = map[string]NPCState{
		"wolf-id": {
			Body: LegacyMonster{
				Name:      "늑대",
				Type:      1,
				RoomID:    1,
				Class:     4,
				Level:     1,
				Stats:     [5]byte{10, 10, 10, 10, 10},
				HPMax:     20,
				HPCurrent: 20,
				DiceCount: 1,
				DiceSides: 6,
			},
			Enemies: []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "a"}, Damage: 0}},
		},
	}
	s.ActiveNPCIDs = []string{"wolf-id"}
	return s
}

func npcCombatRoundRoll(high int) func(int, int) int {
	return func(low, gotHigh int) int {
		if low == 1 && gotHigh == high {
			return gotHigh
		}
		if low == 1 && gotHigh == 30 {
			return gotHigh
		}
		if low == 1 && gotHigh == 100 {
			return gotHigh
		}
		return gotHigh
	}
}

func TestNPCCombatRoundPlansAndAppliesHitDamageAndHP(t *testing.T) {
	s := npcCombatRoundFixture(t)
	proposal, err := s.PlanNPCCombatRound("wolf-id", "a", npcCombatRoundRoll(6))
	if err != nil {
		t.Fatal(err)
	}
	if proposal.NPCID != "wolf-id" || proposal.PlayerID != "a" || proposal.RoomID != 1 || !proposal.Hit || proposal.Critical || proposal.Damage != 6 || proposal.PlayerHPBefore != 40 || proposal.PlayerHPAfter != 34 {
		t.Fatalf("proposal=%+v", proposal)
	}
	if s.Players["a"].Body.HPCurrent != 40 || s.NPCs["wolf-id"].Body.HPCurrent != 20 {
		t.Fatal("planning mutated source state")
	}

	next, result, err := s.ApplyNPCCombatRound(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if result.NPCID != "wolf-id" || result.PlayerID != "a" || !result.Hit || result.Critical || result.Damage != 6 || result.PlayerHP != 34 || result.TargetHP != 34 || result.Killed {
		t.Fatalf("result=%+v", result)
	}
	if next.Players["a"].Body.HPCurrent != 34 || next.NPCs["wolf-id"].Body.HPCurrent != 20 {
		t.Fatalf("next=%+v", next)
	}
	if err := next.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestNPCCombatRoundEmitsSourceAlignedHitAndMissEvents(t *testing.T) {
	t.Run("hit", func(t *testing.T) {
		s := npcCombatRoundFixture(t)
		proposal, err := s.PlanNPCCombatRound("wolf-id", "a", npcCombatRoundRoll(6))
		if err != nil {
			t.Fatal(err)
		}
		if proposal.Event == nil || !proposal.Event.Hit || proposal.Event.Damage != 6 || proposal.Event.RoomID != 1 || proposal.Event.NPCID != "wolf-id" || proposal.Event.TargetID != "a" || proposal.Event.ExcludeTargetID != "a" {
			t.Fatalf("proposal event=%+v", proposal.Event)
		}
		if got, want := proposal.Event.TargetText, NPCCombatHitActorText("늑대", 6); got != want {
			t.Fatalf("hit actor text=%q want=%q", got, want)
		}
		if got, want := proposal.Event.RoomText, NPCCombatHitRoomText("늑대", "Alice", 6); got != want {
			t.Fatalf("hit room text=%q want=%q", got, want)
		}
		_, result, err := s.ApplyNPCCombatRound(proposal)
		if err != nil {
			t.Fatal(err)
		}
		if result.Event == nil || !reflect.DeepEqual(*result.Event, *proposal.Event) {
			t.Fatalf("result event=%+v proposal event=%+v", result.Event, proposal.Event)
		}
	})

	t.Run("miss", func(t *testing.T) {
		s := npcCombatRoundFixture(t)
		npc := s.NPCs["wolf-id"]
		npc.Body.Thaco = 20
		s.NPCs["wolf-id"] = npc
		calls := 0
		proposal, err := s.PlanNPCCombatRound("wolf-id", "a", func(low, high int) int {
			calls++
			if low != 1 || high != 20 {
				t.Fatalf("unexpected miss RNG request %d..%d", low, high)
			}
			return 1
		})
		if err != nil {
			t.Fatal(err)
		}
		if calls != 1 || proposal.Hit || proposal.Damage != 0 || proposal.PlayerHPAfter != proposal.PlayerHPBefore || proposal.Event == nil || proposal.Event.Hit || proposal.Event.Damage != 0 || proposal.Event.RoomText != "" {
			t.Fatalf("miss calls=%d proposal=%+v", calls, proposal)
		}
		if got, want := proposal.Event.TargetText, NPCCombatMissActorText("늑대"); got != want {
			t.Fatalf("miss actor text=%q want=%q", got, want)
		}
		_, result, err := s.ApplyNPCCombatRound(proposal)
		if err != nil {
			t.Fatal(err)
		}
		if result.Event == nil || !reflect.DeepEqual(*result.Event, *proposal.Event) {
			t.Fatalf("result event=%+v proposal event=%+v", result.Event, proposal.Event)
		}
	})
}

func TestNPCCombatRoundUsesOnlyHitAndDamageRNG(t *testing.T) {
	s := npcCombatRoundFixture(t)
	npc := s.NPCs["wolf-id"]
	// This proficiency would trigger the old player->NPC critical branch. The
	// NPC->PLAYER update_active path must never consult it.
	npc.Body.Proficiency[2] = 31214
	s.NPCs["wolf-id"] = npc
	var calls [][2]int
	proposal, err := s.PlanNPCCombatRound("wolf-id", "a", func(low, high int) int {
		calls = append(calls, [2]int{low, high})
		switch {
		case low == 1 && high == 20:
			return 20
		case low == 1 && high == 6:
			return 6
		default:
			t.Fatalf("unexpected random request %d..%d", low, high)
			return 0
		}
	})
	if err != nil || !proposal.Hit || proposal.Critical || proposal.Damage != 6 || proposal.PlayerHPAfter != 34 {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	if want := [][2]int{{1, 20}, {1, 6}}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("RNG calls=%v want=%v", calls, want)
	}
	next, result, err := s.ApplyNPCCombatRound(proposal)
	if err != nil || result.Critical || result.Damage != 6 || next.Players["a"].Body.HPCurrent != 34 {
		t.Fatalf("result=%+v next=%+v err=%v", result, next, err)
	}
}

func TestNPCCombatRoundPoisonerRollSetsPlayerPoison(t *testing.T) {
	s := npcCombatRoundFixture(t)
	npc := s.NPCs["wolf-id"]
	npc.Body.Flags[13/8] |= 1 << (13 % 8) // MPOISS
	s.NPCs["wolf-id"] = npc
	var calls [][2]int
	proposal, err := s.PlanNPCCombatRound("wolf-id", "a", func(low, high int) int {
		calls = append(calls, [2]int{low, high})
		switch high {
		case 20, 6:
			return high
		case 100:
			return 15
		default:
			t.Fatalf("unexpected random request %d..%d", low, high)
			return 0
		}
	})
	if err != nil || !proposal.Hit || !proposal.Poisoned || proposal.Damage != 6 || proposal.PlayerHPAfter != 34 {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	if want := [][2]int{{1, 20}, {1, 6}, {1, 100}}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("RNG calls=%v want=%v", calls, want)
	}

	next, result, err := s.ApplyNPCCombatRound(proposal)
	if err != nil {
		t.Fatal(err)
	}
	nextPlayer := next.Players["a"]
	if !result.Poisoned || !flag(nextPlayer.Body.Flags[:], 16) {
		t.Fatalf("result=%+v player flags=%#x", result, next.Players["a"].Body.Flags)
	}
}

func TestNPCCombatRoundPoisonRollAboveThresholdLeavesExistingPoisonUnchanged(t *testing.T) {
	s := npcCombatRoundFixture(t)
	player := s.Players["a"]
	player.Body.Flags[16/8] |= 1 << (16 % 8) // PPOISN already set
	s.Players["a"] = player
	npc := s.NPCs["wolf-id"]
	npc.Body.Flags[13/8] |= 1 << (13 % 8) // MPOISS
	s.NPCs["wolf-id"] = npc
	var calls [][2]int
	proposal, err := s.PlanNPCCombatRound("wolf-id", "a", func(low, high int) int {
		calls = append(calls, [2]int{low, high})
		switch high {
		case 20, 6:
			return high
		case 100:
			return 16
		default:
			t.Fatalf("unexpected random request %d..%d", low, high)
			return 0
		}
	})
	if err != nil || proposal.Poisoned {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	if want := [][2]int{{1, 20}, {1, 6}, {1, 100}}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("RNG calls=%v want=%v", calls, want)
	}
	next, result, err := s.ApplyNPCCombatRound(proposal)
	if err != nil {
		t.Fatal(err)
	}
	nextPlayer := next.Players["a"]
	if result.Poisoned || !flag(nextPlayer.Body.Flags[:], 16) {
		t.Fatalf("result=%+v player flags=%#x", result, next.Players["a"].Body.Flags)
	}
}

func TestNPCCombatRoundEffectsFollowPoisonDiseaseBlindOrder(t *testing.T) {
	s := npcCombatRoundFixture(t)
	npc := s.NPCs["wolf-id"]
	npc.Body.Flags[13/8] |= 1 << (13 % 8) // MPOISS
	npc.Body.Flags[34/8] |= 1 << (34 % 8) // MDISEA
	npc.Body.Flags[45/8] |= 1 << (45 % 8) // MBLNDR
	s.NPCs["wolf-id"] = npc

	var calls [][2]int
	effectRolls := []int{15, 10, 11} // poison succeeds, disease succeeds, blind misses
	effectIndex := 0
	proposal, err := s.PlanNPCCombatRound("wolf-id", "a", func(low, high int) int {
		calls = append(calls, [2]int{low, high})
		switch high {
		case 20, 6:
			return high
		case 100:
			if effectIndex >= len(effectRolls) {
				t.Fatalf("unexpected extra effect draw")
			}
			value := effectRolls[effectIndex]
			effectIndex++
			return value
		default:
			t.Fatalf("unexpected random request %d..%d", low, high)
			return 0
		}
	})
	if err != nil || !proposal.Hit || !proposal.Poisoned || !proposal.Diseased || proposal.Blinded {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	if want := [][2]int{{1, 20}, {1, 6}, {1, 100}, {1, 100}, {1, 100}}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("RNG calls=%v want=%v", calls, want)
	}

	next, result, err := s.ApplyNPCCombatRound(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Poisoned || !result.Diseased || result.Blinded {
		t.Fatalf("result=%+v", result)
	}
	nextPlayer := next.Players["a"]
	for _, bit := range []uint{16, 41} {
		if !flag(nextPlayer.Body.Flags[:], bit) {
			t.Fatalf("effect flag %d missing: flags=%#x", bit, nextPlayer.Body.Flags)
		}
	}
	if flag(nextPlayer.Body.Flags[:], 42) {
		t.Fatalf("unexpected blind flag: flags=%#x", nextPlayer.Body.Flags)
	}
}

func TestNPCCombatRoundBreathTriggerBoundaryAndLevelBand(t *testing.T) {
	for _, tc := range []struct {
		name          string
		trigger       int
		wantTriggered bool
		wantDamage    int
		wantCalls     [][2]int
	}{
		{
			name:          "trigger below five",
			trigger:       4,
			wantTriggered: true,
			wantDamage:    8,
			wantCalls:     [][2]int{{1, 20}, {1, 30}, {1, 4}, {1, 4}},
		},
		{
			name:          "trigger at five falls back to melee",
			trigger:       5,
			wantTriggered: false,
			wantDamage:    6,
			wantCalls:     [][2]int{{1, 20}, {1, 30}, {1, 6}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := npcCombatRoundFixture(t)
			npc := s.NPCs["wolf-id"]
			npc.Body.Level = 5                    // ((level + 3) / 4) == 2, per src/misc.c:dice.
			npc.Body.Flags[19/8] |= 1 << (19 % 8) // MBRETH
			s.NPCs["wolf-id"] = npc
			var calls [][2]int
			proposal, err := s.PlanNPCCombatRound("wolf-id", "a", func(low, high int) int {
				calls = append(calls, [2]int{low, high})
				switch high {
				case 20:
					return 20
				case 30:
					return tc.trigger
				case 4, 6:
					return high
				default:
					t.Fatalf("unexpected random request %d..%d", low, high)
					return 0
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			if proposal.BreathTriggered != tc.wantTriggered || proposal.BreathRoll != tc.trigger || proposal.Damage != tc.wantDamage {
				t.Fatalf("proposal=%+v", proposal)
			}
			if tc.wantTriggered && (proposal.BreathType != NPCCombatBreathFire || proposal.BreathDiceCount != 2 || proposal.BreathDiceSides != 4 || proposal.BreathDicePlus != 0) {
				t.Fatalf("breath spec=%+v", proposal)
			}
			if !reflect.DeepEqual(calls, tc.wantCalls) {
				t.Fatalf("RNG calls=%v want=%v", calls, tc.wantCalls)
			}
			next, result, err := s.ApplyNPCCombatRound(proposal)
			if err != nil {
				t.Fatal(err)
			}
			if result.BreathTriggered != tc.wantTriggered || result.Damage != tc.wantDamage || int(next.Players["a"].Body.HPCurrent) != 40-tc.wantDamage {
				t.Fatalf("result=%+v next=%+v", result, next)
			}
		})
	}
}

func TestNPCCombatRoundBreathBranchesAndResistance(t *testing.T) {
	for _, tc := range []struct {
		name           string
		setNPCFlags    []uint
		setPlayerFlags []uint
		wantType       NPCCombatBreathType
		wantSides      int
		wantPlus       int
		wantDamage     int
		wantResisted   bool
		wantPoisoned   bool
	}{
		{
			name:       "fire without resistance",
			wantType:   NPCCombatBreathFire,
			wantSides:  4,
			wantDamage: 4,
		},
		{
			name:           "fire resistance halves dice sides",
			setPlayerFlags: []uint{30},
			wantType:       NPCCombatBreathFire,
			wantSides:      2,
			wantDamage:     2,
			wantResisted:   true,
		},
		{
			name:        "mbrwp1 gas branch",
			setNPCFlags: []uint{28},
			wantType:    NPCCombatBreathGas,
			wantSides:   3,
			wantDamage:  3,
		},
		{
			name:        "cold without resistance",
			setNPCFlags: []uint{29},
			wantType:    NPCCombatBreathCold,
			wantSides:   4,
			wantDamage:  4,
		},
		{
			name:           "cold resistance halves dice sides",
			setNPCFlags:    []uint{29},
			setPlayerFlags: []uint{36},
			wantType:       NPCCombatBreathCold,
			wantSides:      2,
			wantDamage:     2,
			wantResisted:   true,
		},
		{
			name:         "acid branch sets poison",
			setNPCFlags:  []uint{28, 29},
			wantType:     NPCCombatBreathAcid,
			wantSides:    2,
			wantPlus:     1,
			wantDamage:   3,
			wantPoisoned: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := npcCombatRoundFixture(t)
			npc := s.NPCs["wolf-id"]
			npc.Body.Flags[19/8] |= 1 << (19 % 8) // MBRETH
			for _, bit := range tc.setNPCFlags {
				npc.Body.Flags[bit/8] |= 1 << (bit % 8)
			}
			s.NPCs["wolf-id"] = npc
			player := s.Players["a"]
			for _, bit := range tc.setPlayerFlags {
				player.Body.Flags[bit/8] |= 1 << (bit % 8)
			}
			s.Players["a"] = player
			var calls [][2]int
			proposal, err := s.PlanNPCCombatRound("wolf-id", "a", func(low, high int) int {
				calls = append(calls, [2]int{low, high})
				switch high {
				case 20:
					return 20
				case 30:
					return 4
				case 2, 3, 4:
					return high
				default:
					t.Fatalf("unexpected random request %d..%d", low, high)
					return 0
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			if !proposal.BreathTriggered || proposal.BreathType != tc.wantType || proposal.BreathDiceCount != 1 || proposal.BreathDiceSides != tc.wantSides || proposal.BreathDicePlus != tc.wantPlus || proposal.BreathResisted != tc.wantResisted || proposal.BreathPoisoned != tc.wantPoisoned || proposal.Damage != tc.wantDamage {
				t.Fatalf("proposal=%+v", proposal)
			}
			if want := [][2]int{{1, 20}, {1, 30}, {1, tc.wantSides}}; !reflect.DeepEqual(calls, want) {
				t.Fatalf("RNG calls=%v want=%v", calls, want)
			}
			next, result, err := s.ApplyNPCCombatRound(proposal)
			if err != nil {
				t.Fatal(err)
			}
			if result.BreathType != tc.wantType || result.BreathResisted != tc.wantResisted || result.BreathPoisoned != tc.wantPoisoned || result.Damage != tc.wantDamage {
				t.Fatalf("result=%+v", result)
			}
			nextPlayer := next.Players["a"]
			if got := flag(nextPlayer.Body.Flags[:], 16); got != tc.wantPoisoned {
				t.Fatalf("PPOISN=%v want=%v flags=%#x", got, tc.wantPoisoned, nextPlayer.Body.Flags)
			}
		})
	}
}

func TestNPCCombatRoundBreathAndSharedEffectsFollowSourceOrder(t *testing.T) {
	s := npcCombatDissolveFixture(t, map[string]Item{"held": {Object: LegacyObject{Name: "쥔검"}}}, [20]string{16: "held"})
	npc := s.NPCs["wolf-id"]
	for _, bit := range []uint{19, 28, 29, 13, 34, 45} { // MBRETH, acid, MPOISS, MDISEA, MBLNDR
		npc.Body.Flags[bit/8] |= 1 << (bit % 8)
	}
	s.NPCs["wolf-id"] = npc
	var calls [][2]int
	proposal, err := s.PlanNPCCombatRound("wolf-id", "a", func(low, high int) int {
		calls = append(calls, [2]int{low, high})
		switch len(calls) {
		case 1:
			return 20 // hit
		case 2:
			return 4 // breath trigger
		case 3:
			return 2 // acid damage die
		case 4:
			return 15 // MPOISS
		case 5, 6:
			return 10 // MDISEA/MBLNDR
		case 7:
			return 15 // MDISIT
		case 8:
			return 0 // one ready item
		default:
			t.Fatalf("unexpected random request %d..%d", low, high)
			return 0
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !proposal.BreathTriggered || proposal.BreathType != NPCCombatBreathAcid || !proposal.BreathPoisoned || !proposal.Poisoned || !proposal.Diseased || !proposal.Blinded || !proposal.Dissolved || proposal.Damage != 3 {
		t.Fatalf("proposal=%+v", proposal)
	}
	wantCalls := [][2]int{{1, 20}, {1, 30}, {1, 2}, {1, 100}, {1, 100}, {1, 100}, {1, 100}, {0, 0}}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("RNG calls=%v want=%v", calls, wantCalls)
	}
	next, result, err := s.ApplyNPCCombatRound(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if !result.BreathPoisoned || !result.Poisoned || !result.Diseased || !result.Blinded || !result.Dissolved {
		t.Fatalf("result=%+v", result)
	}
	nextPlayer := next.Players["a"]
	for _, bit := range []uint{16, 41, 42} {
		if !flag(nextPlayer.Body.Flags[:], bit) {
			t.Fatalf("status flag %d missing: flags=%#x", bit, nextPlayer.Body.Flags)
		}
	}
}

func TestNPCCombatRoundRejectsBreathInvalidRNGWithoutMutation(t *testing.T) {
	for _, tc := range []struct {
		name      string
		invalidAt int
		wantCalls [][2]int
	}{
		{name: "invalid trigger", invalidAt: 2, wantCalls: [][2]int{{1, 20}, {1, 30}}},
		{name: "invalid dice", invalidAt: 3, wantCalls: [][2]int{{1, 20}, {1, 30}, {1, 2}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := npcCombatRoundFixture(t)
			npc := s.NPCs["wolf-id"]
			npc.Body.Flags[19/8] |= 1 << (19 % 8) // MBRETH
			npc.Body.Flags[28/8] |= 1 << (28 % 8)
			npc.Body.Flags[29/8] |= 1 << (29 % 8) // acid, one 1..2 die
			s.NPCs["wolf-id"] = npc
			before := s.clone()
			var calls [][2]int
			proposal, err := s.PlanNPCCombatRound("wolf-id", "a", func(low, high int) int {
				calls = append(calls, [2]int{low, high})
				if len(calls) == tc.invalidAt {
					return high + 1
				}
				if high == 30 {
					return 4
				}
				return high
			})
			if err == nil || !reflect.DeepEqual(proposal, NPCCombatRoundProposal{}) || !reflect.DeepEqual(s, before) {
				t.Fatalf("invalid breath RNG was not fail-closed: proposal=%+v err=%v changed=%v", proposal, err, !reflect.DeepEqual(s, before))
			}
			if !reflect.DeepEqual(calls, tc.wantCalls) {
				t.Fatalf("RNG calls=%v want=%v", calls, tc.wantCalls)
			}
		})
	}
}

func TestNPCCombatRoundRejectsTamperedBreathAndStaleCandidatesAtomically(t *testing.T) {
	s := npcCombatRoundFixture(t)
	npc := s.NPCs["wolf-id"]
	for _, bit := range []uint{19, 28, 29} { // MBRETH + acid
		npc.Body.Flags[bit/8] |= 1 << (bit % 8)
	}
	s.NPCs["wolf-id"] = npc
	proposal, err := s.PlanNPCCombatRound("wolf-id", "a", func(low, high int) int {
		if high == 20 {
			return 20
		}
		if high == 30 {
			return 4
		}
		return high
	})
	if err != nil || !proposal.BreathTriggered || !proposal.BreathPoisoned {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	assertRejected := func(name string, candidate NPCCombatRoundProposal, state State) {
		t.Helper()
		if next, result, err := state.ApplyNPCCombatRound(candidate); err == nil || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(result, NPCCombatRoundResult{}) {
			t.Fatalf("%s accepted: next=%+v result=%+v err=%v", name, next, result, err)
		}
	}

	tampered := proposal
	tampered.BreathTriggered = false
	assertRejected("tampered trigger", tampered, s)
	tampered = proposal
	tampered.BreathType = NPCCombatBreathFire
	assertRejected("tampered type", tampered, s)
	tampered = proposal
	tampered.BreathRoll = 5
	assertRejected("tampered trigger roll", tampered, s)
	tampered = proposal
	tampered.BreathPoisoned = false
	assertRejected("tampered acid poison", tampered, s)
	tampered = proposal
	player := tampered.next.Players["a"]
	player.Body.Flags[16/8] &^= 1 << (16 % 8)
	tampered.next.Players["a"] = player
	assertRejected("tampered acid state", tampered, s)
	changed := s.clone()
	player = changed.Players["a"]
	player.Body.HPCurrent--
	changed.Players["a"] = player
	assertRejected("stale breath candidate", proposal, changed)
}

func TestNPCCombatRoundDiseaseAndBlindThresholdBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name      string
		attacker  uint
		victim    uint
		roll      int
		wantEvent func(NPCCombatRoundResult) bool
	}{
		{name: "disease threshold", attacker: 34, victim: 41, roll: 10, wantEvent: func(result NPCCombatRoundResult) bool { return result.Diseased }},
		{name: "disease above threshold", attacker: 34, victim: 41, roll: 11, wantEvent: func(result NPCCombatRoundResult) bool { return !result.Diseased }},
		{name: "blind threshold", attacker: 45, victim: 42, roll: 10, wantEvent: func(result NPCCombatRoundResult) bool { return result.Blinded }},
		{name: "blind above threshold", attacker: 45, victim: 42, roll: 11, wantEvent: func(result NPCCombatRoundResult) bool { return !result.Blinded }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := npcCombatRoundFixture(t)
			npc := s.NPCs["wolf-id"]
			npc.Body.Flags[tc.attacker/8] |= 1 << (tc.attacker % 8)
			s.NPCs["wolf-id"] = npc
			calls := 0
			proposal, err := s.PlanNPCCombatRound("wolf-id", "a", func(low, high int) int {
				calls++
				if high == 100 {
					return tc.roll
				}
				return high
			})
			if err != nil {
				t.Fatal(err)
			}
			next, result, err := s.ApplyNPCCombatRound(proposal)
			if err != nil {
				t.Fatal(err)
			}
			if calls != 3 || !tc.wantEvent(result) {
				t.Fatalf("calls=%d proposal=%+v result=%+v", calls, proposal, result)
			}
			nextPlayer := next.Players["a"]
			if flag(nextPlayer.Body.Flags[:], tc.victim) != (tc.roll <= 10) {
				t.Fatalf("victim flag %d mismatch: roll=%d flags=%#x", tc.victim, tc.roll, nextPlayer.Body.Flags)
			}
		})
	}
}

func TestNPCCombatRoundDoesNotRollDiseaseOrBlindOnMissOrNonEffect(t *testing.T) {
	for _, tc := range []struct {
		name       string
		allEffects bool
		hitRoll    int
		wantCalls  [][2]int
	}{
		{name: "miss with effects", allEffects: true, hitRoll: 1, wantCalls: [][2]int{{1, 20}}},
		{name: "hit without effects", hitRoll: 20, wantCalls: [][2]int{{1, 20}, {1, 6}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := npcCombatRoundFixture(t)
			npc := s.NPCs["wolf-id"]
			npc.Body.Thaco = 20
			if tc.allEffects {
				npc.Body.Flags[13/8] |= 1 << (13 % 8) // MPOISS
				npc.Body.Flags[34/8] |= 1 << (34 % 8) // MDISEA
				npc.Body.Flags[45/8] |= 1 << (45 % 8) // MBLNDR
			}
			s.NPCs["wolf-id"] = npc
			var calls [][2]int
			proposal, err := s.PlanNPCCombatRound("wolf-id", "a", func(low, high int) int {
				calls = append(calls, [2]int{low, high})
				if high == 20 {
					return tc.hitRoll
				}
				return high
			})
			if err != nil || proposal.Hit != (tc.hitRoll == 20) || proposal.Diseased || proposal.Blinded {
				t.Fatalf("proposal=%+v err=%v", proposal, err)
			}
			if !reflect.DeepEqual(calls, tc.wantCalls) {
				t.Fatalf("RNG calls=%v want=%v", calls, tc.wantCalls)
			}
		})
	}
}

func TestNPCCombatRoundPreservesPreExistingDiseaseAndBlindFlags(t *testing.T) {
	s := npcCombatRoundFixture(t)
	player := s.Players["a"]
	player.Body.Flags[41/8] |= 1 << (41 % 8) // PDISEA already set
	player.Body.Flags[42/8] |= 1 << (42 % 8) // PBLIND already set
	s.Players["a"] = player
	npc := s.NPCs["wolf-id"]
	npc.Body.Flags[34/8] |= 1 << (34 % 8) // MDISEA
	npc.Body.Flags[45/8] |= 1 << (45 % 8) // MBLNDR
	s.NPCs["wolf-id"] = npc

	var calls [][2]int
	proposal, err := s.PlanNPCCombatRound("wolf-id", "a", func(low, high int) int {
		calls = append(calls, [2]int{low, high})
		if high == 100 {
			return 11
		}
		return high
	})
	if err != nil || proposal.Diseased || proposal.Blinded {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	if want := [][2]int{{1, 20}, {1, 6}, {1, 100}, {1, 100}}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("RNG calls=%v want=%v", calls, want)
	}
	next, result, err := s.ApplyNPCCombatRound(proposal)
	if err != nil {
		t.Fatal(err)
	}
	nextPlayer := next.Players["a"]
	if result.Diseased || result.Blinded || !flag(nextPlayer.Body.Flags[:], 41) || !flag(nextPlayer.Body.Flags[:], 42) {
		t.Fatalf("result=%+v flags=%#x", result, nextPlayer.Body.Flags)
	}
}

func TestNPCCombatRoundRejectsInvalidDiseaseOrBlindRNGWithoutChangingState(t *testing.T) {
	for _, tc := range []struct {
		name      string
		flags     []uint
		wantCalls [][2]int
		invalidAt int
	}{
		{name: "disease", flags: []uint{34, 45}, wantCalls: [][2]int{{1, 20}, {1, 6}, {1, 100}}, invalidAt: 2},
		{name: "blind", flags: []uint{45}, wantCalls: [][2]int{{1, 20}, {1, 6}, {1, 100}}, invalidAt: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := npcCombatRoundFixture(t)
			npc := s.NPCs["wolf-id"]
			for _, bit := range tc.flags {
				npc.Body.Flags[bit/8] |= 1 << (bit % 8)
			}
			s.NPCs["wolf-id"] = npc
			before := s.clone()
			var calls [][2]int
			proposal, err := s.PlanNPCCombatRound("wolf-id", "a", func(low, high int) int {
				calls = append(calls, [2]int{low, high})
				if high == 100 && len(calls) == tc.invalidAt+1 {
					return high + 1
				}
				return high
			})
			if err == nil || !reflect.DeepEqual(proposal, NPCCombatRoundProposal{}) || !reflect.DeepEqual(s, before) {
				t.Fatalf("invalid RNG was not fail-closed: proposal=%+v err=%v changed=%v", proposal, err, !reflect.DeepEqual(s, before))
			}
			if !reflect.DeepEqual(calls, tc.wantCalls) {
				t.Fatalf("RNG calls=%v want=%v", calls, tc.wantCalls)
			}
		})
	}
}

func TestNPCCombatRoundRejectsTamperedDiseaseBlindAndStaleCandidatesAtomically(t *testing.T) {
	s := npcCombatRoundFixture(t)
	npc := s.NPCs["wolf-id"]
	npc.Body.Flags[34/8] |= 1 << (34 % 8) // MDISEA
	npc.Body.Flags[45/8] |= 1 << (45 % 8) // MBLNDR
	s.NPCs["wolf-id"] = npc
	proposal, err := s.PlanNPCCombatRound("wolf-id", "a", func(low, high int) int {
		if high == 100 {
			return 10
		}
		return high
	})
	if err != nil || !proposal.Diseased || !proposal.Blinded {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	assertRejected := func(name string, candidate NPCCombatRoundProposal, state State) {
		t.Helper()
		if next, result, err := state.ApplyNPCCombatRound(candidate); err == nil || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(result, NPCCombatRoundResult{}) {
			t.Fatalf("%s accepted: next=%+v result=%+v err=%v", name, next, result, err)
		}
	}

	tampered := proposal
	tampered.Diseased = false
	assertRejected("tampered disease outcome", tampered, s)
	tampered = proposal
	tampered.Blinded = false
	assertRejected("tampered blind outcome", tampered, s)
	tampered = proposal
	player := tampered.next.Players["a"]
	player.Body.Flags[41/8] &^= 1 << (41 % 8)
	tampered.next.Players["a"] = player
	assertRejected("tampered disease state", tampered, s)
	tampered = proposal
	player = tampered.next.Players["a"]
	player.Body.Flags[42/8] &^= 1 << (42 % 8)
	tampered.next.Players["a"] = player
	assertRejected("tampered blind state", tampered, s)
	changed := s.clone()
	player = changed.Players["a"]
	player.Body.HPCurrent--
	changed.Players["a"] = player
	assertRejected("stale candidate", proposal, changed)
}

func TestNPCCombatRoundDoesNotRollPoisonOnMissOrNonPoisoner(t *testing.T) {
	for _, tc := range []struct {
		name       string
		poisoner   bool
		hitRoll    int
		wantHit    bool
		wantPoison bool
		wantCalls  [][2]int
	}{
		{name: "miss poisoner", poisoner: true, hitRoll: 1, wantCalls: [][2]int{{1, 20}}},
		{name: "hit non-poisoner", hitRoll: 20, wantHit: true, wantCalls: [][2]int{{1, 20}, {1, 6}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := npcCombatRoundFixture(t)
			npc := s.NPCs["wolf-id"]
			npc.Body.Thaco = 20
			if tc.poisoner {
				npc.Body.Flags[13/8] |= 1 << (13 % 8) // MPOISS
			}
			s.NPCs["wolf-id"] = npc
			var calls [][2]int
			proposal, err := s.PlanNPCCombatRound("wolf-id", "a", func(low, high int) int {
				calls = append(calls, [2]int{low, high})
				switch high {
				case 20:
					return tc.hitRoll
				case 6:
					return 6
				default:
					t.Fatalf("unexpected random request %d..%d", low, high)
					return 0
				}
			})
			if err != nil || proposal.Hit != tc.wantHit || proposal.Poisoned != tc.wantPoison {
				t.Fatalf("proposal=%+v err=%v", proposal, err)
			}
			if !reflect.DeepEqual(calls, tc.wantCalls) {
				t.Fatalf("RNG calls=%v want=%v", calls, tc.wantCalls)
			}
		})
	}
}

func TestNPCCombatRoundRejectsInvalidPoisonRNGWithoutChangingState(t *testing.T) {
	s := npcCombatRoundFixture(t)
	npc := s.NPCs["wolf-id"]
	npc.Body.Flags[13/8] |= 1 << (13 % 8) // MPOISS
	s.NPCs["wolf-id"] = npc
	before := s.clone()
	var calls [][2]int
	proposal, err := s.PlanNPCCombatRound("wolf-id", "a", func(low, high int) int {
		calls = append(calls, [2]int{low, high})
		if high == 100 {
			return 101
		}
		return high
	})
	if err == nil || !reflect.DeepEqual(proposal, NPCCombatRoundProposal{}) || !reflect.DeepEqual(s, before) {
		t.Fatalf("invalid poison RNG was not fail-closed: proposal=%+v err=%v changed=%v", proposal, err, !reflect.DeepEqual(s, before))
	}
	if want := [][2]int{{1, 20}, {1, 6}, {1, 100}}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("RNG calls=%v want=%v", calls, want)
	}
}

func TestNPCCombatRoundRejectsTamperedPoisonCandidateAtomically(t *testing.T) {
	s := npcCombatRoundFixture(t)
	npc := s.NPCs["wolf-id"]
	npc.Body.Flags[13/8] |= 1 << (13 % 8) // MPOISS
	s.NPCs["wolf-id"] = npc
	proposal, err := s.PlanNPCCombatRound("wolf-id", "a", func(low, high int) int {
		if high == 100 {
			return 15
		}
		return high
	})
	if err != nil || !proposal.Poisoned {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}

	tampered := proposal
	tampered.Poisoned = false
	if next, result, err := s.ApplyNPCCombatRound(tampered); err == nil || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(result, NPCCombatRoundResult{}) {
		t.Fatalf("tampered poison outcome accepted: next=%+v result=%+v err=%v", next, result, err)
	}

	tampered = proposal
	player := tampered.next.Players["a"]
	player.Body.Flags[16/8] &^= 1 << (16 % 8)
	tampered.next.Players["a"] = player
	if next, result, err := s.ApplyNPCCombatRound(tampered); err == nil || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(result, NPCCombatRoundResult{}) {
		t.Fatalf("tampered poison state accepted: next=%+v result=%+v err=%v", next, result, err)
	}
}

func npcCombatDissolveFixture(t *testing.T, items map[string]Item, ready [20]string) State {
	t.Helper()
	s := npcCombatRoundFixture(t)
	player := s.Players["a"]
	player.Items = &ItemCollection{Items: items, Ready: ready}
	s.Players["a"] = player
	npc := s.NPCs["wolf-id"]
	npc.Body.Flags[npcCombatDissolverFlag/8] |= 1 << (npcCombatDissolverFlag % 8)
	s.NPCs["wolf-id"] = npc
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestNPCCombatRoundMDISITRejectsUnmigratedItemsBeforeRNG(t *testing.T) {
	s := npcCombatRoundFixture(t)
	player := s.Players["a"]
	player.Items = nil
	s.Players["a"] = player
	npc := s.NPCs["wolf-id"]
	npc.Body.Flags[npcCombatDissolverFlag/8] |= 1 << (npcCombatDissolverFlag % 8)
	s.NPCs["wolf-id"] = npc
	before := s.clone()
	called := false
	proposal, err := s.PlanNPCCombatRound("wolf-id", "a", func(int, int) int {
		called = true
		return 20
	})
	if err == nil || called || !reflect.DeepEqual(proposal, NPCCombatRoundProposal{}) || !reflect.DeepEqual(s, before) {
		t.Fatalf("unmigrated MDISIT was not fail-closed: proposal=%+v err=%v called=%v changed=%v", proposal, err, called, !reflect.DeepEqual(s, before))
	}
}

func TestNPCCombatRoundDissolvesSelectedReadySubtreeAndRefreshesEquipment(t *testing.T) {
	s := npcCombatDissolveFixture(t, map[string]Item{
		"body": {Object: LegacyObject{Name: "갑옷", Armor: 7}, Contents: []string{"gem"}},
		// This child must disappear with the selected ready root.
		"gem":   {Object: LegacyObject{Name: "보석"}},
		"held":  {Object: LegacyObject{Name: "쥔검", Armor: 2}},
		"wield": {Object: LegacyObject{Name: "무기", Armor: 3, Adjustment: 2}},
	}, [20]string{0: "body", 16: "held", 19: "wield"})
	before := s.clone()
	var calls [][2]int
	proposal, err := s.PlanNPCCombatRound("wolf-id", "a", func(low, high int) int {
		calls = append(calls, [2]int{low, high})
		switch {
		case high == 20:
			return 20
		case high == 6:
			return 6
		case low == 1 && high == 100:
			return 15
		case low == 0 && high == 2:
			return 0
		default:
			t.Fatalf("unexpected random request %d..%d", low, high)
			return 0
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !proposal.DissolveSucceeded || !proposal.Dissolved || proposal.DissolveProtected || proposal.DissolveRoll != 15 || proposal.DissolveSelectionRoll != 0 || proposal.DissolveCandidateCount != 3 || proposal.DissolveReadySlot != 0 || proposal.DissolveItemID != "body" || proposal.DissolveItemName != "갑옷" {
		t.Fatalf("proposal=%+v", proposal)
	}
	if want := [][2]int{{1, 20}, {1, 6}, {1, 100}, {0, 2}}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("RNG calls=%v want=%v", calls, want)
	}
	if !reflect.DeepEqual(s, before) {
		t.Fatal("planning mutated source state")
	}

	next, result, err := s.ApplyNPCCombatRound(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if !result.DissolveSucceeded || !result.Dissolved || result.DissolveItemID != "body" || result.DissolveReadySlot != 0 {
		t.Fatalf("result=%+v", result)
	}
	items := next.Players["a"].Items
	if items.Ready[0] != "" || items.Ready[16] != "held" || items.Ready[19] != "wield" {
		t.Fatalf("ready after dissolve=%v", items.Ready)
	}
	if _, ok := items.Items["body"]; ok {
		t.Fatal("dissolved root remains")
	}
	if _, ok := items.Items["gem"]; ok {
		t.Fatal("dissolved child remains")
	}
	if err := next.Validate(); err != nil {
		t.Fatal(err)
	}
	stats, err := items.CombatStats(next.Players["a"].Body)
	if err != nil {
		t.Fatal(err)
	}
	if int8(next.Players["a"].Body.Armor) != stats.Armor || int8(next.Players["a"].Body.Thaco) != stats.Thaco {
		t.Fatalf("equipment stats not refreshed: body=%+v stats=%+v", next.Players["a"].Body, stats)
	}
}

func TestNPCCombatRoundDissolveSelectsHeldAndWieldReadySlots(t *testing.T) {
	for _, tc := range []struct {
		name string
		slot int
		id   string
	}{
		{name: "held", slot: 16, id: "held"},
		{name: "wield", slot: 19, id: "wield"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var ready [20]string
			ready[tc.slot] = tc.id
			s := npcCombatDissolveFixture(t, map[string]Item{tc.id: {Object: LegacyObject{Name: tc.name}}}, ready)
			var calls [][2]int
			proposal, err := s.PlanNPCCombatRound("wolf-id", "a", func(low, high int) int {
				calls = append(calls, [2]int{low, high})
				if low == 1 && high == 100 {
					return 15
				}
				if low == 0 && high == 0 {
					return 0
				}
				return high
			})
			if err != nil || !proposal.Dissolved || proposal.DissolveReadySlot != tc.slot || proposal.DissolveItemID != tc.id || proposal.DissolveCandidateCount != 1 {
				t.Fatalf("proposal=%+v err=%v", proposal, err)
			}
			if want := [][2]int{{1, 20}, {1, 6}, {1, 100}, {0, 0}}; !reflect.DeepEqual(calls, want) {
				t.Fatalf("RNG calls=%v want=%v", calls, want)
			}
			next, result, err := s.ApplyNPCCombatRound(proposal)
			if err != nil || !result.Dissolved || next.Players["a"].Items.Ready[tc.slot] != "" {
				t.Fatalf("result=%+v next=%+v err=%v", result, next, err)
			}
		})
	}
}

func TestNPCCombatRoundDissolveProtectedSelectionConsumesSelectionRNGButNoOps(t *testing.T) {
	item := Item{Object: LegacyObject{Name: "이벤트검", Flags: [8]byte{objectOneWevFlag / 8: 1 << (objectOneWevFlag % 8)}}}
	s := npcCombatDissolveFixture(t, map[string]Item{"held": item}, [20]string{16: "held"})
	before := s.clone()
	var calls [][2]int
	proposal, err := s.PlanNPCCombatRound("wolf-id", "a", func(low, high int) int {
		calls = append(calls, [2]int{low, high})
		if low == 1 && high == 100 {
			return 15
		}
		if low == 0 && high == 0 {
			return 0
		}
		return high
	})
	if err != nil || !proposal.DissolveSucceeded || proposal.Dissolved || !proposal.DissolveProtected || proposal.DissolveItemID != "held" || proposal.DissolveReadySlot != 16 {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	if want := [][2]int{{1, 20}, {1, 6}, {1, 100}, {0, 0}}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("RNG calls=%v want=%v", calls, want)
	}
	if !reflect.DeepEqual(s, before) {
		t.Fatal("planning mutated protected source state")
	}
	next, result, err := s.ApplyNPCCombatRound(proposal)
	if err != nil || !result.DissolveProtected || result.Dissolved || !reflect.DeepEqual(next.Players["a"].Items, s.Players["a"].Items) {
		t.Fatalf("protected selection changed state: result=%+v next=%+v err=%v", result, next, err)
	}
}

func TestNPCCombatRoundDissolveSuccessWithNoReadyItemsSkipsSelectionRNG(t *testing.T) {
	s := npcCombatDissolveFixture(t, map[string]Item{}, [20]string{})
	var calls [][2]int
	proposal, err := s.PlanNPCCombatRound("wolf-id", "a", func(low, high int) int {
		calls = append(calls, [2]int{low, high})
		if low == 1 && high == 100 {
			return 15
		}
		return high
	})
	if err != nil || !proposal.DissolveSucceeded || proposal.Dissolved || proposal.DissolveCandidateCount != 0 || proposal.DissolveItemID != "" {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	if want := [][2]int{{1, 20}, {1, 6}, {1, 100}}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("RNG calls=%v want=%v", calls, want)
	}
	if _, result, err := s.ApplyNPCCombatRound(proposal); err != nil || !result.DissolveSucceeded || result.DissolveCandidateCount != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestNPCCombatRoundDissolveFollowsExistingEffectRNGOrder(t *testing.T) {
	s := npcCombatDissolveFixture(t, map[string]Item{
		"held": {Object: LegacyObject{Name: "쥔검"}},
	}, [20]string{16: "held"})
	npc := s.NPCs["wolf-id"]
	npc.Body.Flags[13/8] |= 1 << (13 % 8) // MPOISS
	npc.Body.Flags[34/8] |= 1 << (34 % 8) // MDISEA
	npc.Body.Flags[45/8] |= 1 << (45 % 8) // MBLNDR
	s.NPCs["wolf-id"] = npc
	var calls [][2]int
	proposal, err := s.PlanNPCCombatRound("wolf-id", "a", func(low, high int) int {
		calls = append(calls, [2]int{low, high})
		switch len(calls) {
		case 1:
			return 20
		case 2:
			return 6
		case 3:
			return 15 // MPOISS
		case 4, 5:
			return 10 // MDISEA/MBLNDR
		case 6:
			return 15 // MDISIT
		case 7:
			return 0 // one ready candidate
		default:
			t.Fatalf("unexpected random request %d..%d", low, high)
			return 0
		}
	})
	if err != nil || !proposal.Poisoned || !proposal.Diseased || !proposal.Blinded || !proposal.Dissolved {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	want := [][2]int{{1, 20}, {1, 6}, {1, 100}, {1, 100}, {1, 100}, {1, 100}, {0, 0}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("RNG calls=%v want=%v", calls, want)
	}
}

func TestNPCCombatRoundRejectsTamperedDissolveSelectionAtomically(t *testing.T) {
	s := npcCombatDissolveFixture(t, map[string]Item{
		"held":  {Object: LegacyObject{Name: "쥔검"}},
		"wield": {Object: LegacyObject{Name: "무기"}},
	}, [20]string{16: "held", 19: "wield"})
	proposal, err := s.PlanNPCCombatRound("wolf-id", "a", func(low, high int) int {
		if low == 1 && high == 100 {
			return 15
		}
		if low == 0 {
			return 0
		}
		return high
	})
	if err != nil {
		t.Fatal(err)
	}
	tampered := proposal
	tampered.DissolveSelectionRoll = 1
	if next, result, err := s.ApplyNPCCombatRound(tampered); err == nil || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(result, NPCCombatRoundResult{}) {
		t.Fatalf("tampered selection accepted: next=%+v result=%+v err=%v", next, result, err)
	}
	tampered = proposal
	tampered.next.Players["a"].Items.Ready[19] = ""
	if next, result, err := s.ApplyNPCCombatRound(tampered); err == nil || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(result, NPCCombatRoundResult{}) {
		t.Fatalf("tampered item candidate accepted: next=%+v result=%+v err=%v", next, result, err)
	}
}

func TestNPCCombatRoundPreservesNPCStealthOnHitAndMiss(t *testing.T) {
	for _, tc := range []struct {
		name string
		roll int
		hit  bool
	}{
		{name: "miss", roll: 1, hit: false},
		{name: "hit", roll: 20, hit: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := npcCombatRoundFixture(t)
			npc := s.NPCs["wolf-id"]
			// PHIDDN/PINVIS are creature bits 1 and 2. C update_active does
			// not clear either bit on the NPC->PLAYER path.
			const stealthMask = byte(1<<1 | 1<<2)
			npc.Body.Flags[0] |= stealthMask
			npc.Body.Thaco = 20
			s.NPCs["wolf-id"] = npc

			proposal, err := s.PlanNPCCombatRound("wolf-id", "a", func(low, high int) int {
				switch {
				case low == 1 && high == 20:
					return tc.roll
				case low == 1 && high == 6:
					return 6
				default:
					t.Fatalf("unexpected random request %d..%d", low, high)
					return 0
				}
			})
			if err != nil || proposal.Hit != tc.hit {
				t.Fatalf("proposal=%+v err=%v", proposal, err)
			}
			if got := proposal.next.NPCs["wolf-id"].Body.Flags[0]; got&stealthMask != stealthMask {
				t.Fatalf("planning cleared NPC stealth flags: %#x", got)
			}
			next, _, err := s.ApplyNPCCombatRound(proposal)
			if err != nil {
				t.Fatal(err)
			}
			if got := next.NPCs["wolf-id"].Body.Flags[0]; got&stealthMask != stealthMask {
				t.Fatalf("applying cleared NPC stealth flags: %#x", got)
			}
		})
	}
}

func TestNPCCombatRoundRequiresExactOnlineSameRoomEnemy(t *testing.T) {
	base := npcCombatRoundFixture(t)
	cases := []struct {
		name string
		fn   func(*State)
	}{
		{name: "missing npc id", fn: func(s *State) { s.NPCs["other"] = s.NPCs["wolf-id"] }},
		{name: "offline player", fn: func(s *State) {
			p := s.Players["a"]
			p.Online = false
			s.Players["a"] = p
			s.Rooms[1] = RoomState{Resource: s.Rooms[1].Resource, NPCIDs: []string{"wolf-id"}}
		}},
		{name: "wrong room", fn: func(s *State) {
			npc := s.NPCs["wolf-id"]
			npc.Body.RoomID = 2
			s.NPCs["wolf-id"] = npc
			s.Rooms[1] = RoomState{Resource: s.Rooms[1].Resource, PlayerIDs: []string{"a"}}
			s.Rooms[2] = RoomState{Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 2}}, NPCIDs: []string{"wolf-id"}}
		}},
		{name: "enemy absent", fn: func(s *State) { n := s.NPCs["wolf-id"]; n.Enemies = nil; s.NPCs["wolf-id"] = n }},
		{name: "wrong enemy", fn: func(s *State) {
			n := s.NPCs["wolf-id"]
			n.Enemies = []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "other"}}}
			s.Players["other"] = PlayerState{Body: LegacyMonster{Name: "Other", RoomID: 2}, Online: false}
			s.NPCs["wolf-id"] = n
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := base.clone()
			tc.fn(&s)
			if _, err := s.PlanNPCCombatRound("wolf-id", "a", func(int, int) int { t.Fatal("invalid context consumed RNG"); return 0 }); err == nil {
				t.Fatal("accepted invalid NPC/player context")
			}
		})
	}
}

func TestNPCCombatRoundRejectsNilRNGAndLethalBeforeStateChange(t *testing.T) {
	s := npcCombatRoundFixture(t)
	if proposal, err := s.PlanNPCCombatRound("wolf-id", "a", nil); err == nil || !reflect.DeepEqual(proposal, NPCCombatRoundProposal{}) {
		t.Fatalf("nil RNG accepted proposal=%+v err=%v", proposal, err)
	}

	lethal := s.clone()
	p := lethal.Players["a"]
	p.Body.HPCurrent = 1
	lethal.Players["a"] = p
	before := lethal.clone()
	proposal, err := lethal.PlanNPCCombatRound("wolf-id", "a", npcCombatRoundRoll(6))
	if err == nil || !strings.Contains(err.Error(), "death") || !reflect.DeepEqual(proposal, NPCCombatRoundProposal{}) || !reflect.DeepEqual(lethal, before) {
		t.Fatalf("lethal proposal was not fail-closed: proposal=%+v err=%v stateChanged=%v", proposal, err, !reflect.DeepEqual(lethal, before))
	}
}

func TestNPCCombatRoundRejectsStaleAndTamperedProposalAtomically(t *testing.T) {
	s := npcCombatRoundFixture(t)
	proposal, err := s.PlanNPCCombatRound("wolf-id", "a", npcCombatRoundRoll(6))
	if err != nil {
		t.Fatal(err)
	}
	changed := s.clone()
	p := changed.Players["a"]
	p.Body.HPCurrent = 39
	changed.Players["a"] = p
	if next, result, err := changed.ApplyNPCCombatRound(proposal); err == nil || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(result, NPCCombatRoundResult{}) {
		t.Fatalf("stale proposal accepted: next=%+v result=%+v err=%v", next, result, err)
	}

	tampered := proposal
	tampered.Damage++
	if next, result, err := s.ApplyNPCCombatRound(tampered); err == nil || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(result, NPCCombatRoundResult{}) {
		t.Fatalf("tampered damage accepted: next=%+v result=%+v err=%v", next, result, err)
	}
	tampered = proposal
	tampered.NPCID = "other"
	if next, result, err := s.ApplyNPCCombatRound(tampered); err == nil || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(result, NPCCombatRoundResult{}) {
		t.Fatalf("tampered identity accepted: next=%+v result=%+v err=%v", next, result, err)
	}
}

func TestNPCCombatRoundMBEFUDAttenuatesMeleeDamageWithoutExtraRNG(t *testing.T) {
	for _, tc := range []struct {
		name         string
		befuddled    bool
		wantDamage   int
		wantPlayerHP int
	}{
		{name: "ordinary damage unchanged without MBEFUD", wantDamage: 6, wantPlayerHP: 34},
		{name: "ordinary damage 6 becomes 2", befuddled: true, wantDamage: 2, wantPlayerHP: 38},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := npcCombatRoundFixture(t)
			npc := s.NPCs["wolf-id"]
			npc.Body.DiceSides = 6
			if tc.befuddled {
				npc.Body.Flags[51/8] |= 1 << (51 % 8) // MBEFUD
			}
			s.NPCs["wolf-id"] = npc
			var calls [][2]int
			proposal, err := s.PlanNPCCombatRound("wolf-id", "a", func(low, high int) int {
				calls = append(calls, [2]int{low, high})
				return high
			})
			if err != nil {
				t.Fatal(err)
			}
			if !proposal.Hit || proposal.Damage != tc.wantDamage || proposal.PlayerHPAfter != tc.wantPlayerHP {
				t.Fatalf("proposal=%+v", proposal)
			}
			if want := [][2]int{{1, 20}, {1, 6}}; !reflect.DeepEqual(calls, want) {
				t.Fatalf("RNG calls=%v want=%v", calls, want)
			}
			next, result, err := s.ApplyNPCCombatRound(proposal)
			if err != nil {
				t.Fatal(err)
			}
			if result.Damage != tc.wantDamage || result.PlayerHP != tc.wantPlayerHP || next.Players["a"].Body.HPCurrent != int16(tc.wantPlayerHP) {
				t.Fatalf("result=%+v next=%+v", result, next)
			}
		})
	}
}

func TestNPCCombatRoundMBEFUDAttenuatesBreathDamageWithoutChangingOrder(t *testing.T) {
	for _, tc := range []struct {
		name       string
		befuddled  bool
		wantDamage int
	}{
		{name: "breath damage unchanged without MBEFUD", wantDamage: 8},
		{name: "breath damage 8 becomes 2", befuddled: true, wantDamage: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := npcCombatRoundFixture(t)
			npc := s.NPCs["wolf-id"]
			npc.Body.Level = 5                    // two level-band fire dice: ((5 + 3) / 4) == 2.
			npc.Body.Flags[19/8] |= 1 << (19 % 8) // MBRETH
			if tc.befuddled {
				npc.Body.Flags[51/8] |= 1 << (51 % 8) // MBEFUD
			}
			s.NPCs["wolf-id"] = npc
			var calls [][2]int
			proposal, err := s.PlanNPCCombatRound("wolf-id", "a", func(low, high int) int {
				calls = append(calls, [2]int{low, high})
				if high == 30 {
					return 4
				}
				return high
			})
			if err != nil {
				t.Fatal(err)
			}
			if !proposal.Hit || !proposal.BreathTriggered || proposal.Damage != tc.wantDamage || proposal.PlayerHPAfter != 40-tc.wantDamage {
				t.Fatalf("proposal=%+v", proposal)
			}
			if want := [][2]int{{1, 20}, {1, 30}, {1, 4}, {1, 4}}; !reflect.DeepEqual(calls, want) {
				t.Fatalf("RNG calls=%v want=%v", calls, want)
			}
			next, result, err := s.ApplyNPCCombatRound(proposal)
			if err != nil {
				t.Fatal(err)
			}
			if result.Damage != tc.wantDamage || result.PlayerHP != 40-tc.wantDamage || next.Players["a"].Body.HPCurrent != int16(40-tc.wantDamage) {
				t.Fatalf("result=%+v next=%+v", result, next)
			}
		})
	}
}

func TestNPCCombatRoundMBEFUDTurnsBaseDamageOneIntoZero(t *testing.T) {
	s := npcCombatRoundFixture(t)
	npc := s.NPCs["wolf-id"]
	npc.Body.DiceSides = 1
	npc.Body.Flags[51/8] |= 1 << (51 % 8) // MBEFUD
	s.NPCs["wolf-id"] = npc
	var calls [][2]int
	proposal, err := s.PlanNPCCombatRound("wolf-id", "a", func(low, high int) int {
		calls = append(calls, [2]int{low, high})
		return high
	})
	if err != nil {
		t.Fatal(err)
	}
	if !proposal.Hit || proposal.Damage != 0 || proposal.PlayerHPBefore != 40 || proposal.PlayerHPAfter != 40 {
		t.Fatalf("proposal=%+v", proposal)
	}
	if want := [][2]int{{1, 20}, {1, 1}}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("RNG calls=%v want=%v", calls, want)
	}
	next, result, err := s.ApplyNPCCombatRound(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if result.Damage != 0 || result.PlayerHP != 40 || next.Players["a"].Body.HPCurrent != 40 {
		t.Fatalf("result=%+v next=%+v", result, next)
	}
}

func TestNPCCombatRoundMBEFUDMissDoesNotAttenuateOrConsumeRNG(t *testing.T) {
	s := npcCombatRoundFixture(t)
	npc := s.NPCs["wolf-id"]
	npc.Body.Thaco = 20
	for _, bit := range []uint{19, 51, 13, 34, 45} { // MBRETH, MBEFUD, and post-hit effects.
		npc.Body.Flags[bit/8] |= 1 << (bit % 8)
	}
	s.NPCs["wolf-id"] = npc
	var calls [][2]int
	proposal, err := s.PlanNPCCombatRound("wolf-id", "a", func(low, high int) int {
		calls = append(calls, [2]int{low, high})
		if high != 20 {
			t.Fatalf("unexpected random request %d..%d", low, high)
		}
		return 1
	})
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Hit || proposal.Damage != 0 || proposal.PlayerHPAfter != 40 {
		t.Fatalf("proposal=%+v", proposal)
	}
	if want := [][2]int{{1, 20}}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("RNG calls=%v want=%v", calls, want)
	}
	next, result, err := s.ApplyNPCCombatRound(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if result.Hit || result.Damage != 0 || result.PlayerHP != 40 || !reflect.DeepEqual(next, s) {
		t.Fatalf("result=%+v next=%+v", result, next)
	}
}

func TestNPCCombatRoundMBEFUDRunsBeforePostHitEffects(t *testing.T) {
	s := npcCombatRoundFixture(t)
	npc := s.NPCs["wolf-id"]
	for _, bit := range []uint{51, 13, 34, 45} { // MBEFUD, MPOISS, MDISEA, MBLNDR.
		npc.Body.Flags[bit/8] |= 1 << (bit % 8)
	}
	s.NPCs["wolf-id"] = npc
	var calls [][2]int
	proposal, err := s.PlanNPCCombatRound("wolf-id", "a", func(low, high int) int {
		calls = append(calls, [2]int{low, high})
		switch len(calls) {
		case 1:
			return 20 // hit
		case 2:
			return 6 // ordinary damage before MBEFUD attenuation
		case 3:
			return 15 // MPOISS
		case 4:
			return 10 // MDISEA
		case 5:
			return 11 // MBLNDR misses
		default:
			t.Fatalf("unexpected random request %d..%d", low, high)
			return 0
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !proposal.Hit || proposal.Damage != 2 || !proposal.Poisoned || !proposal.Diseased || proposal.Blinded || proposal.PlayerHPAfter != 38 {
		t.Fatalf("proposal=%+v", proposal)
	}
	if want := [][2]int{{1, 20}, {1, 6}, {1, 100}, {1, 100}, {1, 100}}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("RNG calls=%v want=%v", calls, want)
	}
	next, result, err := s.ApplyNPCCombatRound(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if result.Damage != 2 || result.PlayerHP != 38 || !result.Poisoned || !result.Diseased || result.Blinded {
		t.Fatalf("result=%+v", result)
	}
	player := next.Players["a"]
	for _, bit := range []uint{16, 41} {
		if !flag(player.Body.Flags[:], bit) {
			t.Fatalf("effect flag %d missing: flags=%#x", bit, player.Body.Flags)
		}
	}
	if flag(player.Body.Flags[:], 42) {
		t.Fatalf("unexpected blind flag: flags=%#x", player.Body.Flags)
	}
}

func TestNPCCombatRoundMBEFUDRejectsStaleAndTamperedCandidatesAtomically(t *testing.T) {
	s := npcCombatRoundFixture(t)
	npc := s.NPCs["wolf-id"]
	npc.Body.DiceSides = 6
	npc.Body.Flags[51/8] |= 1 << (51 % 8) // MBEFUD
	s.NPCs["wolf-id"] = npc
	proposal, err := s.PlanNPCCombatRound("wolf-id", "a", npcCombatRoundRoll(6))
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Damage != 2 {
		t.Fatalf("proposal=%+v", proposal)
	}

	assertRejected := func(name string, state State, candidate NPCCombatRoundProposal) {
		t.Helper()
		before := state.clone()
		next, result, err := state.ApplyNPCCombatRound(candidate)
		if err == nil || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(result, NPCCombatRoundResult{}) {
			t.Fatalf("%s accepted: next=%+v result=%+v err=%v", name, next, result, err)
		}
		if !reflect.DeepEqual(state, before) {
			t.Fatalf("%s mutated source state", name)
		}
	}

	tampered := proposal
	tampered.Damage = 6 // Unattenuated candidate must not be accepted.
	assertRejected("tampered unattenuated damage", s, tampered)
	tampered = proposal
	tampered.next = tampered.next.clone()
	tamperedPlayer := tampered.next.Players["a"]
	tamperedPlayer.Body.HPCurrent = 34
	tampered.next.Players["a"] = tamperedPlayer
	assertRejected("tampered unattenuated candidate state", s, tampered)
	changed := s.clone()
	player := changed.Players["a"]
	player.Body.HPCurrent--
	changed.Players["a"] = player
	assertRejected("stale MBEFUD candidate", changed, proposal)
}

func npcCombatMENEDRFixture(t *testing.T, experience int32) State {
	t.Helper()
	s := npcCombatRoundFixture(t)
	player := s.Players["a"]
	player.Body.Experience = experience
	player.Body.Proficiency = [5]int32{10, 20, 30, 40, 50}
	player.Body.Realm = [4]int32{1, 2, 3, 4}
	s.Players["a"] = player
	npc := s.NPCs["wolf-id"]
	npc.Body.Level = 5 // MENEDR band=(level+3)/4 == 2.
	npc.Body.DiceSides = 6
	npc.Body.Flags[30/8] |= 1 << (30 % 8) // MENEDR
	s.NPCs["wolf-id"] = npc
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestNPCCombatRoundMissPreservesNonzeroProgression(t *testing.T) {
	s := npcCombatRoundFixture(t)
	player := s.Players["a"]
	player.Body.Experience = 1234
	player.Body.Proficiency = [5]int32{10, 20, 30, 40, 50}
	player.Body.Realm = [4]int32{1, 2, 3, 4}
	s.Players["a"] = player
	npc := s.NPCs["wolf-id"]
	npc.Body.Thaco = 20
	s.NPCs["wolf-id"] = npc
	before := s.Players["a"].Body

	var calls [][2]int
	proposal, err := s.PlanNPCCombatRound("wolf-id", "a", func(low, high int) int {
		calls = append(calls, [2]int{low, high})
		if low != 1 || high != 20 {
			t.Fatalf("unexpected random request %d..%d", low, high)
		}
		return 1
	})
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Hit || proposal.Damage != 0 || proposal.PlayerHPBefore != 40 || proposal.PlayerHPAfter != 40 {
		t.Fatalf("proposal=%+v", proposal)
	}
	if proposal.ExperienceBefore != before.Experience || proposal.ExperienceAfter != before.Experience || proposal.ProficiencyBefore != before.Proficiency || proposal.ProficiencyAfter != before.Proficiency || proposal.RealmBefore != before.Realm || proposal.RealmAfter != before.Realm {
		t.Fatalf("miss progression snapshot=%+v before=%+v", proposal, before)
	}
	if want := [][2]int{{1, 20}}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("RNG calls=%v want=%v", calls, want)
	}

	next, result, err := s.ApplyNPCCombatRound(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if result.Hit || result.Damage != 0 || result.PlayerHP != 40 || result.TargetHP != 40 || result.ExperienceBefore != before.Experience || result.ExperienceAfter != before.Experience || result.ProficiencyBefore != before.Proficiency || result.ProficiencyAfter != before.Proficiency || result.RealmBefore != before.Realm || result.RealmAfter != before.Realm {
		t.Fatalf("result=%+v before=%+v", result, before)
	}
	if !reflect.DeepEqual(next, s) {
		t.Fatalf("miss mutated state: next=%+v before=%+v", next, s)
	}
}

func TestNPCCombatRoundMENEDRFreeAllowsAggregateOverflowProgression(t *testing.T) {
	s := npcCombatRoundFixture(t)
	player := s.Players["a"]
	maxInt32 := int32(^uint32(0) >> 1)
	player.Body.Experience = 1234
	player.Body.Proficiency = [5]int32{maxInt32, maxInt32, maxInt32, maxInt32, maxInt32}
	player.Body.Realm = [4]int32{maxInt32, maxInt32, maxInt32, maxInt32}
	s.Players["a"] = player
	before := s.Players["a"].Body

	proposal, err := s.PlanNPCCombatRound("wolf-id", "a", npcCombatRoundRoll(6))
	if err != nil {
		t.Fatal(err)
	}
	if !proposal.Hit || proposal.Damage != 6 || proposal.ExperienceBefore != before.Experience || proposal.ExperienceAfter != before.Experience || proposal.ProficiencyBefore != before.Proficiency || proposal.ProficiencyAfter != before.Proficiency || proposal.RealmBefore != before.Realm || proposal.RealmAfter != before.Realm {
		t.Fatalf("proposal=%+v before=%+v", proposal, before)
	}

	next, result, err := s.ApplyNPCCombatRound(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExperienceBefore != before.Experience || result.ExperienceAfter != before.Experience || result.ProficiencyBefore != before.Proficiency || result.ProficiencyAfter != before.Proficiency || result.RealmBefore != before.Realm || result.RealmAfter != before.Realm {
		t.Fatalf("result=%+v before=%+v", result, before)
	}
	got := next.Players["a"].Body
	if got.Experience != before.Experience || got.Proficiency != before.Proficiency || got.Realm != before.Realm || got.HPCurrent != 34 {
		t.Fatalf("MENEDR-free progression changed: got=%+v before=%+v", got, before)
	}
}

func TestNPCCombatRoundMENEDRAbsentAndRollTenPreserveProgression(t *testing.T) {
	for _, tc := range []struct {
		name       string
		menedr     bool
		wantCalls  [][2]int
		energyRoll int
	}{
		{
			name:      "MENEDR absent",
			wantCalls: [][2]int{{1, 20}, {1, 6}},
		},
		{
			name:       "MENEDR roll ten",
			menedr:     true,
			energyRoll: 10,
			wantCalls:  [][2]int{{1, 20}, {1, 100}, {1, 6}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := npcCombatMENEDRFixture(t, 1000)
			if !tc.menedr {
				npc := s.NPCs["wolf-id"]
				npc.Body.Flags[30/8] &^= 1 << (30 % 8)
				s.NPCs["wolf-id"] = npc
			}
			before := s.Players["a"].Body
			var calls [][2]int
			proposal, err := s.PlanNPCCombatRound("wolf-id", "a", func(low, high int) int {
				calls = append(calls, [2]int{low, high})
				switch high {
				case 20:
					return 20
				case 100:
					return tc.energyRoll
				case 6:
					return 6
				default:
					t.Fatalf("unexpected random request %d..%d", low, high)
					return 0
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			if !proposal.Hit || proposal.Damage != 6 || proposal.EnergyDrainTriggered || proposal.EnergyDrain != 0 || proposal.ExperienceBefore != 1000 || proposal.ExperienceAfter != 1000 {
				t.Fatalf("proposal=%+v", proposal)
			}
			if !reflect.DeepEqual(calls, tc.wantCalls) {
				t.Fatalf("RNG calls=%v want=%v", calls, tc.wantCalls)
			}
			next, result, err := s.ApplyNPCCombatRound(proposal)
			if err != nil {
				t.Fatal(err)
			}
			if result.EnergyDrainTriggered || result.EnergyDrain != 0 || result.ExperienceBefore != 1000 || result.ExperienceAfter != 1000 {
				t.Fatalf("result=%+v", result)
			}
			got := next.Players["a"].Body
			if got.Experience != before.Experience || got.Proficiency != before.Proficiency || got.Realm != before.Realm || got.HPCurrent != 34 {
				t.Fatalf("progression or HP changed: got=%+v before=%+v", got, before)
			}
		})
	}
}

func TestNPCCombatRoundMENEDRRollNineConsumesEnergyThenOrdinaryDamage(t *testing.T) {
	s := npcCombatMENEDRFixture(t, 1000)
	var calls [][2]int
	proposal, err := s.PlanNPCCombatRound("wolf-id", "a", func(low, high int) int {
		calls = append(calls, [2]int{low, high})
		switch high {
		case 20:
			return 20
		case 100:
			return 9
		case 5:
			return 5
		case 6:
			return 6
		default:
			t.Fatalf("unexpected random request %d..%d", low, high)
			return 0
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !proposal.Hit || !proposal.EnergyDrainTriggered || proposal.EnergyRoll != 9 || proposal.EnergyBand != 2 || proposal.EnergyDiceCount != 2 || proposal.EnergyDiceSides != 5 || proposal.EnergyDicePlus != 10 || proposal.EnergyDrain != 20 || proposal.ExperienceBefore != 1000 || proposal.ExperienceAfter != 980 || proposal.Damage != 6 || proposal.PlayerHPAfter != 34 {
		t.Fatalf("proposal=%+v", proposal)
	}
	if want := [][2]int{{1, 20}, {1, 100}, {1, 5}, {1, 5}, {1, 6}}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("RNG calls=%v want=%v", calls, want)
	}
	next, result, err := s.ApplyNPCCombatRound(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if result.EnergyDrain != 20 || result.ExperienceBefore != 1000 || result.ExperienceAfter != 980 || result.Damage != 6 {
		t.Fatalf("result=%+v", result)
	}
	player := next.Players["a"].Body
	if player.Experience != 980 || player.Proficiency != [5]int32{8, 18, 28, 38, 1024} || player.Realm != [4]int32{} || player.HPCurrent != 34 {
		t.Fatalf("player after MENEDR=%+v", player)
	}
}

func TestNPCCombatRoundMENEDRClampsDrainAtZeroExperienceButKeepsRNGOrder(t *testing.T) {
	s := npcCombatMENEDRFixture(t, 0)
	var calls [][2]int
	proposal, err := s.PlanNPCCombatRound("wolf-id", "a", func(low, high int) int {
		calls = append(calls, [2]int{low, high})
		switch high {
		case 20:
			return 20
		case 100:
			return 9
		case 5:
			return 5
		case 6:
			return 6
		default:
			t.Fatalf("unexpected random request %d..%d", low, high)
			return 0
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if proposal.EnergyDrain != 0 || proposal.ExperienceBefore != 0 || proposal.ExperienceAfter != 0 || proposal.PlayerHPAfter != 34 {
		t.Fatalf("proposal=%+v", proposal)
	}
	if want := [][2]int{{1, 20}, {1, 100}, {1, 5}, {1, 5}, {1, 6}}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("RNG calls=%v want=%v", calls, want)
	}
	next, result, err := s.ApplyNPCCombatRound(proposal)
	if err != nil {
		t.Fatal(err)
	}
	player := next.Players["a"].Body
	if result.EnergyDrain != 0 || player.Experience != 0 || player.Proficiency != [5]int32{10, 20, 30, 40, 1024} || player.Realm != [4]int32{1, 2, 3, 4} || player.HPCurrent != 34 {
		t.Fatalf("clamped MENEDR result=%+v player=%+v", result, player)
	}
}

func TestNPCCombatRoundMENEDRSkipsEnergyOnBreathAndEvaluatesOnFallback(t *testing.T) {
	for _, tc := range []struct {
		name       string
		breathRoll int
		wantDrain  int
		wantCalls  [][2]int
		wantXP     int32
	}{
		{
			name:       "breath trigger",
			breathRoll: 4,
			wantCalls:  [][2]int{{1, 20}, {1, 30}, {1, 4}, {1, 4}},
			wantXP:     1000,
		},
		{
			name:       "ordinary fallback",
			breathRoll: 5,
			wantDrain:  20,
			wantCalls:  [][2]int{{1, 20}, {1, 30}, {1, 100}, {1, 5}, {1, 5}, {1, 6}},
			wantXP:     980,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := npcCombatMENEDRFixture(t, 1000)
			npc := s.NPCs["wolf-id"]
			npc.Body.Flags[19/8] |= 1 << (19 % 8) // MBRETH
			s.NPCs["wolf-id"] = npc
			var calls [][2]int
			proposal, err := s.PlanNPCCombatRound("wolf-id", "a", func(low, high int) int {
				calls = append(calls, [2]int{low, high})
				switch high {
				case 20:
					return 20
				case 30:
					return tc.breathRoll
				case 4:
					return 4
				case 100:
					return 9
				case 5:
					return 5
				case 6:
					return 6
				default:
					t.Fatalf("unexpected random request %d..%d", low, high)
					return 0
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			if proposal.BreathTriggered != (tc.breathRoll < 5) || proposal.EnergyDrain != tc.wantDrain || proposal.ExperienceAfter != tc.wantXP {
				t.Fatalf("proposal=%+v", proposal)
			}
			if !reflect.DeepEqual(calls, tc.wantCalls) {
				t.Fatalf("RNG calls=%v want=%v", calls, tc.wantCalls)
			}
			next, result, err := s.ApplyNPCCombatRound(proposal)
			if err != nil {
				t.Fatal(err)
			}
			if result.EnergyDrain != tc.wantDrain || next.Players["a"].Body.Experience != tc.wantXP {
				t.Fatalf("result=%+v player=%+v", result, next.Players["a"].Body)
			}
		})
	}
}

func TestNPCCombatRoundMBEFUDDoesNotAttenuateMENEDR(t *testing.T) {
	s := npcCombatMENEDRFixture(t, 1000)
	npc := s.NPCs["wolf-id"]
	npc.Body.Flags[51/8] |= 1 << (51 % 8) // MBEFUD
	s.NPCs["wolf-id"] = npc
	proposal, err := s.PlanNPCCombatRound("wolf-id", "a", func(low, high int) int {
		switch high {
		case 20:
			return 20
		case 100:
			return 9
		case 5:
			return 5
		case 6:
			return 6
		default:
			t.Fatalf("unexpected random request %d..%d", low, high)
			return 0
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if proposal.EnergyDrain != 20 || proposal.Damage != 2 || proposal.PlayerHPAfter != 38 || proposal.ExperienceAfter != 980 {
		t.Fatalf("proposal=%+v", proposal)
	}
	next, result, err := s.ApplyNPCCombatRound(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if result.EnergyDrain != 20 || result.Damage != 2 || next.Players["a"].Body.Experience != 980 || next.Players["a"].Body.HPCurrent != 38 {
		t.Fatalf("result=%+v player=%+v", result, next.Players["a"].Body)
	}
}

func TestNPCCombatRoundMENEDRRejectsNegativeProgressionAndOverflowCandidatesAtomically(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*State)
	}{
		{name: "negative experience", mutate: func(s *State) { p := s.Players["a"]; p.Body.Experience = -1; s.Players["a"] = p }},
		{name: "negative weapon proficiency", mutate: func(s *State) { p := s.Players["a"]; p.Body.Proficiency[0] = -1; s.Players["a"] = p }},
		{name: "negative realm proficiency", mutate: func(s *State) { p := s.Players["a"]; p.Body.Realm[0] = -1; s.Players["a"] = p }},
		{name: "proficiency total overflow", mutate: func(s *State) {
			p := s.Players["a"]
			maxInt32 := int32(^uint32(0) >> 1)
			p.Body.Proficiency = [5]int32{maxInt32, maxInt32, maxInt32, maxInt32, maxInt32}
			p.Body.Realm = [4]int32{maxInt32, maxInt32, maxInt32, maxInt32}
			s.Players["a"] = p
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := npcCombatMENEDRFixture(t, 1000)
			test.mutate(&s)
			before := s.clone()
			called := false
			proposal, err := s.PlanNPCCombatRound("wolf-id", "a", func(int, int) int {
				called = true
				return 20
			})
			if err == nil || called || !reflect.DeepEqual(proposal, NPCCombatRoundProposal{}) || !reflect.DeepEqual(s, before) {
				t.Fatalf("accepted or mutated invalid progression proposal=%+v called=%v err=%v", proposal, called, err)
			}
		})
	}

	s := npcCombatMENEDRFixture(t, 1000)
	proposal, err := s.PlanNPCCombatRound("wolf-id", "a", func(low, high int) int {
		switch high {
		case 20:
			return 20
		case 100:
			return 9
		case 5:
			return 5
		case 6:
			return 6
		default:
			t.Fatalf("unexpected random request %d..%d", low, high)
			return 0
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	assertRejected := func(name string, candidate NPCCombatRoundProposal) {
		t.Helper()
		before := s.clone()
		next, result, applyErr := s.ApplyNPCCombatRound(candidate)
		if applyErr == nil || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(result, NPCCombatRoundResult{}) || !reflect.DeepEqual(s, before) {
			t.Fatalf("%s accepted or mutated: next=%+v result=%+v err=%v", name, next, result, applyErr)
		}
	}
	for _, test := range []struct {
		name   string
		mutate func(*NPCCombatRoundProposal)
	}{
		{name: "tampered energy", mutate: func(p *NPCCombatRoundProposal) { p.EnergyDrain++ }},
		{name: "tampered expected experience", mutate: func(p *NPCCombatRoundProposal) { p.ExperienceAfter++ }},
		{name: "tampered expected weapon proficiency", mutate: func(p *NPCCombatRoundProposal) { p.ProficiencyAfter[0]++ }},
		{name: "tampered expected realm", mutate: func(p *NPCCombatRoundProposal) { p.RealmAfter[0]++ }},
	} {
		tampered := proposal
		test.mutate(&tampered)
		assertRejected(test.name, tampered)
	}
	stale := s.clone()
	player := stale.Players["a"]
	player.Body.Experience++
	stale.Players["a"] = player
	if next, result, err := stale.ApplyNPCCombatRound(proposal); err == nil || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(result, NPCCombatRoundResult{}) {
		t.Fatalf("stale progression accepted: next=%+v result=%+v err=%v", next, result, err)
	}
}
