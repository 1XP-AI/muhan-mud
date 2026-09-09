package transport

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/session"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func canonicalVoteTransportState(t *testing.T, choices []byte) (world.State, world.VoteCatalog) {
	t.Helper()
	var flags [8]byte
	flags[world.VoteElectionRoomFlag/8] |= 1 << (world.VoteElectionRoomFlag % 8)
	catalog := world.VoteCatalog{Issue: world.VoteIssue{Number: 2, Prompt: "다음 대표를 선택하세요.", Options: []string{"첫 번째 후보", "두 번째 후보"}}}
	digest, err := catalog.Digest()
	if err != nil {
		t.Fatal(err)
	}
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "투표소", Flags: flags}},
			PlayerIDs: []string{"actor"},
		}},
		Players: map[string]world.PlayerState{"actor": {
			Body: world.LegacyMonster{
				Name: "Alice", Type: 0, Class: 4, RoomID: 1,
				Timers: [45]world.LegacyTimer{world.VoteHoursTimerIndex: {Interval: 3 * 86400}},
			},
			Online: true,
		}},
		Votes: &world.VoteState{Ballots: map[string]world.VoteBallot{}, History: []world.VoteHistoryEntry{}},
	}
	if choices != nil {
		state.Votes.Ballots["actor"] = world.VoteBallot{CatalogDigest: digest, Choices: append([]byte(nil), choices...)}
		state.Votes.History = []world.VoteHistoryEntry{{Sequence: 1, Operation: world.VoteOperationWrite, ActorID: "actor", CatalogDigest: digest, Choices: append([]byte(nil), choices...)}}
	}
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	return state, catalog
}

type voteTransportRetryStore struct {
	base     *connectorCommandStore
	failOnce bool
	attempts []string
}

func (s *voteTransportRetryStore) ReadWorldReceipt(ctx context.Context, worldID, commandID string, request json.RawMessage) (storage.WorldReceipt, error) {
	return s.base.ReadWorldReceipt(ctx, worldID, commandID, request)
}

func (s *voteTransportRetryStore) LoadWorld(ctx context.Context, worldID string) (storage.WorldSnapshot, error) {
	return s.base.LoadWorld(ctx, worldID)
}

func (s *voteTransportRetryStore) CommitWorldCommand(ctx context.Context, worldID, commandID string, request json.RawMessage, revision int64, state, response json.RawMessage) (storage.WorldReceipt, error) {
	s.attempts = append(s.attempts, commandID)
	if s.failOnce {
		s.failOnce = false
		return storage.WorldReceipt{}, errors.New("transient vote commit failure")
	}
	return s.base.CommitWorldCommand(ctx, worldID, commandID, request, revision, state, response)
}

func voteTransportConnection(t *testing.T, store *connectorCommandStore, catalog world.VoteCatalog) (*WorldConnector, *worldConnection) {
	t.Helper()
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "vote-transport-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1,
		VoteCatalog: catalog,
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	connection := &worldConnection{game: connector, lease: lease, ready: true, events: make(chan string, 2)}
	return connector, connection
}

func TestWorldConnectorVoteStopsBeforeBallotReceiptWhenAuthorityIsMissing(t *testing.T) {
	var flags [8]byte
	flags[world.VoteElectionRoomFlag/8] |= 1 << (world.VoteElectionRoomFlag % 8)
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "투표소", Flags: flags}},
			PlayerIDs: []string{"actor"},
		}},
		Players: map[string]world.PlayerState{"actor": {
			Body: world.LegacyMonster{
				Name:   "Alice",
				Type:   0,
				Class:  4,
				RoomID: 1,
				Timers: [45]world.LegacyTimer{world.VoteHoursTimerIndex: {Interval: 3 * 86400}},
			},
			Online: true,
		}},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "vote-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1,
		VoteCatalog: world.VoteCatalog{Issue: world.VoteIssue{Number: 1, Prompt: "선택", Options: []string{"A"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	connection := &worldConnection{game: connector, lease: lease, ready: true, events: make(chan string, 2)}
	connector.mu.Lock()
	connector.connections[connection] = struct{}{}
	connector.mu.Unlock()

	output, err := connection.Submit(context.Background(), "투표")
	if err != nil || output != "아직 구현되지 않은 명령입니다.\r\n" || store.commits != 0 || !connection.ready {
		t.Fatalf("output=%q err=%v commits=%d ready=%t", output, err, store.commits, connection.ready)
	}
}

func TestWorldConnectorVoteContinuationWritesCanonicalBallot(t *testing.T) {
	state, catalog := canonicalVoteTransportState(t, nil)
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	_, connection := voteTransportConnection(t, store, catalog)
	start, err := connection.Submit(context.Background(), "투표")
	if err != nil || !strings.Contains(start, catalog.Issue.Prompt) || !strings.Contains(start, "당신의 선택은? : ") || store.commits != 0 || connection.vote == nil {
		t.Fatalf("start=%q err=%v commits=%d vote=%+v", start, err, store.commits, connection.vote)
	}
	second, err := connection.Submit(context.Background(), "a")
	if err != nil || !strings.Contains(second, catalog.Issue.Options[0]) || store.commits != 0 {
		t.Fatalf("second=%q err=%v commits=%d", second, err, store.commits)
	}
	final, err := connection.Submit(context.Background(), "g")
	if err != nil || final != world.VoteSubmittedResponse || store.commits != 1 || connection.vote != nil {
		t.Fatalf("final=%q err=%v commits=%d vote=%+v", final, err, store.commits, connection.vote)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || string(saved.Votes.Ballots["actor"].Choices) != "AG" || len(saved.Votes.History) != 1 {
		t.Fatalf("saved=%+v err=%v", saved.Votes, err)
	}
}

func TestWorldConnectorVoteExistingBallotConfirmsBeforeRewrite(t *testing.T) {
	state, catalog := canonicalVoteTransportState(t, []byte("AB"))
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	_, connection := voteTransportConnection(t, store, catalog)
	start, err := connection.Submit(context.Background(), "투표")
	if err != nil || !strings.Contains(start, session.VoteAlreadyVotedResponse) || !strings.Contains(start, session.VoteChangePrompt) || store.commits != 0 {
		t.Fatalf("start=%q err=%v commits=%d", start, err, store.commits)
	}
	cancelled, err := connection.Submit(context.Background(), "n")
	if err != nil || cancelled != session.VoteCancelResponse || connection.vote != nil || store.commits != 0 {
		t.Fatalf("cancelled=%q err=%v vote=%+v commits=%d", cancelled, err, connection.vote, store.commits)
	}
	if _, err := connection.Submit(context.Background(), "투표"); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Submit(context.Background(), "y"); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Submit(context.Background(), "g"); err != nil {
		t.Fatal(err)
	}
	final, err := connection.Submit(context.Background(), "a")
	if err != nil || final != world.VoteSubmittedResponse || store.commits != 1 {
		t.Fatalf("rewrite=%q err=%v commits=%d", final, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || string(saved.Votes.Ballots["actor"].Choices) != "GA" || len(saved.Votes.History) != 2 || saved.Votes.History[1].Operation != world.VoteOperationRewrite {
		t.Fatalf("rewritten=%+v err=%v", saved.Votes, err)
	}
}

func TestWorldConnectorVoteContinuationRetriesWithStableReceiptID(t *testing.T) {
	state, catalog := canonicalVoteTransportState(t, nil)
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	base := &connectorCommandStore{state: raw}
	store := &voteTransportRetryStore{base: base, failOnce: true}
	connector, connection := voteTransportConnection(t, base, catalog)
	// Replace the connector store after construction to preserve the helper's
	// ownership setup while exercising the failure/retry adapter.
	connector.config.Store = store
	if _, err := connection.Submit(context.Background(), "투표"); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Submit(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
	first, err := connection.Submit(context.Background(), "g")
	if err != nil || !strings.Contains(first, "다시 시도") || connection.vote == nil || len(store.attempts) != 1 {
		t.Fatalf("first=%q err=%v vote=%+v attempts=%v", first, err, connection.vote, store.attempts)
	}
	wantID := store.attempts[0]
	second, err := connection.Submit(context.Background(), "g")
	if err != nil || second != world.VoteSubmittedResponse || connection.vote != nil || len(store.attempts) != 2 || store.attempts[1] != wantID || base.commits != 1 {
		t.Fatalf("second=%q err=%v vote=%+v attempts=%v commits=%d", second, err, connection.vote, store.attempts, base.commits)
	}
}
