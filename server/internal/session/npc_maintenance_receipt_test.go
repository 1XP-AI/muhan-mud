package session

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

type npcMaintenanceReceiptStore struct {
	mu       sync.Mutex
	state    json.RawMessage
	revision int64
	worldID  string
	command  string
	request  json.RawMessage
	receipt  *storage.WorldReceipt
	commits  int
}

func (s *npcMaintenanceReceiptStore) ReadWorldReceipt(_ context.Context, worldID, commandID string, request json.RawMessage) (storage.WorldReceipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.receipt == nil || s.worldID != worldID || s.command != commandID {
		return storage.WorldReceipt{}, sql.ErrNoRows
	}
	if !bytes.Equal(s.request, request) {
		return storage.WorldReceipt{}, storage.ErrCommandConflict
	}
	replay := *s.receipt
	replay.Response = append(json.RawMessage(nil), replay.Response...)
	replay.Replayed = true
	return replay, nil
}

func (s *npcMaintenanceReceiptStore) LoadWorld(_ context.Context, worldID string) (storage.WorldSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if worldID == "" || s.state == nil {
		return storage.WorldSnapshot{}, errors.New("world snapshot absent")
	}
	return storage.WorldSnapshot{Revision: s.revision, State: append(json.RawMessage(nil), s.state...)}, nil
}

func (s *npcMaintenanceReceiptStore) CommitWorldCommand(_ context.Context, worldID, commandID string, request json.RawMessage, expected int64, state, response json.RawMessage) (storage.WorldReceipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if expected != s.revision {
		return storage.WorldReceipt{}, storage.ErrWorldConflict
	}
	s.state = append(json.RawMessage(nil), state...)
	s.revision++
	s.worldID = worldID
	s.command = commandID
	s.request = append(json.RawMessage(nil), request...)
	s.commits++
	receipt := storage.WorldReceipt{Revision: s.revision, Response: append(json.RawMessage(nil), response...)}
	s.receipt = &receipt
	return receipt, nil
}

func npcMaintenanceReceiptFixture(t *testing.T) json.RawMessage {
	t.Helper()
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}, Traffic: 100},
				PlayerIDs: []string{"hero"},
				NPCIDs:    []string{"wanderer"},
			},
		},
		Players: map[string]world.PlayerState{
			"hero": {Body: world.LegacyMonster{Name: "영웅", Type: 0, RoomID: 1}, Online: true},
		},
		NPCs: map[string]world.NPCState{
			"wanderer": {
				Body:    world.LegacyMonster{Name: "방황자", Type: 1, RoomID: 1, HPMax: 10, HPCurrent: 10, MPMax: 10, MPCurrent: 10},
				Enemies: []world.NPCEnemy{},
			},
		},
		ActiveNPCIDs: []string{"wanderer"},
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

func TestExecuteNPCMaintenanceReceiptPersistsAndReplaysWithoutReroll(t *testing.T) {
	store := &npcMaintenanceReceiptStore{state: npcMaintenanceReceiptFixture(t)}
	rollCalls := 0
	first, err := ExecuteNPCMaintenanceReceipt(context.Background(), store, "maintenance-world", "maintenance-1", NPCMaintenanceOptions{
		Slot: 10,
		Now:  100,
		Roll: func(low, high int) int {
			rollCalls++
			if low != 1 || high != 100 {
				t.Fatalf("unexpected C wander range %d..%d", low, high)
			}
			return 1
		},
	})
	if err != nil || first.Replayed || first.Revision != 1 || store.commits != 1 || rollCalls != 1 {
		t.Fatalf("first=%+v commits=%d rolls=%d err=%v", first, store.commits, rollCalls, err)
	}
	var result world.NPCMaintenanceResult
	if err := json.Unmarshal(first.Response, &result); err != nil || len(result.Actions) != 1 || !result.Actions[0].Wandered || len(result.ActiveNPCIDs) != 0 {
		t.Fatalf("maintenance receipt=%+v err=%v", result, err)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || len(saved.Rooms[1].NPCIDs) != 0 || len(saved.NPCs) != 0 || len(saved.ActiveNPCIDs) != 0 {
		t.Fatalf("saved maintenance state=%+v err=%v", saved, err)
	}

	replay, err := ExecuteNPCMaintenanceReceipt(context.Background(), store, "maintenance-world", "maintenance-1", NPCMaintenanceOptions{
		Slot: 10,
		Now:  100,
		Roll: func(int, int) int {
			t.Fatal("NPC maintenance receipt replay rerolled")
			return 1
		},
	})
	if err != nil || !replay.Replayed || replay.Revision != first.Revision || store.commits != 1 || rollCalls != 1 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v commits=%d rolls=%d err=%v", replay, store.commits, rollCalls, err)
	}
}

func TestExecuteNPCMaintenanceReceiptBindsSlotAndRejectsInvalidInput(t *testing.T) {
	store := &npcMaintenanceReceiptStore{state: npcMaintenanceReceiptFixture(t)}
	if _, err := ExecuteNPCMaintenanceReceipt(context.Background(), store, "w", "bad-slot", NPCMaintenanceOptions{Slot: -1, Now: 1, Roll: func(int, int) int { return 1 }}); !errors.Is(err, errInvalidNPCMaintenanceReceipt) {
		t.Fatalf("negative slot err=%v", err)
	}
	if _, err := ExecuteNPCMaintenanceReceipt(context.Background(), store, "w", "bad-rng", NPCMaintenanceOptions{Slot: 1, Now: 1}); !errors.Is(err, errInvalidNPCMaintenanceReceipt) {
		t.Fatalf("nil RNG err=%v", err)
	}
	first, err := ExecuteNPCMaintenanceReceipt(context.Background(), store, "maintenance-world", "maintenance-conflict", NPCMaintenanceOptions{Slot: 1, Now: 100, Roll: func(int, int) int { return 1 }})
	if err != nil || first.Replayed {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	if _, err := ExecuteNPCMaintenanceReceipt(context.Background(), store, "maintenance-world", "maintenance-conflict", NPCMaintenanceOptions{Slot: 2, Now: 100, Roll: func(int, int) int { t.Fatal("conflict rerolled"); return 1 }}); !errors.Is(err, storage.ErrCommandConflict) {
		t.Fatalf("request conflict err=%v", err)
	}
}
