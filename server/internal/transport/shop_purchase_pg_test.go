package transport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestPostgresRunShopPurchasePersistsReplaysAndRejectsConflict exercises the
// durable shop command boundary against an isolated PostgreSQL instance. The
// replay uses a fresh connector so only the receipt in PostgreSQL can make it
// succeed without allocating another owned item identity.
func TestPostgresRunShopPurchasePersistsReplaysAndRejectsConflict(t *testing.T) {
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
	initial := shopPurchaseState(t)
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	worldID := fmt.Sprintf("shop-purchase-pg-%d", time.Now().UnixNano())
	if err := store.CreateWorld(ctx, worldID, raw); err != nil {
		t.Fatal(err)
	}

	var allocationCalls atomic.Int32
	allocated := []string{"pg-owned-root", "pg-owned-child"}
	allocate := func() (string, error) {
		index := int(allocationCalls.Add(1)) - 1
		if index >= len(allocated) {
			return "", fmt.Errorf("unexpected extra shop allocation")
		}
		return allocated[index], nil
	}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: worldID, Clock: func() (int32, int) { return 100, 12 },
		Allocate: allocate, MaxSessions: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	const commandID = "shop-pg-command-1"
	first, err := connector.RunShopPurchase(ctx, commandID, "actor-1", "stock-root")
	if err != nil || first.Replayed || first.Revision != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	if allocationCalls.Load() != 2 {
		t.Fatalf("first allocation calls=%d want=2", allocationCalls.Load())
	}

	snapshot, err := store.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := world.DecodeState(snapshot.State)
	if err != nil {
		t.Fatal(err)
	}
	player := saved.Players["actor-1"]
	if snapshot.Revision != 1 || player.Body.Gold != 60 || len(player.Items.Inventory) != 2 || len(player.Items.Items) != 3 {
		t.Fatalf("persisted revision=%d player=%+v", snapshot.Revision, player)
	}
	if player.Items.Items["pg-owned-root"].Contents[0] != "pg-owned-child" {
		t.Fatalf("persisted purchased graph=%+v", player.Items.Items)
	}
	if _, ok := saved.Rooms[11].Items.Items["stock-root"]; !ok {
		t.Fatal("shop stock was removed instead of copied")
	}
	var result world.ShopPurchaseResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != "buy-shop-item" || result.StockID != "stock-root" || result.Price != 40 || result.GoldBefore != 100 || result.GoldAfter != 60 {
		t.Fatalf("purchase result=%+v", result)
	}
	if !reflect.DeepEqual(result.PurchasedIDs, []string{"pg-owned-root", "pg-owned-child"}) {
		t.Fatalf("purchased IDs=%v", result.PurchasedIDs)
	}

	var receiptCount int
	var storedRevision int64
	if err := db.QueryRowContext(ctx, `SELECT count(*), COALESCE(max(revision), 0) FROM mud_go.world_commands WHERE world_id=$1`, worldID).Scan(&receiptCount, &storedRevision); err != nil {
		t.Fatal(err)
	}
	if receiptCount != 1 || storedRevision != 1 {
		t.Fatalf("receipt rows=%d revision=%d", receiptCount, storedRevision)
	}

	var replayAllocationCalls atomic.Int32
	restarted, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: worldID, Clock: func() (int32, int) { return 100, 12 },
		Allocate: func() (string, error) {
			replayAllocationCalls.Add(1)
			return "", errors.New("replay must not allocate")
		}, MaxSessions: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	replay, err := restarted.RunShopPurchase(ctx, commandID, "actor-1", "stock-root")
	if err != nil || !replay.Replayed || replay.Revision != first.Revision || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	if replayAllocationCalls.Load() != 0 {
		t.Fatalf("replay allocations=%d", replayAllocationCalls.Load())
	}

	if _, err := restarted.RunShopPurchase(ctx, commandID, "actor-1", "stock-other"); !errors.Is(err, storage.ErrCommandConflict) {
		t.Fatalf("request conflict=%v", err)
	}
	afterReplay, err := store.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	if afterReplay.Revision != 1 {
		t.Fatalf("conflict changed revision=%d", afterReplay.Revision)
	}
	var afterCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM mud_go.world_commands WHERE world_id=$1`, worldID).Scan(&afterCount); err != nil {
		t.Fatal(err)
	}
	if afterCount != 1 {
		t.Fatalf("conflict changed receipt count=%d", afterCount)
	}
}

// TestPostgresRunShopPurchaseRollsBackWorldAndReceiptOnInsertFailure forces
// the receipt insert to fail after the world UPDATE. PostgreSQL must roll the
// entire command transaction back, leaving no state change and no receipt for
// a later retry to mistake for a committed purchase.
func TestPostgresRunShopPurchaseRollsBackWorldAndReceiptOnInsertFailure(t *testing.T) {
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
	initial := shopPurchaseState(t)
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	worldID := fmt.Sprintf("shop-purchase-rollback-pg-%d", time.Now().UnixNano())
	if err := store.CreateWorld(ctx, worldID, raw); err != nil {
		t.Fatal(err)
	}

	if _, err := db.ExecContext(ctx, `ALTER TABLE mud_go.world_commands DROP CONSTRAINT IF EXISTS shop_purchase_rollback_test_reject`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE mud_go.world_commands ADD CONSTRAINT shop_purchase_rollback_test_reject CHECK (command_id <> 'shop-pg-rollback')`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = db.ExecContext(context.Background(), `ALTER TABLE mud_go.world_commands DROP CONSTRAINT IF EXISTS shop_purchase_rollback_test_reject`)
	}()

	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: worldID, Clock: func() (int32, int) { return 100, 12 },
		Allocate: func() (string, error) { return "pg-rollback-root", nil }, MaxSessions: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connector.RunShopPurchase(ctx, "shop-pg-rollback", "actor-1", "stock-other"); err == nil {
		t.Fatal("receipt constraint unexpectedly allowed purchase")
	}

	snapshot, err := store.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Revision != 0 {
		t.Fatalf("rollback changed world revision=%d", snapshot.Revision)
	}
	saved, err := world.DecodeState(snapshot.State)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(saved, initial) {
		t.Fatalf("rollback changed world state: saved=%+v initial=%+v", saved, initial)
	}
	var receiptCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM mud_go.world_commands WHERE world_id=$1`, worldID).Scan(&receiptCount); err != nil {
		t.Fatal(err)
	}
	if receiptCount != 0 {
		t.Fatalf("rollback left %d receipt rows", receiptCount)
	}
}
