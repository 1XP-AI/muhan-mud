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

// ErrUnsupportedKickLine is returned before a durable receipt for lines
// outside command8.c's bounded `차기 <대상>` form.
var ErrUnsupportedKickLine = errors.New("line is not an implemented kick command")

// KickCommand carries only the display-name selector. The authenticated
// actor, canonical target identity and all mutable state remain world-owned.
type KickCommand struct {
	Target string
}

// KickOptions supplies server-owned clock and randomness. Neither value is
// accepted from the terminal line; ExecuteGame's receipt fence ensures a
// replay never calls Roll again.
type KickOptions struct {
	Now  int32
	Roll func(int, int) int
}

type kickLineRequest struct {
	Kind   string `json:"kind"`
	Line   string `json:"line"`
	Target string `json:"target"`
	Now    int32  `json:"now"`
}

func validKickLine(line string) bool {
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

// ParseKickLine admits one non-whitespace target token. Prefix, occurrence,
// key and quoted multi-word selectors remain outside this bounded identity
// contract; the canonical room order is resolved by world.PlanKick.
func ParseKickLine(line string) (KickCommand, bool) {
	if !validKickLine(line) {
		return KickCommand{}, false
	}
	fields := strings.Fields(line)
	if len(fields) != 2 || fields[0] != "차기" || fields[1] == "" || strings.TrimSpace(fields[1]) != fields[1] {
		return KickCommand{}, false
	}
	for _, r := range fields[1] {
		if unicode.IsSpace(r) {
			return KickCommand{}, false
		}
	}
	return KickCommand{Target: fields[1]}, true
}

func IsKickLine(line string) bool {
	_, ok := ParseKickLine(line)
	return ok
}

func ParseKickCommandLine(line string) (KickCommand, bool) { return ParseKickLine(line) }
func IsKickCommandLine(line string) bool                   { return IsKickLine(line) }

// ExecuteKickLine is the durable terminal boundary for the bounded kick
// slice. Planning and application happen only after the authenticated session
// and command-ID replay fence have been established.
func (o *Ownership) ExecuteKickLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32, roll func(int, int) int) (storage.WorldReceipt, error) {
	return o.ExecuteKickLineWithOptions(ctx, store, worldID, commandID, lease, line, KickOptions{Now: now, Roll: roll})
}

func (o *Ownership) ExecuteKickLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options KickOptions) (storage.WorldReceipt, error) {
	command, ok := ParseKickLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedKickLine
	}
	payload, err := json.Marshal(kickLineRequest{Kind: "kick", Line: line, Target: command.Target, Now: options.Now})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanKick(actorID, command.Target, options.Now, options.Roll)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyKick(proposal)
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

func (o *Ownership) ExecuteKickCommandLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32, roll func(int, int) int) (storage.WorldReceipt, error) {
	return o.ExecuteKickLine(ctx, store, worldID, commandID, lease, line, now, roll)
}

func (o *Ownership) ExecuteKickCommandLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options KickOptions) (storage.WorldReceipt, error) {
	return o.ExecuteKickLineWithOptions(ctx, store, worldID, commandID, lease, line, options)
}
