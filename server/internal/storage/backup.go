package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

const (
	WorldBackupFormat  = "muhan-mud.world-backup"
	WorldBackupVersion = 1
)

var ErrWorldRestoreConflict = errors.New("world restore requires an empty target or explicit force")

// WorldBackup is a self-contained, checksummed snapshot envelope. State is
// kept as the exact JSON bytes loaded from PostgreSQL so a backup can be
// inspected and verified without trusting a caller-provided checksum.
type WorldBackup struct {
	Format      string          `json:"format"`
	Version     int             `json:"version"`
	WorldID     string          `json:"world_id"`
	Revision    int64           `json:"revision"`
	State       json.RawMessage `json:"state"`
	StateSHA256 string          `json:"state_sha256"`
}

// WorldBackupOptions controls destructive restore behaviour. The default is
// fail-closed: an existing target must provide the expected revision and have
// no command receipts. Force explicitly invalidates prior receipts and bumps
// writer_epoch so a previously running writer cannot continue after restore.
type WorldBackupOptions struct {
	ExpectedRevision *int64
	AllowCreate      bool
	Force            bool
}

func NewWorldBackup(worldID string, snapshot WorldSnapshot) (WorldBackup, error) {
	state, err := canonicalJSON(snapshot.State)
	if err != nil {
		return WorldBackup{}, err
	}
	b := WorldBackup{
		Format:      WorldBackupFormat,
		Version:     WorldBackupVersion,
		WorldID:     worldID,
		Revision:    snapshot.Revision,
		State:       state,
		StateSHA256: stateSHA256(state),
	}
	if err := b.Validate(); err != nil {
		return WorldBackup{}, err
	}
	return b, nil
}

func (b WorldBackup) Marshal() ([]byte, error) {
	if err := b.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(b)
}

func ParseWorldBackup(raw []byte) (WorldBackup, error) {
	var b WorldBackup
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&b); err != nil {
		return WorldBackup{}, fmt.Errorf("decode world backup: %w", err)
	}
	var trailing any
	if err := d.Decode(&trailing); err == nil {
		return WorldBackup{}, errors.New("world backup has trailing data")
	} else if !errors.Is(err, io.EOF) {
		return WorldBackup{}, errors.New("world backup has trailing data")
	}
	if err := b.Validate(); err != nil {
		return WorldBackup{}, err
	}
	return b, nil
}

func (b WorldBackup) Validate() error {
	if b.Format != WorldBackupFormat || b.Version != WorldBackupVersion {
		return errors.New("unsupported world backup format")
	}
	if len(b.WorldID) == 0 || len(b.WorldID) > 128 || b.Revision < 0 {
		return errors.New("invalid world backup identity")
	}
	if len(bytes.TrimSpace(b.State)) == 0 || !json.Valid(b.State) || bytes.TrimSpace(b.State)[0] != '{' {
		return errors.New("world backup state must be a JSON object")
	}
	canonical, err := canonicalJSON(b.State)
	if err != nil || !bytes.Equal(canonical, b.State) {
		return errors.New("world backup state must use canonical JSON")
	}
	if len(b.StateSHA256) != sha256.Size*2 {
		return errors.New("invalid world backup checksum")
	}
	if _, err := hex.DecodeString(b.StateSHA256); err != nil {
		return errors.New("invalid world backup checksum")
	}
	if stateSHA256(b.State) != b.StateSHA256 {
		return errors.New("world backup checksum mismatch")
	}
	if _, err := world.DecodeState(b.State); err != nil {
		return fmt.Errorf("invalid world backup state: %w", err)
	}
	return nil
}

func canonicalJSON(raw []byte) (json.RawMessage, error) {
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return nil, err
	}
	return json.RawMessage(compact.Bytes()), nil
}

func stateSHA256(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// RestoreWorldBackup restores a validated snapshot in one PostgreSQL
// transaction. Existing command receipts are retained only for a non-force
// restore of a target with no receipts; force restore removes them and fences
// the current writer generation.
func (p *Postgres) RestoreWorldBackup(ctx context.Context, backup WorldBackup, options WorldBackupOptions) error {
	if p == nil || p.db == nil {
		return errors.New("nil postgres store")
	}
	if err := backup.Validate(); err != nil {
		return err
	}
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var currentRevision, epoch int64
	err = tx.QueryRowContext(ctx, `SELECT revision,writer_epoch FROM mud_go.worlds WHERE id=$1 FOR UPDATE`, backup.WorldID).Scan(&currentRevision, &epoch)
	if errors.Is(err, sql.ErrNoRows) {
		if !options.AllowCreate {
			return ErrWorldRestoreConflict
		}
		if options.ExpectedRevision != nil && *options.ExpectedRevision != 0 {
			return ErrWorldConflict
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO mud_go.worlds(id,revision,state) VALUES($1,$2,$3)`, backup.WorldID, backup.Revision, string(backup.State)); err != nil {
			return err
		}
		return tx.Commit()
	}
	if err != nil {
		return err
	}
	if err = p.admitRestoreWriter(backup.WorldID, epoch); err != nil {
		return err
	}
	if !options.Force {
		if options.ExpectedRevision == nil || *options.ExpectedRevision != currentRevision {
			return ErrWorldConflict
		}
		var receiptCount int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM mud_go.world_commands WHERE world_id=$1`, backup.WorldID).Scan(&receiptCount); err != nil {
			return err
		}
		if receiptCount != 0 {
			return ErrWorldRestoreConflict
		}
		_, err = tx.ExecContext(ctx, `UPDATE mud_go.worlds SET revision=$2,state=$3 WHERE id=$1`, backup.WorldID, backup.Revision, string(backup.State))
		if err != nil {
			return err
		}
		return tx.Commit()
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM mud_go.world_commands WHERE world_id=$1`, backup.WorldID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE mud_go.worlds SET revision=$2,state=$3,writer_epoch=writer_epoch+1 WHERE id=$1`, backup.WorldID, backup.Revision, string(backup.State)); err != nil {
		return err
	}
	return tx.Commit()
}

// admitRestoreWriter fences a claimed handle that no longer holds the current
// generation. An unbound operator store is the recovery path: Force restore
// advances writer_epoch so a live or SIGKILL'd writer cannot continue.
func (p *Postgres) admitRestoreWriter(worldID string, epoch int64) error {
	if p.writerWorld == "" {
		return nil
	}
	return p.checkWriter(worldID, epoch)
}
