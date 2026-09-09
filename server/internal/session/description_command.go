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

var ErrUnsupportedDescriptionLine = errors.New("line is not an implemented description command")

// DescriptionCommand contains only the free-form text before command12.c's
// final 묘사 token. A bare 묘사 command has an empty Description and clears the
// persisted player field.
type DescriptionCommand struct {
	Description string
	Text        string // descriptive alias for free-form command callers
	Clear       bool
}

type descriptionLineRequest struct {
	Kind        string `json:"kind"`
	Line        string `json:"line"`
	Description string `json:"description"`
	Clear       bool   `json:"clear"`
}

// ParseDescriptionLine admits only the source suffix form:
//
//	<description> 묘사
//
// The bare final token is the C empty-description branch. The 31-byte limit
// deliberately covers the original fullstr, including its final command
// token, because command12.c checks strlen(fullstr) before removing it.
func ParseDescriptionLine(line string) (DescriptionCommand, bool) {
	if !utf8.ValidString(line) || len(line) > world.MaxDescriptionLineBytes {
		return DescriptionCommand{}, false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return DescriptionCommand{}, false
		}
	}
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return DescriptionCommand{}, false
	}
	if trimmed == "묘사" {
		return DescriptionCommand{Clear: true}, true
	}
	const suffix = " 묘사"
	if !strings.HasSuffix(trimmed, suffix) {
		return DescriptionCommand{}, false
	}
	text := strings.TrimSpace(strings.TrimSuffix(trimmed, suffix))
	if text == "" {
		return DescriptionCommand{Clear: true}, true
	}
	for _, r := range text {
		if unicode.IsSpace(r) && r != ' ' {
			return DescriptionCommand{}, false
		}
	}
	if err := world.ValidateDescriptionText(text); err != nil {
		return DescriptionCommand{}, false
	}
	return DescriptionCommand{Description: text, Text: text}, true
}

// IsDescriptionLine is a descriptive parser helper for a later transport
// dispatcher. It intentionally does not modify the central parser file.
func IsDescriptionLine(line string) bool {
	_, ok := ParseDescriptionLine(line)
	return ok
}

// ExecuteDescriptionLine connects the source-backed description proposal to
// the authenticated session and durable ExecuteGame receipt boundary. The
// reducer persists the candidate state and typed response; receipt replay
// therefore returns the original response without applying twice.
func (o *Ownership) ExecuteDescriptionLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	command, ok := ParseDescriptionLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedDescriptionLine
	}
	payload, err := json.Marshal(descriptionLineRequest{
		Kind: "description", Line: line, Description: command.Description, Clear: command.Clear,
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanDescription(actorID, command.Description)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyDescription(proposal)
		if err != nil {
			return nil, nil, err
		}
		nextRaw, err := json.Marshal(next)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		return nextRaw, response, err
	})
}
