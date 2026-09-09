package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

var (
	ErrUnsupportedReplyLine   = errors.New("line is not an implemented reply command")
	ErrReplyTargetUnavailable = errors.New("reply target is unavailable")
)

// ReplyCommand is the connection-local projection of command12.c:resend.
// The target is intentionally absent: it comes from the server-maintained
// last incoming direct-message identity, never from terminal input.
type ReplyCommand struct {
	Text string
}

type replyLineRequest struct {
	Kind       string `json:"kind"`
	Line       string `json:"line"`
	TargetID   string `json:"target_id"`
	TargetName string `json:"target_name"`
	Text       string `json:"text"`
}

func validReplyLine(line string) bool {
	if !utf8.ValidString(line) {
		return false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

// ParseReplyLine admits the source aliases `대답` and `/`. The rest of the
// line is one message payload, including repeated internal spaces. Empty text
// is accepted so the world reducer can return the source-compatible prompt
// without creating a second parser interpretation.
func ParseReplyLine(line string) (ReplyCommand, bool) {
	if !validReplyLine(line) {
		return ReplyCommand{}, false
	}
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return ReplyCommand{}, false
	}
	var remainder string
	switch {
	case trimmed == "대답":
		remainder = ""
	case strings.HasPrefix(trimmed, "대답") && len(trimmed) > len("대답"):
		if !unicode.IsSpace([]rune(trimmed)[len([]rune("대답"))]) {
			return ReplyCommand{}, false
		}
		remainder = strings.TrimSpace(trimmed[len("대답"):])
	case trimmed == "/":
		remainder = ""
	case strings.HasPrefix(trimmed, "/"):
		// A second slash is message text only after the command separator; a
		// bare `//` would otherwise make accidental paths look like replies.
		if strings.HasPrefix(trimmed, "//") {
			return ReplyCommand{}, false
		}
		remainder = strings.TrimSpace(strings.TrimPrefix(trimmed, "/"))
	default:
		return ReplyCommand{}, false
	}
	if err := world.ValidateDirectMessageText(remainder); err != nil {
		return ReplyCommand{}, false
	}
	return ReplyCommand{Text: remainder}, true
}

func IsReplyLine(line string) bool {
	_, ok := ParseReplyLine(line)
	return ok
}

// ExecuteReplyLine reuses the canonical direct-message reducer while binding
// the exact target identity captured on the receiving connection. A stale or
// logged-out target fails before a receipt can be committed.
func (o *Ownership) ExecuteReplyLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line, targetID, targetName string) (storage.WorldReceipt, error) {
	command, ok := ParseReplyLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedReplyLine
	}
	if targetID == "" || targetName == "" {
		return storage.WorldReceipt{}, ErrReplyTargetUnavailable
	}
	payload, err := json.Marshal(replyLineRequest{
		Kind: "reply", Line: line, TargetID: targetID, TargetName: targetName, Text: command.Text,
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		s, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := s.PlanDirectMessage(actorID, targetName, command.Text)
		if err != nil || proposal.TargetID != targetID || proposal.TargetName != targetName {
			return nil, nil, fmt.Errorf("%w: %v", ErrReplyTargetUnavailable, err)
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
