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
	ErrBankSnapshotImport         = errors.New("bank snapshot import rejected")
	ErrBankSnapshotImportConflict = errors.New("bank snapshot import conflict")
)

// BankSnapshotImport is an operator/importer-owned request. The account and
// world player must already be explicitly linked; a bank artifact can never
// claim a character from a display name. ItemIDs are the reviewed IDs for the
// graph's non-root nodes in preorder.
type BankSnapshotImport struct {
	WorldID          string
	CommandID        string
	ExpectedRevision int64
	AccountName      string
	PlayerID         string
	Snapshot         []byte
	ItemIDs          []string
}

// BankSnapshotImportResult is the durable result of one bank import receipt.
// It contains metadata only; the raw graph and account credentials are never
// placed in the receipt or evidence row.
type BankSnapshotImportResult struct {
	PlayerID        string
	Balance         int64
	ItemCount       int
	SourceSHA256    [sha256.Size]byte
	CanonicalSHA256 [sha256.Size]byte
	Replayed        bool
}

type bankSnapshotImportRequest struct {
	Kind         string
	AccountName  string
	PlayerID     string
	SourceSHA256 [sha256.Size]byte
	ItemIDs      []string
}

type bankSnapshotImportReceipt struct {
	PlayerID        string
	Balance         int64
	ItemCount       int
	SourceSHA256    [sha256.Size]byte
	CanonicalSHA256 [sha256.Size]byte
}

type normalizedBankSnapshotImport struct {
	input       BankSnapshotImport
	accountName string
	request     bankSnapshotImportRequest
	inspection  world.BankSnapshotV1Inspection
}

// ImportBankSnapshot atomically attaches one verified bank graph to an
// already-linked world character. The world row, bank evidence and replayable
// command receipt are committed together. Retry an uncertain commit with the
// exact same request and item IDs.
func (p *Postgres) ImportBankSnapshot(ctx context.Context, input BankSnapshotImport) (BankSnapshotImportResult, error) {
	normalized, err := normalizeBankSnapshotImport(input)
	if err != nil {
		return BankSnapshotImportResult{}, err
	}
	if p == nil || p.db == nil || ctx == nil {
		return BankSnapshotImportResult{}, ErrBankSnapshotImport
	}
	if err := ctx.Err(); err != nil {
		return BankSnapshotImportResult{}, err
	}
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return BankSnapshotImportResult{}, err
	}
	defer tx.Rollback()
	var revision, epoch int64
	var raw []byte
	if err = tx.QueryRowContext(ctx, `SELECT revision,state,writer_epoch FROM mud_go.worlds WHERE id=$1 FOR UPDATE`, normalized.input.WorldID).Scan(&revision, &raw, &epoch); err != nil {
		return BankSnapshotImportResult{}, err
	}
	if err = p.checkWriter(normalized.input.WorldID, epoch); err != nil {
		return BankSnapshotImportResult{}, err
	}
	requestHash := bankSnapshotRequestHash(normalized.request)
	var storedHash, storedResponse []byte
	err = tx.QueryRowContext(ctx, `SELECT request_hash,response FROM mud_go.world_commands WHERE world_id=$1 AND command_id=$2`, normalized.input.WorldID, normalized.input.CommandID).Scan(&storedHash, &storedResponse)
	if err == nil {
		if !bytes.Equal(storedHash, requestHash[:]) {
			return BankSnapshotImportResult{}, ErrCommandConflict
		}
		var receipt bankSnapshotImportReceipt
		if err := json.Unmarshal(storedResponse, &receipt); err != nil || receipt.PlayerID == "" {
			return BankSnapshotImportResult{}, fmt.Errorf("%w: invalid import receipt", ErrBankSnapshotImport)
		}
		return BankSnapshotImportResult{
			PlayerID: receipt.PlayerID, Balance: receipt.Balance, ItemCount: receipt.ItemCount,
			SourceSHA256: receipt.SourceSHA256, CanonicalSHA256: receipt.CanonicalSHA256, Replayed: true,
		}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return BankSnapshotImportResult{}, err
	}
	if revision != normalized.input.ExpectedRevision {
		return BankSnapshotImportResult{}, ErrWorldConflict
	}
	// A bank import binds to an existing account/character link. It does not
	// create credentials and does not accept an unlinked draft as authority.
	var linkedID string
	err = tx.QueryRowContext(ctx, `SELECT c.id FROM mud_go.characters c JOIN mud_go.accounts a ON a.id=c.account_id WHERE a.name=$1 AND c.stage='linked' AND c.world_id=$2 AND c.world_player_id=$3`, normalized.accountName, normalized.input.WorldID, normalized.input.PlayerID).Scan(&linkedID)
	if errors.Is(err, sql.ErrNoRows) {
		return BankSnapshotImportResult{}, ErrBankSnapshotImportConflict
	}
	if err != nil {
		return BankSnapshotImportResult{}, err
	}
	state, err := world.DecodeState(raw)
	if err != nil {
		return BankSnapshotImportResult{}, err
	}
	player, ok := state.Players[normalized.input.PlayerID]
	if !ok || player.Body.Name != normalized.accountName {
		return BankSnapshotImportResult{}, ErrBankSnapshotImportConflict
	}
	if _, exists := state.BankAccounts[normalized.input.PlayerID]; exists {
		return BankSnapshotImportResult{}, ErrBankSnapshotImportConflict
	}
	itemIndex := 0
	allocate := func() (string, error) {
		if itemIndex >= len(normalized.input.ItemIDs) {
			return "", fmt.Errorf("%w: item manifest exhausted", ErrBankSnapshotImport)
		}
		id := normalized.input.ItemIDs[itemIndex]
		itemIndex++
		return id, nil
	}
	account, err := normalized.inspection.Snapshot.ToBankAccount(allocate)
	if err != nil {
		return BankSnapshotImportResult{}, fmt.Errorf("%w: %v", ErrBankSnapshotImport, err)
	}
	if itemIndex != len(normalized.input.ItemIDs) {
		return BankSnapshotImportResult{}, fmt.Errorf("%w: item manifest has unused IDs", ErrBankSnapshotImport)
	}
	if state.BankAccounts == nil {
		state.BankAccounts = make(map[string]world.BankAccount)
	}
	state.BankAccounts[normalized.input.PlayerID] = account
	if err := state.Validate(); err != nil {
		return BankSnapshotImportResult{}, fmt.Errorf("%w: state validation: %v", ErrBankSnapshotImport, err)
	}
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return BankSnapshotImportResult{}, err
	}
	if err = tx.QueryRowContext(ctx, `UPDATE mud_go.worlds SET state=$2,revision=revision+1 WHERE id=$1 RETURNING revision`, normalized.input.WorldID, string(stateJSON)).Scan(&revision); err != nil {
		return BankSnapshotImportResult{}, err
	}
	canonicalDigest := normalized.inspection.SHA256
	receipt := bankSnapshotImportReceipt{
		PlayerID: normalized.input.PlayerID, Balance: account.Balance, ItemCount: len(normalized.input.ItemIDs),
		SourceSHA256: normalized.request.SourceSHA256, CanonicalSHA256: canonicalDigest,
	}
	response, err := json.Marshal(receipt)
	if err != nil {
		return BankSnapshotImportResult{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO mud_go.bank_imports(world_id,world_player_id,command_id,source_sha256,canonical_sha256,source_octets,item_count,balance,imported_revision) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, normalized.input.WorldID, normalized.input.PlayerID, normalized.input.CommandID, normalized.request.SourceSHA256[:], canonicalDigest[:], len(normalized.input.Snapshot), len(normalized.input.ItemIDs), account.Balance, revision); err != nil {
		return BankSnapshotImportResult{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO mud_go.world_commands(world_id,command_id,request_hash,revision,response) VALUES($1,$2,$3,$4,$5)`, normalized.input.WorldID, normalized.input.CommandID, requestHash[:], revision, string(response)); err != nil {
		return BankSnapshotImportResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return BankSnapshotImportResult{}, err
	}
	return BankSnapshotImportResult{
		PlayerID: normalized.input.PlayerID, Balance: account.Balance, ItemCount: len(normalized.input.ItemIDs),
		SourceSHA256: normalized.request.SourceSHA256, CanonicalSHA256: canonicalDigest,
	}, nil
}

func normalizeBankSnapshotImport(input BankSnapshotImport) (normalizedBankSnapshotImport, error) {
	if input.WorldID == "" || len(input.WorldID) > 128 || input.CommandID == "" || len(input.CommandID) > 128 || input.ExpectedRevision < 0 || input.PlayerID == "" || len(input.PlayerID) > 128 || len(input.Snapshot) == 0 {
		return normalizedBankSnapshotImport{}, ErrBankSnapshotImport
	}
	canonical, err := identity.CanonicalName(input.AccountName)
	if err != nil {
		return normalizedBankSnapshotImport{}, fmt.Errorf("%w: account name", ErrBankSnapshotImport)
	}
	inspection, err := world.InspectBankSnapshotV1(input.Snapshot)
	if err != nil {
		return normalizedBankSnapshotImport{}, fmt.Errorf("%w: snapshot decode: %v", ErrBankSnapshotImport, err)
	}
	itemCount := len(inspection.Snapshot.Root.Nodes) - 1
	if itemCount < 0 || itemCount > 8191 || len(input.ItemIDs) != itemCount {
		return normalizedBankSnapshotImport{}, fmt.Errorf("%w: item manifest count", ErrBankSnapshotImport)
	}
	itemIDs := append([]string(nil), input.ItemIDs...)
	seen := make(map[string]struct{}, len(itemIDs))
	for _, id := range itemIDs {
		if id == "" || len(id) > 128 {
			return normalizedBankSnapshotImport{}, fmt.Errorf("%w: invalid item ID", ErrBankSnapshotImport)
		}
		if _, exists := seen[id]; exists {
			return normalizedBankSnapshotImport{}, fmt.Errorf("%w: duplicate item ID", ErrBankSnapshotImport)
		}
		seen[id] = struct{}{}
	}
	input.AccountName = canonical
	input.Snapshot = append([]byte(nil), input.Snapshot...)
	input.ItemIDs = itemIDs
	return normalizedBankSnapshotImport{
		input:       input,
		accountName: canonical,
		request:     bankSnapshotImportRequest{Kind: "import-bank-snapshot-v1", AccountName: canonical, PlayerID: input.PlayerID, SourceSHA256: inspection.SHA256, ItemIDs: itemIDs},
		inspection:  inspection,
	}, nil
}

func bankSnapshotRequestHash(request bankSnapshotImportRequest) [sha256.Size]byte {
	raw, _ := json.Marshal(request)
	return sha256.Sum256(raw)
}
