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

var ErrUnsupportedDoorKeyLine = errors.New("line is not an implemented door key command")

type DoorKeyCommand struct {
	Action string
	Target string
	Key    string
}

type DoorKeyOptions struct {
	Now  int32
	Roll func(int, int) int
}

type doorKeyLineRequest struct {
	Kind   string `json:"kind"`
	Line   string `json:"line"`
	Action string `json:"action"`
	Target string `json:"target"`
	Key    string `json:"key"`
	Now    int32  `json:"now"`
}

// ParseDoorKeyLine admits the source command6.c vocabulary.  The target is a
// room-exit prefix and the optional key is an inventory name; neither a room
// ID nor an item ID can be supplied by the client.
func ParseDoorKeyLine(line string) (DoorKeyCommand, bool) {
	if !utf8.ValidString(line) {
		return DoorKeyCommand{}, false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\r' || r == '\n' || r == '\x00' {
			return DoorKeyCommand{}, false
		}
	}
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || len(tokens) < 1 || len(tokens) > 3 {
		return DoorKeyCommand{}, false
	}
	command := DoorKeyCommand{}
	switch tokens[0] {
	case "풀어":
		command.Action = world.DoorUnlock
	case "잠궈":
		command.Action = world.DoorLock
	case "따":
		command.Action = world.DoorPick
	default:
		return DoorKeyCommand{}, false
	}
	if len(tokens) >= 2 {
		command.Target = tokens[1]
	}
	if len(tokens) == 3 {
		if command.Action == world.DoorPick {
			return DoorKeyCommand{}, false
		}
		command.Key = tokens[2]
	}
	if command.Action == world.DoorPick && len(tokens) > 2 {
		return DoorKeyCommand{}, false
	}
	return command, true
}

func IsDoorKeyLine(line string) bool {
	_, ok := ParseDoorKeyLine(line)
	return ok
}

func (o *Ownership) ExecuteDoorKeyLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32, roll func(int, int) int) (storage.WorldReceipt, error) {
	return o.ExecuteDoorKeyLineWithOptions(ctx, store, worldID, commandID, lease, line, DoorKeyOptions{Now: now, Roll: roll})
}

// ExecuteDoorKeyLineWithOptions binds the one picklock random result inside
// the reducer candidate. Replay reads the stored receipt and never invokes
// Roll again.
func (o *Ownership) ExecuteDoorKeyLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options DoorKeyOptions) (storage.WorldReceipt, error) {
	command, ok := ParseDoorKeyLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedDoorKeyLine
	}
	payload, err := json.Marshal(doorKeyLineRequest{Kind: "door-key", Line: line, Action: command.Action, Target: command.Target, Key: command.Key, Now: options.Now})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		s, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := s.PlanDoorKey(actorID, command.Action, command.Target, command.Key, options.Now, options.Roll)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := s.ApplyDoorKey(proposal)
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
