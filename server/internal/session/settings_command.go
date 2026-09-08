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

var ErrUnsupportedSettingsLine = errors.New("line is not an implemented settings command")

type SettingsCommand struct {
	Action   string
	Key      string
	Value    int32
	HasValue bool
}

type settingsLineRequest struct {
	Kind     string `json:"kind"`
	Line     string `json:"line"`
	Action   string `json:"action"`
	Key      string `json:"key"`
	Value    int32  `json:"value,omitempty"`
	HasValue bool   `json:"has_value,omitempty"`
}

// ParseSettingsLine admits the original two command words and the one
// numeric setting accepted by C (설정 도망수치 <value>). Unknown option names
// are left to the world reducer, where set can reproduce C's flag-list
// fallback and clear can reproduce its explicit error response.
func ParseSettingsLine(line string) (SettingsCommand, bool) {
	if !utf8.ValidString(line) {
		return SettingsCommand{}, false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\r' || r == '\n' || r == '\x00' {
			return SettingsCommand{}, false
		}
	}
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || len(tokens) < 1 || len(tokens) > 3 {
		return SettingsCommand{}, false
	}
	command := SettingsCommand{}
	switch tokens[0] {
	case "설정":
		command.Action = "set"
	case "해제":
		command.Action = "clear"
	default:
		return SettingsCommand{}, false
	}
	if len(tokens) == 1 {
		return command, true
	}
	command.Key = tokens[1]
	if len(tokens) == 2 {
		return command, true
	}
	if command.Action != "set" || command.Key != "도망수치" {
		return SettingsCommand{}, false
	}
	value, err := world.ParseSettingsValue(tokens[2])
	if err != nil {
		return SettingsCommand{}, false
	}
	command.Value, command.HasValue = value, true
	return command, true
}

func IsSettingsLine(line string) bool {
	_, ok := ParseSettingsLine(line)
	return ok
}

// ExecuteSettingsLine persists the setting mutation and its exact actor
// response through the same receipt/replay reducer used by gameplay commands.
func (o *Ownership) ExecuteSettingsLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	command, ok := ParseSettingsLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedSettingsLine
	}
	payload, err := json.Marshal(settingsLineRequest{
		Kind:     "settings",
		Line:     line,
		Action:   command.Action,
		Key:      command.Key,
		Value:    command.Value,
		HasValue: command.HasValue,
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		s, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		var value *int32
		if command.HasValue {
			value = &command.Value
		}
		proposal, err := s.PlanSettings(actorID, command.Action, command.Key, value)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := s.ApplySettings(proposal)
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
