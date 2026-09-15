package transport

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/session"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestRunShopPurchaseByNameResolvesCanonicalStockAndReplays(t *testing.T) {
	state := shopPurchaseState(t)
	storage := state.Rooms[11]
	storage.Items.Items["stock-root-2"] = world.Item{
		Object: world.LegacyObject{Name: "검", Value: 60, Weight: 2, Flags: storage.Items.Items["stock-root"].Object.Flags},
	}
	storage.Items.Inventory = append(storage.Items.Inventory, "stock-root-2")
	state.Rooms[11] = storage
	store, _ := shopPurchaseStore(t, state)
	allocated := []string{"owned-root-2"}
	allocations := 0
	connector := newShopPurchaseConnector(t, store, func() (string, error) {
		if allocations >= len(allocated) {
			return "", errors.New("allocator exhausted")
		}
		id := allocated[allocations]
		allocations++
		return id, nil
	})

	first, err := connector.RunShopPurchaseByName(context.Background(), "shop-name-1", "actor-1", "사 검 2")
	if err != nil || first.Replayed || first.Revision != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	var result world.ShopPurchaseResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.StockID != "stock-root-2" || result.ItemName != "검" || result.Price != 60 {
		t.Fatalf("result=%+v", result)
	}
	if allocations != 1 {
		t.Fatalf("allocations=%d want 1", allocations)
	}

	connector.config.Allocate = func() (string, error) {
		t.Fatal("replay invoked allocator")
		return "", nil
	}
	replay, err := connector.RunShopPurchaseByName(context.Background(), "shop-name-1", "actor-1", "사 검 2")
	if err != nil || !replay.Replayed || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}

	if _, err := connector.RunShopPurchaseByName(context.Background(), "shop-name-bad", "actor-1", "사 검 0"); !errors.Is(err, session.ErrUnsupportedShopPurchaseLine) {
		t.Fatalf("invalid occurrence err=%v", err)
	}

	prefixState := shopPurchaseState(t)
	prefixStore, prefixInitial := shopPurchaseStore(t, prefixState)
	prefixAllocations := 0
	prefixConnector := newShopPurchaseConnector(t, prefixStore, func() (string, error) {
		prefixAllocations++
		return "unused", nil
	})
	notSold, err := prefixConnector.RunShopPurchaseByName(context.Background(), "shop-name-prefix", "actor-1", "사 stock-root")
	if err != nil {
		t.Fatal(err)
	}
	var notSoldResult world.ShopPurchaseResult
	if err := json.Unmarshal(notSold.Response, &notSoldResult); err != nil {
		t.Fatal(err)
	}
	stateRaw, _, _, _, _, commits := prefixStore.snapshot()
	if notSoldResult.Response != world.ShopPurchaseNotSoldResponse || prefixAllocations != 0 || commits != 1 || string(stateRaw) != string(prefixInitial) {
		t.Fatalf("client-provided stock ID resolved: result=%+v allocations=%d commits=%d", notSoldResult, prefixAllocations, commits)
	}
}

func TestRunShopPurchaseByNameCParityReceiptsAndReplay(t *testing.T) {
	tests := []struct {
		name   string
		line   string
		mutate func(world.State) world.State
		want   string
		action string
	}{
		{
			name: "not shop",
			line: "사 검",
			mutate: func(s world.State) world.State {
				room := s.Rooms[10]
				room.Resource.Flags = [8]byte{}
				s.Rooms[10] = room
				return s
			},
			want:   world.ShopPurchaseNotShopResponse,
			action: world.ShopPurchaseNotShopAction,
		},
		{
			name:   "bare 사",
			line:   "사",
			want:   world.ShopPurchaseAskWhatResponse,
			action: world.ShopPurchaseAskWhatAction,
		},
		{
			name:   "bare 구입",
			line:   "구입",
			want:   world.ShopPurchaseAskWhatResponse,
			action: world.ShopPurchaseAskWhatAction,
		},
		{
			name: "missing storage",
			line: "사 검",
			mutate: func(s world.State) world.State {
				delete(s.Rooms, 11)
				return s
			},
			want:   world.ShopPurchaseNoStockResponse,
			action: world.ShopPurchaseNoStockAction,
		},
		{
			name:   "name missing",
			line:   "사 방패아님",
			want:   world.ShopPurchaseNotSoldResponse,
			action: world.ShopPurchaseNotSoldAction,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state := shopPurchaseState(t)
			if tc.mutate != nil {
				state = tc.mutate(state)
			}
			store, initialRaw := shopPurchaseStore(t, state)
			var calls int
			connector := newShopPurchaseConnector(t, store, func() (string, error) {
				calls++
				return "unused", nil
			})
			first, err := connector.RunShopPurchaseByName(context.Background(), "shop-name-cprint-"+tc.name, "actor-1", tc.line)
			if err != nil || first.Replayed {
				t.Fatalf("first=%+v err=%v", first, err)
			}
			var result world.ShopPurchaseResult
			if err := json.Unmarshal(first.Response, &result); err != nil {
				t.Fatal(err)
			}
			if result.Action != tc.action || result.Response != tc.want || result.Event != nil {
				t.Fatalf("result=%+v", result)
			}
			stateRaw, _, _, _, _, commits := store.snapshot()
			if string(stateRaw) != string(initialRaw) || commits != 1 || calls != 0 {
				t.Fatalf("cloned or allocated commits=%d calls=%d", commits, calls)
			}
			connector.config.Allocate = func() (string, error) {
				t.Fatal("replay invoked allocator")
				return "", nil
			}
			replay, err := connector.RunShopPurchaseByName(context.Background(), "shop-name-cprint-"+tc.name, "actor-1", tc.line)
			if err != nil || !replay.Replayed || string(replay.Response) != string(first.Response) {
				t.Fatalf("replay=%+v err=%v", replay, err)
			}
			_, _, _, _, _, commits = store.snapshot()
			if commits != 1 {
				t.Fatalf("replay recommitted commits=%d", commits)
			}
		})
	}
}

func TestWorldConnectionSubmitShopPurchaseRequiresAdmissionAndRendersOutput(t *testing.T) {
	store, _ := shopPurchaseStore(t, shopPurchaseState(t))
	allocated := []string{"live-owned-root", "live-owned-child"}
	connector := newShopPurchaseConnector(t, store, func() (string, error) {
		if len(allocated) == 0 {
			return "", errors.New("allocator exhausted")
		}
		id := allocated[0]
		allocated = allocated[1:]
		return id, nil
	})
	lease, err := connector.owners.Acquire("actor-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	connection := &worldConnection{game: connector, lease: lease, ready: true}
	output, err := connection.Submit(context.Background(), "사 검")
	if err != nil {
		t.Fatal(err)
	}
	if output != "당신은 검을(를) 샀습니다.\r\n" {
		t.Fatalf("output=%q", output)
	}

	unauthorizedLease, err := connector.owners.Acquire("unadmitted")
	if err != nil {
		t.Fatal(err)
	}
	unauthorized := &worldConnection{game: connector, lease: unauthorizedLease, ready: true}
	if _, err := unauthorized.Submit(context.Background(), "사 검"); err == nil {
		t.Fatal("unadmitted shop purchase unexpectedly succeeded")
	}
}
