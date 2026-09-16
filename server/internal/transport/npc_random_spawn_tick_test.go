package transport

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

type npcRandomSpawnTickCatalog struct {
	mu           sync.Mutex
	monster      world.LegacyMonster
	monsterCalls int
	objectCalls  int
}

func (c *npcRandomSpawnTickCatalog) Monster(int16) (world.LegacyMonster, error) {
	c.mu.Lock()
	c.monsterCalls++
	c.mu.Unlock()
	return c.monster, nil
}

func (c *npcRandomSpawnTickCatalog) Object(int16) (world.LegacyObject, error) {
	c.mu.Lock()
	c.objectCalls++
	c.mu.Unlock()
	return world.LegacyObject{}, errors.New("unexpected NPC random object lookup")
}

func (c *npcRandomSpawnTickCatalog) calls() (int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.monsterCalls, c.objectCalls
}

func npcRandomSpawnTickCatalogFixture() *npcRandomSpawnTickCatalog {
	return &npcRandomSpawnTickCatalog{monster: world.LegacyMonster{Name: "늑대", Type: 1}}
}

func npcRandomSpawnTickState(t *testing.T, roomIDs ...int16) json.RawMessage {
	t.Helper()
	state := world.State{
		Version:      1,
		Rooms:        make(map[int16]world.RoomState, len(roomIDs)),
		Players:      make(map[string]world.PlayerState, len(roomIDs)),
		NPCs:         map[string]world.NPCState{},
		ActiveNPCIDs: []string{},
	}
	for _, roomID := range roomIDs {
		playerID := "player-" + string(rune('0'+roomID))
		state.Rooms[roomID] = world.RoomState{
			Resource: world.LegacyRoom{
				LegacyRoomHeader: world.LegacyRoomHeader{ID: roomID},
				Traffic:          100,
				Random:           [10]int16{1},
			},
			Items:     &world.ItemCollection{Items: map[string]world.Item{}},
			PlayerIDs: []string{playerID},
		}
		state.Players[playerID] = world.PlayerState{
			Body:   world.LegacyMonster{Name: playerID, Type: 0, RoomID: roomID},
			Online: true,
			Items:  &world.ItemCollection{Items: map[string]world.Item{}},
		}
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

func npcRandomSpawnTickRoll(low, _ int) int { return low }

type npcRandomSpawnRequestStore struct {
	mu             sync.Mutex
	state          json.RawMessage
	receipt        *storage.WorldReceipt
	command        string
	request        json.RawMessage
	commits        int
	readRequests   []json.RawMessage
	commitRequests []json.RawMessage
}

func (s *npcRandomSpawnRequestStore) ReadWorldReceipt(_ context.Context, _ string, command string, request json.RawMessage) (storage.WorldReceipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.readRequests = append(s.readRequests, append(json.RawMessage(nil), request...))
	if s.receipt == nil || s.command != command {
		return storage.WorldReceipt{}, sql.ErrNoRows
	}
	if !bytes.Equal(s.request, request) {
		return storage.WorldReceipt{}, storage.ErrCommandConflict
	}
	receipt := *s.receipt
	receipt.Replayed = true
	return receipt, nil
}

func (s *npcRandomSpawnRequestStore) LoadWorld(context.Context, string) (storage.WorldSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return storage.WorldSnapshot{State: append(json.RawMessage(nil), s.state...)}, nil
}

func (s *npcRandomSpawnRequestStore) CommitWorldCommand(_ context.Context, _ string, command string, request json.RawMessage, revision int64, state, response json.RawMessage) (storage.WorldReceipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commitRequests = append(s.commitRequests, append(json.RawMessage(nil), request...))
	if s.receipt != nil && s.command == command {
		if !bytes.Equal(s.request, request) {
			return storage.WorldReceipt{}, storage.ErrCommandConflict
		}
		receipt := *s.receipt
		receipt.Replayed = true
		return receipt, nil
	}
	s.commits++
	s.state = append(json.RawMessage(nil), state...)
	s.command = command
	s.request = append(json.RawMessage(nil), request...)
	receipt := storage.WorldReceipt{Revision: revision + 1, Response: append(json.RawMessage(nil), response...)}
	s.receipt = &receipt
	return receipt, nil
}

func TestRunNPCRandomSpawnTickPersistsOrderedSummaryAndSuppressesNotDueSlot(t *testing.T) {
	base := &connectorCommandStore{state: npcRandomSpawnTickState(t, 1)}
	store := &retryTickStore{base: base}
	catalog := npcRandomSpawnTickCatalogFixture()
	allocateCalls := 0
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "npc-random-spawn-due", MaxSessions: 1,
		Clock: func() (int32, int) { return 103, 12 }, Catalog: catalog,
		Roll: npcRandomSpawnTickRoll,
		Allocate: func() (string, error) {
			allocateCalls++
			return "random-npc", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	first, ran, err := connector.RunNPCRandomSpawnTick(context.Background(), 20*time.Second)
	if err != nil || !ran || first.Replayed || base.commits != 1 {
		t.Fatalf("first=%+v ran=%v commits=%d err=%v", first, ran, base.commits, err)
	}
	if base.command != "npc-random-spawn-5" {
		t.Fatalf("command=%q", base.command)
	}
	var request npcRandomSpawnTickRequest
	if err := json.Unmarshal(store.requests[0], &request); err != nil {
		t.Fatal(err)
	}
	if request.Kind != "npc-random-spawn-phase" || request.Slot != 5 || request.Now != 100 || request.PlyOrder != nil {
		t.Fatalf("request=%+v", request)
	}
	var summary npcRandomSpawnPhaseSummary
	if err := json.Unmarshal(first.Response, &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Now != 100 || !reflect.DeepEqual(summary.SpawnedRooms, []int16{1}) || !reflect.DeepEqual(summary.SpawnedNPCIDs, []string{"random-npc"}) {
		t.Fatalf("summary=%+v", summary)
	}
	if allocateCalls != 1 {
		t.Fatalf("allocator calls=%d", allocateCalls)
	}
	monsterCalls, objectCalls := catalog.calls()
	if monsterCalls != 1 || objectCalls != 0 {
		t.Fatalf("catalog calls monster=%d object=%d", monsterCalls, objectCalls)
	}

	duplicate, ran, err := connector.RunNPCRandomSpawnTick(context.Background(), 20*time.Second)
	if err != nil || ran || duplicate.Response != nil || base.commits != 1 || allocateCalls != 1 {
		t.Fatalf("not-due duplicate=%+v ran=%v commits=%d alloc=%d err=%v", duplicate, ran, base.commits, allocateCalls, err)
	}
}

func TestRunNPCRandomSpawnTickReplaysWithoutRerollOrReallocation(t *testing.T) {
	base := &connectorCommandStore{state: npcRandomSpawnTickState(t, 1)}
	store := &retryTickStore{base: base}
	catalog := npcRandomSpawnTickCatalogFixture()
	rollCalls := 0
	allocateCalls := 0
	config := WorldConnectorConfig{
		Store: store, WorldID: "npc-random-spawn-replay", MaxSessions: 1,
		Clock: func() (int32, int) { return 103, 12 }, Catalog: catalog,
		Roll: func(low, high int) int {
			rollCalls++
			return npcRandomSpawnTickRoll(low, high)
		},
		Allocate: func() (string, error) {
			allocateCalls++
			return "replay-npc", nil
		},
	}
	firstConnector, err := NewWorldConnector(config)
	if err != nil {
		t.Fatal(err)
	}
	first, ran, err := firstConnector.RunNPCRandomSpawnTick(context.Background(), 20*time.Second)
	if err != nil || !ran || first.Replayed {
		t.Fatalf("first=%+v ran=%v err=%v", first, ran, err)
	}
	if rollCalls != 4 || allocateCalls != 1 {
		t.Fatalf("first dependencies rolls=%d allocations=%d", rollCalls, allocateCalls)
	}

	secondConnector, err := NewWorldConnector(config)
	if err != nil {
		t.Fatal(err)
	}
	replay, ran, err := secondConnector.RunNPCRandomSpawnTick(context.Background(), 20*time.Second)
	if err != nil || !ran || !replay.Replayed || base.commits != 1 || rollCalls != 4 || allocateCalls != 1 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v ran=%v commits=%d rolls=%d allocations=%d err=%v", replay, ran, base.commits, rollCalls, allocateCalls, err)
	}
}

func TestRunNPCRandomSpawnTickRetainsExactPendingRequestAndPlyOrder(t *testing.T) {
	base := &connectorCommandStore{state: npcRandomSpawnTickState(t, 1, 2)}
	store := &retryTickStore{base: base, failOnce: true}
	clockNow := int32(103)
	clockCalls := 0
	allocateCalls := 0
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "npc-random-spawn-retry", MaxSessions: 1,
		Clock:   func() (int32, int) { clockCalls++; return clockNow, 12 },
		Catalog: npcRandomSpawnTickCatalogFixture(), Roll: npcRandomSpawnTickRoll,
		Allocate: func() (string, error) {
			ids := []string{"retry-npc-1", "retry-npc-2"}
			id := ids[allocateCalls%len(ids)]
			allocateCalls++
			return id, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	firstOrder := []string{"player-2", "player-1"}
	if _, ran, err := connector.RunNPCRandomSpawnTick(context.Background(), 20*time.Second, firstOrder); err == nil || !ran {
		t.Fatalf("uncertain first ran=%v err=%v", ran, err)
	}
	clockNow = 119
	retryOrder := []string{"player-1", "player-2"}
	retry, ran, err := connector.RunNPCRandomSpawnTick(context.Background(), 20*time.Second, retryOrder)
	if err != nil || !ran || retry.Replayed || clockCalls != 1 || base.commits != 1 {
		t.Fatalf("retry=%+v ran=%v clock-calls=%d commits=%d err=%v", retry, ran, clockCalls, base.commits, err)
	}
	if len(store.ids) != 2 || store.ids[0] != "npc-random-spawn-5" || store.ids[1] != store.ids[0] || len(store.requests) != 2 || !bytes.Equal(store.requests[0], store.requests[1]) {
		t.Fatalf("retry IDs=%v requests=%q", store.ids, store.requests)
	}
	var request npcRandomSpawnTickRequest
	if err := json.Unmarshal(store.requests[0], &request); err != nil {
		t.Fatal(err)
	}
	if request.Slot != 5 || request.Now != 100 || !reflect.DeepEqual(request.PlyOrder, firstOrder) {
		t.Fatalf("retry request=%+v", request)
	}
	if allocateCalls != 4 {
		t.Fatalf("allocator calls=%d", allocateCalls)
	}
}

func TestRunNPCRandomSpawnTickRequiresExplicitMultiRoomOrder(t *testing.T) {
	store := &connectorCommandStore{state: npcRandomSpawnTickState(t, 1, 2)}
	rollCalls := 0
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "npc-random-spawn-order", MaxSessions: 1,
		Clock: func() (int32, int) { return 103, 12 }, Catalog: npcRandomSpawnTickCatalogFixture(),
		Roll:     func(low, high int) int { rollCalls++; return npcRandomSpawnTickRoll(low, high) },
		Allocate: func() (string, error) { return "unexpected", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ran, err := connector.RunNPCRandomSpawnTick(context.Background(), 20*time.Second); err == nil || !ran {
		t.Fatalf("missing order ran=%v err=%v", ran, err)
	}
	if store.commits != 0 || rollCalls != 0 || store.receipt != nil {
		t.Fatalf("missing order committed=%d rolls=%d receipt=%+v", store.commits, rollCalls, store.receipt)
	}
}

func TestRunNPCRandomSpawnTickRetriesCorrectedOrderAfterReducerValidationError(t *testing.T) {
	store := &npcRandomSpawnRequestStore{state: npcRandomSpawnTickState(t, 1, 2)}
	rollCalls := 0
	allocateCalls := 0
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "npc-random-spawn-order-retry", MaxSessions: 1,
		Clock: func() (int32, int) { return 103, 12 }, Catalog: npcRandomSpawnTickCatalogFixture(),
		Roll: func(low, high int) int {
			rollCalls++
			return npcRandomSpawnTickRoll(low, high)
		},
		Allocate: func() (string, error) {
			allocateCalls++
			return fmt.Sprintf("retry-order-npc-%d", allocateCalls), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ran, err := connector.RunNPCRandomSpawnTick(context.Background(), 20*time.Second); err == nil || !ran {
		t.Fatalf("missing order ran=%v err=%v", ran, err)
	}
	if store.commits != 0 || rollCalls != 0 || allocateCalls != 0 || connector.pendingNPCRandomSpawn != nil || connector.lastNPCRandomSpawnSlot != -1 {
		t.Fatalf("missing order claimed slot: commits=%d rolls=%d allocations=%d pending=%+v last=%d", store.commits, rollCalls, allocateCalls, connector.pendingNPCRandomSpawn, connector.lastNPCRandomSpawnSlot)
	}

	order := []string{"player-2", "player-1"}
	retry, ran, err := connector.RunNPCRandomSpawnTick(context.Background(), 20*time.Second, order)
	if err != nil || !ran || retry.Replayed || store.commits != 1 {
		t.Fatalf("corrected order retry=%+v ran=%v commits=%d err=%v", retry, ran, store.commits, err)
	}
	var request npcRandomSpawnTickRequest
	if err := json.Unmarshal(store.request, &request); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(request.PlyOrder, order) || request.Slot != 5 || request.Now != 100 {
		t.Fatalf("corrected order request=%+v", request)
	}
}

func TestRunNPCRandomSpawnTickRejectsExplicitOrderForEmptySnapshot(t *testing.T) {
	store := &connectorCommandStore{state: npcRandomSpawnTickState(t)}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "npc-random-spawn-empty-order", MaxSessions: 1,
		Clock: func() (int32, int) { return 103, 12 },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ran, err := connector.RunNPCRandomSpawnTick(context.Background(), 20*time.Second, []string{"ghost"}); err == nil || !ran {
		t.Fatalf("explicit order on empty snapshot ran=%v err=%v", ran, err)
	}
	if store.commits != 0 || connector.pendingNPCRandomSpawn != nil || connector.lastNPCRandomSpawnSlot != -1 {
		t.Fatalf("explicit order on empty snapshot claimed slot: commits=%d pending=%+v last=%d", store.commits, connector.pendingNPCRandomSpawn, connector.lastNPCRandomSpawnSlot)
	}
	if _, ran, err := connector.RunNPCRandomSpawnTick(context.Background(), 20*time.Second); err != nil || !ran || store.commits != 1 {
		t.Fatalf("empty snapshot retry without order ran=%v commits=%d err=%v", ran, store.commits, err)
	}
}

func TestRunNPCRandomSpawnTickUsesExplicitMultiRoomOrderAndNoFanout(t *testing.T) {
	store := &connectorCommandStore{state: npcRandomSpawnTickState(t, 1, 2)}
	allocateCalls := 0
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "npc-random-spawn-order-success", MaxSessions: 1,
		Clock: func() (int32, int) { return 103, 12 }, Catalog: npcRandomSpawnTickCatalogFixture(),
		Roll: npcRandomSpawnTickRoll,
		Allocate: func() (string, error) {
			allocateCalls++
			return []string{"random-1", "random-2"}[allocateCalls-1], nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	connections := installNPCAITickConnections(t, connector, "player-1")
	first, ran, err := connector.RunNPCRandomSpawnTick(context.Background(), 20*time.Second, []string{"player-2", "player-1"})
	if err != nil || !ran || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v ran=%v commits=%d err=%v", first, ran, store.commits, err)
	}
	var summary npcRandomSpawnPhaseSummary
	if err := json.Unmarshal(first.Response, &summary); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(summary.SpawnedRooms, []int16{2, 1}) || !reflect.DeepEqual(summary.SpawnedNPCIDs, []string{"random-1", "random-2"}) {
		t.Fatalf("ordered summary=%+v", summary)
	}

	select {
	case got := <-connections["player-1"].events:
		t.Fatalf("random spawn fanout delivered=%q", got)
	default:
	}
}

func TestRunNPCRandomSpawnTickBindsExactOrderAcrossRestart(t *testing.T) {
	store := &npcRandomSpawnRequestStore{state: npcRandomSpawnTickState(t, 1, 2)}
	config := WorldConnectorConfig{
		Store: store, WorldID: "npc-random-spawn-request-binding", MaxSessions: 1,
		Clock: func() (int32, int) { return 103, 12 }, Catalog: npcRandomSpawnTickCatalogFixture(),
		Roll: npcRandomSpawnTickRoll,
		Allocate: func() (string, error) {
			return "request-binding-npc", nil
		},
	}
	order := []string{"player-2", "player-1"}
	allocateCalls := 0
	config.Allocate = func() (string, error) {
		allocateCalls++
		return fmt.Sprintf("request-binding-npc-%d", allocateCalls), nil
	}
	firstConnector, err := NewWorldConnector(config)
	if err != nil {
		t.Fatal(err)
	}
	first, ran, err := firstConnector.RunNPCRandomSpawnTick(context.Background(), 20*time.Second, order)
	if err != nil || !ran || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v ran=%v commits=%d err=%v", first, ran, store.commits, err)
	}
	if len(store.readRequests) != 1 || len(store.commitRequests) != 1 || !bytes.Equal(store.readRequests[0], store.commitRequests[0]) {
		t.Fatalf("first request binding read=%q commit=%q", store.readRequests, store.commitRequests)
	}

	restarted, err := NewWorldConnector(config)
	if err != nil {
		t.Fatal(err)
	}
	replay, ran, err := restarted.RunNPCRandomSpawnTick(context.Background(), 20*time.Second, order)
	if err != nil || !ran || !replay.Replayed || store.commits != 1 {
		t.Fatalf("same-order restart replay=%+v ran=%v commits=%d err=%v", replay, ran, store.commits, err)
	}
	if len(store.readRequests) != 2 || !bytes.Equal(store.readRequests[0], store.readRequests[1]) {
		t.Fatalf("same-order restart changed request: %q", store.readRequests)
	}

	conflictConnector, err := NewWorldConnector(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, ran, err := conflictConnector.RunNPCRandomSpawnTick(context.Background(), 20*time.Second, []string{"player-1", "player-2"}); !errors.Is(err, storage.ErrCommandConflict) || !ran {
		t.Fatalf("different-order replay ran=%v err=%v", ran, err)
	}
	if len(store.readRequests) != 3 || bytes.Equal(store.readRequests[0], store.readRequests[2]) || store.commits != 1 {
		t.Fatalf("different-order request binding reads=%q commits=%d", store.readRequests, store.commits)
	}
}

func TestRunNPCRandomSpawnTickProducerErrorDoesNotCommit(t *testing.T) {
	before := npcRandomSpawnTickState(t, 1)
	store := &connectorCommandStore{state: before}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "npc-random-spawn-error", MaxSessions: 1,
		Clock: func() (int32, int) { return 103, 12 }, Catalog: npcRandomSpawnTickCatalogFixture(),
		Roll:     npcRandomSpawnTickRoll,
		Allocate: func() (string, error) { return "", errors.New("allocator unavailable") },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ran, err := connector.RunNPCRandomSpawnTick(context.Background(), 20*time.Second); err == nil || !ran {
		t.Fatalf("producer error ran=%v err=%v", ran, err)
	}
	after, _ := store.snapshot()
	if store.commits != 0 || store.receipt != nil || !bytes.Equal(after, before) {
		t.Fatalf("producer error committed=%d receipt=%+v state changed=%v", store.commits, store.receipt, !bytes.Equal(after, before))
	}
}

func TestRunNPCRandomSpawnTickRejectsExtraOrderArguments(t *testing.T) {
	store := &connectorCommandStore{state: npcRandomSpawnTickState(t, 1)}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "npc-random-spawn-args", MaxSessions: 1,
		Clock: func() (int32, int) { return 103, 12 },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ran, err := connector.RunNPCRandomSpawnTick(context.Background(), 20*time.Second, []string{"player-1"}, []string{"player-1"}); err == nil || ran {
		t.Fatalf("extra order args ran=%v err=%v", ran, err)
	}
	if store.commits != 0 {
		t.Fatalf("extra order args committed=%d", store.commits)
	}
}
