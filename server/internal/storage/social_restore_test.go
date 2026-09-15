package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestSocialRestoreRejectsInvalidReadContracts(t *testing.T) {
	if _, err := (&Postgres{}).ReadFamilyLedgerEvidence(context.Background(), "../raw"); !errors.Is(err, ErrSocialRestore) {
		t.Fatalf("path-like world ID err=%v", err)
	}
	if _, err := (&Postgres{}).RestoreSocialState(context.Background(), SocialRestoreOptions{WorldID: "world", ExpectedRevision: -1}); !errors.Is(err, ErrSocialRestore) {
		t.Fatalf("negative revision err=%v", err)
	}
	if _, err := (*Postgres)(nil).ReadCharacterMemosEvidence(context.Background(), "world"); !errors.Is(err, ErrSocialRestore) {
		t.Fatalf("nil store err=%v", err)
	}
}

func TestPostgresSocialEvidenceReadAndRestore(t *testing.T) {
	dsn := os.Getenv("MUHAN_SOCIAL_IMPORT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL URL required")
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
	worldID := fmt.Sprintf("social-restore-%d", time.Now().UnixNano())
	state := socialImportState()
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateWorld(ctx, worldID, raw); err != nil {
		t.Fatal(err)
	}
	familyInput := FamilyLedgerImport{
		WorldID: worldID, CommandID: "restore-family", ExpectedRevision: 0,
		Family:  world.FamilyState{Members: map[int16][]world.FamilyMember{1: {{ID: "actor", Name: "Alice", Class: 4}}}},
		Catalog: world.FamilyCatalog{Families: map[int16]world.FamilyDefinition{1: {ID: 1, Name: "청룡", Boss: "Alice", Fee: 3}}},
	}
	familyResult, err := store.ImportFamilyLedger(ctx, familyInput)
	if err != nil || familyResult.Revision != 1 {
		t.Fatalf("family import=%+v err=%v", familyResult, err)
	}
	memoResult, err := store.ImportCharacterMemos(ctx, CharacterMemosImport{
		WorldID: worldID, CommandID: "restore-memos", ExpectedRevision: 1,
		Memos: map[string][]world.CharacterMemo{"target": {{ID: "memo-restore", SenderID: "actor", SenderName: "Alice", Body: "hello", CreatedAt: time.Unix(10, 0)}}},
	})
	if err != nil || memoResult.Revision != 2 {
		t.Fatalf("memo import=%+v err=%v", memoResult, err)
	}

	restored, err := store.RestoreSocialState(ctx, SocialRestoreOptions{WorldID: worldID, ExpectedRevision: 2, RequireFamily: true, RequireMemos: true})
	if err != nil {
		t.Fatal(err)
	}
	if restored.Revision != 2 || restored.Family == nil || restored.Memos == nil || restored.State.Family == nil || restored.State.Memos == nil {
		t.Fatalf("restore result=%+v", restored)
	}
	if restored.State.Family.Members[1][0].ID != "actor" || restored.State.Memos["target"][0].Body != "hello" {
		t.Fatalf("restored snapshot social state=%+v/%+v", restored.State.Family, restored.State.Memos)
	}
	familyEvidence, err := store.ReadFamilyLedgerEvidence(ctx, worldID)
	if err != nil || familyEvidence.SnapshotRevision != 2 || familyEvidence.ImportedRevision != 1 || familyEvidence.MemberCount != 1 {
		t.Fatalf("family evidence=%+v err=%v", familyEvidence, err)
	}
	memoEvidence, err := store.ReadCharacterMemosEvidence(ctx, worldID)
	if err != nil || memoEvidence.SnapshotRevision != 2 || memoEvidence.ImportedRevision != 2 || memoEvidence.MemoCount != 1 {
		t.Fatalf("memo evidence=%+v err=%v", memoEvidence, err)
	}
	if _, err := store.RestoreSocialState(ctx, SocialRestoreOptions{WorldID: worldID, ExpectedRevision: 1, RequireFamily: true}); !errors.Is(err, ErrWorldConflict) {
		t.Fatalf("stale expected revision err=%v", err)
	}

	// A later authoritative snapshot may legitimately diverge from imported
	// evidence. Recovery must return it and never overwrite it with evidence.
	restored.State.Rooms[1] = func(room world.RoomState) world.RoomState {
		room.Resource.Name = "복구 후 광장"
		return room
	}(restored.State.Rooms[1])
	updatedRaw, err := json.Marshal(restored.State)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CommitWorldCommand(ctx, worldID, "restore-later", json.RawMessage(`{"actor":"recovery"}`), 2, updatedRaw, json.RawMessage(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	later, err := store.RestoreSocialState(ctx, SocialRestoreOptions{WorldID: worldID, ExpectedRevision: 3, RequireFamily: true, RequireMemos: true})
	if err != nil || later.State.Rooms[1].Resource.Name != "복구 후 광장" {
		t.Fatalf("later authoritative snapshot=%+v err=%v", later.State.Rooms[1].Resource.Name, err)
	}
	var revision int64
	if err := db.QueryRowContext(ctx, `SELECT revision FROM mud_go.worlds WHERE id=$1`, worldID).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if revision != 3 {
		t.Fatalf("restore mutated world revision=%d", revision)
	}

	// Evidence tampering is not repaired or ignored.
	if _, err := db.ExecContext(ctx, `UPDATE mud_go.family_imports SET member_count=member_count+1 WHERE world_id=$1`, worldID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadFamilyLedgerEvidence(ctx, worldID); !errors.Is(err, ErrSocialRestoreConflict) {
		t.Fatalf("tampered evidence err=%v", err)
	}
}

func TestPostgresSocialRestoreHonorsWriterFence(t *testing.T) {
	dsn := os.Getenv("MUHAN_SOCIAL_IMPORT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL URL required")
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
	worldID := fmt.Sprintf("social-fence-%d", time.Now().UnixNano())
	raw, _ := json.Marshal(socialImportState())
	if err := store.CreateWorld(ctx, worldID, raw); err != nil {
		t.Fatal(err)
	}
	writer, err := store.ClaimWorldWriter(ctx, worldID, "restore-writer")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RestoreSocialState(ctx, SocialRestoreOptions{WorldID: worldID, ExpectedRevision: 0}); !errors.Is(err, ErrWriterFenced) {
		t.Fatalf("stale reader was not fenced: %v", err)
	}
	if _, err := writer.RestoreSocialState(ctx, SocialRestoreOptions{WorldID: worldID, ExpectedRevision: 0}); err != nil {
		t.Fatalf("current writer read err=%v", err)
	}
}
