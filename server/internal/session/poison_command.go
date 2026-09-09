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

// ErrUnsupportedPoisonLine is returned before a durable receipt exists for a
// line outside the bounded `독살포 <대상>` form.
var ErrUnsupportedPoisonLine = errors.New("line is not an implemented poison command")

// PoisonCommand carries the one-token display-name selector. The world
// reducer resolves canonical identity, permission, visibility, and state.
type PoisonCommand struct {
	Target string
}

// PoisonOptions supplies server-owned clock and randomness. Neither value is
// accepted from the terminal line; command-ID replay returns its receipt.
type PoisonOptions struct {
	Now  int32
	Roll func(int, int) int
}

type poisonLineRequest struct {
	Kind   string `json:"kind"`
	Line   string `json:"line"`
	Target string `json:"target"`
	Now    int32  `json:"now"`
}

func validPoisonLine(line string) bool {
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

// ParsePoisonLine accepts the source alias and exactly one non-whitespace
// selector token. Prefix, key, quoted multi-word, and occurrence forms remain
// outside this bounded parser.
func ParsePoisonLine(line string) (PoisonCommand, bool) {
	if !validPoisonLine(line) {
		return PoisonCommand{}, false
	}
	fields := strings.Fields(line)
	if len(fields) != 2 || fields[0] != "독살포" || fields[1] == "" || strings.TrimSpace(fields[1]) != fields[1] {
		return PoisonCommand{}, false
	}
	for _, r := range fields[1] {
		if unicode.IsSpace(r) {
			return PoisonCommand{}, false
		}
	}
	return PoisonCommand{Target: fields[1]}, true
}

func IsPoisonLine(line string) bool {
	_, ok := ParsePoisonLine(line)
	return ok
}

func ParsePoisonCommandLine(line string) (PoisonCommand, bool) { return ParsePoisonLine(line) }
func IsPoisonCommandLine(line string) bool                     { return IsPoisonLine(line) }

// ExecutePoisonLine is the durable terminal boundary. Planning and applying
// occur only after ExecuteGame's command-ID replay fence, so a replay cannot
// rerun target lookup or any random draw.
func (o *Ownership) ExecutePoisonLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32, roll func(int, int) int) (storage.WorldReceipt, error) {
	return o.ExecutePoisonLineWithOptions(ctx, store, worldID, commandID, lease, line, PoisonOptions{Now: now, Roll: roll})
}

func (o *Ownership) ExecutePoisonLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options PoisonOptions) (storage.WorldReceipt, error) {
	command, ok := ParsePoisonLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedPoisonLine
	}
	payload, err := json.Marshal(poisonLineRequest{Kind: "poison", Line: line, Target: command.Target, Now: options.Now})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanPoison(actorID, command.Target, options.Now, options.Roll)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyPoison(proposal)
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

func (o *Ownership) ExecutePoisonCommandLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32, roll func(int, int) int) (storage.WorldReceipt, error) {
	return o.ExecutePoisonLine(ctx, store, worldID, commandID, lease, line, now, roll)
}

func (o *Ownership) ExecutePoisonCommandLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options PoisonOptions) (storage.WorldReceipt, error) {
	return o.ExecutePoisonLineWithOptions(ctx, store, worldID, commandID, lease, line, options)
}

func (o *Ownership) ExecutePoison(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32, roll func(int, int) int) (storage.WorldReceipt, error) {
	return o.ExecutePoisonLine(ctx, store, worldID, commandID, lease, line, now, roll)
}
