package world

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func boardFixture() BoardState {
	return BoardState{
		Boards: map[int]Board{
			100: {
				Posts: []BoardPost{
					{Number: 1, Author: "Alice", Title: "첫 글", Body: "첫 본문\n", CreatedAt: time.Date(2026, 9, 9, 1, 0, 0, 0, time.UTC)},
					{Number: 2, Author: "Bob", Title: "삭제 글", Body: "삭제된 본문\n", CreatedAt: time.Date(2026, 9, 9, 2, 0, 0, 0, time.UTC), ReadCount: 2, Deleted: true},
					{Number: 3, Author: "Alice", Title: "새 글", Body: "새 본문\n", CreatedAt: time.Date(2026, 9, 9, 3, 0, 0, 0, time.UTC), ReadCount: 1},
				},
			},
		},
	}
}

func TestBoardStateValidateCloneAndRejectMalformedState(t *testing.T) {
	s := boardFixture()
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}

	clone := s.Clone()
	clone.Boards[100].Posts[0].Title = "changed"
	posts := clone.Boards[100].Posts
	posts[0].Body = "changed body\n"
	clone.Boards[100] = Board{Posts: posts}
	if s.Boards[100].Posts[0].Title != "첫 글" || s.Boards[100].Posts[0].Body != "첫 본문\n" {
		t.Fatal("clone mutation changed source state")
	}

	invalid := []struct {
		name   string
		mutate func(*BoardState)
	}{
		{name: "nil boards", mutate: func(s *BoardState) { s.Boards = nil }},
		{name: "invalid board", mutate: func(s *BoardState) { s.Boards[999] = Board{} }},
		{name: "duplicate post number", mutate: func(s *BoardState) {
			p := s.Boards[100].Posts
			p[1].Number = p[0].Number
			s.Boards[100] = Board{Posts: p}
		}},
		{name: "non contiguous post number", mutate: func(s *BoardState) { p := s.Boards[100].Posts; p[1].Number = 4; s.Boards[100] = Board{Posts: p} }},
		{name: "negative read count", mutate: func(s *BoardState) { p := s.Boards[100].Posts; p[0].ReadCount = -1; s.Boards[100] = Board{Posts: p} }},
		{name: "missing author", mutate: func(s *BoardState) { p := s.Boards[100].Posts; p[0].Author = ""; s.Boards[100] = Board{Posts: p} }},
		{name: "invalid utf8", mutate: func(s *BoardState) {
			p := s.Boards[100].Posts
			p[0].Title = string([]byte{0xff})
			s.Boards[100] = Board{Posts: p}
		}},
		{name: "control body", mutate: func(s *BoardState) {
			p := s.Boards[100].Posts
			p[0].Body = "bad\x1bbody"
			s.Boards[100] = Board{Posts: p}
		}},
		{name: "missing timestamp", mutate: func(s *BoardState) {
			p := s.Boards[100].Posts
			p[0].CreatedAt = time.Time{}
			s.Boards[100] = Board{Posts: p}
		}},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			candidate := boardFixture()
			tc.mutate(&candidate)
			if err := candidate.Validate(); err == nil {
				t.Fatal("malformed board state accepted")
			}
		})
	}
}

func TestBoardListIsNewestFirstAndHidesDeletedForOrdinaryReader(t *testing.T) {
	s := boardFixture()
	ordinary, err := s.ListBoard(100, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := []int{ordinary[0].Number, ordinary[1].Number}; !reflect.DeepEqual(got, []int{3, 1}) {
		t.Fatalf("ordinary list=%v", got)
	}
	admin, err := s.ListBoard(100, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := []int{admin[0].Number, admin[1].Number, admin[2].Number}; !reflect.DeepEqual(got, []int{3, 2, 1}) {
		t.Fatalf("admin list=%v", got)
	}
	ordinary[0].Title = "mutated"
	if s.Boards[100].Posts[2].Title == "mutated" {
		t.Fatal("list returned aliased post data")
	}
	if _, err := s.ListBoard(999, false); !errors.Is(err, ErrBoardInvalidID) {
		t.Fatalf("invalid board error=%v", err)
	}
}

func TestBoardReadOwnerDoesNotIncrementAndNonOwnerDoes(t *testing.T) {
	s := boardFixture()
	ownerProposal, err := s.PlanRead(100, 1, "Alice", false)
	if err != nil {
		t.Fatal(err)
	}
	ownerNext, ownerResult, err := s.ApplyRead(ownerProposal)
	if err != nil {
		t.Fatal(err)
	}
	if ownerNext.Boards[100].Posts[0].ReadCount != 0 || ownerResult.Changed || ownerResult.ReadCountAfter != 0 {
		t.Fatalf("owner read next=%+v result=%+v", ownerNext.Boards[100].Posts[0], ownerResult)
	}

	nonOwnerProposal, err := s.PlanRead(100, 1, "Carol", false)
	if err != nil {
		t.Fatal(err)
	}
	next, result, err := s.ApplyRead(nonOwnerProposal)
	if err != nil {
		t.Fatal(err)
	}
	if next.Boards[100].Posts[0].ReadCount != 1 || !result.Changed || result.ReadCountBefore != 0 || result.ReadCountAfter != 1 {
		t.Fatalf("non-owner read next=%+v result=%+v", next.Boards[100].Posts[0], result)
	}
	if s.Boards[100].Posts[0].ReadCount != 0 {
		t.Fatal("read planning/apply mutated source snapshot")
	}

	if _, err := s.PlanRead(100, 2, "Carol", false); !errors.Is(err, ErrBoardDeleted) {
		t.Fatalf("ordinary deleted read error=%v", err)
	}
	adminProposal, err := s.PlanRead(100, 2, "DM", true)
	if err != nil {
		t.Fatal(err)
	}
	adminNext, _, err := s.ApplyRead(adminProposal)
	if err != nil || adminNext.Boards[100].Posts[1].ReadCount != 3 {
		t.Fatalf("admin deleted read next=%+v err=%v", adminNext.Boards[100].Posts[1], err)
	}
}

func TestBoardDeleteRestoreRequiresAuthorOrAdminAndToggles(t *testing.T) {
	s := boardFixture()
	if _, err := s.PlanDeleteRestore(100, 1, "Carol", false); !errors.Is(err, ErrBoardUnauthorized) {
		t.Fatalf("ordinary delete permission error=%v", err)
	}
	authorProposal, err := s.PlanDeleteRestore(100, 1, "Alice", false)
	if err != nil {
		t.Fatal(err)
	}
	deleted, result, err := s.ApplyDeleteRestore(authorProposal)
	if err != nil {
		t.Fatal(err)
	}
	if !deleted.Boards[100].Posts[0].Deleted || !result.Deleted || result.Changed != true {
		t.Fatalf("delete next=%+v result=%+v", deleted.Boards[100].Posts[0], result)
	}

	if _, _, err := deleted.ApplyDeleteRestore(authorProposal); !errors.Is(err, ErrBoardStaleProposal) {
		t.Fatalf("replayed delete proposal error=%v", err)
	}
	adminProposal, err := deleted.PlanDeleteRestore(100, 1, "DM", true)
	if err != nil {
		t.Fatal(err)
	}
	restored, restoreResult, err := deleted.ApplyDeleteRestore(adminProposal)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Boards[100].Posts[0].Deleted || restoreResult.Deleted || !restoreResult.Changed {
		t.Fatalf("restore next=%+v result=%+v", restored.Boards[100].Posts[0], restoreResult)
	}
}

func TestBoardProposalsRejectTamperingAndMalformedInputs(t *testing.T) {
	s := boardFixture()
	p, err := s.PlanRead(100, 1, "Carol", false)
	if err != nil {
		t.Fatal(err)
	}
	p.PostNumber = 2
	if _, _, err := s.ApplyRead(p); !errors.Is(err, ErrBoardStaleProposal) {
		t.Fatalf("tampered read proposal error=%v", err)
	}

	deleteProposal, err := s.PlanDeleteRestore(100, 1, "Alice", false)
	if err != nil {
		t.Fatal(err)
	}
	deleteProposal.ActorID = "Carol"
	if _, _, err := s.ApplyDeleteRestore(deleteProposal); !errors.Is(err, ErrBoardUnauthorized) {
		t.Fatalf("tampered delete actor error=%v", err)
	}

	for _, boardID := range []int{0, 99, 117, 119, 121} {
		if _, err := s.ListBoard(boardID, false); !errors.Is(err, ErrBoardInvalidID) {
			t.Fatalf("board %d error=%v", boardID, err)
		}
	}
	for _, postNumber := range []int{0, -1, 4} {
		if _, err := s.PlanRead(100, postNumber, "Carol", false); err == nil {
			t.Fatalf("post %d accepted", postNumber)
		}
	}
	for _, input := range []string{"", " ", string([]byte{0xff}), "bad\x00", strings.Repeat("가", MaxBoardTitleBytes/3+1)} {
		if err := ValidateBoardTitle(input); err == nil {
			t.Fatalf("invalid title accepted: %q", input)
		}
	}
	if err := ValidateBoardBody("한 줄\n둘째 줄\n"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateBoardBody("bad\rbody"); err == nil {
		t.Fatal("carriage return body accepted")
	}
}
