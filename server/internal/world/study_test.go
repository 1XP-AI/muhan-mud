package world

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func studyStateFixture(class, level byte) State {
	actor := LegacyMonster{
		Name:   "Alice",
		Type:   0,
		Class:  class,
		Level:  level,
		Stats:  [5]byte{10, 15, 10, 10, 10},
		RoomID: 1,
	}
	scroll := LegacyObject{Name: "두루마리", Type: studyScrollType, DiceCount: 1, MagicPower: 1}
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{1: {
			Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}},
			Items:     &ItemCollection{Items: map[string]Item{"floor": {Object: LegacyObject{Name: "돌"}}}, Inventory: []string{"floor"}},
			PlayerIDs: []string{"alice"},
		}},
		Players: map[string]PlayerState{
			"alice": {
				Body:   actor,
				Online: true,
				Items:  &ItemCollection{Items: map[string]Item{"scroll-1": {Object: scroll}}, Inventory: []string{"scroll-1"}},
			},
		},
	}
}

func studyObject(s State) Item {
	return s.Players["alice"].Items.Items["scroll-1"]
}

func TestPlanAndApplyStudyLearnsCanonicalSpellAndDeletesScroll(t *testing.T) {
	s := studyStateFixture(4, 10)
	player := s.Players["alice"]
	player.Body.Flags[studyPlayerHiddenFlag/8] |= 1 << (studyPlayerHiddenFlag % 8)
	s.Players["alice"] = player
	proposal, err := s.PlanStudy("alice", "두루마리", 1)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.ItemID != "scroll-1" || proposal.SpellIndex != 0 || proposal.SpellName != "회복" || !proposal.Learned || proposal.MoveToRoom || !proposal.ClearHidden {
		t.Fatalf("proposal=%+v", proposal)
	}
	next, result, err := s.ApplyStudy(proposal)
	if err != nil {
		t.Fatal(err)
	}
	actor := next.Players["alice"].Body
	if len(next.Players["alice"].Items.Inventory) != 0 || len(next.Players["alice"].Items.Items) != 0 {
		t.Fatalf("scroll was not deleted: %+v", next.Players["alice"].Items)
	}
	if !flag(actor.Spells[:], 0) || flag(actor.Flags[:], studyPlayerHiddenFlag) {
		t.Fatalf("actor spell/hidden flags=%v/%v", actor.Spells, actor.Flags)
	}
	if !result.Learned || result.MovedToRoom || !result.Broadcast || result.Event == nil {
		t.Fatalf("result=%+v", result)
	}
	if result.Event.Text != "\nAlice님이 두루마리의 내용을 읽고 연마합니다.\r\n" {
		t.Fatalf("event=%+v", result.Event)
	}
	if _, _, err := next.ApplyStudy(proposal); err == nil {
		t.Fatal("same study proposal replayed against committed state")
	}
}

func TestStudyAlignmentMismatchMovesScrollToRoomWithoutLearningOrClearingHidden(t *testing.T) {
	s := studyStateFixture(4, 10)
	actor := s.Players["alice"]
	actor.Body.Alignment = -200
	actor.Body.Flags[studyPlayerHiddenFlag/8] |= 1 << (studyPlayerHiddenFlag % 8)
	s.Players["alice"] = actor
	playerItems := s.Players["alice"].Items
	item := playerItems.Items["scroll-1"]
	item.Object.Flags[studyGoodOnlyFlag/8] |= 1 << (studyGoodOnlyFlag % 8)
	playerItems.Items["scroll-1"] = item
	proposal, err := s.PlanStudy("alice", "두루마리", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !proposal.MoveToRoom || proposal.Learned || proposal.ClearHidden || proposal.RoomText != "" || !strings.Contains(proposal.Response, "화염") {
		t.Fatalf("proposal=%+v", proposal)
	}
	next, result, err := s.ApplyStudy(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if result.Learned || !result.MovedToRoom || result.Broadcast || result.Event != nil {
		t.Fatalf("result=%+v", result)
	}
	if len(next.Players["alice"].Items.Inventory) != 0 || len(next.Players["alice"].Items.Items) != 0 {
		t.Fatalf("player still owns rejected scroll: %+v", next.Players["alice"].Items)
	}
	roomItems := next.Rooms[1].Items
	if roomItems == nil || roomItems.Items["scroll-1"].Object.Name != "두루마리" || !containsString(roomItems.Inventory, "scroll-1") {
		t.Fatalf("room did not receive rejected scroll: %+v", roomItems)
	}
	updated := next.Players["alice"].Body
	if flag(updated.Spells[:], 0) || !flag(updated.Flags[:], studyPlayerHiddenFlag) {
		t.Fatalf("alignment rejection changed actor: spells=%v flags=%v", updated.Spells, updated.Flags)
	}
}

func TestStudySelectsExactDirectInventoryOccurrenceAndIgnoresNestedRoot(t *testing.T) {
	s := studyStateFixture(4, 10)
	player := s.Players["alice"]
	player.Items = &ItemCollection{Items: map[string]Item{
		"scroll-1": {Object: LegacyObject{Name: "두루마리", Type: studyScrollType, DiceCount: 1, MagicPower: 1}, Contents: []string{"nested"}},
		"scroll-2": {Object: LegacyObject{Name: "두루마리", Type: studyScrollType, DiceCount: 1, MagicPower: 2}},
		"nested":   {Object: LegacyObject{Name: "두루마리", Type: studyScrollType, DiceCount: 1, MagicPower: 3}},
	}, Inventory: []string{"scroll-1", "scroll-2"}}
	s.Players["alice"] = player
	proposal, err := s.PlanStudy("alice", "두루마리", 2)
	if err != nil || proposal.ItemID != "scroll-2" || proposal.SpellIndex != 1 || proposal.SpellName != "삭풍" {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	if _, err := s.PlanStudy("alice", "두루마리", 3); err == nil {
		t.Fatal("nested object counted as a direct occurrence")
	}
	if _, err := s.PlanStudy("alice", "두루마리", 0); err == nil {
		t.Fatal("zero occurrence accepted")
	}
}

func TestStudySelectorUsesPrefixKeysAndDirectPrecedenceOverReady(t *testing.T) {
	s := studyStateFixture(4, 10)
	player := s.Players["alice"]
	player.Items = &ItemCollection{
		Items: map[string]Item{
			"direct": {Object: LegacyObject{Name: "비법서원본", Keys: [3]string{"direct-scroll", "", ""}, Type: studyScrollType, MagicPower: 1}},
			"key":    {Object: LegacyObject{Name: "전혀다른표시", Keys: [3]string{"secret-scroll", "", ""}, Type: studyScrollType, MagicPower: 3}},
			"ready":  {Object: LegacyObject{Name: "Ready Scroll", Keys: [3]string{"ready-scroll", "", ""}, Type: studyScrollType, MagicPower: 2}},
		},
		Inventory: []string{"direct", "key"},
	}
	player.Items.Ready[4] = "ready"
	s.Players["alice"] = player

	proposal, err := s.PlanStudy("alice", "비법", 1)
	if err != nil || proposal.ItemID != "direct" || proposal.Location != StudyInventoryRoot || proposal.ReadySlot != -1 {
		t.Fatalf("direct prefix selector proposal=%+v err=%v", proposal, err)
	}
	proposal, err = s.PlanStudy("alice", "SECRET", 1)
	if err != nil || proposal.ItemID != "key" || proposal.SpellIndex != 2 {
		t.Fatalf("case-insensitive key selector proposal=%+v err=%v", proposal, err)
	}
	proposal, err = s.PlanStudy("alice", "ready", 1)
	if err != nil || proposal.ItemID != "ready" || proposal.Location != StudyReadySlot || proposal.ReadySlot != 4 {
		t.Fatalf("ready prefix fallback proposal=%+v err=%v", proposal, err)
	}
}

func TestStudySelectorHidesInvisibleObjectsAndCountsVisibleOccurrences(t *testing.T) {
	s := studyStateFixture(4, 10)
	player := s.Players["alice"]
	hidden := LegacyObject{Name: "숨은스크롤", Keys: [3]string{"Scroll", "", ""}, Type: studyScrollType, MagicPower: 1}
	setObjectFlag(&hidden.Flags, objectInvisibleFlag, true)
	player.Items = &ItemCollection{
		Items: map[string]Item{
			"hidden":   {Object: hidden},
			"visible1": {Object: LegacyObject{Name: "보이는첫번째", Keys: [3]string{"Scroll", "", ""}, Type: studyScrollType, MagicPower: 2}},
			"visible2": {Object: LegacyObject{Name: "보이는두번째", Keys: [3]string{"Scroll", "", ""}, Type: studyScrollType, MagicPower: 3}},
		},
		Inventory: []string{"hidden", "visible1", "visible2"},
	}
	s.Players["alice"] = player

	proposal, err := s.PlanStudy("alice", "SCROLL", 1)
	if err != nil || proposal.ItemID != "visible1" || proposal.SpellIndex != 1 {
		t.Fatalf("invisible object counted proposal=%+v err=%v", proposal, err)
	}
	proposal, err = s.PlanStudy("alice", "scroll", 2)
	if err != nil || proposal.ItemID != "visible2" || proposal.SpellIndex != 2 {
		t.Fatalf("visible occurrence proposal=%+v err=%v", proposal, err)
	}
	if _, err := s.PlanStudy("alice", "scroll", 3); !errors.Is(err, ErrStudyMissingItem) {
		t.Fatalf("hidden object exposed as occurrence: %v", err)
	}

	player = s.Players["alice"]
	player.Body.Flags[playerDetectInvisibleFlag/8] |= 1 << (playerDetectInvisibleFlag % 8)
	s.Players["alice"] = player
	proposal, err = s.PlanStudy("alice", "scroll", 1)
	if err != nil || proposal.ItemID != "hidden" {
		t.Fatalf("PDINVI did not reveal first invisible object proposal=%+v err=%v", proposal, err)
	}
}

func TestStudyReadyFallbackUsesIndependentPositiveOccurrenceAndRemovesReadyRoot(t *testing.T) {
	s := studyStateFixture(4, 10)
	player := s.Players["alice"]
	player.Items = &ItemCollection{
		Items: map[string]Item{
			"direct": {Object: LegacyObject{Name: "직접물건", Keys: [3]string{"learn", "", ""}, Type: 5}},
			"ready1": {Object: LegacyObject{Name: "Ready One", Keys: [3]string{"learn", "", ""}, Type: studyScrollType, MagicPower: 1}},
			"ready2": {Object: LegacyObject{Name: "Ready Two", Keys: [3]string{"learn", "", ""}, Type: studyScrollType, MagicPower: 2}},
		},
		Inventory: []string{"direct"},
	}
	player.Items.Ready[3] = "ready1"
	player.Items.Ready[8] = "ready2"
	s.Players["alice"] = player

	proposal, err := s.PlanStudy("alice", "LEARN", 2)
	if err != nil || proposal.ItemID != "ready2" || proposal.Location != StudyReadySlot || proposal.ReadySlot != 8 {
		t.Fatalf("independent ready occurrence proposal=%+v err=%v", proposal, err)
	}
	next, result, err := s.ApplyStudy(proposal)
	if err != nil {
		t.Fatal(err)
	}
	got := next.Players["alice"]
	if result.Learned == false || got.Items.Ready[8] != "" || got.Items.Ready[3] != "ready1" || !containsString(got.Items.Inventory, "direct") {
		t.Fatalf("ready root removal changed wrong owner result=%+v items=%+v", result, got.Items)
	}
	if _, ok := got.Items.Items["ready2"]; ok || !flag(got.Body.Spells[:], 1) {
		t.Fatalf("ready root or spell bit not committed items=%+v spells=%v", got.Items, got.Body.Spells)
	}
	if _, _, err := next.ApplyStudy(proposal); err == nil {
		t.Fatal("ready study proposal replayed against committed state")
	}
}

func TestStudyReadyAlignmentFallbackTransfersRootAndNestedSubtreeOnce(t *testing.T) {
	s := studyStateFixture(4, 10)
	player := s.Players["alice"]
	player.Body.Alignment = -200
	root := LegacyObject{Name: "악한비법", Keys: [3]string{"bad-scroll", "", ""}, Type: studyScrollType, MagicPower: 1}
	root.Flags[studyGoodOnlyFlag/8] |= 1 << (studyGoodOnlyFlag % 8)
	player.Items = &ItemCollection{
		Items: map[string]Item{
			"keep":  {Object: LegacyObject{Name: "남은물건", Type: 5}},
			"root":  {Object: root, Contents: []string{"child"}},
			"child": {Object: LegacyObject{Name: "자식물건", Type: 5}},
		},
		Inventory: []string{"keep"},
	}
	player.Items.Ready[6] = "root"
	s.Players["alice"] = player

	proposal, err := s.PlanStudy("alice", "BAD", 1)
	if err != nil || !proposal.MoveToRoom || proposal.Location != StudyReadySlot || proposal.ReadySlot != 6 {
		t.Fatalf("ready alignment proposal=%+v err=%v", proposal, err)
	}
	next, result, err := s.ApplyStudy(proposal)
	if err != nil {
		t.Fatal(err)
	}
	got := next.Players["alice"]
	if !result.MovedToRoom || result.Learned || got.Items.Ready[6] != "" || !containsString(got.Items.Inventory, "keep") || len(got.Items.Items) != 1 {
		t.Fatalf("ready source after transfer result=%+v items=%+v", result, got.Items)
	}
	floor := next.Rooms[1].Items
	if floor == nil || !containsString(floor.Inventory, "root") || floor.Items["root"].Contents[0] != "child" || len(floor.Items) != 3 {
		t.Fatalf("ready destination after transfer=%+v", floor)
	}
	body := next.Players["alice"].Body
	if flag(body.Spells[:], 0) {
		t.Fatal("alignment rejection learned a spell")
	}
	if s.Players["alice"].Items.Ready[6] != "root" || containsString(s.Rooms[1].Items.Inventory, "root") {
		t.Fatal("ready transfer mutated input state")
	}
}

func TestStudyRemovesSelectedDirectRootAndNestedSubtreeOnce(t *testing.T) {
	s := studyStateFixture(4, 10)
	player := s.Players["alice"]
	player.Items = &ItemCollection{
		Items: map[string]Item{
			"keep":  {Object: LegacyObject{Name: "남은물건", Type: 5}},
			"root":  {Object: LegacyObject{Name: "비법서", Keys: [3]string{"spellbook", "", ""}, Type: studyScrollType, MagicPower: 1}, Contents: []string{"child"}},
			"child": {Object: LegacyObject{Name: "자식비법", Type: 5}},
		},
		Inventory: []string{"keep", "root"},
	}
	s.Players["alice"] = player

	proposal, err := s.PlanStudy("alice", "SPELL", 1)
	if err != nil || proposal.ItemID != "root" || proposal.Location != StudyInventoryRoot {
		t.Fatalf("nested direct proposal=%+v err=%v", proposal, err)
	}
	next, _, err := s.ApplyStudy(proposal)
	if err != nil {
		t.Fatal(err)
	}
	got := next.Players["alice"].Items
	if got == nil || len(got.Items) != 1 || got.Items["keep"].Object.Name != "남은물건" || len(got.Inventory) != 1 || got.Inventory[0] != "keep" {
		t.Fatalf("selected root subtree was not removed once: %+v", got)
	}
	if _, ok := got.Items["root"]; ok {
		t.Fatal("selected root remained")
	}
	if _, ok := got.Items["child"]; ok {
		t.Fatal("selected nested child remained")
	}
}

func TestStudyRejectsBlindAndAllSourceAdmissionGates(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*State)
		want   error
	}{
		{name: "blind", mutate: func(s *State) {
			p := s.Players["alice"]
			p.Body.Flags[studyPlayerBlindFlag/8] |= 1 << (studyPlayerBlindFlag % 8)
			s.Players["alice"] = p
		}, want: ErrStudyBlind},
		{name: "not-scroll", mutate: func(s *State) {
			p := s.Players["alice"]
			item := p.Items.Items["scroll-1"]
			item.Object.Type = 5
			p.Items.Items["scroll-1"] = item
			s.Players["alice"] = p
		}, want: ErrStudyNotScroll},
		{name: "level", mutate: func(s *State) {
			p := s.Players["alice"]
			p.Body.Level = 0
			p.Items.Items["scroll-1"] = studyObject(*s)
			item := p.Items.Items["scroll-1"]
			item.Object.DiceCount = 1
			p.Items.Items["scroll-1"] = item
			s.Players["alice"] = p
		}, want: ErrStudyLevel},
		{name: "class", mutate: func(s *State) {
			p := s.Players["alice"]
			item := p.Items.Items["scroll-1"]
			item.Object.Flags[studyClassSelectFlag/8] |= 1 << (studyClassSelectFlag % 8)
			p.Items.Items["scroll-1"] = item
			s.Players["alice"] = p
		}, want: ErrStudyClass},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := studyStateFixture(4, 10)
			tc.mutate(&s)
			_, err := s.PlanStudy("alice", "두루마리", 1)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v, want errors.Is(%v)", err, tc.want)
			}
		})
	}

	for _, power := range []byte{0, 57, 255} {
		s := studyStateFixture(4, 10)
		p := s.Players["alice"]
		item := p.Items.Items["scroll-1"]
		item.Object.MagicPower = power
		p.Items.Items["scroll-1"] = item
		s.Players["alice"] = p
		if _, err := s.PlanStudy("alice", "두루마리", 1); !errors.Is(err, ErrStudySpellUnavailable) {
			t.Fatalf("magicpower=%d err=%v", power, err)
		}
	}
}

func TestStudyRequiresCanonicalInventoryWithoutLegacyDuplicate(t *testing.T) {
	s := studyStateFixture(4, 10)
	p := s.Players["alice"]
	// A migrated player owns roots in Items only. Keeping the legacy nested
	// inventory alongside it is ambiguous and must fail closed at validation.
	p.Body.Inventory = []LegacyObject{{Name: "두루마리", Type: studyScrollType, MagicPower: 1}}
	s.Players["alice"] = p
	if _, err := s.PlanStudy("alice", "두루마리", 1); err == nil {
		t.Fatal("study accepted duplicate legacy and canonical inventories")
	}
}

func TestStudyClassSelectionAllowsExplicitClassBitAndCatalogFailsClosed(t *testing.T) {
	s := studyStateFixture(4, 10)
	p := s.Players["alice"]
	item := p.Items.Items["scroll-1"]
	item.Object.Flags[studyClassSelectFlag/8] |= 1 << (studyClassSelectFlag % 8)
	item.Object.Flags[(studyClassSelectFlag+int(p.Body.Class))/8] |= 1 << ((studyClassSelectFlag + int(p.Body.Class)) % 8)
	p.Items.Items["scroll-1"] = item
	s.Players["alice"] = p
	if _, err := s.PlanStudy("alice", "두루마리", 1); err != nil {
		t.Fatal(err)
	}
	upper := studyStateFixture(4, 10)
	upperPlayer := upper.Players["alice"]
	upperItem := upperPlayer.Items.Items["scroll-1"]
	upperItem.Object.MagicPower = studyMaxSpellPower
	upperPlayer.Items.Items["scroll-1"] = upperItem
	upper.Players["alice"] = upperPlayer
	upperProposal, err := upper.PlanStudy("alice", "두루마리", 1)
	if err != nil || upperProposal.SpellIndex != studyMaxSpellPower-1 || upperProposal.SpellName != "이혼대법" {
		t.Fatalf("upper spell mapping proposal=%+v err=%v", upperProposal, err)
	}
	learned, _, err := upper.ApplyStudy(upperProposal)
	learnedBody := learned.Players["alice"].Body
	if err != nil || !flag(learnedBody.Spells[:], studyMaxSpellPower-1) {
		t.Fatalf("upper spell bit not learned: err=%v state=%+v", err, learned.Players["alice"].Body.Spells)
	}

	original := legacyInfoSpellNames[0]
	legacyInfoSpellNames[0] = ""
	t.Cleanup(func() { legacyInfoSpellNames[0] = original })
	if _, _, err := studySpellName(1); !errors.Is(err, ErrStudySpellUnavailable) {
		t.Fatalf("unresolved catalog accepted: %v", err)
	}
}

func TestApplyStudyRejectsStaleItemWithoutPartialMutation(t *testing.T) {
	s := studyStateFixture(4, 10)
	proposal, err := s.PlanStudy("alice", "두루마리", 1)
	if err != nil {
		t.Fatal(err)
	}
	changed := s.clone()
	actor := changed.Players["alice"]
	item := actor.Items.Items["scroll-1"]
	item.Object.MagicPower = 2
	actor.Items.Items["scroll-1"] = item
	changed.Players["alice"] = actor
	before := changed
	if _, _, err := changed.ApplyStudy(proposal); err == nil {
		t.Fatal("stale study proposal accepted")
	}
	if !reflect.DeepEqual(changed, before) {
		t.Fatal("stale apply mutated source state")
	}
}
