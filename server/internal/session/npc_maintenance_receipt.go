package session

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

// NPCMaintenanceOptions binds the world-tick boundary to a deterministic
// slot/time and RNG supplied by the owning world loop. It is intentionally a
// session-package adapter rather than a WorldConnector change: this bounded
// slice can be tested and replayed before the full NPC scheduler is admitted.
type NPCMaintenanceOptions struct {
	Slot int64
	Now  int32
	Roll func(int, int) int
}

type npcMaintenanceRequest struct {
	Kind string `json:"kind"`
	Slot int64  `json:"slot"`
	Now  int32  `json:"now"`
}

var errInvalidNPCMaintenanceReceipt = errors.New("invalid NPC maintenance receipt input")

// ExecuteNPCMaintenanceReceipt persists the pre-combat NPC update_active
// prefix as one idempotent world command. engine.Execute checks the durable
// receipt before decoding or reducing the world, so replay never consumes the
// injected RNG a second time.
func ExecuteNPCMaintenanceReceipt(ctx context.Context, store engine.CommandStore, worldID, commandID string, options NPCMaintenanceOptions) (storage.WorldReceipt, error) {
	if options.Slot < 0 || options.Roll == nil {
		return storage.WorldReceipt{}, errInvalidNPCMaintenanceReceipt
	}
	request, err := json.Marshal(npcMaintenanceRequest{Kind: "npc-maintenance", Slot: options.Slot, Now: options.Now})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return engine.Execute(ctx, store, worldID, commandID, request, func(raw json.RawMessage) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanNPCMaintenance(options.Now, options.Roll)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyNPCMaintenance(proposal)
		if err != nil {
			return nil, nil, err
		}
		saved, err := json.Marshal(next)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		return saved, response, err
	})
}
