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

var ErrUnsupportedDrinkLine = errors.New("line is not an implemented drink command")

// DrinkCommand accepts the original Korean verbs 먹어/마셔 and an optional
// one-based occurrence for duplicate direct item roots.
type DrinkCommand struct {
	Alias      string
	ItemName   string
	Occurrence int
}

type drinkLineRequest struct {
	Kind       string `json:"kind"`
	Line       string `json:"line"`
	Alias      string `json:"alias"`
	ItemName   string `json:"item_name"`
	Occurrence int    `json:"occurrence"`
	Now        int32  `json:"now"`
}

type DrinkOptions = world.DrinkOptions

func validDrinkLine(line string) bool {
	if !utf8.ValidString(line) {
		return false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\r' || r == '\n' || r == '\x00' || r == '\x1b' || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

func ParseDrinkLine(line string) (DrinkCommand, bool) {
	if !validDrinkLine(line) {
		return DrinkCommand{}, false
	}
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || len(tokens) < 2 || len(tokens) > 3 {
		return DrinkCommand{}, false
	}
	if tokens[0] != "먹어" && tokens[0] != "마셔" {
		return DrinkCommand{}, false
	}
	if tokens[1] == "" || strings.TrimSpace(tokens[1]) != tokens[1] {
		return DrinkCommand{}, false
	}
	command := DrinkCommand{Alias: tokens[0], ItemName: tokens[1], Occurrence: 1}
	if len(tokens) == 3 {
		occurrence, err := strconv.ParseUint(tokens[2], 10, 31)
		if err != nil || occurrence < 1 {
			return DrinkCommand{}, false
		}
		command.Occurrence = int(occurrence)
	}
	return command, true
}

func IsDrinkLine(line string) bool {
	_, ok := ParseDrinkLine(line)
	return ok
}

func (o *Ownership) ExecuteDrinkLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32, roll func(int, int) int) (storage.WorldReceipt, error) {
	return o.ExecuteDrinkLineWithOptions(ctx, store, worldID, commandID, lease, line, DrinkOptions{Now: now, Roll: roll})
}

func (o *Ownership) ExecuteDrinkLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options DrinkOptions) (storage.WorldReceipt, error) {
	command, ok := ParseDrinkLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedDrinkLine
	}
	payload, err := json.Marshal(drinkLineRequest{Kind: "drink", Line: line, Alias: command.Alias, ItemName: command.ItemName, Occurrence: command.Occurrence, Now: options.Now})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanDrink(actorID, command.ItemName, command.Occurrence, options)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyDrink(proposal)
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
