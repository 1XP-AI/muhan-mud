package storage

import (
	"context"
	"database/sql"
	"errors"
	"github.com/1XP-Inc/muhan-mud/server/internal/game"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
	"os"
	"testing"
)

func TestPostgresCreateIsAtomic(t *testing.T) {
	dsn := os.Getenv("MUHAN_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL URL required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	// The caller must provide an empty disposable database; never truncate data.
	repo := NewPostgres(db)
	if err := repo.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	draft, err := game.BuildCreation(game.CreationChoices{Class: 4, Weapon: 1, RaceChoice: 7, Stats: game.Stats{12, 10, 12, 10, 10}})
	if err != nil {
		t.Fatal(err)
	}
	id, err := repo.Create(ctx, "타봇", []byte("test-only-opaque-hash"), draft)
	if err != nil {
		t.Fatal(err)
	}
	got, err := repo.Load(ctx, "타봇")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != id || got.Draft != draft {
		t.Fatal("roundtrip mismatch")
	}
	if _, err := repo.Create(ctx, "타봇", []byte("different-hash"), draft); err == nil {
		t.Fatal("duplicate accepted")
	}
	// Force the second insert to fail after the account insert succeeded.
	// NOT VALID preserves existing fixture rows but rejects new valid drafts.
	// Malformed drafts now fail before BeginTx and cannot prove rollback here.
	if _, err := db.ExecContext(ctx, `ALTER TABLE mud_go.characters ADD CONSTRAINT test_gold CHECK ((draft->>'Gold')::int <> 500) NOT VALID`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create(ctx, "실패", []byte("test-hash"), draft); err == nil {
		t.Fatal("second insert failure ignored")
	} else {
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" || pgErr.ConstraintName != "test_gold" {
			t.Fatalf("did not reach injected DB constraint: %v", err)
		}
	}
	var accounts, characters int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM mud_go.accounts").Scan(&accounts); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM mud_go.characters").Scan(&characters); err != nil {
		t.Fatal(err)
	}
	if accounts != 1 || characters != 1 {
		t.Fatal("duplicate left orphan rows")
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE mud_go.characters DROP CONSTRAINT test_gold`); err != nil {
		t.Fatal(err)
	}
	registered, err := repo.Register(ctx, "aLICE", []byte("pw1234"), draft)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := repo.Authenticate(ctx, "ALICE", []byte("pw1234"))
	if err != nil || verified.ID != registered {
		t.Fatal("relogin failed")
	}
	for _, name := range []string{"Alice", "Nobody"} {
		got, err := repo.Authenticate(ctx, name, []byte("wrong"))
		if !errors.Is(err, ErrCredentials) || got != (Character{}) {
			t.Fatal("invalid login disclosed character")
		}
	}
	var stored []byte
	if err := db.QueryRowContext(ctx, "SELECT credential_hash FROM mud_go.accounts WHERE name='Alice'").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if string(stored) == "pw1234" {
		t.Fatal("plaintext persisted")
	}
}
