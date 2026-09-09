package storage

import (
	"bytes"
	"context"
	"database/sql"
	"errors"

	"github.com/1XP-Inc/muhan-mud/server/internal/identity"
)

// ErrPasswordChangeConflict means the account identity or credential changed
// after the operation began. The caller must start a new bounded change rather
// than guessing which account or password should win.
var ErrPasswordChangeConflict = errors.New("password change conflict")

// LookupCredential returns the exact account UUID and an opaque bcrypt hash.
// It intentionally does not join characters: credentials belong to accounts,
// and a draft/linked character row must not change password ownership.
func (p *Postgres) LookupCredential(ctx context.Context, name string) (string, []byte, error) {
	if p == nil || p.db == nil {
		return "", nil, ErrCredentials
	}
	canonical, err := identity.CanonicalName(name)
	if err != nil {
		return "", nil, ErrCredentials
	}
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	var accountID string
	var hash []byte
	err = p.db.QueryRowContext(ctx, `SELECT id,credential_hash FROM mud_go.accounts WHERE name=$1`, canonical).Scan(&accountID, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, ErrCredentials
	}
	if err != nil {
		return "", nil, err
	}
	if accountID == "" || validateCredentialHash(hash) != nil {
		return "", nil, ErrCredentials
	}
	return accountID, hash, nil
}

// UpdateCredential atomically replaces one exact account's hash. The expected
// hash binds the operation to the credential verified by the connection. If a
// response is lost after commit, the same operation can be retried: seeing the
// replacement hash is an idempotent success, while any other hash conflicts.
// No password or hash is persisted as a receipt.
func (p *Postgres) UpdateCredential(ctx context.Context, accountID string, expectedHash, replacementHash []byte) error {
	if p == nil || p.db == nil || accountID == "" || len(expectedHash) == 0 || len(replacementHash) == 0 {
		return ErrPasswordChangeConflict
	}
	if validateCredentialHash(expectedHash) != nil || validateCredentialHash(replacementHash) != nil {
		return ErrPasswordChangeConflict
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var storedHash []byte
	err = tx.QueryRowContext(ctx, `SELECT credential_hash FROM mud_go.accounts WHERE id=$1 FOR UPDATE`, accountID).Scan(&storedHash)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrPasswordChangeConflict
	}
	if err != nil {
		return err
	}
	if bytes.Equal(storedHash, replacementHash) {
		// The first attempt committed but its response was lost. There is no
		// state to change, so the transaction can safely roll back.
		return nil
	}
	if !bytes.Equal(storedHash, expectedHash) {
		return ErrPasswordChangeConflict
	}
	result, err := tx.ExecContext(ctx, `UPDATE mud_go.accounts SET credential_hash=$1 WHERE id=$2 AND credential_hash=$3`, replacementHash, accountID, expectedHash)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrPasswordChangeConflict
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

// Ensure this adapter remains compatible with the session-only credential
// boundary without coupling Accounts to a password-write method.
var _ interface {
	LookupCredential(context.Context, string) (string, []byte, error)
	UpdateCredential(context.Context, string, []byte, []byte) error
} = (*Postgres)(nil)
