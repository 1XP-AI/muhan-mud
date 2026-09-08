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

func TestPostgresEmoteCommandPersistsAndReplays(t *testing.T) {
	dsn := os.Getenv("MUHAN_EMOTE_TEST_DATABASE_URL")
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
	state, err := world.DecodeState(emoteCommandFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	worldID := fmt.Sprintf("emote-%d", time.Now().UnixNano())
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
	first, err := owners.ExecuteEmoteLine(ctx, store, worldID, "emote-1", lease, "미소 Bob")
	if err != nil || first.Replayed || first.Revision != 1 || !strings.Contains(string(first.Response), "Bob님에게 미소") {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	replay, err := owners.ExecuteEmoteLine(ctx, store, worldID, "emote-1", lease, "미소 Bob")
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
	if saved.Players["a"].Body.Flags[0]&(1<<1) != 0 {
		t.Fatal("emote did not reveal actor in PostgreSQL snapshot")
	}
}
