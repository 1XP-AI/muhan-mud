package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

var ErrUnsupportedItemMutationLine = errors.New("line is not an implemented item mutation command")

type itemMutationAction struct {
	verb, name          string
	occurrence          int
	containerOccurrence int
	all                 bool
	nested              bool
	container           string
}

func isItemMutationVerb(token string) bool {
	switch token {
	case "주워", "주", "가져", "꺼내", "버려", "넣어":
		return true
	default:
		return false
	}
}

func validItemMutationLine(line string) bool {
	if !utf8.ValidString(line) {
		return false
	}
	for _, r := range line {
		switch r {
		case '\r', '\n', '\x00', '\x1b', '\u2028', '\u2029':
			return false
		}
	}
	return true
}

// parseItemMutationLine admits prefix `주워 검` and C parse() last-token
// `검 주워` / `검 버려`. Last-token verbs win when both ends are mutation
// aliases. C get()/drop() with cmnd->num>2 are nested str[1]/str[2]
// (container/item); 4+ token last-token keeps those two and ignores
// str[3+] (`검 junk extra 주워` → take junk from 검). A number in rest[1]
// fills val[1] and rest[2] is still the nested peer (`검 2 extra 버려`).
// A number as the last rest token fills C val[2] of the previous name
// (`가방 보석 2 꺼내` item #2, `보석 가방 2 버려` container #2). Take case 3
// applies rest[1] to the container (`가방 2 보석 꺼내`). Simultaneous
// val[1]+val[2] keeps both (`가방 2 보석 3 꺼내` item #3 from container
// #2, `보석 3 가방 2 버려` item #3 into container #2). Exact 2–3 token
// last-token forms stay floor/occurrence/nested. Prefix extras and
// controls stay fail-closed.
func parseItemMutationLine(line string) (itemMutationAction, bool) {
	if !validItemMutationLine(line) {
		return itemMutationAction{}, false
	}
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || len(tokens) < 2 || len(tokens) > 7 {
		return itemMutationAction{}, false
	}
	var verb string
	var rest []string
	if isItemMutationVerb(tokens[len(tokens)-1]) {
		verb = tokens[len(tokens)-1]
		rest = parseItemMutationLastTokenRest(tokens[:len(tokens)-1])
	} else if len(tokens) > 3 {
		return itemMutationAction{}, false
	} else if isItemMutationVerb(tokens[0]) {
		verb = tokens[0]
		rest = tokens[1:]
	} else {
		return itemMutationAction{}, false
	}
	if len(rest) < 1 || len(rest) > 4 || rest[0] == "" || strings.TrimSpace(rest[0]) != rest[0] {
		return itemMutationAction{}, false
	}
	take := verb == "주워" || verb == "주" || verb == "가져" || verb == "꺼내"
	action := itemMutationAction{verb: verb, name: rest[0], occurrence: 1, containerOccurrence: 1}
	switch len(rest) {
	case 2:
		if occurrence, err := strconv.Atoi(rest[1]); err == nil {
			if occurrence < 1 {
				return itemMutationAction{}, false
			}
			action.occurrence = occurrence
		} else {
			action.nested = true
			if take {
				action.container = rest[0]
				action.name = rest[1]
			} else {
				action.container = rest[1]
			}
		}
	case 3:
		if rest[1] == "" || strings.TrimSpace(rest[1]) != rest[1] || rest[2] == "" || strings.TrimSpace(rest[2]) != rest[2] {
			return itemMutationAction{}, false
		}
		action.nested = true
		if occurrence, err := strconv.Atoi(rest[1]); err == nil {
			if occurrence < 1 {
				return itemMutationAction{}, false
			}
			if take {
				action.container = rest[0]
				action.name = rest[2]
				action.containerOccurrence = occurrence
			} else {
				action.container = rest[2]
				action.occurrence = occurrence
			}
			break
		}
		occurrence, err := strconv.Atoi(rest[2])
		if err != nil || occurrence < 1 {
			return itemMutationAction{}, false
		}
		if take {
			action.container = rest[0]
			action.name = rest[1]
			action.occurrence = occurrence
		} else {
			action.container = rest[1]
			action.containerOccurrence = occurrence
		}
	case 4:
		if rest[1] == "" || strings.TrimSpace(rest[1]) != rest[1] || rest[2] == "" || strings.TrimSpace(rest[2]) != rest[2] || rest[3] == "" || strings.TrimSpace(rest[3]) != rest[3] {
			return itemMutationAction{}, false
		}
		action.nested = true
		val1, err := strconv.Atoi(rest[1])
		if err != nil || val1 < 1 {
			return itemMutationAction{}, false
		}
		val2, err := strconv.Atoi(rest[3])
		if err != nil || val2 < 1 {
			return itemMutationAction{}, false
		}
		if take {
			action.container = rest[0]
			action.name = rest[2]
			action.containerOccurrence = val1
			action.occurrence = val2
		} else {
			action.occurrence = val1
			action.container = rest[2]
			action.containerOccurrence = val2
		}
	}
	if action.nested && action.name == "모두" {
		return itemMutationAction{}, false
	}
	if action.name == "모두" {
		if len(rest) != 1 {
			return itemMutationAction{}, false
		}
		action.all = true
	}
	if take {
		action.verb = "take"
	} else {
		action.verb = "drop"
	}
	return action, true
}

func parseItemMutationLastTokenRest(rest []string) []string {
	if len(rest) <= 2 {
		return rest
	}
	if occurrence, err := strconv.Atoi(rest[1]); err == nil && occurrence >= 1 {
		if len(rest) >= 4 {
			if occurrence2, err := strconv.Atoi(rest[3]); err == nil && occurrence2 >= 1 {
				return rest[:4]
			}
		}
		return rest[:3]
	}
	if occurrence, err := strconv.Atoi(rest[2]); err == nil && occurrence >= 1 {
		return rest[:3]
	}
	return rest[:2]
}

func IsItemMutationLine(line string) bool {
	_, ok := parseItemMutationLine(line)
	return ok
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
			next, result, err = s.TakeContainedItem(actorID, action.container, action.name, action.occurrence, action.containerOccurrence)
		} else if action.nested && action.verb == "drop" {
			next, result, err = s.DropContainedItem(actorID, action.name, action.container, action.occurrence, action.containerOccurrence)
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
