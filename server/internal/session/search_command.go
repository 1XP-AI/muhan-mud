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

// ErrUnsupportedSearchLine is returned before a receipt exists for a line
// outside the exact bare search aliases. Prefix, occurrence and target forms
// are not inferred from the legacy command table.
var ErrUnsupportedSearchLine = errors.New("line is not an implemented search command")

// SearchCommand is the parser's closed command result. Search has no target;
// all target identities are discovered from the committed room snapshot.
type SearchCommand struct{}

// SearchOptions binds host-owned clock and random sources to one request.
// They are not accepted from the terminal payload. The clock participates in
// the durable request hash, while committed receipt replay never calls Roll.
type SearchOptions struct {
	Now  int32
	Roll func(int, int) int
}

type searchLineRequest struct {
	Kind string `json:"kind"`
	Line string `json:"line"`
	Now  int32  `json:"now"`
}

// ParseSearchLine recognizes only exact bare 검색/찾아 aliases. Keeping this
// boundary line-oriented prevents control bytes and accidental free-form
// target names from entering a durable receipt.
func ParseSearchLine(line string) (SearchCommand, bool) {
	if !utf8.ValidString(line) {
		return SearchCommand{}, false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\r' || r == '\n' || r == '\x00' {
			return SearchCommand{}, false
		}
	}
	trimmed := strings.TrimSpace(line)
	if trimmed != "검색" && trimmed != "찾아" {
		return SearchCommand{}, false
	}
	return SearchCommand{}, true
}

func IsSearchLine(line string) bool {
	_, ok := ParseSearchLine(line)
	return ok
}

// ExecuteSearchLine uses the explicit host clock/random arguments used by
// unit tests and callers that do not need additional options.
func (o *Ownership) ExecuteSearchLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32, roll func(int, int) int) (storage.WorldReceipt, error) {
	return o.ExecuteSearchLineWithOptions(ctx, store, worldID, commandID, lease, line, SearchOptions{Now: now, Roll: roll})
}

// ExecuteSearchLineWithOptions connects PlanSearch/ApplySearch to the durable
// command receipt. The response is a SearchResult envelope rather than a
// plain string because the committed room event needs receipt-persisted
// target IDs; transport unwraps Response for the actor and uses Targets only
// on the first, non-replayed receipt.
func (o *Ownership) ExecuteSearchLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options SearchOptions) (storage.WorldReceipt, error) {
	if _, ok := ParseSearchLine(line); !ok {
		return storage.WorldReceipt{}, ErrUnsupportedSearchLine
	}
	payload, err := json.Marshal(searchLineRequest{Kind: "search", Line: line, Now: options.Now})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		s, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := s.PlanSearch(actorID, options.Now, options.Roll)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := s.ApplySearch(proposal)
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
