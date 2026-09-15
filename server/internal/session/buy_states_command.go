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

var ErrUnsupportedBuyStatesLine = errors.New("line is not an implemented buy-states command")

// BuyStatesCommand is the parser-facing form of command11.c:buy_states.
// C parse() takes the last token as the verb, so prefix `향상 체력` is not
// this command. This slice admits the bare prompt and a single suffix
// selector; the world reducer owns all state and apply gates.
type BuyStatesCommand struct {
	Stat string
}

// BuyStatesOptions supplies a server-owned random source for the seven
// admitted C apply branches. The terminal line never supplies or controls it;
// a nil Roll preserves the old fail-closed first-gate behavior.
type BuyStatesOptions struct {
	Roll func(int, int) int
}

type buyStatesLineRequest struct {
	Kind string `json:"kind"`
	Line string `json:"line"`
	Stat string `json:"stat,omitempty"`
}

func validBuyStatesLine(line string) bool {
	if !utf8.ValidString(line) {
		return false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' || (unicode.IsSpace(r) && r != ' ') {
			return false
		}
	}
	return true
}

// ParseBuyStatesLine admits the original suffix form:
//
//	향상
//	<stat> 향상
//
// Prefix `향상 체력` is not this command.
func ParseBuyStatesLine(line string) (BuyStatesCommand, bool) {
	if !validBuyStatesLine(line) {
		return BuyStatesCommand{}, false
	}
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || len(tokens) == 0 || tokens[len(tokens)-1] != "향상" {
		return BuyStatesCommand{}, false
	}
	rest := tokens[:len(tokens)-1]
	switch len(rest) {
	case 0:
		return BuyStatesCommand{}, true
	case 1:
		stat := rest[0]
		if stat == "" || strings.TrimSpace(stat) != stat {
			return BuyStatesCommand{}, false
		}
		return BuyStatesCommand{Stat: stat}, true
	default:
		return BuyStatesCommand{}, false
	}
}

func IsBuyStatesLine(line string) bool {
	_, ok := ParseBuyStatesLine(line)
	return ok
}

// ExecuteBuyStatesLine keeps the existing no-RNG API and therefore remains
// fail-closed when a selected branch would need random state.
func (o *Ownership) ExecuteBuyStatesLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	return o.ExecuteBuyStatesLineWithOptions(ctx, store, worldID, commandID, lease, line, BuyStatesOptions{})
}

// ExecuteBuyStatesLineWithOptions binds a server-owned Roll to the world
// planner. ExecuteGame checks the durable command receipt before this reducer,
// so a command-ID replay returns the recorded rolls/result without invoking
// the reducer or Roll again.
func (o *Ownership) ExecuteBuyStatesLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options BuyStatesOptions) (storage.WorldReceipt, error) {
	command, ok := ParseBuyStatesLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedBuyStatesLine
	}
	payload, err := json.Marshal(buyStatesLineRequest{Kind: "buy-states", Line: line, Stat: command.Stat})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanBuyStatesWithOptions(actorID, command.Stat, world.BuyStatesOptions{Roll: options.Roll})
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyBuyStates(proposal)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		if err != nil {
			return nil, nil, err
		}
		if !result.Changed {
			return raw, response, nil
		}
		nextRaw, err := json.Marshal(next)
		return nextRaw, response, err
	})
}

// CommandLine spellings mirror the other session adapters without changing
// parser or transport wiring in this bounded slice.
func (o *Ownership) ExecuteBuyStatesCommandLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	return o.ExecuteBuyStatesLine(ctx, store, worldID, commandID, lease, line)
}

func (o *Ownership) ExecuteBuyStatesCommandLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options BuyStatesOptions) (storage.WorldReceipt, error) {
	return o.ExecuteBuyStatesLineWithOptions(ctx, store, worldID, commandID, lease, line, options)
}
