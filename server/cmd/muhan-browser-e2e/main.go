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

	"github.com/1XP-Inc/muhan-mud/server/internal/game"
	"github.com/1XP-Inc/muhan-mud/server/internal/identity"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	databaseURL := flag.String("database", "", "disposable PostgreSQL connection URL")
	worldID := flag.String("world", "browser-e2e", "world snapshot ID to reset")
	characterName := flag.String("name", "BrowserAlice", "character name to reset")
	existingName := flag.String("existing-name", "BrowserOld", "canonical character already linked to the disposable world")
	existingPassword := flag.String("existing-password", "existingpw", "password for the pre-seeded canonical character")
	flag.Parse()
	if *databaseURL == "" || *worldID == "" || *characterName == "" || *existingName == "" || *existingPassword == "" {
		log.Fatal("-database, -world, -name, -existing-name, and -existing-password are required")
	}

	name, err := identity.CanonicalName(*characterName)
	if err != nil {
		log.Fatalf("invalid character name: %v", err)
	}
	canonicalExistingName, err := identity.CanonicalName(*existingName)
	if err != nil {
		log.Fatalf("invalid existing character name: %v", err)
	}
	if canonicalExistingName == name {
		log.Fatal("existing character name must differ from signup character name")
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
	if err := resetDisposableFixture(ctx, db, *worldID, name, canonicalExistingName); err != nil {
		log.Fatalf("reset disposable fixture: %v", err)
	}
	existingDraft, err := game.BuildCreation(game.CreationChoices{
		Male: true, Class: 4, Stats: game.Stats{12, 10, 12, 10, 10}, Weapon: 1, RaceChoice: 7,
	})
	if err != nil {
		log.Fatalf("build existing character: %v", err)
	}
	existingPlayer, err := world.NewPlayerFromDraft(canonicalExistingName, existingDraft)
	if err != nil {
		log.Fatalf("build existing world character: %v", err)
	}
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource: world.LegacyRoom{
					LegacyRoomHeader: world.LegacyRoomHeader{
						ID: 1, Name: "브라우저 광장",
						Exits: []world.LegacyExit{
							{Name: "북", Destination: 2},
							{Name: "동굴", Destination: 3},
						},
					},
					ShortDescription: "실제 Go 서버와 PostgreSQL이 연결된 테스트 방입니다.",
				},
				NPCIDs: []string{"npc-key", "npc-name"},
			},
			2: {
				Resource: world.LegacyRoom{
					LegacyRoomHeader: world.LegacyRoomHeader{
						ID: 2, Name: "브라우저 북쪽",
						Exits: []world.LegacyExit{{Name: "남", Destination: 1}},
					},
					ShortDescription: "북쪽으로 이동한 테스트 방입니다.",
				},
			},
			3: {
				Resource: world.LegacyRoom{
					LegacyRoomHeader: world.LegacyRoomHeader{
						ID: 3, Name: "브라우저 동굴",
						Exits: []world.LegacyExit{{Name: "광장", Destination: 1}},
					},
					ShortDescription: "가 명령으로 들어온 테스트 동굴입니다.",
				},
			},
		},
		Players: map[string]world.PlayerState{"legacy-existing-player": existingPlayer},
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
	existingHash, err := identity.HashPassword([]byte(*existingPassword))
	if err != nil {
		log.Fatalf("hash existing character password: %v", err)
	}
	if _, err := root.LinkExistingWorldCharacter(ctx, *worldID, "browser-link-existing", 0, canonicalExistingName, existingHash, "legacy-existing-player"); err != nil {
		clear(existingHash)
		log.Fatalf("link existing world character: %v", err)
	}
	clear(existingHash)
	fmt.Printf("browser fixture ready: world=%s name=%s\n", *worldID, name)
}

func resetDisposableFixture(ctx context.Context, db *sql.DB, worldID string, names ...string) error {
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
	for _, name := range names {
		for _, statement := range []string{
			`DELETE FROM mud_go.characters WHERE account_id IN (SELECT id FROM mud_go.accounts WHERE name=$1)`,
			`DELETE FROM mud_go.accounts WHERE name=$1`,
		} {
			if _, err := tx.ExecContext(ctx, statement, name); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
