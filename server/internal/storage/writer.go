package storage

import (
	"context"
	"database/sql"
	"errors"
)

var ErrWriterFenced = errors.New("world writer generation is no longer current")

// ClaimWorldWriter is an explicit takeover, never an automatic retry after a
// fenced write. The host must stop its retired connections and recover online
// state before admitting players. The returned handle is immutable and cannot
// promote itself. Retry uncertain claims with the SAME server-owned boot ID.
// Once superseded, that ID cannot take over again. Distinct boot processes must
// never share a claim ID. This is not a timed lease/election or authentication.
func (p *Postgres) ClaimWorldWriter(ctx context.Context, worldID, claimID string) (*Postgres, error) {
	if p.writerWorld != "" {
		return nil, ErrWriterFenced
	}
	if len(claimID) == 0 || len(claimID) > 128 {
		return nil, errors.New("invalid writer claim")
	}
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var epoch int64
	if err = tx.QueryRowContext(ctx, `SELECT writer_epoch FROM mud_go.worlds WHERE id=$1 FOR UPDATE`, worldID).Scan(&epoch); err != nil {
		return nil, err
	}
	var claimed int64
	err = tx.QueryRowContext(ctx, `SELECT epoch FROM mud_go.world_writer_claims WHERE world_id=$1 AND claim_id=$2`, worldID, claimID).Scan(&claimed)
	if err == nil {
		if epoch != claimed {
			return nil, ErrWriterFenced
		}
		return &Postgres{db: p.db, writerWorld: worldID, writerEpoch: epoch}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err = tx.QueryRowContext(ctx, `UPDATE mud_go.worlds SET writer_epoch=writer_epoch+1 WHERE id=$1 RETURNING writer_epoch`, worldID).Scan(&epoch); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO mud_go.world_writer_claims(world_id,claim_id,epoch) VALUES($1,$2,$3)`, worldID, claimID, epoch); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &Postgres{db: p.db, writerWorld: worldID, writerEpoch: epoch}, nil
}
func (p *Postgres) checkWriter(worldID string, epoch int64) error {
	if p.writerWorld == "" {
		if epoch == 0 {
			return nil
		}
		return ErrWriterFenced
	}
	if p.writerWorld != worldID || p.writerEpoch != epoch {
		return ErrWriterFenced
	}
	return nil
}
