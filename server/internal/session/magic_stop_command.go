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

// ErrUnsupportedMagicStopLine is returned before a receipt exists when a
// line is outside the bounded original `혈도봉쇄 <대상>` form.
var ErrUnsupportedMagicStopLine = errors.New("line is not an implemented magic_stop command")

// MagicStopCommand contains the one-token selector accepted by the terminal
// boundary. Canonical display/key identity is resolved by the world reducer;
// terminal occurrence selectors are not part of this bounded parser.
type MagicStopCommand struct {
	Target     string
	Occurrence int
}

// MagicStopOptions binds server-owned clock and randomness. They never come
// from the terminal line; a replay returns the durable receipt first.
type MagicStopOptions struct {
	Now  int32
	Roll func(int, int) int
}

type magicStopLineRequest struct {
	Kind       string `json:"kind"`
	Line       string `json:"line"`
	Target     string `json:"target"`
	Occurrence int    `json:"occurrence"`
	Now        int32  `json:"now"`
}

func validMagicStopLine(line string) bool {
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

// ParseMagicStopLine admits the source alias and one non-whitespace target
// token. The world reducer resolves that token against an exact canonical
// display name or key; prefix, occurrence, and quoted/multi-word selectors
// remain outside this bounded parser.
func ParseMagicStopLine(line string) (MagicStopCommand, bool) {
	if !validMagicStopLine(line) {
		return MagicStopCommand{}, false
	}
	tokens := strings.Fields(strings.TrimSpace(line))
	if len(tokens) != 2 || tokens[0] != "혈도봉쇄" {
		return MagicStopCommand{}, false
	}
	if tokens[1] == "" || strings.TrimSpace(tokens[1]) != tokens[1] {
		return MagicStopCommand{}, false
	}
	return MagicStopCommand{Target: tokens[1], Occurrence: 1}, true
}

func IsMagicStopLine(line string) bool {
	_, ok := ParseMagicStopLine(line)
	return ok
}

// ExecuteMagicStopLine is the durable terminal boundary. The first attempt
// plans/applies against one snapshot and commits a receipt; retries with the
// same command ID replay that response without rerunning lookup or RNG.
func (o *Ownership) ExecuteMagicStopLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32, roll func(int, int) int) (storage.WorldReceipt, error) {
	return o.ExecuteMagicStopLineWithOptions(ctx, store, worldID, commandID, lease, line, MagicStopOptions{Now: now, Roll: roll})
}

func (o *Ownership) ExecuteMagicStopLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options MagicStopOptions) (storage.WorldReceipt, error) {
	command, ok := ParseMagicStopLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedMagicStopLine
	}
	payload, err := json.Marshal(magicStopLineRequest{
		Kind: "magic_stop", Line: line, Target: command.Target,
		Occurrence: command.Occurrence, Now: options.Now,
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		s, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := s.PlanMagicStopWithOccurrence(actorID, command.Target, command.Occurrence, options.Now, options.Roll)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := s.ApplyMagicStop(proposal)
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

// ExecuteMagicStop is a concise alias for callers that name the operation
// without its terminal-line suffix.
func (o *Ownership) ExecuteMagicStop(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32, roll func(int, int) int) (storage.WorldReceipt, error) {
	return o.ExecuteMagicStopLine(ctx, store, worldID, commandID, lease, line, now, roll)
}
