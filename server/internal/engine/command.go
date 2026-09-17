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

// ProjectedCommandStore is an optional receipt extension for commands whose
// post-commit output is not part of the public response. Implementations must
// persist projection alongside the immutable command receipt so a process
// restart can recover output without rerunning the reducer.
type ProjectedCommandStore interface {
	CommandStore
	CommitWorldCommandWithProjection(context.Context, string, string, json.RawMessage, int64, json.RawMessage, json.RawMessage, json.RawMessage) (storage.WorldReceipt, error)
}

// Reducer must be pure: no network output or irreversible effects before commit.
// Time/randomness must be bound to the command by the owning world loop.
type Reducer func(json.RawMessage) (state, response json.RawMessage, err error)

// ProjectedReducer returns a durable, non-public receipt projection in
// addition to the canonical state and public response.
type ProjectedReducer func(json.RawMessage) (state, response, projection json.RawMessage, err error)

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

// ExecuteProjected is Execute's receipt-aware variant for commands that have
// committed asynchronous output. Stores without the optional projection
// extension still receive the canonical response; the first caller retains
// the projection in memory, while production stores persist it and return it
// on replay.
func ExecuteProjected(ctx context.Context, store CommandStore, worldID, commandID string, request json.RawMessage, reduce ProjectedReducer) (storage.WorldReceipt, error) {
	if store == nil || reduce == nil || worldID == "" || commandID == "" || !json.Valid(request) {
		return storage.WorldReceipt{}, errors.New("invalid projected command execution input")
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
	next, response, projection, err := reduce(append(json.RawMessage(nil), snapshot.State...))
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	if projection == nil {
		projection = json.RawMessage(`{}`)
	}
	if !json.Valid(projection) {
		return storage.WorldReceipt{}, errors.New("invalid projected command output")
	}
	var commitErr error
	if projectedStore, ok := store.(ProjectedCommandStore); ok {
		receipt, commitErr = projectedStore.CommitWorldCommandWithProjection(ctx, worldID, commandID, request, snapshot.Revision, next, response, projection)
	} else {
		receipt, commitErr = store.CommitWorldCommand(ctx, worldID, commandID, request, snapshot.Revision, next, response)
		if commitErr == nil {
			receipt.Projection = append(json.RawMessage(nil), projection...)
		}
	}
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
