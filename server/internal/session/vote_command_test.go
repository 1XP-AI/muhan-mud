package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func voteSessionCatalog() world.VoteCatalog {
	return world.VoteCatalog{Issue: world.VoteIssue{
		Number:  2,
		Prompt:  "다음 대표를 선택하세요.",
		Options: []string{"첫 번째 후보", "두 번째 후보"},
	}}
}

func voteSessionState(t *testing.T) []byte {
	t.Helper()
	var flags [8]byte
	flags[world.VoteElectionRoomFlag/8] |= 1 << (world.VoteElectionRoomFlag % 8)
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "투표소", Flags: flags}},
				PlayerIDs: []string{"actor"},
			},
		},
		Players: map[string]world.PlayerState{
			"actor": {
				Body: world.LegacyMonster{
					Name: "Alice", Type: 0, Class: 4, RoomID: 1,
					Timers: [45]world.LegacyTimer{world.VoteHoursTimerIndex: {Interval: 3 * 86400}},
				},
				Online: true,
			},
		},
	}
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func voteSessionOwner(t *testing.T) (*Ownership, SessionLease) {
	t.Helper()
	owners := &Ownership{}
	lease, err := owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	return owners, lease
}

func TestParseVoteLineAdmitsOnlyExactBareAlias(t *testing.T) {
	for _, line := range []string{"투표", "  투표  "} {
		command, ok := ParseVoteLine(line)
		if !ok || command.Alias != "투표" || !IsVoteLine(line) {
			t.Fatalf("line=%q command=%+v ok=%t", line, command, ok)
		}
	}
	for _, line := range []string{"투표 후보", "투표\n선택", "투표\x00", "vote", ""} {
		if _, ok := ParseVoteLine(line); ok || IsVoteLine(line) {
			t.Fatalf("unsupported vote line accepted: %q", line)
		}
	}
	parsed, err := ParseCommand("투표")
	if err != nil || parsed.Kind != CommandVote {
		t.Fatalf("ParseCommand(투표)=%+v err=%v", parsed, err)
	}
}

func TestParseVoteContinuationLineKeepsPromptsOutsideReceiptParser(t *testing.T) {
	for _, test := range []struct {
		line  string
		kind  VoteContinuationKind
		value byte
	}{
		{line: "y", kind: VoteConfirmContinuation, value: 'Y'},
		{line: "N", kind: VoteConfirmContinuation, value: 'N'},
		{line: "a", kind: VoteChoiceContinuation, value: 'A'},
		{line: "G", kind: VoteChoiceContinuation, value: 'G'},
	} {
		input, ok := ParseVoteContinuationLine(test.line)
		if !ok || input.Kind != test.kind || input.Choice != test.value {
			t.Fatalf("line=%q input=%+v ok=%t", test.line, input, ok)
		}
	}
	for _, line := range []string{"yes", "", "h", "1", "a\n"} {
		if _, ok := ParseVoteContinuationLine(line); ok {
			t.Fatalf("unsupported vote continuation accepted: %q", line)
		}
	}
	if _, ok := ParseVoteConfirmationLine("a"); ok {
		t.Fatal("choice parsed as confirmation")
	}
	if _, ok := ParseVoteChoiceLine("y"); ok {
		t.Fatal("confirmation parsed as choice")
	}
}

func TestVotePromptForOptionUsesSourceOneBasedAnswerLines(t *testing.T) {
	issue := voteSessionCatalog().Issue
	question, err := VotePromptForOption(issue, 0)
	if err != nil || !bytes.Contains([]byte(question), []byte(issue.Prompt)) {
		t.Fatalf("question=%q err=%v", question, err)
	}
	first, err := VotePromptForOption(issue, 1)
	if err != nil || !bytes.Contains([]byte(first), []byte(issue.Options[0])) || bytes.Contains([]byte(first), []byte(issue.Options[1])) {
		t.Fatalf("first=%q err=%v", first, err)
	}
	second, err := VotePromptForOption(issue, 2)
	if err != nil || !bytes.Contains([]byte(second), []byte(issue.Options[1])) {
		t.Fatalf("second=%q err=%v", second, err)
	}
	if _, err := VotePromptForOption(issue, 3); !errors.Is(err, world.ErrVoteInvalidProposal) {
		t.Fatalf("out-of-range err=%v", err)
	}
}

func TestExecuteVoteLineFailsClosedBeforeReceiptWithoutBallotState(t *testing.T) {
	store := &departureStore{state: voteSessionState(t)}
	owners, lease := voteSessionOwner(t)
	before := append([]byte(nil), store.state...)
	_, err := owners.ExecuteVoteLine(context.Background(), store, "w", "vote-1", lease, "투표", voteSessionCatalog())
	if !errors.Is(err, world.ErrVoteStateUnresolved) || store.commits != 0 {
		t.Fatalf("vote err=%v commits=%d", err, store.commits)
	}
	if !bytes.Equal(before, store.state) {
		t.Fatal("failed-closed vote changed snapshot")
	}

	if _, err := owners.ExecuteVoteLine(context.Background(), store, "w", "vote-missing-catalog", lease, "투표"); !errors.Is(err, world.ErrVoteCatalogUnavailable) || store.commits != 0 {
		t.Fatalf("missing catalog err=%v commits=%d", err, store.commits)
	}
	if _, err := owners.ExecuteVoteLine(context.Background(), store, "w", "vote-bad-line", lease, "투표 Alice", voteSessionCatalog()); !errors.Is(err, ErrUnsupportedVoteLine) || store.commits != 0 {
		t.Fatalf("bad line err=%v commits=%d", err, store.commits)
	}
	if _, err := owners.ExecuteVoteLine(context.Background(), store, "w", "vote-multiple-catalogs", lease, "투표", voteSessionCatalog(), voteSessionCatalog()); err == nil || store.commits != 0 {
		t.Fatalf("multiple catalogs err=%v commits=%d", err, store.commits)
	}
}

func TestExecuteVoteLineReceiptReplayPrecedesUnresolvedBallotReducer(t *testing.T) {
	store := &departureStore{
		state:        voteSessionState(t),
		receipt:      &storage.WorldReceipt{Revision: 11, Response: json.RawMessage(`{"action":"vote-issue"}`)},
		receiptWorld: "w",
		receiptID:    "vote-replay",
	}
	owners, lease := voteSessionOwner(t)
	replay, err := owners.ExecuteVoteLine(context.Background(), store, "w", "vote-replay", lease, "투표", voteSessionCatalog())
	if err != nil || !replay.Replayed || replay.Revision != 11 || !bytes.Equal(replay.Response, []byte(`{"action":"vote-issue"}`)) || store.commits != 0 {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestBeginVoteContinuationRequiresResolvedCanonicalState(t *testing.T) {
	store := &departureStore{state: voteSessionState(t)}
	owners, lease := voteSessionOwner(t)
	if _, err := owners.BeginVoteContinuation(context.Background(), store, "w", lease, VoteOptions{Catalog: voteSessionCatalog()}); !errors.Is(err, world.ErrVoteStateUnresolved) {
		t.Fatalf("unresolved vote start err=%v", err)
	}
	if store.commits != 0 {
		t.Fatalf("unresolved vote start committed=%d", store.commits)
	}
}

func TestExecuteVoteContinuationWritesAndReplaysCanonicalBallot(t *testing.T) {
	state, err := world.DecodeState(voteSessionState(t))
	if err != nil {
		t.Fatal(err)
	}
	state.Votes = &world.VoteState{Ballots: map[string]world.VoteBallot{}, History: []world.VoteHistoryEntry{}}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: raw}
	owners, lease := voteSessionOwner(t)
	start, err := owners.BeginVoteContinuation(context.Background(), store, "w", lease, VoteOptions{Catalog: voteSessionCatalog()})
	if err != nil || start.Projection.HasBallot || start.Continuation.NextOption != 1 {
		t.Fatalf("start=%+v err=%v", start, err)
	}
	continuation, done, err := start.Continuation.Choose("a")
	if err != nil || done {
		t.Fatalf("first choice=%+v done=%t err=%v", continuation, done, err)
	}
	continuation, done, err = continuation.Choose("g")
	if err != nil || !done {
		t.Fatalf("final choice=%+v done=%t err=%v", continuation, done, err)
	}
	first, err := owners.ExecuteVoteContinuation(context.Background(), store, "w", "vote-submit-1", lease, start.Catalog, continuation)
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.VoteResult
	if err := json.Unmarshal(first.Response, &result); err != nil || result.Operation != world.VoteOperationWrite || result.Response != world.VoteSubmittedResponse {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || string(saved.Votes.Ballots["actor"].Choices) != "AG" || len(saved.Votes.History) != 1 {
		t.Fatalf("saved vote=%+v err=%v", saved.Votes, err)
	}
	replay, err := owners.ExecuteVoteContinuation(context.Background(), store, "w", "vote-submit-1", lease, start.Catalog, continuation)
	if err != nil || !replay.Replayed || replay.Revision != first.Revision || !bytes.Equal(replay.Response, first.Response) || store.commits != 1 {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}
