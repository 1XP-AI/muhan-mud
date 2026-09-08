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

var ErrUnsupportedStatusLine = errors.New("line is not an implemented status command")

// ExecuteStatusLine handles the exact health/score aliases. It is a durable
// no-state-change receipt so a lost response is replayed without recalculating
// from a newer snapshot under the same command ID.
func (o *Ownership) ExecuteStatusLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	verb := strings.TrimSpace(line)
	if verb != "건강" && verb != "점수" {
		return storage.WorldReceipt{}, ErrUnsupportedStatusLine
	}
	payload, err := json.Marshal(struct{ Line string }{line})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		s, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		text, err := s.PlayerStatus(actorID)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(text)
		return raw, response, err
	})
}
