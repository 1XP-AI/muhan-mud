package session

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPostgresItemsCommandPersistsAndReplays(t *testing.T) {
	dsn := os.Getenv("MUHAN_ITEMS_TEST_DATABASE_URL")
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
	state, err := world.DecodeState(followCommandFixture())
	if err != nil {
		t.Fatal(err)
	}
	worldID := fmt.Sprintf("items-%d", time.Now().UnixNano())
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
	first, err := owners.ExecuteItemsLine(ctx, store, worldID, "items-1", lease, "소지품")
	if err != nil || first.Replayed || first.Revision != 1 || !strings.Contains(string(first.Response), "소지품") {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	replay, err := owners.ExecuteItemsLine(ctx, store, worldID, "items-1", lease, "소지품")
	if err != nil || !replay.Replayed || replay.Revision != first.Revision || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	second, err := owners.ExecuteItemsLine(ctx, store, worldID, "items-2", lease, "장비")
	if err != nil || second.Replayed || second.Revision != 2 || !strings.Contains(string(second.Response), "걸치고") {
		t.Fatalf("second=%+v err=%v", second, err)
	}
}
