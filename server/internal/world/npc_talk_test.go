package world

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
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

func npcTalkTopicCatalog(t *testing.T, keyLine, response string) TalkCatalog {
	t.Helper()
	catalog, err := LoadTalkCatalog(fstest.MapFS{
		"Guide-7": &fstest.MapFile{Data: []byte(keyLine + "\n" + response + "\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func npcTalkCatalogFile(t *testing.T, path, body string) TalkCatalog {
	t.Helper()
	catalog, err := LoadTalkCatalog(fstest.MapFS{
		path: &fstest.MapFile{Data: []byte(body)},
	})
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func TestPlanAndApplyNPCTalkWithCatalogUsesFirstExactTopicResponse(t *testing.T) {
	s := npcTalkFixture(t)
	npc := s.NPCs["guide-one"]
	npc.Body.Level = 7
	npc.Body.Flags[npcTalkFlag/8] |= 1 << (npcTalkFlag % 8)
	s.NPCs["guide-one"] = npc
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	catalog := npcTalkCatalogFile(t, "Guide-7", "quest\n첫 번째 주제 응답\nquest\n두 번째 주제 응답\n")

	proposal, err := s.PlanNPCTalkProposal("a", "Guide", 1, "quest", catalog)
	if err != nil {
		t.Fatal(err)
	}
	if !proposal.TopicFound || proposal.TopicEntry.Key != "quest" || proposal.TopicEntry.Response != "첫 번째 주제 응답" {
		t.Fatalf("proposal=%+v", proposal)
	}
	next, result, err := s.ApplyNPCTalk(proposal, catalog)
	if err != nil {
		t.Fatal(err)
	}
	if result.TargetID != "guide-one" || result.Topic != "quest" || !strings.Contains(result.Response, "첫 번째 주제 응답") || result.Event == nil {
		t.Fatalf("result=%+v", result)
	}
	if !strings.Contains(result.Event.RoomText, "quest") || !strings.Contains(result.Event.RoomText, "첫 번째 주제 응답") {
		t.Fatalf("topic room event=%+v", result.Event)
	}
	nextActor := next.Players["a"]
	if result.Event.ActorText != result.Response || flag(nextActor.Body.Flags[:], playerHiddenStateFlag) {
		t.Fatalf("topic actor projection=%+v next actor=%+v", result.Event, next.Players["a"])
	}
	projected, ok, err := next.RoomNPCTalkEvent("a", "guide-one", "quest", catalog)
	if err != nil || !ok || !reflect.DeepEqual(projected, *result.Event) {
		t.Fatalf("projected=%+v ok=%v err=%v want=%+v", projected, ok, err, result.Event)
	}
}

func TestNPCTalkTopicActionsFailClosedWithoutStateMutation(t *testing.T) {
	for _, tc := range []struct {
		name string
		line string
	}{
		{name: "action", line: "quest ACTION smile PLAYER"},
		{name: "cast", line: "quest CAST cure PLAYER"},
		{name: "give", line: "quest GIVE 107"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := npcTalkFixture(t)
			npc := s.NPCs["guide-one"]
			npc.Body.Level = 7
			npc.Body.Flags[npcTalkFlag/8] |= 1 << (npcTalkFlag % 8)
			s.NPCs["guide-one"] = npc
			before := s
			catalog := npcTalkTopicCatalog(t, tc.line, "응답")
			if _, err := s.PlanNPCTalkProposal("a", "Guide", 1, "quest", catalog); !errors.Is(err, ErrNPCTalkActionUnavailable) {
				t.Fatalf("action=%q err=%v", tc.name, err)
			}
			if !reflect.DeepEqual(s, before) {
				t.Fatal("unsupported topic action mutated state")
			}
		})
	}
}

func TestNPCTalkTopicAttackAddsEnemyAndProjectsAction(t *testing.T) {
	s := npcTalkFixture(t)
	npc := s.NPCs["guide-one"]
	npc.Body.Level = 7
	npc.Body.Flags[npcTalkFlag/8] |= 1 << (npcTalkFlag % 8)
	s.NPCs["guide-one"] = npc
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	catalog := npcTalkTopicCatalog(t, "quest ATTACK", "응답")
	proposal, err := s.PlanNPCTalkProposal("a", "Guide", 1, "quest", catalog)
	if err != nil || !proposal.TopicFound || proposal.TopicEntry.Action.Kind != TalkActionAttack || !proposal.AddEnemy {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	next, result, err := s.ApplyNPCTalk(proposal, catalog)
	if err != nil {
		t.Fatal(err)
	}
	wantResponse := "\nGuide가 당신에게 \"응답\"라고 이야기합니다.\r\n\nGuide가 당신을 공격합니다.\n"
	if !result.Broadcast || !result.EnemyAdded || result.Response != wantResponse || result.Event == nil {
		t.Fatalf("result=%+v", result)
	}
	wantRoom := []string{
		"\nAlice님이 Guide에게 \"quest\"에 관해 물어봅니다.\r\n",
		"\nGuide가 Alice님에게 \"응답\"라고 이야기합니다.\r\n",
		"\nGuide가 Alice님을 공격합니다.\n",
	}
	if len(result.Event.RoomMessages) != len(wantRoom) || len(result.Event.ActorMessages) != 2 {
		t.Fatalf("event=%+v", result.Event)
	}
	for i, want := range wantRoom {
		if result.Event.RoomMessages[i].Text != want || result.Event.RoomMessages[i].ExcludeActorID != "a" {
			t.Fatalf("room message[%d]=%+v want=%q", i, result.Event.RoomMessages[i], want)
		}
	}
	if result.Event.ActorMessages[0] != "\nGuide가 당신에게 \"응답\"라고 이야기합니다.\r\n" || result.Event.ActorMessages[1] != "\nGuide가 당신을 공격합니다.\n" || result.Event.ActorText != wantResponse || result.Event.RoomText != strings.Join(wantRoom, "") {
		t.Fatalf("event projection=%+v", result.Event)
	}
	enemies := next.NPCs["guide-one"].Enemies
	if len(enemies) != 1 || enemies[0].Target != (EntityRef{Kind: "player", ID: "a"}) {
		t.Fatalf("enemy relation=%+v", enemies)
	}
	projected, ok, err := next.RoomNPCTalkEvent("a", "guide-one", "quest", catalog)
	if err != nil || !ok || !reflect.DeepEqual(projected, *result.Event) {
		t.Fatalf("projected=%+v ok=%v err=%v want=%+v", projected, ok, err, result.Event)
	}
}

func npcTalkCastState(t *testing.T, spell string) State {
	t.Helper()
	s := npcTalkFixture(t)
	actor := s.Players["a"]
	actor.Body.Level = 1
	actor.Body.Class = 4 // FIGHTER: valid thaco table, no spell gate needed
	actor.Body.Stats = [5]byte{10, 10, 10, 10, 10}
	actor.Items = &ItemCollection{Items: map[string]Item{}}
	stats, err := actor.Items.CombatStats(actor.Body)
	if err != nil {
		t.Fatal(err)
	}
	actor.Body.Armor, actor.Body.Thaco = byte(stats.Armor), byte(stats.Thaco)
	actor.Body.Flags = [8]byte{}
	s.Players["a"] = actor

	npc := s.NPCs["guide-one"]
	npc.Body.Level = 5
	npc.Body.Class = 3 // CLERIC: interval includes the source level-band term
	npc.Body.Stats[3] = 10
	npc.Body.MPMax, npc.Body.MPCurrent = 30, 30
	npc.Body.Flags[npcTalkFlag/8] |= 1 << (npcTalkFlag % 8)
	index := npcTalkBlessSpell
	if spell == "수호진" {
		index = npcTalkProtectionSpell
	}
	npc.Body.Spells[index/8] |= 1 << (index % 8)
	s.NPCs["guide-one"] = npc
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	return s
}

func npcTalkCastCatalog(t *testing.T, spell string) TalkCatalog {
	t.Helper()
	catalog, err := LoadTalkCatalog(fstest.MapFS{
		"Guide-5": &fstest.MapFile{Data: []byte("quest CAST " + spell + " PLAYER\n응답\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func TestNPCTalkCastBlessAndProtectionPersistDeterministicEffects(t *testing.T) {
	for _, tc := range []struct {
		name      string
		spell     string
		flag      uint
		wantArmor int8
	}{
		{name: "bless", spell: "성현진", flag: npcTalkBlessFlag, wantArmor: 100},
		{name: "protection", spell: "수호진", flag: npcTalkProtectionFlag, wantArmor: 90},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := npcTalkCastState(t, tc.spell)
			catalog := npcTalkCastCatalog(t, tc.spell)
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
			if !proposal.CastAttempted || !proposal.CastSucceeded || proposal.CastFailed || proposal.CastRoll != 1 || proposal.CastChance != 75 || proposal.CastInterval != 1320 {
				t.Fatalf("cast proposal=%+v", proposal)
			}
			next, result, err := s.ApplyNPCTalk(proposal, catalog)
			if err != nil {
				t.Fatal(err)
			}
			if result.Action == nil || result.Action.Kind != TalkActionCast || result.SpellName != tc.spell || result.SpellTargetID != "a" || !result.SpellAttempted || !result.SpellSucceeded || result.SpellFailed || result.SpellInterval != 1320 {
				t.Fatalf("cast result=%+v", result)
			}
			if result.Event == nil || !strings.Contains(result.Event.RoomText, tc.spell) || (tc.spell == "성현진" && !strings.Contains(result.Response, tc.spell)) || (tc.spell == "수호진" && !strings.Contains(result.Response, "수호인")) {
				t.Fatalf("cast event=%+v response=%q", result.Event, result.Response)
			}
			actor := next.Players["a"]
			if !flag(actor.Body.Flags[:], tc.flag) || actor.Body.Timers[map[uint]int{npcTalkBlessFlag: npcTalkBlessTimer, npcTalkProtectionFlag: npcTalkProtectionTimer}[tc.flag]] != (LegacyTimer{LastTime: 1000, Interval: 1320}) {
				t.Fatalf("target effect body=%+v", actor.Body)
			}
			if int8(actor.Body.Armor) != tc.wantArmor || next.NPCs["guide-one"].Body.MPCurrent != 20 {
				t.Fatalf("target/caster state actor=%+v npc=%+v", actor.Body, next.NPCs["guide-one"].Body)
			}
			if _, _, err := next.RoomNPCTalkEvent("a", "guide-one", "quest", catalog); !errors.Is(err, ErrNPCTalkCastProjectionUnavailable) {
				t.Fatalf("cast post-state projection err=%v", err)
			}
		})
	}
}

func TestNPCTalkCastFailureConsumesNPCPowerWithoutTargetMutation(t *testing.T) {
	s := npcTalkCastState(t, "성현진")
	catalog := npcTalkCastCatalog(t, "성현진")
	proposal, err := s.PlanNPCTalkProposalWithEffectOptions("a", "Guide", 1, "quest", catalog, NPCTalkEffectOptions{Now: 1000, Roll: func(int, int) int { return 100 }})
	if err != nil {
		t.Fatal(err)
	}
	next, result, err := s.ApplyNPCTalk(proposal, catalog)
	if err != nil {
		t.Fatal(err)
	}
	if !result.SpellAttempted || result.SpellSucceeded || !result.SpellFailed || result.SpellRoll != 100 || result.SpellChance != 75 || next.NPCs["guide-one"].Body.MPCurrent != 20 {
		t.Fatalf("failure result=%+v npc=%+v", result, next.NPCs["guide-one"].Body)
	}
	actor := next.Players["a"]
	if flag(actor.Body.Flags[:], npcTalkBlessFlag) || actor.Body.Timers[npcTalkBlessTimer] != (LegacyTimer{}) {
		t.Fatalf("failed cast changed target=%+v", actor.Body)
	}
}

func TestNPCTalkCastRefusalKeepsStateAndExplainsToActor(t *testing.T) {
	s := npcTalkCastState(t, "성현진")
	npc := s.NPCs["guide-one"]
	npc.Enemies = []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "a"}}}
	s.NPCs["guide-one"] = npc
	catalog := npcTalkCastCatalog(t, "성현진")
	proposal, err := s.PlanNPCTalkProposalWithEffectOptions("a", "Guide", 1, "quest", catalog, NPCTalkEffectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	next, result, err := s.ApplyNPCTalk(proposal, catalog)
	if err != nil {
		t.Fatal(err)
	}
	actor := next.Players["a"]
	if !proposal.CastRefused || result.SpellAttempted || result.SpellSucceeded || result.SpellFailed || next.NPCs["guide-one"].Body.MPCurrent != 30 || flag(actor.Body.Flags[:], npcTalkBlessFlag) {
		t.Fatalf("refusal proposal/result/state=%+v/%+v/%+v", proposal, result, next)
	}
	if !strings.Contains(result.Response, "거부했습니다") || result.Event == nil || strings.Contains(result.Event.RoomText, "거부했습니다") {
		t.Fatalf("refusal projection=%+v", result.Event)
	}
}

func TestNPCTalkTopicCatalogMissFailsClosedWithoutChangingNoTopicContract(t *testing.T) {
	s := npcTalkFixture(t)
	npc := s.NPCs["guide-one"]
	npc.Body.Level = 7
	npc.Body.Flags[npcTalkFlag/8] |= 1 << (npcTalkFlag % 8)
	s.NPCs["guide-one"] = npc
	before := s
	catalog := npcTalkTopicCatalog(t, "other", "응답")
	proposal, err := s.PlanNPCTalkProposal("a", "Guide", 1, "quest", catalog)
	if err != nil || proposal.TopicFound || !proposal.TopicCatalogFound {
		t.Fatalf("missing exact topic proposal=%+v err=%v", proposal, err)
	}
	if !reflect.DeepEqual(s, before) {
		t.Fatal("missing topic mutated state")
	}
	_, result, err := s.ApplyNPCTalk(proposal, catalog)
	if err != nil || result.Event == nil || !strings.Contains(result.Response, "어깨를 으쓱") {
		t.Fatalf("missing exact topic result=%+v err=%v", result, err)
	}
	missingFile := npcTalkCatalogFile(t, "Other-7", "quest\n응답\n")
	if _, err := s.PlanNPCTalkProposal("a", "Guide", 1, "quest", missingFile); !errors.Is(err, ErrNPCTalkTopicsUnavailable) {
		t.Fatalf("missing topic file err=%v", err)
	}

	// A non-MTALKS creature keeps the old no-topic path even when a catalog is
	// available and the caller supplied a topic token.
	npc = s.NPCs["guide-one"]
	npc.Body.Flags[npcTalkFlag/8] &^= 1 << (npcTalkFlag % 8)
	s.NPCs["guide-one"] = npc
	_, result, err = s.PlanNPCTalkWithOccurrence("a", "Guide", 1, "quest", catalog)
	if err != nil || result.Topic != "quest" || !strings.Contains(result.Response, "첫 번째 안내") {
		t.Fatalf("no-topic fallback result=%+v err=%v", result, err)
	}
}
