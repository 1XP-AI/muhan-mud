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

// ErrUnsupportedUseLine is returned before ExecuteGame when the terminal line
// is outside command9.c:use's deliberately one-item bounded surface.
var ErrUnsupportedUseLine = errors.New("line is not an implemented use command")

// UseCommand contains only a display selector. The world reducer resolves the
// canonical item ID from the authenticated actor and current room snapshot.
// Occurrence is retained for the world contract even though this first
// terminal slice admits exactly one selector token (and therefore occurrence
// one); future occurrence syntax must be added explicitly to this parser.
type UseCommand struct {
	Alias      string
	ItemName   string
	Occurrence int
}

type useLineRequest struct {
	Kind       string `json:"kind"`
	Line       string `json:"line"`
	Alias      string `json:"alias"`
	ItemName   string `json:"item_name"`
	Occurrence int    `json:"occurrence"`
	Now        int32  `json:"now"`
}

type UseOptions = world.UseOptions

func validUseLine(line string) bool {
	if !utf8.ValidString(line) {
		return false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\r' || r == '\n' || r == '\x00' || r == '\x1b' || r == '\u2028' || r == '\u2029' {
			return false
		}
		// tokenizeLegacy intentionally supports quoted tokens for other
		// commands. command9.c:use is bounded to one exact token, so quoted
		// forms are rejected before tokenization rather than silently widening
		// the selector to a multi-word name.
		if r == '\'' || r == '"' {
			return false
		}
	}
	return true
}

// ParseUseLine admits only the exact source alias `사용` followed by one
// unquoted item token. `모두`, occurrence suffixes, quoted names, prefixes,
// and extra tokens are intentionally fail-closed.
func ParseUseLine(line string) (UseCommand, bool) {
	if !validUseLine(line) {
		return UseCommand{}, false
	}
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || len(tokens) != 2 || tokens[0] != "사용" || tokens[1] == "" || tokens[1] == "모두" {
		return UseCommand{}, false
	}
	if strings.TrimSpace(tokens[1]) != tokens[1] {
		return UseCommand{}, false
	}
	return UseCommand{Alias: tokens[0], ItemName: tokens[1], Occurrence: 1}, true
}

func IsUseLine(line string) bool {
	_, ok := ParseUseLine(line)
	return ok
}

func (o *Ownership) ExecuteUseLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32, roll func(int, int) int) (storage.WorldReceipt, error) {
	return o.ExecuteUseLineWithOptions(ctx, store, worldID, commandID, lease, line, UseOptions{Now: now, Roll: roll})
}

// ExecuteUseLineWithOptions binds the use dispatcher to one durable receipt.
// PlanUse is the only place where a delegated potion may consume randomness;
// engine receipt replay returns the stored response and never re-enters this
// reducer callback.
func (o *Ownership) ExecuteUseLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options UseOptions) (storage.WorldReceipt, error) {
	command, ok := ParseUseLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedUseLine
	}
	payload, err := json.Marshal(useLineRequest{
		Kind: "use", Line: line, Alias: command.Alias, ItemName: command.ItemName,
		Occurrence: command.Occurrence, Now: options.Now,
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanUse(actorID, command.ItemName, command.Occurrence, options)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyUse(proposal)
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
