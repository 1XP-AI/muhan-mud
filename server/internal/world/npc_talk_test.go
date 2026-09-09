package world

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func npcTalkFixture(t *testing.T) State {
	t.Helper()
	var aggressive [8]byte
	aggressive[npcTalkAggressiveFlag/8] |= 1 << (npcTalkAggressiveFlag % 8)
	s := State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"a"},
				NPCIDs:    []string{"guide-one", "guide-two"},
			},
			2: {
				Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 2, Name: "외곽"}},
				NPCIDs:   []string{"far-guide"},
			},
		},
		Players: map[string]PlayerState{
			"a": {Body: LegacyMonster{Name: "Alice", Type: 0, RoomID: 1, Flags: [8]byte{2}}, Online: true},
		},
		NPCs: map[string]NPCState{
			"guide-one": {Body: LegacyMonster{Name: "Guide", Type: 1, RoomID: 1, Talk: "첫 번째 안내"}, Enemies: []NPCEnemy{}},
			"guide-two": {Body: LegacyMonster{Name: "Guide", Type: 1, RoomID: 1, Talk: "두 번째 안내", Flags: aggressive}, Enemies: []NPCEnemy{}},
			"far-guide": {Body: LegacyMonster{Name: "Guide", Type: 1, RoomID: 2, Talk: "멀리 있음"}, Enemies: []NPCEnemy{}},
		},
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPlanAndApplyNPCTalkUsesExactNameOccurrenceAndAtomicEffects(t *testing.T) {
	s := npcTalkFixture(t)
	original := s
	proposal, err := s.PlanNPCTalkProposal("a", "Guide", 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if proposal.TargetID != "guide-two" || proposal.TargetName != "Guide" || !proposal.AddEnemy || !proposal.ClearHidden {
		t.Fatalf("proposal=%+v", proposal)
	}
	next, result, err := s.ApplyNPCTalk(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Broadcast || !result.EnemyAdded || result.TargetID != "guide-two" || !strings.Contains(result.Response, "두 번째 안내") || result.Event == nil {
		t.Fatalf("result=%+v", result)
	}
	nextActor := next.Players["a"]
	if flag(nextActor.Body.Flags[:], playerHiddenStateFlag) {
		t.Fatal("successful talk did not clear PHIDDN")
	}
	enemies := next.NPCs["guide-two"].Enemies
	if len(enemies) != 1 || enemies[0].Target != (EntityRef{Kind: "player", ID: "a"}) || enemies[0].Damage != 0 {
		t.Fatalf("enemy relation=%+v", enemies)
	}
	if !reflect.DeepEqual(s, original) {
		t.Fatal("planning/apply mutated input snapshot")
	}
	if result.Event.RoomID != 1 || result.Event.ActorID != "a" || result.Event.NPCID != "guide-two" || result.Event.ExcludeActorID != "a" {
		t.Fatalf("event=%+v", result.Event)
	}
	if result.Event.ActorText != result.Response || !strings.Contains(result.Event.RoomText, "Alice") || !strings.Contains(result.Event.RoomText, "두 번째 안내") {
		t.Fatalf("event projection=%+v", result.Event)
	}
	projected, ok, err := next.RoomNPCTalkEvent("a", "guide-two", "")
	if err != nil || !ok || !reflect.DeepEqual(projected, *result.Event) {
		t.Fatalf("projected=%+v ok=%v err=%v want=%+v", projected, ok, err, result.Event)
	}
}

func TestPlanNPCTalkNoTopicResponseWorksWithoutMTALKSAndTopicFailsClosed(t *testing.T) {
	s := npcTalkFixture(t)
	// The first exact occurrence has no MTALKS. A topic therefore follows the
	// source's !MTALKS no-topic branch rather than guessing a talk-file answer.
	next, result, err := s.PlanNPCTalkWithOccurrence("a", "Guide", 1, "quest")
	if err != nil || result.Event == nil || result.TargetID != "guide-one" || !strings.Contains(result.Response, "첫 번째 안내") {
		t.Fatalf("fallback result=%+v err=%v", result, err)
	}
	nextActor := next.Players["a"]
	if flag(nextActor.Body.Flags[:], playerHiddenStateFlag) {
		t.Fatal("no-topic fallback did not clear PHIDDN")
	}

	interactive := s.NPCs["guide-one"]
	interactive.Body.Flags[npcTalkFlag/8] |= 1 << (npcTalkFlag % 8)
	s.NPCs["guide-one"] = interactive
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PlanNPCTalkProposal("a", "Guide", 1, "quest"); !errors.Is(err, ErrNPCTalkTopicsUnavailable) {
		t.Fatalf("topic without canonical loader err=%v", err)
	}
	inputActor := s.Players["a"]
	if !flag(inputActor.Body.Flags[:], playerHiddenStateFlag) {
		t.Fatal("failed topic plan changed PHIDDN")
	}
}

func TestNPCTalkRejectsPrefixAndStaleProposalWithoutMutation(t *testing.T) {
	s := npcTalkFixture(t)
	if _, err := s.PlanNPCTalkProposal("a", "Gui", 1, ""); err != nil {
		t.Fatal(err)
	}
	if _, result, err := s.PlanNPCTalkWithOccurrence("a", "Gui", 1, ""); err != nil || result.TargetID != "" || result.Response != "그런 것은 여기 없습니다.\r\n" {
		t.Fatalf("prefix result=%+v err=%v", result, err)
	}
	if _, err := s.PlanNPCTalkProposal("a", "Guide", 0, ""); err == nil {
		t.Fatal("zero occurrence accepted")
	}

	proposal, err := s.PlanNPCTalkProposal("a", "Guide", 2, "")
	if err != nil {
		t.Fatal(err)
	}
	changed := s.clone()
	npc := changed.NPCs["guide-two"]
	npc.Body.Talk = "changed"
	changed.NPCs["guide-two"] = npc
	if _, _, err := changed.ApplyNPCTalk(proposal); err == nil {
		t.Fatal("stale NPC talk proposal applied")
	}
	inputActor := s.Players["a"]
	if !flag(inputActor.Body.Flags[:], playerHiddenStateFlag) || len(s.NPCs["guide-two"].Enemies) != 0 {
		t.Fatal("stale proposal mutated original state")
	}
}

func TestNPCTalkRejectsUnresolvedAggressiveEnemyRelation(t *testing.T) {
	s := npcTalkFixture(t)
	npc := s.NPCs["guide-two"]
	npc.Enemies = nil
	s.NPCs["guide-two"] = npc
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PlanNPCTalkProposal("a", "Guide", 2, ""); err == nil {
		t.Fatal("unresolved enemy relation accepted")
	}
}
