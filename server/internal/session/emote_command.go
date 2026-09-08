package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

// ErrUnsupportedEmoteLine is returned before a receipt is created when the
// line is outside the exact action alias/argument boundary admitted here.
// Prefix matching, occurrence selectors, NPC targets, and extra legacy
// message arguments remain intentionally unimplemented.
var ErrUnsupportedEmoteLine = errors.New("line is not an implemented emote command")

// EmoteCommand is the parsed, bounded form of an action line. Target is a
// display name, never an authoritative identity; PlanEmote resolves it from
// the committed snapshot used by the reducer.
type EmoteCommand struct {
	Alias  string
	Target string
}

// ParseEmoteLine recognizes one exact action alias with at most one exact
// target token. It reuses the existing legacy tokenizer so quoted tokens are
// handled consistently, but does not admit a quoted target containing spaces
// unless the canonical player name is itself exactly that token.
func ParseEmoteLine(line string) (EmoteCommand, bool) {
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || len(tokens) == 0 || len(tokens) > 2 || !world.IsEmoteAlias(tokens[0]) {
		return EmoteCommand{}, false
	}
	command := EmoteCommand{Alias: tokens[0]}
	if len(tokens) == 2 {
		command.Target = tokens[1]
	}
	return command, true
}

// EmoteLineText is a compatibility-shaped alias for command-boundary code
// that names all line recognizers *LineText. It returns the parsed action,
// not a client-supplied player identity.
func EmoteLineText(line string) (EmoteCommand, bool) {
	return ParseEmoteLine(line)
}

// ExecuteEmoteLine connects the source-backed action reducer to the durable
// command/receipt boundary. The actor response is persisted in the receipt;
// a caller may derive world.RoomEmoteEvent from the committed state after a
// first (non-replayed) receipt and publish its TargetText/Text projections.
func (o *Ownership) ExecuteEmoteLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	command, ok := ParseEmoteLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedEmoteLine
	}
	payload, err := json.Marshal(struct {
		Line   string
		Alias  string
		Target string
	}{Line: line, Alias: command.Alias, Target: command.Target})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		s, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := s.PlanEmote(actorID, command.Alias, command.Target)
		if err != nil {
			return nil, nil, err
		}
		state, err := json.Marshal(next)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result.Response)
		return state, response, err
	})
}
