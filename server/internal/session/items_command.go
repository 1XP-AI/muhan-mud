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

var ErrUnsupportedItemsLine = errors.New("line is not an implemented items command")

func isItemsVerb(token string) bool {
	switch token {
	case "소지품", "장비", "장":
		return true
	default:
		return false
	}
}

func itemsCommand(line string) (string, bool) {
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || len(tokens) == 0 || len(tokens) > 7 {
		return "", false
	}
	switch tokens[len(tokens)-1] {
	case "소지품":
		return "inventory", true
	case "장비", "장":
		return "equipment", true
	default:
		return "", false
	}
}

func IsItemsLine(line string) bool {
	_, ok := itemsCommand(line)
	return ok
}

// ExecuteItemsLine connects the read-only inventory/equipment projections to
// the same durable receipt boundary as other terminal commands.
func (o *Ownership) ExecuteItemsLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	kind, ok := itemsCommand(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedItemsLine
	}
	payload, err := json.Marshal(struct{ Line string }{line})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		s, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		var text string
		if kind == "inventory" {
			text, err = s.PlayerInventory(actorID)
		} else {
			text, err = s.PlayerEquipment(actorID)
		}
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(text)
		return raw, response, err
	})
}
