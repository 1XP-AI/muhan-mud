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
// Characters may be draft-only registrations or explicitly linked to an
// already-initialized world player. World snapshots remain the gameplay
// authority; credentials never replace canonical player state.
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
  stage text NOT NULL DEFAULT 'draft' CHECK(stage IN ('draft','linked')),
  draft jsonb NOT NULL CHECK(jsonb_typeof(draft)='object'),
  revision bigint NOT NULL DEFAULT 0 CHECK(revision>=0),
  world_id text,
  world_player_id text
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
 CREATE TABLE IF NOT EXISTS mud_go.character_imports (
  world_id text NOT NULL REFERENCES mud_go.worlds(id),
  world_player_id text NOT NULL,
  command_id text NOT NULL,
  source_sha256 bytea NOT NULL CHECK(octet_length(source_sha256)=32),
  canonical_sha256 bytea NOT NULL CHECK(octet_length(canonical_sha256)=32),
  source_octets bigint NOT NULL CHECK(source_octets>0),
  inventory_nodes integer NOT NULL CHECK(inventory_nodes>=0),
  imported_revision bigint NOT NULL CHECK(imported_revision>0),
  PRIMARY KEY(world_id,world_player_id),
  UNIQUE(world_id,source_sha256)
 );
 CREATE TABLE IF NOT EXISTS mud_go.bank_imports (
  world_id text NOT NULL REFERENCES mud_go.worlds(id),
  world_player_id text NOT NULL,
  command_id text NOT NULL,
  source_sha256 bytea NOT NULL CHECK(octet_length(source_sha256)=32),
  canonical_sha256 bytea NOT NULL CHECK(octet_length(canonical_sha256)=32),
  source_octets bigint NOT NULL CHECK(source_octets>0),
  item_count integer NOT NULL CHECK(item_count BETWEEN 0 AND 8191),
  balance bigint NOT NULL CHECK(balance BETWEEN 0 AND 300000000),
  imported_revision bigint NOT NULL CHECK(imported_revision>0),
  PRIMARY KEY(world_id,world_player_id),
  UNIQUE(world_id,source_sha256)
 );
 CREATE TABLE IF NOT EXISTS mud_go.player_snapshot_import_ledger (
  world_id text NOT NULL REFERENCES mud_go.worlds(id),
  source_path text NOT NULL CHECK(length(source_path) BETWEEN 1 AND 1024),
  source_sha256 bytea NOT NULL CHECK(octet_length(source_sha256)=32),
  source_octets bigint NOT NULL CHECK(source_octets BETWEEN 0 AND 67108864),
  parser_version text NOT NULL CHECK(length(parser_version) BETWEEN 1 AND 64),
  abi text NOT NULL CHECK(length(abi) BETWEEN 1 AND 128),
  result text NOT NULL CHECK(result IN ('validated','quarantined')),
  quarantine_reason text NOT NULL CHECK(length(quarantine_reason)<=512),
  inventory_nodes integer NOT NULL CHECK(inventory_nodes BETWEEN 0 AND 8192),
  observed_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY(world_id,source_path,source_sha256),
  CHECK((result='validated' AND source_octets>0 AND length(quarantine_reason)=0) OR result='quarantined')
 );
 ALTER TABLE mud_go.worlds ADD COLUMN IF NOT EXISTS writer_epoch bigint NOT NULL DEFAULT 0 CHECK(writer_epoch>=0);
 ALTER TABLE mud_go.characters DROP CONSTRAINT IF EXISTS characters_stage_check;
 ALTER TABLE mud_go.characters ADD CONSTRAINT characters_stage_check CHECK(stage IN ('draft','linked'));
 ALTER TABLE mud_go.characters ADD COLUMN IF NOT EXISTS world_id text;
 ALTER TABLE mud_go.characters ADD COLUMN IF NOT EXISTS world_player_id text;
 CREATE UNIQUE INDEX IF NOT EXISTS characters_world_player_unique ON mud_go.characters(world_id,world_player_id) WHERE world_id IS NOT NULL AND world_player_id IS NOT NULL;
 CREATE TABLE IF NOT EXISTS mud_go.world_writer_claims (
  world_id text NOT NULL REFERENCES mud_go.worlds(id),
  claim_id text NOT NULL CHECK(length(claim_id) BETWEEN 1 AND 128),
  epoch bigint NOT NULL CHECK(epoch>0),
  PRIMARY KEY(world_id,claim_id), UNIQUE(world_id,epoch)
 );
 CREATE TABLE IF NOT EXISTS mud_go.family_imports (
  world_id text NOT NULL REFERENCES mud_go.worlds(id),
  command_id text NOT NULL CHECK(length(command_id) BETWEEN 1 AND 128),
  aggregate_sha256 bytea NOT NULL CHECK(octet_length(aggregate_sha256)=32),
  family_count integer NOT NULL CHECK(family_count BETWEEN 0 AND 15),
  member_count integer NOT NULL CHECK(member_count BETWEEN 0 AND 100000),
  imported_revision bigint NOT NULL CHECK(imported_revision>0),
  PRIMARY KEY(world_id,command_id),
  UNIQUE(world_id,aggregate_sha256)
 );
 CREATE TABLE IF NOT EXISTS mud_go.family_catalog (
  world_id text NOT NULL REFERENCES mud_go.worlds(id),
  family_id smallint NOT NULL CHECK(family_id BETWEEN 1 AND 15),
  family_name text NOT NULL CHECK(length(family_name) BETWEEN 1 AND 256),
  boss_id text NOT NULL CHECK(length(boss_id)<=128),
  boss_name text NOT NULL CHECK(length(boss_name) BETWEEN 1 AND 256),
  fee bigint NOT NULL CHECK(fee>=0 AND fee<=214748),
  command_id text NOT NULL,
  PRIMARY KEY(world_id,family_id)
 );
 CREATE TABLE IF NOT EXISTS mud_go.family_members (
  world_id text NOT NULL REFERENCES mud_go.worlds(id),
  family_id smallint NOT NULL CHECK(family_id BETWEEN 1 AND 15),
  member_position integer NOT NULL CHECK(member_position>=0),
  character_id text NOT NULL CHECK(length(character_id) BETWEEN 1 AND 128),
  character_name text NOT NULL CHECK(length(character_name) BETWEEN 1 AND 256),
  character_class smallint NOT NULL CHECK(character_class BETWEEN 0 AND 255),
  command_id text NOT NULL,
  PRIMARY KEY(world_id,family_id,member_position),
  UNIQUE(world_id,character_id)
 );
 CREATE TABLE IF NOT EXISTS mud_go.character_memo_imports (
  world_id text NOT NULL REFERENCES mud_go.worlds(id),
  command_id text NOT NULL CHECK(length(command_id) BETWEEN 1 AND 128),
  aggregate_sha256 bytea NOT NULL CHECK(octet_length(aggregate_sha256)=32),
  recipient_count integer NOT NULL CHECK(recipient_count BETWEEN 0 AND 100000),
  memo_count integer NOT NULL CHECK(memo_count BETWEEN 0 AND 1000000),
  imported_revision bigint NOT NULL CHECK(imported_revision>0),
  PRIMARY KEY(world_id,command_id),
  UNIQUE(world_id,aggregate_sha256)
 );
 CREATE TABLE IF NOT EXISTS mud_go.character_memos (
  world_id text NOT NULL REFERENCES mud_go.worlds(id),
  recipient_id text NOT NULL CHECK(length(recipient_id) BETWEEN 1 AND 128),
  memo_position integer NOT NULL CHECK(memo_position>=0),
  memo_id text NOT NULL CHECK(length(memo_id) BETWEEN 1 AND 128),
  sender_id text NOT NULL CHECK(length(sender_id) BETWEEN 1 AND 128),
  sender_name text NOT NULL CHECK(length(sender_name) BETWEEN 1 AND 256),
  body text NOT NULL CHECK(length(body) BETWEEN 1 AND 80),
  created_at timestamptz NOT NULL,
  command_id text NOT NULL,
 PRIMARY KEY(world_id,recipient_id,memo_position),
  UNIQUE(world_id,memo_id)
 );
 CREATE TABLE IF NOT EXISTS mud_go.vote_imports (
  world_id text NOT NULL REFERENCES mud_go.worlds(id),
  command_id text NOT NULL CHECK(length(command_id) BETWEEN 1 AND 128),
  catalog_digest bytea NOT NULL CHECK(octet_length(catalog_digest)=32),
  aggregate_sha256 bytea NOT NULL CHECK(octet_length(aggregate_sha256)=32),
  ballot_count integer NOT NULL CHECK(ballot_count BETWEEN 0 AND 1000000),
  history_count integer NOT NULL CHECK(history_count BETWEEN 0 AND 1000000),
  imported_revision bigint NOT NULL CHECK(imported_revision>0),
  PRIMARY KEY(world_id,command_id),
  UNIQUE(world_id,aggregate_sha256)
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
	ID string
	// Name is the canonical game account name. It is carried separately from
	// the world/player ID so account-only commands cannot infer credential
	// ownership from a mutable or database-local character identifier.
	Name          string
	Draft         game.Creation
	Linked        bool
	WorldID       string
	WorldPlayerID string
}

func (p *Postgres) Load(ctx context.Context, name string) (Character, error) {
	canonical, nameErr := identity.CanonicalName(name)
	if nameErr != nil {
		return Character{}, nameErr
	}
	name = canonical
	var result Character
	var databaseID, accountName, worldID, worldPlayerID, stage string
	var raw []byte
	err := p.db.QueryRowContext(ctx, `SELECT c.id,a.name,c.draft,c.stage,COALESCE(c.world_id,''),COALESCE(c.world_player_id,'') FROM mud_go.characters c
	JOIN mud_go.accounts a ON a.id=c.account_id WHERE a.name=$1`, name).Scan(&databaseID, &accountName, &raw, &stage, &worldID, &worldPlayerID)
	if err != nil {
		return Character{}, err
	}
	if err = json.Unmarshal(raw, &result.Draft); err != nil {
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
