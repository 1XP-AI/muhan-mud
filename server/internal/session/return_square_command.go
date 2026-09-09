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

// ErrUnsupportedReturnSquareLine is returned before a receipt exists when a
// line is not one of command1.c's exact aliases.
var ErrUnsupportedReturnSquareLine = errors.New("line is not an implemented return-square command")

// ReturnSquareCommand is the parser-facing, client-owned alias. It has no
// destination or other state claims; the world snapshot selects the room.
type ReturnSquareCommand struct {
	Alias string
}

type returnSquareLineRequest struct {
	Kind  string `json:"kind"`
	Line  string `json:"line"`
	Alias string `json:"alias"`
}

// ParseReturnSquareLine admits only the exact global.c aliases 귀환 and 귀,
// with optional outer terminal whitespace. Arguments and suffixes are not
// part of this bounded command slice.
func ParseReturnSquareLine(line string) (ReturnSquareCommand, bool) {
	if !utf8.ValidString(line) {
		return ReturnSquareCommand{}, false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\r' || r == '\n' || r == '\x00' {
			return ReturnSquareCommand{}, false
		}
	}
	trimmed := strings.TrimSpace(line)
	switch trimmed {
	case "귀환", "귀":
		return ReturnSquareCommand{Alias: trimmed}, true
	default:
		return ReturnSquareCommand{}, false
	}
}

// IsReturnSquareLine reports whether line is an exact return alias.
func IsReturnSquareLine(line string) bool {
	_, ok := ParseReturnSquareLine(line)
	return ok
}

// ExecuteReturnSquareLine runs the stateful return through authenticated
// ownership and the durable ExecuteGame receipt boundary. Denials commit the
// original state bytes as a normal no-op receipt; successful moves marshal the
// atomic candidate. The typed world.ReturnSquareResult is the receipt body.
func (o *Ownership) ExecuteReturnSquareLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	command, ok := ParseReturnSquareLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedReturnSquareLine
	}
	payload, err := json.Marshal(returnSquareLineRequest{Kind: "return-square", Line: line, Alias: command.Alias})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.PlanReturnSquare(actorID)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		if err != nil {
			return nil, nil, err
		}
		if !result.Moved {
			return raw, response, nil
		}
		candidate, err := json.Marshal(next)
		if err != nil {
			return nil, nil, err
		}
		return candidate, response, nil
	})
}
