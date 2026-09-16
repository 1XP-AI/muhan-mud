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

type npcScavengeReceiptStore struct {
	mu       sync.Mutex
	state    json.RawMessage
	revision int64
	worldID  string
	command  string
	request  json.RawMessage
	receipt  *storage.WorldReceipt
	commits  int
}

func (s *npcScavengeReceiptStore) ReadWorldReceipt(_ context.Context, worldID, commandID string, request json.RawMessage) (storage.WorldReceipt, error) {
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

func (s *npcScavengeReceiptStore) LoadWorld(_ context.Context, worldID string) (storage.WorldSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if worldID == "" || s.state == nil {
		return storage.WorldSnapshot{}, errors.New("world snapshot absent")
	}
	return storage.WorldSnapshot{Revision: s.revision, State: append(json.RawMessage(nil), s.state...)}, nil
}

func (s *npcScavengeReceiptStore) CommitWorldCommand(_ context.Context, worldID, commandID string, request json.RawMessage, expected int64, state, response json.RawMessage) (storage.WorldReceipt, error) {
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

func npcScavengeReceiptFixture(t *testing.T, lastTime int32) json.RawMessage {
	t.Helper()
	npcFlags := [8]byte{}
	npcFlags[11/8] |= 1 << (11 % 8)
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "scavenge"}},
				PlayerIDs: []string{"player"},
				NPCIDs:    []string{"scavenger"},
				Items: &world.ItemCollection{
					Items:     map[string]world.Item{"gem": {Object: world.LegacyObject{Name: "gem"}}},
					Inventory: []string{"gem"},
				},
			},
		},
		Players: map[string]world.PlayerState{
			"player": {Body: world.LegacyMonster{Name: "영웅", Type: 0, RoomID: 1}, Online: true},
		},
		NPCs: map[string]world.NPCState{
			"scavenger": {
				Body: world.LegacyMonster{
					Name: "수집자", Type: 1, RoomID: 1, Flags: npcFlags,
					Timers: [45]world.LegacyTimer{{}},
				},
				Items:   &world.ItemCollection{Items: map[string]world.Item{}},
				Enemies: []world.NPCEnemy{},
			},
		},
		ActiveNPCIDs: []string{"scavenger"},
	}
	npc := state.NPCs["scavenger"]
	npc.Body.Timers[4] = world.LegacyTimer{LastTime: lastTime}
	state.NPCs["scavenger"] = npc
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestExecuteNPCScavengeReceiptPersistsAndReplaysWithoutReroll(t *testing.T) {
	store := &npcScavengeReceiptStore{state: npcScavengeReceiptFixture(t, 100)}
	rollCalls := 0
	first, err := ExecuteNPCScavengeReceipt(context.Background(), store, "scavenge-world", "scavenge-1", NPCScavengeOptions{
		Slot: 10,
		Now:  200,
		Roll: func(low, high int) int {
			rollCalls++
			if low != 1 || high != 100 {
				t.Fatalf("unexpected MSCAVE range %d..%d", low, high)
			}
			return 1
		},
	})
	if err != nil || first.Replayed || first.Revision != 1 || store.commits != 1 || rollCalls != 1 {
		t.Fatalf("first=%+v commits=%d rolls=%d err=%v", first, store.commits, rollCalls, err)
	}
	var result world.NPCScavengeTickSummary
	if err := json.Unmarshal(first.Response, &result); err != nil || len(result.Actions) != 1 || result.Actions[0].Decision != world.NPCScavengeTransferred || len(result.Events) != 1 {
		t.Fatalf("receipt result=%+v err=%v", result, err)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.NPCs["scavenger"].Body.Timers[4].LastTime != 200 || !worldFlag(saved.NPCs["scavenger"].Body, 18) {
		t.Fatalf("saved MSCAVE state=%+v err=%v", saved.NPCs["scavenger"], err)
	}

	replay, err := ExecuteNPCScavengeReceipt(context.Background(), store, "scavenge-world", "scavenge-1", NPCScavengeOptions{
		Slot: 10,
		Now:  200,
		Roll: func(int, int) int {
			t.Fatal("MSCAVE receipt replay rerolled")
			return 1
		},
	})
	if err != nil || !replay.Replayed || replay.Revision != first.Revision || store.commits != 1 || rollCalls != 1 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v commits=%d rolls=%d err=%v", replay, store.commits, rollCalls, err)
	}
}

func TestExecuteNPCScavengeReceiptAllowsNonDueWithoutRNGAndBindsRequest(t *testing.T) {
	store := &npcScavengeReceiptStore{state: npcScavengeReceiptFixture(t, 100)}
	first, err := ExecuteNPCScavengeReceipt(context.Background(), store, "scavenge-world", "scavenge-nondue", NPCScavengeOptions{Slot: 2, Now: 110})
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("non-due first=%+v commits=%d err=%v", first, store.commits, err)
	}
	var result world.NPCScavengeTickSummary
	if err := json.Unmarshal(first.Response, &result); err != nil || len(result.Actions) != 1 || result.Actions[0].Attempted || result.Changed || !result.NoOp {
		t.Fatalf("non-due result=%+v err=%v", result, err)
	}
	if _, err := ExecuteNPCScavengeReceipt(context.Background(), store, "scavenge-world", "scavenge-nondue", NPCScavengeOptions{Slot: 3, Now: 110}); !errors.Is(err, storage.ErrCommandConflict) {
		t.Fatalf("request conflict err=%v", err)
	}
	if _, err := ExecuteNPCScavengeReceipt(context.Background(), store, "scavenge-world", "bad-slot", NPCScavengeOptions{Slot: -1, Now: 110}); !errors.Is(err, errInvalidNPCScavengeReceipt) {
		t.Fatalf("negative slot err=%v", err)
	}
	if _, err := ExecuteNPCScavengeReceipt(context.Background(), store, "scavenge-world", "bad-now", NPCScavengeOptions{Slot: 1, Now: -1}); !errors.Is(err, errInvalidNPCScavengeReceipt) {
		t.Fatalf("negative now err=%v", err)
	}
}

func worldFlag(body world.LegacyMonster, bit uint) bool {
	return body.Flags[bit/8]&(1<<(bit%8)) != 0
}
