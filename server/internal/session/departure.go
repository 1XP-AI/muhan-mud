package session

import (
	"context"
	"encoding/json"
	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

// Depart holds the reservation through durable LeavePlayer and receipt recovery.
// commandID must be globally unique across process boots and retained on retry.
// No socket output or admission of another owner occurs before confirmed commit.
func (o *Ownership) Depart(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease) (storage.WorldReceipt, error) {
	request, err := json.Marshal(struct {
		Kind, Actor string
		Generation  uint64
	}{"depart", lease.ActorID, lease.Generation})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	var receipt storage.WorldReceipt
	err = o.Finish(lease, func() error {
		var err error
		receipt, err = engine.Execute(ctx, store, worldID, commandID, request, func(raw json.RawMessage) (json.RawMessage, json.RawMessage, error) {
			s, err := world.DecodeState(raw)
			if err != nil {
				return nil, nil, err
			}
			next, departure, err := s.LeavePlayer(lease.ActorID)
			if err != nil {
				return nil, nil, err
			}
			state, err := json.Marshal(next)
			if err != nil {
				return nil, nil, err
			}
			response, err := json.Marshal(departure)
			return state, response, err
		})
		return err
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return receipt, nil
}
