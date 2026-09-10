package world

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

type npcTalkGiveCatalog struct {
	objects map[int16]LegacyObject
}

func (c npcTalkGiveCatalog) Monster(int16) (LegacyMonster, error) {
	return LegacyMonster{}, errors.New("monster lookup not used by NPC talk GIVE")
}

func (c npcTalkGiveCatalog) Object(id int16) (LegacyObject, error) {
	object, ok := c.objects[id]
	if !ok {
		return LegacyObject{}, errors.New("object not found")
	}
	return cloneObjects([]LegacyObject{object})[0], nil
}

func npcTalkGiveState(t *testing.T, object LegacyObject) (State, TalkCatalog, npcTalkGiveCatalog) {
	t.Helper()
	s := npcTalkFixture(t)
	actor := s.Players["a"]
	actor.Body.Stats = [5]byte{10, 10, 10, 10, 10}
	actor.Items = &ItemCollection{Items: map[string]Item{}}
	s.Players["a"] = actor
	npc := s.NPCs["guide-one"]
	npc.Body.Level = 7
	npc.Body.Flags[npcTalkFlag/8] |= 1 << (npcTalkFlag % 8)
	s.NPCs["guide-one"] = npc
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	talk := npcTalkTopicCatalog(t, "quest GIVE 107", "응답")
	objects := npcTalkGiveCatalog{objects: map[int16]LegacyObject{107: object}}
	return s, talk, objects
}

func TestNPCTalkGiveTransfersCatalogObjectAndBindsReceipt(t *testing.T) {
	s, talk, objects := npcTalkGiveState(t, LegacyObject{Name: "선물", Weight: 1})
	before := s
	allocations := 0
	proposal, err := s.PlanNPCTalkProposalWithEffectOptions("a", "Guide", 1, "quest", talk, NPCTalkEffectOptions{
		ObjectCatalog: objects,
		Allocate: func() (string, error) {
			allocations++
			return "gift-1", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !proposal.GiveAttempted || !proposal.GiveGranted || proposal.GiveRejected || proposal.GiveObjectID != 107 || proposal.GiveItemID != "gift-1" || proposal.GiveItemName != "선물" || allocations != 1 {
		t.Fatalf("proposal=%+v allocations=%d", proposal, allocations)
	}
	if !reflect.DeepEqual(s, before) {
		t.Fatal("planning mutated source state")
	}
	next, result, err := s.ApplyNPCTalk(proposal, talk)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action == nil || result.Action.Kind != TalkActionGive || !result.GiveGranted || result.GiveItemID != "gift-1" || !strings.Contains(result.Response, "선물을 줍니다") {
		t.Fatalf("result=%+v", result)
	}
	nextActor := next.Players["a"]
	item, ok := nextActor.Items.Items["gift-1"]
	if !ok || item.Object.Name != "선물" || flag(nextActor.Body.Flags[:], playerHiddenStateFlag) {
		t.Fatalf("next player=%+v item=%+v", next.Players["a"], item)
	}
	if len(result.Event.RoomMessages) != 3 || !strings.Contains(result.Event.RoomMessages[2].Text, "Guide가 Alice에게 선물을 줍니다.") || !strings.Contains(result.Event.ActorText, "Guide가 당신에게 선물을 줍니다") {
		t.Fatalf("event=%+v", result.Event)
	}
	if _, _, err := next.RoomNPCTalkEvent("a", "guide-one", "quest", talk); !errors.Is(err, ErrNPCTalkGiveProjectionUnavailable) {
		t.Fatalf("post-state projection err=%v", err)
	}
	// Apply uses the recorded object and allocated identity; it never calls the
	// catalog or allocator again.
	if allocations != 1 {
		t.Fatalf("allocator called during apply: %d", allocations)
	}
}

func TestNPCTalkGiveEnchantAndQuestAreDeterministic(t *testing.T) {
	object := LegacyObject{Name: "검", Weight: 1, Quest: 1, Flags: [8]byte{2: 1 << (npcTalkGiveEnchantFlag % 8)}}
	s, talk, objects := npcTalkGiveState(t, object)
	rollCalls := 0
	proposal, err := s.PlanNPCTalkProposalWithEffectOptions("a", "Guide", 1, "quest", talk, NPCTalkEffectOptions{
		ObjectCatalog: objects,
		Roll: func(low, high int) int {
			rollCalls++
			if low != 1 || high != 100 {
				t.Fatalf("roll bounds=%d..%d", low, high)
			}
			return 99
		},
		Allocate: func() (string, error) { return "quest-gift", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !proposal.GiveEnchanted || proposal.GiveRoll != 99 || proposal.GiveQuest != 1 || proposal.GiveQuestXP != 120 || rollCalls != 1 {
		t.Fatalf("proposal=%+v rollCalls=%d", proposal, rollCalls)
	}
	next, result, err := s.ApplyNPCTalk(proposal, talk)
	if err != nil {
		t.Fatal(err)
	}
	item := next.Players["a"].Items.Items["quest-gift"].Object
	if item.Adjustment != 3 || item.DicePlus != 3 || item.Flags[1]&64 == 0 || result.GiveRoll != 99 || result.GiveQuestXP != 120 {
		t.Fatalf("item=%+v result=%+v", item, result)
	}
	actor := next.Players["a"]
	if !flag(actor.Body.Quests[:], 0) || actor.Body.Experience != 120 || actor.Body.Proficiency[0] != 13 || !strings.Contains(result.Response, "임무 달성") || !strings.Contains(result.Response, "경험치 120") {
		t.Fatalf("quest actor=%+v response=%q", actor.Body, result.Response)
	}
}

func TestNPCTalkGiveCapacityAndQuestDuplicateRejectAfterTopic(t *testing.T) {
	for _, tc := range []struct {
		name   string
		object LegacyObject
		setup  func(*PlayerState)
		needle string
	}{
		{name: "capacity", object: LegacyObject{Name: "무거운 선물", Weight: 21}, setup: func(actor *PlayerState) { actor.Body.Stats[0] = 0 }, needle: "더이상 가질 수 없습니다"},
		{name: "quest duplicate", object: LegacyObject{Name: "중복 선물", Weight: 1, Quest: 1}, setup: func(actor *PlayerState) { actor.Body.Quests[0] = 1 }, needle: "이미 이 임무를 달성했습니다"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, talk, objects := npcTalkGiveState(t, tc.object)
			if tc.setup != nil {
				actor := s.Players["a"]
				tc.setup(&actor)
				s.Players["a"] = actor
			}
			allocations := 0
			proposal, err := s.PlanNPCTalkProposalWithEffectOptions("a", "Guide", 1, "quest", talk, NPCTalkEffectOptions{
				ObjectCatalog: objects,
				Allocate: func() (string, error) {
					allocations++
					return "unused", nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if !proposal.GiveAttempted || !proposal.GiveRejected || proposal.GiveGranted || allocations != 0 || !strings.Contains(proposal.GiveRejectText, tc.needle) {
				t.Fatalf("proposal=%+v allocations=%d", proposal, allocations)
			}
			next, result, err := s.ApplyNPCTalk(proposal, talk)
			if err != nil {
				t.Fatal(err)
			}
			if !result.GiveRejected || result.GiveGranted || !strings.Contains(result.Response, tc.needle) || len(next.Players["a"].Items.Items) != 0 {
				t.Fatalf("result=%+v next=%+v", result, next.Players["a"])
			}
		})
	}
}
