package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func backupFixtureSnapshot() WorldSnapshot {
	return WorldSnapshot{
		Revision: 7,
		State:    json.RawMessage("{\n  \"Version\": 1,\n  \"Rooms\": {},\n  \"Players\": {}\n}"),
	}
}

func equivalentJSON(a, b []byte) bool {
	var left, right any
	return json.Unmarshal(a, &left) == nil && json.Unmarshal(b, &right) == nil && reflect.DeepEqual(left, right)
}

func TestWorldBackupIsDeterministicAndChecksummed(t *testing.T) {
	backup, err := NewWorldBackup("backup-world", backupFixtureSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	first, err := backup.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	second, err := backup.Marshal()
	if err != nil || string(first) != string(second) {
		t.Fatalf("marshal is not deterministic: %s / %s / %v", first, second, err)
	}
	decoded, err := ParseWorldBackup(first)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, backup) {
		t.Fatalf("decoded=%+v backup=%+v", decoded, backup)
	}

	var object map[string]any
	if err := json.Unmarshal(first, &object); err != nil {
		t.Fatal(err)
	}
	object["state_sha256"] = "0000000000000000000000000000000000000000000000000000000000000000"
	tampered, err := json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseWorldBackup(tampered); err == nil {
		t.Fatal("tampered checksum accepted")
	}
	if _, err := ParseWorldBackup(append(first, []byte(` {}`)...)); err == nil {
		t.Fatal("trailing JSON accepted")
	}
	if _, err := ParseWorldBackup(append(first[:len(first)-1], []byte(`,"extra":true}`)...)); err == nil {
		t.Fatal("unknown backup field accepted")
	}
}

func TestWorldBackupRejectsInvalidStateBeforeRestore(t *testing.T) {
	backup := WorldBackup{
		Format:      WorldBackupFormat,
		Version:     WorldBackupVersion,
		WorldID:     "invalid-world",
		Revision:    0,
		State:       json.RawMessage(`{"Version":99}`),
		StateSHA256: stateSHA256([]byte(`{"Version":99}`)),
	}
	if err := backup.Validate(); err == nil {
		t.Fatal("invalid state accepted")
	}
}

func TestNewWorldBackupRejectsInvalidIdentityAndState(t *testing.T) {
	if _, err := NewWorldBackup("", backupFixtureSnapshot()); err == nil {
		t.Fatal("empty world ID accepted")
	}
	if _, err := NewWorldBackup(strings.Repeat("w", 129), backupFixtureSnapshot()); err == nil {
		t.Fatal("overlong world ID accepted")
	}
	negative := backupFixtureSnapshot()
	negative.Revision = -1
	if _, err := NewWorldBackup("backup-world", negative); err == nil {
		t.Fatal("negative revision accepted")
	}
	arrayState := backupFixtureSnapshot()
	arrayState.State = json.RawMessage(`[]`)
	if _, err := NewWorldBackup("backup-world", arrayState); err == nil {
		t.Fatal("array state accepted")
	}
	emptyState := backupFixtureSnapshot()
	emptyState.State = json.RawMessage(``)
	if _, err := NewWorldBackup("backup-world", emptyState); err == nil {
		t.Fatal("empty state accepted")
	}
	if _, err := ParseWorldBackup(nil); err == nil {
		t.Fatal("empty backup accepted")
	}
	if _, err := ParseWorldBackup([]byte(`{`)); err == nil {
		t.Fatal("truncated backup accepted")
	}
}

func TestRestoreWorldBackupRejectsNilStore(t *testing.T) {
	backup, err := NewWorldBackup("backup-world", backupFixtureSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	var store *Postgres
	if err := store.RestoreWorldBackup(context.Background(), backup, WorldBackupOptions{Force: true}); err == nil {
		t.Fatal("nil store restore accepted")
	}
	empty := &Postgres{}
	if err := empty.RestoreWorldBackup(context.Background(), backup, WorldBackupOptions{Force: true}); err == nil {
		t.Fatal("nil db restore accepted")
	}
}

func TestAdmitRestoreWriterAllowsUnboundOperatorAndFencesRetiredHandle(t *testing.T) {
	unbound := &Postgres{}
	if err := unbound.admitRestoreWriter("w", 4); err != nil {
		t.Fatalf("operator restore fenced: %v", err)
	}
	current := &Postgres{writerWorld: "w", writerEpoch: 4}
	if err := current.admitRestoreWriter("w", 4); err != nil {
		t.Fatal(err)
	}
	retired := &Postgres{writerWorld: "w", writerEpoch: 3}
	if err := retired.admitRestoreWriter("w", 4); !errors.Is(err, ErrWriterFenced) {
		t.Fatalf("retired writer restore err=%v", err)
	}
	wrongWorld := &Postgres{writerWorld: "other", writerEpoch: 4}
	if err := wrongWorld.admitRestoreWriter("w", 4); !errors.Is(err, ErrWriterFenced) {
		t.Fatalf("wrong-world writer restore err=%v", err)
	}
}

func TestPostgresWorldBackupRestoreFencesReceipts(t *testing.T) {
	dsn := os.Getenv("MUHAN_BACKUP_TEST_DATABASE_URL")
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
	store := NewPostgres(db)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	worldID := "backup-world-" + time.Now().Format("20060102150405.000000000")
	initial := backupFixtureSnapshot()
	if err := store.CreateWorld(ctx, worldID, initial.State); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := NewWorldBackup(worldID, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RestoreWorldBackup(ctx, backup, WorldBackupOptions{}); !errors.Is(err, ErrWorldConflict) {
		t.Fatalf("restore without expected revision err=%v", err)
	}
	expected := snapshot.Revision
	if err := store.RestoreWorldBackup(ctx, backup, WorldBackupOptions{ExpectedRevision: &expected}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CommitWorldCommand(ctx, worldID, "backup-receipt", json.RawMessage(`{"actor":"a"}`), 0, json.RawMessage(`{"Version":1,"Rooms":{},"Players":{}}`), json.RawMessage(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	withReceipt, err := store.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RestoreWorldBackup(ctx, backup, WorldBackupOptions{ExpectedRevision: &withReceipt.Revision}); !errors.Is(err, ErrWorldRestoreConflict) {
		t.Fatalf("receipt-bearing restore err=%v", err)
	}
	if err := store.RestoreWorldBackup(ctx, backup, WorldBackupOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
	restored, err := store.LoadWorld(ctx, worldID)
	restoredState, canonicalErr := canonicalJSON(restored.State)
	if err != nil || canonicalErr != nil || restored.Revision != backup.Revision || string(restoredState) != string(backup.State) {
		t.Fatalf("restored=%+v err=%v", restored, err)
	}
	var receipts int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM mud_go.world_commands WHERE world_id=$1`, worldID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if receipts != 0 {
		t.Fatalf("force restore retained %d receipts", receipts)
	}
	var epoch int64
	if err := db.QueryRowContext(ctx, `SELECT writer_epoch FROM mud_go.worlds WHERE id=$1`, worldID).Scan(&epoch); err != nil {
		t.Fatal(err)
	}
	if epoch != 1 {
		t.Fatalf("force restore writer epoch=%d, want 1", epoch)
	}
	if _, err := store.CommitWorldCommand(ctx, worldID, "after-force", json.RawMessage(`{"actor":"a"}`), 0, json.RawMessage(`{"Version":1,"Rooms":{},"Players":{}}`), json.RawMessage(`{"ok":true}`)); !errors.Is(err, ErrWriterFenced) {
		t.Fatalf("pre-restore writer remained usable: %v", err)
	}

	writer, err := store.ClaimWorldWriter(ctx, worldID, "backup-claim-"+worldID)
	if err != nil || writer.writerEpoch != 2 {
		t.Fatalf("claim after force restore writer=%+v err=%v", writer, err)
	}
	if err := store.RestoreWorldBackup(ctx, backup, WorldBackupOptions{Force: true}); err != nil {
		t.Fatalf("unbound operator force restore after writer claim: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT writer_epoch FROM mud_go.worlds WHERE id=$1`, worldID).Scan(&epoch); err != nil {
		t.Fatal(err)
	}
	if epoch != 3 {
		t.Fatalf("operator force restore writer epoch=%d, want 3", epoch)
	}
	postClaimState := json.RawMessage(`{"Version":1,"Rooms":{},"Players":{}}`)
	postClaimResponse := json.RawMessage(`{"ok":true}`)
	if _, err := writer.CommitWorldCommand(ctx, worldID, "after-claim-force", json.RawMessage(`{"actor":"a"}`), backup.Revision, postClaimState, postClaimResponse); !errors.Is(err, ErrWriterFenced) {
		t.Fatalf("claimed writer survived operator force restore: %v", err)
	}
	if err := writer.RestoreWorldBackup(ctx, backup, WorldBackupOptions{Force: true}); !errors.Is(err, ErrWriterFenced) {
		t.Fatalf("retired writer restored after fencing: %v", err)
	}
	successor, err := store.ClaimWorldWriter(ctx, worldID, "backup-claim-successor-"+worldID)
	if err != nil || successor.writerEpoch != 4 {
		t.Fatalf("successor writer=%+v err=%v", successor, err)
	}
	request := json.RawMessage(`{"actor":"a"}`)
	first, err := successor.CommitWorldCommand(ctx, worldID, "post-operator-restore", request, backup.Revision, postClaimState, postClaimResponse)
	if err != nil || first.Revision != backup.Revision+1 || first.Replayed {
		t.Fatalf("post-restore receipt=%+v err=%v", first, err)
	}
	replay, err := successor.CommitWorldCommand(ctx, worldID, "post-operator-restore", request, backup.Revision, postClaimState, postClaimResponse)
	if err != nil || !replay.Replayed || replay.Revision != first.Revision {
		t.Fatalf("duplicate command lost or reapplied: %+v err=%v", replay, err)
	}
	if _, err := successor.CommitWorldCommand(ctx, worldID, "post-operator-restore", json.RawMessage(`{"actor":"tampered"}`), backup.Revision, postClaimState, postClaimResponse); !errors.Is(err, ErrCommandConflict) {
		t.Fatalf("duplicate command identity accepted: %v", err)
	}
}

// TestPostgresWorldBackupPhysicalRestore is driven in two phases by
// scripts/run-go-backup-restore-local.sh. The shell harness owns the actual
// pg_dump/pg_restore boundary; this test owns the Go receipt and writer
// contracts on either side of that boundary.
func TestPostgresWorldBackupPhysicalRestore(t *testing.T) {
	role := os.Getenv("MUHAN_BACKUP_PHYSICAL_RESTORE_ROLE")
	if role == "" || os.Getenv("MUHAN_BACKUP_PHYSICAL_RESTORE_ALLOW_DISPOSABLE") != "1" {
		t.Skip("disposable PostgreSQL backup/restore harness required")
	}
	dsn := os.Getenv("MUHAN_BACKUP_PHYSICAL_RESTORE_DATABASE_URL")
	worldID := os.Getenv("MUHAN_BACKUP_PHYSICAL_RESTORE_WORLD_ID")
	commandOne := os.Getenv("MUHAN_BACKUP_PHYSICAL_RESTORE_COMMAND_ONE")
	commandTwo := os.Getenv("MUHAN_BACKUP_PHYSICAL_RESTORE_COMMAND_TWO")
	claimID := os.Getenv("MUHAN_BACKUP_PHYSICAL_RESTORE_CLAIM_ID")
	if dsn == "" || worldID == "" || commandOne == "" || commandTwo == "" || claimID == "" {
		t.Fatal("physical backup/restore test requires database, world, command, and claim identifiers")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store := NewPostgres(db)
	stateOne := json.RawMessage(`{"Version":1,"Rooms":{"1":{"Name":"one"}},"Players":{}}`)
	stateTwo := json.RawMessage(`{"Version":1,"Rooms":{"2":{"Name":"two"}},"Players":{}}`)
	requestOne := json.RawMessage(`{"actor":"backup-test","command":"one"}`)
	requestTwo := json.RawMessage(`{"actor":"backup-test","command":"two"}`)
	responseOne := json.RawMessage(`{"text":"one"}`)
	responseTwo := json.RawMessage(`{"text":"two"}`)

	switch role {
	case "seed":
		if err := store.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		if err := store.CreateWorld(ctx, worldID, json.RawMessage(`{"Version":1,"Rooms":{},"Players":{}}`)); err != nil {
			t.Fatal(err)
		}
		writer, err := store.ClaimWorldWriter(ctx, worldID, claimID)
		if err != nil {
			t.Fatal(err)
		}
		first, err := writer.CommitWorldCommand(ctx, worldID, commandOne, requestOne, 0, stateOne, responseOne)
		if err != nil || first.Revision != 1 || first.Replayed {
			t.Fatalf("first receipt=%+v err=%v", first, err)
		}
		second, err := writer.CommitWorldCommand(ctx, worldID, commandTwo, requestTwo, 1, stateTwo, responseTwo)
		if err != nil || second.Revision != 2 || second.Replayed {
			t.Fatalf("second receipt=%+v err=%v", second, err)
		}
	case "verify":
		snapshot, err := store.LoadWorld(ctx, worldID)
		if err != nil {
			t.Fatal(err)
		}
		var gotState, wantState any
		if json.Unmarshal(snapshot.State, &gotState) != nil || json.Unmarshal(stateTwo, &wantState) != nil || !reflect.DeepEqual(gotState, wantState) || snapshot.Revision != 2 {
			t.Fatalf("restored snapshot=%+v, want revision 2/state two", snapshot)
		}
		var epoch int64
		if err := db.QueryRowContext(ctx, `SELECT writer_epoch FROM mud_go.worlds WHERE id=$1`, worldID).Scan(&epoch); err != nil {
			t.Fatal(err)
		}
		if epoch != 1 {
			t.Fatalf("restored writer epoch=%d, want 1", epoch)
		}
		writer, err := store.ClaimWorldWriter(ctx, worldID, claimID+"-restored")
		if err != nil {
			t.Fatal(err)
		}
		if writer.writerEpoch != 2 {
			t.Fatalf("new writer epoch=%d, want 2", writer.writerEpoch)
		}
		stale := &Postgres{db: db, writerWorld: worldID, writerEpoch: epoch}
		if _, err := stale.CommitWorldCommand(ctx, worldID, "stale-after-restore", json.RawMessage(`{"actor":"stale"}`), 2, stateTwo, responseTwo); !errors.Is(err, ErrWriterFenced) {
			t.Fatalf("stale writer was not fenced: %v", err)
		}
		first, err := writer.CommitWorldCommand(ctx, worldID, commandOne, requestOne, 0, stateTwo, json.RawMessage(`{"text":"wrong"}`))
		if err != nil || !first.Replayed || first.Revision != 1 || !equivalentJSON(first.Response, responseOne) {
			t.Fatalf("first receipt replay=%+v err=%v", first, err)
		}
		second, err := writer.CommitWorldCommand(ctx, worldID, commandTwo, requestTwo, 1, stateOne, json.RawMessage(`{"text":"wrong"}`))
		if err != nil || !second.Replayed || second.Revision != 2 || !equivalentJSON(second.Response, responseTwo) {
			t.Fatalf("second receipt replay=%+v err=%v", second, err)
		}
		if _, err := writer.CommitWorldCommand(ctx, worldID, commandOne, json.RawMessage(`{"actor":"tampered"}`), 0, stateOne, responseOne); !errors.Is(err, ErrCommandConflict) {
			t.Fatalf("receipt request conflict err=%v", err)
		}
		third, err := writer.CommitWorldCommand(ctx, worldID, "post-restore", json.RawMessage(`{"actor":"backup-test","command":"three"}`), 2, json.RawMessage(`{"Version":1,"Rooms":{"3":{"Name":"three"}},"Players":{}}`), json.RawMessage(`{"text":"three"}`))
		if err != nil || third.Revision != 3 || third.Replayed {
			t.Fatalf("post-restore receipt=%+v err=%v", third, err)
		}
	default:
		t.Fatalf("unknown physical backup/restore role %q", role)
	}
}
