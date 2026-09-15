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
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPostgresItemMutationCommandPersistsAndReplays(t *testing.T) {
	dsn := os.Getenv("MUHAN_ITEM_MUTATION_TEST_DATABASE_URL")
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
	state := itemMutationFixtureForSession()
	worldID := fmt.Sprintf("item-mutation-%d", time.Now().UnixNano())
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
	first, err := owners.ExecuteItemMutationLine(ctx, store, worldID, "item-1", lease, "주워 가방")
	if err != nil || first.Replayed || first.Revision != 1 || !strings.Contains(string(first.Response), "주웠습니다") {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	replay, err := owners.ExecuteItemMutationLine(ctx, store, worldID, "item-1", lease, "주워 가방")
	if err != nil || !replay.Replayed || replay.Revision != first.Revision || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	second, err := owners.ExecuteItemMutationLine(ctx, store, worldID, "item-2", lease, "버려 가방")
	if err != nil || second.Replayed || second.Revision != 2 || !strings.Contains(string(second.Response), "버렸습니다") {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	third, err := owners.ExecuteItemMutationLine(ctx, store, worldID, "item-3", lease, "주워 모두")
	if err != nil || third.Replayed || third.Revision != 3 || !strings.Contains(string(third.Response), "주웠습니다") {
		t.Fatalf("third=%+v err=%v", third, err)
	}
	fourth, err := owners.ExecuteItemMutationLine(ctx, store, worldID, "item-4", lease, "버려 모두")
	if err != nil || fourth.Replayed || fourth.Revision != 4 || !strings.Contains(string(fourth.Response), "버렸습니다") {
		t.Fatalf("fourth=%+v err=%v", fourth, err)
	}
	fifth, err := owners.ExecuteItemMutationLine(ctx, store, worldID, "item-5", lease, "꺼내 가방 보석")
	if err != nil || fifth.Replayed || fifth.Revision != 5 || !strings.Contains(string(fifth.Response), "주웠습니다") {
		t.Fatalf("fifth=%+v err=%v", fifth, err)
	}
	sixth, err := owners.ExecuteItemMutationLine(ctx, store, worldID, "item-6", lease, "주워 가방")
	if err != nil || sixth.Replayed || sixth.Revision != 6 || !strings.Contains(string(sixth.Response), "주웠습니다") {
		t.Fatalf("sixth=%+v err=%v", sixth, err)
	}
	seventh, err := owners.ExecuteItemMutationLine(ctx, store, worldID, "item-7", lease, "넣어 보석 가방")
	if err != nil || seventh.Replayed || seventh.Revision != 7 || !strings.Contains(string(seventh.Response), "버렸습니다") {
		t.Fatalf("seventh=%+v err=%v", seventh, err)
	}
}
