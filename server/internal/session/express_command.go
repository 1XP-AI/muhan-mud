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

var ErrUnsupportedExpressLine = errors.New("line is not an implemented 표현 command")

// ExpressCommand is the parsed command11.c:emote payload.  Text is data, not
// a formatting template or an identity/authorization source.
type ExpressCommand struct {
	Text string
}

// ParseExpressLine recognizes only the exact global.c alias 표현.  The
// remainder is kept as one free-form payload (rather than tokenized) and is
// bounded/validated before it can become a receipt request.  Leading/trailing
// command whitespace is insignificant; whitespace inside the payload remains
// meaningful.
func ParseExpressLine(line string) (ExpressCommand, bool) {
	if !isValidUTF8Line(line) {
		return ExpressCommand{}, false
	}
	// A transport normally removes the submit delimiter, but this command
	// boundary is also called directly by tests and recovery paths.  Do not
	// let TrimSpace silently turn an injected line break into a valid command.
	for _, r := range line {
		switch r {
		case '\r', '\n', '\x00', '\x1b', '\u2028', '\u2029':
			return ExpressCommand{}, false
		}
	}
	trimmed := strings.TrimSpace(line)
	if trimmed == world.ExpressAlias {
		return ExpressCommand{}, true
	}
	if !strings.HasPrefix(trimmed, world.ExpressAlias) {
		return ExpressCommand{}, false
	}
	remainder := trimmed[len(world.ExpressAlias):]
	if remainder == "" {
		return ExpressCommand{}, true
	}
	first, _ := utf8.DecodeRuneInString(remainder)
	if !unicode.IsSpace(first) {
		return ExpressCommand{}, false
	}
	text := strings.TrimSpace(remainder)
	if err := world.ValidateExpressText(text); err != nil {
		return ExpressCommand{}, false
	}
	return ExpressCommand{Text: text}, true
}

// ExpressLineText is the text-only counterpart used by command dispatchers.
// It intentionally returns false for malformed UTF-8, control injection, and
// oversized payloads rather than allowing encoding/json to normalize them.
func ExpressLineText(line string) (string, bool) {
	command, ok := ParseExpressLine(line)
	if !ok {
		return "", false
	}
	return command.Text, true
}

// ExecuteExpressLine owns the durable receipt boundary for 표현.  The room
// event is not placed in the response/state candidate.  A caller may derive
// world.RoomExpressEvent from the committed snapshot only when the returned
// receipt is not Replayed.
func (o *Ownership) ExecuteExpressLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	command, ok := ParseExpressLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedExpressLine
	}
	payload, err := json.Marshal(struct {
		Line string
		Text string
	}{Line: line, Text: command.Text})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		s, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := s.PlanExpress(actorID, command.Text)
		if err != nil {
			return nil, nil, err
		}
		state, err := json.Marshal(next)
		if err != nil {
			return nil, nil, err
		}
		// Only the actor response is durable.  In particular, do not marshal an
		// ExpressEvent or arbitrary payload into the receipt.
		response, err := json.Marshal(result.Response)
		return state, response, err
	})
}

func isValidUTF8Line(line string) bool {
	// Avoid importing a second parser/normalizer here; world.ValidateExpressText
	// is applied to the payload after exact alias extraction.  The line itself
	// still needs an explicit UTF-8 check because invalid bytes before the
	// payload would otherwise be silently replaced by encoding/json.
	return utf8.ValidString(line)
}
