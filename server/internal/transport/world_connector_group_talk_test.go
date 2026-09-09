package transport

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

// groupTalkConnectorStore deliberately replays the one committed receipt for
// any later command lookup. The connector normally generates a fresh random
// command ID for each Submit, so this test store makes the replay branch
// deterministic without changing production transport code.
type groupTalkConnectorStore struct {
	mu      sync.Mutex
	state   json.RawMessage
	receipt *storage.WorldReceipt
	commits int
}

func (s *groupTalkConnectorStore) ReadWorldReceipt(context.Context, string, string, json.RawMessage) (storage.WorldReceipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.receipt == nil {
		return storage.WorldReceipt{}, sql.ErrNoRows
	}
	receipt := *s.receipt
	receipt.Replayed = true
	return receipt, nil
}

func (s *groupTalkConnectorStore) LoadWorld(context.Context, string) (storage.WorldSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return storage.WorldSnapshot{State: append(json.RawMessage(nil), s.state...)}, nil
}

func (s *groupTalkConnectorStore) CommitWorldCommand(_ context.Context, _ string, _ string, _ json.RawMessage, revision int64, state, response json.RawMessage) (storage.WorldReceipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commits++
	s.state = append(json.RawMessage(nil), state...)
	receipt := storage.WorldReceipt{Revision: revision + 1, Response: append(json.RawMessage(nil), response...)}
	s.receipt = &receipt
	return receipt, nil
}

func (s *groupTalkConnectorStore) snapshot() (json.RawMessage, *storage.WorldReceipt, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var receipt *storage.WorldReceipt
	if s.receipt != nil {
		copy := *s.receipt
		copy.Response = append(json.RawMessage(nil), s.receipt.Response...)
		receipt = &copy
	}
	return append(json.RawMessage(nil), s.state...), receipt, s.commits
}

func groupTalkConnectorState(t *testing.T) []byte {
	t.Helper()
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"leader", "follower"},
			},
		},
		Players: map[string]world.PlayerState{
			"leader": {
				Body:        world.LegacyMonster{Name: "Leader", Type: 0, RoomID: 1, Class: 4},
				Online:      true,
				FollowerIDs: []string{"follower"},
				FollowerRefs: []world.EntityRef{
					{Kind: "player", ID: "follower"},
				},
			},
			"follower": {
				Body:        world.LegacyMonster{Name: "Follower", Type: 0, RoomID: 1, Class: 4},
				Online:      true,
				FollowingID: "leader",
			},
		},
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestWorldConnectorSubmitDispatchesGroupTalkAndSuppressesReplayFanout(t *testing.T) {
	initial := groupTalkConnectorState(t)
	store := &groupTalkConnectorStore{state: append(json.RawMessage(nil), initial...)}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "group-talk-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	leaderLease, err := connector.owners.Acquire("leader")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(leaderLease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	followerLease, err := connector.owners.Acquire("follower")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(followerLease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	leader := &worldConnection{game: connector, lease: leaderLease, ready: true, events: make(chan string, 4)}
	follower := &worldConnection{game: connector, lease: followerLease, ready: true, events: make(chan string, 4)}
	connector.mu.Lock()
	connector.connections[leader] = struct{}{}
	connector.connections[follower] = struct{}{}
	connector.mu.Unlock()

	output, err := leader.Submit(context.Background(), "그룹말 hello")
	if err != nil || output != "예. 좋습니다.\r\n" {
		t.Fatalf("output=%q err=%v", output, err)
	}
	state, receipt, commits := store.snapshot()
	if commits != 1 || string(state) != string(initial) || receipt == nil {
		t.Fatalf("state/receipt/commits changed unexpectedly: state=%d/%d receipt=%v commits=%d", len(state), len(initial), receipt, commits)
	}
	var result world.GroupTalkResult
	if err := json.Unmarshal(receipt.Response, &result); err != nil {
		t.Fatal(err)
	}
	if !result.Broadcast || len(result.Events) != 2 || result.Events[0].RecipientID != "follower" || result.Events[1].RecipientID != "leader" {
		t.Fatalf("group result=%+v", result)
	}
	wantEvent := "Leader이 그룹원들에게 \"hello\"라고 말합니다.\r\n"
	select {
	case event := <-follower.events:
		if event != wantEvent {
			t.Fatalf("follower event=%q want=%q", event, wantEvent)
		}
	default:
		t.Fatal("follower group event missing")
	}
	select {
	case event := <-leader.events:
		if event != wantEvent {
			t.Fatalf("leader event=%q want=%q", event, wantEvent)
		}
	default:
		t.Fatal("leader group event missing")
	}

	replayOutput, err := leader.Submit(context.Background(), "그룹말 hello")
	if err != nil || replayOutput != output {
		t.Fatalf("replay output=%q err=%v", replayOutput, err)
	}
	if _, _, commits = store.snapshot(); commits != 1 {
		t.Fatalf("replay committed again: commits=%d", commits)
	}
	select {
	case event := <-follower.events:
		t.Fatalf("replay fanned out to follower: %q", event)
	default:
	}
	select {
	case event := <-leader.events:
		t.Fatalf("replay fanned out to leader: %q", event)
	default:
	}
}
