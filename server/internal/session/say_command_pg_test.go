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

func TestPostgresSayCommandPersistsAndReplays(t *testing.T) {
	dsn := os.Getenv("MUHAN_SAY_TEST_DATABASE_URL")
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
	p := state.Players["a"]
	p.Body.Flags[0] |= 1 << 1
	state.Players["a"] = p
	worldID := fmt.Sprintf("say-%d", time.Now().UnixNano())
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
	first, err := owners.ExecuteSayLine(ctx, store, worldID, "say-1", lease, "말 안녕하세요")
	if err != nil || first.Replayed || first.Revision != 1 || !strings.Contains(string(first.Response), "좋습니다") {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	replay, err := owners.ExecuteSayLine(ctx, store, worldID, "say-1", lease, "말 안녕하세요")
	if err != nil || !replay.Replayed || replay.Revision != first.Revision || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	snapshot, err := store.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := world.DecodeState(snapshot.State)
	if err != nil {
		t.Fatal(err)
	}
	if flag := saved.Players["a"].Body.Flags[0] & (1 << 1); flag != 0 {
		t.Fatal("say did not reveal player in persisted state")
	}
}
