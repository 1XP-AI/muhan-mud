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

var ErrUnsupportedHideLine = errors.New("line is not an implemented bare hide command")

type HideCommand struct{}

type HideOptions struct {
	Now  int32
	Roll func(int, int) int
}

type hideLineRequest struct {
	Kind string `json:"kind"`
	Line string `json:"line"`
	Now  int32  `json:"now"`
}

// ParseHideLine admits only the original bare player aliases. Object hiding
// remains an explicit future slice because its ONOTAK/object inventory
// authority is not present in the current canonical world state.
func ParseHideLine(line string) (HideCommand, bool) {
	if !utf8.ValidString(line) {
		return HideCommand{}, false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\r' || r == '\n' || r == '\x00' {
			return HideCommand{}, false
		}
	}
	switch strings.TrimSpace(line) {
	case "숨겨", "숨어":
		return HideCommand{}, true
	default:
		return HideCommand{}, false
	}
}

func IsHideLine(line string) bool {
	_, ok := ParseHideLine(line)
	return ok
}

func (o *Ownership) ExecuteHideLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32, roll func(int, int) int) (storage.WorldReceipt, error) {
	return o.ExecuteHideLineWithOptions(ctx, store, worldID, commandID, lease, line, HideOptions{Now: now, Roll: roll})
}

func (o *Ownership) ExecuteHideLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options HideOptions) (storage.WorldReceipt, error) {
	if _, ok := ParseHideLine(line); !ok {
		return storage.WorldReceipt{}, ErrUnsupportedHideLine
	}
	payload, err := json.Marshal(hideLineRequest{Kind: "hide", Line: line, Now: options.Now})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		s, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := s.PlanHide(actorID, options.Now, options.Roll)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := s.ApplyHide(proposal)
		if err != nil {
			return nil, nil, err
		}
		state, err := json.Marshal(next)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		return state, response, err
	})
}
