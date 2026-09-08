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

func TestPostgresDoorKeyCommandPersistsAndReplays(t *testing.T) {
	dsn := os.Getenv("MUHAN_DOOR_KEY_TEST_DATABASE_URL")
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
	worldID := fmt.Sprintf("door-key-%d", time.Now().UnixNano())
	if err := store.CreateWorld(ctx, worldID, doorKeyCommandFixture(t)); err != nil {
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
	first, err := owners.ExecuteDoorKeyLineWithOptions(ctx, store, worldID, "door-key-1", lease, "풀어 북 열쇠", DoorKeyOptions{Now: 10})
	if err != nil || first.Replayed || first.Revision != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	replay, err := owners.ExecuteDoorKeyLineWithOptions(ctx, store, worldID, "door-key-1", lease, "풀어 북 열쇠", DoorKeyOptions{Now: 10})
	if err != nil || !replay.Replayed || replay.Revision != first.Revision || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	saved, err := store.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	state, err := world.DecodeState(saved.State)
	var result world.DoorKeyCommandResult
	if err := json.Unmarshal(first.Response, &result); err != nil || state.Rooms[1].Resource.Exits[0].Flags[0]&4 != 0 || state.Players["a"].Items.Items["key-1"].Object.ShotsCurrent != 1 || !result.Broadcast {
		t.Fatalf("state=%+v result=%+v err=%v", state, result, err)
	}
}
