package storage

// This file is the PostgreSQL boundary for a reviewed canonical vote
// aggregate. It never opens player/vote files or infers a character from a
// display name; the filesystem collector and operator manifest are separate
// stages. A VoteStateImport only updates the world snapshot when the current
// snapshot is still unresolved and the caller supplies the exact expected
// revision.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

var (
	ErrVoteStateImport         = errors.New("vote state import rejected")
	ErrVoteStateImportConflict = errors.New("vote state import conflict")
)

// VoteStateImport is a reviewed, pointer-free canonical aggregate keyed by
// immutable player IDs. CatalogDigest binds active ballots to the ISSUE
// snapshot used by the migration review. Source paths, raw bytes, names as
// claims, and credentials are intentionally rejected fields.
type VoteStateImport struct {
	WorldID          string
	CommandID        string
	ExpectedRevision int64
	CatalogDigest    string
	Votes            world.VoteState
	// BallotState is a descriptive alias for callers that use the persisted
	// aggregate's terminology. Supplying both fields is rejected.
	BallotState    world.VoteState
	SourcePath     string
	RawPath        string
	CredentialHash []byte
}

type VoteStateImportResult struct {
	Revision        int64
	BallotCount     int
	HistoryCount    int
	CatalogDigest   string
	AggregateSHA256 [sha256.Size]byte
	Replayed        bool
}

type voteStateImportReceipt struct {
	Revision        int               `json:"revision"`
	BallotCount     int               `json:"ballot_count"`
	HistoryCount    int               `json:"history_count"`
	CatalogDigest   string            `json:"catalog_digest"`
	AggregateSHA256 [sha256.Size]byte `json:"aggregate_sha256"`
}

type voteStateImportRequest struct {
	Kind          string            `json:"kind"`
	CatalogDigest string            `json:"catalog_digest"`
	Aggregate     [sha256.Size]byte `json:"aggregate_sha256"`
}

type normalizedVoteStateImport struct {
	input        VoteStateImport
	votes        world.VoteState
	aggregate    [sha256.Size]byte
	ballotCount  int
	historyCount int
}

func validVoteDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func normalizeVoteStateImport(input VoteStateImport) (normalizedVoteStateImport, error) {
	if !validImportToken(input.WorldID, 128) || !validImportToken(input.CommandID, 128) || input.ExpectedRevision < 0 || !validVoteDigest(input.CatalogDigest) {
		return normalizedVoteStateImport{}, ErrVoteStateImport
	}
	if err := validateNoSensitiveImportFields(input.SourcePath, input.RawPath, input.CredentialHash); err != nil {
		return normalizedVoteStateImport{}, fmt.Errorf("%w: %v", ErrVoteStateImport, err)
	}
	if input.Votes.Ballots != nil && input.BallotState.Ballots != nil {
		return normalizedVoteStateImport{}, fmt.Errorf("%w: duplicate vote aggregate fields", ErrVoteStateImport)
	}
	votes := input.Votes
	if votes.Ballots == nil && votes.History == nil {
		votes = input.BallotState
	} else if input.BallotState.Ballots != nil || input.BallotState.History != nil {
		return normalizedVoteStateImport{}, fmt.Errorf("%w: duplicate vote aggregate fields", ErrVoteStateImport)
	}
	if err := votes.Validate(); err != nil {
		return normalizedVoteStateImport{}, fmt.Errorf("%w: %v", ErrVoteStateImport, err)
	}
	for actorID, ballot := range votes.Ballots {
		if actorID == "" || ballot.CatalogDigest != input.CatalogDigest {
			return normalizedVoteStateImport{}, fmt.Errorf("%w: ballot catalog digest mismatch", ErrVoteStateImport)
		}
	}
	payload, err := json.Marshal(struct {
		CatalogDigest string          `json:"catalog_digest"`
		Votes         world.VoteState `json:"votes"`
	}{input.CatalogDigest, votes})
	if err != nil {
		return normalizedVoteStateImport{}, fmt.Errorf("%w: aggregate encoding", ErrVoteStateImport)
	}
	return normalizedVoteStateImport{
		input: input, votes: votes.Clone(), aggregate: sha256.Sum256(payload),
		ballotCount: len(votes.Ballots), historyCount: len(votes.History),
	}, nil
}

// ImportVoteState atomically installs one reviewed canonical vote aggregate
// into the world snapshot and records an immutable command receipt. Reusing a
// command ID with a different aggregate is a conflict; replaying the same
// request returns the original receipt without rewriting the snapshot.
func (p *Postgres) ImportVoteState(ctx context.Context, input VoteStateImport) (VoteStateImportResult, error) {
	normalized, err := normalizeVoteStateImport(input)
	if err != nil || p == nil || p.db == nil || ctx == nil {
		if err != nil {
			return VoteStateImportResult{}, err
		}
		return VoteStateImportResult{}, ErrVoteStateImport
	}
	request := voteStateImportRequest{Kind: "import-vote-state-v1", CatalogDigest: input.CatalogDigest, Aggregate: normalized.aggregate}
	requestRaw, _ := json.Marshal(request)
	requestDigest := sha256.Sum256(requestRaw)
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return VoteStateImportResult{}, err
	}
	defer tx.Rollback()
	var revision, epoch int64
	var raw []byte
	if err = tx.QueryRowContext(ctx, `SELECT revision,state,writer_epoch FROM mud_go.worlds WHERE id=$1 FOR UPDATE`, input.WorldID).Scan(&revision, &raw, &epoch); err != nil {
		return VoteStateImportResult{}, err
	}
	if err = p.checkWriter(input.WorldID, epoch); err != nil {
		return VoteStateImportResult{}, err
	}
	var storedHash, storedResponse []byte
	err = tx.QueryRowContext(ctx, `SELECT request_hash,response FROM mud_go.world_commands WHERE world_id=$1 AND command_id=$2`, input.WorldID, input.CommandID).Scan(&storedHash, &storedResponse)
	if err == nil {
		if !bytes.Equal(storedHash, requestDigest[:]) {
			return VoteStateImportResult{}, ErrCommandConflict
		}
		var receipt voteStateImportReceipt
		if json.Unmarshal(storedResponse, &receipt) != nil || receipt.CatalogDigest != input.CatalogDigest {
			return VoteStateImportResult{}, ErrVoteStateImport
		}
		return VoteStateImportResult{Revision: int64(receipt.Revision), BallotCount: receipt.BallotCount, HistoryCount: receipt.HistoryCount, CatalogDigest: receipt.CatalogDigest, AggregateSHA256: receipt.AggregateSHA256, Replayed: true}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return VoteStateImportResult{}, err
	}
	if revision != input.ExpectedRevision {
		return VoteStateImportResult{}, ErrWorldConflict
	}
	state, err := world.DecodeState(raw)
	if err != nil {
		return VoteStateImportResult{}, err
	}
	if state.Votes != nil {
		return VoteStateImportResult{}, ErrVoteStateImportConflict
	}
	if err := validateUniquePlayerNames(state); err != nil {
		return VoteStateImportResult{}, fmt.Errorf("%w: %v", ErrVoteStateImportConflict, err)
	}
	for actorID := range normalized.votes.Ballots {
		if _, ok := state.Players[actorID]; !ok {
			return VoteStateImportResult{}, fmt.Errorf("%w: foreign ballot actor", ErrVoteStateImportConflict)
		}
	}
	next, err := state.WithVoteState(normalized.votes)
	if err != nil {
		return VoteStateImportResult{}, fmt.Errorf("%w: %v", ErrVoteStateImport, err)
	}
	stateJSON, err := json.Marshal(next)
	if err != nil {
		return VoteStateImportResult{}, err
	}
	if err = tx.QueryRowContext(ctx, `UPDATE mud_go.worlds SET state=$2,revision=revision+1 WHERE id=$1 RETURNING revision`, input.WorldID, string(stateJSON)).Scan(&revision); err != nil {
		return VoteStateImportResult{}, err
	}
	receipt := voteStateImportReceipt{Revision: int(revision), BallotCount: normalized.ballotCount, HistoryCount: normalized.historyCount, CatalogDigest: input.CatalogDigest, AggregateSHA256: normalized.aggregate}
	response, _ := json.Marshal(receipt)
	if _, err = tx.ExecContext(ctx, `INSERT INTO mud_go.vote_imports(world_id,command_id,catalog_digest,aggregate_sha256,ballot_count,history_count,imported_revision) VALUES($1,$2,$3,$4,$5,$6,$7)`, input.WorldID, input.CommandID, mustDecodeDigest(input.CatalogDigest), normalized.aggregate[:], normalized.ballotCount, normalized.historyCount, revision); err != nil {
		return VoteStateImportResult{}, mapVoteImportDatabaseError(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO mud_go.world_commands(world_id,command_id,request_hash,revision,response) VALUES($1,$2,$3,$4,$5)`, input.WorldID, input.CommandID, requestDigest[:], revision, string(response)); err != nil {
		return VoteStateImportResult{}, mapVoteImportDatabaseError(err)
	}
	if err = tx.Commit(); err != nil {
		return VoteStateImportResult{}, err
	}
	return VoteStateImportResult{Revision: revision, BallotCount: normalized.ballotCount, HistoryCount: normalized.historyCount, CatalogDigest: input.CatalogDigest, AggregateSHA256: normalized.aggregate}, nil
}

func mustDecodeDigest(value string) []byte {
	decoded, _ := hex.DecodeString(value)
	return decoded
}

func mapVoteImportDatabaseError(err error) error {
	// Keep driver-specific details out of the migration API while preserving
	// the original error for operator diagnostics.
	return fmt.Errorf("%w: %v", ErrVoteStateImportConflict, err)
}
