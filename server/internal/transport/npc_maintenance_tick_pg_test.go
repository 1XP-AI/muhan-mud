package transport

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestWorldConnectorNPCMaintenancePostgresPersistsReplaysAndRollsBack
// exercises the durable pre-combat NPC maintenance boundary against a real
// PostgreSQL database.  The DSN must point at a disposable test database;
// keeping this behind an explicit opt-in prevents the default local suite
// from touching an operating or shared database.
func TestWorldConnectorNPCMaintenancePostgresPersistsReplaysAndRollsBack(t *testing.T) {
	dsn := os.Getenv("MUHAN_NPC_MAINTENANCE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL required")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(4)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store := storage.NewPostgres(db)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	worldID := fmt.Sprintf("npc-maintenance-pg-%d", time.Now().UnixNano())
	initial := npcMaintenancePostgresFixture(t)
	if err := store.CreateWorld(ctx, worldID, initial); err != nil {
		t.Fatal(err)
	}

	rollCalls := 0
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: worldID, MaxSessions: 1,
		Clock: func() (int32, int) { return 103, 12 },
		Roll: func(low, high int) int {
			rollCalls++
			if low != 1 || high != 100 {
				t.Fatalf("unexpected maintenance wander range %d..%d", low, high)
			}
			return 1
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	first, ran, err := connector.RunNPCMaintenanceTick(ctx, 20*time.Second)
	if err != nil || !ran || first.Replayed || first.Revision != 1 {
		t.Fatalf("first=%+v ran=%v err=%v", first, ran, err)
	}
	if rollCalls != 1 {
		t.Fatalf("first maintenance RNG calls=%d, want 1", rollCalls)
	}
	var firstResult world.NPCMaintenanceResult
	if err := json.Unmarshal(first.Response, &firstResult); err != nil {
		t.Fatal(err)
	}
	if firstResult.Now != 100 || len(firstResult.Actions) != 1 || !firstResult.Actions[0].Wandered || len(firstResult.ActiveNPCIDs) != 0 {
		t.Fatalf("first maintenance result=%+v", firstResult)
	}

	saved, err := store.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	savedState, err := world.DecodeState(saved.State)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Revision != 1 || len(savedState.NPCs) != 0 || len(savedState.Rooms[1].NPCIDs) != 0 || len(savedState.ActiveNPCIDs) != 0 {
		t.Fatalf("persisted maintenance state revision=%d rooms=%+v npcs=%+v active=%v", saved.Revision, savedState.Rooms[1].NPCIDs, savedState.NPCs, savedState.ActiveNPCIDs)
	}

	// Verify that the response was actually written to the receipt table, not
	// merely returned by the in-memory connector.
	request, err := json.Marshal(npcMaintenanceTickRequest{Kind: "npc-maintenance", Slot: 5, Now: 100})
	if err != nil {
		t.Fatal(err)
	}
	storedReceipt, err := store.ReadWorldReceipt(ctx, worldID, npcMaintenanceCommandID(5), request)
	if err != nil {
		t.Fatal(err)
	}
	if !storedReceipt.Replayed || storedReceipt.Revision != first.Revision || !bytes.Equal(storedReceipt.Response, first.Response) {
		t.Fatalf("stored receipt=%+v", storedReceipt)
	}
	var receiptCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM mud_go.world_commands WHERE world_id=$1 AND command_id=$2`, worldID, npcMaintenanceCommandID(5)).Scan(&receiptCount); err != nil {
		t.Fatal(err)
	}
	if receiptCount != 1 {
		t.Fatalf("maintenance receipt rows=%d, want 1", receiptCount)
	}

	// A fresh connector models process restart.  It has no slot cursor and its
	// RNG must not be called while the durable receipt is replayed.
	restarted, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: worldID, MaxSessions: 1,
		Clock: func() (int32, int) { return 103, 12 },
		Roll: func(int, int) int {
			t.Fatal("replayed NPC maintenance reran the reducer")
			return 0
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	replay, ran, err := restarted.RunNPCMaintenanceTick(ctx, 20*time.Second)
	if err != nil || !ran || !replay.Replayed || replay.Revision != first.Revision || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v ran=%v err=%v", replay, ran, err)
	}

	// Reusing the receipt identity with a different slot/time must fail before
	// any reducer or world write and must leave the committed snapshot intact.
	beforeConflict, err := store.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.runNPCMaintenancePhaseAt(ctx, npcMaintenanceCommandID(5), 6, 120); !errors.Is(err, storage.ErrCommandConflict) {
		t.Fatalf("maintenance request conflict err=%v", err)
	}
	afterConflict, err := store.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	if afterConflict.Revision != beforeConflict.Revision || !bytes.Equal(afterConflict.State, beforeConflict.State) {
		t.Fatalf("command conflict changed world revision/state %d/%s -> %d/%s", beforeConflict.Revision, beforeConflict.State, afterConflict.Revision, afterConflict.State)
	}

	// Force the receipt INSERT to fail after the world UPDATE is attempted.
	// CommitWorldCommand must roll back both statements, leaving the exact
	// command retryable once the injected constraint is removed.
	failedCommandID := fmt.Sprintf("npc-maintenance-fail-%d", time.Now().UnixNano())
	constraintName := fmt.Sprintf("npc_maintenance_receipt_reject_%d", time.Now().UnixNano())
	constraintAdded := false
	defer func() {
		if constraintAdded {
			if _, cleanupErr := db.ExecContext(context.Background(), fmt.Sprintf("ALTER TABLE mud_go.world_commands DROP CONSTRAINT %s", constraintName)); cleanupErr != nil {
				t.Logf("drop test constraint %s: %v", constraintName, cleanupErr)
			}
		}
	}()
	if _, err := db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE mud_go.world_commands ADD CONSTRAINT %s CHECK (command_id <> '%s')", constraintName, failedCommandID)); err != nil {
		t.Fatal(err)
	}
	constraintAdded = true

	beforeRollback, err := store.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.runNPCMaintenancePhaseAt(ctx, failedCommandID, 6, 120); err == nil {
		t.Fatal("receipt constraint failure was ignored")
	}
	afterRollback, err := store.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	if afterRollback.Revision != beforeRollback.Revision || !bytes.Equal(afterRollback.State, beforeRollback.State) {
		t.Fatalf("failed maintenance changed world revision/state %d/%s -> %d/%s", beforeRollback.Revision, beforeRollback.State, afterRollback.Revision, afterRollback.State)
	}
	var failedReceiptCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM mud_go.world_commands WHERE world_id=$1 AND command_id=$2`, worldID, failedCommandID).Scan(&failedReceiptCount); err != nil {
		t.Fatal(err)
	}
	if failedReceiptCount != 0 {
		t.Fatalf("failed maintenance left %d receipt rows", failedReceiptCount)
	}

	if _, err := db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE mud_go.world_commands DROP CONSTRAINT %s", constraintName)); err != nil {
		t.Fatal(err)
	}
	constraintAdded = false
	retried, err := restarted.runNPCMaintenancePhaseAt(ctx, failedCommandID, 6, 120)
	if err != nil || retried.Replayed || retried.Revision != beforeRollback.Revision+1 {
		t.Fatalf("maintenance retry after rollback=%+v err=%v", retried, err)
	}
}

func npcMaintenancePostgresFixture(t *testing.T) json.RawMessage {
	t.Helper()
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}, Traffic: 100},
				PlayerIDs: []string{"hero"},
				NPCIDs:    []string{"wanderer"},
			},
		},
		Players: map[string]world.PlayerState{
			"hero": {Body: world.LegacyMonster{Name: "영웅", Type: 0, RoomID: 1}, Online: true},
		},
		NPCs: map[string]world.NPCState{
			"wanderer": {
				Body:    world.LegacyMonster{Name: "방황자", Type: 1, RoomID: 1, HPMax: 10, HPCurrent: 10, MPMax: 10, MPCurrent: 10},
				Enemies: []world.NPCEnemy{},
			},
		},
		ActiveNPCIDs: []string{"wanderer"},
	}
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
