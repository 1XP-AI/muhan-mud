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

var ErrUnsupportedYellLine = errors.New("line is not an implemented yell command")

// YellLineText keeps the command verb out of the player text while retaining
// all remaining spaces, matching the line-oriented C command boundary.
func YellLineText(line string) (string, bool) {
	line = strings.TrimSpace(line)
	if line == "외쳐" {
		return "", true
	}
	if strings.HasPrefix(line, "외쳐 ") {
		return strings.TrimSpace(strings.TrimPrefix(line, "외쳐")), true
	}
	return "", false
}

func (o *Ownership) ExecuteYellLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	text, ok := YellLineText(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedYellLine
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
		next, result, err := s.PlanYell(actorID, text)
		if err != nil {
			return nil, nil, err
		}
		state, err := json.Marshal(next)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result.Response)
		return state, response, err
	})
}
