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

var ErrUnsupportedPeekLine = errors.New("line is not an implemented peek command")

type PeekCommand struct{ Target string }

type PeekOptions struct {
	Now  int32
	Roll func(int, int) int
}

type peekLineRequest struct {
	Kind   string `json:"kind"`
	Line   string `json:"line"`
	Target string `json:"target"`
	Now    int32  `json:"now"`
}

func ParsePeekLine(line string) (PeekCommand, bool) {
	if !utf8.ValidString(line) {
		return PeekCommand{}, false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\r' || r == '\n' || r == '\x00' {
			return PeekCommand{}, false
		}
	}
	fields := strings.Fields(line)
	if len(fields) != 2 || fields[0] != "엿봐" || fields[1] == "" || strings.TrimSpace(fields[1]) != fields[1] {
		return PeekCommand{}, false
	}
	return PeekCommand{Target: fields[1]}, true
}

func IsPeekLine(line string) bool {
	_, ok := ParsePeekLine(line)
	return ok
}

func (o *Ownership) ExecutePeekLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32, roll func(int, int) int) (storage.WorldReceipt, error) {
	return o.ExecutePeekLineWithOptions(ctx, store, worldID, commandID, lease, line, PeekOptions{Now: now, Roll: roll})
}

func (o *Ownership) ExecutePeekLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options PeekOptions) (storage.WorldReceipt, error) {
	command, ok := ParsePeekLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedPeekLine
	}
	payload, err := json.Marshal(peekLineRequest{Kind: "peek", Line: line, Target: command.Target, Now: options.Now})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		s, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := s.PlanPeek(actorID, command.Target, options.Now, options.Roll)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := s.ApplyPeek(proposal)
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
