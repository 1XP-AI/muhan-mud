package transport

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func vitalTickState() json.RawMessage {
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1, Class: 4, Level: 1, HPMax: 100, HPCurrent: 10, MPMax: 80, MPCurrent: 2}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
	}
	r := s.Rooms[1]
	r.PlayerIDs = []string{"a"}
	s.Rooms[1] = r
	raw, _ := json.Marshal(s)
	return raw
}

func TestRunPlayerVitalTickUsesDeterministicSlotAndSkipsDuplicate(t *testing.T) {
	clockNow := int32(123)
	store := &connectorCommandStore{state: vitalTickState()}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "tick-world", MaxSessions: 1,
		Clock: func() (int32, int) { return clockNow, 12 },
	})
	if err != nil {
		t.Fatal(err)
	}
	first, ran, err := connector.RunPlayerVitalTick(context.Background(), 20*time.Second)
	if err != nil || !ran || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v ran=%v commits=%d err=%v", first, ran, store.commits, err)
	}
	if store.command != "player-vitals-6" {
		t.Fatalf("command=%q", store.command)
	}
	second, ran, err := connector.RunPlayerVitalTick(context.Background(), 20*time.Second)
	if err != nil || ran || second.Response != nil || store.commits != 1 {
		t.Fatalf("duplicate=%+v ran=%v commits=%d err=%v", second, ran, store.commits, err)
	}
	clockNow = 140
	third, ran, err := connector.RunPlayerVitalTick(context.Background(), 20*time.Second)
	if err != nil || !ran || third.Replayed || store.commits != 2 {
		t.Fatalf("next slot=%+v ran=%v commits=%d err=%v", third, ran, store.commits, err)
	}
	if store.command != "player-vitals-7" {
		t.Fatalf("next command=%q", store.command)
	}
}

type boundaryTickSpawnCatalog struct{}

func (boundaryTickSpawnCatalog) Monster(int16) (world.LegacyMonster, error) {
	return world.LegacyMonster{}, errors.New("unexpected NPC room refresh")
}

func (boundaryTickSpawnCatalog) Object(int16) (world.LegacyObject, error) {
	return world.LegacyObject{}, errors.New("unexpected room object refresh")
}

func TestRunPlayerVitalTickDoesNotClaimNPCRoomRefreshPhase(t *testing.T) {
	var initial world.State
	if err := json.Unmarshal(vitalTickState(), &initial); err != nil {
		t.Fatal(err)
	}
	room := initial.Rooms[1]
	room.Resource.PermanentMonsters[0] = world.LegacyTimer{LastTime: 0, Interval: 1, Misc: 7}
	room.Resource.PermanentObjects[0] = world.LegacyTimer{LastTime: 0, Interval: 1, Misc: 8}
	room.NPCIDs = []string{"wolf"}
	initial.Rooms[1] = room
	initial.NPCs = map[string]world.NPCState{
		"wolf": {
			Body:    world.LegacyMonster{Name: "늑대", Type: 1, RoomID: 1, HPMax: 30, HPCurrent: 30},
			Enemies: []world.NPCEnemy{},
		},
	}
	initial.ActiveNPCIDs = []string{"wolf"}
	if err := initial.Validate(); err != nil {
		t.Fatal(err)
	}
	initialRaw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: initialRaw}
	rollCalls, allocateCalls := 0, 0
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "tick-boundary-world", MaxSessions: 1,
		Clock:   func() (int32, int) { return 123, 12 },
		Catalog: boundaryTickSpawnCatalog{},
		Roll: func(int, int) int {
			rollCalls++
			return 0
		},
		Allocate: func() (string, error) {
			allocateCalls++
			return "", errors.New("unexpected room identity allocation")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ran, err := connector.RunPlayerVitalTick(context.Background(), 20*time.Second); err != nil || !ran {
		t.Fatalf("vital tick ran=%v err=%v", ran, err)
	}
	if rollCalls != 0 || allocateCalls != 0 {
		t.Fatalf("NPC/room refresh dependencies consumed: rolls=%d allocations=%d", rollCalls, allocateCalls)
	}
	stateRaw, _ := store.snapshot()
	after, err := world.DecodeState(stateRaw)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after.Rooms[1], initial.Rooms[1]) ||
		!reflect.DeepEqual(after.NPCs, initial.NPCs) ||
		!reflect.DeepEqual(after.ActiveNPCIDs, initial.ActiveNPCIDs) {
		t.Fatalf("player vital tick claimed room/NPC state: room=%+v NPCs=%+v active=%v", after.Rooms[1], after.NPCs, after.ActiveNPCIDs)
	}
}

type retryTickStore struct {
	mu       sync.Mutex
	base     *connectorCommandStore
	failOnce bool
	ids      []string
	requests []json.RawMessage
}

func (s *retryTickStore) ReadWorldReceipt(ctx context.Context, worldID, commandID string, request json.RawMessage) (storage.WorldReceipt, error) {
	return s.base.ReadWorldReceipt(ctx, worldID, commandID, request)
}

func (s *retryTickStore) LoadWorld(ctx context.Context, worldID string) (storage.WorldSnapshot, error) {
	return s.base.LoadWorld(ctx, worldID)
}

func (s *retryTickStore) CommitWorldCommand(ctx context.Context, worldID, commandID string, request json.RawMessage, expected int64, state, response json.RawMessage) (storage.WorldReceipt, error) {
	s.mu.Lock()
	s.ids = append(s.ids, commandID)
	s.requests = append(s.requests, append(json.RawMessage(nil), request...))
	fail := s.failOnce
	s.failOnce = false
	s.mu.Unlock()
	if fail {
		return storage.WorldReceipt{}, errors.New("uncertain commit")
	}
	return s.base.CommitWorldCommand(ctx, worldID, commandID, request, expected, state, response)
}

func TestRunPlayerVitalTickRetriesExactPendingCommand(t *testing.T) {
	base := &connectorCommandStore{state: vitalTickState()}
	store := &retryTickStore{base: base, failOnce: true}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "retry-tick-world", MaxSessions: 1,
		Clock: func() (int32, int) { return 100, 12 },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ran, err := connector.RunPlayerVitalTick(context.Background(), 20*time.Second); err == nil || !ran {
		t.Fatalf("first attempt ran=%v err=%v", ran, err)
	}
	if _, ran, err := connector.RunPlayerVitalTick(context.Background(), 20*time.Second); err != nil || !ran {
		t.Fatalf("retry ran=%v err=%v", ran, err)
	}
	if len(store.ids) != 2 || store.ids[0] != store.ids[1] || store.ids[0] != "player-vitals-5" {
		t.Fatalf("ids=%v", store.ids)
	}
	if len(store.requests) != 2 || string(store.requests[0]) != string(store.requests[1]) {
		t.Fatalf("requests differ: %s / %s", store.requests[0], store.requests[1])
	}
}

type cancelOnCommitStore struct {
	*connectorCommandStore
	cancel context.CancelFunc
}

func (s *cancelOnCommitStore) CommitWorldCommand(ctx context.Context, worldID, commandID string, request json.RawMessage, expected int64, state, response json.RawMessage) (storage.WorldReceipt, error) {
	r, err := s.connectorCommandStore.CommitWorldCommand(ctx, worldID, commandID, request, expected, state, response)
	if err == nil {
		s.cancel()
	}
	return r, err
}

func TestRunPlayerVitalSchedulerStopsAfterContextCancellation(t *testing.T) {
	base := &connectorCommandStore{state: vitalTickState()}
	ctx, cancel := context.WithCancel(context.Background())
	store := &cancelOnCommitStore{connectorCommandStore: base, cancel: cancel}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "scheduler-world", MaxSessions: 1,
		Clock: func() (int32, int) { return 100, 12 },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.RunPlayerVitalScheduler(ctx, time.Hour); err != nil {
		t.Fatal(err)
	}
	if base.commits != 1 || base.command != "player-vitals-0" {
		t.Fatalf("commits=%d command=%q", base.commits, base.command)
	}
}

func TestPlayerVitalIntervalRejectsSubsecondAndZero(t *testing.T) {
	for _, interval := range []time.Duration{0, -time.Second, 500 * time.Millisecond} {
		if _, err := playerVitalSlot(10, interval); err == nil {
			t.Fatalf("interval %s accepted", interval)
		}
	}
	if _, err := playerVitalSlot(10, time.Second); err != nil {
		t.Fatal(err)
	}
}
