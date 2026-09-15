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

// ErrUnsupportedTurnLine is returned before a durable receipt for a line
// outside the bounded original `방혼술 [대상]` command.
var ErrUnsupportedTurnLine = errors.New("line is not an implemented turn command")

// TurnCommand carries the optional one-token display-name selector. Canonical
// same-room NPC identity, permission, visibility, cooldown, and state remain
// owned by the world reducer.
type TurnCommand struct {
	Target     string
	Occurrence int
}

// TurnOptions binds server-owned time, randomness, and item allocation to one
// command attempt. None of these dependencies are accepted from terminal
// input; an existing command ID replays its stored receipt before reduction.
type TurnOptions struct {
	Now      int32
	Roll     func(int, int) int
	Allocate func() (string, error)
}

type turnLineRequest struct {
	Kind       string `json:"kind"`
	Line       string `json:"line"`
	Target     string `json:"target,omitempty"`
	Occurrence int    `json:"occurrence"`
	Now        int32  `json:"now"`
}

func validTurnLine(line string) bool {
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

// ParseTurnLine admits the original no-argument prompt and the bounded
// one-token target form. Prefix/key, quoted multi-word, and occurrence
// selectors remain outside this command boundary until their canonical
// identity contract is explicitly ported.
func ParseTurnLine(line string) (TurnCommand, bool) {
	if !validTurnLine(line) {
		return TurnCommand{}, false
	}
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) == 0 || fields[0] != "방혼술" || len(fields) > 2 {
		return TurnCommand{}, false
	}
	command := TurnCommand{Occurrence: 1}
	if len(fields) == 2 {
		if fields[1] == "" || strings.TrimSpace(fields[1]) != fields[1] {
			return TurnCommand{}, false
		}
		for _, r := range fields[1] {
			if unicode.IsSpace(r) {
				return TurnCommand{}, false
			}
		}
		command.Target = fields[1]
	}
	return command, true
}

func IsTurnLine(line string) bool {
	_, ok := ParseTurnLine(line)
	return ok
}

func ParseTurnCommandLine(line string) (TurnCommand, bool) { return ParseTurnLine(line) }
func IsTurnCommandLine(line string) bool                   { return IsTurnLine(line) }

// ExecuteTurnLine is the durable terminal boundary for 방혼술. Target lookup
// and random draws happen only after ExecuteGame's receipt replay fence.
func (o *Ownership) ExecuteTurnLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32, roll func(int, int) int) (storage.WorldReceipt, error) {
	return o.ExecuteTurnLineWithOptions(ctx, store, worldID, commandID, lease, line, TurnOptions{Now: now, Roll: roll})
}

func (o *Ownership) ExecuteTurnLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options TurnOptions) (storage.WorldReceipt, error) {
	command, ok := ParseTurnLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedTurnLine
	}
	payload, err := json.Marshal(turnLineRequest{Kind: "turn", Line: line, Target: command.Target, Occurrence: command.Occurrence, Now: options.Now})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		s, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := s.PlanTurnWithOccurrence(actorID, command.Target, command.Occurrence, options.Now, options.Roll, world.TurnOptions{Allocate: options.Allocate})
		if err != nil {
			return nil, nil, err
		}
		next, result, err := s.ApplyTurn(proposal)
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

func (o *Ownership) ExecuteTurnCommandLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32, roll func(int, int) int) (storage.WorldReceipt, error) {
	return o.ExecuteTurnLine(ctx, store, worldID, commandID, lease, line, now, roll)
}

func (o *Ownership) ExecuteTurnCommandLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options TurnOptions) (storage.WorldReceipt, error) {
	return o.ExecuteTurnLineWithOptions(ctx, store, worldID, commandID, lease, line, options)
}

func (o *Ownership) ExecuteTurn(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32, roll func(int, int) int) (storage.WorldReceipt, error) {
	return o.ExecuteTurnLine(ctx, store, worldID, commandID, lease, line, now, roll)
}
