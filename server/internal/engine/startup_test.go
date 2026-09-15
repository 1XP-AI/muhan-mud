package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
	"os"
	"testing"
	"time"
)

func TestPostgresStartupRecoversBeforeReturningWriter(t *testing.T) {
	dsn := os.Getenv("MUHAN_STARTUP_TEST_DATABASE_URL")
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
	root := storage.NewPostgres(db)
	if err = root.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	s := world.State{Version: 1, Rooms: map[int16]world.RoomState{1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}}, PlayerIDs: []string{"a"}}}, Players: map[string]world.PlayerState{"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1, HPCurrent: 37, Gold: 777}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{"kept": {Object: world.LegacyObject{Name: "검"}}}, Inventory: []string{"kept"}}}}}
	room := s.Rooms[1]
	room.NPCIDs = []string{"known", "unknown"}
	s.Rooms[1] = room
	s.NPCs = map[string]world.NPCState{
		"known": {Body: world.LegacyMonster{Name: "Guard", Type: 1, RoomID: 1, HPCurrent: 55}, Enemies: []world.NPCEnemy{
			{Target: world.EntityRef{Kind: "player", ID: "a"}, Damage: -1},
			{Target: world.EntityRef{Kind: "npc", ID: "unknown"}, Damage: 17},
		}},
		"unknown": {Body: world.LegacyMonster{Name: "Wolf", Type: 1, RoomID: 1}},
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if err = root.CreateWorld(ctx, "startup", raw); err != nil {
		t.Fatal(err)
	}
	writer, r, err := StartWorld(ctx, root, "startup", "boot-a")
	if err != nil || writer == nil || r.Revision != 1 || r.Replayed {
		t.Fatalf("%+v %v", r, err)
	}
	saved, err := writer.LoadWorld(ctx, "startup")
	if err != nil {
		t.Fatal(err)
	}
	got, err := world.DecodeState(saved.State)
	if err != nil {
		t.Fatal(err)
	}
	p := got.Players["a"]
	if p.Online || len(got.Rooms[1].PlayerIDs) != 0 || p.Body.HPCurrent != 37 || p.Body.Gold != 777 || p.Items.Inventory[0] != "kept" {
		t.Fatal("startup damaged persistent state")
	}
	known := got.NPCs["known"]
	if len(got.Rooms[1].NPCIDs) != 2 || got.Rooms[1].NPCIDs[0] != "known" || known.Body.HPCurrent != 55 || len(known.Enemies) != 1 || known.Enemies[0].Target != (world.EntityRef{Kind: "npc", ID: "unknown"}) || known.Enemies[0].Damage != 17 || got.NPCs["unknown"].Enemies != nil {
		t.Fatal("startup NPC cleanup lost identity or relationship state")
	}
	_, retry, err := StartWorld(ctx, root, "startup", "boot-a")
	if err != nil || !retry.Replayed || retry.Revision != 1 {
		t.Fatalf("startup not idempotent %+v %v", retry, err)
	}
	if err = root.CreateWorld(ctx, "corrupt", []byte(`{"Version":1}`)); err != nil {
		t.Fatal(err)
	}
	if writer, r, err := StartWorld(ctx, root, "corrupt", "boot-b"); err == nil || writer != nil || len(r.Response) != 0 {
		t.Fatal("invalid world became ready")
	}
}

func TestPostgresSeedLegacyRoomCatalog(t *testing.T) {
	dsn := os.Getenv("MUHAN_SEED_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	root := storage.NewPostgres(db)
	if err := root.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	catalog, err := world.LoadLegacyRoomCatalog(os.DirFS("../../../rooms"), world.LegacyRoomCompatibilityPolicy)
	if err != nil {
		t.Fatal(err)
	}
	worldID := "seed-" + time.Now().UTC().Format("20060102150405.000000000")
	if err := SeedWorldFromCatalog(ctx, root, worldID, catalog); err != nil {
		t.Fatal(err)
	}
	snapshot, err := root.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	state, err := world.DecodeState(snapshot.State)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Revision != 0 || len(state.Players) != 0 || len(state.Rooms) != catalog.Len() || state.Rooms[1].Resource.Name != "무한대전" {
		t.Fatalf("unexpected seeded state revision=%d rooms=%d: %+v", snapshot.Revision, len(state.Rooms), state.Rooms[1])
	}
}

func TestPostgresSeedCanonicalRoomGraphs(t *testing.T) {
	dsn := os.Getenv("MUHAN_SEED_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	root := storage.NewPostgres(db)
	if err := root.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	catalog, err := world.LoadLegacyRoomCatalog(os.DirFS("../../../rooms"), world.LegacyRoomCompatibilityPolicy)
	if err != nil {
		t.Fatal(err)
	}
	worldID := "canonical-seed-" + time.Now().UTC().Format("20060102150405.000000000")
	if err := SeedCanonicalWorldFromCatalog(ctx, root, worldID, catalog); err != nil {
		t.Fatal(err)
	}
	snapshot, err := root.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	state, err := world.DecodeState(snapshot.State)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Revision != 0 || len(state.Players) != 0 || len(state.Rooms) != catalog.Len() || state.NPCs == nil {
		t.Fatalf("unexpected canonical seed revision=%d rooms=%d NPCs=%d", snapshot.Revision, len(state.Rooms), len(state.NPCs))
	}
	for id, room := range state.Rooms {
		if room.Items == nil || len(room.Resource.Objects) != 0 || len(room.Resource.Monsters) != 0 {
			t.Fatalf("room %d retained legacy resource ownership", id)
		}
	}
	for id, npc := range state.NPCs {
		if npc.Items == nil || len(npc.Body.Inventory) != 0 {
			t.Fatalf("NPC %s retained legacy inventory", id)
		}
	}
}
