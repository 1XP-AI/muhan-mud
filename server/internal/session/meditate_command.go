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

var ErrUnsupportedMeditateLine = errors.New("line is not an implemented meditate command")

// MeditateCommand carries only the exact admitted bare alias.  Actor identity,
// permission and all mutable state remain derived by the world reducer.
type MeditateCommand struct{ Alias string }

type MeditationCommand = MeditateCommand

type MeditateOptions struct {
	Now  int32
	Roll func(int, int) int
}

type MeditationOptions = MeditateOptions

type meditateLineRequest struct {
	Kind  string `json:"kind"`
	Line  string `json:"line"`
	Alias string `json:"alias"`
	Now   int32  `json:"now"`
}

func validMeditateLine(line string) bool {
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

// ParseMeditateLine admits only the source's exact bare Korean alias.  Outer
// terminal whitespace is tolerated consistently with other session commands;
// extra tokens, alternate spellings, and internal whitespace are rejected.
func ParseMeditateLine(line string) (MeditateCommand, bool) {
	if !validMeditateLine(line) || strings.TrimSpace(line) != "참선" {
		return MeditateCommand{}, false
	}
	return MeditateCommand{Alias: "참선"}, true
}

func IsMeditateLine(line string) bool {
	_, ok := ParseMeditateLine(line)
	return ok
}

func ParseMeditationLine(line string) (MeditationCommand, bool) { return ParseMeditateLine(line) }
func IsMeditationLine(line string) bool                         { return IsMeditateLine(line) }

// ExecuteMeditateLine is the durable command boundary.  The injected clock
// and random source are used only on the first reducer evaluation; ExecuteGame
// returns the stored response on a command-ID replay before invoking it.
func (o *Ownership) ExecuteMeditateLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32, roll func(int, int) int) (storage.WorldReceipt, error) {
	return o.ExecuteMeditateLineWithOptions(ctx, store, worldID, commandID, lease, line, MeditateOptions{Now: now, Roll: roll})
}

func (o *Ownership) ExecuteMeditateLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options MeditateOptions) (storage.WorldReceipt, error) {
	command, ok := ParseMeditateLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedMeditateLine
	}
	payload, err := json.Marshal(meditateLineRequest{Kind: "meditate", Line: line, Alias: command.Alias, Now: options.Now})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanMeditate(actorID, options.Now, options.Roll)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyMeditate(proposal)
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

// ExecuteMeditationLine is a spelling alias for ExecuteMeditateLine.
func (o *Ownership) ExecuteMeditationLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32, roll func(int, int) int) (storage.WorldReceipt, error) {
	return o.ExecuteMeditateLine(ctx, store, worldID, commandID, lease, line, now, roll)
}

func (o *Ownership) ExecuteMeditationLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options MeditationOptions) (storage.WorldReceipt, error) {
	return o.ExecuteMeditateLineWithOptions(ctx, store, worldID, commandID, lease, line, options)
}
