package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

type durableMovementReceiptStore struct {
	mu            sync.Mutex
	state         json.RawMessage
	receipt       *storage.WorldReceipt
	commandID     string
	commitCount   int
	commitThenErr bool
}

func (s *durableMovementReceiptStore) ReadWorldReceipt(_ context.Context, _ string, commandID string, _ json.RawMessage) (storage.WorldReceipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.receipt == nil || s.commandID != commandID {
		return storage.WorldReceipt{}, sql.ErrNoRows
	}
	receipt := *s.receipt
	receipt.Replayed = true
	receipt.Projection = append(json.RawMessage(nil), s.receipt.Projection...)
	return receipt, nil
}

func (s *durableMovementReceiptStore) LoadWorld(context.Context, string) (storage.WorldSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return storage.WorldSnapshot{State: append(json.RawMessage(nil), s.state...)}, nil
}

func (s *durableMovementReceiptStore) CommitWorldCommand(ctx context.Context, worldID, commandID string, request json.RawMessage, expected int64, state, response json.RawMessage) (storage.WorldReceipt, error) {
	return s.CommitWorldCommandWithProjection(ctx, worldID, commandID, request, expected, state, response, json.RawMessage(`{}`))
}

func (s *durableMovementReceiptStore) CommitWorldCommandWithProjection(_ context.Context, _ string, commandID string, _ json.RawMessage, revision int64, state, response, projection json.RawMessage) (storage.WorldReceipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commitCount++
	s.state = append(json.RawMessage(nil), state...)
	s.commandID = commandID
	receipt := storage.WorldReceipt{
		Revision:   revision + 1,
		Response:   append(json.RawMessage(nil), response...),
		Projection: append(json.RawMessage(nil), projection...),
	}
	s.receipt = &receipt
	if s.commitThenErr {
		s.commitThenErr = false
		return storage.WorldReceipt{}, errors.New("commit outcome unknown")
	}
	return receipt, nil
}

func (s *durableMovementReceiptStore) MarkWorldReceiptProjectionDelivered(_ context.Context, _ string, commandID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.receipt == nil || s.commandID != commandID {
		return sql.ErrNoRows
	}
	s.receipt.ProjectionDelivered = true
	return nil
}

func durableFollowerTrapGoFixture(t *testing.T) []byte {
	t.Helper()
	s, err := world.DecodeState(goCommandFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	leader := s.Players["actor"]
	leader.FollowerIDs = []string{"follower"}
	leader.Body.Stats[1] = 1
	s.Players["actor"] = leader
	s.Players["follower"] = world.PlayerState{
		Body:        world.LegacyMonster{Name: "Bob", Type: 0, RoomID: 1, Class: 4, Level: 1, HPMax: 30, HPCurrent: 30, Stats: [5]byte{10, 1, 10, 10, 10}},
		Online:      true,
		FollowingID: "actor",
		Items:       &world.ItemCollection{Items: map[string]world.Item{}},
	}
	room := s.Rooms[1]
	room.PlayerIDs = []string{"actor", "follower"}
	s.Rooms[1] = room
	destination := s.Rooms[2]
	destination.Resource.Trap = world.TrapDart
	s.Rooms[2] = destination
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestExecuteGoLineRecoversFollowerTrapProjectionFromDurableReceipt(t *testing.T) {
	store := &durableMovementReceiptStore{state: durableFollowerTrapGoFixture(t), commitThenErr: true}
	owners := &Ownership{}
	lease, err := owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	rolls := []int{100, 7, 100, 8}
	first, err := owners.ExecuteGoLine(context.Background(), store, "w", "durable-follower-trap", lease, "가 동굴", 100, 12, nil, func(int, int) int {
		value := rolls[0]
		rolls = rolls[1:]
		return value
	}, nil)
	if err != nil || !first.Replayed || len(first.FollowerArrivalTrapEvents) != 1 || store.commitCount != 1 {
		t.Fatalf("recovered first=%+v err=%v commits=%d", first, err, store.commitCount)
	}
	if len(rolls) != 0 || first.FollowerArrivalTrapEvents[0].ActorID != "follower" {
		t.Fatalf("projection=%+v rolls=%v", first.FollowerArrivalTrapEvents, rolls)
	}

	// A new Ownership models a restarted command handler. The reducer/RNG is
	// unavailable, but the durable projection remains replayable.
	restarted := &Ownership{}
	restartedLease, err := restarted.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.Admit(restartedLease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	recovered, err := restarted.ExecuteGoLine(context.Background(), store, "w", "durable-follower-trap", restartedLease, "가 동굴", 100, 12, nil, func(int, int) int {
		t.Fatal("durable replay reran reducer RNG")
		return 0
	}, nil)
	if err != nil || !recovered.Replayed || len(recovered.FollowerArrivalTrapEvents) != 1 || string(recovered.Response) != string(first.Response) {
		t.Fatalf("restarted recovered=%+v err=%v", recovered, err)
	}
	if err := store.MarkWorldReceiptProjectionDelivered(context.Background(), "w", "durable-follower-trap"); err != nil {
		t.Fatal(err)
	}
	acknowledged, err := restarted.ExecuteGoLine(context.Background(), store, "w", "durable-follower-trap", restartedLease, "가 동굴", 100, 12, nil, func(int, int) int {
		t.Fatal("acknowledged replay reran reducer RNG")
		return 0
	}, nil)
	if err != nil || !acknowledged.Replayed || len(acknowledged.FollowerArrivalTrapEvents) != 0 {
		t.Fatalf("acknowledged replay=%+v err=%v", acknowledged, err)
	}
}
