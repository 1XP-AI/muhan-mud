package storage

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/identity"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPostgresImportsPlayerSnapshotAtomicallyAndReplays(t *testing.T) {
	dsn := os.Getenv("MUHAN_PLAYER_SNAPSHOT_IMPORT_TEST_DATABASE_URL")
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
	worldID := fmt.Sprintf("snapshot-import-%d", time.Now().UnixNano())
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		Players: map[string]world.PlayerState{},
	}
	rawState, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateWorld(ctx, worldID, rawState); err != nil {
		t.Fatal(err)
	}
	snapshotRaw := readImportSnapshotFixture(t, "player_snapshot_v1_legacy_decoder_canonical.hex")
	snapshot, err := world.DecodePlayerSnapshotV1(snapshotRaw)
	if err != nil {
		t.Fatal(err)
	}
	// Use a unique canonical account name while preserving the fixture's
	// canonical graph and all scalar values.
	name := fmt.Sprintf("S%d", time.Now().UnixNano()%100000000)
	var nameBytes [80]byte
	copy(nameBytes[:], name)
	snapshot.Name = nameBytes
	snapshot.RoomNumber = 1
	snapshotRaw, err = world.EncodePlayerSnapshotV1(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := identity.HashPassword([]byte("pw1234"))
	if err != nil {
		t.Fatal(err)
	}
	input := PlayerSnapshotImport{
		WorldID:          worldID,
		CommandID:        "snapshot-import-1",
		ExpectedRevision: 0,
		AccountName:      name,
		CredentialHash:   hash,
		PlayerID:         "legacy-explicit-1",
		Snapshot:         snapshotRaw,
		ItemIDs:          []string{"snapshot-item-root", "snapshot-item-child"},
	}
	result, err := repo.ImportPlayerSnapshot(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Replayed || result.PlayerID != input.PlayerID || result.InventoryNodeCount != 2 {
		t.Fatalf("import result=%+v", result)
	}
	loaded, err := repo.LoadWorld(ctx, worldID)
	if err != nil || loaded.Revision != 1 {
		t.Fatalf("world revision=%+v err=%v", loaded, err)
	}
	loadedState, err := world.DecodeState(loaded.State)
	if err != nil {
		t.Fatal(err)
	}
	player, ok := loadedState.Players[input.PlayerID]
	if !ok || player.Online || player.Body.Name != name || player.Items == nil || !reflect.DeepEqual(player.Items.Inventory, []string{"snapshot-item-root"}) {
		t.Fatalf("imported player=%+v ok=%v", player, ok)
	}
	if err := player.Items.Validate(); err != nil {
		t.Fatal(err)
	}
	var stage, linkedWorld, linkedPlayer string
	if err := db.QueryRowContext(ctx, `SELECT c.stage,c.world_id,c.world_player_id FROM mud_go.characters c JOIN mud_go.accounts a ON a.id=c.account_id WHERE a.name=$1`, name).Scan(&stage, &linkedWorld, &linkedPlayer); err != nil {
		t.Fatal(err)
	}
	if stage != "linked" || linkedWorld != worldID || linkedPlayer != input.PlayerID {
		t.Fatalf("character link stage=%q world=%q player=%q", stage, linkedWorld, linkedPlayer)
	}
	var evidenceNodes int
	var sourceOctets int
	if err := db.QueryRowContext(ctx, `SELECT inventory_nodes,source_octets FROM mud_go.character_imports WHERE world_id=$1 AND world_player_id=$2`, worldID, input.PlayerID).Scan(&evidenceNodes, &sourceOctets); err != nil {
		t.Fatal(err)
	}
	if evidenceNodes != 2 || sourceOctets != len(snapshotRaw) {
		t.Fatalf("evidence nodes=%d octets=%d", evidenceNodes, sourceOctets)
	}
	replayed, err := repo.ImportPlayerSnapshot(ctx, input)
	if err != nil || !replayed.Replayed || replayed.PlayerID != input.PlayerID || replayed.SourceSHA256 != result.SourceSHA256 {
		t.Fatalf("replay=%+v err=%v", replayed, err)
	}
	if after, err := repo.LoadWorld(ctx, worldID); err != nil || after.Revision != 1 {
		t.Fatalf("replay changed revision=%+v err=%v", after, err)
	}
	changed := input
	changed.Snapshot = append([]byte(nil), input.Snapshot...)
	changed.Snapshot[len(changed.Snapshot)-1] ^= 1
	if _, err := repo.ImportPlayerSnapshot(ctx, changed); !errors.Is(err, ErrCommandConflict) {
		t.Fatalf("changed snapshot accepted: %v", err)
	}
}

func TestPostgresPlayerSnapshotImportRollsBackIdentityAndEvidence(t *testing.T) {
	dsn := os.Getenv("MUHAN_PLAYER_SNAPSHOT_IMPORT_TEST_DATABASE_URL")
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
	worldID := fmt.Sprintf("snapshot-rollback-%d", time.Now().UnixNano())
	state := world.State{Version: 1, Rooms: map[int16]world.RoomState{1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}}, Items: &world.ItemCollection{Items: map[string]world.Item{}}}}, Players: map[string]world.PlayerState{}}
	rawState, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateWorld(ctx, worldID, rawState); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE mud_go.worlds ADD CONSTRAINT reject_snapshot_import CHECK(revision=0) NOT VALID`); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = db.ExecContext(ctx, `ALTER TABLE mud_go.worlds DROP CONSTRAINT reject_snapshot_import`) }()
	snapshotRaw := readImportSnapshotFixture(t, "player_snapshot_v1_legacy_decoder_minimal.hex")
	snapshot, err := world.DecodePlayerSnapshotV1(snapshotRaw)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("R%d", time.Now().UnixNano()%100000000)
	var nameBytes [80]byte
	copy(nameBytes[:], name)
	snapshot.Name = nameBytes
	snapshot.RoomNumber = 1
	snapshotRaw, err = world.EncodePlayerSnapshotV1(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := identity.HashPassword([]byte("pw1234"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.ImportPlayerSnapshot(ctx, PlayerSnapshotImport{WorldID: worldID, CommandID: "snapshot-rollback-1", AccountName: name, CredentialHash: hash, PlayerID: "legacy-rollback", Snapshot: snapshotRaw})
	if err == nil {
		t.Fatal("constraint failure ignored")
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM mud_go.accounts WHERE name=$1`, name).Scan(&count); err != nil || count != 0 {
		t.Fatalf("orphan account count=%d err=%v", count, err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM mud_go.character_imports WHERE world_id=$1`, worldID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("orphan evidence count=%d err=%v", count, err)
	}
}

func readImportSnapshotFixture(t *testing.T, name string) []byte {
	t.Helper()
	encoded, err := os.ReadFile("../../../tests/fixtures/" + name)
	if err != nil {
		t.Fatal(err)
	}
	decoded := make([]byte, hex.DecodedLen(len(bytes.TrimSpace(encoded))))
	if _, err := hex.Decode(decoded, bytes.TrimSpace(encoded)); err != nil {
		t.Fatal(err)
	}
	return decoded
}
