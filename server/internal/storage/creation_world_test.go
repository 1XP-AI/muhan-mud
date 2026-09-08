package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/game"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestPostgresCreationIncludesWorld(t *testing.T) {
	dsn := os.Getenv("MUHAN_CREATION_WORLD_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	p := NewPostgres(db)
	if err = p.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	state := world.State{Version: 1, Rooms: map[int16]world.RoomState{1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}}, Items: &world.ItemCollection{Items: map[string]world.Item{}}}}, Players: map[string]world.PlayerState{}}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err = p.CreateWorld(ctx, "creation", raw); err != nil {
		t.Fatal(err)
	}
	draft, err := game.BuildCreation(game.CreationChoices{Class: 4, Weapon: 1, RaceChoice: 7, Stats: game.Stats{12, 10, 12, 10, 10}})
	if err != nil {
		t.Fatal(err)
	}
	// Fail the final world UPDATE after both identity rows have been inserted.
	if _, err = db.ExecContext(ctx, `ALTER TABLE mud_go.worlds ADD CONSTRAINT reject_creation CHECK(revision=0)`); err != nil {
		t.Fatal(err)
	}
	if _, err = p.CreateInWorld(ctx, "creation", "register-alice", 0, "Alice", []byte("opaque-hash"), draft); err == nil {
		t.Fatal("world failure ignored")
	} else {
		var pe *pgconn.PgError
		if !errors.As(err, &pe) || pe.ConstraintName != "reject_creation" {
			t.Fatalf("wrong failure: %v", err)
		}
	}
	var count int
	if err = db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM mud_go.accounts)+(SELECT count(*) FROM mud_go.characters)`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("orphan identity: %d %v", count, err)
	}
	if _, err = db.ExecContext(ctx, `ALTER TABLE mud_go.worlds DROP CONSTRAINT reject_creation`); err != nil {
		t.Fatal(err)
	}
	id, err := p.CreateInWorld(ctx, "creation", "register-alice", 0, "Alice", []byte("opaque-hash"), draft)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := p.LoadWorld(ctx, "creation")
	if err != nil {
		t.Fatal(err)
	}
	got, err := world.DecodeState(snap.State)
	if err != nil {
		t.Fatal(err)
	}
	player, ok := got.Players[id]
	if !ok || snap.Revision != 1 || player.Online || player.Body.Level != 1 || player.Body.HPCurrent != 56 || player.Body.Gold != 500 || player.Items == nil {
		t.Fatalf("incomplete creation: %+v %+v", snap, player)
	}
	loaded, err := p.Load(ctx, "Alice")
	if err != nil || loaded.ID != id {
		t.Fatalf("identity mismatch %+v %v", loaded, err)
	}
	if _, err = p.CreateInWorld(ctx, "creation", "register-bob", 0, "Bob", []byte("opaque-hash"), draft); !errors.Is(err, ErrWorldConflict) {
		t.Fatalf("stale revision: %v", err)
	}
	if exists, err := p.Exists(ctx, "Bob"); err != nil || exists {
		t.Fatalf("stale write created Bob: %v", err)
	}
	// Reconnect to ensure replay depends on committed data, not process memory.
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p = NewPostgres(db)
	replayed, err := p.CreateInWorld(ctx, "creation", "register-alice", 0, "ALICE", []byte("opaque-hash"), draft)
	if err != nil || replayed != id {
		t.Fatalf("replay %q %v", replayed, err)
	}
	type replayResult struct {
		id  string
		err error
	}
	results := make(chan replayResult, 8)
	for i := 0; i < 8; i++ {
		go func() {
			id, err := p.CreateInWorld(ctx, "creation", "register-alice", 0, "Alice", []byte("opaque-hash"), draft)
			results <- replayResult{id, err}
		}()
	}
	for i := 0; i < 8; i++ {
		r := <-results
		if r.err != nil || r.id != id {
			t.Fatalf("concurrent replay %+v", r)
		}
	}
	changedDraft := draft
	changedDraft.Male = !changedDraft.Male
	if _, err = p.CreateInWorld(ctx, "creation", "register-alice", 0, "Alice", []byte("opaque-hash"), changedDraft); !errors.Is(err, ErrCommandConflict) {
		t.Fatalf("changed draft accepted: %v", err)
	}
	for _, changed := range []struct{ name, hash string }{{"Bob", "opaque-hash"}, {"Alice", "changed-hash"}} {
		if _, err = p.CreateInWorld(ctx, "creation", "register-alice", 0, changed.name, []byte(changed.hash), draft); !errors.Is(err, ErrCommandConflict) {
			t.Fatalf("changed request accepted: %v", err)
		}
	}
	snap, err = p.LoadWorld(ctx, "creation")
	if err != nil || snap.Revision != 1 {
		t.Fatalf("replay changed world: %+v %v", snap, err)
	}
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM mud_go.world_commands WHERE world_id='creation'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("receipt count %d %v", count, err)
	}
}
