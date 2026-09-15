package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
	"testing"
)

type departureStore struct {
	state        json.RawMessage
	receipt      *storage.WorldReceipt
	receiptWorld string
	receiptID    string
	fail         bool
	commits      int
}

func (s *departureStore) ReadWorldReceipt(_ context.Context, worldID, commandID string, _ json.RawMessage) (storage.WorldReceipt, error) {
	if s.receipt == nil || s.receiptWorld != worldID || s.receiptID != commandID {
		return storage.WorldReceipt{}, sql.ErrNoRows
	}
	r := *s.receipt
	r.Replayed = true
	return r, nil
}
func (s *departureStore) LoadWorld(context.Context, string) (storage.WorldSnapshot, error) {
	return storage.WorldSnapshot{State: s.state}, nil
}
func (s *departureStore) CommitWorldCommand(_ context.Context, worldID, commandID string, _ json.RawMessage, revision int64, state, response json.RawMessage) (storage.WorldReceipt, error) {
	s.commits++
	if s.fail {
		return storage.WorldReceipt{}, errors.New("commit unavailable")
	}
	s.state = state
	s.receiptWorld = worldID
	s.receiptID = commandID
	r := storage.WorldReceipt{Revision: revision + 1, Response: response}
	s.receipt = &r
	return r, nil
}

func TestDepartCommitsBeforeReleaseAndRejectsStale(t *testing.T) {
	initial := world.State{Version: 1, Rooms: map[int16]world.RoomState{1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}}, PlayerIDs: []string{"a"}}}, Players: map[string]world.PlayerState{"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1, HPCurrent: 30}, Online: true}}}
	raw, _ := json.Marshal(initial)
	store := &departureStore{state: raw, fail: true}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	if r, err := owners.Depart(context.Background(), store, "w", "boot-1-depart-1", lease); err == nil || len(r.Response) != 0 || !owners.Owns(lease) {
		t.Fatal("released failed commit")
	}
	if owners.Release(lease) {
		t.Fatal("released closing reservation")
	}
	store.fail = false
	r, err := owners.Depart(context.Background(), store, "w", "boot-1-depart-1", lease)
	if err != nil || r.Revision != 1 || owners.Owns(lease) {
		t.Fatalf("%+v %v", r, err)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Online || len(saved.Rooms[1].PlayerIDs) != 0 || saved.Players["a"].Body.HPCurrent != 30 {
		t.Fatal("departure state")
	}
	next, _ := owners.Acquire("a")
	if _, err := owners.Depart(context.Background(), store, "w", "boot-1-depart-1", lease); err == nil || !owners.Owns(next) || store.commits != 2 {
		t.Fatal("stale departure reached store")
	}
}
