package storage

import (
	"context"
	"database/sql"
	"errors"
	"github.com/1XP-Inc/muhan-mud/server/internal/game"
	"os"
	"testing"
)

func TestPostgresWriterGenerationRejectsPreviousServer(t *testing.T) {
	dsn := os.Getenv("MUHAN_WRITER_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	root := NewPostgres(db)
	if err = root.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = root.CreateWorld(ctx, "w", []byte(`{"value":0}`)); err != nil {
		t.Fatal(err)
	}
	a, err := root.ClaimWorldWriter(ctx, "w", "boot-a")
	if err != nil {
		t.Fatal(err)
	}
	request := []byte(`{"kind":"test"}`)
	if _, err = root.CommitWorldCommand(ctx, "w", "unbound", request, 0, []byte(`{"value":1}`), []byte(`null`)); !errors.Is(err, ErrWriterFenced) {
		t.Fatalf("unbound write: %v", err)
	}
	if _, err = a.CommitWorldCommand(ctx, "w", "a", request, 0, []byte(`{"value":1}`), []byte(`null`)); err != nil {
		t.Fatal(err)
	}
	aReplay, err := root.ClaimWorldWriter(ctx, "w", "boot-a")
	if err != nil || aReplay.writerEpoch != a.writerEpoch {
		t.Fatalf("claim replay advanced epoch: %v", err)
	}
	b, err := root.ClaimWorldWriter(ctx, "w", "boot-b")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = root.ClaimWorldWriter(ctx, "w", "boot-a"); !errors.Is(err, ErrWriterFenced) {
		t.Fatalf("old claim retook world: %v", err)
	}
	// Even receipt replay cannot let a retired writer continue serving commands.
	if _, err = a.CommitWorldCommand(ctx, "w", "a", request, 0, []byte(`{"value":1}`), []byte(`null`)); !errors.Is(err, ErrWriterFenced) {
		t.Fatalf("retired writer replay: %v", err)
	}
	draft, err := game.BuildCreation(game.CreationChoices{Class: 4, Weapon: 1, RaceChoice: 7, Stats: game.Stats{12, 10, 12, 10, 10}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.CreateInWorld(ctx, "w", "new", 1, "Alice", []byte("hash"), draft); !errors.Is(err, ErrWriterFenced) {
		t.Fatalf("retired registration: %v", err)
	}
	if _, err = b.CommitWorldCommand(ctx, "w", "b", request, 1, []byte(`{"value":2}`), []byte(`null`)); err != nil {
		t.Fatal(err)
	}
	snap, err := root.LoadWorld(ctx, "w")
	if err != nil || snap.Revision != 2 {
		t.Fatalf("%+v %v", snap, err)
	}
}
