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
