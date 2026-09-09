package storage

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf8"
)

var (
	ErrPlayerSnapshotInspection         = errors.New("player snapshot inspection rejected")
	ErrPlayerSnapshotInspectionConflict = errors.New("player snapshot inspection conflict")
)

// PlayerSnapshotInspection is metadata for a read-only scan. The snapshot
// payload is intentionally not stored; SourceSHA256 and SourceOctets bind the
// operator's evidence to the exact bytes that were inspected.
type PlayerSnapshotInspection struct {
	WorldID            string
	SourcePath         string
	SourceSHA256       [32]byte
	SourceOctets       int64
	ParserVersion      string
	ABI                string
	Result             string
	QuarantineReason   string
	InventoryNodeCount int
}

// RecordPlayerSnapshotInspection inserts one immutable scan result. An
// identical key and metadata is a replay; the same key with changed metadata
// is a conflict. A changed source digest for the same path is a new evidence
// revision and is therefore allowed.
func (p *Postgres) RecordPlayerSnapshotInspection(ctx context.Context, input PlayerSnapshotInspection) (bool, error) {
	if p == nil || p.db == nil || ctx == nil {
		return false, ErrPlayerSnapshotInspection
	}
	normalized, err := normalizePlayerSnapshotInspection(input)
	if err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	result, reason := normalized.Result, normalized.QuarantineReason
	res, err := p.db.ExecContext(ctx, `
 INSERT INTO mud_go.player_snapshot_import_ledger
  (world_id,source_path,source_sha256,source_octets,parser_version,abi,result,quarantine_reason,inventory_nodes)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
 ON CONFLICT(world_id,source_path,source_sha256) DO NOTHING`,
		normalized.WorldID, normalized.SourcePath, normalized.SourceSHA256[:], normalized.SourceOctets,
		normalized.ParserVersion, normalized.ABI, result, reason, normalized.InventoryNodeCount)
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if rows == 1 {
		return false, nil
	}
	var stored PlayerSnapshotInspection
	var storedDigest []byte
	err = p.db.QueryRowContext(ctx, `
 SELECT world_id,source_path,source_sha256,source_octets,parser_version,abi,result,quarantine_reason,inventory_nodes
 FROM mud_go.player_snapshot_import_ledger
 WHERE world_id=$1 AND source_path=$2 AND source_sha256=$3`,
		normalized.WorldID, normalized.SourcePath, normalized.SourceSHA256[:]).Scan(
		&stored.WorldID, &stored.SourcePath, &storedDigest, &stored.SourceOctets, &stored.ParserVersion,
		&stored.ABI, &stored.Result, &stored.QuarantineReason, &stored.InventoryNodeCount)
	if err != nil {
		return false, err
	}
	if len(storedDigest) != len(normalized.SourceSHA256) {
		return false, ErrPlayerSnapshotInspectionConflict
	}
	copy(stored.SourceSHA256[:], storedDigest)
	if stored != normalized {
		return false, ErrPlayerSnapshotInspectionConflict
	}
	return true, nil
}

func normalizePlayerSnapshotInspection(input PlayerSnapshotInspection) (PlayerSnapshotInspection, error) {
	if !validInspectionText(input.WorldID, 128) || !validInspectionText(input.SourcePath, 1024) ||
		!validInspectionText(input.ParserVersion, 64) || !validInspectionText(input.ABI, 128) {
		return PlayerSnapshotInspection{}, fmt.Errorf("%w: invalid metadata text", ErrPlayerSnapshotInspection)
	}
	if input.SourceOctets < 0 || input.SourceOctets > 64<<20 || input.InventoryNodeCount < 0 || input.InventoryNodeCount > 8192 {
		return PlayerSnapshotInspection{}, fmt.Errorf("%w: metadata bounds", ErrPlayerSnapshotInspection)
	}
	switch input.Result {
	case "validated":
		if input.SourceOctets == 0 || input.QuarantineReason != "" || isZeroDigest(input.SourceSHA256) {
			return PlayerSnapshotInspection{}, fmt.Errorf("%w: validated result metadata", ErrPlayerSnapshotInspection)
		}
	case "quarantined":
		if input.QuarantineReason == "" || len(input.QuarantineReason) > 512 || !utf8.ValidString(input.QuarantineReason) {
			return PlayerSnapshotInspection{}, fmt.Errorf("%w: quarantine reason", ErrPlayerSnapshotInspection)
		}
	default:
		return PlayerSnapshotInspection{}, fmt.Errorf("%w: unknown result", ErrPlayerSnapshotInspection)
	}
	if len(input.QuarantineReason) > 512 || !utf8.ValidString(input.QuarantineReason) {
		return PlayerSnapshotInspection{}, fmt.Errorf("%w: quarantine reason", ErrPlayerSnapshotInspection)
	}
	return input, nil
}

func validInspectionText(value string, maxBytes int) bool {
	if value == "" || len(value) > maxBytes || !utf8.ValidString(value) {
		return false
	}
	for _, b := range []byte(value) {
		if b < 32 || b == 127 {
			return false
		}
	}
	return true
}

func isZeroDigest(value [32]byte) bool {
	for _, b := range value {
		if b != 0 {
			return false
		}
	}
	return true
}
