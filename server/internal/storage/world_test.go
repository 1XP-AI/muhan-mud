package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"
)

func TestWorldCommandTransaction(t *testing.T) {
	dsn := os.Getenv("MUHAN_WORLD_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	p := NewPostgres(db)
	if err = p.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = p.CreateWorld(ctx, "test-world", json.RawMessage(`{"room":1}`)); err != nil {
		t.Fatal(err)
	}
	request := json.RawMessage(`{"actor":"a","move":"north"}`)
	r, err := p.CommitWorldCommand(ctx, "test-world", "command-1", request, 0, json.RawMessage(`{"room":2}`), json.RawMessage(`{"text":"arrived"}`))
	if err != nil || r.Revision != 1 || r.Replayed {
		t.Fatalf("%+v %v", r, err)
	}
	replay, err := p.CommitWorldCommand(ctx, "test-world", "command-1", request, 0, json.RawMessage(`{"room":99}`), json.RawMessage(`{"text":"wrong"}`))
	if err != nil || !replay.Replayed || replay.Revision != 1 || string(replay.Response) != string(r.Response) {
		t.Fatalf("%+v %v", replay, err)
	}
	if _, err = p.CommitWorldCommand(ctx, "test-world", "command-1", json.RawMessage(`{"move":"south"}`), 1, json.RawMessage(`{}`), json.RawMessage(`{}`)); !errors.Is(err, ErrCommandConflict) {
		t.Fatal(err)
	}
	if _, err = p.CommitWorldCommand(ctx, "test-world", "stale", request, 0, json.RawMessage(`{}`), json.RawMessage(`{}`)); !errors.Is(err, ErrWorldConflict) {
		t.Fatal(err)
	}
	// Fail receipt insert after state UPDATE; both must roll back.
	if _, err = db.ExecContext(ctx, `ALTER TABLE mud_go.world_commands ADD CONSTRAINT reject_fixture CHECK(command_id<>'fail-receipt')`); err != nil {
		t.Fatal(err)
	}
	if _, err = p.CommitWorldCommand(ctx, "test-world", "fail-receipt", request, 1, json.RawMessage(`{"room":3}`), json.RawMessage(`{}`)); err == nil {
		t.Fatal("receipt failure ignored")
	}
	snapshot, err := p.LoadWorld(ctx, "test-world")
	if err != nil || snapshot.Revision != 1 {
		t.Fatalf("%+v %v", snapshot, err)
	}
	var state struct{ Room int }
	if err = json.Unmarshal(snapshot.State, &state); err != nil || state.Room != 2 {
		t.Fatalf("%+v %v", state, err)
	}
	var count int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM mud_go.world_commands WHERE world_id='test-world'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("receipts=%d %v", count, err)
	}
}

func TestWorldCommandConcurrency(t *testing.T) {
	dsn := os.Getenv("MUHAN_WORLD_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	p := NewPostgres(db)
	if err = p.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = p.CreateWorld(ctx, "parallel", json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	type result struct {
		r   WorldReceipt
		err error
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			r, err := p.CommitWorldCommand(ctx, "parallel", "same", json.RawMessage(`{"actor":"a"}`), 0, json.RawMessage(`{"moved":true}`), json.RawMessage(`{"ok":true}`))
			results <- result{r, err}
		}()
	}
	close(start)
	replayed := 0
	for i := 0; i < 2; i++ {
		got := <-results
		if got.err != nil || got.r.Revision != 1 {
			t.Fatalf("%+v", got)
		}
		if got.r.Replayed {
			replayed++
		}
	}
	if replayed != 1 {
		t.Fatalf("replay count=%d", replayed)
	}
	start = make(chan struct{})
	for _, id := range []string{"next-a", "next-b"} {
		go func(id string) {
			<-start
			r, err := p.CommitWorldCommand(ctx, "parallel", id, json.RawMessage(`{}`), 1, json.RawMessage(`{"moved":2}`), json.RawMessage(`{}`))
			results <- result{r, err}
		}(id)
	}
	close(start)
	conflicts := 0
	for i := 0; i < 2; i++ {
		got := <-results
		if errors.Is(got.err, ErrWorldConflict) {
			conflicts++
		} else if got.err != nil || got.r.Revision != 2 {
			t.Fatalf("%+v", got)
		}
	}
	if conflicts != 1 {
		t.Fatalf("conflicts=%d", conflicts)
	}
}
