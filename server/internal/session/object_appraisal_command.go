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

// ErrUnsupportedObjectAppraisalLine is returned before a receipt exists when
// a line is outside the exact `감정 <이름> [occurrence]` boundary.
var ErrUnsupportedObjectAppraisalLine = errors.New("line is not an implemented object appraisal command")

// ObjectAppraisalCommand is the parser-facing, client-owned portion of an
// object appraisal request. Item IDs are intentionally absent; world state
// resolves the authoritative direct inventory root during ExecuteGame.
type ObjectAppraisalCommand struct {
	Alias      string
	Name       string
	Occurrence int
}

// AppraisalCommand is a short compatibility alias for callers that use the
// command name without the object qualifier.
type AppraisalCommand = ObjectAppraisalCommand

type objectAppraisalLineRequest struct {
	Kind       string `json:"kind"`
	Line       string `json:"line"`
	Alias      string `json:"alias"`
	Name       string `json:"name"`
	Occurrence int    `json:"occurrence"`
}

// ParseObjectAppraisalLine admits only global.c's exact `감정` alias, one
// exact display name, and an optional positive one-based occurrence. Legacy
// quote handling is retained for names containing spaces; prefix/key aliases,
// equipped roots, and other occurrence syntaxes remain outside this slice.
func ParseObjectAppraisalLine(line string) (ObjectAppraisalCommand, bool) {
	if !utf8.ValidString(line) {
		return ObjectAppraisalCommand{}, false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\r' || r == '\n' || r == '\x00' || r == '\u2028' || r == '\u2029' {
			return ObjectAppraisalCommand{}, false
		}
	}
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || len(tokens) < 2 || len(tokens) > 3 || tokens[0] != "감정" {
		return ObjectAppraisalCommand{}, false
	}
	if tokens[1] == "" || strings.TrimSpace(tokens[1]) != tokens[1] {
		return ObjectAppraisalCommand{}, false
	}
	command := ObjectAppraisalCommand{Alias: "감정", Name: tokens[1], Occurrence: 1}
	if len(tokens) == 3 {
		occurrence, err := strconv.ParseUint(tokens[2], 10, 31)
		if err != nil || occurrence < 1 {
			return ObjectAppraisalCommand{}, false
		}
		command.Occurrence = int(occurrence)
	}
	return command, true
}

// ParseAppraisalLine is the concise parser spelling used by command adapters.
func ParseAppraisalLine(line string) (ObjectAppraisalCommand, bool) {
	return ParseObjectAppraisalLine(line)
}

// IsObjectAppraisalLine is a descriptive parser helper for transport
// dispatchers. The central parser remains intentionally unchanged in this
// independently owned command lane.
func IsObjectAppraisalLine(line string) bool {
	_, ok := ParseObjectAppraisalLine(line)
	return ok
}

// IsAppraisalLine is the concise helper spelling used by command adapters.
func IsAppraisalLine(line string) bool {
	return IsObjectAppraisalLine(line)
}

// ExecuteObjectAppraisalLine runs the read-only appraisal through authenticated
// session ownership and the durable ExecuteGame receipt/replay boundary. The
// reducer returns the original snapshot byte-for-byte; only the typed result
// is stored in the receipt response.
func (o *Ownership) ExecuteObjectAppraisalLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	command, ok := ParseObjectAppraisalLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedObjectAppraisalLine
	}
	request, err := json.Marshal(objectAppraisalLineRequest{
		Kind: "object_appraisal", Line: line, Alias: command.Alias, Name: command.Name, Occurrence: command.Occurrence,
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, request, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		result, err := state.AppraiseObjectByName(actorID, command.Name, command.Occurrence)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		return raw, response, err
	})
}

// ExecuteAppraisalLine is the concise execution spelling used by command
// adapters.
func (o *Ownership) ExecuteAppraisalLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	return o.ExecuteObjectAppraisalLine(ctx, store, worldID, commandID, lease, line)
}
