package session

import (
	"context"
	"encoding/json"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

// spellListRequest identifies the command4.c info_2 continuation. The
// connection-local [엔터] continuation is consumed by the transport; this
// adapter owns only the durable read-only receipt once that continuation is
// admitted.
type spellListRequest struct {
	Kind string `json:"kind"`
}

// ExecuteSpellList persists the canonical info_2 known-spell projection as a
// no-state-change receipt. The caller must reuse commandID on an uncertain
// response; engine replay then returns the original response without reading
// the world or rerunning any effect. Parser/transport wiring remains outside
// this file so the existing info continuation can adopt it explicitly.
func (o *Ownership) ExecuteSpellList(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease) (storage.WorldReceipt, error) {
	payload, err := json.Marshal(spellListRequest{Kind: "spell_list"})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		_, result, err := state.SpellList(actorID)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		// SpellList is read-only: preserve the exact loaded snapshot bytes so
		// a receipt does not rewrite unrelated map ordering or optional fields.
		return raw, response, err
	})
}

// ExecuteKnownSpellList is a descriptive alias for continuation callers.
func (o *Ownership) ExecuteKnownSpellList(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease) (storage.WorldReceipt, error) {
	return o.ExecuteSpellList(ctx, store, worldID, commandID, lease)
}
