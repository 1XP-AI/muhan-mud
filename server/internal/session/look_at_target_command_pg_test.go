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
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPostgresLookAtTargetCommandPersistsAndReplays(t *testing.T) {
	dsn := os.Getenv("MUHAN_LOOK_AT_TARGET_TEST_DATABASE_URL")
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
	state, err := world.DecodeState(lookAtTargetCommandFixture(t, true, false))
	if err != nil {
		t.Fatal(err)
	}
	worldID := fmt.Sprintf("look-at-%d", time.Now().UnixNano())
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
	first, err := owners.ExecuteLookAtTargetLine(ctx, store, worldID, "look-at-1", lease, "보아 Bob")
	if err != nil || first.Replayed || first.Revision != 1 || !strings.Contains(string(first.Response), "Bob님을 봅니다") {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	replay, err := owners.ExecuteLookAtTargetLine(ctx, store, worldID, "look-at-1", lease, "보아 Bob")
	if err != nil || !replay.Replayed || replay.Revision != first.Revision || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	snapshot, err := store.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := world.DecodeState(snapshot.State)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["a"].Body.Flags[0]&(1<<1) != 0 {
		t.Fatal("look-at target did not reveal actor in PostgreSQL snapshot")
	}
}

func TestPostgresLookAtTargetOccurrencePersistsAndReplays(t *testing.T) {
	dsn := os.Getenv("MUHAN_LOOK_AT_TARGET_TEST_DATABASE_URL")
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
	state, err := world.DecodeState(lookAtTargetCommandFixture(t, false, false))
	if err != nil {
		t.Fatal(err)
	}
	room := state.Rooms[1]
	room.NPCIDs = []string{"npc-key", "npc-name"}
	state.Rooms[1] = room
	state.NPCs = map[string]world.NPCState{
		"npc-key":  {Body: world.LegacyMonster{Name: "Guard", Keys: [3]string{"goblin"}, Type: 1, RoomID: 1}},
		"npc-name": {Body: world.LegacyMonster{Name: "Goblin", Type: 1, RoomID: 1}},
	}
	worldID := fmt.Sprintf("look-at-occurrence-%d", time.Now().UnixNano())
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
	first, err := owners.ExecuteLookAtTargetLine(ctx, store, worldID, "look-at-occurrence-1", lease, "보아 gob 2")
	if err != nil || first.Replayed || first.Revision != 1 || !strings.Contains(string(first.Response), "Goblin") {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	replay, err := owners.ExecuteLookAtTargetLine(ctx, store, worldID, "look-at-occurrence-1", lease, "보아 gob 2")
	if err != nil || !replay.Replayed || replay.Revision != first.Revision || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
}
