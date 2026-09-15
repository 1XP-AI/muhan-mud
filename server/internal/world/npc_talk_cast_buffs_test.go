package world

import (
	"strings"
	"testing"
)

func npcTalkTimedBuffState(t *testing.T, spell string) (State, npcTalkCastSpec) {
	t.Helper()
	s := npcTalkFixture(t)
	spec, err := npcTalkCastSpecFor(spell)
	if err != nil {
		t.Fatal(err)
	}
	actor := s.Players["a"]
	actor.Body.Level = 1
	actor.Body.Class = 4
	actor.Body.Stats = [5]byte{10, 10, 10, 10, 10}
	actor.Body.Flags = [8]byte{}
	s.Players["a"] = actor

	npc := s.NPCs["guide-one"]
	npc.Body.Level = 5
	npc.Body.Class = 4 // these utility spells have no source class gate
	npc.Body.Stats[3] = 10
	npc.Body.MPMax, npc.Body.MPCurrent = 40, 40
	npc.Body.Flags[npcTalkFlag/8] |= 1 << (npcTalkFlag % 8)
	npc.Body.Spells[spec.Spell/8] |= 1 << (spec.Spell % 8)
	s.NPCs["guide-one"] = npc
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	return s, spec
}

func TestNPCTalkCastTimedUtilityBuffsUseSourceSlotsAndIntervals(t *testing.T) {
	tests := []struct {
		spell       string
		wantFlag    uint
		wantTimer   int
		wantBase    int32
		wantCost    int16
		wantMessage string
	}{
		{spell: "부양술", wantFlag: npcTalkLevitateFlag, wantTimer: npcTalkLevitateTimer, wantBase: 2400, wantCost: 10, wantMessage: "부양부적"},
		{spell: "방열진", wantFlag: npcTalkResistFireFlag, wantTimer: npcTalkResistFireTimer, wantBase: 1200, wantCost: 12, wantMessage: "방열부적"},
		{spell: "비상술", wantFlag: npcTalkFlyFlag, wantTimer: npcTalkFlyTimer, wantBase: 1200, wantCost: 15, wantMessage: "비상부"},
		{spell: "보마진", wantFlag: npcTalkResistMagicFlag, wantTimer: npcTalkResistMagicTimer, wantBase: 1200, wantCost: 12, wantMessage: "보마부"},
		{spell: "방한진", wantFlag: npcTalkResistColdFlag, wantTimer: npcTalkResistColdTimer, wantBase: 1200, wantCost: 12, wantMessage: "방한진"},
		{spell: "지방호", wantFlag: npcTalkEarthShieldFlag, wantTimer: npcTalkEarthShieldTimer, wantBase: 1200, wantCost: 12, wantMessage: "지방호"},
	}
	for _, tc := range tests {
		t.Run(tc.spell, func(t *testing.T) {
			s, _ := npcTalkTimedBuffState(t, tc.spell)
			catalog := npcTalkCastCatalog(t, tc.spell)
			room := s.Rooms[1]
			room.Resource.Flags[npcTalkRoomMagicExtend/8] |= 1 << (npcTalkRoomMagicExtend % 8)
			s.Rooms[1] = room
			proposal, err := s.PlanNPCTalkProposalWithEffectOptions("a", "Guide", 1, "quest", catalog, NPCTalkEffectOptions{
				Now: 1000,
				Roll: func(low, high int) int {
					if low != 1 || high != 100 {
						t.Fatalf("roll bounds=%d..%d", low, high)
					}
					return 1
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			wantInterval := tc.wantBase + 800
			if tc.spell == "비상술" {
				wantInterval = tc.wantBase + 600
			}
			if proposal.CastInterval != wantInterval || proposal.CastNow != 1000 || proposal.CastSpellName != tc.spell || !proposal.CastAttempted || !proposal.CastSucceeded || proposal.CastFailed {
				t.Fatalf("proposal=%+v want interval=%d", proposal, wantInterval)
			}
			next, result, err := s.ApplyNPCTalk(proposal, catalog)
			if err != nil {
				t.Fatal(err)
			}
			actor := next.Players["a"]
			if !flag(actor.Body.Flags[:], tc.wantFlag) || actor.Body.Timers[tc.wantTimer] != (LegacyTimer{LastTime: 1000, Interval: wantInterval}) {
				t.Fatalf("actor effect=%+v", actor.Body)
			}
			if next.NPCs["guide-one"].Body.MPCurrent != 40-tc.wantCost || result.SpellInterval != wantInterval || result.Event == nil || !strings.Contains(result.Response, tc.wantMessage) || !strings.Contains(result.Event.RoomText, tc.wantMessage) {
				t.Fatalf("result=%+v actor=%+v npc=%+v", result, actor.Body, next.NPCs["guide-one"].Body)
			}
		})
	}
}

func TestNPCTalkCastTimedUtilityBuffFailureOnlyConsumesNPCMana(t *testing.T) {
	s, _ := npcTalkTimedBuffState(t, "비상술")
	catalog := npcTalkCastCatalog(t, "비상술")
	proposal, err := s.PlanNPCTalkProposalWithEffectOptions("a", "Guide", 1, "quest", catalog, NPCTalkEffectOptions{
		Now:  1000,
		Roll: func(int, int) int { return 100 },
	})
	if err != nil {
		t.Fatal(err)
	}
	next, result, err := s.ApplyNPCTalk(proposal, catalog)
	if err != nil {
		t.Fatal(err)
	}
	if !result.SpellAttempted || result.SpellSucceeded || !result.SpellFailed || next.NPCs["guide-one"].Body.MPCurrent != 25 {
		t.Fatalf("result=%+v npc=%+v", result, next.NPCs["guide-one"].Body)
	}
	actor := next.Players["a"]
	if flag(actor.Body.Flags[:], npcTalkFlyFlag) || actor.Body.Timers[npcTalkFlyTimer] != (LegacyTimer{}) {
		t.Fatalf("failed buff changed target=%+v", actor.Body)
	}
}
