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
	name := fmt.Sprintf("T%d", time.Now().UnixNano()%100000000)
	for _, line := range []string{name, "예", "", "여", "4", "12 10 12 10 10", "1", "선", "7", "pw1234"} {
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
	view = other.Submit(ctx, name)
	if !view.Secret {
		t.Fatal("existing character offered signup")
	}
	view = other.Submit(ctx, "badpass")
	if view.Verified != nil || view.Closed {
		t.Fatal("wrong password flow")
	}
	view = other.Submit(ctx, "pw1234")
	if view.Verified == nil || view.Verified.ID != saved.ID || view.Verified.Draft != saved.Draft {
		t.Fatal("relogin changed character")
	}
}

func TestPostgresLegacyPlayerFileLoginAndRelogin(t *testing.T) {
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
	worldID := fmt.Sprintf("legacy-login-%d", time.Now().UnixNano())
	name := fmt.Sprintf("L%d", time.Now().UnixNano()%100000000)
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}}},
		},
		Players: map[string]world.PlayerState{},
	}
	rawState, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateWorld(ctx, worldID, rawState); err != nil {
		t.Fatal(err)
	}
	raw := legacyLoginRaw(name, "pw1234", 1)
	files := memoryLegacyFiles{
		name: {PlayerID: "legacy-" + name, CommandID: "import-" + name, Raw: raw},
	}
	accounts := NewLiveAccounts(repo, repo, files, repo, worldID)
	s := NewLogin(accounts)
	view := s.Submit(ctx, name)
	if view.Verified != nil || view.Closed || !view.Secret {
		t.Fatal("name-only ownership")
	}
	view = s.Submit(ctx, "wrong")
	if view.Verified != nil || view.Closed {
		t.Fatal("wrong password imported")
	}
	view = s.Submit(ctx, "pw1234")
	if view.Verified == nil || view.Verified.ID != "legacy-"+name || !view.Verified.Linked {
		t.Fatalf("legacy PG login failed: %+v", view.Verified)
	}
	loaded, err := repo.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	loadedState, err := world.DecodeState(loaded.State)
	if err != nil {
		t.Fatal(err)
	}
	player, ok := loadedState.Players["legacy-"+name]
	if !ok || player.Online || player.Body.Name != name {
		t.Fatalf("imported player=%+v ok=%v", player, ok)
	}
	s.Close()
	relogin := NewLogin(repo)
	view = relogin.Submit(ctx, name)
	if !view.Secret {
		t.Fatal("imported character offered signup")
	}
	view = relogin.Submit(ctx, "pw1234")
	if view.Verified == nil || view.Verified.ID != "legacy-"+name {
		t.Fatal("relogin after C-file import failed")
	}
}
