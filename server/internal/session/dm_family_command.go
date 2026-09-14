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

var ErrUnsupportedDMFamilyLine = errors.New("line is not an implemented DM family command")

type DMFamilyCommand struct {
	Action world.DMFamilyAction
}

type dmFamilyLineRequest struct {
	Kind   string               `json:"kind"`
	Line   string               `json:"line"`
	Action world.DMFamilyAction `json:"action"`
}

func validDMFamilyLine(line string) bool {
	if !utf8.ValidString(line) {
		return false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

func ParseDMFamilyLine(line string) (DMFamilyCommand, bool) {
	if !validDMFamilyLine(line) {
		return DMFamilyCommand{}, false
	}
	switch strings.TrimSpace(line) {
	case "*떨어져라":
		return DMFamilyCommand{Action: world.DMFamilyMoonstone}, true
	case "*침공":
		return DMFamilyCommand{Action: world.DMFamilyInvasion}, true
	default:
		return DMFamilyCommand{}, false
	}
}

func IsDMFamilyLine(line string) bool {
	_, ok := ParseDMFamilyLine(line)
	return ok
}

type DMFamilyOptions struct {
	Catalog  world.SpawnCatalog
	Roll     func(int, int) int
	Allocate func() (string, error)
}

func (o *Ownership) ExecuteDMFamilyLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options DMFamilyOptions) (storage.WorldReceipt, error) {
	command, ok := ParseDMFamilyLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedDMFamilyLine
	}
	payload, err := json.Marshal(dmFamilyLineRequest{Kind: "dm-family", Line: line, Action: command.Action})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		var proposal world.DMFamilyProposal
		switch command.Action {
		case world.DMFamilyMoonstone:
			proposal, err = state.PlanDMMoonstone(actorID, options.Catalog, options.Roll, options.Allocate)
		case world.DMFamilyInvasion:
			proposal, err = state.PlanDMInvasion(actorID, options.Catalog, options.Roll, options.Allocate)
		default:
			return nil, nil, ErrUnsupportedDMFamilyLine
		}
		if err != nil {
			return nil, nil, err
		}
		var next world.State
		var result world.DMFamilyResult
		if !proposal.Changed {
			result = world.DMFamilyResult{Action: proposal.Action, ActorID: proposal.ActorID, Response: proposal.Response}
			next = state
		} else {
			next, result, err = state.ApplyDMFamily(proposal)
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
