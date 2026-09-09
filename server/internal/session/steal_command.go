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

// ErrUnsupportedStealLine is returned before a durable receipt exists when a
// line is outside the bounded original `훔쳐 <물건> <대상>` form.
var ErrUnsupportedStealLine = errors.New("line is not an implemented steal command")

// StealCommand is the display-name portion of the command. The world reducer
// resolves both names to canonical IDs from the committed room snapshot.
type StealCommand struct {
	Item   string
	Target string
}

// StealOptions binds host-owned time and randomness to one command attempt.
// A receipt replay never invokes Roll again.
type StealOptions struct {
	Now  int32
	Roll func(int, int) int
}

type stealLineRequest struct {
	Kind   string `json:"kind"`
	Line   string `json:"line"`
	Item   string `json:"item"`
	Target string `json:"target"`
	Now    int32  `json:"now"`
}

// ParseStealLine recognizes exactly two non-whitespace display-name tokens.
// Prefix/occurrence selectors and quoted multi-word names remain out of this
// bounded slice until their canonical identity contract is admitted.
func ParseStealLine(line string) (StealCommand, bool) {
	if !utf8.ValidString(line) {
		return StealCommand{}, false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\r' || r == '\n' || r == '\x00' || r == '\u2028' || r == '\u2029' {
			return StealCommand{}, false
		}
	}
	fields := strings.Fields(line)
	if len(fields) != 3 || fields[0] != "훔쳐" {
		return StealCommand{}, false
	}
	if fields[1] == "" || fields[2] == "" || strings.TrimSpace(fields[1]) != fields[1] || strings.TrimSpace(fields[2]) != fields[2] {
		return StealCommand{}, false
	}
	return StealCommand{Item: fields[1], Target: fields[2]}, true
}

func IsStealLine(line string) bool {
	_, ok := ParseStealLine(line)
	return ok
}

// ExecuteStealLine is the command boundary used by terminal sessions. The
// request hash includes the exact line and host-owned clock, while all target,
// permission, inventory and random decisions are made by world.PlanSteal.
func (o *Ownership) ExecuteStealLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32, roll func(int, int) int) (storage.WorldReceipt, error) {
	return o.ExecuteStealLineWithOptions(ctx, store, worldID, commandID, lease, line, StealOptions{Now: now, Roll: roll})
}

func (o *Ownership) ExecuteStealLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options StealOptions) (storage.WorldReceipt, error) {
	command, ok := ParseStealLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedStealLine
	}
	payload, err := json.Marshal(stealLineRequest{Kind: "steal", Line: line, Item: command.Item, Target: command.Target, Now: options.Now})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		s, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := s.PlanSteal(actorID, command.Item, command.Target, options.Now, options.Roll)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := s.ApplySteal(proposal)
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
