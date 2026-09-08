package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPostgresSearchCommandPersistsAndReplays(t *testing.T) {
	dsn := os.Getenv("MUHAN_SEARCH_TEST_DATABASE_URL")
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
	state, err := world.DecodeState(searchCommandFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	worldID := fmt.Sprintf("search-%d", time.Now().UnixNano())
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
	first, err := owners.ExecuteSearchLine(ctx, store, worldID, "search-1", lease, "찾아", 100, func(_, _ int) int { return 1 })
	if err != nil || first.Replayed || first.Revision != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	var result world.SearchResult
	if err := json.Unmarshal(first.Response, &result); err != nil || len(result.Targets) != 3 || result.Targets[0].ID != "exit:1:0" || result.Targets[1].ID != "hidden-root" || result.Targets[2].ID != "b" || !strings.Contains(result.Response, "비밀문") || !strings.Contains(result.Response, "숨은상자") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	replay, err := owners.ExecuteSearchLine(ctx, store, worldID, "search-1", lease, "찾아", 100, func(int, int) int { t.Fatal("search RNG replayed"); return 0 })
	if err != nil || !replay.Replayed || replay.Revision != first.Revision || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
}
