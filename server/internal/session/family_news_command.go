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
	// ErrUnsupportedFamilyNewsLine is returned before a durable receipt for
	// input outside post.c:family_news.
	ErrUnsupportedFamilyNewsLine = errors.New("line is not an implemented family news command")
	// ErrFamilyNewsAppendContinuationRequired marks the original `패거리공지 a`
	// editor start. Line collection is connection-local; each submitted line
	// becomes its own append receipt.
	ErrFamilyNewsAppendContinuationRequired = errors.New("family news append requires a connection-local continuation")
)

// FamilyNewsCommand is the parser-owned projection of family_news. Append
// start never crosses ExecuteFamilyNewsLine; the world reducer only sees
// view/delete/invalid or a single editor line.
type FamilyNewsCommand struct {
	Action world.FamilyNewsAction
}

type familyNewsLineRequest struct {
	Kind   string                 `json:"kind"`
	Line   string                 `json:"line"`
	Action world.FamilyNewsAction `json:"action"`
	Body   string                 `json:"body,omitempty"`
}

func validFamilyNewsLine(line string) bool {
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

func familyNewsOption(token string) rune {
	r, _ := utf8.DecodeRuneInString(token)
	if r >= 'A' && r <= 'Z' {
		return r + ('a' - 'A')
	}
	return r
}

// ParseFamilyNewsLine admits the original family_news shapes:
//
//	패거리공지
//	패거리공지 a
//	패거리공지 d
//
// C compares only the first character of the second token when cmnd->num==2.
// Three or more tokens fall through to the view branch, matching cmnd->num!=2.
func ParseFamilyNewsLine(line string) (FamilyNewsCommand, bool) {
	if !validFamilyNewsLine(line) {
		return FamilyNewsCommand{}, false
	}
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || len(tokens) == 0 || tokens[0] != "패거리공지" {
		return FamilyNewsCommand{}, false
	}
	if len(tokens) != 2 {
		return FamilyNewsCommand{Action: world.FamilyNewsView}, true
	}
	switch familyNewsOption(tokens[1]) {
	case 'a':
		return FamilyNewsCommand{Action: world.FamilyNewsAppend}, true
	case 'd':
		return FamilyNewsCommand{Action: world.FamilyNewsDelete}, true
	default:
		return FamilyNewsCommand{Action: world.FamilyNewsInvalid}, true
	}
}

func IsFamilyNewsLine(line string) bool {
	_, ok := ParseFamilyNewsLine(line)
	return ok
}

func ParseFamilyNewsAppendStartLine(line string) bool {
	command, ok := ParseFamilyNewsLine(line)
	return ok && command.Action == world.FamilyNewsAppend
}

func IsFamilyNewsAppendStartLine(line string) bool {
	return ParseFamilyNewsAppendStartLine(line)
}

func (o *Ownership) executeFamilyNews(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, request familyNewsLineRequest, catalog world.FamilyCatalog) (storage.WorldReceipt, error) {
	payload, err := json.Marshal(request)
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		var result world.FamilyNewsResult
		var next world.State
		switch request.Action {
		case world.FamilyNewsView:
			result, err = state.PlanFamilyNewsView(actorID, catalog)
			next = state
		case world.FamilyNewsInvalid:
			result = world.FamilyNewsResult{Action: world.FamilyNewsInvalid, ActorID: actorID, Response: world.FamilyNewsInvalidOptionResponse}
			next = state
		case world.FamilyNewsDelete:
			var proposal world.FamilyNewsProposal
			proposal, err = state.PlanFamilyNewsDelete(actorID, catalog)
			if err != nil {
				return nil, nil, err
			}
			if !proposal.Changed {
				result = world.FamilyNewsResult{
					Action: proposal.Action, ActorID: proposal.ActorID, ActorName: proposal.ActorName,
					FamilyID: proposal.FamilyID, FamilyName: proposal.FamilyName, Body: proposal.BeforeBody,
					Response: proposal.Response, Changed: false,
				}
				next = state
				break
			}
			next, result, err = state.ApplyFamilyNews(proposal)
		case world.FamilyNewsAppend:
			var proposal world.FamilyNewsProposal
			proposal, err = state.PlanFamilyNewsAppend(actorID, request.Body, catalog)
			if err != nil {
				return nil, nil, err
			}
			if !proposal.Changed {
				result = world.FamilyNewsResult{
					Action: proposal.Action, ActorID: proposal.ActorID, ActorName: proposal.ActorName,
					Response: proposal.Response, Changed: false,
				}
				next = state
				break
			}
			next, result, err = state.ApplyFamilyNews(proposal)
		default:
			return nil, nil, ErrUnsupportedFamilyNewsLine
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

// ExecuteFamilyNewsLine connects view/delete/invalid option to ExecuteGame.
// The `a` editor start is connection-local and does not create a receipt.
func (o *Ownership) ExecuteFamilyNewsLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, catalog world.FamilyCatalog) (storage.WorldReceipt, error) {
	command, ok := ParseFamilyNewsLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedFamilyNewsLine
	}
	if command.Action == world.FamilyNewsAppend {
		return storage.WorldReceipt{}, ErrFamilyNewsAppendContinuationRequired
	}
	return o.executeFamilyNews(ctx, store, worldID, commandID, lease, familyNewsLineRequest{
		Kind: "family-news", Line: line, Action: command.Action,
	}, catalog)
}

// ExecuteFamilyNewsAppendLine persists one newsedit line. The terminator `.`
// never reaches this reducer.
func (o *Ownership) ExecuteFamilyNewsAppendLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, catalog world.FamilyCatalog) (storage.WorldReceipt, error) {
	if !validFamilyNewsLine(line) {
		return storage.WorldReceipt{}, world.ErrFamilyNewsLineInvalid
	}
	return o.executeFamilyNews(ctx, store, worldID, commandID, lease, familyNewsLineRequest{
		Kind: "family-news", Line: line, Action: world.FamilyNewsAppend, Body: line,
	}, catalog)
}
