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

var ErrUnsupportedDirectMessageLine = errors.New("line is not an implemented direct-message command")

// DirectMessageCommand is the parsed command4.c:sendman payload. Target is a
// display-name selector, never a client-owned player identity.
type DirectMessageCommand struct {
	Target string
	Text   string
}

type directMessageLineRequest struct {
	Kind   string `json:"kind"`
	Line   string `json:"line"`
	Target string `json:"target"`
	Text   string `json:"text"`
}

// ParseDirectMessageLine recognizes the exact global.c aliases 얘기 and
// 이야기. The first token after the alias is the online-player selector; all
// remaining text is retained as one message payload. Prefix target resolution
// and visibility authority belong to the world reducer, not this parser.
func ParseDirectMessageLine(line string) (DirectMessageCommand, bool) {
	if !utf8.ValidString(line) {
		return DirectMessageCommand{}, false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return DirectMessageCommand{}, false
		}
	}
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return DirectMessageCommand{}, false
	}
	aliasEnd := strings.IndexFunc(trimmed, unicode.IsSpace)
	alias := trimmed
	remainder := ""
	if aliasEnd >= 0 {
		alias = trimmed[:aliasEnd]
		remainder = strings.TrimLeftFunc(trimmed[aliasEnd:], unicode.IsSpace)
	}
	if alias != "얘기" && alias != "이야기" {
		return DirectMessageCommand{}, false
	}
	if remainder == "" {
		return DirectMessageCommand{}, true
	}
	targetEnd := strings.IndexFunc(remainder, unicode.IsSpace)
	if targetEnd < 0 {
		return DirectMessageCommand{Target: remainder}, true
	}
	target := remainder[:targetEnd]
	text := strings.TrimSpace(remainder[targetEnd:])
	if target == "" {
		return DirectMessageCommand{}, false
	}
	if err := world.ValidateDirectMessageText(text); err != nil {
		return DirectMessageCommand{}, false
	}
	return DirectMessageCommand{Target: target, Text: text}, true
}

func IsDirectMessageLine(line string) bool {
	_, ok := ParseDirectMessageLine(line)
	return ok
}

// ExecuteDirectMessageLine connects the pure proposal/apply boundary to the
// durable game receipt. The state candidate is intentionally unchanged: the
// current model has no persisted last-message metadata, so only the actor
// response and deterministic recipient event live in the receipt.
func (o *Ownership) ExecuteDirectMessageLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	command, ok := ParseDirectMessageLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedDirectMessageLine
	}
	payload, err := json.Marshal(directMessageLineRequest{Kind: "direct-message", Line: line, Target: command.Target, Text: command.Text})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		s, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := s.PlanDirectMessage(actorID, command.Target, command.Text)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := s.ApplyDirectMessage(proposal)
		if err != nil {
			return nil, nil, err
		}
		state, err := json.Marshal(next)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		return state, response, err
	})
}
