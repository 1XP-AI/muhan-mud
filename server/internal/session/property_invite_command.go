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

var ErrUnsupportedPropertyInviteLine = errors.New("line is not an implemented property invite command")

type PropertyInviteCommand struct {
	Target string
}

type propertyInviteLineRequest struct {
	Kind   string `json:"kind"`
	Line   string `json:"line"`
	Target string `json:"target,omitempty"`
}

func validPropertyInviteLine(line string) bool {
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

// ParsePropertyInviteLine admits the original `초대` list form and one exact
// target token for the add/remove toggle. Client names are resolved again by
// the world reducer; no client-supplied ID is accepted.
func ParsePropertyInviteLine(line string) (PropertyInviteCommand, bool) {
	if !validPropertyInviteLine(line) {
		return PropertyInviteCommand{}, false
	}
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || len(tokens) < 1 || len(tokens) > 2 || tokens[0] != "초대" {
		return PropertyInviteCommand{}, false
	}
	if len(tokens) == 1 {
		return PropertyInviteCommand{}, true
	}
	if tokens[1] == "" || strings.TrimSpace(tokens[1]) != tokens[1] {
		return PropertyInviteCommand{}, false
	}
	return PropertyInviteCommand{Target: tokens[1]}, true
}

func IsPropertyInviteLine(line string) bool {
	_, ok := ParsePropertyInviteLine(line)
	return ok
}

// ExecutePropertyInviteLine uses a single receipt for each toggle; the bare
// list form is also durable so a replay returns the exact ordered names.
func (o *Ownership) ExecutePropertyInviteLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	command, ok := ParsePropertyInviteLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedPropertyInviteLine
	}
	payload, err := json.Marshal(propertyInviteLineRequest{Kind: "property-invite", Line: line, Target: command.Target})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		var result world.PropertyInviteResult
		var next world.State = state
		if command.Target == "" {
			result, err = state.ListPropertyInvitations(actorID)
		} else {
			proposal, planErr := state.PlanPropertyInvite(actorID, command.Target)
			if planErr != nil {
				return nil, nil, planErr
			}
			next, result, err = state.ApplyPropertyInvite(proposal)
		}
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
