package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/1XP-Inc/muhan-mud/server/internal/identity"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
	"golang.org/x/crypto/bcrypt"
)

var (
	// ErrWorldCharacterConflict means that the requested legacy identity cannot
	// be linked without guessing ownership or replacing an existing account.
	ErrWorldCharacterConflict = errors.New("world character link conflict")
	ErrInvalidCredentialHash  = errors.New("invalid credential hash")
)

// LinkExistingWorldCharacter binds an already-initialized canonical player in
// a world snapshot to a game-name/password account. It is an explicit,
// operator/importer-facing path, not a name-claim endpoint: the caller must
// supply the exact player ID and an expected world revision. The world state
// is never rebuilt from a draft and the player remains offline during linking.
//
// The account and character rows, revision bump, and immutable command receipt
// are committed in one transaction. A retry with the same command ID must use
// the same salted hash bytes; a changed hash or player identity is rejected.
func (p *Postgres) LinkExistingWorldCharacter(ctx context.Context, worldID, commandID string, expected int64, name string, hash []byte, playerID string) (string, error) {
	if p == nil || p.db == nil || worldID == "" || commandID == "" || len(commandID) > 128 || expected < 0 || playerID == "" {
		return "", ErrWorldCharacterConflict
	}
	canonical, err := identity.CanonicalName(name)
	if err != nil {
		return "", ErrWorldCharacterConflict
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	request, err := json.Marshal(struct {
		Kind             string
		Name             string
		PlayerID         string
		CredentialDigest [32]byte
	}{"link-world-character", canonical, playerID, sha256.Sum256(hash)})
	if err != nil {
		return "", err
	}
	requestHash := sha256.Sum256(request)

	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	var revision int64
	var raw []byte
	if err = tx.QueryRowContext(ctx, `SELECT revision,state FROM mud_go.worlds WHERE id=$1 FOR UPDATE`, worldID).Scan(&revision, &raw); err != nil {
		return "", err
	}
	var storedHash, response []byte
	err = tx.QueryRowContext(ctx, `SELECT request_hash,response FROM mud_go.world_commands WHERE world_id=$1 AND command_id=$2`, worldID, commandID).Scan(&storedHash, &response)
	if err == nil {
		if !bytes.Equal(storedHash, requestHash[:]) {
			return "", ErrCommandConflict
		}
		var linkedID string
		if err := json.Unmarshal(response, &linkedID); err != nil || linkedID == "" {
			return "", errors.New("invalid world character link receipt")
		}
		return linkedID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if revision != expected {
		return "", ErrWorldConflict
	}
	// Check the immutable receipt before validating the new credential bytes so
	// an uncertain retry with the same command ID reports identity conflict,
	// even when the caller supplied a malformed replacement hash.
	if err := validateCredentialHash(hash); err != nil {
		return "", err
	}
	state, err := world.DecodeState(raw)
	if err != nil {
		return "", err
	}
	player, ok := state.Players[playerID]
	if !ok || player.Body.Type != 0 || player.Body.Name != canonical || player.Items == nil {
		return "", ErrWorldCharacterConflict
	}
	// A legacy name is not an identity. Require exactly one matching body and
	// ensure the caller's explicit ID is that same entry.
	matches := 0
	for id, candidate := range state.Players {
		if candidate.Body.Name != canonical {
			continue
		}
		matches++
		if id != playerID {
			return "", ErrWorldCharacterConflict
		}
	}
	if matches != 1 {
		return "", ErrWorldCharacterConflict
	}
	var existingAccount string
	err = tx.QueryRowContext(ctx, `SELECT a.id FROM mud_go.accounts a WHERE a.name=$1`, canonical).Scan(&existingAccount)
	if err == nil {
		return "", ErrWorldCharacterConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	var linkedAccount string
	err = tx.QueryRowContext(ctx, `SELECT a.id FROM mud_go.accounts a JOIN mud_go.characters c ON c.account_id=a.id WHERE c.world_id=$1 AND c.world_player_id=$2`, worldID, playerID).Scan(&linkedAccount)
	if err == nil {
		return "", ErrWorldCharacterConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}

	var accountID, characterID string
	if err = tx.QueryRowContext(ctx, `INSERT INTO mud_go.accounts(name,credential_hash) VALUES($1,$2) RETURNING id`, canonical, hash).Scan(&accountID); err != nil {
		return "", err
	}
	// A linked character stores an explicit empty creation object only as a
	// schema-compatible migration marker. Runtime admission uses world_player_id
	// and the canonical world snapshot, never this draft value.
	if err = tx.QueryRowContext(ctx, `INSERT INTO mud_go.characters(account_id,stage,draft,world_id,world_player_id) VALUES($1,'linked','{}'::jsonb,$2,$3) RETURNING id`, accountID, worldID, playerID).Scan(&characterID); err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE mud_go.worlds SET revision=revision+1 WHERE id=$1`, worldID); err != nil {
		return "", err
	}
	response, err = json.Marshal(playerID)
	if err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO mud_go.world_commands(world_id,command_id,request_hash,revision,response) VALUES($1,$2,$3,$4,$5)`, worldID, commandID, requestHash[:], revision+1, string(response)); err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return playerID, nil
}

func validateCredentialHash(hash []byte) error {
	if len(hash) == 0 {
		return ErrInvalidCredentialHash
	}
	cost, err := bcrypt.Cost(hash)
	if err != nil || cost != bcrypt.DefaultCost {
		return ErrInvalidCredentialHash
	}
	return nil
}
