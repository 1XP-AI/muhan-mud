package transport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPostgresRunShopPurchaseByNamePersistsReplaysAndRejectsConflict(t *testing.T) {
	dsn := os.Getenv("MUHAN_SHOP_PURCHASE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store := storage.NewPostgres(db)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(shopPurchaseState(t))
	if err != nil {
		t.Fatal(err)
	}
	worldID := fmt.Sprintf("shop-purchase-name-pg-%d", time.Now().UnixNano())
	if err := store.CreateWorld(ctx, worldID, raw); err != nil {
		t.Fatal(err)
	}

	var allocations atomic.Int32
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: worldID, Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1,
		Allocate: func() (string, error) {
			switch allocations.Add(1) {
			case 1:
				return "pg-name-owned", nil
			case 2:
				return "pg-name-owned-child", nil
			default:
				return "", errors.New("unexpected extra allocation")
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	const commandID = "shop-pg-name-command-1"
	first, err := connector.RunShopPurchaseByName(ctx, commandID, "actor-1", `사 "검"`)
	if err != nil || first.Replayed {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	if allocations.Load() != 2 {
		t.Fatalf("allocations=%d", allocations.Load())
	}

	var result world.ShopPurchaseResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.StockID != "stock-root" || result.ItemName != "검" || result.GoldAfter != 60 {
		t.Fatalf("result=%+v", result)
	}

	restarted, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: worldID, Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1,
		Allocate: func() (string, error) { t.Fatal("replay allocated a new item"); return "", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	// The second alias resolves to the same canonical stock identity; replay is
	// receipt-authoritative and does not re-run BuyShopItem.
	replay, err := restarted.RunShopPurchaseByName(ctx, commandID, "actor-1", "구입 검")
	if err != nil || !replay.Replayed || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	if _, err := restarted.RunShopPurchaseByName(ctx, commandID, "actor-1", "사 방패"); !errors.Is(err, storage.ErrCommandConflict) {
		t.Fatalf("request conflict=%v", err)
	}

	snapshot, err := store.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Revision != 1 {
		t.Fatalf("revision=%d", snapshot.Revision)
	}
	saved, err := world.DecodeState(snapshot.State)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor-1"].Body.Gold != 60 || len(saved.Players["actor-1"].Items.Inventory) != 2 {
		t.Fatalf("saved player=%+v", saved.Players["actor-1"])
	}
}
