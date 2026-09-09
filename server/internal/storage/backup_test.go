package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"reflect"
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
	if err := store.RestoreWorldBackup(ctx, backup, WorldBackupOptions{ExpectedRevision: &expected}); !errors.Is(err, ErrWorldConflict) {
		t.Fatalf("receipt-bearing restore err=%v", err)
	}
	if err := store.RestoreWorldBackup(ctx, backup, WorldBackupOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
	restored, err := store.LoadWorld(ctx, worldID)
	if err != nil || restored.Revision != backup.Revision || string(restored.State) != string(backup.State) {
		t.Fatalf("restored=%+v err=%v", restored, err)
	}
	var receipts int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM mud_go.world_commands WHERE world_id=$1`, worldID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if receipts != 0 {
		t.Fatalf("force restore retained %d receipts", receipts)
	}
}
