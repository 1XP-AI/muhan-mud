package world

import (
	"strings"
	"testing"
)

func npcTalkHealState(t *testing.T) State {
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
	npc.Body.MPMax, npc.Body.MPCurrent = 30, 30
	npc.Body.Flags[npcTalkFlag/8] |= 1 << (npcTalkFlag % 8)
	npc.Body.Spells[npcTalkHealSpell/8] |= 1 << (npcTalkHealSpell % 8)
	s.NPCs["guide-one"] = npc
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestNPCTalkCastHealRestoresTargetHPAndChargesMana(t *testing.T) {
	s := npcTalkHealState(t)
	catalog := npcTalkCastCatalog(t, "완치")
	proposal, err := s.PlanNPCTalkProposalWithEffectOptions("a", "Guide", 1, "quest", catalog, NPCTalkEffectOptions{
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
	if !proposal.CastAttempted || !proposal.CastSucceeded || proposal.CastFailed || proposal.CastInterval != 0 || proposal.CastNow != 0 {
		t.Fatalf("proposal=%+v", proposal)
	}
	next, result, err := s.ApplyNPCTalk(proposal, catalog)
	if err != nil {
		t.Fatal(err)
	}
	if next.Players["a"].Body.HPCurrent != 100 || next.NPCs["guide-one"].Body.MPCurrent != 10 {
		t.Fatalf("actor=%+v npc=%+v", next.Players["a"].Body, next.NPCs["guide-one"].Body)
	}
	if result.SpellName != "완치" || !result.SpellSucceeded || result.Event == nil || !strings.Contains(result.Response, "완치") || !strings.Contains(result.Event.RoomText, "완치") {
		t.Fatalf("result=%+v", result)
	}
}

func TestNPCTalkCastHealFailureKeepsHPAndConsumesMana(t *testing.T) {
	s := npcTalkHealState(t)
	catalog := npcTalkCastCatalog(t, "완치")
	proposal, err := s.PlanNPCTalkProposalWithEffectOptions("a", "Guide", 1, "quest", catalog, NPCTalkEffectOptions{Roll: func(int, int) int { return 100 }})
	if err != nil {
		t.Fatal(err)
	}
	next, result, err := s.ApplyNPCTalk(proposal, catalog)
	if err != nil {
		t.Fatal(err)
	}
	if !result.SpellAttempted || result.SpellSucceeded || !result.SpellFailed || next.Players["a"].Body.HPCurrent != 12 || next.NPCs["guide-one"].Body.MPCurrent != 10 {
		t.Fatalf("result=%+v actor=%+v npc=%+v", result, next.Players["a"].Body, next.NPCs["guide-one"].Body)
	}
}

func TestNPCTalkCastHealHonorsClericPaladinGate(t *testing.T) {
	s := npcTalkHealState(t)
	npc := s.NPCs["guide-one"]
	npc.Body.Class = 4
	s.NPCs["guide-one"] = npc
	catalog := npcTalkCastCatalog(t, "완치")
	proposal, err := s.PlanNPCTalkProposalWithEffectOptions("a", "Guide", 1, "quest", catalog, NPCTalkEffectOptions{Roll: func(int, int) int { t.Fatal("class-gated cast must not roll"); return 1 }})
	if err != nil {
		t.Fatal(err)
	}
	if proposal.CastAttempted || proposal.CastSucceeded || proposal.CastFailed {
		t.Fatalf("gated proposal=%+v", proposal)
	}
	next, result, err := s.ApplyNPCTalk(proposal, catalog)
	if err != nil {
		t.Fatal(err)
	}
	if next.Players["a"].Body.HPCurrent != 12 || next.NPCs["guide-one"].Body.MPCurrent != 30 || !strings.Contains(result.Response, "주문을 걸어줄 수") {
		t.Fatalf("result=%+v actor=%+v npc=%+v", result, next.Players["a"].Body, next.NPCs["guide-one"].Body)
	}
}
