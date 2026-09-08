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

var ErrUnsupportedDoorLine = errors.New("line is not an implemented door command")

type DoorCommand struct {
	Action string
	Target string
}

type DoorOptions struct {
	Now int32
}

type doorLineRequest struct {
	Kind   string `json:"kind"`
	Line   string `json:"line"`
	Action string `json:"action"`
	Target string `json:"target"`
	Now    int32  `json:"now"`
}

// ParseDoorLine admits the bounded open/close vocabulary. The target remains
// a source-style prefix resolved against the authoritative room snapshot; no
// client can submit an exit index or room ID.
func ParseDoorLine(line string) (DoorCommand, bool) {
	if !utf8.ValidString(line) {
		return DoorCommand{}, false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\r' || r == '\n' || r == '\x00' {
			return DoorCommand{}, false
		}
	}
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || len(tokens) < 1 || len(tokens) > 2 {
		return DoorCommand{}, false
	}
	command := DoorCommand{}
	switch tokens[0] {
	case "열어":
		command.Action = world.DoorOpen
	case "닫아":
		command.Action = world.DoorClose
	default:
		return DoorCommand{}, false
	}
	if len(tokens) == 2 {
		if tokens[1] == "" || strings.TrimSpace(tokens[1]) != tokens[1] {
			return DoorCommand{}, false
		}
		command.Target = tokens[1]
	}
	return command, true
}

func IsDoorLine(line string) bool {
	_, ok := ParseDoorLine(line)
	return ok
}

func (o *Ownership) ExecuteDoorLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32) (storage.WorldReceipt, error) {
	return o.ExecuteDoorLineWithOptions(ctx, store, worldID, commandID, lease, line, DoorOptions{Now: now})
}

func (o *Ownership) ExecuteDoorLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options DoorOptions) (storage.WorldReceipt, error) {
	command, ok := ParseDoorLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedDoorLine
	}
	payload, err := json.Marshal(doorLineRequest{Kind: "door", Line: line, Action: command.Action, Target: command.Target, Now: options.Now})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		s, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := s.PlanDoor(actorID, command.Action, command.Target, options.Now)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := s.ApplyDoor(proposal)
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
