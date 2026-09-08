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

var ErrUnsupportedEquipmentLine = errors.New("line is not an implemented equipment mutation command")

type equipmentAction struct {
	mode       world.EquipmentMode
	remove     bool
	all        bool
	name       string
	occurrence int
}

func parseEquipmentLine(line string) (equipmentAction, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 || len(fields) > 3 {
		return equipmentAction{}, false
	}
	action := equipmentAction{name: fields[1], occurrence: 1}
	switch fields[0] {
	case "입어":
		action.mode = world.EquipmentWear
	case "쥐어":
		action.mode = world.EquipmentHold
	case "무장":
		action.mode = world.EquipmentWield
	case "벗어":
		action.remove = true
	default:
		return equipmentAction{}, false
	}
	if len(fields) == 3 {
		occurrence, err := strconv.Atoi(fields[2])
		if err != nil || occurrence < 1 {
			return equipmentAction{}, false
		}
		action.occurrence = occurrence
	}
	if action.name == "모두" {
		if len(fields) != 2 {
			return equipmentAction{}, false
		}
		if action.remove {
			action.all = true
		} else if action.mode == world.EquipmentWear {
			action.all = true
		} else {
			return equipmentAction{}, false
		}
	}
	return action, true
}

// ExecuteEquipmentLine is the durable command boundary for command3's live
// inventory/ready transitions. It never mutates the state outside the receipt
// transaction and replay returns the original response without recalculation.
func (o *Ownership) ExecuteEquipmentLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	action, ok := parseEquipmentLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedEquipmentLine
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
		var result world.EquipmentMutationResult
		if action.all && action.remove {
			next, result, err = s.RemoveAllItems(actorID)
		} else if action.all {
			next, result, err = s.WearAllItems(actorID)
		} else if action.remove {
			next, result, err = s.RemoveItem(actorID, action.name, action.occurrence)
		} else {
			next, result, err = s.ReadyItem(actorID, action.name, action.occurrence, action.mode)
		}
		if err != nil {
			return nil, nil, err
		}
		state, err := json.Marshal(next)
		if err != nil {
			return nil, nil, err
		}
		verb := "입었습니다"
		switch result.Action {
		case "wear-all":
			if result.Count == 0 {
				verb = "입을 물건이 없습니다"
			} else {
				verb = "입었습니다"
			}
		case string(world.EquipmentHold):
			verb = "쥐었습니다"
		case string(world.EquipmentWield):
			verb = "전투태세를 취합니다"
		case "remove-all":
			if result.Count == 0 {
				verb = "벗을 물건이 없습니다"
			} else {
				verb = "벗었습니다"
			}
		case "remove":
			verb = "벗었습니다"
		}
		if result.Count == 0 && (result.Action == "wear-all" || result.Action == "remove-all") {
			response, err := json.Marshal(fmt.Sprintf("당신은 %s.\r\n", verb))
			return state, response, err
		}
		response, err := json.Marshal(fmt.Sprintf("당신은 %s을(를) %s.\r\n", result.ItemName, verb))
		return state, response, err
	})
}
