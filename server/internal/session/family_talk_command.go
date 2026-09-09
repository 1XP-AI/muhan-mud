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

var ErrUnsupportedFamilyTalkLine = errors.New("line is not an implemented family-talk command")

// FamilyTalkCommand keeps the user-facing alias separate from the message.
// Recipient IDs are always resolved from the committed world snapshot.
type FamilyTalkCommand struct {
	Alias   string
	Message string
}

type familyTalkLineRequest struct {
	Kind    string `json:"kind"`
	Line    string `json:"line"`
	Alias   string `json:"alias"`
	Message string `json:"message"`
}

var familyTalkAliases = [...]string{"패거리말", "]"}

// ParseFamilyTalkLine accepts the original prefix aliases. A bare alias is
// admitted so the world reducer can return C's deterministic empty-message
// prompt; suffix forms are not inferred because command11.c reads the full
// command buffer after its alias dispatch.
func ParseFamilyTalkLine(line string) (FamilyTalkCommand, bool) {
	if !utf8.ValidString(line) {
		return FamilyTalkCommand{}, false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\r' || r == '\n' || r == '\x00' {
			return FamilyTalkCommand{}, false
		}
	}
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return FamilyTalkCommand{}, false
	}
	for _, alias := range familyTalkAliases {
		if trimmed == alias {
			return FamilyTalkCommand{Alias: alias}, true
		}
		if !strings.HasPrefix(trimmed, alias) || len(trimmed) == len(alias) {
			continue
		}
		remainder := trimmed[len(alias):]
		if remainder == "" {
			continue
		}
		first, size := utf8.DecodeRuneInString(remainder)
		if first == utf8.RuneError || !unicode.IsSpace(first) {
			continue
		}
		return FamilyTalkCommand{Alias: alias, Message: strings.TrimSpace(remainder[size:])}, true
	}
	return FamilyTalkCommand{}, false
}

func IsFamilyTalkLine(line string) bool {
	_, ok := ParseFamilyTalkLine(line)
	return ok
}

// ExecuteFamilyTalkLine persists the deterministic recipient projection as a
// no-state-change receipt. The transport publishes the event list only after
// the first commit, so retries cannot duplicate family chat.
func (o *Ownership) ExecuteFamilyTalkLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, catalog world.FamilyCatalog) (storage.WorldReceipt, error) {
	command, ok := ParseFamilyTalkLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedFamilyTalkLine
	}
	payload, err := json.Marshal(familyTalkLineRequest{Kind: "family-talk", Line: line, Alias: command.Alias, Message: command.Message})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		result, err := state.PlanFamilyTalk(actorID, command.Message, catalog)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		return raw, response, err
	})
}
