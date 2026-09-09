package transport

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

// TestWorldConnectorNPCResourceTickPostgresPersistsAndReplays exercises the
// scheduler's durable receipt boundary against a real PostgreSQL database.
// It is opt-in so the default local suite never creates or reuses a database.
func TestWorldConnectorNPCResourceTickPostgresPersistsAndReplays(t *testing.T) {
	dsn := os.Getenv("MUHAN_NPC_RESOURCE_TEST_DATABASE_URL")
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
	pg := storage.NewPostgres(db)
	if err := pg.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	worldID := fmt.Sprintf("npc-resource-pg-%d", time.Now().UnixNano())
	raw := npcIdentityTickState(t, true)
	if _, err := world.DecodeState(raw); err != nil {
		t.Fatal(err)
	}
	if err := pg.CreateWorld(ctx, worldID, raw); err != nil {
		t.Fatal(err)
	}

	allocated := []string{"pg-npc-0", "pg-npc-1"}
	allocate := func() (string, error) {
		if len(allocated) == 0 {
			return "", fmt.Errorf("unexpected extra NPC allocation")
		}
		id := allocated[0]
		allocated = allocated[1:]
		return id, nil
	}
	clock := func() (int32, int) { return 40, 12 }
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: pg, WorldID: worldID, Clock: clock, MaxSessions: 1,
		Catalog: npcIdentityTickCatalogFixture(), Roll: npcIdentityTickRoll, Allocate: allocate,
	})
	if err != nil {
		t.Fatal(err)
	}
	first, ran, err := connector.RunNPCResourceTick(ctx, 20*time.Second)
	if err != nil || !ran || first.Replayed || first.Revision != 1 {
		t.Fatalf("first=%+v ran=%v err=%v", first, ran, err)
	}
	saved, err := pg.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	state, err := world.DecodeState(saved.State)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Revision != 1 || len(state.NPCs) != 3 || len(state.Rooms[1].NPCIDs) != 3 {
		t.Fatalf("persisted NPC state revision=%d room=%#v NPCs=%d", saved.Revision, state.Rooms[1].NPCIDs, len(state.NPCs))
	}

	// A new connector has no in-memory slot cursor. The same command ID must
	// therefore resolve the durable receipt without re-running catalog/RNG or
	// allocating another identity.
	restarted, err := NewWorldConnector(WorldConnectorConfig{
		Store: pg, WorldID: worldID, Clock: clock, MaxSessions: 1,
		Catalog: npcIdentityTickCatalogFixture(), Roll: func(int, int) int { t.Fatal("replayed NPC tick rerolled"); return 0 },
		Allocate: func() (string, error) { t.Fatal("replayed NPC tick reallocated"); return "", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	replay, ran, err := restarted.RunNPCResourceTick(ctx, 20*time.Second)
	if err != nil || !ran || !replay.Replayed || replay.Revision != first.Revision || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v ran=%v err=%v", replay, ran, err)
	}
	var summary npcResourcePhaseSummary
	if err := json.Unmarshal(replay.Response, &summary); err != nil || len(summary.SpawnedNPCIDs) != 2 {
		t.Fatalf("replay summary=%+v err=%v", summary, err)
	}
}
