package world

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func familyNewsFlag(body *LegacyMonster, bit uint, enabled bool) {
	if enabled {
		body.Flags[bit/8] |= 1 << (bit % 8)
		return
	}
	body.Flags[bit/8] &^= 1 << (bit % 8)
}

func familyNewsFixture(t *testing.T) (State, FamilyCatalog) {
	t.Helper()
	room := RoomState{Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}}, PlayerIDs: []string{"member", "boss"}}
	member := LegacyMonster{Name: "Alice", Type: 0, Class: 4, RoomID: 1}
	member.Daily[FamilyDailySlot].Max = 2
	familyNewsFlag(&member, FamilyMemberFlag, true)
	boss := LegacyMonster{Name: "Boss", Type: 0, Class: 4, RoomID: 1}
	boss.Daily[FamilyDailySlot].Max = 2
	familyNewsFlag(&boss, FamilyMemberFlag, true)
	familyNewsFlag(&boss, FamilyBossFlag, true)
	s := State{
		Version: 1,
		Rooms:   map[int16]RoomState{1: room},
		Players: map[string]PlayerState{
			"member": {Body: member, Online: true},
			"boss":   {Body: boss, Online: true},
		},
		FamilyNews: &FamilyNewsState{Bodies: map[int16]string{}},
	}
	catalog := FamilyCatalog{Families: map[int16]FamilyDefinition{
		2: {ID: 2, Name: "청룡", Boss: "Boss"},
	}}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	return s, catalog
}

func TestFamilyNewsViewRequiresMembershipAndDoesNotMutate(t *testing.T) {
	s, catalog := familyNewsFixture(t)
	outsider := s.Players["member"]
	familyNewsFlag(&outsider.Body, FamilyMemberFlag, false)
	outsider.Body.Daily[FamilyDailySlot].Max = 0
	s.Players["member"] = outsider
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	original := s.clone()

	result, err := s.PlanFamilyNewsView("member", catalog)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != FamilyNewsView || result.Changed || result.Response != FamilyNewsNotMemberResponse || result.Body != "" {
		t.Fatalf("result=%+v", result)
	}
	if !reflect.DeepEqual(s, original) {
		t.Fatal("view planning mutated state")
	}
}

func TestFamilyNewsViewFailClosedWithoutLedgerOrCatalog(t *testing.T) {
	s, catalog := familyNewsFixture(t)
	s.FamilyNews = nil
	if _, err := s.PlanFamilyNewsView("member", catalog); !errors.Is(err, ErrFamilyNewsUnresolved) {
		t.Fatalf("unmigrated err=%v", err)
	}
	s, catalog = familyNewsFixture(t)
	if _, err := s.PlanFamilyNewsView("member", FamilyCatalog{}); !errors.Is(err, ErrFamilyCatalogUnavailable) {
		t.Fatalf("catalog err=%v", err)
	}
}

func TestFamilyNewsViewReturnsMissingAndExistingBody(t *testing.T) {
	s, catalog := familyNewsFixture(t)
	result, err := s.PlanFamilyNewsView("member", catalog)
	if err != nil {
		t.Fatal(err)
	}
	if result.Response != FamilyNewsMissingResponse || result.Changed || result.FamilyID != 2 || result.FamilyName != "청룡" {
		t.Fatalf("missing result=%+v", result)
	}

	s.FamilyNews.Bodies[2] = FamilyNewsHeader + "hello\n"
	result, err = s.PlanFamilyNewsView("member", catalog)
	if err != nil {
		t.Fatal(err)
	}
	if result.Body != FamilyNewsHeader+"hello\n" || result.Response != FamilyNewsHeader+"hello\n" || result.Changed {
		t.Fatalf("existing result=%+v", result)
	}
}

func TestFamilyNewsAppendCreatesHeaderThenLinesAndRejectsReplay(t *testing.T) {
	s, catalog := familyNewsFixture(t)
	original := s.clone()

	proposal, err := s.PlanFamilyNewsAppend("member", "첫 줄", catalog)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Action != FamilyNewsAppend || !proposal.Changed || proposal.Response != FamilyNewsAppendContinuePrompt {
		t.Fatalf("proposal=%+v", proposal)
	}
	wantBody := FamilyNewsHeader + "첫 줄\n"
	if proposal.AfterBody != wantBody || proposal.BeforePresent || !proposal.AfterPresent {
		t.Fatalf("body proposal=%+v", proposal)
	}
	if !reflect.DeepEqual(s, original) {
		t.Fatal("append planning mutated state")
	}

	next, result, err := s.ApplyFamilyNews(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if result.Body != wantBody || result.Response != FamilyNewsAppendContinuePrompt || !result.Changed {
		t.Fatalf("result=%+v", result)
	}
	if next.FamilyNews == nil || next.FamilyNews.Bodies[2] != wantBody {
		t.Fatalf("next news=%+v", next.FamilyNews)
	}
	if !reflect.DeepEqual(s, original) {
		t.Fatal("apply mutated source")
	}
	if _, _, err := next.ApplyFamilyNews(proposal); !errors.Is(err, ErrFamilyNewsStaleProposal) {
		t.Fatalf("replay err=%v", err)
	}

	second, err := next.PlanFamilyNewsAppend("member", "둘째", catalog)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(second.AfterBody, FamilyNewsHeader) || strings.Count(second.AfterBody, "=== 패거리 공지 ===") != 1 || !strings.Contains(second.AfterBody, "둘째\n") {
		t.Fatalf("second body=%q", second.AfterBody)
	}
	next, _, err = next.ApplyFamilyNews(second)
	if err != nil {
		t.Fatal(err)
	}
	if err := next.Validate(); err != nil {
		t.Fatalf("next invalid: %v", err)
	}
}

func TestFamilyNewsAppendTruncatesTo79BytesOnRuneBoundary(t *testing.T) {
	s, catalog := familyNewsFixture(t)
	line := strings.Repeat("a", 80) + "한글"
	proposal, err := s.PlanFamilyNewsAppend("member", line, catalog)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.TrimPrefix(proposal.AfterBody, FamilyNewsHeader)
	got = strings.TrimSuffix(got, "\n")
	if len(got) > MaxFamilyNewsLineBytes || !utf8.ValidString(got) || got != strings.Repeat("a", 79) {
		t.Fatalf("truncated=%q len=%d", got, len(got))
	}
}

func TestFamilyNewsDeleteRequiresBossAndClearsBody(t *testing.T) {
	s, catalog := familyNewsFixture(t)
	s.FamilyNews.Bodies[2] = FamilyNewsHeader + "notice\n"

	memberDelete, err := s.PlanFamilyNewsDelete("member", catalog)
	if err != nil {
		t.Fatal(err)
	}
	if memberDelete.Changed || memberDelete.Response != FamilyNewsNotBossResponse || memberDelete.AfterBody != FamilyNewsHeader+"notice\n" {
		t.Fatalf("member delete=%+v", memberDelete)
	}

	proposal, err := s.PlanFamilyNewsDelete("boss", catalog)
	if err != nil {
		t.Fatal(err)
	}
	if !proposal.Changed || proposal.Response != FamilyNewsDeletedResponse || proposal.AfterPresent || proposal.AfterBody != "" {
		t.Fatalf("boss delete=%+v", proposal)
	}
	next, result, err := s.ApplyFamilyNews(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := next.FamilyNews.Bodies[2]; ok || result.Body != "" || !result.Changed {
		t.Fatalf("after delete news=%+v result=%+v", next.FamilyNews, result)
	}
	if _, _, err := next.ApplyFamilyNews(proposal); !errors.Is(err, ErrFamilyNewsStaleProposal) {
		t.Fatalf("delete replay err=%v", err)
	}

	empty, err := next.PlanFamilyNewsDelete("boss", catalog)
	if err != nil {
		t.Fatal(err)
	}
	if empty.Changed || empty.Response != FamilyNewsDeletedResponse {
		t.Fatalf("empty delete=%+v", empty)
	}
}
