// Command muhan-browser-e2e provisions the smallest disposable world used by
// the real Go + PostgreSQL + browser smoke test. It is deliberately separate
// from the production server command: the test harness owns the database and
// supplies an explicit world/name pair, so this command never discovers or
// mutates an operational world.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/identity"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	databaseURL := flag.String("database", "", "disposable PostgreSQL connection URL")
	worldID := flag.String("world", "browser-e2e", "world snapshot ID to reset")
	characterName := flag.String("name", "BrowserAlice", "character name to reset")
	flag.Parse()
	if *databaseURL == "" || *worldID == "" || *characterName == "" {
		log.Fatal("-database, -world, and -name are required")
	}

	name, err := identity.CanonicalName(*characterName)
	if err != nil {
		log.Fatalf("invalid character name: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := sql.Open("pgx", *databaseURL)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("database unavailable: %v", err)
	}
	root := storage.NewPostgres(db)
	if err := root.Migrate(ctx); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	if err := resetDisposableFixture(ctx, db, *worldID, name); err != nil {
		log.Fatalf("reset disposable fixture: %v", err)
	}
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource: world.LegacyRoom{
					LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "브라우저 광장"},
					ShortDescription: "실제 Go 서버와 PostgreSQL이 연결된 테스트 방입니다.",
				},
				NPCIDs: []string{"npc-key", "npc-name"},
			},
		},
		Players: map[string]world.PlayerState{},
		NPCs: map[string]world.NPCState{
			"npc-key": {
				Body: world.LegacyMonster{
					Name: "Guard", Keys: [3]string{"goblin"}, Type: 1, RoomID: 1,
					HPMax: 30, HPCurrent: 30,
				},
				Enemies: []world.NPCEnemy{},
			},
			"npc-name": {
				Body:    world.LegacyMonster{Name: "Goblin", Type: 1, RoomID: 1, HPMax: 30, HPCurrent: 30},
				Enemies: []world.NPCEnemy{},
			},
		},
		ActiveNPCIDs: []string{},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		log.Fatalf("marshal world: %v", err)
	}
	if err := root.CreateWorld(ctx, *worldID, raw); err != nil {
		log.Fatalf("create world: %v", err)
	}
	fmt.Printf("browser fixture ready: world=%s name=%s\n", *worldID, name)
}

func resetDisposableFixture(ctx context.Context, db *sql.DB, worldID, name string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// The caller must provide a dedicated disposable database. Scope every
	// destructive statement to the exact fixture identifiers; no broad reset
	// or database drop is performed here.
	for _, statement := range []string{
		`DELETE FROM mud_go.world_commands WHERE world_id=$1`,
		`DELETE FROM mud_go.world_writer_claims WHERE world_id=$1`,
		`DELETE FROM mud_go.worlds WHERE id=$1`,
	} {
		if _, err := tx.ExecContext(ctx, statement, worldID); err != nil {
			return err
		}
	}
	for _, statement := range []string{
		`DELETE FROM mud_go.characters WHERE account_id IN (SELECT id FROM mud_go.accounts WHERE name=$1)`,
		`DELETE FROM mud_go.accounts WHERE name=$1`,
	} {
		if _, err := tx.ExecContext(ctx, statement, name); err != nil {
			return err
		}
	}
	return tx.Commit()
}
