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

var ErrUnsupportedFamilyWarLine = errors.New("line is not an implemented family-war command")

type FamilyWarCommand struct {
	Target string
}

type familyWarLineRequest struct {
	Kind   string `json:"kind"`
	Line   string `json:"line"`
	Target string `json:"target,omitempty"`
}

func validFamilyWarLine(line string) bool {
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

// ParseFamilyWarLine admits special1.c:call_war. Bare `선전포고` is the original
// missing-argument prompt; extra tokens also take that branch because C
// requires cmnd->num == 2.
func ParseFamilyWarLine(line string) (FamilyWarCommand, bool) {
	if !validFamilyWarLine(line) {
		return FamilyWarCommand{}, false
	}
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || len(tokens) == 0 || tokens[0] != "선전포고" {
		return FamilyWarCommand{}, false
	}
	command := FamilyWarCommand{}
	if len(tokens) == 2 && tokens[1] != "" && strings.TrimSpace(tokens[1]) == tokens[1] {
		command.Target = tokens[1]
	}
	return command, true
}

func IsFamilyWarLine(line string) bool {
	_, ok := ParseFamilyWarLine(line)
	return ok
}

func (o *Ownership) ExecuteFamilyWarLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, catalog world.FamilyCatalog) (storage.WorldReceipt, error) {
	command, ok := ParseFamilyWarLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedFamilyWarLine
	}
	payload, err := json.Marshal(familyWarLineRequest{Kind: "family-war", Line: line, Target: command.Target})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanFamilyWar(actorID, command.Target, catalog)
		if err != nil {
			return nil, nil, err
		}
		var next world.State
		var result world.FamilyWarResult
		if !proposal.Changed {
			result = world.FamilyWarResult{
				Action: proposal.Action, ActorID: proposal.ActorID, ActorName: proposal.ActorName,
				FamilyID: proposal.FamilyID, FamilyName: proposal.FamilyName,
				TargetID: proposal.TargetID, TargetName: proposal.TargetName,
				Before: proposal.Before, After: proposal.After,
				Response: proposal.Response, Changed: false,
			}
			next = state
		} else {
			next, result, err = state.ApplyFamilyWar(proposal)
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
