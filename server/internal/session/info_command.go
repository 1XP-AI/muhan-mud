package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

// ErrUnsupportedInfoLine keeps command4.c's continuation and argument forms
// out of this first-page slice. The [엔터] -> info_2 spell display, title
// lookup and document-backed help remain explicit follow-up work.
var ErrUnsupportedInfoLine = errors.New("line is not an implemented info command")

// ExecuteInfoLine renders the deterministic first page of command4.c info.
// It is a no-state-change receipt: PlayerInfo validates and reads the
// canonical snapshot, while ExecuteGame persists the response for exact retry.
func (o *Ownership) ExecuteInfoLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	if strings.TrimSpace(line) != "정보" {
		return storage.WorldReceipt{}, ErrUnsupportedInfoLine
	}
	payload, err := json.Marshal(struct{ Line string }{Line: "정보"})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		text, err := state.PlayerInfo(actorID)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(text)
		return raw, response, err
	})
}
