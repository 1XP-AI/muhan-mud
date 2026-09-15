package transport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func shopMarketplacePostgresState(t *testing.T) world.State {
	t.Helper()
	var flags, storageFlags [8]byte
	flags[world.RoomShopFlag/8] |= 1 << (world.RoomShopFlag % 8)
	flags[world.RoomPawnFlag/8] |= 1 << (world.RoomPawnFlag % 8)
	storageFlags[12/8] |= 1 << (12 % 8)
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			200: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 200, Flags: flags}}, PlayerIDs: []string{"a"}},
			201: {
				Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 201, Flags: storageFlags}},
				Items:    &world.ItemCollection{Items: map[string]world.Item{"stock": {Object: world.LegacyObject{Name: "기존", Type: 13, Value: 80, Weight: 1}}}, Inventory: []string{"stock"}},
			},
		},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 200, Gold: 100}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{"sword": {Object: world.LegacyObject{Name: "검", Type: 13, Value: 100, Weight: 2}}}, Inventory: []string{"sword"}}},
		},
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPostgresShopMarketplaceListSellPersistReplayAndConflict(t *testing.T) {
	dsn := os.Getenv("MUHAN_SHOP_MARKETPLACE_TEST_DATABASE_URL")
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
	state := shopMarketplacePostgresState(t)
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	worldID := fmt.Sprintf("shop-marketplace-pg-%d", time.Now().UnixNano())
	if err := store.CreateWorld(ctx, worldID, raw); err != nil {
		t.Fatal(err)
	}
	connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: worldID, Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	list, err := connector.owners.ExecuteShopLine(ctx, store, worldID, "shop-pg-list", lease, "품목")
	if err != nil || list.Replayed {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	sold, err := connector.owners.ExecuteShopLine(ctx, store, worldID, "shop-pg-sell", lease, "팔아 검")
	if err != nil || sold.Replayed {
		t.Fatalf("sell=%+v err=%v", sold, err)
	}
	restarted, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: worldID, Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1})
	if err != nil {
		t.Fatal(err)
	}
	replayLease, err := restarted.owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.owners.Admit(replayLease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	replay, err := restarted.owners.ExecuteShopLine(ctx, store, worldID, "shop-pg-sell", replayLease, "팔아 검")
	if err != nil || !replay.Replayed || string(replay.Response) != string(sold.Response) {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	if _, err := restarted.owners.ExecuteShopLine(ctx, store, worldID, "shop-pg-sell", replayLease, "팔아 없음"); !errors.Is(err, storage.ErrCommandConflict) {
		t.Fatalf("request conflict=%v", err)
	}
	snapshot, err := store.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := world.DecodeState(snapshot.State)
	if err != nil || snapshot.Revision != 2 || saved.Players["a"].Body.Gold != 150 || saved.Rooms[201].Items.Items["sword"].Object.Name != "검" {
		t.Fatalf("snapshot=%+v saved=%+v err=%v", snapshot, saved, err)
	}
	var receipts int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM mud_go.world_commands WHERE world_id=$1`, worldID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if receipts != 2 {
		t.Fatalf("receipt rows=%d", receipts)
	}
}
