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

var ErrUnsupportedFamilyLine = errors.New("line is not an implemented family status command")

type FamilyCommandAction string

const (
	FamilyWhoAction    FamilyCommandAction = "who"
	FamilyMemberAction FamilyCommandAction = "member"
	FamilyListAction   FamilyCommandAction = "list"
)

type FamilyCommand struct {
	Action FamilyCommandAction
	Target string
}

type familyLineRequest struct {
	Kind   string              `json:"kind"`
	Line   string              `json:"line"`
	Action FamilyCommandAction `json:"action"`
	Target string              `json:"target,omitempty"`
}

func validFamilyCommandLine(line string) bool {
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

// ParseFamilyLine admits the read-only source forms. Membership changes,
// family chat, boss actions, marriage and war remain separate reducers.
func ParseFamilyLine(line string) (FamilyCommand, bool) {
	if !validFamilyCommandLine(line) {
		return FamilyCommand{}, false
	}
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil {
		return FamilyCommand{}, false
	}
	switch {
	case len(tokens) == 1 && tokens[0] == "패거리누구":
		return FamilyCommand{Action: FamilyWhoAction}, true
	case len(tokens) == 2 && tokens[0] == "패거리누구" && tokens[1] != "":
		return FamilyCommand{Action: FamilyWhoAction, Target: tokens[1]}, true
	case len(tokens) == 1 && tokens[0] == "패거리원":
		return FamilyCommand{Action: FamilyMemberAction}, true
	case len(tokens) == 1 && tokens[0] == "모든패거리":
		return FamilyCommand{Action: FamilyListAction}, true
	default:
		return FamilyCommand{}, false
	}
}

func IsFamilyLine(line string) bool {
	_, ok := ParseFamilyLine(line)
	return ok
}

// ExecuteFamilyLineWithCatalog routes all read-only family status commands
// through one receipt. Family names/bosses come from an immutable server-owned
// catalog; a missing catalog fails closed in the world reducer.
func (o *Ownership) ExecuteFamilyLineWithCatalog(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, catalog world.FamilyCatalog) (storage.WorldReceipt, error) {
	command, ok := ParseFamilyLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedFamilyLine
	}
	payload, err := json.Marshal(familyLineRequest{Kind: "family-status", Line: line, Action: command.Action, Target: command.Target})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		var text string
		switch command.Action {
		case FamilyWhoAction:
			text, err = state.FamilyWho(actorID, command.Target, catalog)
		case FamilyMemberAction:
			text, err = state.FamilyMember(actorID, catalog)
		case FamilyListAction:
			text, err = state.ListFamily(catalog)
		default:
			return nil, nil, ErrUnsupportedFamilyLine
		}
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(text)
		return raw, response, err
	})
}

func (o *Ownership) ExecuteFamilyWhoLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, catalog world.FamilyCatalog) (storage.WorldReceipt, error) {
	command, ok := ParseFamilyLine(line)
	if !ok || command.Action != FamilyWhoAction {
		return storage.WorldReceipt{}, ErrUnsupportedFamilyLine
	}
	return o.ExecuteFamilyLineWithCatalog(ctx, store, worldID, commandID, lease, line, catalog)
}
