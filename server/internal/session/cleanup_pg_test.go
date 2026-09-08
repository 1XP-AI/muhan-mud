package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestPostgresCleanupQueueSurvivesWorldLockTimeout(t *testing.T) {
	dsn := os.Getenv("MUHAN_DEPARTURE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pg := storage.NewPostgres(db)
	if err = pg.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	state := world.State{Version: 1, Rooms: map[int16]world.RoomState{1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}}, PlayerIDs: []string{"a"}}}, Players: map[string]world.PlayerState{"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1, HPCurrent: 30}, Online: true}}}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err = pg.CreateWorld(ctx, "locked-cleanup", raw); err != nil {
		t.Fatal(err)
	}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	q := NewWorldCleanupQueue(&owners, pg, "locked-cleanup")
	if err = q.Enqueue(lease); err != nil {
		t.Fatal(err)
	}
	requestID := q.Pending()[0].CommandID
	lock, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback()
	var revision int64
	if err = lock.QueryRowContext(ctx, `SELECT revision FROM mud_go.worlds WHERE id='locked-cleanup' FOR UPDATE`).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if err = q.Retry(ctx, 50*time.Millisecond); err == nil {
		t.Fatal("locked world unexpectedly cleaned")
	}
	pending := q.Pending()
	if len(pending) != 1 || pending[0].CommandID != requestID || pending[0].LastError == nil || !owners.Owns(lease) {
		t.Fatal("timeout discarded pending cleanup")
	}
	var receipts int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM mud_go.world_commands WHERE world_id='locked-cleanup'`).Scan(&receipts); err != nil || receipts != 0 {
		t.Fatalf("timeout committed receipt %d %v", receipts, err)
	}
	if err = lock.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err = q.Retry(ctx, time.Second); err != nil || len(q.Pending()) != 0 || owners.Owns(lease) {
		t.Fatalf("retry failed: %v", err)
	}
	saved, err := pg.LoadWorld(ctx, "locked-cleanup")
	if err != nil {
		t.Fatal(err)
	}
	got, err := world.DecodeState(saved.State)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Revision != 1 || got.Players["a"].Online || got.Players["a"].Body.HPCurrent != 30 || len(got.Rooms[1].PlayerIDs) != 0 {
		t.Fatal("cleanup altered game data or left occupancy")
	}
	var storedID string
	if err = db.QueryRowContext(ctx, `SELECT command_id FROM mud_go.world_commands WHERE world_id='locked-cleanup'`).Scan(&storedID); err != nil || storedID != requestID {
		t.Fatalf("request identity changed: %v", err)
	}
}
