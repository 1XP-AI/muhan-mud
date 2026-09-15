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

func TestPostgresPeekCommandPersistsAndReplays(t *testing.T) {
	dsn := os.Getenv("MUHAN_PEEK_TEST_DATABASE_URL")
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
	state, err := world.DecodeState(peekCommandFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	worldID := fmt.Sprintf("peek-%d", time.Now().UnixNano())
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateWorld(ctx, worldID, raw); err != nil {
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
	rolls := []int{1, 100}
	first, err := owners.ExecutePeekLine(ctx, store, worldID, "peek-1", lease, "엿봐 Bob", 100, func(int, int) int {
		value := rolls[0]
		rolls = rolls[1:]
		return value
	})
	if err != nil || first.Replayed || first.Revision != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	replay, err := owners.ExecutePeekLine(ctx, store, worldID, "peek-1", lease, "엿봐 Bob", 100, func(int, int) int { t.Fatal("peek RNG replayed"); return 0 })
	if err != nil || !replay.Replayed || replay.Revision != first.Revision || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
}
