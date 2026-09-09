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

var (
	ErrUnsupportedPowerLine         = errors.New("line is not an implemented bare power command")
	ErrUnsupportedAccurateLine      = errors.New("line is not an implemented bare accurate command")
	ErrUnsupportedPowerAccuracyLine = errors.New("line is not an implemented bare power/accurate command")
)

// PowerCommand and AccurateCommand retain only the exact admitted alias. They
// intentionally do not carry actor IDs, state, weapon IDs, or client rolls.
type PowerCommand struct{ Alias string }
type AccurateCommand struct{ Alias string }

type PowerOptions struct {
	Now  int32
	Roll func(int, int) int
}

type AccurateOptions struct {
	Now  int32
	Roll func(int, int) int
}

type PowerAccuracyOptions struct {
	Now  int32
	Roll func(int, int) int
}

// PowerAccuracyCommand is the closed parser result for either exact bare
// alias. No target/occurrence/weapon selection is admitted in this lane.
type PowerAccuracyCommand struct {
	Kind  world.PowerAccuracyKind
	Alias string
}

type powerAccuracyLineRequest struct {
	Kind  string `json:"kind"`
	Line  string `json:"line"`
	Alias string `json:"alias"`
	Now   int32  `json:"now"`
}

func validPowerAccuracyLine(line string) bool {
	if !utf8.ValidString(line) {
		return false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\r' || r == '\n' || r == '\x00' || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

// ParsePowerLine admits only command9.c/global.c's exact bare 기공집결 alias.
// Outer terminal whitespace follows the existing session parser contract;
// extra tokens and internal whitespace are rejected.
func ParsePowerLine(line string) (PowerCommand, bool) {
	if !validPowerAccuracyLine(line) || strings.TrimSpace(line) != "기공집결" {
		return PowerCommand{}, false
	}
	return PowerCommand{Alias: "기공집결"}, true
}

func IsPowerLine(line string) bool {
	_, ok := ParsePowerLine(line)
	return ok
}

// ParseAccurateLine admits only command9.c/global.c's exact bare 살기충전 alias.
func ParseAccurateLine(line string) (AccurateCommand, bool) {
	if !validPowerAccuracyLine(line) || strings.TrimSpace(line) != "살기충전" {
		return AccurateCommand{}, false
	}
	return AccurateCommand{Alias: "살기충전"}, true
}

func IsAccurateLine(line string) bool {
	_, ok := ParseAccurateLine(line)
	return ok
}

func ParsePowerAccuracyLine(line string) (PowerAccuracyCommand, bool) {
	if command, ok := ParsePowerLine(line); ok {
		return PowerAccuracyCommand{Kind: world.PowerAbility, Alias: command.Alias}, true
	}
	if command, ok := ParseAccurateLine(line); ok {
		return PowerAccuracyCommand{Kind: world.AccurateAbility, Alias: command.Alias}, true
	}
	return PowerAccuracyCommand{}, false
}

func IsPowerAccuracyLine(line string) bool {
	_, ok := ParsePowerAccuracyLine(line)
	return ok
}

func (o *Ownership) ExecutePowerLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32, roll func(int, int) int) (storage.WorldReceipt, error) {
	return o.ExecutePowerLineWithOptions(ctx, store, worldID, commandID, lease, line, PowerOptions{Now: now, Roll: roll})
}

func (o *Ownership) ExecutePowerLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options PowerOptions) (storage.WorldReceipt, error) {
	command, ok := ParsePowerLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedPowerLine
	}
	return o.executePowerAccuracyLine(ctx, store, worldID, commandID, lease, line, world.PowerAbility, command.Alias, options.Now, options.Roll)
}

func (o *Ownership) ExecuteAccurateLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32, roll func(int, int) int) (storage.WorldReceipt, error) {
	return o.ExecuteAccurateLineWithOptions(ctx, store, worldID, commandID, lease, line, AccurateOptions{Now: now, Roll: roll})
}

func (o *Ownership) ExecuteAccurateLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options AccurateOptions) (storage.WorldReceipt, error) {
	command, ok := ParseAccurateLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedAccurateLine
	}
	return o.executePowerAccuracyLine(ctx, store, worldID, commandID, lease, line, world.AccurateAbility, command.Alias, options.Now, options.Roll)
}

// ExecutePowerAccuracyLine dispatches the two exact aliases through one
// durable ExecuteGame receipt. The server-owned random source is used only on
// first execution; a replay returns the stored response before this reducer.
func (o *Ownership) ExecutePowerAccuracyLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options PowerAccuracyOptions) (storage.WorldReceipt, error) {
	command, ok := ParsePowerAccuracyLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedPowerAccuracyLine
	}
	return o.executePowerAccuracyLine(ctx, store, worldID, commandID, lease, line, command.Kind, command.Alias, options.Now, options.Roll)
}

func (o *Ownership) executePowerAccuracyLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, kind world.PowerAccuracyKind, alias string, now int32, roll func(int, int) int) (storage.WorldReceipt, error) {
	payload, err := json.Marshal(powerAccuracyLineRequest{Kind: string(kind), Line: line, Alias: alias, Now: now})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanPowerAccuracy(kind, actorID, now, roll)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyPowerAccuracy(proposal)
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

func (o *Ownership) ExecutePowerAccuracyLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options PowerAccuracyOptions) (storage.WorldReceipt, error) {
	return o.ExecutePowerAccuracyLine(ctx, store, worldID, commandID, lease, line, options)
}
