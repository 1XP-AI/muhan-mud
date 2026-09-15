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

// ErrUnsupportedAbsorbLine is returned before a durable receipt for text
// outside the bounded original `흡성대법 <대상>` command.
var ErrUnsupportedAbsorbLine = errors.New("line is not an implemented absorb command")

// AbsorbCommand carries one display-name selector.  World identity,
// visibility, permission, cooldown, and mutable state remain server-owned.
type AbsorbCommand struct {
	Target     string
	Occurrence int
}

// AbsorbOptions binds the host-owned clock and random source.  Neither value
// is accepted from the terminal line; a command-ID replay returns its stored
// receipt before this reducer is called.
type AbsorbOptions struct {
	Now  int32
	Roll func(int, int) int
}

type absorbLineRequest struct {
	Kind       string `json:"kind"`
	Line       string `json:"line"`
	Target     string `json:"target"`
	Occurrence int    `json:"occurrence"`
	Now        int32  `json:"now"`
}

func validAbsorbLine(line string) bool {
	if !utf8.ValidString(line) {
		return false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\r' || r == '\n' || r == '\x00' || r == '\x1b' || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

// ParseAbsorbLine admits the source alias and exactly one non-whitespace
// target token.  Prefix/key/quoted selectors remain outside this bounded
// parser; occurrence is exposed on the world API for canonical callers.
func ParseAbsorbLine(line string) (AbsorbCommand, bool) {
	if !validAbsorbLine(line) {
		return AbsorbCommand{}, false
	}
	tokens := strings.Fields(strings.TrimSpace(line))
	if len(tokens) != 2 || tokens[0] != "흡성대법" || tokens[1] == "" || strings.TrimSpace(tokens[1]) != tokens[1] {
		return AbsorbCommand{}, false
	}
	for _, r := range tokens[1] {
		if unicode.IsSpace(r) {
			return AbsorbCommand{}, false
		}
	}
	return AbsorbCommand{Target: tokens[1], Occurrence: 1}, true
}

func IsAbsorbLine(line string) bool {
	_, ok := ParseAbsorbLine(line)
	return ok
}

func ParseAbsorbCommandLine(line string) (AbsorbCommand, bool) { return ParseAbsorbLine(line) }
func IsAbsorbCommandLine(line string) bool                     { return IsAbsorbLine(line) }

// ExecuteAbsorbLine is the durable terminal boundary.  The world reducer is
// evaluated only after ExecuteGame's command-ID replay fence, so a retry does
// not rerun target lookup, random draw, or HP/MP arithmetic.
func (o *Ownership) ExecuteAbsorbLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32, roll func(int, int) int) (storage.WorldReceipt, error) {
	return o.ExecuteAbsorbLineWithOptions(ctx, store, worldID, commandID, lease, line, AbsorbOptions{Now: now, Roll: roll})
}

func (o *Ownership) ExecuteAbsorbLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options AbsorbOptions) (storage.WorldReceipt, error) {
	command, ok := ParseAbsorbLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedAbsorbLine
	}
	payload, err := json.Marshal(absorbLineRequest{Kind: "absorb", Line: line, Target: command.Target, Occurrence: command.Occurrence, Now: options.Now})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		s, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := s.PlanAbsorbWithOccurrence(actorID, command.Target, command.Occurrence, options.Now, options.Roll)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := s.ApplyAbsorb(proposal)
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

func (o *Ownership) ExecuteAbsorbCommandLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32, roll func(int, int) int) (storage.WorldReceipt, error) {
	return o.ExecuteAbsorbLine(ctx, store, worldID, commandID, lease, line, now, roll)
}

func (o *Ownership) ExecuteAbsorbCommandLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options AbsorbOptions) (storage.WorldReceipt, error) {
	return o.ExecuteAbsorbLineWithOptions(ctx, store, worldID, commandID, lease, line, options)
}

func (o *Ownership) ExecuteAbsorb(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32, roll func(int, int) int) (storage.WorldReceipt, error) {
	return o.ExecuteAbsorbLine(ctx, store, worldID, commandID, lease, line, now, roll)
}
