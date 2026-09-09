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

// ErrUnsupportedRepairLine is returned before a receipt exists when the line
// is outside the bounded direct-inventory repair command.
var ErrUnsupportedRepairLine = errors.New("line is not an implemented repair command")

// RepairCommand contains only client-facing display-name input. Item IDs are
// resolved by the world reducer from the committed actor snapshot.
type RepairCommand struct {
	Name       string
	Occurrence int
}

type repairLineRequest struct {
	Kind       string `json:"kind"`
	Line       string `json:"line"`
	Name       string `json:"name"`
	Occurrence int    `json:"occurrence"`
}

// RepairOptions carries the one random source used by the repair reducer.
// The source is consumed during planning only; ExecuteGame retries use the
// durable receipt and never invoke it again.
type RepairOptions struct {
	Roll func(int, int) int
}

// ParseRepairLine admits the exact Korean repair alias with an exact display
// name and an optional positive one-based occurrence. Prefix/key/equipped
// matching and legacy linked-list inventory remain outside this boundary.
func ParseRepairLine(line string) (RepairCommand, bool) {
	if !utf8.ValidString(line) {
		return RepairCommand{}, false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\r' || r == '\n' || r == '\x00' || r == '\x1b' || r == '\u2028' || r == '\u2029' {
			return RepairCommand{}, false
		}
	}
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || len(tokens) < 2 || len(tokens) > 3 || tokens[0] != "수리" {
		return RepairCommand{}, false
	}
	if tokens[1] == "" || strings.TrimSpace(tokens[1]) != tokens[1] {
		return RepairCommand{}, false
	}
	command := RepairCommand{Name: tokens[1], Occurrence: 1}
	if len(tokens) == 3 {
		occurrence, err := strconv.ParseUint(tokens[2], 10, 31)
		if err != nil || occurrence < 1 {
			return RepairCommand{}, false
		}
		command.Occurrence = int(occurrence)
	}
	return command, true
}

// IsRepairLine is a descriptive parser helper for transport dispatchers.
func IsRepairLine(line string) bool {
	_, ok := ParseRepairLine(line)
	return ok
}

// ExecuteRepairLine runs repair through the authenticated session and durable
// ExecuteGame boundary. The random source is injected by the owning game loop.
func (o *Ownership) ExecuteRepairLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, roll func(int, int) int) (storage.WorldReceipt, error) {
	return o.ExecuteRepairLineWithOptions(ctx, store, worldID, commandID, lease, line, RepairOptions{Roll: roll})
}

func (o *Ownership) ExecuteRepairLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options RepairOptions) (storage.WorldReceipt, error) {
	command, ok := ParseRepairLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedRepairLine
	}
	payload, err := json.Marshal(repairLineRequest{
		Kind: "repair", Line: line, Name: command.Name, Occurrence: command.Occurrence,
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanRepair(actorID, command.Name, command.Occurrence, options.Roll)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyRepair(proposal)
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
