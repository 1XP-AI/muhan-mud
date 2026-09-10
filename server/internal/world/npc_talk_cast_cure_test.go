package world

import (
	"errors"
	"strings"
	"testing"
)

func npcTalkCurePoisonState(t *testing.T) State {
	t.Helper()
	s := npcTalkFixture(t)
	actor := s.Players["a"]
	// Cure is a targeted body transition and must not require an imported
	// inventory graph merely to remove PPOISN.
	actor.Body.Flags[npcTalkPoisonFlag/8] |= 1 << (npcTalkPoisonFlag % 8)
	s.Players["a"] = actor
	npc := s.NPCs["guide-one"]
	npc.Body.Level = 5
	npc.Body.Class = 3 // CLERIC: source spell_fail chance is 75% here.
	npc.Body.Stats[3] = 10
	npc.Body.MPMax, npc.Body.MPCurrent = 12, 12
	npc.Body.Flags[npcTalkFlag/8] |= 1 << (npcTalkFlag % 8)
	npc.Body.Spells[npcTalkCurePoisonSpell/8] |= 1 << (npcTalkCurePoisonSpell % 8)
	s.NPCs["guide-one"] = npc
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestNPCTalkCastCurePoisonClearsTargetAndChargesSixMana(t *testing.T) {
	s := npcTalkCurePoisonState(t)
	catalog := npcTalkCastCatalog(t, "해독")
	rolls := 0
	proposal, err := s.PlanNPCTalkProposalWithEffectOptions("a", "Guide", 1, "quest", catalog, NPCTalkEffectOptions{
		Roll: func(low, high int) int {
			rolls++
			if low != 1 || high != 100 {
				t.Fatalf("roll bounds=%d..%d", low, high)
			}
			return 1
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !proposal.CastAttempted || !proposal.CastSucceeded || proposal.CastFailed || proposal.CastRoll != 1 || proposal.CastChance != 75 || proposal.CastInterval != 0 || proposal.CastNow != 0 {
		t.Fatalf("proposal=%+v", proposal)
	}
	next, result, err := s.ApplyNPCTalk(proposal, catalog)
	if err != nil {
		t.Fatal(err)
	}
	if rolls != 1 || result.SpellName != "해독" || !result.SpellSucceeded || next.NPCs["guide-one"].Body.MPCurrent != 6 {
		t.Fatalf("result=%+v rolls=%d npc=%+v", result, rolls, next.NPCs["guide-one"].Body)
	}
	nextActor := next.Players["a"]
	if flag(nextActor.Body.Flags[:], npcTalkPoisonFlag) {
		t.Fatal("cure cast left PPOISN set")
	}
	if result.Event == nil || !strings.Contains(result.Response, "해독 주문") || !strings.Contains(result.Event.RoomText, "해독 주문") {
		t.Fatalf("event=%+v response=%q", result.Event, result.Response)
	}
	if _, _, err := next.RoomNPCTalkEvent("a", "guide-one", "quest", catalog); !errors.Is(err, ErrNPCTalkCastProjectionUnavailable) {
		t.Fatalf("post-state projection err=%v", err)
	}
}

func TestNPCTalkCastCurePoisonFailureConsumesManaAndKeepsPoison(t *testing.T) {
	s := npcTalkCurePoisonState(t)
	catalog := npcTalkCastCatalog(t, "해독")
	proposal, err := s.PlanNPCTalkProposalWithEffectOptions("a", "Guide", 1, "quest", catalog, NPCTalkEffectOptions{Roll: func(int, int) int { return 100 }})
	if err != nil {
		t.Fatal(err)
	}
	next, result, err := s.ApplyNPCTalk(proposal, catalog)
	if err != nil {
		t.Fatal(err)
	}
	if !result.SpellAttempted || result.SpellSucceeded || !result.SpellFailed || next.NPCs["guide-one"].Body.MPCurrent != 6 {
		t.Fatalf("failure result=%+v npc=%+v", result, next.NPCs["guide-one"].Body)
	}
	nextActor := next.Players["a"]
	if !flag(nextActor.Body.Flags[:], npcTalkPoisonFlag) {
		t.Fatal("failed cure unexpectedly cleared PPOISN")
	}
	if strings.Contains(result.Response, "해독 주문") || len(result.Event.ActorMessages) != 1 {
		t.Fatalf("failed cure emitted success projection=%+v", result.Event)
	}
}

func TestNPCTalkCastCurePoisonWithMissingInventoryStillAdmits(t *testing.T) {
	s := npcTalkCurePoisonState(t)
	if s.Players["a"].Items != nil {
		t.Fatal("fixture unexpectedly has inventory")
	}
	catalog := npcTalkCastCatalog(t, "해독")
	proposal, err := s.PlanNPCTalkProposalWithEffectOptions("a", "Guide", 1, "quest", catalog, NPCTalkEffectOptions{Roll: func(int, int) int { return 1 }})
	if err != nil {
		t.Fatalf("cure should not require inventory: %v", err)
	}
	if _, _, err := s.ApplyNPCTalk(proposal, catalog); err != nil {
		t.Fatal(err)
	}
}

func npcTalkCleansingState(t *testing.T, spell string, flagBit uint) State {
	t.Helper()
	s := npcTalkFixture(t)
	actor := s.Players["a"]
	actor.Body.Flags[flagBit/8] |= 1 << (flagBit % 8)
	s.Players["a"] = actor
	npc := s.NPCs["guide-one"]
	npc.Body.Level = 5
	npc.Body.Class = 3 // CLERIC satisfies both source removal-spell gates.
	npc.Body.Stats[3] = 10
	npc.Body.MPMax, npc.Body.MPCurrent = 24, 24
	npc.Body.Flags[npcTalkFlag/8] |= 1 << (npcTalkFlag % 8)
	index := npcTalkDiseaseSpell
	if spell == "개안술" {
		index = npcTalkBlindSpell
	} else if spell == "수생술" {
		index = npcTalkWaterSpell
	}
	npc.Body.Spells[index/8] |= 1 << (index % 8)
	s.NPCs["guide-one"] = npc
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestNPCTalkCastCleansingSpellsClearCanonicalTargetFlags(t *testing.T) {
	for _, tc := range []struct {
		spell string
		flag  uint
	}{
		{spell: "치료", flag: npcTalkDiseaseFlag},
		{spell: "개안술", flag: npcTalkBlindFlag},
	} {
		t.Run(tc.spell, func(t *testing.T) {
			s := npcTalkCleansingState(t, tc.spell, tc.flag)
			catalog := npcTalkCastCatalog(t, tc.spell)
			proposal, err := s.PlanNPCTalkProposalWithEffectOptions("a", "Guide", 1, "quest", catalog, NPCTalkEffectOptions{Roll: func(int, int) int { return 1 }})
			if err != nil {
				t.Fatal(err)
			}
			if proposal.CastSpellName != tc.spell || !proposal.CastAttempted || !proposal.CastSucceeded || proposal.CastInterval != 0 || proposal.CastNow != 0 {
				t.Fatalf("proposal=%+v", proposal)
			}
			next, result, err := s.ApplyNPCTalk(proposal, catalog)
			if err != nil {
				t.Fatal(err)
			}
			nextActor := next.Players["a"]
			if flag(nextActor.Body.Flags[:], tc.flag) || next.NPCs["guide-one"].Body.MPCurrent != 12 {
				t.Fatalf("result=%+v actor=%+v npc=%+v", result, nextActor.Body, next.NPCs["guide-one"].Body)
			}
			if tc.spell == "치료" && !strings.Contains(result.Response, "치료") {
				t.Fatalf("treatment response=%q", result.Response)
			}
			if tc.spell == "개안술" && !strings.Contains(result.Response, "개안부") {
				t.Fatalf("blindness response=%q", result.Response)
			}
		})
	}
}

func TestNPCTalkCastCleansingClassGateRejectsWithoutRNG(t *testing.T) {
	s := npcTalkCleansingState(t, "치료", npcTalkDiseaseFlag)
	npc := s.NPCs["guide-one"]
	npc.Body.Class = 4 // FIGHTER is below the source CLERIC gate.
	s.NPCs["guide-one"] = npc
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	catalog := npcTalkCastCatalog(t, "치료")
	calls := 0
	proposal, err := s.PlanNPCTalkProposalWithEffectOptions("a", "Guide", 1, "quest", catalog, NPCTalkEffectOptions{Roll: func(int, int) int { calls++; return 1 }})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 || proposal.CastAttempted || proposal.CastSucceeded || proposal.CastFailed || !strings.Contains(proposal.Response, "주문을 걸어줄 수 없다고") {
		t.Fatalf("class-gated proposal=%+v calls=%d", proposal, calls)
	}
	if _, _, err := s.ApplyNPCTalk(proposal, catalog); err != nil {
		t.Fatal(err)
	}
}

func TestNPCTalkCastWaterBreathingUsesTimedBodyEffectWithoutInventory(t *testing.T) {
	s := npcTalkCleansingState(t, "수생술", npcTalkWaterFlag)
	actor := s.Players["a"]
	actor.Body.Flags[npcTalkWaterFlag/8] &^= 1 << (npcTalkWaterFlag % 8)
	s.Players["a"] = actor
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	catalog := npcTalkCastCatalog(t, "수생술")
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
	if proposal.CastSpellName != "수생술" || proposal.CastInterval != 1200 || !proposal.CastAttempted || !proposal.CastSucceeded {
		t.Fatalf("proposal=%+v", proposal)
	}
	next, result, err := s.ApplyNPCTalk(proposal, catalog)
	if err != nil {
		t.Fatal(err)
	}
	nextActor := next.Players["a"]
	if !flag(nextActor.Body.Flags[:], npcTalkWaterFlag) || nextActor.Body.Timers[30] != (LegacyTimer{LastTime: 1000, Interval: 1200}) || next.NPCs["guide-one"].Body.MPCurrent != 12 {
		t.Fatalf("water effect actor=%+v npc=%+v", nextActor.Body, next.NPCs["guide-one"].Body)
	}
	if result.Event == nil || !strings.Contains(result.Response, "수생부") || !strings.Contains(result.Event.RoomText, "수생부") {
		t.Fatalf("water event=%+v response=%q", result.Event, result.Response)
	}
}
