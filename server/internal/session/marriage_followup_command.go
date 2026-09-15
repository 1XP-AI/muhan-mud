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

var (
	ErrUnsupportedDivorceLine      = errors.New("line is not an implemented divorce command")
	ErrUnsupportedMarriageSendLine = errors.New("line is not an implemented spouse-message command")
)

// DivorceCommand is the exact no-argument `이혼` command from command11.c.
// The reducer owns all relationship state and spouse resolution.
type DivorceCommand struct{}

func ParseDivorceLine(line string) (DivorceCommand, bool) {
	if !validFollowupLine(line) || strings.TrimSpace(line) != "이혼" {
		return DivorceCommand{}, false
	}
	return DivorceCommand{}, true
}

func IsDivorceLine(line string) bool {
	_, ok := ParseDivorceLine(line)
	return ok
}

type divorceLineRequest struct {
	Kind string `json:"kind"`
	Line string `json:"line"`
}

// ExecuteDivorceLine stores the source divorce proposal/result as one durable
// world receipt. Recipient and broadcast projections are emitted by transport
// only after a successful first commit; retries return the saved bytes.
func (o *Ownership) ExecuteDivorceLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	if _, ok := ParseDivorceLine(line); !ok {
		return storage.WorldReceipt{}, ErrUnsupportedDivorceLine
	}
	payload, err := json.Marshal(divorceLineRequest{Kind: "divorce", Line: line})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanDivorce(actorID)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyDivorce(proposal)
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

// MarriageSendCommand preserves the text before the source suffix alias
// `사랑말` as one payload. command1.c parses the final token as the command and
// m_send then cuts that suffix from fullstr; the reducer receives only the
// message bytes and never reparses them as a command.
type MarriageSendCommand struct {
	Message string
}

func validFollowupLine(line string) bool {
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

func ParseMarriageSendLine(line string) (MarriageSendCommand, bool) {
	if !validFollowupLine(line) {
		return MarriageSendCommand{}, false
	}
	trimmed := strings.TrimSpace(line)
	if trimmed == "사랑말" {
		return MarriageSendCommand{}, true
	}
	separator := strings.LastIndexFunc(trimmed, unicode.IsSpace)
	if separator < 0 || strings.TrimSpace(trimmed[separator:]) != "사랑말" {
		return MarriageSendCommand{}, false
	}
	message := strings.TrimSpace(trimmed[:separator])
	return MarriageSendCommand{Message: message}, true
}

func IsMarriageSendLine(line string) bool {
	_, ok := ParseMarriageSendLine(line)
	return ok
}

type marriageSendLineRequest struct {
	Kind    string `json:"kind"`
	Line    string `json:"line"`
	Message string `json:"message"`
}

// ExecuteMarriageSendLine stores the source spouse-message response and
// recipient projection as one durable receipt. Transport emits the recipient
// event only after the first successful commit.
func (o *Ownership) ExecuteMarriageSendLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	command, ok := ParseMarriageSendLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedMarriageSendLine
	}
	payload, err := json.Marshal(marriageSendLineRequest{Kind: "marriage-send", Line: line, Message: command.Message})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanMarriageSend(actorID, command.Message)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyMarriageSend(proposal)
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
