package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPostgresFleeCommandPersistsAndReplays(t *testing.T) {
	dsn := os.Getenv("MUHAN_FLEE_TEST_DATABASE_URL")
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
	store := storage.NewPostgres(db)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	state, err := world.DecodeState(fleeCommandFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	worldID := fmt.Sprintf("flee-%d", time.Now().UnixNano())
	if err := store.CreateWorld(ctx, worldID, mustJSON(state)); err != nil {
		t.Fatal(err)
	}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteFleeLine(ctx, store, worldID, "flee-pg-1", lease, "도망", 100, 12, func(_, _ int) int { return 1 }, nil, nil)
	if err != nil || first.Replayed || first.Revision != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	var result world.FleeResult
	if err := json.Unmarshal(first.Response, &result); err != nil || !result.Moved {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	replay, err := owners.ExecuteFleeLine(ctx, store, worldID, "flee-pg-1", lease, "도망", 100, 12, func(int, int) int { t.Fatal("flee RNG replayed"); return 0 }, nil, nil)
	if err != nil || !replay.Replayed || replay.Revision != first.Revision || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
}
