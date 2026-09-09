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

func socialImportState() world.State {
	return world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
			PlayerIDs: []string{"actor"},
		}},
		Players: map[string]world.PlayerState{
			"actor":  {Body: world.LegacyMonster{Name: "Alice", Type: 0, Class: 4, RoomID: 1}, Online: true},
			"target": {Body: world.LegacyMonster{Name: "Bob", Type: 0, Class: 5, RoomID: 1}, Online: false},
		},
	}
}

func TestNormalizeSocialImportsRejectUnresolvedAndSensitiveInputs(t *testing.T) {
	if _, err := normalizeMemosImport(CharacterMemosImport{WorldID: "w", CommandID: "c"}); !errors.Is(err, ErrCharacterMemosImport) {
		t.Fatalf("nil memos err=%v", err)
	}
	if _, err := normalizeMemosImport(CharacterMemosImport{WorldID: "w", CommandID: "c", Memos: map[string][]world.CharacterMemo{}, RawPath: "/player/fal/Alice"}); err == nil {
		t.Fatal("raw path accepted")
	}
	family := world.FamilyState{Members: map[int16][]world.FamilyMember{1: {{ID: "actor", Name: "Alice", Class: 4}}}}
	catalog := world.FamilyCatalog{Families: map[int16]world.FamilyDefinition{1: {ID: 1, Name: "청룡", Boss: "Alice", Fee: 3}}}
	if _, err := normalizeFamilyLedgerImport(FamilyLedgerImport{WorldID: "w", CommandID: "c", Family: family, Catalog: catalog, CredentialHash: []byte("not-allowed")}); err == nil {
		t.Fatal("credential accepted")
	}
}

func TestPostgresImportsSocialAggregatesAndReplays(t *testing.T) {
	dsn := os.Getenv("MUHAN_SOCIAL_IMPORT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL URL required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	repo := NewPostgres(db)
	if err := repo.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	worldID := fmt.Sprintf("social-import-%d", time.Now().UnixNano())
	state := socialImportState()
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateWorld(ctx, worldID, raw); err != nil {
		t.Fatal(err)
	}
	family := FamilyLedgerImport{
		WorldID: worldID, CommandID: "family-import-1", ExpectedRevision: 0,
		Family:  world.FamilyState{Members: map[int16][]world.FamilyMember{1: {{ID: "actor", Name: "Alice", Class: 4}}}},
		Catalog: world.FamilyCatalog{Families: map[int16]world.FamilyDefinition{1: {ID: 1, Name: "청룡", Boss: "Alice", Fee: 3}}},
	}
	first, err := repo.ImportFamilyLedger(ctx, family)
	if err != nil || first.Replayed || first.Revision != 1 || first.MemberCount != 1 {
		t.Fatalf("family result=%+v err=%v", first, err)
	}
	replayed, err := repo.ImportFamilyLedger(ctx, family)
	if err != nil || !replayed.Replayed || replayed.Revision != 1 {
		t.Fatalf("family replay=%+v err=%v", replayed, err)
	}
	changed := family
	changed.CommandID = "family-import-2"
	changed.ExpectedRevision = 1
	if _, err := repo.ImportFamilyLedger(ctx, changed); !errors.Is(err, ErrFamilyLedgerImportConflict) {
		t.Fatalf("second aggregate err=%v", err)
	}
	stateAfter, err := repo.LoadWorld(ctx, worldID)
	if err != nil || stateAfter.Revision != 1 {
		t.Fatalf("family replay changed snapshot=%+v err=%v", stateAfter, err)
	}

	memoWorldID := fmt.Sprintf("memo-import-%d", time.Now().UnixNano())
	if err := repo.CreateWorld(ctx, memoWorldID, raw); err != nil {
		t.Fatal(err)
	}
	memo := CharacterMemosImport{WorldID: memoWorldID, CommandID: "memo-import-1", ExpectedRevision: 0, Memos: map[string][]world.CharacterMemo{
		"target": {{ID: "memo-1", SenderID: "actor", SenderName: "Alice", Body: "hello", CreatedAt: time.Unix(10, 0)}},
	}}
	gotMemo, err := repo.ImportCharacterMemos(ctx, memo)
	if err != nil || gotMemo.Replayed || gotMemo.Revision != 1 || gotMemo.MemoCount != 1 {
		t.Fatalf("memo result=%+v err=%v", gotMemo, err)
	}
	gotMemoReplay, err := repo.ImportCharacterMemos(ctx, memo)
	if err != nil || !gotMemoReplay.Replayed {
		t.Fatalf("memo replay=%+v err=%v", gotMemoReplay, err)
	}
	loaded, err := repo.LoadWorld(ctx, memoWorldID)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := world.DecodeState(loaded.State)
	if err != nil || len(decoded.Memos["target"]) != 1 || decoded.Memos["target"][0].Body != "hello" {
		t.Fatalf("memo state=%+v err=%v", decoded.Memos, err)
	}
}

func TestPostgresSocialImportRollsBackSnapshotAndEvidence(t *testing.T) {
	dsn := os.Getenv("MUHAN_SOCIAL_IMPORT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL URL required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	repo := NewPostgres(db)
	if err := repo.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	worldID := fmt.Sprintf("social-rollback-%d", time.Now().UnixNano())
	state := socialImportState()
	raw, _ := json.Marshal(state)
	if err := repo.CreateWorld(ctx, worldID, raw); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE mud_go.family_members ADD CONSTRAINT social_import_test_reject CHECK(character_class <> 4) NOT VALID`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = db.ExecContext(ctx, `ALTER TABLE mud_go.family_members DROP CONSTRAINT social_import_test_reject`)
	}()
	input := FamilyLedgerImport{WorldID: worldID, CommandID: "family-rollback", ExpectedRevision: 0,
		Family:  world.FamilyState{Members: map[int16][]world.FamilyMember{1: {{ID: "actor", Name: "Alice", Class: 4}}}},
		Catalog: world.FamilyCatalog{Families: map[int16]world.FamilyDefinition{1: {ID: 1, Name: "청룡", Boss: "Alice", Fee: 3}}},
	}
	if _, err := repo.ImportFamilyLedger(ctx, input); err == nil {
		t.Fatal("injected evidence failure ignored")
	}
	loaded, err := repo.LoadWorld(ctx, worldID)
	if err != nil || loaded.Revision != 0 {
		t.Fatalf("rollback revision=%+v err=%v", loaded, err)
	}
	decoded, err := world.DecodeState(loaded.State)
	if err != nil || decoded.Family != nil {
		t.Fatalf("rollback family=%#v err=%v", decoded.Family, err)
	}
	var evidence, commands int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM mud_go.family_imports WHERE world_id=$1`, worldID).Scan(&evidence); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM mud_go.world_commands WHERE world_id=$1`, worldID).Scan(&commands); err != nil {
		t.Fatal(err)
	}
	if evidence != 0 || commands != 0 {
		t.Fatalf("orphan evidence=%d commands=%d", evidence, commands)
	}
}
