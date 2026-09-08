package session

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
	"strings"
)

var ErrUnsupportedLookLine = errors.New("line is not an implemented bare look command")

// ExecuteLookLine handles only the original exact bare aliases. It does not
// silently turn target inspection or other commands into a room look. The full
// legacy parser (abbreviation, argument order and other verbs) is pending.
// Hour is server-owned. Rendering is replayed from the committed receipt.
func (o *Ownership) ExecuteLookLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, hour int) (storage.WorldReceipt, error) {
	verb := strings.TrimSpace(line)
	switch verb {
	case "봐", "보다", "조사":
	default:
		return storage.WorldReceipt{}, ErrUnsupportedLookLine
	}
	payload, err := json.Marshal(struct {
		Line string
		Hour int
	}{line, hour})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		s, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		scene, err := s.CurrentScene(actorID, hour)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(scene)
		return raw, response, err
	})
}
