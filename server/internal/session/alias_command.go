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

var ErrUnsupportedAliasLine = errors.New("line is not an implemented alias command")

// AliasCommand is the bounded parser result for alias.c:ply_aliases. The
// command is accepted in the Go terminal's prefix form and in C's suffix form
// (`<alias> <process> 줄임말`) so dispatch integration can choose either
// tokenizer convention without widening the world reducer.
type AliasCommand struct {
	Action  world.AliasAction
	Alias   string
	Process string
}

type aliasLineRequest struct {
	Kind    string            `json:"kind"`
	Line    string            `json:"line"`
	Action  world.AliasAction `json:"action"`
	Alias   string            `json:"alias,omitempty"`
	Process string            `json:"process,omitempty"`
}

const aliasCommandWord = "줄임말"

// ParseAliasLine admits:
//
//	줄임말
//	줄임말 <alias> <process>
//	줄임말 [숫자]<alias>
//
// The source parser places the command token last, so the equivalent suffix
// forms are admitted as well. Alias expansion itself is not performed here;
// process validation rejects the legacy $N/$* substitution syntax.
func ParseAliasLine(line string) (AliasCommand, bool) {
	if !utf8.ValidString(line) {
		return AliasCommand{}, false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' || (unicode.IsSpace(r) && r != ' ') {
			return AliasCommand{}, false
		}
	}
	trimmed := strings.TrimSpace(line)
	if trimmed == aliasCommandWord {
		return AliasCommand{Action: world.AliasView}, true
	}
	if strings.HasPrefix(trimmed, aliasCommandWord+" ") {
		return parseAliasBody(trimmed[len(aliasCommandWord)+1:])
	}
	const suffix = " " + aliasCommandWord
	if strings.HasSuffix(trimmed, suffix) {
		return parseAliasBody(strings.TrimSuffix(trimmed, suffix))
	}
	return AliasCommand{}, false
}

func parseAliasBody(body string) (AliasCommand, bool) {
	if body == "" {
		return AliasCommand{}, false
	}
	space := strings.IndexByte(body, ' ')
	if space < 0 {
		alias, _ := stripAliasNumberPrefix(body)
		if err := world.ValidatePlayerAliasName(alias); err != nil {
			return AliasCommand{}, false
		}
		return AliasCommand{Action: world.AliasDelete, Alias: alias}, true
	}
	alias, _ := stripAliasNumberPrefix(body[:space])
	process := body[space+1:]
	if err := world.ValidatePlayerAliasName(alias); err != nil {
		return AliasCommand{}, false
	}
	if err := world.ValidatePlayerAliasProcess(process); err != nil {
		return AliasCommand{}, false
	}
	return AliasCommand{Action: world.AliasAdd, Alias: alias, Process: process}, true
}

func stripAliasNumberPrefix(value string) (string, bool) {
	const prefix = "숫자"
	if strings.HasPrefix(value, prefix) {
		return strings.TrimPrefix(value, prefix), true
	}
	return value, false
}

// IsAliasLine is a descriptive parser helper for the later central
// dispatcher. This lane intentionally leaves command_parser.go untouched.
func IsAliasLine(line string) bool {
	_, ok := ParseAliasLine(line)
	return ok
}

// ParseAliasesLine is the plural compatibility spelling used by source
// adapters that name the C handler ply_aliases.
func ParseAliasesLine(line string) (AliasCommand, bool) { return ParseAliasLine(line) }

// IsAliasesLine is a plural compatibility spelling for callers naming the C
// handler rather than the individual command line.
func IsAliasesLine(line string) bool { return IsAliasLine(line) }

// ExecuteAliasLine binds the pure alias proposal/apply reducer to the
// authenticated durable ExecuteGame receipt. A replay returns the original
// typed response and never re-applies the ordered slice mutation.
func (o *Ownership) ExecuteAliasLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	command, ok := ParseAliasLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedAliasLine
	}
	request, err := json.Marshal(aliasLineRequest{
		Kind: "alias", Line: line, Action: command.Action, Alias: command.Alias, Process: command.Process,
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, request, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanAlias(actorID, command.Action, command.Alias, command.Process)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyAlias(proposal)
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

// ExecuteAliasesLine is the plural compatibility spelling used by a few
// source-oriented adapters.
func (o *Ownership) ExecuteAliasesLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	return o.ExecuteAliasLine(ctx, store, worldID, commandID, lease, line)
}
