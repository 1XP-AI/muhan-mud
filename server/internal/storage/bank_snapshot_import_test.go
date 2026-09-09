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

func bankSnapshotImportFixture(t *testing.T) []byte {
	t.Helper()
	var root, item world.PlayerSnapshotObjectV1
	root.Value = 1234
	root.Name[0] = 'b'
	root.Name[1] = 'a'
	root.Name[2] = 'n'
	root.Name[3] = 'k'
	root.Name[4] = 0
	item.Name[0] = 'r'
	item.Name[1] = 'i'
	item.Name[2] = 'n'
	item.Name[3] = 'g'
	item.Name[4] = 0
	snapshot, err := world.EncodeBankSnapshotV1(world.BankSnapshotV1{Root: world.PlayerSnapshotObjectGraphV1{Nodes: []world.PlayerSnapshotObjectNodeV1{
		{Object: root, ChildIndex: 0},
		{Object: item, ParentIndex: func() *uint32 { value := uint32(0); return &value }(), ChildIndex: 0},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestNormalizeBankSnapshotImportRequiresExplicitLinkedShape(t *testing.T) {
	snapshot := bankSnapshotImportFixture(t)
	normalized, err := normalizeBankSnapshotImport(BankSnapshotImport{
		WorldID: "w", CommandID: "c", ExpectedRevision: 0, AccountName: "Alice", PlayerID: "p", Snapshot: snapshot, ItemIDs: []string{"item-1"},
	})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if normalized.accountName != "Alice" || len(normalized.input.ItemIDs) != 1 || normalized.request.Kind != "import-bank-snapshot-v1" {
		t.Fatalf("normalized=%+v", normalized)
	}
	for _, changed := range []BankSnapshotImport{
		{WorldID: "w", CommandID: "c", AccountName: "Alice", PlayerID: "p", Snapshot: snapshot},
		{WorldID: "w", CommandID: "c", AccountName: "Alice", PlayerID: "p", Snapshot: snapshot, ItemIDs: []string{"item-1", "item-2"}},
		{WorldID: "w", CommandID: "c", AccountName: "Alice", PlayerID: "p", Snapshot: snapshot, ItemIDs: []string{"item-1", "item-1"}},
	} {
		if _, err := normalizeBankSnapshotImport(changed); !errors.Is(err, ErrBankSnapshotImport) {
			t.Fatalf("invalid manifest accepted: input=%+v err=%v", changed, err)
		}
	}
	bad := append([]byte(nil), snapshot...)
	bad[len(bad)-1] ^= 1
	if _, err := normalizeBankSnapshotImport(BankSnapshotImport{WorldID: "w", CommandID: "c", AccountName: "Alice", PlayerID: "p", Snapshot: bad, ItemIDs: []string{"item-1"}}); !errors.Is(err, ErrBankSnapshotImport) {
		t.Fatalf("digest-corrupt snapshot accepted: %v", err)
	}
}

func TestPostgresImportsBankSnapshotAtomicallyAndReplays(t *testing.T) {
	dsn := os.Getenv("MUHAN_BANK_SNAPSHOT_IMPORT_TEST_DATABASE_URL")
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
	worldID := fmt.Sprintf("bank-snapshot-import-%d", time.Now().UnixNano())
	flags := [8]byte{}
	flags[world.RoomBankFlag/8] = 1 << uint(world.RoomBankFlag%8)
	state := world.State{
		Version: 1,
		Rooms:   map[int16]world.RoomState{1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "은행", Flags: flags}}, Items: &world.ItemCollection{Items: map[string]world.Item{}}}},
		Players: map[string]world.PlayerState{"player-1": {Body: world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 1}, Items: &world.ItemCollection{Items: map[string]world.Item{}}}},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateWorld(ctx, worldID, raw); err != nil {
		t.Fatal(err)
	}
	var accountID string
	if err := db.QueryRowContext(ctx, `INSERT INTO mud_go.accounts(name,credential_hash) VALUES('Alice',decode('616263','hex')) RETURNING id`).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO mud_go.characters(account_id,stage,draft,world_id,world_player_id) VALUES($1,'linked','{}'::jsonb,$2,'player-1')`, accountID, worldID); err != nil {
		t.Fatal(err)
	}
	snapshot := bankSnapshotImportFixture(t)
	input := BankSnapshotImport{WorldID: worldID, CommandID: "bank-import-1", AccountName: "Alice", PlayerID: "player-1", Snapshot: snapshot, ItemIDs: []string{"bank-item-1"}}
	first, err := repo.ImportBankSnapshot(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if first.Replayed || first.PlayerID != input.PlayerID || first.Balance != 1234 || first.ItemCount != 1 {
		t.Fatalf("first=%+v", first)
	}
	loaded, err := repo.LoadWorld(ctx, worldID)
	if err != nil || loaded.Revision != 1 {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
	loadedState, err := world.DecodeState(loaded.State)
	if err != nil {
		t.Fatal(err)
	}
	account, ok := loadedState.BankAccounts[input.PlayerID]
	if !ok || account.Balance != 1234 || account.Items == nil || len(account.Items.Inventory) != 1 || account.Items.Inventory[0] != "bank-item-1" {
		t.Fatalf("bank account=%+v ok=%v", account, ok)
	}
	replay, err := repo.ImportBankSnapshot(ctx, input)
	if err != nil || !replay.Replayed || replay.Balance != first.Balance || replay.SourceSHA256 != first.SourceSHA256 {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	if after, err := repo.LoadWorld(ctx, worldID); err != nil || after.Revision != 1 {
		t.Fatalf("replay changed world=%+v err=%v", after, err)
	}
	changed := input
	changed.CommandID = "bank-import-2"
	changed.ExpectedRevision = 1
	changed.ItemIDs = []string{"different-item"}
	if _, err := repo.ImportBankSnapshot(ctx, changed); !errors.Is(err, ErrBankSnapshotImportConflict) {
		t.Fatalf("second bank import accepted: %v", err)
	}
	var evidenceCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM mud_go.bank_imports WHERE world_id=$1`, worldID).Scan(&evidenceCount); err != nil || evidenceCount != 1 {
		t.Fatalf("bank evidence count=%d err=%v", evidenceCount, err)
	}
}

func TestPostgresBankSnapshotImportRollsBackWorldEvidenceAndReceipt(t *testing.T) {
	dsn := os.Getenv("MUHAN_BANK_SNAPSHOT_IMPORT_TEST_DATABASE_URL")
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
	worldID := fmt.Sprintf("bank-snapshot-rollback-%d", time.Now().UnixNano())
	accountName := fmt.Sprintf("Rb%010d", time.Now().UnixNano()%10000000000)
	state := world.State{
		Version: 1,
		Rooms:   map[int16]world.RoomState{1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}}, Items: &world.ItemCollection{Items: map[string]world.Item{}}}},
		Players: map[string]world.PlayerState{"player-1": {Body: world.LegacyMonster{Name: accountName, Type: 0, RoomID: 1}, Items: &world.ItemCollection{Items: map[string]world.Item{}}}},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateWorld(ctx, worldID, raw); err != nil {
		t.Fatal(err)
	}
	var accountID string
	if err := db.QueryRowContext(ctx, `INSERT INTO mud_go.accounts(name,credential_hash) VALUES($1,decode('646566','hex')) RETURNING id`, accountName).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO mud_go.characters(account_id,stage,draft,world_id,world_player_id) VALUES($1,'linked','{}'::jsonb,$2,'player-1')`, accountID, worldID); err != nil {
		t.Fatal(err)
	}
	constraint := fmt.Sprintf("reject_bank_snapshot_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE mud_go.bank_imports ADD CONSTRAINT %s CHECK(item_count <> 1) NOT VALID`, constraint)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), fmt.Sprintf("ALTER TABLE mud_go.bank_imports DROP CONSTRAINT IF EXISTS %s", constraint))
	})

	_, err = repo.ImportBankSnapshot(ctx, BankSnapshotImport{
		WorldID: worldID, CommandID: "bank-rollback-1", AccountName: accountName, PlayerID: "player-1",
		Snapshot: bankSnapshotImportFixture(t), ItemIDs: []string{"bank-item-1"},
	})
	if err == nil {
		t.Fatal("constraint failure did not reject bank import")
	}
	loaded, err := repo.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Revision != 0 {
		t.Fatalf("failed import advanced revision: %d", loaded.Revision)
	}
	loadedState, err := world.DecodeState(loaded.State)
	if err != nil {
		t.Fatal(err)
	}
	if len(loadedState.BankAccounts) != 0 {
		t.Fatalf("failed import left bank accounts: %+v", loadedState.BankAccounts)
	}
	var evidence, receipts int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM mud_go.bank_imports WHERE world_id=$1`, worldID).Scan(&evidence); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM mud_go.world_commands WHERE world_id=$1`, worldID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if evidence != 0 || receipts != 0 {
		t.Fatalf("failed import left evidence=%d receipts=%d", evidence, receipts)
	}
}
