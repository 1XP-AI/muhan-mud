package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

var ErrUnsupportedDMCharmLine = errors.New("line is not an implemented DM charm command")

// DMCharmCommand is the bounded suffix form of dm6.c:list_charm:
// `<player> *charm` / `<player> *최면`. The bare aliases are admitted so the
// world reducer can preserve C's explicit missing-target boundary.
type DMCharmCommand struct {
	Verb   string
	Target string
}

type dmCharmLineRequest struct {
	Kind   string `json:"kind"`
	Line   string `json:"line"`
	Verb   string `json:"verb"`
	Target string `json:"target,omitempty"`
}

func validDMCharmLine(line string) bool {
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

func ParseDMCharmLine(line string) (DMCharmCommand, bool) {
	if !validDMCharmLine(line) {
		return DMCharmCommand{}, false
	}
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.ContainsAny(trimmed, "'\\\"") {
		return DMCharmCommand{}, false
	}
	tokens := strings.Split(trimmed, " ")
	compact := tokens[:0]
	for _, token := range tokens {
		if token != "" {
			compact = append(compact, token)
		}
	}
	if len(compact) < 1 || len(compact) > 2 {
		return DMCharmCommand{}, false
	}
	verb := compact[len(compact)-1]
	if verb != "*charm" && verb != "*최면" {
		return DMCharmCommand{}, false
	}
	command := DMCharmCommand{Verb: verb}
	if len(compact) == 2 {
		command.Target = compact[0]
		if command.Target == "" || strings.TrimSpace(command.Target) != command.Target || strings.IndexFunc(command.Target, unicode.IsSpace) >= 0 {
			return DMCharmCommand{}, false
		}
	}
	return command, true
}

func IsDMCharmLine(line string) bool {
	_, ok := ParseDMCharmLine(line)
	return ok
}

func (o *Ownership) ExecuteDMCharmLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	command, ok := ParseDMCharmLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedDMCharmLine
	}
	payload, err := json.Marshal(dmCharmLineRequest{
		Kind: "dm-charm", Line: line, Verb: command.Verb, Target: command.Target,
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, fmt.Errorf("%w: %v", world.ErrDMCharmStateInvalid, err)
		}
		proposal, err := state.PlanDMCharm(actorID, command.Verb, command.Target)
		if err != nil {
			return nil, nil, err
		}
		_, result, err := state.ApplyDMCharm(proposal)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		return raw, response, err
	})
}
