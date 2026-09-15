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
	ErrUnsupportedHasteLine      = errors.New("line is not an implemented haste command")
	ErrUnsupportedPrayLine       = errors.New("line is not an implemented pray command")
	ErrUnsupportedRangerPrayLine = errors.New("line is not an implemented haste/pray command")
)

type HasteCommand struct{ Alias string }
type PrayCommand struct{ Alias string }
type PrayerCommand = PrayCommand

// RangerPrayCommand is the closed parser result for either exact bare alias.
// It intentionally carries no target, occurrence, or client-selected state.
type RangerPrayCommand struct {
	Kind  world.RangerPrayKind
	Alias string
}

type HasteOptions struct {
	Now  int32
	Roll func(int, int) int
}

type PrayOptions struct {
	Now  int32
	Roll func(int, int) int
}

type PrayerOptions = PrayOptions

type RangerPrayOptions struct {
	Now  int32
	Roll func(int, int) int
}

type rangerPrayLineRequest struct {
	Kind string `json:"kind"`
	Line string `json:"line"`
	Now  int32  `json:"now"`
}

func validRangerPrayLine(line string) bool {
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

// ParseHasteLine admits only the exact bare 활보법 alias.  Outer terminal
// whitespace follows the other session parsers; any extra token or internal
// whitespace is rejected by the equality check after trimming.
func ParseHasteLine(line string) (HasteCommand, bool) {
	if !validRangerPrayLine(line) || strings.TrimSpace(line) != "활보법" {
		return HasteCommand{}, false
	}
	return HasteCommand{Alias: "활보법"}, true
}

func IsHasteLine(line string) bool {
	_, ok := ParseHasteLine(line)
	return ok
}

// ParsePrayLine admits only the exact bare 신원법 alias.
func ParsePrayLine(line string) (PrayCommand, bool) {
	if !validRangerPrayLine(line) || strings.TrimSpace(line) != "신원법" {
		return PrayCommand{}, false
	}
	return PrayCommand{Alias: "신원법"}, true
}

func IsPrayLine(line string) bool {
	_, ok := ParsePrayLine(line)
	return ok
}

func ParsePrayerLine(line string) (PrayerCommand, bool) { return ParsePrayLine(line) }
func IsPrayerLine(line string) bool                     { return IsPrayLine(line) }

// ParseRangerPrayLine is useful to dispatch a line without opening an
// argument-bearing parser path.  It is intentionally independent of the
// central parser table so this bounded command can be reviewed in isolation.
func ParseRangerPrayLine(line string) (RangerPrayCommand, bool) {
	if command, ok := ParseHasteLine(line); ok {
		return RangerPrayCommand{Kind: world.RangerHaste, Alias: command.Alias}, true
	}
	if command, ok := ParsePrayLine(line); ok {
		return RangerPrayCommand{Kind: world.RangerPray, Alias: command.Alias}, true
	}
	return RangerPrayCommand{}, false
}

func IsRangerPrayLine(line string) bool {
	_, ok := ParseRangerPrayLine(line)
	return ok
}

func (o *Ownership) ExecuteHasteLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32, roll func(int, int) int) (storage.WorldReceipt, error) {
	return o.ExecuteHasteLineWithOptions(ctx, store, worldID, commandID, lease, line, HasteOptions{Now: now, Roll: roll})
}

func (o *Ownership) ExecuteHasteLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options HasteOptions) (storage.WorldReceipt, error) {
	_, ok := ParseHasteLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedHasteLine
	}
	return o.executeRangerPrayLine(ctx, store, worldID, commandID, lease, world.RangerHaste, line, options.Now, options.Roll)
}

func (o *Ownership) ExecutePrayLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32, roll func(int, int) int) (storage.WorldReceipt, error) {
	return o.ExecutePrayLineWithOptions(ctx, store, worldID, commandID, lease, line, PrayOptions{Now: now, Roll: roll})
}

func (o *Ownership) ExecutePrayLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options PrayOptions) (storage.WorldReceipt, error) {
	_, ok := ParsePrayLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedPrayLine
	}
	return o.executeRangerPrayLine(ctx, store, worldID, commandID, lease, world.RangerPray, line, options.Now, options.Roll)
}

func (o *Ownership) ExecutePrayerLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32, roll func(int, int) int) (storage.WorldReceipt, error) {
	return o.ExecutePrayLine(ctx, store, worldID, commandID, lease, line, now, roll)
}

func (o *Ownership) ExecutePrayerLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options PrayerOptions) (storage.WorldReceipt, error) {
	return o.ExecutePrayLineWithOptions(ctx, store, worldID, commandID, lease, line, options)
}

// ExecuteRangerPrayLine dispatches the two exact bare aliases through the
// same durable receipt boundary.  The random source is captured by the
// caller, while engine.Execute skips this reducer entirely on replay.
func (o *Ownership) ExecuteRangerPrayLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options RangerPrayOptions) (storage.WorldReceipt, error) {
	command, ok := ParseRangerPrayLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedRangerPrayLine
	}
	return o.executeRangerPrayLine(ctx, store, worldID, commandID, lease, command.Kind, line, options.Now, options.Roll)
}

func (o *Ownership) executeRangerPrayLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, kind world.RangerPrayKind, line string, now int32, roll func(int, int) int) (storage.WorldReceipt, error) {
	payload, err := json.Marshal(rangerPrayLineRequest{Kind: string(kind), Line: line, Now: now})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		s, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := s.PlanRangerPray(kind, actorID, now, roll)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := s.ApplyRangerPray(proposal)
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
