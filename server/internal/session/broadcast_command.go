package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

var ErrUnsupportedBroadcastLine = errors.New("line is not an implemented broadcast command")

type BroadcastCommand struct {
	Kind world.BroadcastKind
	Text string
}

// ParseBroadcastLine recognizes only the exact public aliases. The suffix is
// kept as one bounded message rather than passing it through the seven-token
// legacy parser: C's broadsend reads cmnd.fullstr and accepts a full 255-byte
// line even though parse() stops populating command tokens at COMMANDMAX.
func ParseBroadcastLine(line string) (BroadcastCommand, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return BroadcastCommand{}, false
	}
	aliasEnd := strings.IndexFunc(trimmed, unicode.IsSpace)
	alias := trimmed
	remainder := ""
	if aliasEnd >= 0 {
		alias = trimmed[:aliasEnd]
		remainder = strings.TrimSpace(trimmed[aliasEnd:])
	}
	var kind world.BroadcastKind
	switch alias {
	case "잡담", "잡":
		kind = world.BroadcastChat
	case "환호":
		kind = world.BroadcastCheer
	default:
		return BroadcastCommand{}, false
	}
	return BroadcastCommand{Kind: kind, Text: remainder}, true
}

type broadcastLineRequest struct {
	Kind     world.BroadcastKind `json:"kind"`
	Line     string              `json:"line"`
	Text     string              `json:"text"`
	Now      int32               `json:"now"`
	LastAt   int32               `json:"last_at"`
	GlobalAt int32               `json:"global_at"`
}

// ExecuteBroadcastLine persists HP/daily usage and the actor response. The
// global event is kept inside the receipt response; transport publishes it
// only after a first commit, so receipt replay cannot broadcast twice.
func (o *Ownership) ExecuteBroadcastLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options world.BroadcastOptions) (storage.WorldReceipt, error) {
	command, ok := ParseBroadcastLine(line)
	if !ok || world.ValidateBroadcastText(command.Text) != nil {
		return storage.WorldReceipt{}, ErrUnsupportedBroadcastLine
	}
	payload, err := json.Marshal(broadcastLineRequest{Kind: command.Kind, Line: line, Text: command.Text, Now: options.Now, LastAt: options.LastAt, GlobalAt: options.GlobalAt})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		s, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := s.PlanBroadcast(actorID, command.Kind, command.Text, options)
		if err != nil {
			return nil, nil, err
		}
		state, err := json.Marshal(next)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		return state, response, err
	})
}
