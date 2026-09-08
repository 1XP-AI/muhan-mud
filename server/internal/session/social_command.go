package session

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

var ErrUnsupportedSocialLine = errors.New("line is not an implemented social status command")

func socialCommand(line string) (string, bool) {
	switch line {
	case "누구":
		return "who", true
	case "그룹":
		return "group", true
	default:
		return "", false
	}
}

func (o *Ownership) ExecuteSocialLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	kind, ok := socialCommand(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedSocialLine
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
		var text string
		if kind == "who" {
			text, err = s.PlayerWho(actorID)
		} else {
			text, err = s.PlayerGroup(actorID)
		}
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(text)
		return raw, response, err
	})
}
