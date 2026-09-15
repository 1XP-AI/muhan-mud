package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/1XP-Inc/muhan-mud/server/internal/game"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

// CreateInWorld atomically creates an identity and its offline world character.
// hash must already be a password hash. Retry an uncertain commit with the SAME
// command ID, choices, and hash bytes (do not generate a fresh salted hash).
// The server owns command IDs; a known name or request ID is not authentication.
// Terminal registration/recovery orchestration is not wired yet.
func (p *Postgres) CreateInWorld(ctx context.Context, worldID, commandID string, expected int64, name string, hash []byte, draft game.Creation) (string, error) {
	if len(hash) == 0 || expected < 0 || len(commandID) == 0 || len(commandID) > 128 {
		return "", errors.New("invalid creation context")
	}
	player, err := world.NewPlayerFromDraft(name, draft)
	if err != nil {
		return "", err
	}
	// Only the final request digest is persisted, never credentials in receipts.
	request, err := json.Marshal(struct {
		Kind             string
		Name             string
		Draft            game.Creation
		CredentialDigest [32]byte
	}{"register-world", player.Body.Name, draft, sha256.Sum256(hash)})
	if err != nil {
		return "", err
	}
	requestHash := sha256.Sum256(request)
	draftJSON, err := json.Marshal(draft)
	if err != nil {
		return "", err
	}
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var revision, epoch int64
	var raw []byte
	// Same first lock as every game command; no lost updates across registration.
	if err = tx.QueryRowContext(ctx, `SELECT revision,state,writer_epoch FROM mud_go.worlds WHERE id=$1 FOR UPDATE`, worldID).Scan(&revision, &raw, &epoch); err != nil {
		return "", err
	}
	if err = p.checkWriter(worldID, epoch); err != nil {
		return "", err
	}
	var storedHash, response []byte
	err = tx.QueryRowContext(ctx, `SELECT request_hash,response FROM mud_go.world_commands WHERE world_id=$1 AND command_id=$2`, worldID, commandID).Scan(&storedHash, &response)
	if err == nil {
		if !bytes.Equal(storedHash, requestHash[:]) {
			return "", ErrCommandConflict
		}
		var characterID string
		if err = json.Unmarshal(response, &characterID); err != nil {
			return "", err
		}
		if characterID == "" {
			return "", errors.New("invalid registration receipt")
		}
		return characterID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if revision != expected {
		return "", ErrWorldConflict
	}
	state, err := world.DecodeState(raw)
	if err != nil {
		return "", err
	}
	if _, ok := state.Rooms[player.Body.RoomID]; !ok {
		return "", errors.New("creation start room missing")
	}
	for _, existing := range state.Players {
		if existing.Body.Name == player.Body.Name {
			return "", errors.New("world character name already exists")
		}
	}
	var accountID, characterID string
	if err = tx.QueryRowContext(ctx, `INSERT INTO mud_go.accounts(name,credential_hash) VALUES($1,$2) RETURNING id`, player.Body.Name, hash).Scan(&accountID); err != nil {
		return "", err
	}
	if err = tx.QueryRowContext(ctx, `INSERT INTO mud_go.characters(account_id,draft) VALUES($1,$2) RETURNING id`, accountID, string(draftJSON)).Scan(&characterID); err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE mud_go.characters SET stage='linked',world_id=$2,world_player_id=$1 WHERE id=$1`, characterID, worldID); err != nil {
		return "", err
	}
	if _, exists := state.Players[characterID]; exists {
		return "", errors.New("world character identity collision")
	}
	state.Players[characterID] = player
	if err = state.Validate(); err != nil {
		return "", err
	}
	raw, err = json.Marshal(state)
	if err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE mud_go.worlds SET state=$2,revision=revision+1 WHERE id=$1`, worldID, string(raw)); err != nil {
		return "", err
	}
	response, err = json.Marshal(characterID)
	if err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO mud_go.world_commands(world_id,command_id,request_hash,revision,response) VALUES($1,$2,$3,$4,$5)`, worldID, commandID, requestHash[:], revision+1, string(response)); err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return characterID, nil
}
