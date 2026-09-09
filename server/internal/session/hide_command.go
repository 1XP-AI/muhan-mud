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

var ErrUnsupportedHideLine = errors.New("line is not an implemented bare hide command")

type HideCommand struct {
	Target     string
	Occurrence int
}

type HideOptions struct {
	Now  int32
	Roll func(int, int) int
}

type hideLineRequest struct {
	Kind       string `json:"kind"`
	Line       string `json:"line"`
	Target     string `json:"target,omitempty"`
	Occurrence int    `json:"occurrence"`
	Now        int32  `json:"now"`
}

// ParseHideLine admits the original bare aliases and the bounded object form
// `숨겨 <name> [occurrence]`/`숨어 <name> [occurrence]`. The target is a display
// name only; canonical object identity is resolved by the world reducer.
func ParseHideLine(line string) (HideCommand, bool) {
	if !utf8.ValidString(line) {
		return HideCommand{}, false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\r' || r == '\n' || r == '\x00' {
			return HideCommand{}, false
		}
	}
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || len(tokens) < 1 || len(tokens) > 3 || (tokens[0] != "숨겨" && tokens[0] != "숨어") {
		return HideCommand{}, false
	}
	command := HideCommand{Occurrence: 1}
	if len(tokens) == 1 {
		return command, true
	}
	if tokens[1] == "" || strings.TrimSpace(tokens[1]) != tokens[1] {
		return HideCommand{}, false
	}
	command.Target = tokens[1]
	if len(tokens) == 3 {
		occurrence, err := strconv.ParseUint(tokens[2], 10, 31)
		if err != nil || occurrence < 1 {
			return HideCommand{}, false
		}
		command.Occurrence = int(occurrence)
	}
	return command, true
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
	command, ok := ParseHideLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedHideLine
	}
	payload, err := json.Marshal(hideLineRequest{Kind: "hide", Line: line, Target: command.Target, Occurrence: command.Occurrence, Now: options.Now})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		s, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := s.PlanHideTargetWithOccurrence(actorID, command.Target, command.Occurrence, options.Now, options.Roll)
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
