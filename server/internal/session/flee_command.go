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

var ErrUnsupportedFleeLine = errors.New("line is not an implemented flee command")

type FleeCommand struct{}

type FleeOptions struct {
	Now      int32
	Hour     int
	Roll     func(int, int) int
	Catalog  world.SpawnCatalog
	Allocate func() (string, error)
}

type fleeLineRequest struct {
	Kind string `json:"kind"`
	Line string `json:"line"`
	Now  int32  `json:"now"`
	Hour int    `json:"hour"`
}

// ParseFleeLine admits only the original bare player aliases. Targeted and
// prefixed forms remain unsupported until their legacy authority is ported.
func ParseFleeLine(line string) (FleeCommand, bool) {
	if !utf8.ValidString(line) {
		return FleeCommand{}, false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\r' || r == '\n' || r == '\x00' {
			return FleeCommand{}, false
		}
	}
	switch strings.TrimSpace(line) {
	case "도망", "도":
		return FleeCommand{}, true
	default:
		return FleeCommand{}, false
	}
}

func IsFleeLine(line string) bool {
	_, ok := ParseFleeLine(line)
	return ok
}

func (o *Ownership) ExecuteFleeLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32, hour int, roll func(int, int) int, catalog world.SpawnCatalog, allocate func() (string, error)) (storage.WorldReceipt, error) {
	return o.ExecuteFleeLineWithOptions(ctx, store, worldID, commandID, lease, line, FleeOptions{Now: now, Hour: hour, Roll: roll, Catalog: catalog, Allocate: allocate})
}

func (o *Ownership) ExecuteFleeLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options FleeOptions) (storage.WorldReceipt, error) {
	if _, ok := ParseFleeLine(line); !ok {
		return storage.WorldReceipt{}, ErrUnsupportedFleeLine
	}
	payload, err := json.Marshal(fleeLineRequest{Kind: "flee", Line: line, Now: options.Now, Hour: options.Hour})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		s, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := s.PlanFlee(actorID, options.Now, options.Hour, options.Roll, options.Catalog, options.Allocate)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := s.ApplyFlee(proposal)
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
