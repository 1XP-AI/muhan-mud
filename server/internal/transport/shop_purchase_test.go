package transport

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

type shopPurchaseCommandStore struct {
	mu sync.Mutex

	state    json.RawMessage
	revision int64
	receipt  *storage.WorldReceipt
	command  string
	request  json.RawMessage

	reads          int
	loads          int
	commitAttempts int
	commits        int
}

func (s *shopPurchaseCommandStore) ReadWorldReceipt(_ context.Context, _ string, commandID string, request json.RawMessage) (storage.WorldReceipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads++
	if s.receipt == nil || commandID != s.command {
		return storage.WorldReceipt{}, sql.ErrNoRows
	}
	if !bytes.Equal(request, s.request) {
		return storage.WorldReceipt{}, storage.ErrCommandConflict
	}
	r := *s.receipt
	r.Response = append(json.RawMessage(nil), r.Response...)
	r.Replayed = true
	return r, nil
}

func (s *shopPurchaseCommandStore) LoadWorld(_ context.Context, _ string) (storage.WorldSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loads++
	return storage.WorldSnapshot{
		Revision: s.revision,
		State:    append(json.RawMessage(nil), s.state...),
	}, nil
}

func (s *shopPurchaseCommandStore) CommitWorldCommand(_ context.Context, _ string, commandID string, request json.RawMessage, expected int64, state, response json.RawMessage) (storage.WorldReceipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commitAttempts++
	if expected != s.revision {
		return storage.WorldReceipt{}, storage.ErrWorldConflict
	}
	if s.receipt != nil {
		if commandID != s.command || !bytes.Equal(request, s.request) {
			return storage.WorldReceipt{}, storage.ErrCommandConflict
		}
		r := *s.receipt
		r.Response = append(json.RawMessage(nil), r.Response...)
		r.Replayed = true
		return r, nil
	}
	s.state = append(json.RawMessage(nil), state...)
	s.request = append(json.RawMessage(nil), request...)
	s.command = commandID
	s.revision++
	r := storage.WorldReceipt{Revision: s.revision, Response: append(json.RawMessage(nil), response...)}
	s.receipt = &r
	s.commits++
	return r, nil
}

func (s *shopPurchaseCommandStore) snapshot() (state json.RawMessage, request json.RawMessage, reads, loads, commitAttempts, commits int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append(json.RawMessage(nil), s.state...), append(json.RawMessage(nil), s.request...), s.reads, s.loads, s.commitAttempts, s.commits
}

func shopPurchaseState(t *testing.T) world.State {
	t.Helper()
	var shopFlags, storageFlags, permanence [8]byte
	shopFlags[world.RoomShopFlag/8] |= 1 << (world.RoomShopFlag % 8)
	// world.shopStorage's RNOTEL is the zero-based flag 12.
	storageFlags[12/8] |= 1 << (12 % 8)
	permanence[0] |= 1 << 0
	permanence[1] |= 1 << 0
	permanence[1] |= 1 << 1

	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			10: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 10, Name: "상점", Flags: shopFlags}},
				PlayerIDs: []string{"actor-1"},
			},
			11: {
				Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 11, Name: "상점 저장고", Flags: storageFlags}},
				Items: &world.ItemCollection{
					Items: map[string]world.Item{
						"stock-root": {
							Object:   world.LegacyObject{Name: "검", Value: 40, Weight: 3, Flags: permanence},
							Contents: []string{"stock-child"},
						},
						"stock-child": {Object: world.LegacyObject{Name: "보석", Weight: 2, Flags: permanence}},
						"stock-other": {Object: world.LegacyObject{Name: "방패", Value: 70, Weight: 1}},
					},
					Inventory: []string{"stock-root", "stock-other"},
				},
			},
		},
		Players: map[string]world.PlayerState{
			"actor-1": {
				Body:   world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 10, Gold: 100, Stats: [5]byte{10}},
				Online: true,
				Items: &world.ItemCollection{
					Items:     map[string]world.Item{"carried": {Object: world.LegacyObject{Name: "낡은 물건", Weight: 1}}},
					Inventory: []string{"carried"},
				},
			},
		},
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	return s
}

func shopPurchaseStore(t *testing.T, state world.State) (*shopPurchaseCommandStore, json.RawMessage) {
	t.Helper()
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return &shopPurchaseCommandStore{state: append(json.RawMessage(nil), raw...)}, raw
}

func newShopPurchaseConnector(t *testing.T, store *shopPurchaseCommandStore, allocate func() (string, error)) *WorldConnector {
	t.Helper()
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store:       store,
		WorldID:     "shop-world",
		Clock:       func() (int32, int) { return 100, 12 },
		Allocate:    allocate,
		MaxSessions: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return connector
}

func TestRunShopPurchaseCommitsCanonicalRequestAndReceipt(t *testing.T) {
	store, initialRaw := shopPurchaseStore(t, shopPurchaseState(t))
	var allocations atomic.Int32
	ids := []string{"owned-root", "owned-child"}
	connector := newShopPurchaseConnector(t, store, func() (string, error) {
		index := int(allocations.Add(1)) - 1
		if index >= len(ids) {
			return "", errors.New("allocator exhausted")
		}
		return ids[index], nil
	})

	first, err := connector.RunShopPurchase(context.Background(), "shop-command-1", "actor-1", "stock-root")
	if err != nil {
		t.Fatal(err)
	}
	if first.Replayed || first.Revision != 1 {
		t.Fatalf("first receipt=%+v", first)
	}
	stateRaw, requestRaw, _, loads, commitAttempts, commits := store.snapshot()
	wantRequest := []byte(`{"actor_id":"actor-1","stock_id":"stock-root"}`)
	if !bytes.Equal(requestRaw, wantRequest) {
		t.Fatalf("request=%s want=%s", requestRaw, wantRequest)
	}
	if loads != 1 || commitAttempts != 1 || commits != 1 || allocations.Load() != 2 {
		t.Fatalf("loads=%d commit_attempts=%d commits=%d allocations=%d", loads, commitAttempts, commits, allocations.Load())
	}
	if bytes.Equal(stateRaw, initialRaw) {
		t.Fatal("successful purchase did not change state")
	}

	saved, err := world.DecodeState(stateRaw)
	if err != nil {
		t.Fatal(err)
	}
	player := saved.Players["actor-1"]
	if player.Body.Gold != 60 || len(player.Items.Inventory) != 2 || player.Items.Items["owned-root"].Contents[0] != "owned-child" {
		t.Fatalf("saved player=%+v", player)
	}
	var result world.ShopPurchaseResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != "buy-shop-item" || result.StockID != "stock-root" || result.Price != 40 || result.GoldBefore != 100 || result.GoldAfter != 60 {
		t.Fatalf("result=%+v", result)
	}

	// A durable receipt is authoritative. Replacing the allocator with a
	// failing function proves replay does not re-enter BuyShopItem.
	connector.config.Allocate = func() (string, error) {
		t.Fatal("replay invoked shop allocator")
		return "", nil
	}
	replay, err := connector.RunShopPurchase(context.Background(), "shop-command-1", "actor-1", "stock-root")
	if err != nil || !replay.Replayed || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	stateRaw, _, _, loads, commitAttempts, commits = store.snapshot()
	if loads != 1 || commitAttempts != 1 || commits != 1 || allocations.Load() != 2 {
		t.Fatalf("replay reran transition: loads=%d commit_attempts=%d commits=%d allocations=%d", loads, commitAttempts, commits, allocations.Load())
	}
	if !json.Valid(stateRaw) {
		t.Fatal("stored state is not valid JSON")
	}
}

func TestRunShopPurchaseFailuresAreAtomicAndFailClosed(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(world.State) world.State
		stockID   string
		allocate  func(*atomic.Int32) func() (string, error)
		wantCalls int32
	}{
		{
			name:    "nil allocator",
			stockID: "stock-root",
			allocate: func(*atomic.Int32) func() (string, error) {
				return nil
			},
		},
		{
			name:    "duplicate allocator ID",
			stockID: "stock-root",
			allocate: func(calls *atomic.Int32) func() (string, error) {
				return func() (string, error) {
					calls.Add(1)
					return "carried", nil
				}
			},
			wantCalls: 1,
		},
		{
			name:    "insufficient gold",
			stockID: "stock-root",
			mutate: func(s world.State) world.State {
				p := s.Players["actor-1"]
				p.Body.Gold = 39
				s.Players["actor-1"] = p
				return s
			},
			allocate: func(calls *atomic.Int32) func() (string, error) {
				return func() (string, error) {
					calls.Add(1)
					return "unused", nil
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state := shopPurchaseState(t)
			if tc.mutate != nil {
				state = tc.mutate(state)
			}
			store, initialRaw := shopPurchaseStore(t, state)
			var calls atomic.Int32
			connector := newShopPurchaseConnector(t, store, tc.allocate(&calls))
			if _, err := connector.RunShopPurchase(context.Background(), "shop-failure-"+tc.name, "actor-1", tc.stockID); err == nil {
				t.Fatal("failure case unexpectedly committed")
			}
			stateRaw, _, _, loads, commitAttempts, commits := store.snapshot()
			if !bytes.Equal(stateRaw, initialRaw) || loads != 1 || commitAttempts != 0 || commits != 0 {
				t.Fatalf("non-atomic failure state=%s loads=%d commit_attempts=%d commits=%d", stateRaw, loads, commitAttempts, commits)
			}
			if calls.Load() != tc.wantCalls {
				t.Fatalf("allocator calls=%d want=%d", calls.Load(), tc.wantCalls)
			}
		})
	}
}

func TestRunShopPurchaseConcurrentSameCommandReplaysWithoutSecondAllocation(t *testing.T) {
	store, _ := shopPurchaseStore(t, shopPurchaseState(t))
	var allocations atomic.Int32
	connector := newShopPurchaseConnector(t, store, func() (string, error) {
		if allocations.Add(1) == 1 {
			return "owned-root", nil
		}
		return "unexpected-second-allocation", nil
	})

	results := make(chan storage.WorldReceipt, 2)
	errorsOut := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			receipt, err := connector.RunShopPurchase(context.Background(), "shop-concurrent", "actor-1", "stock-other")
			results <- receipt
			errorsOut <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errorsOut)

	var replayed int
	for err := range errorsOut {
		if err != nil {
			t.Fatal(err)
		}
	}
	for receipt := range results {
		if receipt.Replayed {
			replayed++
		}
	}
	if replayed != 1 || allocations.Load() != 1 {
		t.Fatalf("receipts replayed=%d allocations=%d", replayed, allocations.Load())
	}
	_, _, _, loads, commitAttempts, commits := store.snapshot()
	if loads != 1 || commitAttempts != 1 || commits != 1 {
		t.Fatalf("store calls loads=%d commit_attempts=%d commits=%d", loads, commitAttempts, commits)
	}
}

func TestRunShopPurchaseBindsCommandIDToCanonicalActorAndStockRequest(t *testing.T) {
	store, _ := shopPurchaseStore(t, shopPurchaseState(t))
	connector := newShopPurchaseConnector(t, store, func() (string, error) { return "owned-root", nil })
	if _, err := connector.RunShopPurchase(context.Background(), "shop-bound", "actor-1", "stock-other"); err != nil {
		t.Fatal(err)
	}
	if _, err := connector.RunShopPurchase(context.Background(), "shop-bound", "actor-1", "stock-root"); !errors.Is(err, storage.ErrCommandConflict) {
		t.Fatalf("request reuse was not rejected: %v", err)
	}
	_, request, _, _, _, _ := store.snapshot()
	want := []byte(`{"actor_id":"actor-1","stock_id":"stock-other"}`)
	if !reflect.DeepEqual(request, json.RawMessage(want)) {
		t.Fatalf("stored request=%s want=%s", request, want)
	}
}

func TestRunShopPurchaseCParityReceiptsDoNotCloneOrAllocate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(world.State) world.State
		stockID string
		want    string
		action  string
	}{
		{
			name: "not shop",
			mutate: func(s world.State) world.State {
				room := s.Rooms[10]
				room.Resource.Flags = [8]byte{}
				room.Resource.Flags[world.RoomPawnFlag/8] |= 1 << (world.RoomPawnFlag % 8)
				s.Rooms[10] = room
				return s
			},
			stockID: "stock-root",
			want:    world.ShopPurchaseNotShopResponse,
			action:  world.ShopPurchaseNotShopAction,
		},
		{
			name: "missing storage",
			mutate: func(s world.State) world.State {
				delete(s.Rooms, 11)
				return s
			},
			stockID: "stock-root",
			want:    world.ShopPurchaseNoStockResponse,
			action:  world.ShopPurchaseNoStockAction,
		},
		{
			name:    "stock missing",
			stockID: "missing-stock",
			want:    world.ShopPurchaseNotSoldResponse,
			action:  world.ShopPurchaseNotSoldAction,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state := shopPurchaseState(t)
			if tc.mutate != nil {
				state = tc.mutate(state)
			}
			store, initialRaw := shopPurchaseStore(t, state)
			var calls atomic.Int32
			connector := newShopPurchaseConnector(t, store, func() (string, error) {
				calls.Add(1)
				return "unused", nil
			})
			first, err := connector.RunShopPurchase(context.Background(), "shop-cprint-"+tc.name, "actor-1", tc.stockID)
			if err != nil || first.Replayed {
				t.Fatalf("receipt=%+v err=%v", first, err)
			}
			var result world.ShopPurchaseResult
			if err := json.Unmarshal(first.Response, &result); err != nil {
				t.Fatal(err)
			}
			if result.Action != tc.action || result.Response != tc.want || result.Event != nil {
				t.Fatalf("result=%+v", result)
			}
			stateRaw, _, _, _, commitAttempts, commits := store.snapshot()
			if !bytes.Equal(stateRaw, initialRaw) || commitAttempts != 1 || commits != 1 || calls.Load() != 0 {
				t.Fatalf("cloned or allocated: commits=%d attempts=%d calls=%d", commits, commitAttempts, calls.Load())
			}
			connector.config.Allocate = func() (string, error) {
				t.Fatal("replay invoked shop allocator")
				return "", nil
			}
			replay, err := connector.RunShopPurchase(context.Background(), "shop-cprint-"+tc.name, "actor-1", tc.stockID)
			if err != nil || !replay.Replayed || string(replay.Response) != string(first.Response) {
				t.Fatalf("replay=%+v err=%v", replay, err)
			}
			_, _, _, _, commitAttempts, commits = store.snapshot()
			if commitAttempts != 1 || commits != 1 {
				t.Fatalf("replay recommitted attempts=%d commits=%d", commitAttempts, commits)
			}
		})
	}
}

func TestRunShopPurchaseUnmigratedStorageFailClosed(t *testing.T) {
	state := shopPurchaseState(t)
	room := state.Rooms[11]
	room.Items = nil
	state.Rooms[11] = room
	store, initialRaw := shopPurchaseStore(t, state)
	connector := newShopPurchaseConnector(t, store, func() (string, error) {
		t.Fatal("allocator called for unmigrated storage")
		return "", nil
	})
	if _, err := connector.RunShopPurchase(context.Background(), "shop-unmigrated", "actor-1", "stock-root"); err == nil {
		t.Fatal("unmigrated storage unexpectedly purchased")
	}
	stateRaw, _, _, _, commitAttempts, commits := store.snapshot()
	if !bytes.Equal(stateRaw, initialRaw) || commitAttempts != 0 || commits != 0 {
		t.Fatalf("fail-closed storage committed attempts=%d commits=%d", commitAttempts, commits)
	}
}
