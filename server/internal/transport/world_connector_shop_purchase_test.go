package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func admitShopPurchaseConnection(t *testing.T, connector *WorldConnector) *worldConnection {
	t.Helper()
	lease, err := connector.owners.Acquire("actor-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	return &worldConnection{game: connector, lease: lease, ready: true}
}

func TestWorldConnectorSubmitDispatchesShopPurchaseByCanonicalNameAndAlias(t *testing.T) {
	state := shopPurchaseState(t)
	player := state.Players["actor-1"]
	player.Body.Gold = 200
	state.Players["actor-1"] = player
	store, _ := shopPurchaseStore(t, state)
	connector := newShopPurchaseConnector(t, store, func() (string, error) { return "owned-shield", nil })
	connection := admitShopPurchaseConnection(t, connector)

	text, err := connection.Submit(context.Background(), "구입 방패")
	if err != nil || !strings.Contains(text, "방패") {
		t.Fatalf("purchase output=%q err=%v", text, err)
	}
	savedRaw, _, _, _, _, commits := store.snapshot()
	saved, err := world.DecodeState(savedRaw)
	if err != nil {
		t.Fatal(err)
	}
	if commits != 1 || saved.Players["actor-1"].Body.Gold != 130 || len(saved.Players["actor-1"].Items.Inventory) != 2 {
		t.Fatalf("commits=%d saved player=%+v", commits, saved.Players["actor-1"])
	}

	// Prefix-like input is a different exact name and must fail before any
	// durable command commit; the live adapter does not infer a stock ID.
	secondState := shopPurchaseState(t)
	secondStore, _ := shopPurchaseStore(t, secondState)
	secondConnector := newShopPurchaseConnector(t, secondStore, func() (string, error) { return "unused", nil })
	secondConnection := admitShopPurchaseConnection(t, secondConnector)
	if _, err := secondConnection.Submit(context.Background(), "사 방"); err == nil {
		t.Fatal("prefix-like stock name unexpectedly succeeded")
	}
	_, _, _, _, _, commits = secondStore.snapshot()
	if commits != 0 {
		t.Fatalf("failed name created durable commit=%d", commits)
	}
}

func TestWorldConnectorSubmitShopPurchaseResolvesPositiveOccurrenceWithoutTrustingStockID(t *testing.T) {
	state := shopPurchaseState(t)
	storage := state.Rooms[11]
	storage.Items.Items["stock-third"] = world.Item{Object: world.LegacyObject{Name: "검", Value: 10, Weight: 1}}
	storage.Items.Inventory = append(storage.Items.Inventory, "stock-third")
	state.Rooms[11] = storage
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	store, _ := shopPurchaseStore(t, state)
	connector := newShopPurchaseConnector(t, store, func() (string, error) { return "owned-second", nil })
	connection := admitShopPurchaseConnection(t, connector)

	text, err := connection.Submit(context.Background(), "사 검 2")
	if err != nil || !strings.Contains(text, "검") {
		t.Fatalf("occurrence purchase output=%q err=%v", text, err)
	}
	savedRaw, request, _, _, _, commits := store.snapshot()
	if commits != 1 {
		t.Fatalf("commits=%d", commits)
	}
	var canonical struct {
		ActorID string `json:"actor_id"`
		StockID string `json:"stock_id"`
	}
	if err := json.Unmarshal(request, &canonical); err != nil {
		t.Fatal(err)
	}
	if canonical.ActorID != "actor-1" || canonical.StockID != "stock-third" {
		t.Fatalf("durable request=%s", request)
	}
	saved, err := world.DecodeState(savedRaw)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor-1"].Body.Gold != 90 {
		t.Fatalf("saved gold=%d", saved.Players["actor-1"].Body.Gold)
	}
}
