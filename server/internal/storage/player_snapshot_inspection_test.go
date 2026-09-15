package storage

import (
	"context"
	"crypto/sha256"
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

func TestNormalizePlayerSnapshotInspectionRejectsAmbiguousEvidence(t *testing.T) {
	var digest [32]byte
	digest[0] = 1
	valid := PlayerSnapshotInspection{
		WorldID: "world", SourcePath: "alice.cdto", SourceSHA256: digest, SourceOctets: 4,
		ParserVersion: "go-player-snapshot-v1", ABI: "cdto-v1", Result: "validated",
	}
	if got, err := normalizePlayerSnapshotInspection(valid); err != nil || got != valid {
		t.Fatalf("valid=%+v err=%v", got, err)
	}
	tests := []struct {
		name   string
		mutate func(*PlayerSnapshotInspection)
	}{
		{name: "validated zero digest", mutate: func(in *PlayerSnapshotInspection) { in.SourceSHA256 = [32]byte{} }},
		{name: "validated reason", mutate: func(in *PlayerSnapshotInspection) { in.QuarantineReason = "unexpected" }},
		{name: "quarantine missing reason", mutate: func(in *PlayerSnapshotInspection) { in.Result = "quarantined" }},
		{name: "unknown result", mutate: func(in *PlayerSnapshotInspection) { in.Result = "other" }},
		{name: "control path", mutate: func(in *PlayerSnapshotInspection) { in.SourcePath = "bad\npath" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidate := valid
			tt.mutate(&candidate)
			if _, err := normalizePlayerSnapshotInspection(candidate); !errors.Is(err, ErrPlayerSnapshotInspection) {
				t.Fatalf("candidate=%+v err=%v", candidate, err)
			}
		})
	}
}

func TestPostgresPlayerSnapshotInspectionLedgerIsIdempotent(t *testing.T) {
	dsn := os.Getenv("MUHAN_PLAYER_SNAPSHOT_IMPORT_TEST_DATABASE_URL")
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
	repo := NewPostgres(db)
	if err := repo.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	worldID := fmt.Sprintf("inspection-%d", time.Now().UnixNano())
	state := world.State{Version: 1, Rooms: map[int16]world.RoomState{1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}}}}, Players: map[string]world.PlayerState{}}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateWorld(ctx, worldID, raw); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("snapshot-one"))
	input := PlayerSnapshotInspection{
		WorldID: worldID, SourcePath: "shard/alice.cdto", SourceSHA256: digest, SourceOctets: 12,
		ParserVersion: "go-player-snapshot-v1", ABI: "cdto-v1", Result: "validated", InventoryNodeCount: 2,
	}
	replayed, err := repo.RecordPlayerSnapshotInspection(ctx, input)
	if err != nil || replayed {
		t.Fatalf("first record replayed=%v err=%v", replayed, err)
	}
	replayed, err = repo.RecordPlayerSnapshotInspection(ctx, input)
	if err != nil || !replayed {
		t.Fatalf("same record replayed=%v err=%v", replayed, err)
	}
	conflict := input
	conflict.InventoryNodeCount = 3
	if _, err := repo.RecordPlayerSnapshotInspection(ctx, conflict); !errors.Is(err, ErrPlayerSnapshotInspectionConflict) {
		t.Fatalf("metadata conflict err=%v", err)
	}
	changed := input
	changed.SourceSHA256 = sha256.Sum256([]byte("snapshot-two"))
	changed.SourceOctets = 13
	if replayed, err := repo.RecordPlayerSnapshotInspection(ctx, changed); err != nil || replayed {
		t.Fatalf("changed source replayed=%v err=%v", replayed, err)
	}
	quarantine := PlayerSnapshotInspection{
		WorldID: worldID, SourcePath: "shard/broken.cdto", Result: "quarantined",
		QuarantineReason: "malformed CDTO", ParserVersion: "go-player-snapshot-v1", ABI: "cdto-v1",
	}
	if replayed, err := repo.RecordPlayerSnapshotInspection(ctx, quarantine); err != nil || replayed {
		t.Fatalf("quarantine replayed=%v err=%v", replayed, err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM mud_go.player_snapshot_import_ledger WHERE world_id=$1`, worldID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("ledger rows=%d, want 3", count)
	}
}
