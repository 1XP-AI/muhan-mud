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

// ErrUnsupportedSaveLine is returned before a durable receipt exists when a
// line is not one of this bounded command's exact aliases.
var ErrUnsupportedSaveLine = errors.New("line is not an implemented save command")

// SaveResponse is the terminal response persisted in a save receipt.  It is a
// named string so the transport can decode the receipt through a public type
// while retaining the existing command convention of a JSON-encoded text
// response.
type SaveResponse string

// SaveResponseText is the smallest successful response admitted by this
// bounded slice.  It is the success text emitted by command8.c's savegame().
const SaveResponseText = "저장하였습니다.\n"

// SaveCommand is the parser-facing result for an exact save alias.
type SaveCommand struct {
	Alias string
}

type saveLineRequest struct {
	Kind  string `json:"kind"`
	Line  string `json:"line"`
	Alias string `json:"alias"`
}

// ParseSaveLine admits only the two aliases explicitly assigned to the Go
// bounded surface: the original Korean command and the ASCII compatibility
// spelling.  Outer terminal whitespace is harmless, but arguments, control
// bytes, invalid UTF-8, and the function name "savegame" are rejected.
//
// The legacy evidence is intentionally kept precise here.  src/global.c:323
// has one command-table entry, {"저장", 52, savegame}, and its trailing
// /* save */ is a comment, not a second cmdlist alias.  src/command1.c:1581-
// 1625 resolves that table entry and invokes its function.  The Go "save"
// spelling is therefore an explicit bounded compatibility alias; it is not
// claimed as an additional original cmdlist entry.
func ParseSaveLine(line string) (SaveCommand, bool) {
	if !utf8.ValidString(line) {
		return SaveCommand{}, false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\r' || r == '\n' || r == '\x00' || r == '\u2028' || r == '\u2029' {
			return SaveCommand{}, false
		}
	}
	switch alias := strings.TrimSpace(line); alias {
	case "저장", "save":
		return SaveCommand{Alias: alias}, true
	default:
		return SaveCommand{}, false
	}
}

// IsSaveLine reports whether line is an exact bounded save alias.
func IsSaveLine(line string) bool {
	_, ok := ParseSaveLine(line)
	return ok
}

// ExecuteSaveLine records an authenticated, no-state-change save receipt.
// Mutating commands already commit their canonical state in their own
// receipts; this command only acknowledges that durable boundary.  The
// reducer deliberately has no clock or random source, so a receipt replay
// cannot consume a new RNG value or derive a different response.
//
// command8.c:717-763 copied a creature, unreadied a duplicate, called
// save_ply, and printed "저장하였습니다.\n".  The Go runtime's canonical
// state is already persisted atomically by each mutation receipt, so doing a
// second player-file-style write here would violate the single durable
// authority.  Returning raw unchanged is the bounded Go equivalent.
func (o *Ownership) ExecuteSaveLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	command, ok := ParseSaveLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedSaveLine
	}
	request, err := json.Marshal(saveLineRequest{
		Kind: "save",
		// Keep the accepted terminal line in the receipt identity, matching
		// other session commands.  The parser still supplies the canonical
		// alias separately, so equivalent-looking whitespace is not silently
		// treated as the same client request under one command ID.
		Line:  line,
		Alias: command.Alias,
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, request, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		player, ok := state.Players[actorID]
		if !ok || !player.Online {
			return nil, nil, errors.New("online save actor required")
		}
		response, err := json.Marshal(SaveResponse(SaveResponseText))
		if err != nil {
			return nil, nil, err
		}
		return raw, response, nil
	})
}
