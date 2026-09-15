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

var ErrUnsupportedSayLine = errors.New("line is not an implemented say command")

// SayLineText recognizes the legacy say aliases that can be represented by a
// complete terminal line. It intentionally does not treat arbitrary prefixes
// as aliases; occurrence/abbreviation parsing remains a later command layer.
func SayLineText(line string) (string, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", false
	}
	if line == "말" {
		return "", true
	}
	if strings.HasPrefix(line, "말 ") {
		return strings.TrimSpace(strings.TrimPrefix(line, "말")), true
	}
	if line[0] == '"' || line[0] == '\'' {
		return strings.TrimSpace(line[1:]), true
	}
	return "", false
}

func (o *Ownership) ExecuteSayLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	text, ok := SayLineText(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedSayLine
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
		next, result, err := s.PlanSay(actorID, text)
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
