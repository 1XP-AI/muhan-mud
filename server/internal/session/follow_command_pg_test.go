package session

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPostgresFollowAndLoseCommandPersistsAndReplays(t *testing.T) {
	dsn := os.Getenv("MUHAN_FOLLOW_TEST_DATABASE_URL")
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
	worldID := fmt.Sprintf("follow-%d", time.Now().UnixNano())
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

	follow, err := owners.ExecuteFollowLine(ctx, store, worldID, "follow-1", lease, "따라 Bob")
	if err != nil || follow.Replayed || follow.Revision != 1 {
		t.Fatalf("follow=%+v err=%v", follow, err)
	}
	followReplay, err := owners.ExecuteFollowLine(ctx, store, worldID, "follow-1", lease, "따라 Bob")
	if err != nil || !followReplay.Replayed || followReplay.Revision != follow.Revision || string(followReplay.Response) != string(follow.Response) {
		t.Fatalf("follow replay=%+v err=%v", followReplay, err)
	}

	lose, err := owners.ExecuteFollowLine(ctx, store, worldID, "lose-1", lease, "내보내")
	if err != nil || lose.Replayed || lose.Revision != 2 {
		t.Fatalf("lose=%+v err=%v", lose, err)
	}
	loseReplay, err := owners.ExecuteFollowLine(ctx, store, worldID, "lose-1", lease, "내보내")
	if err != nil || !loseReplay.Replayed || loseReplay.Revision != lose.Revision || string(loseReplay.Response) != string(lose.Response) {
		t.Fatalf("lose replay=%+v err=%v", loseReplay, err)
	}
	snapshot, err := store.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := world.DecodeState(snapshot.State)
	if err != nil || saved.Players["a"].FollowingID != "" || len(saved.Players["b"].FollowerIDs) != 0 {
		t.Fatalf("saved=%+v err=%v", saved.Players, err)
	}
}
