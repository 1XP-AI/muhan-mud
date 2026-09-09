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

var ErrUnsupportedTitleLine = errors.New("line is not an implemented title command")

type TitleCommand struct {
	Action world.TitleAction
	Title  string
}

type titleLineRequest struct {
	Kind   string            `json:"kind"`
	Line   string            `json:"line"`
	Action world.TitleAction `json:"action"`
	Title  string            `json:"title,omitempty"`
}

func ParseTitleLine(line string) (TitleCommand, bool) {
	if !utf8.ValidString(line) {
		return TitleCommand{}, false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return TitleCommand{}, false
		}
	}
	if line == "칭호삭제" {
		return TitleCommand{Action: world.TitleClear}, true
	}
	if line == "칭호" {
		return TitleCommand{Action: world.TitleView}, true
	}
	if !strings.HasPrefix(line, "칭호 ") {
		return TitleCommand{}, false
	}
	title := strings.TrimPrefix(line, "칭호 ")
	if err := world.ValidatePlayerTitle(title); err != nil {
		return TitleCommand{}, false
	}
	return TitleCommand{Action: world.TitleSet, Title: title}, true
}

func IsTitleLine(line string) bool {
	_, ok := ParseTitleLine(line)
	return ok
}

func (o *Ownership) ExecuteTitleLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	command, ok := ParseTitleLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedTitleLine
	}
	request, err := json.Marshal(titleLineRequest{Kind: "title", Line: line, Action: command.Action, Title: command.Title})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, request, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		s, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		if command.Action == world.TitleView {
			result, err := s.ViewTitle(actorID)
			if err != nil {
				return nil, nil, err
			}
			response, err := json.Marshal(result)
			return raw, response, err
		}
		var proposal world.TitleProposal
		if command.Action == world.TitleSet {
			proposal, err = s.PlanSetTitle(actorID, command.Title)
		} else {
			proposal, err = s.PlanClearTitle(actorID)
		}
		if err != nil {
			return nil, nil, err
		}
		next, result, err := s.ApplyTitle(proposal)
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
