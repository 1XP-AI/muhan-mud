package session

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

var ErrUnsupportedCastLine = errors.New("line is not an implemented cast command")

// CastCommand keeps the original Korean command boundary separate from the
// source spell index. An empty spell is the original `주문` prompt. A third
// token is admitted only when the spell uniquely resolves to a migrated
// targeted branch.
type CastCommand struct {
	SpellName string
	Target    string
	// Occurrence is the one-based target occurrence carried by the legacy
	// parser's val[2]. It is populated only for targeted forms; the existing
	// three-token form defaults to the first matching player.
	Occurrence int
}

type castLineRequest struct {
	Kind       string `json:"kind"`
	Line       string `json:"line"`
	SpellName  string `json:"spell_name"`
	Target     string `json:"target,omitempty"`
	Occurrence int    `json:"occurrence,omitempty"`
	Now        int32  `json:"now"`
	Hour       int    `json:"hour"`
}

func parseCastOccurrence(token string) (int, bool) {
	if token == "" {
		return 0, false
	}
	for _, r := range token {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	value, err := strconv.ParseUint(token, 10, 31)
	if err != nil || value < 1 {
		return 0, false
	}
	return int(value), true
}

func castTargetLooksLikeOccurrence(token string) bool {
	if token == "" {
		return false
	}
	if token[0] == '+' || token[0] == '-' {
		token = token[1:]
	}
	if token == "" {
		return false
	}
	for _, r := range token {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

type CastOptions = world.CastOptions

func validCastLine(line string) bool {
	if !utf8.ValidString(line) {
		return false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' || r == '\x00' {
			return false
		}
	}
	return true
}

// ParseCastLine admits the original `주문` prompt, its self-target form
// `주문 <주문명>`, and the targeted forms `주문 천리안 <name>` /
// `주문 소환 <name>` / `주문 귀환 <name>`. Other three-token casts stay
// rejected so they cannot reach an unimplemented reducer.
func ParseCastLine(line string) (CastCommand, bool) {
	if !validCastLine(line) {
		return CastCommand{}, false
	}
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || len(tokens) < 1 || tokens[0] != "주문" {
		return CastCommand{}, false
	}
	if len(tokens) == 1 {
		return CastCommand{}, true
	}
	if tokens[1] == "" || strings.TrimSpace(tokens[1]) != tokens[1] {
		return CastCommand{}, false
	}
	if len(tokens) == 2 {
		return CastCommand{SpellName: tokens[1]}, true
	}
	if len(tokens) == 3 {
		if tokens[2] == "" || strings.TrimSpace(tokens[2]) != tokens[2] || (!world.IsTargetedCastSpell(tokens[1]) && !world.IsRecallCastSpell(tokens[1])) {
			return CastCommand{}, false
		}
		if world.IsRecallCastSpell(tokens[1]) && strings.IndexFunc(tokens[2], unicode.IsSpace) >= 0 {
			return CastCommand{}, false
		}
		// A numeric-only third token after 귀환 is the legacy val[2] with
		// its target omitted, not a player display name. Reject it before
		// ExecuteGame can create a deterministic missing-target receipt.
		if world.IsRecallCastSpell(tokens[1]) && castTargetLooksLikeOccurrence(tokens[2]) {
			return CastCommand{}, false
		}
		return CastCommand{SpellName: tokens[1], Target: tokens[2], Occurrence: 1}, true
	}
	if len(tokens) != 4 || !world.IsRecallCastSpell(tokens[1]) || tokens[2] == "" || strings.TrimSpace(tokens[2]) != tokens[2] || strings.IndexFunc(tokens[2], unicode.IsSpace) >= 0 || castTargetLooksLikeOccurrence(tokens[2]) {
		return CastCommand{}, false
	}
	occurrence, ok := parseCastOccurrence(tokens[3])
	if !ok {
		return CastCommand{}, false
	}
	return CastCommand{SpellName: tokens[1], Target: tokens[2], Occurrence: occurrence}, true
}

func IsCastLine(line string) bool {
	_, ok := ParseCastLine(line)
	return ok
}

// ExecuteCastLineWithOptions binds cast()'s self-target reducer to a durable
// receipt.  Roll is consumed only by the first reducer invocation; engine
// replay returns the stored CastResult unchanged.
func (o *Ownership) ExecuteCastLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options CastOptions) (storage.WorldReceipt, error) {
	command, ok := ParseCastLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedCastLine
	}
	options.Target = command.Target
	options.Occurrence = command.Occurrence
	payload, err := json.Marshal(castLineRequest{Kind: "cast", Line: line, SpellName: command.SpellName, Target: command.Target, Occurrence: command.Occurrence, Now: options.Now, Hour: options.Hour})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanCast(actorID, command.SpellName, options)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyCast(proposal)
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

func (o *Ownership) ExecuteCastLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32, hour int, roll func(int, int) int) (storage.WorldReceipt, error) {
	return o.ExecuteCastLineWithOptions(ctx, store, worldID, commandID, lease, line, CastOptions{Now: now, Hour: hour, Roll: roll})
}
