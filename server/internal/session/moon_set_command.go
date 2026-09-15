package session

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

var ErrUnsupportedMoonSetLine = errors.New("line is not an implemented moon-set command")

// MoonSetCommand is the parser-facing form of command8.c:moon_set.
// The item name is a display selector; the world reducer resolves the
// authoritative inventory root from its snapshot.
type MoonSetCommand struct {
	ItemName   string
	Occurrence int
}

type moonSetLineRequest struct {
	Kind       string `json:"kind"`
	Line       string `json:"line"`
	ItemName   string `json:"item_name,omitempty"`
	Occurrence int    `json:"occurrence,omitempty"`
}

func validMoonSetLine(line string) bool {
	if !utf8.ValidString(line) {
		return false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' || (unicode.IsSpace(r) && r != ' ') {
			return false
		}
	}
	return true
}

func looksLikeMoonSetOccurrence(token string) bool {
	if token == "" {
		return false
	}
	for i, r := range token {
		if (r == '+' || r == '-') && i == 0 {
			continue
		}
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// ParseMoonSetLine admits the original suffix form:
//
//	기억
//	<item...> 기억
//	<item...> <occurrence> 기억
//
// C parse() takes the last word as the verb, so prefix `기억 돌` is not this
// command. Unquoted spaces in the item selector are joined so `초인의 돌 기억`
// stays one inventory name; C EQUAL prefix matching is not broadened here.
func ParseMoonSetLine(line string) (MoonSetCommand, bool) {
	if !validMoonSetLine(line) {
		return MoonSetCommand{}, false
	}
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || len(tokens) == 0 || tokens[len(tokens)-1] != "기억" {
		return MoonSetCommand{}, false
	}
	command := MoonSetCommand{Occurrence: 1}
	rest := tokens[:len(tokens)-1]
	if len(rest) >= 2 && looksLikeMoonSetOccurrence(rest[len(rest)-1]) {
		candidate := rest[len(rest)-1]
		if strings.HasPrefix(candidate, "+") {
			return MoonSetCommand{}, false
		}
		occurrence, parseErr := strconv.ParseInt(candidate, 10, 31)
		if parseErr != nil || occurrence < 1 {
			return MoonSetCommand{}, false
		}
		command.Occurrence = int(occurrence)
		rest = rest[:len(rest)-1]
	}
	if len(rest) == 0 {
		return command, true
	}
	command.ItemName = strings.Join(rest, " ")
	if command.ItemName == "" || strings.TrimSpace(command.ItemName) != command.ItemName {
		return MoonSetCommand{}, false
	}
	return command, true
}

func IsMoonSetLine(line string) bool {
	_, ok := ParseMoonSetLine(line)
	return ok
}

func (o *Ownership) ExecuteMoonSetLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	command, ok := ParseMoonSetLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedMoonSetLine
	}
	payload, err := json.Marshal(moonSetLineRequest{
		Kind: "moon-set", Line: line, ItemName: command.ItemName, Occurrence: command.Occurrence,
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanMoonSet(actorID, command.ItemName, command.Occurrence)
		if err != nil {
			return nil, nil, err
		}
		var next world.State
		var result world.MoonSetResult
		if !proposal.Changed {
			result = world.MoonSetResult{
				Action: proposal.Action, ActorID: proposal.ActorID, RoomID: proposal.RoomID,
				RoomName: proposal.RoomName, ItemID: proposal.ItemID, ItemName: proposal.ItemName,
				Occurrence: proposal.Occurrence, Response: proposal.Response, Changed: false,
			}
			next = state
		} else {
			next, result, err = state.ApplyMoonSet(proposal)
			if err != nil {
				return nil, nil, err
			}
		}
		nextRaw, err := json.Marshal(next)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		return nextRaw, response, err
	})
}
