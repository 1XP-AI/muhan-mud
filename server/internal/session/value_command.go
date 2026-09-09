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

// ErrUnsupportedValueLine is returned before a receipt exists when the line
// is outside the exact value/price alias and bounded name-occurrence form.
var ErrUnsupportedValueLine = errors.New("line is not an implemented value command")

// ValueCommand is the parser-facing, client-owned portion of a value request.
// Item identity remains server-owned and is resolved from the committed world
// snapshot by ExecuteValueLine.
type ValueCommand struct {
	Alias      string
	Name       string
	Occurrence int
}

type valueLineRequest struct {
	Kind       string `json:"kind"`
	Line       string `json:"line"`
	Alias      string `json:"alias"`
	Name       string `json:"name"`
	Occurrence int    `json:"occurrence"`
}

// ParseValueLine admits only global.c's exact Korean aliases. The occurrence
// is one-based; omitting it preserves the legacy parser's default of one, while
// an explicit occurrence must be a positive decimal integer.
func ParseValueLine(line string) (ValueCommand, bool) {
	if !utf8.ValidString(line) {
		return ValueCommand{}, false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\r' || r == '\n' || r == '\x00' {
			return ValueCommand{}, false
		}
	}
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || len(tokens) < 2 || len(tokens) > 3 {
		return ValueCommand{}, false
	}
	if tokens[0] != "가치" && tokens[0] != "가격" {
		return ValueCommand{}, false
	}
	if tokens[1] == "" || strings.TrimSpace(tokens[1]) != tokens[1] {
		return ValueCommand{}, false
	}
	command := ValueCommand{Alias: tokens[0], Name: tokens[1], Occurrence: 1}
	if len(tokens) == 3 {
		occurrence, err := strconv.ParseUint(tokens[2], 10, 31)
		if err != nil || occurrence < 1 {
			return ValueCommand{}, false
		}
		command.Occurrence = int(occurrence)
	}
	return command, true
}

// parseValueLine keeps the package-local naming convention used by older
// command reducers while ParseValueLine remains available to transport code.
func parseValueLine(line string) (ValueCommand, bool) {
	return ParseValueLine(line)
}

// IsValueLine is a descriptive parser helper for transport dispatchers. The
// central command parser intentionally remains owned by the integration lane.
func IsValueLine(line string) bool {
	_, ok := ParseValueLine(line)
	return ok
}

// ExecuteValueLine runs the read-only value quote through the authenticated
// session ownership and durable ExecuteGame boundary. The candidate snapshot
// is returned unchanged; only the typed world.ValueResult is persisted as the
// response so retries never re-select a different duplicate item.
func (o *Ownership) ExecuteValueLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	command, ok := ParseValueLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedValueLine
	}
	request, err := json.Marshal(valueLineRequest{
		Kind: "value", Line: line, Alias: command.Alias, Name: command.Name, Occurrence: command.Occurrence,
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, request, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		result, err := state.QuoteValueByName(actorID, command.Name, command.Occurrence)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		return raw, response, err
	})
}
