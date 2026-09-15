package session

import (
	"context"
	"encoding/json"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

// CancelWorldAdmission resolves a failed/uncertain Open before releasing its
// reservation. The character may be missing, offline, or already entered by a
// commit whose reply was lost. Even the no-op case commits a receipt under the
// world revision lock: an unlocked read alone cannot authorize release.
// Finish serializes with EnterWorld and prevents any later admission retry for
// this lease. Runtime must retain this operation ID until success; cross-process
// fencing and cold-start recovery are still separate requirements.
func (o *Ownership) CancelWorldAdmission(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease) (storage.WorldReceipt, error) {
	request, err := json.Marshal(struct {
		Kind, Actor string
		Generation  uint64
	}{"cancel-admission", lease.ActorID, lease.Generation})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	var receipt storage.WorldReceipt
	err = o.Finish(lease, func() error {
		var err error
		receipt, err = engine.Execute(ctx, store, worldID, commandID, request, func(raw json.RawMessage) (json.RawMessage, json.RawMessage, error) {
			state, err := world.DecodeState(raw)
			if err != nil {
				return nil, nil, err
			}
			var departure world.RoomDeparture
			if player, ok := state.Players[lease.ActorID]; ok && player.Online {
				state, departure, err = state.LeavePlayer(lease.ActorID)
				if err != nil {
					return nil, nil, err
				}
			}
			next, err := json.Marshal(state)
			if err != nil {
				return nil, nil, err
			}
			response, err := json.Marshal(departure)
			return next, response, err
		})
		return err
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return receipt, nil
}
