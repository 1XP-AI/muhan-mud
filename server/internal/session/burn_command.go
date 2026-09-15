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

var ErrUnsupportedBurnLine = errors.New("line is not an implemented burn command")

// BurnCommand admits the two source aliases and the positive direct-root
// occurrence extension: 태워 <물건> [순번] or 소각 <물건> [순번].
type BurnCommand struct {
	Alias      string
	ItemName   string
	Occurrence int
}

type burnLineRequest struct {
	Kind       string `json:"kind"`
	Line       string `json:"line"`
	Alias      string `json:"alias"`
	ItemName   string `json:"item_name"`
	Occurrence int    `json:"occurrence"`
	Now        int32  `json:"now"`
}

type BurnOptions = world.BurnOptions

func validBurnLine(line string) bool {
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

func ParseBurnLine(line string) (BurnCommand, bool) {
	if !validBurnLine(line) {
		return BurnCommand{}, false
	}
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || (len(tokens) != 2 && len(tokens) != 3) {
		return BurnCommand{}, false
	}
	if tokens[0] != "태워" && tokens[0] != "소각" {
		return BurnCommand{}, false
	}
	if tokens[1] == "" || strings.TrimSpace(tokens[1]) != tokens[1] {
		return BurnCommand{}, false
	}
	command := BurnCommand{Alias: tokens[0], ItemName: tokens[1], Occurrence: 1}
	if len(tokens) == 3 {
		occurrence, err := strconv.ParseUint(tokens[2], 10, 31)
		if err != nil || occurrence < 1 {
			return BurnCommand{}, false
		}
		command.Occurrence = int(occurrence)
	}
	return command, true
}

func IsBurnLine(line string) bool {
	_, ok := ParseBurnLine(line)
	return ok
}

// ExecuteBurnLine uses a host-bound clock and optional jackpot draw. The draw
// is consumed only if the first reducer evaluation reaches a successful burn;
// receipt replay returns the stored typed result without re-running it.
func (o *Ownership) ExecuteBurnLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32, roll func(int, int) int) (storage.WorldReceipt, error) {
	return o.ExecuteBurnLineWithOptions(ctx, store, worldID, commandID, lease, line, BurnOptions{Now: now, Roll: roll})
}

func (o *Ownership) ExecuteBurnLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options BurnOptions) (storage.WorldReceipt, error) {
	command, ok := ParseBurnLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedBurnLine
	}
	payload, err := json.Marshal(burnLineRequest{
		Kind: "burn", Line: line, Alias: command.Alias, ItemName: command.ItemName,
		Occurrence: command.Occurrence, Now: options.Now,
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanBurn(actorID, command.ItemName, command.Occurrence, options.Now, options.Roll)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyBurn(proposal)
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
