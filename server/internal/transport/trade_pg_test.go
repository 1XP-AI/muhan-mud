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

	"github.com/1XP-Inc/muhan-mud/server/internal/session"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func tradePostgresState(t *testing.T) world.State {
	t.Helper()
	var npcFlags [8]byte
	npcFlags[37/8] |= 1 << (37 % 8)
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{200: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 200, Name: "교역방"}},
			PlayerIDs: []string{"a"},
			NPCIDs:    []string{"npc"},
		}},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 200}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{"offered": {Object: world.LegacyObject{Name: "사과", Keys: [3]string{"apple"}, Type: 13, ShotsMax: 10, ShotsCurrent: 10}}}, Inventory: []string{"offered"}}},
		},
		NPCs: map[string]world.NPCState{
			"npc": {Body: world.LegacyMonster{Name: "상인", Type: 1, RoomID: 200, Flags: npcFlags}, TradeOffers: []world.NPCTradeOffer{{Wanted: world.LegacyObject{Name: "사과", Keys: [3]string{"apple"}, Type: 13}, Reward: &world.LegacyObject{Name: "보상검", Type: 13}}}},
		},
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPostgresTradeNPCByNamePersistsReplaysAndRejectsConflict(t *testing.T) {
	dsn := os.Getenv("MUHAN_TRADE_TEST_DATABASE_URL")
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
	raw, err := json.Marshal(tradePostgresState(t))
	if err != nil {
		t.Fatal(err)
	}
	worldID := fmt.Sprintf("trade-pg-%d", time.Now().UnixNano())
	if err := store.CreateWorld(ctx, worldID, raw); err != nil {
		t.Fatal(err)
	}
	owners := &session.Ownership{}
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	const commandID = "trade-pg-command-1"
	first, err := owners.ExecuteTradeLine(ctx, store, worldID, commandID, lease, "사과 상인 교환")
	if err != nil || first.Replayed {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	restarted := &session.Ownership{}
	replayLease, err := restarted.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.Admit(replayLease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	replay, err := restarted.ExecuteTradeLine(ctx, store, worldID, commandID, replayLease, "사과 상인 교환")
	if err != nil || !replay.Replayed || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	if _, err := restarted.ExecuteTradeLine(ctx, store, worldID, commandID, replayLease, "사과 다른사람 교환"); !errors.Is(err, storage.ErrCommandConflict) {
		t.Fatalf("request conflict=%v", err)
	}
	snapshot, err := store.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := world.DecodeState(snapshot.State)
	if err != nil || snapshot.Revision != 1 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	items := saved.Players["a"].Items
	if _, exists := items.Items["offered"]; exists || len(items.Inventory) != 1 || items.Items[items.Inventory[0]].Object.Name != "보상검" {
		t.Fatalf("saved items=%+v", items)
	}
}
