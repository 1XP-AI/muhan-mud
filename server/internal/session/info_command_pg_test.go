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
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPostgresInfoCommandPersistsAndReplaysWithoutNewStateVersion(t *testing.T) {
	dsn := os.Getenv("MUHAN_INFO_TEST_DATABASE_URL")
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
	state, err := decodeInfoFixture()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	worldID := fmt.Sprintf("info-%d", time.Now().UnixNano())
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
	first, err := owners.ExecuteInfoLine(ctx, store, worldID, "info-1", lease, "정보")
	if err != nil || first.Replayed || first.Revision != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	var text string
	if err := json.Unmarshal(first.Response, &text); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"[이름] Alice",
		InfoContinuationPrompt,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("info response missing %q: %q", want, text)
		}
	}

	var revision int64
	if err := db.QueryRowContext(ctx, `SELECT revision FROM mud_go.worlds WHERE id=$1`, worldID).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if revision != first.Revision {
		t.Fatalf("world revision=%d first=%d", revision, first.Revision)
	}
	var receipts int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM mud_go.world_commands WHERE world_id=$1`, worldID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if receipts != 1 {
		t.Fatalf("receipt count after first=%d", receipts)
	}

	replay, err := owners.ExecuteInfoLine(ctx, store, worldID, "info-1", lease, "정보")
	if err != nil || !replay.Replayed || replay.Revision != first.Revision || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	if err := db.QueryRowContext(ctx, `SELECT revision FROM mud_go.worlds WHERE id=$1`, worldID).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if revision != first.Revision {
		t.Fatalf("replay changed world revision=%d first=%d", revision, first.Revision)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM mud_go.world_commands WHERE world_id=$1`, worldID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if receipts != 1 {
		t.Fatalf("receipt count after replay=%d", receipts)
	}

	continuation, err := owners.ExecuteInfoContinuation(ctx, store, worldID, "info-continuation-1", lease)
	if err != nil || continuation.Replayed || continuation.Revision != first.Revision+1 {
		t.Fatalf("continuation=%+v err=%v", continuation, err)
	}
	var continuationText string
	if err := json.Unmarshal(continuation.Response, &continuationText); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(continuationText, "주문:") {
		t.Fatalf("continuation response=%q", continuationText)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM mud_go.world_commands WHERE world_id=$1`, worldID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if receipts != 2 {
		t.Fatalf("receipt count after continuation=%d", receipts)
	}
	continuationReplay, err := owners.ExecuteInfoContinuation(ctx, store, worldID, "info-continuation-1", lease)
	if err != nil || !continuationReplay.Replayed || continuationReplay.Revision != continuation.Revision || string(continuationReplay.Response) != string(continuation.Response) {
		t.Fatalf("continuation replay=%+v err=%v", continuationReplay, err)
	}
}
