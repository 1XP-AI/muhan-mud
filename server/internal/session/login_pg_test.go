package session

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPostgresTerminalSignupAndRelogin(t *testing.T) {
	dsn := os.Getenv("MUHAN_SESSION_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated session DB required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := storage.NewPostgres(db)
	ctx := context.Background()
	if err := repo.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	s := NewLogin(repo)
	var view View
	for _, line := range []string{"테스터", "예", "", "여", "4", "12 10 12 10 10", "1", "선", "7", "pw1234"} {
		view = s.Submit(ctx, line)
		if view.Closed {
			t.Fatal("signup closed unexpectedly")
		}
	}
	if view.Verified == nil {
		t.Fatal("signup not persisted")
	}
	saved := *view.Verified
	s.Close()
	other := NewLogin(repo)
	view = other.Submit(ctx, "테스터")
	if !view.Secret {
		t.Fatal("existing character offered signup")
	}
	view = other.Submit(ctx, "badpass")
	if view.Verified != nil || view.Closed {
		t.Fatal("wrong password flow")
	}
	view = other.Submit(ctx, "pw1234")
	if view.Verified == nil || *view.Verified != saved {
		t.Fatal("relogin changed character")
	}
}
