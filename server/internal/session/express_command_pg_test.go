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

func TestPostgresExpressCommandPersistsAndReplays(t *testing.T) {
	dsn := os.Getenv("MUHAN_EXPRESS_TEST_DATABASE_URL")
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
	state, err := world.DecodeState(expressCommandFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	worldID := fmt.Sprintf("express-%d", time.Now().UnixNano())
	if err := store.CreateWorld(ctx, worldID, mustJSON(state)); err != nil {
		t.Fatal(err)
	}
	owners, lease := newExpressOwners(t)
	first, err := owners.ExecuteExpressLine(ctx, store, worldID, "express-1", lease, "표현 hello")
	if err != nil || first.Replayed || first.Revision != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	var response string
	if err := json.Unmarshal(first.Response, &response); err != nil || response != "예. 좋습니다.\r\n" {
		t.Fatalf("response=%q err=%v", response, err)
	}
	replay, err := owners.ExecuteExpressLine(ctx, store, worldID, "express-1", lease, "표현 hello")
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
		t.Fatal("expression did not reveal actor in persisted state")
	}
	event, ok, err := saved.RoomExpressEvent("a", "hello")
	if err != nil || !ok || event.Text != "\n:Alice님이 hello.\r\n" {
		t.Fatalf("event=%+v ok=%v err=%v", event, ok, err)
	}
}
