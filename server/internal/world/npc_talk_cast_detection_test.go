package world

import (
	"strings"
	"testing"
)

func npcTalkDetectionState(t *testing.T, spell string) (State, npcTalkCastSpec) {
	t.Helper()
	s := npcTalkFixture(t)
	spec, err := npcTalkCastSpecFor(spell)
	if err != nil {
		t.Fatal(err)
	}
	npc := s.NPCs["guide-one"]
	npc.Body.Class = npcTalkMageClass
	npc.Body.Level = 5
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

func TestNPCTalkCastDetectionSpellsUseCanonicalFlagsAndSourceIntervals(t *testing.T) {
	tests := []struct {
		spell     string
		flag      uint
		timer     int
		cost      int16
		interval  int32
		roomBonus string
	}{
		{spell: "은둔감지술", flag: npcTalkDetectInvisibleFlag, timer: npcTalkDetectInvisibleTimer, cost: 10, interval: 1920, roomBonus: "푸른광안"},
		{spell: "주문감지술", flag: npcTalkDetectMagicFlag, timer: npcTalkDetectMagicTimer, cost: 10, interval: 1920, roomBonus: "은빛광안"},
		{spell: "선악감지", flag: npcTalkKnowAlignmentFlag, timer: npcTalkKnowAlignmentTimer, cost: 6, interval: 2000, roomBonus: "선악"},
	}
	for _, tc := range tests {
		t.Run(tc.spell, func(t *testing.T) {
			s, _ := npcTalkDetectionState(t, tc.spell)
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
			if proposal.CastInterval != tc.interval || !proposal.CastAttempted || !proposal.CastSucceeded || proposal.CastSpellName != tc.spell {
				t.Fatalf("proposal=%+v want interval=%d", proposal, tc.interval)
			}
			next, result, err := s.ApplyNPCTalk(proposal, catalog)
			if err != nil {
				t.Fatal(err)
			}
			actor := next.Players["a"]
			if !flag(actor.Body.Flags[:], tc.flag) || actor.Body.Timers[tc.timer] != (LegacyTimer{LastTime: 1000, Interval: tc.interval}) {
				t.Fatalf("actor effect=%+v", actor.Body)
			}
			if next.NPCs["guide-one"].Body.MPCurrent != 40-tc.cost || result.SpellInterval != tc.interval || result.Event == nil || !strings.Contains(result.Response, tc.roomBonus) || !strings.Contains(result.Event.RoomText, tc.roomBonus) {
				t.Fatalf("result=%+v actor=%+v npc=%+v", result, actor.Body, next.NPCs["guide-one"].Body)
			}
		})
	}
}

func TestNPCTalkCastDetectionFailureOnlyConsumesNPCMana(t *testing.T) {
	s, _ := npcTalkDetectionState(t, "은둔감지술")
	catalog := npcTalkCastCatalog(t, "은둔감지술")
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
	if !result.SpellAttempted || result.SpellSucceeded || !result.SpellFailed || next.NPCs["guide-one"].Body.MPCurrent != 30 {
		t.Fatalf("result=%+v npc=%+v", result, next.NPCs["guide-one"].Body)
	}
	actor := next.Players["a"]
	if flag(actor.Body.Flags[:], npcTalkDetectInvisibleFlag) || actor.Body.Timers[npcTalkDetectInvisibleTimer] != (LegacyTimer{}) {
		t.Fatalf("failed detection changed target=%+v", actor.Body)
	}
}
