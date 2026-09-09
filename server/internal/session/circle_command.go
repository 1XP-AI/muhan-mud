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

// ErrUnsupportedCircleLine is returned before a receipt exists when a line is
// outside the bounded original `교란 <대상>` form.
var ErrUnsupportedCircleLine = errors.New("line is not an implemented circle command")

// CircleCommand contains only the display-name target. The world reducer
// resolves it against canonical same-room identities during the transaction.
// Prefix/occurrence selectors and quoted multi-word names remain unsupported.
type CircleCommand struct {
	Target string
}

// CircleOptions binds host-owned time and randomness to one command attempt.
// Neither value is accepted from the terminal line, and a receipt replay does
// not invoke Roll again.
type CircleOptions struct {
	Now  int32
	Roll func(int, int) int
}

type circleLineRequest struct {
	Kind   string `json:"kind"`
	Line   string `json:"line"`
	Target string `json:"target"`
	Now    int32  `json:"now"`
}

// ParseCircleLine recognizes exactly one non-whitespace display-name token.
// This intentionally does not guess the legacy prefix/occurrence grammar.
func ParseCircleLine(line string) (CircleCommand, bool) {
	if !utf8.ValidString(line) {
		return CircleCommand{}, false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\r' || r == '\n' || r == '\x00' || r == '\u2028' || r == '\u2029' {
			return CircleCommand{}, false
		}
	}
	fields := strings.Fields(line)
	if len(fields) != 2 || fields[0] != "교란" {
		return CircleCommand{}, false
	}
	if fields[1] == "" || strings.TrimSpace(fields[1]) != fields[1] {
		return CircleCommand{}, false
	}
	return CircleCommand{Target: fields[1]}, true
}

func IsCircleLine(line string) bool {
	_, ok := ParseCircleLine(line)
	return ok
}

// ExecuteCircleLine is the durable terminal boundary for the bounded circle
// slice. Retries with the same command ID return the stored receipt, so target
// selection and random draws are not repeated after commit.
func (o *Ownership) ExecuteCircleLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32, roll func(int, int) int) (storage.WorldReceipt, error) {
	return o.ExecuteCircleLineWithOptions(ctx, store, worldID, commandID, lease, line, CircleOptions{Now: now, Roll: roll})
}

func (o *Ownership) ExecuteCircleLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options CircleOptions) (storage.WorldReceipt, error) {
	command, ok := ParseCircleLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedCircleLine
	}
	payload, err := json.Marshal(circleLineRequest{Kind: "circle", Line: line, Target: command.Target, Now: options.Now})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		s, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := s.PlanCircle(actorID, command.Target, options.Now, options.Roll)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := s.ApplyCircle(proposal)
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
