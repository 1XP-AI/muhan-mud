package transport

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestWorldConnectorNPCCombatTickPostgresPersistsAndReplays exercises the
// canonical active-NPC order through the real receipt transaction.  It is
// opt-in because the test needs an isolated PostgreSQL database.
func TestWorldConnectorNPCCombatTickPostgresPersistsAndReplays(t *testing.T) {
	dsn := os.Getenv("MUHAN_NPC_COMBAT_TEST_DATABASE_URL")
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

	worldID := fmt.Sprintf("npc-combat-pg-%d", time.Now().UnixNano())
	initial := npcCombatTickFixture(t)
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := world.DecodeState(raw); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateWorld(ctx, worldID, raw); err != nil {
		t.Fatal(err)
	}

	rollCalls := 0
	roll := func(low, high int) int {
		rollCalls++
		return npcCombatTickRoll(low, high)
	}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store:       store,
		WorldID:     worldID,
		Clock:       func() (int32, int) { return 103, 12 },
		MaxSessions: 1,
		Roll:        roll,
	})
	if err != nil {
		t.Fatal(err)
	}

	const commandID = "npc-combat-pg-5"
	first, err := connector.RunNPCCombatPhase(ctx, commandID, 5, 100)
	if err != nil || first.Replayed || first.Revision != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	var firstSummary NPCCombatTickSummary
	if err := json.Unmarshal(first.Response, &firstSummary); err != nil {
		t.Fatal(err)
	}
	if firstSummary.Slot != 5 || firstSummary.Now != 100 || len(firstSummary.Attacks) != 2 || len(firstSummary.Skipped) != 0 || len(firstSummary.FailClosed) != 0 {
		t.Fatalf("first summary=%+v", firstSummary)
	}
	if got := []string{firstSummary.Attacks[0].NPCID, firstSummary.Attacks[1].NPCID}; !reflect.DeepEqual(got, []string{"npc-b", "npc-a"}) {
		t.Fatalf("active attack order=%v", got)
	}

	saved, err := store.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	savedState, err := world.DecodeState(saved.State)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Revision != 1 || savedState.Players["player-a"].Body.HPCurrent != 26 || savedState.Players["player-b"].Body.HPCurrent != 26 {
		t.Fatalf("persisted combat state revision=%d players=%+v", saved.Revision, savedState.Players)
	}
	if rollCalls == 0 {
		t.Fatal("combat reducer did not consume its bound RNG")
	}

	// The same connector must resolve the immutable receipt before invoking
	// the reducer a second time.
	callsAfterCommit := rollCalls
	same, err := connector.RunNPCCombatPhase(ctx, commandID, 5, 100)
	if err != nil || !same.Replayed || same.Revision != first.Revision || string(same.Response) != string(first.Response) || rollCalls != callsAfterCommit {
		t.Fatalf("same-command replay=%+v calls=%d/%d err=%v", same, rollCalls, callsAfterCommit, err)
	}

	// Re-open the pool and build a fresh connector.  This models process
	// restart: no in-memory scheduler state or RNG result is trusted for the
	// replay.
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db2, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	db2.SetMaxOpenConns(4)
	restartedStore := storage.NewPostgres(db2)
	restarted, err := NewWorldConnector(WorldConnectorConfig{
		Store:       restartedStore,
		WorldID:     worldID,
		Clock:       func() (int32, int) { return 103, 12 },
		MaxSessions: 1,
		Roll: func(int, int) int {
			t.Fatal("replayed combat tick reran the reducer")
			return 0
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	replay, err := restarted.RunNPCCombatPhase(ctx, commandID, 5, 100)
	if err != nil || !replay.Replayed || replay.Revision != first.Revision || string(replay.Response) != string(first.Response) {
		t.Fatalf("new connector replay=%+v err=%v", replay, err)
	}

	// A receipt identity is bound to the complete request, not only the
	// command ID.  The conflict must not touch the committed snapshot.
	beforeConflict, err := restartedStore.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	rollbackConnector, err := NewWorldConnector(WorldConnectorConfig{
		Store:       restartedStore,
		WorldID:     worldID,
		Clock:       func() (int32, int) { return 103, 12 },
		MaxSessions: 1,
		Roll:        npcCombatTickRoll,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.RunNPCCombatPhase(ctx, commandID, 6, 120); !errors.Is(err, storage.ErrCommandConflict) {
		t.Fatalf("same command with changed request err=%v", err)
	}
	afterConflict, err := restartedStore.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	if afterConflict.Revision != beforeConflict.Revision {
		t.Fatalf("command conflict advanced revision %d -> %d", beforeConflict.Revision, afterConflict.Revision)
	}

	// Force the receipt INSERT to fail after the world UPDATE is attempted.
	// CommitWorldCommand must roll back both statements, leaving no receipt so
	// the exact same command can be retried after the injected failure is gone.
	failedCommandID := fmt.Sprintf("npc-combat-fail-receipt-%d", time.Now().UnixNano())
	constraintName := fmt.Sprintf("npc_combat_receipt_reject_%d", time.Now().UnixNano())
	constraintAdded := false
	defer func() {
		if constraintAdded {
			if _, cleanupErr := db2.ExecContext(context.Background(), fmt.Sprintf("ALTER TABLE mud_go.world_commands DROP CONSTRAINT %s", constraintName)); cleanupErr != nil {
				t.Logf("drop test constraint %s: %v", constraintName, cleanupErr)
			}
		}
	}()
	if _, err := db2.ExecContext(ctx, fmt.Sprintf("ALTER TABLE mud_go.world_commands ADD CONSTRAINT %s CHECK (command_id <> '%s')", constraintName, failedCommandID)); err != nil {
		t.Fatal(err)
	}
	constraintAdded = true

	beforeRollback, err := restartedStore.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	beforeRollbackState, err := world.DecodeState(beforeRollback.State)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rollbackConnector.RunNPCCombatPhase(ctx, failedCommandID, 6, 120); err == nil {
		t.Fatal("receipt constraint failure was ignored")
	}
	afterRollback, err := restartedStore.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	afterRollbackState, err := world.DecodeState(afterRollback.State)
	if err != nil {
		t.Fatal(err)
	}
	if afterRollback.Revision != beforeRollback.Revision || !reflect.DeepEqual(afterRollbackState, beforeRollbackState) || !bytes.Equal(afterRollback.State, beforeRollback.State) {
		t.Fatalf("failed combat changed world before=%d/%s after=%d/%s", beforeRollback.Revision, beforeRollback.State, afterRollback.Revision, afterRollback.State)
	}
	var failedReceiptCount int
	if err := db2.QueryRowContext(ctx, `SELECT count(*) FROM mud_go.world_commands WHERE world_id=$1 AND command_id=$2`, worldID, failedCommandID).Scan(&failedReceiptCount); err != nil {
		t.Fatal(err)
	}
	if failedReceiptCount != 0 {
		t.Fatalf("failed combat left %d receipt rows", failedReceiptCount)
	}

	if _, err := db2.ExecContext(ctx, fmt.Sprintf("ALTER TABLE mud_go.world_commands DROP CONSTRAINT %s", constraintName)); err != nil {
		t.Fatal(err)
	}
	constraintAdded = false
	retried, err := rollbackConnector.RunNPCCombatPhase(ctx, failedCommandID, 6, 120)
	if err != nil || retried.Replayed || retried.Revision != beforeRollback.Revision+1 {
		t.Fatalf("retry after rollback=%+v err=%v", retried, err)
	}
}
