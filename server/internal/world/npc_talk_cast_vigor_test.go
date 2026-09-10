package world

import (
	"strings"
	"testing"
)

func npcTalkHealingState(t *testing.T, spell string) State {
	t.Helper()
	s := npcTalkFixture(t)
	actor := s.Players["a"]
	actor.Body.HPMax = 100
	actor.Body.HPCurrent = 12
	s.Players["a"] = actor
	npc := s.NPCs["guide-one"]
	npc.Body.Level = 5
	npc.Body.Class = clericClass
	npc.Body.Stats[3] = 10
	npc.Body.Stats[4] = 14
	npc.Body.MPMax, npc.Body.MPCurrent = 40, 40
	npc.Body.Flags[npcTalkFlag/8] |= 1 << (npcTalkFlag % 8)
	spec, err := npcTalkCastSpecFor(spell)
	if err != nil {
		t.Fatal(err)
	}
	npc.Body.Spells[spec.Spell/8] |= 1 << (spec.Spell % 8)
	s.NPCs["guide-one"] = npc
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestNPCTalkCastHealingSpellsUseSourceRollOrderAndRoomBonus(t *testing.T) {
	tests := []struct {
		spell       string
		rolls       []int
		bounds      [][2]int
		wantHP      int16
		wantDelta   int32
		wantCost    int16
		wantMessage string
	}{
		{spell: "회복", rolls: []int{2, 4, 3}, bounds: [][2]int{{1, 2}, {1, 6}, {1, 3}}, wantHP: 24, wantDelta: 12, wantCost: 2, wantMessage: "회복을 기원"},
		{spell: "원기회복", rolls: []int{2, 3, 4, 5}, bounds: [][2]int{{1, 2}, {1, 6}, {1, 6}, {1, 6}}, wantHP: 32, wantDelta: 20, wantCost: 4, wantMessage: "원기회복의 주문"},
	}
	for _, tc := range tests {
		t.Run(tc.spell, func(t *testing.T) {
			s := npcTalkHealingState(t, tc.spell)
			room := s.Rooms[1]
			room.Resource.Flags[npcTalkRoomMagicExtend/8] |= 1 << (npcTalkRoomMagicExtend % 8)
			s.Rooms[1] = room
			catalog := npcTalkCastCatalog(t, tc.spell)
			calls := 0
			proposal, err := s.PlanNPCTalkProposalWithEffectOptions("a", "Guide", 1, "quest", catalog, NPCTalkEffectOptions{
				Roll: func(low, high int) int {
					if calls >= len(tc.bounds) {
						t.Fatalf("unexpected random call %d", calls)
					}
					if got := [2]int{low, high}; got != tc.bounds[calls] {
						t.Fatalf("roll bounds=%d..%d want=%d..%d", low, high, tc.bounds[calls][0], tc.bounds[calls][1])
					}
					value := tc.rolls[calls]
					calls++
					return value
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if !proposal.CastAttempted || !proposal.CastSucceeded || proposal.CastFailed || proposal.CastRoll != 0 || proposal.CastChance != 100 || proposal.CastInterval != 0 || proposal.CastHPDelta != tc.wantDelta || len(proposal.CastEffectRolls) != len(tc.rolls) {
				t.Fatalf("proposal=%+v", proposal)
			}
			next, result, err := s.ApplyNPCTalk(proposal, catalog)
			if err != nil {
				t.Fatal(err)
			}
			if calls != len(tc.rolls) || next.Players["a"].Body.HPCurrent != tc.wantHP || next.NPCs["guide-one"].Body.MPCurrent != 40-tc.wantCost || result.SpellHPDelta != tc.wantDelta || len(result.SpellEffectRolls) != len(tc.rolls) || result.Event == nil || !strings.Contains(result.Response, tc.wantMessage) {
				t.Fatalf("calls=%d result=%+v actor=%+v npc=%+v", calls, result, next.Players["a"].Body, next.NPCs["guide-one"].Body)
			}
		})
	}
}

func TestNPCTalkCastMendFailureConsumesOnlyManaForSourceFailClass(t *testing.T) {
	s := npcTalkHealingState(t, "원기회복")
	npc := s.NPCs["guide-one"]
	npc.Body.Class = 2 // BARBARIAN: mend invokes spell_fail before healing rolls.
	s.NPCs["guide-one"] = npc
	catalog := npcTalkCastCatalog(t, "원기회복")
	calls := 0
	proposal, err := s.PlanNPCTalkProposalWithEffectOptions("a", "Guide", 1, "quest", catalog, NPCTalkEffectOptions{Roll: func(low, high int) int {
		calls++
		if low != 1 || high != 100 {
			t.Fatalf("spell-fail bounds=%d..%d", low, high)
		}
		return 100
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !proposal.CastAttempted || proposal.CastSucceeded || !proposal.CastFailed || proposal.CastRoll != 100 || proposal.CastChance != 10 || len(proposal.CastEffectRolls) != 0 {
		t.Fatalf("proposal=%+v", proposal)
	}
	next, result, err := s.ApplyNPCTalk(proposal, catalog)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || !result.SpellFailed || next.Players["a"].Body.HPCurrent != 12 || next.NPCs["guide-one"].Body.MPCurrent != 36 {
		t.Fatalf("calls=%d result=%+v actor=%+v npc=%+v", calls, result, next.Players["a"].Body, next.NPCs["guide-one"].Body)
	}
}
