package session

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

// NPCScavengeOptions binds one MSCAVE phase to the owning cadence slot. Roll
// is allowed to be nil for a provably non-due pass; PlanNPCScavengeTick will
// reject it only if an eligible NPC actually reaches a due roll.
type NPCScavengeOptions struct {
	Slot int64
	Now  int32
	Roll func(int, int) int
}

// NPCScavengeReceiptOptions is a descriptive alias for callers that name the
// boundary after its durable receipt role.
type NPCScavengeReceiptOptions = NPCScavengeOptions

// NPCScavengeTickOptions is a descriptive alias for transport callers that
// name the command boundary after its cadence role.
type NPCScavengeTickOptions = NPCScavengeOptions

type npcScavengeRequest struct {
	Kind string `json:"kind"`
	Slot int64  `json:"slot"`
	Now  int32  `json:"now"`
}

var errInvalidNPCScavengeReceipt = errors.New("invalid NPC scavenge receipt input")

// ExecuteNPCScavengeReceipt persists one ordered MSCAVE pass through the
// engine command boundary. engine.Execute checks the existing receipt before
// invoking the reducer, so a replay returns the original rolls/events without
// calling the injected RNG or writing a second world revision.
func ExecuteNPCScavengeReceipt(ctx context.Context, store engine.CommandStore, worldID, commandID string, options NPCScavengeOptions) (storage.WorldReceipt, error) {
	if options.Slot < 0 || options.Now < 0 {
		return storage.WorldReceipt{}, errInvalidNPCScavengeReceipt
	}
	request, err := json.Marshal(npcScavengeRequest{
		Kind: "npc-scavenge-phase",
		Slot: options.Slot,
		Now:  options.Now,
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return engine.Execute(ctx, store, worldID, commandID, request, func(raw json.RawMessage) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanNPCScavengeTick(options.Now, options.Roll)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyNPCScavengeTick(proposal)
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

// ExecuteNPCScavengeTickReceipt is a descriptive alias for the same durable
// receipt boundary; it does not create a second reducer or command kind.
func ExecuteNPCScavengeTickReceipt(ctx context.Context, store engine.CommandStore, worldID, commandID string, options NPCScavengeTickOptions) (storage.WorldReceipt, error) {
	return ExecuteNPCScavengeReceipt(ctx, store, worldID, commandID, options)
}
