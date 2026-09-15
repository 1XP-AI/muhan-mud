package session

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

var errAdmissionCharacter = errors.New("canonical game character required")

// EnterWorld admits an already credential-verified, fully initialized canonical
// character. Creation drafts must not use this path. The caller keeps the same
// command ID, time, view, catalog snapshot and RNG/ID streams on uncertain retry.
// Failed admission retains its lease until persistence ambiguity is resolved.
func (o *Ownership) EnterWorld(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, now int32, view world.SceneOptions, catalog world.SpawnCatalog, roll func(int, int) int, allocate func() (string, error)) (storage.WorldReceipt, error) {
	request, err := json.Marshal(struct {
		Kind, Actor string
		Generation  uint64
		Now         int32
		View        world.SceneOptions
	}{"enter", lease.ActorID, lease.Generation, now, view})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	var receipt storage.WorldReceipt
	err = o.Admit(lease, func() error {
		var err error
		receipt, err = engine.Execute(ctx, store, worldID, commandID, request, func(raw json.RawMessage) (json.RawMessage, json.RawMessage, error) {
			s, err := world.DecodeState(raw)
			if err != nil {
				return nil, nil, err
			}
			// Nil Items explicitly means migration is incomplete; don't admit it as a
			// full runtime character merely because legacy room placement accepts it.
			if p, ok := s.Players[lease.ActorID]; !ok || p.Items == nil {
				return nil, nil, errAdmissionCharacter
			}
			next, entry, err := s.EnterSavedPlayerWithIDs(lease.ActorID, view, catalog, now, roll, allocate)
			if err != nil {
				return nil, nil, err
			}
			state, err := json.Marshal(next)
			if err != nil {
				return nil, nil, err
			}
			response, err := json.Marshal(entry)
			return state, response, err
		})
		return err
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return receipt, nil
}
