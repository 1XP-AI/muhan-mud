package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

var ErrUnsupportedDMActiveLine = errors.New("line is not an implemented DM active command")

// DMActiveCommand is the exact, argument-free form of update.c:list_act.
type DMActiveCommand struct {
	Verb string
}

type dmActiveLineRequest struct {
	Kind string `json:"kind"`
	Line string `json:"line"`
	Verb string `json:"verb"`
}

func validDMActiveLine(line string) bool {
	if !utf8.ValidString(line) {
		return false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' || (unicode.IsSpace(r) && r != ' ') {
			return false
		}
	}
	return true
}

// ParseDMActiveLine admits only the two registered aliases. It deliberately
// does not expand prefixes, parse arguments, or accept a quoted alias.
func ParseDMActiveLine(line string) (DMActiveCommand, bool) {
	if !validDMActiveLine(line) {
		return DMActiveCommand{}, false
	}
	switch strings.TrimSpace(line) {
	case "*active", "*활성":
		return DMActiveCommand{Verb: strings.TrimSpace(line)}, true
	default:
		return DMActiveCommand{}, false
	}
}

func IsDMActiveLine(line string) bool {
	_, ok := ParseDMActiveLine(line)
	return ok
}

// ExecuteDMActiveLine persists one read-only list_act projection. The exact
// loaded state bytes are returned unchanged, so a receipt cannot rewrite or
// mutate unrelated world state; ApplyDMActive only rechecks the projection's
// stale boundary.
func (o *Ownership) ExecuteDMActiveLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	command, ok := ParseDMActiveLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedDMActiveLine
	}
	payload, err := json.Marshal(dmActiveLineRequest{Kind: "dm-active", Line: line, Verb: command.Verb})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, fmt.Errorf("%w: %v", world.ErrDMActiveStateInvalid, err)
		}
		proposal, err := state.PlanDMActive(actorID, command.Verb)
		if err != nil {
			return nil, nil, err
		}
		_, result, err := state.ApplyDMActive(proposal)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		return raw, response, err
	})
}
