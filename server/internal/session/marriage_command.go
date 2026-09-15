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

var ErrUnsupportedMarriageLine = errors.New("line is not an implemented marriage command")

// MarriageCommand is the parser-owned projection of command11.c's marriage
// alias. An empty target is meaningful: it cancels an actor's pending request
// before any target lookup.
type MarriageCommand struct {
	Target string
}

type marriageLineRequest struct {
	Kind   string `json:"kind"`
	Line   string `json:"line"`
	Target string `json:"target,omitempty"`
}

func validMarriageLine(line string) bool {
	if !utf8.ValidString(line) {
		return false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

// ParseMarriageLine admits the direct one-line forms understood by the
// terminal adapter. The legacy parser places the command in the final token,
// while prefix form is convenient for clients; both forms map to the same
// target and no multi-token/interactive continuation is inferred.
func ParseMarriageLine(line string) (MarriageCommand, bool) {
	if !validMarriageLine(line) {
		return MarriageCommand{}, false
	}
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || len(tokens) == 0 || len(tokens) > 2 {
		return MarriageCommand{}, false
	}
	if len(tokens) == 1 {
		if tokens[0] != "결혼" {
			return MarriageCommand{}, false
		}
		return MarriageCommand{}, true
	}
	if tokens[0] == "결혼" && tokens[1] != "결혼" {
		return MarriageCommand{Target: tokens[1]}, true
	}
	if tokens[1] == "결혼" && tokens[0] != "결혼" {
		return MarriageCommand{Target: tokens[0]}, true
	}
	return MarriageCommand{}, false
}

func IsMarriageLine(line string) bool {
	_, ok := ParseMarriageLine(line)
	return ok
}

// ExecuteMarriageLine binds the parser projection to the actor-owned durable
// receipt. The reducer resolves target identity and applies both partners in
// one snapshot transaction; recipient/broadcast events are delivered only by
// transport after the receipt is confirmed.
func (o *Ownership) ExecuteMarriageLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	command, ok := ParseMarriageLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedMarriageLine
	}
	payload, err := json.Marshal(marriageLineRequest{Kind: "marriage", Line: line, Target: command.Target})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanMarriage(actorID, command.Target)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyMarriage(proposal)
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

// ExecuteMarriageCommandLine is a source-oriented alias for adapters that
// use the numeric command name rather than the command11.c function name.
func (o *Ownership) ExecuteMarriageCommandLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	return o.ExecuteMarriageLine(ctx, store, worldID, commandID, lease, line)
}
