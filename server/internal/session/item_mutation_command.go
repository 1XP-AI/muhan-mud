package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

var ErrUnsupportedItemMutationLine = errors.New("line is not an implemented item mutation command")

type itemMutationAction struct {
	verb, name string
	occurrence int
	all        bool
	nested     bool
	container  string
}

func parseItemMutationLine(line string) (itemMutationAction, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 || len(fields) > 3 {
		return itemMutationAction{}, false
	}
	verb := fields[0]
	if verb != "주워" && verb != "주" && verb != "가져" && verb != "꺼내" && verb != "버려" && verb != "넣어" {
		return itemMutationAction{}, false
	}
	action := itemMutationAction{verb: verb, name: fields[1], occurrence: 1}
	if len(fields) == 3 {
		if occurrence, err := strconv.Atoi(fields[2]); err == nil {
			if occurrence < 1 {
				return itemMutationAction{}, false
			}
			action.occurrence = occurrence
		} else {
			action.nested = true
			if fields[0] == "주워" || fields[0] == "주" || fields[0] == "가져" || fields[0] == "꺼내" {
				action.container = fields[1]
				action.name = fields[2]
			} else {
				action.container = fields[2]
			}
		}
	}
	if action.nested && action.name == "모두" {
		return itemMutationAction{}, false
	}
	if action.name == "모두" {
		if len(fields) != 2 {
			return itemMutationAction{}, false
		}
		action.all = true
	}
	if verb == "주워" || verb == "주" || verb == "가져" || verb == "꺼내" {
		action.verb = "take"
	} else {
		action.verb = "drop"
	}
	return action, true
}

func (o *Ownership) ExecuteItemMutationLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	action, ok := parseItemMutationLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedItemMutationLine
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
		var next world.State
		var result world.ItemMutationResult
		if action.nested && action.verb == "take" {
			next, result, err = s.TakeContainedItem(actorID, action.container, action.name, action.occurrence)
		} else if action.nested && action.verb == "drop" {
			next, result, err = s.DropContainedItem(actorID, action.name, action.container, action.occurrence)
		} else if action.verb == "take" && action.all {
			next, result, err = s.TakeAllItems(actorID)
		} else if action.verb == "drop" && action.all {
			next, result, err = s.DropAllItems(actorID)
		} else if action.verb == "take" {
			next, result, err = s.TakeItem(actorID, action.name, action.occurrence)
		} else {
			next, result, err = s.DropItem(actorID, action.name, action.occurrence)
		}
		if err != nil {
			return nil, nil, err
		}
		state, err := json.Marshal(next)
		if err != nil {
			return nil, nil, err
		}
		verb := "버렸습니다"
		if result.Action == "take" || result.Action == "take-all" || result.Action == "take-contained" {
			verb = "주웠습니다"
		}
		if result.Count == 0 && (result.Action == "take-all" || result.Action == "drop-all") {
			if result.Action == "take-all" {
				response, marshalErr := json.Marshal("여기에는 주울 수 있는 물건이 없습니다.\r\n")
				return state, response, marshalErr
			}
			response, marshalErr := json.Marshal("버릴 수 있는 물건이 없습니다.\r\n")
			return state, response, marshalErr
		}
		response, err := json.Marshal(fmt.Sprintf("%s을(를) %s.\r\n", result.ItemName, verb))
		return state, response, err
	})
}
