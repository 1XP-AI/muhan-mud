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

// ErrUnsupportedBackstabLine is returned before a receipt exists when a line
// is outside the bounded original `기습 <대상>` form.
var ErrUnsupportedBackstabLine = errors.New("line is not an implemented backstab command")

// BackstabCommand contains only a display-name target. The world reducer
// resolves that name against canonical room identities during the transaction.
type BackstabCommand struct {
	Target string
}

// BackstabOptions binds host-owned time and randomness to one command attempt.
// Neither value is accepted from the terminal line or persisted as authority.
type BackstabOptions struct {
	Now  int32
	Roll func(int, int) int
}

type backstabLineRequest struct {
	Kind   string `json:"kind"`
	Line   string `json:"line"`
	Target string `json:"target"`
	Now    int32  `json:"now"`
}

// ParseBackstabLine recognizes exactly one non-whitespace display-name token.
// Prefix/occurrence selectors and quoted multi-word names stay outside this
// bounded slice until their canonical identity contract is admitted.
func ParseBackstabLine(line string) (BackstabCommand, bool) {
	if !utf8.ValidString(line) {
		return BackstabCommand{}, false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\r' || r == '\n' || r == '\x00' || r == '\u2028' || r == '\u2029' {
			return BackstabCommand{}, false
		}
	}
	fields := strings.Fields(line)
	if len(fields) != 2 || fields[0] != "기습" {
		return BackstabCommand{}, false
	}
	if fields[1] == "" || strings.TrimSpace(fields[1]) != fields[1] {
		return BackstabCommand{}, false
	}
	return BackstabCommand{Target: fields[1]}, true
}

func IsBackstabLine(line string) bool {
	_, ok := ParseBackstabLine(line)
	return ok
}

// ExecuteBackstabLine is the durable terminal boundary for the bounded
// backstab slice. Retries with the same command ID replay the stored receipt,
// so target lookup and random draws are never repeated after commit.
func (o *Ownership) ExecuteBackstabLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32, roll func(int, int) int) (storage.WorldReceipt, error) {
	return o.ExecuteBackstabLineWithOptions(ctx, store, worldID, commandID, lease, line, BackstabOptions{Now: now, Roll: roll})
}

func (o *Ownership) ExecuteBackstabLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options BackstabOptions) (storage.WorldReceipt, error) {
	command, ok := ParseBackstabLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedBackstabLine
	}
	payload, err := json.Marshal(backstabLineRequest{Kind: "backstab", Line: line, Target: command.Target, Now: options.Now})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		s, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := s.PlanBackstab(actorID, command.Target, options.Now, options.Roll)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := s.ApplyBackstab(proposal)
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
