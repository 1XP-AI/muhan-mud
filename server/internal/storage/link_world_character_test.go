package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/identity"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestPostgresLinksExistingWorldCharacterWithReceipt(t *testing.T) {
	dsn := os.Getenv("MUHAN_LINK_WORLD_CHARACTER_TEST_DATABASE_URL")
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
	worldID := fmt.Sprintf("link-%d", time.Now().UnixNano())
	name := fmt.Sprintf("A%d", time.Now().UnixNano()%10000000)
	state := linkTestState(name, "legacy-player")
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err = p.CreateWorld(ctx, worldID, raw); err != nil {
		t.Fatal(err)
	}
	hash, err := identity.HashPassword([]byte("pw1234"))
	if err != nil {
		t.Fatal(err)
	}
	id, err := p.LinkExistingWorldCharacter(ctx, worldID, "link-alice", 0, name, hash, "legacy-player")
	if err != nil || id != "legacy-player" {
		t.Fatalf("link %q %v", id, err)
	}
	snapshot, err := p.LoadWorld(ctx, worldID)
	if err != nil || snapshot.Revision != 1 {
		t.Fatalf("world revision %+v %v", snapshot, err)
	}
	linkedState, err := world.DecodeState(snapshot.State)
	if err != nil || linkedState.Players["legacy-player"].Online {
		t.Fatalf("link mutated player state: %v", err)
	}
	character, err := p.Authenticate(ctx, name, []byte("pw1234"))
	if err != nil || character.ID != "legacy-player" || !character.Linked || character.WorldID != worldID {
		t.Fatalf("linked authentication %+v %v", character, err)
	}
	if _, err = p.Authenticate(ctx, name, []byte("wrong1")); !errors.Is(err, ErrCredentials) {
		t.Fatalf("wrong password accepted: %v", err)
	}
	replayed, err := p.LinkExistingWorldCharacter(ctx, worldID, "link-alice", 0, name, hash, "legacy-player")
	if err != nil || replayed != id {
		t.Fatalf("replay %q %v", replayed, err)
	}
	snapshot, err = p.LoadWorld(ctx, worldID)
	if err != nil || snapshot.Revision != 1 {
		t.Fatalf("replay advanced world: %+v %v", snapshot, err)
	}
	if _, err = p.LinkExistingWorldCharacter(ctx, worldID, "link-alice", 0, name, []byte("changed-hash"), "legacy-player"); !errors.Is(err, ErrCommandConflict) {
		t.Fatalf("changed credential accepted: %v", err)
	}
	if _, err = p.LinkExistingWorldCharacter(ctx, worldID, "link-duplicate", 1, name, hash, "legacy-player"); !errors.Is(err, ErrWorldCharacterConflict) {
		t.Fatalf("duplicate player link accepted: %v", err)
	}
	if _, err = p.LinkExistingWorldCharacter(ctx, worldID, "link-stale", 0, name, hash, "missing-player"); !errors.Is(err, ErrWorldConflict) {
		t.Fatalf("stale revision accepted: %v", err)
	}
	if _, err = p.LinkExistingWorldCharacter(ctx, worldID, "link-invalid-hash", 1, name, []byte("not-a-bcrypt-hash"), "legacy-player"); !errors.Is(err, ErrInvalidCredentialHash) {
		t.Fatalf("invalid hash accepted: %v", err)
	}
	if _, err = p.LinkExistingWorldCharacter(ctx, worldID, "link-name-mismatch", 1, "Other", hash, "legacy-player"); !errors.Is(err, ErrWorldCharacterConflict) {
		t.Fatalf("name-only mismatch accepted: %v", err)
	}
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM mud_go.world_commands WHERE world_id=$1`, worldID).Scan(new(int)); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresRejectsAmbiguousExistingWorldCharacterName(t *testing.T) {
	dsn := os.Getenv("MUHAN_LINK_WORLD_CHARACTER_TEST_DATABASE_URL")
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
	worldID := fmt.Sprintf("ambiguous-%d", time.Now().UnixNano())
	name := fmt.Sprintf("B%d", time.Now().UnixNano()%10000000)
	state := linkTestState(name, "first-player")
	state.Players["second-player"] = state.Players["first-player"]
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err = p.CreateWorld(ctx, worldID, raw); err != nil {
		t.Fatal(err)
	}
	hash, err := identity.HashPassword([]byte("pw1234"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.LinkExistingWorldCharacter(ctx, worldID, "link-ambiguous", 0, name, hash, "first-player"); !errors.Is(err, ErrWorldCharacterConflict) {
		t.Fatalf("ambiguous name accepted: %v", err)
	}
	var accounts int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM mud_go.accounts WHERE name=$1`, name).Scan(&accounts); err != nil {
		t.Fatal(err)
	}
	if accounts != 0 {
		t.Fatalf("ambiguous link created account: %d", accounts)
	}
}

func linkTestState(name, playerID string) world.State {
	return world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
				Items:    &world.ItemCollection{Items: map[string]world.Item{}},
			},
		},
		Players: map[string]world.PlayerState{
			playerID: {
				Body:  world.LegacyMonster{Name: name, Level: 1, RoomID: 1},
				Items: &world.ItemCollection{Items: map[string]world.Item{}},
			},
		},
	}
}
