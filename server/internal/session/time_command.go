package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

// The source registration is global.c cmdno 49: {"시간", 49, prt_time}.
// This independent API does not alter the existing central parser/transport;
// callers can adopt it at an integration boundary when the clock contract is
// ready.
var ErrUnsupportedTimeLine = errors.New("line is not an implemented time command")

// TimeCommand is the exact bare 시간 command after parser normalization.
// prt_time has no target or additional argument.
type TimeCommand struct {
	Alias string
}

// TimeCommandOptions binds every clock input used by ExecuteTimeLine. Now is
// the legacy game clock (the world reducer derives now%24); WallClock is the
// server realtime observation and is rendered in fixed PST by world.ProjectTime.
type TimeCommandOptions struct {
	Now       int64
	WallClock time.Time
}

type timeLineRequest struct {
	Kind      string    `json:"kind"`
	Line      string    `json:"line"`
	Alias     string    `json:"alias"`
	Now       int64     `json:"now"`
	WallClock time.Time `json:"wall_clock"`
}

// ParseTimeLine admits only the exact global.c alias 시간 with optional outer
// whitespace. Arguments, control characters and line separators are rejected
// before a durable request is constructed.
func ParseTimeLine(line string) (TimeCommand, bool) {
	if !utf8.ValidString(line) {
		return TimeCommand{}, false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return TimeCommand{}, false
		}
	}
	if strings.TrimSpace(line) != world.TimeAlias {
		return TimeCommand{}, false
	}
	return TimeCommand{Alias: world.TimeAlias}, true
}

// IsTimeLine reports whether a line belongs to the bounded bare time command.
func IsTimeLine(line string) bool {
	_, ok := ParseTimeLine(line)
	return ok
}

// ExecuteTimeLine runs prt_time through the authenticated session and durable
// receipt boundary. It returns the original world snapshot bytes unchanged;
// a replay returns the immutable response without applying any state change.
// The caller owns both clock observations and must reuse them with the command
// ID if a commit outcome is uncertain.
func (o *Ownership) ExecuteTimeLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options TimeCommandOptions) (storage.WorldReceipt, error) {
	command, ok := ParseTimeLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedTimeLine
	}
	// Validate before entering ExecuteGame so invalid clock input can never
	// create a receipt. The callback repeats the pure render only on a first
	// execution; engine replay bypasses it.
	if _, err := world.ProjectTime(options.Now, options.WallClock); err != nil {
		return storage.WorldReceipt{}, err
	}
	request, err := json.Marshal(timeLineRequest{
		Kind: "time", Line: line, Alias: command.Alias,
		Now: options.Now, WallClock: options.WallClock.Round(0),
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, request, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		actor, ok := state.Players[actorID]
		if !ok || !actor.Online || actor.Body.Type != 0 {
			return nil, nil, errors.New("online time actor required")
		}
		responseText, err := world.RenderTime(options.Now, options.WallClock)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(responseText)
		return raw, response, err
	})
}
