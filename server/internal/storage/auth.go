package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/1XP-Inc/muhan-mud/server/internal/game"
	"github.com/1XP-Inc/muhan-mud/server/internal/identity"
)

var ErrCredentials = errors.New("name or password is incorrect")

func (p *Postgres) Exists(ctx context.Context, name string) (bool, error) {
	canonical, err := identity.CanonicalName(name)
	if err != nil {
		return false, err
	}
	var exists bool
	err = p.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM mud_go.accounts WHERE name=$1)", canonical).Scan(&exists)
	return exists, err
}

// Register stores credentials and a draft atomically. The caller must complete
// level initialization before world admission; this is not an admission API.
func (p *Postgres) Register(ctx context.Context, name string, password []byte, draft game.Creation) (string, error) {
	canonical, err := identity.CanonicalName(name)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	hash, err := identity.HashPassword(password)
	if err != nil {
		return "", err
	}
	defer clear(hash)
	return p.Create(ctx, canonical, hash, draft)
}

// Authenticate verifies stored credentials before returning any character data.
// For a linked legacy character, ID is the explicit world_player_id so the
// world connector cannot accidentally admit the database row UUID instead of
// the canonical player identity. Session ownership, rate limits and live-world
// admission remain separate gates.
func (p *Postgres) Authenticate(ctx context.Context, name string, password []byte) (Character, error) {
	canonical, err := identity.CanonicalName(name)
	if err != nil {
		return Character{}, ErrCredentials
	}
	var result Character
	var databaseID, accountName, worldID, worldPlayerID, stage string
	var hash, raw []byte
	err = p.db.QueryRowContext(ctx, `SELECT c.id,a.name,c.draft,a.credential_hash,c.stage,COALESCE(c.world_id,''),COALESCE(c.world_player_id,'')
 FROM mud_go.accounts a JOIN mud_go.characters c ON c.account_id=a.id
	 WHERE a.name=$1`, canonical).Scan(&databaseID, &accountName, &raw, &hash, &stage, &worldID, &worldPlayerID)
	defer clear(hash)
	if errors.Is(err, sql.ErrNoRows) {
		return Character{}, ErrCredentials
	}
	if err != nil {
		return Character{}, err
	}
	if !identity.CheckPassword(hash, password) {
		return Character{}, ErrCredentials
	}
	if err := ctx.Err(); err != nil {
		return Character{}, err
	}
	if err := json.Unmarshal(raw, &result.Draft); err != nil {
		return Character{}, err
	}
	result.ID = databaseID
	result.Name = accountName
	if stage == "linked" && worldID != "" && worldPlayerID != "" {
		result.ID = worldPlayerID
		result.Linked = true
		result.WorldID = worldID
		result.WorldPlayerID = worldPlayerID
	}
	return result, nil
}
