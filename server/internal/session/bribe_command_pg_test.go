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

func TestPostgresBribeCommandPersistsAndReplays(t *testing.T) {
	dsn := os.Getenv("MUHAN_BRIBE_TEST_DATABASE_URL")
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
	state, err := world.DecodeState(bribeCommandFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	worldID := fmt.Sprintf("bribe-%d", time.Now().UnixNano())
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateWorld(ctx, worldID, raw); err != nil {
		t.Fatal(err)
	}
	var owners Ownership
	lease, err := owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteBribeLine(ctx, store, worldID, "bribe-pg-1", lease, "Guard 100냥 뇌물")
	if err != nil || first.Replayed || first.Revision != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	replay, err := owners.ExecuteBribeLine(ctx, store, worldID, "bribe-pg-1", lease, "Guard 100냥 뇌물")
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
	if saved.Players["actor"].Body.Gold != 400 || saved.NPCs["guard-one"].Body.Gold != 100 {
		t.Fatalf("saved bribe state=%+v npc=%+v", saved.Players["actor"].Body, saved.NPCs["guard-one"].Body)
	}
}
