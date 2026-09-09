package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/1XP-Inc/muhan-mud/server/internal/identity"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

var (
	ErrPlayerSnapshotImport         = errors.New("player snapshot import rejected")
	ErrPlayerSnapshotImportConflict = errors.New("player snapshot import conflict")
)

// PlayerSnapshotImport is an operator/importer-owned request. Snapshot is a
// complete canonical CDTO envelope; PlayerID and ItemIDs must come from the
// reviewed migration manifest and are never derived from display names.
type PlayerSnapshotImport struct {
	WorldID          string
	CommandID        string
	ExpectedRevision int64
	AccountName      string
	CredentialHash   []byte
	PlayerID         string
	Snapshot         []byte
	ItemIDs          []string
}

// PlayerSnapshotImportResult is the durable result of one import receipt.
// Hashes are metadata evidence, not credential or source payload contents.
type PlayerSnapshotImportResult struct {
	PlayerID           string
	SourceSHA256       [sha256.Size]byte
	CanonicalSHA256    [sha256.Size]byte
	InventoryNodeCount int
	Replayed           bool
}

type playerSnapshotImportRequest struct {
	Kind             string
	AccountName      string
	PlayerID         string
	SourceSHA256     [sha256.Size]byte
	CredentialDigest [sha256.Size]byte
	ItemIDs          []string
}

type playerSnapshotImportReceipt struct {
	PlayerID           string
	SourceSHA256       [sha256.Size]byte
	CanonicalSHA256    [sha256.Size]byte
	InventoryNodeCount int
}

// ImportPlayerSnapshot atomically admits one verified CDTO player, creates its
// game account/link, stores migration evidence, and records a replayable world
// receipt. It does not read legacy files or start a session. Retry an uncertain
// commit with the exact same request, including salted hash and item IDs.
func (p *Postgres) ImportPlayerSnapshot(ctx context.Context, input PlayerSnapshotImport) (PlayerSnapshotImportResult, error) {
	normalized, canonicalName, request, err := normalizePlayerSnapshotImport(input)
	if err != nil {
		return PlayerSnapshotImportResult{}, err
	}
	if p == nil || p.db == nil {
		return PlayerSnapshotImportResult{}, ErrPlayerSnapshotImport
	}
	if ctx == nil {
		return PlayerSnapshotImportResult{}, ErrPlayerSnapshotImport
	}
	if err := ctx.Err(); err != nil {
		return PlayerSnapshotImportResult{}, err
	}
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return PlayerSnapshotImportResult{}, err
	}
	defer tx.Rollback()
	var revision, epoch int64
	var raw []byte
	if err = tx.QueryRowContext(ctx, `SELECT revision,state,writer_epoch FROM mud_go.worlds WHERE id=$1 FOR UPDATE`, normalized.WorldID).Scan(&revision, &raw, &epoch); err != nil {
		return PlayerSnapshotImportResult{}, err
	}
	if err = p.checkWriter(normalized.WorldID, epoch); err != nil {
		return PlayerSnapshotImportResult{}, err
	}
	var storedHash, storedResponse []byte
	err = tx.QueryRowContext(ctx, `SELECT request_hash,response FROM mud_go.world_commands WHERE world_id=$1 AND command_id=$2`, normalized.WorldID, normalized.CommandID).Scan(&storedHash, &storedResponse)
	if err == nil {
		requestDigest := requestHash(request)
		if !bytes.Equal(storedHash, requestDigest[:]) {
			return PlayerSnapshotImportResult{}, ErrCommandConflict
		}
		var receipt playerSnapshotImportReceipt
		if err := json.Unmarshal(storedResponse, &receipt); err != nil || receipt.PlayerID == "" {
			return PlayerSnapshotImportResult{}, fmt.Errorf("%w: invalid import receipt", ErrPlayerSnapshotImport)
		}
		return PlayerSnapshotImportResult{
			PlayerID: receipt.PlayerID, SourceSHA256: receipt.SourceSHA256,
			CanonicalSHA256: receipt.CanonicalSHA256, InventoryNodeCount: receipt.InventoryNodeCount, Replayed: true,
		}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return PlayerSnapshotImportResult{}, err
	}
	if revision != normalized.ExpectedRevision {
		return PlayerSnapshotImportResult{}, ErrWorldConflict
	}
	if err := validateCredentialHash(normalized.CredentialHash); err != nil {
		return PlayerSnapshotImportResult{}, err
	}
	inspection, err := world.InspectPlayerSnapshotV1(normalized.Snapshot)
	if err != nil {
		return PlayerSnapshotImportResult{}, fmt.Errorf("%w: snapshot decode: %v", ErrPlayerSnapshotImport, err)
	}
	canonicalWire, err := world.EncodePlayerSnapshotV1(inspection.Snapshot)
	if err != nil {
		return PlayerSnapshotImportResult{}, fmt.Errorf("%w: snapshot canonicalization: %v", ErrPlayerSnapshotImport, err)
	}
	canonicalDigest := sha256.Sum256(canonicalWire)
	if !bytes.Equal(canonicalWire, normalized.Snapshot) {
		return PlayerSnapshotImportResult{}, fmt.Errorf("%w: non-canonical snapshot", ErrPlayerSnapshotImport)
	}
	if len(normalized.ItemIDs) != len(inspection.Snapshot.Inventory.Nodes) {
		return PlayerSnapshotImportResult{}, fmt.Errorf("%w: item manifest count does not match graph", ErrPlayerSnapshotImport)
	}
	state, err := world.DecodeState(raw)
	if err != nil {
		return PlayerSnapshotImportResult{}, err
	}
	itemIndex := 0
	allocate := func() (string, error) {
		if itemIndex >= len(normalized.ItemIDs) {
			return "", fmt.Errorf("%w: item manifest exhausted", ErrPlayerSnapshotImport)
		}
		id := normalized.ItemIDs[itemIndex]
		itemIndex++
		return id, nil
	}
	next, err := state.AdmitPlayerSnapshot(normalized.PlayerID, inspection.Snapshot, allocate)
	if err != nil {
		return PlayerSnapshotImportResult{}, fmt.Errorf("%w: %v", ErrPlayerSnapshotImport, err)
	}
	if itemIndex != len(normalized.ItemIDs) {
		return PlayerSnapshotImportResult{}, fmt.Errorf("%w: item manifest has unused IDs", ErrPlayerSnapshotImport)
	}
	admittedName := next.Players[normalized.PlayerID].Body.Name
	if admittedName != canonicalName {
		return PlayerSnapshotImportResult{}, fmt.Errorf("%w: account name does not match snapshot", ErrPlayerSnapshotImportConflict)
	}
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT a.id FROM mud_go.accounts a WHERE a.name=$1`, canonicalName).Scan(&existing)
	if err == nil {
		return PlayerSnapshotImportResult{}, ErrPlayerSnapshotImportConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return PlayerSnapshotImportResult{}, err
	}
	err = tx.QueryRowContext(ctx, `SELECT c.id FROM mud_go.characters c WHERE c.world_id=$1 AND c.world_player_id=$2`, normalized.WorldID, normalized.PlayerID).Scan(&existing)
	if err == nil {
		return PlayerSnapshotImportResult{}, ErrPlayerSnapshotImportConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return PlayerSnapshotImportResult{}, err
	}
	var accountID, characterID string
	if err = tx.QueryRowContext(ctx, `INSERT INTO mud_go.accounts(name,credential_hash) VALUES($1,$2) RETURNING id`, canonicalName, normalized.CredentialHash).Scan(&accountID); err != nil {
		return PlayerSnapshotImportResult{}, err
	}
	if err = tx.QueryRowContext(ctx, `INSERT INTO mud_go.characters(account_id,stage,draft,world_id,world_player_id) VALUES($1,'linked','{}'::jsonb,$2,$3) RETURNING id`, accountID, normalized.WorldID, normalized.PlayerID).Scan(&characterID); err != nil {
		return PlayerSnapshotImportResult{}, err
	}
	stateJSON, err := json.Marshal(next)
	if err != nil {
		return PlayerSnapshotImportResult{}, err
	}
	if err = tx.QueryRowContext(ctx, `UPDATE mud_go.worlds SET state=$2,revision=revision+1 WHERE id=$1 RETURNING revision`, normalized.WorldID, string(stateJSON)).Scan(&revision); err != nil {
		return PlayerSnapshotImportResult{}, err
	}
	receipt := playerSnapshotImportReceipt{
		PlayerID: normalized.PlayerID, SourceSHA256: request.SourceSHA256,
		CanonicalSHA256: canonicalDigest, InventoryNodeCount: len(inspection.Snapshot.Inventory.Nodes),
	}
	response, err := json.Marshal(receipt)
	if err != nil {
		return PlayerSnapshotImportResult{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO mud_go.character_imports(world_id,world_player_id,command_id,source_sha256,canonical_sha256,source_octets,inventory_nodes,imported_revision) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, normalized.WorldID, normalized.PlayerID, normalized.CommandID, request.SourceSHA256[:], canonicalDigest[:], len(normalized.Snapshot), len(inspection.Snapshot.Inventory.Nodes), revision); err != nil {
		return PlayerSnapshotImportResult{}, err
	}
	hash := requestHash(request)
	if _, err = tx.ExecContext(ctx, `INSERT INTO mud_go.world_commands(world_id,command_id,request_hash,revision,response) VALUES($1,$2,$3,$4,$5)`, normalized.WorldID, normalized.CommandID, hash[:], revision, string(response)); err != nil {
		return PlayerSnapshotImportResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return PlayerSnapshotImportResult{}, err
	}
	return PlayerSnapshotImportResult{
		PlayerID: normalized.PlayerID, SourceSHA256: request.SourceSHA256,
		CanonicalSHA256: canonicalDigest, InventoryNodeCount: len(inspection.Snapshot.Inventory.Nodes),
	}, nil
}

func normalizePlayerSnapshotImport(input PlayerSnapshotImport) (PlayerSnapshotImport, string, playerSnapshotImportRequest, error) {
	if input.WorldID == "" || len(input.WorldID) > 128 || input.CommandID == "" || len(input.CommandID) > 128 || input.ExpectedRevision < 0 || input.PlayerID == "" || len(input.PlayerID) > 128 || len(input.Snapshot) == 0 || len(input.CredentialHash) == 0 {
		return PlayerSnapshotImport{}, "", playerSnapshotImportRequest{}, ErrPlayerSnapshotImport
	}
	canonical, err := identity.CanonicalName(input.AccountName)
	if err != nil {
		return PlayerSnapshotImport{}, "", playerSnapshotImportRequest{}, fmt.Errorf("%w: account name", ErrPlayerSnapshotImport)
	}
	itemIDs := append([]string(nil), input.ItemIDs...)
	if len(itemIDs) > 8192 {
		return PlayerSnapshotImport{}, "", playerSnapshotImportRequest{}, fmt.Errorf("%w: too many item IDs", ErrPlayerSnapshotImport)
	}
	seen := make(map[string]struct{}, len(itemIDs))
	for _, id := range itemIDs {
		if id == "" || len(id) > 128 {
			return PlayerSnapshotImport{}, "", playerSnapshotImportRequest{}, fmt.Errorf("%w: invalid item ID", ErrPlayerSnapshotImport)
		}
		if _, exists := seen[id]; exists {
			return PlayerSnapshotImport{}, "", playerSnapshotImportRequest{}, fmt.Errorf("%w: duplicate item ID", ErrPlayerSnapshotImport)
		}
		seen[id] = struct{}{}
	}
	if itemIDs == nil {
		itemIDs = []string{}
	}
	credential := append([]byte(nil), input.CredentialHash...)
	snapshot := append([]byte(nil), input.Snapshot...)
	input.CredentialHash, input.Snapshot, input.ItemIDs = credential, snapshot, itemIDs
	request := playerSnapshotImportRequest{
		Kind: "import-player-snapshot-v1", AccountName: canonical, PlayerID: input.PlayerID,
		SourceSHA256: sha256.Sum256(snapshot), CredentialDigest: sha256.Sum256(credential), ItemIDs: itemIDs,
	}
	return input, canonical, request, nil
}

func requestHash(request playerSnapshotImportRequest) [sha256.Size]byte {
	raw, _ := json.Marshal(request)
	return sha256.Sum256(raw)
}
