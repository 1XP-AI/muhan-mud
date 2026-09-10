package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

var ErrUnsupportedCastLine = errors.New("line is not an implemented cast command")

// CastCommand keeps the original Korean command boundary separate from the
// source spell index.  An empty spell is the original `주문` prompt; target
// forms remain unsupported until targeted spell state is migrated.
type CastCommand struct {
	SpellName string
}

type castLineRequest struct {
	Kind      string `json:"kind"`
	Line      string `json:"line"`
	SpellName string `json:"spell_name"`
	Now       int32  `json:"now"`
}

type CastOptions = world.CastOptions

func validCastLine(line string) bool {
	if !utf8.ValidString(line) {
		return false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' || r == '\x00' {
			return false
		}
	}
	return true
}

// ParseCastLine admits the original `주문` prompt and its self-target form
// `주문 <주문명>`.  A third token is deliberately rejected rather than
// routing a targeted spell to a self-only reducer.
func ParseCastLine(line string) (CastCommand, bool) {
	if !validCastLine(line) {
		return CastCommand{}, false
	}
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || len(tokens) < 1 || len(tokens) > 2 || tokens[0] != "주문" {
		return CastCommand{}, false
	}
	if len(tokens) == 1 {
		return CastCommand{}, true
	}
	if tokens[1] == "" || strings.TrimSpace(tokens[1]) != tokens[1] {
		return CastCommand{}, false
	}
	return CastCommand{SpellName: tokens[1]}, true
}

func IsCastLine(line string) bool {
	_, ok := ParseCastLine(line)
	return ok
}

// ExecuteCastLineWithOptions binds cast()'s self-target reducer to a durable
// receipt.  Roll is consumed only by the first reducer invocation; engine
// replay returns the stored CastResult unchanged.
func (o *Ownership) ExecuteCastLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options CastOptions) (storage.WorldReceipt, error) {
	command, ok := ParseCastLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedCastLine
	}
	payload, err := json.Marshal(castLineRequest{Kind: "cast", Line: line, SpellName: command.SpellName, Now: options.Now})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanCast(actorID, command.SpellName, options)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyCast(proposal)
		if err != nil {
			return nil, nil, err
		}
		nextRaw, err := json.Marshal(next)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		return nextRaw, response, err
	})
}

func (o *Ownership) ExecuteCastLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32, roll func(int, int) int) (storage.WorldReceipt, error) {
	return o.ExecuteCastLineWithOptions(ctx, store, worldID, commandID, lease, line, CastOptions{Now: now, Roll: roll})
}
