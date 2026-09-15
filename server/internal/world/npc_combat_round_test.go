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
