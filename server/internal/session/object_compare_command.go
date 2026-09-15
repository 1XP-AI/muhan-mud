package session

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

// ErrUnsupportedCompareLine is returned before a receipt exists when the
// line is outside the exact 비교 alias and bounded item/occurrence form.
var ErrUnsupportedCompareLine = errors.New("line is not an implemented compare command")

// CompareCommand is the parser-facing portion of obj_compare.  Item IDs are
// intentionally absent: the world reducer resolves a display name against
// the actor's canonical inventory snapshot at execution time.
type CompareCommand struct {
	Alias      string
	Name       string
	Occurrence int
}

type compareLineRequest struct {
	Kind       string `json:"kind"`
	Line       string `json:"line"`
	Alias      string `json:"alias"`
	Name       string `json:"name"`
	Occurrence int    `json:"occurrence"`
}

// ParseCompareLine admits global.c's exact Korean alias.  A bare alias is
// retained because command12.c has a distinct no-argument response; a target
// name may be quoted to preserve spaces and an explicit occurrence is a
// positive one-based decimal integer.
func ParseCompareLine(line string) (CompareCommand, bool) {
	if !utf8.ValidString(line) {
		return CompareCommand{}, false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\r' || r == '\n' || r == '\x00' || r == '\x1b' || r == '\u2028' || r == '\u2029' {
			return CompareCommand{}, false
		}
	}
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || len(tokens) < 1 || len(tokens) > 3 || tokens[0] != "비교" {
		return CompareCommand{}, false
	}
	command := CompareCommand{Alias: "비교", Occurrence: 1}
	if len(tokens) == 1 {
		return command, true
	}
	if tokens[1] == "" || strings.TrimSpace(tokens[1]) != tokens[1] {
		return CompareCommand{}, false
	}
	command.Name = tokens[1]
	if len(tokens) == 3 {
		occurrence, err := strconv.ParseUint(tokens[2], 10, 31)
		if err != nil || occurrence < 1 {
			return CompareCommand{}, false
		}
		command.Occurrence = int(occurrence)
	}
	return command, true
}

// parseCompareLine retains the package-local naming convention used by other
// command reducers while ParseCompareLine remains available to transport
// dispatchers.
func parseCompareLine(line string) (CompareCommand, bool) {
	return ParseCompareLine(line)
}

// IsCompareLine reports whether a line belongs to the bounded 비교 command.
func IsCompareLine(line string) bool {
	_, ok := ParseCompareLine(line)
	return ok
}

// ExecuteCompareLine runs the source-backed read-only comparison through the
// authenticated session and durable ExecuteGame boundary.  The reducer
// returns the original state bytes unchanged, including for source-level
// missing/unsupported responses, so replay cannot mutate or re-select items.
func (o *Ownership) ExecuteCompareLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	command, ok := ParseCompareLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedCompareLine
	}
	request, err := json.Marshal(compareLineRequest{
		Kind: "compare", Line: line, Alias: command.Alias, Name: command.Name, Occurrence: command.Occurrence,
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, request, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		var result world.CompareResult
		if command.Name == "" {
			result, err = state.ComparePrompt(actorID)
		} else {
			result, err = state.CompareItemByName(actorID, command.Name, command.Occurrence)
		}
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		return raw, response, err
	})
}
