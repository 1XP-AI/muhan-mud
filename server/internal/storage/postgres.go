// Package storage persists game data without invoking legacy C or Rust.
package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

import "github.com/1XP-Inc/muhan-mud/server/internal/game"
import "github.com/1XP-Inc/muhan-mud/server/internal/identity"

type Postgres struct {
	db          *sql.DB
	writerWorld string
	writerEpoch int64
}

func NewPostgres(db *sql.DB) *Postgres { return &Postgres{db: db} }

// Migrate is an explicit provisioning step, not called on player connections.
// Characters remain pre-level drafts; world snapshots are an internal command
// persistence boundary. No world admission path exists yet.
func (p *Postgres) Migrate(ctx context.Context) error {
	_, err := p.db.ExecContext(ctx, `
 CREATE SCHEMA IF NOT EXISTS mud_go;
 REVOKE ALL ON SCHEMA mud_go FROM PUBLIC;
 CREATE TABLE IF NOT EXISTS mud_go.accounts (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name text COLLATE "C" UNIQUE NOT NULL,
  credential_hash bytea NOT NULL CHECK (octet_length(credential_hash)>0)
 );
 CREATE TABLE IF NOT EXISTS mud_go.characters (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  account_id uuid NOT NULL UNIQUE REFERENCES mud_go.accounts(id),
  stage text NOT NULL DEFAULT 'draft' CHECK(stage='draft'),
  draft jsonb NOT NULL CHECK(jsonb_typeof(draft)='object'),
  revision bigint NOT NULL DEFAULT 0 CHECK(revision>=0)
 );
 CREATE TABLE IF NOT EXISTS mud_go.worlds (
  id text PRIMARY KEY CHECK(length(id) BETWEEN 1 AND 128),
  revision bigint NOT NULL DEFAULT 0 CHECK(revision>=0),
  state jsonb NOT NULL CHECK(jsonb_typeof(state)='object')
 );
 CREATE TABLE IF NOT EXISTS mud_go.world_commands (
  world_id text NOT NULL REFERENCES mud_go.worlds(id),
  command_id text NOT NULL CHECK(length(command_id) BETWEEN 1 AND 128),
  request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
  revision bigint NOT NULL CHECK(revision>0),
  response jsonb NOT NULL,
  PRIMARY KEY(world_id,command_id),
  UNIQUE(world_id,revision)
 );
 ALTER TABLE mud_go.worlds ADD COLUMN IF NOT EXISTS writer_epoch bigint NOT NULL DEFAULT 0 CHECK(writer_epoch>=0);
 CREATE TABLE IF NOT EXISTS mud_go.world_writer_claims (
  world_id text NOT NULL REFERENCES mud_go.worlds(id),
  claim_id text NOT NULL CHECK(length(claim_id) BETWEEN 1 AND 128),
  epoch bigint NOT NULL CHECK(epoch>0),
  PRIMARY KEY(world_id,claim_id), UNIQUE(world_id,epoch)
 );`)
	return err
}

// Create is an internal persistence boundary. The caller must validate the
// canonical name, hash the password, and supply validated game choices.
// Credentials are opaque hash bytes here, never a password hashing API.
func (p *Postgres) Create(ctx context.Context, name string, hash []byte, draft game.Creation) (string, error) {
	canonical, err := identity.CanonicalName(name)
	if err != nil {
		return "", err
	}
	name = canonical
	if name == "" || len(hash) == 0 {
		return "", errors.New("missing account fields")
	}
	if _, err := draft.Choices(); err != nil {
		return "", err
	}
	raw, err := json.Marshal(draft)
	if err != nil {
		return "", err
	}
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var accountID, characterID string
	err = tx.QueryRowContext(ctx, "INSERT INTO mud_go.accounts(name,credential_hash) VALUES($1,$2) RETURNING id", name, hash).Scan(&accountID)
	if err != nil {
		return "", err
	}
	err = tx.QueryRowContext(ctx, "INSERT INTO mud_go.characters(account_id,draft) VALUES($1,$2) RETURNING id", accountID, string(raw)).Scan(&characterID)
	if err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return characterID, nil
}

type Character struct {
	ID    string
	Draft game.Creation
}

func (p *Postgres) Load(ctx context.Context, name string) (Character, error) {
	canonical, nameErr := identity.CanonicalName(name)
	if nameErr != nil {
		return Character{}, nameErr
	}
	name = canonical
	var result Character
	var raw []byte
	err := p.db.QueryRowContext(ctx, `SELECT c.id,c.draft FROM mud_go.characters c
 JOIN mud_go.accounts a ON a.id=c.account_id WHERE a.name=$1`, name).Scan(&result.ID, &raw)
	if err != nil {
		return Character{}, err
	}
	if err = json.Unmarshal(raw, &result.Draft); err != nil {
		return Character{}, err
	}
	return result, nil
}
