package world

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
)

func voteTestState(class byte, interval int32, electionRoom bool) State {
	var roomFlags [8]byte
	if electionRoom {
		roomFlags[VoteElectionRoomFlag/8] |= 1 << (VoteElectionRoomFlag % 8)
	}
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Name: "투표소", Flags: roomFlags}},
				PlayerIDs: []string{"actor"},
			},
		},
		Players: map[string]PlayerState{
			"actor": {
				Body: LegacyMonster{
					Name:   "Alice",
					Type:   0,
					Class:  class,
					RoomID: 1,
					Timers: [45]LegacyTimer{VoteHoursTimerIndex: {Interval: interval}},
				},
				Online: true,
			},
		},
	}
}

func voteTestCatalog() VoteCatalog {
	return VoteCatalog{Issue: VoteIssue{
		Number:  3,
		Prompt:  "다음 운영자를 선택하세요.",
		Options: []string{"첫 번째 후보", "두 번째 후보", "세 번째 후보"},
	}}
}

func TestVoteCatalogValidatesSourceShapeAndClonesOptions(t *testing.T) {
	catalog := voteTestCatalog()
	if err := catalog.Validate(); err != nil {
		t.Fatal(err)
	}
	clone, err := catalog.Clone()
	if err != nil {
		t.Fatal(err)
	}
	clone.Issue.Options[0] = "변경된 복사본"
	if catalog.Issue.Options[0] == clone.Issue.Options[0] {
		t.Fatal("catalog clone shares option storage")
	}

	digest, err := catalog.Digest()
	if err != nil || len(digest) != 64 {
		t.Fatalf("digest=%q err=%v", digest, err)
	}
	changed := catalog
	changed.Issue.Options = append([]string(nil), catalog.Issue.Options...)
	changed.Issue.Options[0] = "다른 후보"
	changedDigest, err := changed.Digest()
	if err != nil || digest == changedDigest {
		t.Fatalf("digest did not bind exact catalog: before=%q after=%q err=%v", digest, changedDigest, err)
	}

	for _, invalid := range []VoteCatalog{
		{},
		{Issue: VoteIssue{Number: 0, Prompt: "안건", Options: []string{"A"}}},
		{Issue: VoteIssue{Number: 80, Prompt: "안건", Options: make([]string, 80)}},
		{Issue: VoteIssue{Number: 2, Prompt: "안건", Options: []string{"A"}}},
		{Issue: VoteIssue{Number: 1, Prompt: "", Options: []string{"A"}}},
		{Issue: VoteIssue{Number: 1, Prompt: "안건\n주입", Options: []string{"A"}}},
		{Issue: VoteIssue{Number: 1, Prompt: "안건", Options: []string{""}}},
		{Issue: VoteIssue{Number: 1, Prompt: "안건", Options: []string{"A\r\n주입"}}},
	} {
		if err := invalid.Validate(); !errors.Is(err, ErrVoteCatalogInvalid) && !errors.Is(err, ErrVoteCatalogUnavailable) {
			t.Fatalf("invalid catalog err=%v", err)
		}
	}
	if VoteOptionKey(-1) != 0 || VoteOptionKey(VoteMaxOptions) != 0 || VoteOptionKey(0) != 'A' || VoteOptionKey(6) != 'G' {
		t.Fatalf("unexpected source option key mapping")
	}
}

func TestPlanVoteBindsAgeRoomAndImmutableCatalogWithoutFilesystem(t *testing.T) {
	state := voteTestState(4, 3*86400, true) // 18 + 3 days = 21
	catalog := voteTestCatalog()
	proposal, err := state.PlanVote("actor", catalog)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Action != "vote" || proposal.ActorID != "actor" || proposal.ActorName != "Alice" || proposal.RoomID != 1 || proposal.Issue.Number != 3 || proposal.CatalogDigest == "" || proposal.Changed {
		t.Fatalf("proposal=%+v", proposal)
	}

	projection, err := state.ProjectVoteIssue("actor", catalog)
	if err != nil {
		t.Fatal(err)
	}
	if projection.Action != "vote-issue" || projection.Issue.Prompt != catalog.Issue.Prompt || !reflect.DeepEqual(projection.Issue.Options, catalog.Issue.Options) || projection.Changed {
		t.Fatalf("projection=%+v", projection)
	}
	projection.Issue.Options[0] = "클라이언트 변경"
	if catalog.Issue.Options[0] == projection.Issue.Options[0] {
		t.Fatal("projection aliases server-owned catalog")
	}

	tooYoung := voteTestState(4, 2*86400, true)
	if _, err := tooYoung.PlanVote("actor", catalog); !errors.Is(err, ErrVoteAge) {
		t.Fatalf("too-young err=%v", err)
	}
	admin := voteTestState(VoteInvincibleClass, 0, true)
	if _, err := admin.PlanVote("actor", catalog); err != nil {
		t.Fatalf("invincible age bypass err=%v", err)
	}
	notElection := voteTestState(4, 3*86400, false)
	if _, err := notElection.PlanVote("actor", catalog); !errors.Is(err, ErrVoteRoom) {
		t.Fatalf("non-election room err=%v", err)
	}
	if _, err := state.PlanVote("actor", VoteCatalog{}); !errors.Is(err, ErrVoteCatalogUnavailable) {
		t.Fatalf("missing catalog err=%v", err)
	}
	negative := voteTestState(4, -1, true)
	if _, err := negative.PlanVote("actor", catalog); !errors.Is(err, ErrVoteNumeric) {
		t.Fatalf("negative interval err=%v", err)
	}
}

func TestApplyVoteFailsClosedWhenBallotAuthorityIsAbsent(t *testing.T) {
	state := voteTestState(4, 3*86400, true)
	proposal, err := state.PlanVote("actor", voteTestCatalog())
	if err != nil {
		t.Fatal(err)
	}
	before := state
	if _, _, err := state.ApplyVote(proposal); !errors.Is(err, ErrVoteStateUnresolved) {
		t.Fatalf("valid vote proposal err=%v", err)
	}
	if !reflect.DeepEqual(state, before) {
		t.Fatal("failed-closed vote changed state")
	}

	tampered := proposal
	tampered.Changed = true
	if _, _, err := state.ApplyVote(tampered); !errors.Is(err, ErrVoteInvalidProposal) {
		t.Fatalf("tampered proposal err=%v", err)
	}
	stale := state
	staleActor := stale.Players["actor"]
	staleActor.Body.Level++
	stale.Players["actor"] = staleActor
	if _, _, err := stale.ApplyVote(proposal); !errors.Is(err, ErrVoteStaleProposal) {
		t.Fatalf("stale proposal err=%v", err)
	}
	if _, _, err := state.ApplyVote(VoteProposal{}); !errors.Is(err, ErrVoteInvalidProposal) {
		t.Fatalf("zero proposal err=%v", err)
	}
}

func TestVoteContinuationIsConnectionLocalAndNeverMutatesProjection(t *testing.T) {
	state := voteTestState(4, 3*86400, true)
	projection, err := state.ProjectVoteIssue("actor", voteTestCatalog())
	if err != nil {
		t.Fatal(err)
	}
	continuation, err := NewVoteContinuation(projection)
	if err != nil || continuation.NextOption != 1 || len(continuation.Choices) != 0 {
		t.Fatalf("continuation=%+v err=%v", continuation, err)
	}

	if _, _, err := continuation.Choose("z"); !errors.Is(err, ErrVoteInvalidProposal) {
		t.Fatalf("invalid choice err=%v", err)
	}
	if _, _, err := continuation.Choose("AB"); !errors.Is(err, ErrVoteInvalidProposal) {
		t.Fatalf("multi-byte choice err=%v", err)
	}
	next, done, err := continuation.Choose("a")
	if err != nil || done || next.NextOption != 2 || !bytes.Equal(next.Choices, []byte("A")) {
		t.Fatalf("first choice next=%+v done=%t err=%v", next, done, err)
	}
	next, done, err = next.Choose("B")
	if err != nil || done || next.NextOption != 3 || !bytes.Equal(next.Choices, []byte("AB")) {
		t.Fatalf("second choice next=%+v done=%t err=%v", next, done, err)
	}
	next, done, err = next.Choose("g")
	if err != nil || !done || next.NextOption != 4 || !bytes.Equal(next.Choices, []byte("ABG")) {
		t.Fatalf("final choice next=%+v done=%t err=%v", next, done, err)
	}
	if _, _, err := next.Choose("A"); !errors.Is(err, ErrVoteInvalidProposal) {
		t.Fatalf("choice after completion err=%v", err)
	}
}
