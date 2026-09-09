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

// The legacy registration is global.c cmdno 154, alias 상태.  cmdno 135 is
// the unrelated administrator *group handler; it is intentionally not
// treated as an enemy-status alias here.
var ErrUnsupportedEnemyStatusLine = errors.New("line is not an implemented enemy status command")

// EnemyStatusCommand carries only the exact display-name token supplied by a
// client. The world reducer resolves the authoritative NPC ID from the actor's
// current room and never trusts a client-provided identity.
type EnemyStatusCommand struct {
	Target string
}

type enemyStatusLineRequest struct {
	Kind   string `json:"kind"`
	Line   string `json:"line"`
	Target string `json:"target"`
}

// ParseEnemyStatusLine admits the bounded command8.c:enemy_status form:
// `상태 <exact NPC name>`. Bare 상태, prefixes/occurrences, player targets,
// and extra tokens remain outside this slice. Legacy quoted token handling is
// retained, but a quoted multi-word name is still rejected because C's
// command argument is one creature token and the admitted identity is exact.
func ParseEnemyStatusLine(line string) (EnemyStatusCommand, bool) {
	if !utf8.ValidString(line) {
		return EnemyStatusCommand{}, false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return EnemyStatusCommand{}, false
		}
	}
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || len(tokens) != 2 || tokens[0] != "상태" || tokens[1] == "" || strings.TrimSpace(tokens[1]) != tokens[1] || strings.IndexFunc(tokens[1], unicode.IsSpace) >= 0 {
		return EnemyStatusCommand{}, false
	}
	return EnemyStatusCommand{Target: tokens[1]}, true
}

// IsEnemyStatusLine is a descriptive helper for a later central dispatcher;
// this independently owned lane does not modify the shared parser table.
func IsEnemyStatusLine(line string) bool {
	_, ok := ParseEnemyStatusLine(line)
	return ok
}

// ExecuteEnemyStatusLine connects the read-only enemy status projection to
// the authenticated session and durable receipt boundary. The reducer keeps
// the original snapshot byte-for-byte; replay therefore returns the exact
// first response without re-resolving a newer NPC state. Unsupported or
// unresolved canonical targets fail before a receipt rather than guessing
// from legacy room.Monsters or display-only fields.
func (o *Ownership) ExecuteEnemyStatusLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	command, ok := ParseEnemyStatusLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedEnemyStatusLine
	}
	request, err := json.Marshal(enemyStatusLineRequest{
		Kind: "enemy_status", Line: line, Target: command.Target,
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, request, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		text, err := state.EnemyStatus(actorID, command.Target)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(text)
		return raw, response, err
	})
}

// ExecuteStatusOfEnemyLine is a descriptive compatibility alias for command
// adapters that name the operation after its rendered status.
func (o *Ownership) ExecuteStatusOfEnemyLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	return o.ExecuteEnemyStatusLine(ctx, store, worldID, commandID, lease, line)
}
