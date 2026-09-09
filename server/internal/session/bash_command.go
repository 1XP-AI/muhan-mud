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

// ErrUnsupportedBashLine is returned before a receipt exists for a line
// outside the bounded original `맹공 <대상>` form.
var ErrUnsupportedBashLine = errors.New("line is not an implemented bash command")

// BashCommand carries only the display-name target. The authenticated actor,
// canonical identity, permission and mutable state remain world-owned.
type BashCommand struct {
	Target string
}

// BashOptions binds host-owned clock and randomness to one command attempt.
// Retries with the same command ID replay the durable receipt and do not call
// Roll again.
type BashOptions struct {
	Now  int32
	Roll func(int, int) int
}

type bashLineRequest struct {
	Kind   string `json:"kind"`
	Line   string `json:"line"`
	Target string `json:"target"`
	Now    int32  `json:"now"`
}

func validBashLine(line string) bool {
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

// ParseBashLine admits exactly one non-whitespace display-name token. Legacy
// prefix/occurrence selectors and quoted multi-word names remain outside this
// bounded command contract.
func ParseBashLine(line string) (BashCommand, bool) {
	if !validBashLine(line) {
		return BashCommand{}, false
	}
	fields := strings.Fields(line)
	if len(fields) != 2 || fields[0] != "맹공" || fields[1] == "" || strings.TrimSpace(fields[1]) != fields[1] {
		return BashCommand{}, false
	}
	return BashCommand{Target: fields[1]}, true
}

func IsBashLine(line string) bool {
	_, ok := ParseBashLine(line)
	return ok
}

// Descriptive aliases keep the source command name and the Korean command
// adapter discoverable without introducing additional command spellings.
func ParseBashCommandLine(line string) (BashCommand, bool) { return ParseBashLine(line) }
func IsBashCommandLine(line string) bool                   { return IsBashLine(line) }

// ExecuteBashLine is the durable terminal boundary for the bounded bash
// slice. Target lookup, permission and RNG happen only inside the reducer
// after ExecuteGame has fenced command-ID replays.
func (o *Ownership) ExecuteBashLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32, roll func(int, int) int) (storage.WorldReceipt, error) {
	return o.ExecuteBashLineWithOptions(ctx, store, worldID, commandID, lease, line, BashOptions{Now: now, Roll: roll})
}

func (o *Ownership) ExecuteBashLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options BashOptions) (storage.WorldReceipt, error) {
	command, ok := ParseBashLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedBashLine
	}
	payload, err := json.Marshal(bashLineRequest{Kind: "bash", Line: line, Target: command.Target, Now: options.Now})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanBash(actorID, command.Target, options.Now, options.Roll)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyBash(proposal)
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

// Spelling aliases make migration callers that use “command” explicit while
// preserving one parser and one durable receipt implementation.
func (o *Ownership) ExecuteBashCommandLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32, roll func(int, int) int) (storage.WorldReceipt, error) {
	return o.ExecuteBashLine(ctx, store, worldID, commandID, lease, line, now, roll)
}

func (o *Ownership) ExecuteBashCommandLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options BashOptions) (storage.WorldReceipt, error) {
	return o.ExecuteBashLineWithOptions(ctx, store, worldID, commandID, lease, line, options)
}
