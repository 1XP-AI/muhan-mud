package session

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPostgresAttackCommandPersistsAndReplays(t *testing.T) {
	dsn := os.Getenv("MUHAN_ATTACK_TEST_DATABASE_URL")
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
	worldID := fmt.Sprintf("attack-%d", time.Now().UnixNano())
	state, err := world.DecodeState(attackCommandFixture())
	if err != nil {
		t.Fatal(err)
	}
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
	first, err := owners.ExecuteAttackLine(ctx, store, worldID, "attack-1", lease, "공격 늑대", func(_, hi int) int { return hi })
	if err != nil || first.Replayed || first.Revision != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	replay, err := owners.ExecuteAttackLine(ctx, store, worldID, "attack-1", lease, "공격 늑대", func(int, int) int { t.Fatal("attack RNG replayed"); return 0 })
	if err != nil || !replay.Replayed || replay.Revision != first.Revision || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	saved, err := store.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := world.DecodeState(saved.State)
	if err != nil || got.NPCs["wolf-id"].Body.HPCurrent != 45 || len(got.NPCs["wolf-id"].Enemies) != 1 {
		t.Fatalf("saved=%+v err=%v", got.NPCs["wolf-id"], err)
	}
}

func TestPostgresLethalAttackCommandPersistsNPCDeath(t *testing.T) {
	dsn := os.Getenv("MUHAN_ATTACK_TEST_DATABASE_URL")
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
	worldID := fmt.Sprintf("attack-lethal-%d", time.Now().UnixNano())
	state, err := world.DecodeState(lethalAttackCommandFixture(t))
	if err != nil {
		t.Fatal(err)
	}
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
	first, err := owners.ExecuteAttackLine(ctx, store, worldID, "attack-lethal", lease, "공격 늑대", func(_, hi int) int { return hi })
	if err != nil || first.Replayed || first.Revision != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	replay, err := owners.ExecuteAttackLine(ctx, store, worldID, "attack-lethal", lease, "공격 늑대", func(int, int) int { t.Fatal("attack RNG replayed"); return 0 })
	if err != nil || !replay.Replayed || replay.Revision != first.Revision || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	saved, err := store.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := world.DecodeState(saved.State)
	if err != nil || len(got.Rooms[1].NPCIDs) != 0 {
		t.Fatalf("saved room=%+v err=%v", got.Rooms[1].NPCIDs, err)
	}
	if _, exists := got.NPCs["wolf-id"]; exists {
		t.Fatal("dead NPC persisted in PostgreSQL")
	}
}

func TestPostgresAttackCommandPersistsWeaponDrop(t *testing.T) {
	dsn := os.Getenv("MUHAN_ATTACK_TEST_DATABASE_URL")
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
	worldID := fmt.Sprintf("attack-weapon-drop-%d", time.Now().UnixNano())
	state, err := world.DecodeState(weaponDropAttackCommandFixture(t))
	if err != nil {
		t.Fatal(err)
	}
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
	first, err := owners.ExecuteAttackLineWithOptions(ctx, store, worldID, "attack-drop", lease, "공격 늑대", weaponDropRoll(t), AttackOptions{})
	if err != nil || first.Replayed || first.Revision != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	replay, err := owners.ExecuteAttackLineWithOptions(ctx, store, worldID, "attack-drop", lease, "공격 늑대", func(int, int) int { t.Fatal("attack RNG replayed"); return 0 }, AttackOptions{})
	if err != nil || !replay.Replayed || replay.Revision != first.Revision || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	saved, err := store.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := world.DecodeState(saved.State)
	if err != nil || got.Players["a"].Items.Ready[19] != "" || len(got.Players["a"].Items.Inventory) != 1 || got.NPCs["wolf-id"].Body.HPCurrent != 50 {
		t.Fatalf("saved player=%+v npc=%+v err=%v", got.Players["a"].Items, got.NPCs["wolf-id"].Body, err)
	}
}

func TestPostgresAttackCommandPersistsPowerDamageSequence(t *testing.T) {
	dsn := os.Getenv("MUHAN_ATTACK_TEST_DATABASE_URL")
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
	worldID := fmt.Sprintf("attack-power-%d", time.Now().UnixNano())
	state, err := world.DecodeState(powerDamageAttackCommandFixture(t))
	if err != nil {
		t.Fatal(err)
	}
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
	first, err := owners.ExecuteAttackLine(ctx, store, worldID, "attack-power", lease, "공격 늑대", powerDamageRoll(t))
	if err != nil || first.Replayed || first.Revision != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	replay, err := owners.ExecuteAttackLine(ctx, store, worldID, "attack-power", lease, "공격 늑대", func(int, int) int { t.Fatal("attack RNG replayed"); return 0 })
	if err != nil || !replay.Replayed || replay.Revision != first.Revision || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	saved, err := store.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := world.DecodeState(saved.State)
	if err != nil || got.NPCs["wolf-id"].Body.HPCurrent != 40 || got.NPCs["wolf-id"].Enemies[0].Damage != 10 {
		t.Fatalf("saved npc=%+v err=%v", got.NPCs["wolf-id"], err)
	}
}

func TestPostgresLethalAttackCommandPersistsNPCDrops(t *testing.T) {
	dsn := os.Getenv("MUHAN_ATTACK_TEST_DATABASE_URL")
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
	worldID := fmt.Sprintf("attack-drops-%d", time.Now().UnixNano())
	state, err := world.DecodeState(lethalDropAttackCommandFixture(t))
	if err != nil {
		t.Fatal(err)
	}
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
	allocated := 0
	first, err := owners.ExecuteAttackLineWithOptions(ctx, store, worldID, "attack-drops", lease, "공격 늑대", func(_, hi int) int { return hi }, AttackOptions{
		Now: 99,
		Allocate: func() (string, error) {
			allocated++
			return fmt.Sprintf("drop-%d", allocated), nil
		},
	})
	if err != nil || first.Replayed || first.Revision != 1 || allocated != 2 {
		t.Fatalf("first=%+v err=%v allocated=%d", first, err, allocated)
	}
	replay, err := owners.ExecuteAttackLineWithOptions(ctx, store, worldID, "attack-drops", lease, "공격 늑대", func(int, int) int { t.Fatal("attack RNG replayed"); return 0 }, AttackOptions{
		Now:      100,
		Allocate: func() (string, error) { t.Fatal("drop allocator replayed"); return "", nil },
	})
	if err != nil || !replay.Replayed || replay.Revision != first.Revision || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	saved, err := store.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := world.DecodeState(saved.State)
	if err != nil || len(got.Rooms[1].Items.Items) != 2 || len(got.Rooms[1].Items.Inventory) != 2 {
		t.Fatalf("saved drops=%+v err=%v", got.Rooms[1].Items, err)
	}
}
