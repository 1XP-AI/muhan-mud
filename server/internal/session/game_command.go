package session

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
)

// ActorReducer receives the verified session actor separately from untrusted
// command payload. Implementations must derive permissions from world state.
type ActorReducer func(state json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error)

// ActorProjectedReducer returns a durable, non-public projection alongside a
// canonical response. It is used only for output that must survive a commit
// boundary without changing the terminal response.
type ActorProjectedReducer func(state json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, json.RawMessage, error)

// ExecuteGame serializes session validation through durable command application.
// Payload is receipt identity, not an authorization source. IDs are caller-owned
// across boots/retries. Publish only the returned confirmed receipt, to the
// captured connection. This is not yet a game command parser or WebSocket loop.
func (o *Ownership) ExecuteGame(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, payload json.RawMessage, reduce ActorReducer) (storage.WorldReceipt, error) {
	if !json.Valid(payload) || reduce == nil {
		return storage.WorldReceipt{}, errors.New("invalid game command")
	}
	request, err := json.Marshal(struct {
		Kind, Actor string
		Generation  uint64
		Payload     json.RawMessage
	}{"game", lease.ActorID, lease.Generation, payload})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	var receipt storage.WorldReceipt
	err = o.RunGame(lease, func() error {
		var err error
		receipt, err = engine.Execute(ctx, store, worldID, commandID, request, func(raw json.RawMessage) (json.RawMessage, json.RawMessage, error) { return reduce(raw, lease.ActorID) })
		return err
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return receipt, nil
}

// ExecuteGameWithProjection binds a projected command to the same actor and
// lease identity as ExecuteGame. The durable receipt owns the projection;
// callers decide when transport has accepted it and acknowledge delivery.
func (o *Ownership) ExecuteGameWithProjection(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, payload json.RawMessage, reduce ActorProjectedReducer) (storage.WorldReceipt, error) {
	if !json.Valid(payload) || reduce == nil {
		return storage.WorldReceipt{}, errors.New("invalid projected game command")
	}
	request, err := json.Marshal(struct {
		Kind, Actor string
		Generation  uint64
		Payload     json.RawMessage
	}{"game", lease.ActorID, lease.Generation, payload})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	var receipt storage.WorldReceipt
	err = o.RunGame(lease, func() error {
		var err error
		receipt, err = engine.ExecuteProjected(ctx, store, worldID, commandID, request, func(raw json.RawMessage) (json.RawMessage, json.RawMessage, json.RawMessage, error) {
			return reduce(raw, lease.ActorID)
		})
		return err
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return receipt, nil
}
