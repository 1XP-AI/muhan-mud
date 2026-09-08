// Package engine coordinates pure game transitions and durable command results.
package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
)

type CommandStore interface {
	ReadWorldReceipt(context.Context, string, string, json.RawMessage) (storage.WorldReceipt, error)
	LoadWorld(context.Context, string) (storage.WorldSnapshot, error)
	CommitWorldCommand(context.Context, string, string, json.RawMessage, int64, json.RawMessage, json.RawMessage) (storage.WorldReceipt, error)
}

// Reducer must be pure: no network output or irreversible effects before commit.
// Time/randomness must be bound to the command by the owning world loop.
type Reducer func(json.RawMessage) (state, response json.RawMessage, err error)

// Execute checks durable receipts BEFORE evaluating game rules. It never retries
// a reducer automatically on conflict. An uncertain commit is reconciled by a
// receipt read with the same context; if unavailable, caller retains command ID
// and exact request for later retry. Only confirmed durable output is returned.
// This is an internal boundary, not authorization or a WebSocket handler.
func Execute(ctx context.Context, store CommandStore, worldID, commandID string, request json.RawMessage, reduce Reducer) (storage.WorldReceipt, error) {
	if store == nil || reduce == nil || worldID == "" || commandID == "" || !json.Valid(request) {
		return storage.WorldReceipt{}, errors.New("invalid command execution input")
	}
	receipt, err := store.ReadWorldReceipt(ctx, worldID, commandID, request)
	if err == nil {
		return receipt, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return storage.WorldReceipt{}, err
	}
	snapshot, err := store.LoadWorld(ctx, worldID)
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	next, response, err := reduce(append(json.RawMessage(nil), snapshot.State...))
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	receipt, commitErr := store.CommitWorldCommand(ctx, worldID, commandID, request, snapshot.Revision, next, response)
	if commitErr == nil {
		return receipt, nil
	}
	receipt, recoveryErr := store.ReadWorldReceipt(ctx, worldID, commandID, request)
	if recoveryErr == nil {
		return receipt, nil
	}
	if errors.Is(recoveryErr, sql.ErrNoRows) {
		return storage.WorldReceipt{}, commitErr
	}
	return storage.WorldReceipt{}, errors.Join(commitErr, recoveryErr)
}

var _ CommandStore = (*storage.Postgres)(nil)
