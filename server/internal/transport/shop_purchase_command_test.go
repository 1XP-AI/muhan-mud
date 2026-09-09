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

	if _, err := connector.RunShopPurchaseByName(context.Background(), "shop-name-prefix", "actor-1", "사 stock-root"); err == nil {
		t.Fatal("client-provided stock ID unexpectedly resolved")
	}
	if _, err := connector.RunShopPurchaseByName(context.Background(), "shop-name-bad", "actor-1", "사 검 0"); !errors.Is(err, session.ErrUnsupportedShopPurchaseLine) {
		t.Fatalf("invalid occurrence err=%v", err)
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
