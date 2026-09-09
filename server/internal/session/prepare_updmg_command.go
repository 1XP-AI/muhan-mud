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
	ErrUnsupportedPrepareLine = errors.New("line is not an implemented bare prepare command")
	ErrUnsupportedUpDmgLine   = errors.New("line is not an implemented bare up_dmg command")
)

// PrepareCommand and UpDmgCommand retain the exact admitted alias for receipt
// identity while exposing no client-controlled actor or state fields.
type PrepareCommand struct{ Alias string }
type UpDmgCommand struct{ Alias string }

type PrepareOptions struct{ Now int32 }
type UpDmgOptions struct {
	Now  int32
	Roll func(int, int) int
}

type prepareUpDmgLineRequest struct {
	Kind  string `json:"kind"`
	Line  string `json:"line"`
	Alias string `json:"alias"`
	Now   int32  `json:"now"`
}

func validBarePrepareUpDmgLine(line string) bool {
	if !utf8.ValidString(line) {
		return false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\r' || r == '\n' || r == '\x00' || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

// ParsePrepareLine admits only the source's bare Korean command.  In
// particular, quoted or whitespace-separated extra tokens are not silently
// passed to a world reducer.
func ParsePrepareLine(line string) (PrepareCommand, bool) {
	if !validBarePrepareUpDmgLine(line) {
		return PrepareCommand{}, false
	}
	switch strings.TrimSpace(line) {
	case "경계":
		return PrepareCommand{Alias: "경계"}, true
	default:
		return PrepareCommand{}, false
	}
}

func IsPrepareLine(line string) bool {
	_, ok := ParsePrepareLine(line)
	return ok
}

// ParseUpDmgLine admits only the exact bare 잠력격발 alias.  Prefixes,
// suffixes and target/option tokens are outside this bounded command lane.
func ParseUpDmgLine(line string) (UpDmgCommand, bool) {
	if !validBarePrepareUpDmgLine(line) {
		return UpDmgCommand{}, false
	}
	switch strings.TrimSpace(line) {
	case "잠력격발":
		return UpDmgCommand{Alias: "잠력격발"}, true
	default:
		return UpDmgCommand{}, false
	}
}

func IsUpDmgLine(line string) bool {
	_, ok := ParseUpDmgLine(line)
	return ok
}

// Spelling aliases keep the source function name and prose name on one
// durable implementation.
func ParsePreparationLine(line string) (PrepareCommand, bool) { return ParsePrepareLine(line) }
func IsPreparationLine(line string) bool                      { return IsPrepareLine(line) }
func ParseUpDamageLine(line string) (UpDmgCommand, bool)      { return ParseUpDmgLine(line) }
func IsUpDamageLine(line string) bool                         { return IsUpDmgLine(line) }

// ExecutePrepareLine binds the injected clock to the authenticated session
// and stores the typed world result in ExecuteGame's receipt.  A replay reads
// that receipt before this reducer and therefore never re-evaluates the room
// event or state transition.
func (o *Ownership) ExecutePrepareLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32) (storage.WorldReceipt, error) {
	return o.ExecutePrepareLineWithOptions(ctx, store, worldID, commandID, lease, line, PrepareOptions{Now: now})
}

func (o *Ownership) ExecutePrepareLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options PrepareOptions) (storage.WorldReceipt, error) {
	command, ok := ParsePrepareLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedPrepareLine
	}
	payload, err := json.Marshal(prepareUpDmgLineRequest{Kind: "prepare", Line: line, Alias: command.Alias, Now: options.Now})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanPrepare(actorID, options.Now)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyPrepare(proposal)
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

// ExecuteUpDmgLine binds one server-owned roll source to up_dmg planning. The
// roll is called only on an eligible first execution; ExecuteGame receipt
// recovery handles retries without rerolling.
func (o *Ownership) ExecuteUpDmgLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32, roll func(int, int) int) (storage.WorldReceipt, error) {
	return o.ExecuteUpDmgLineWithOptions(ctx, store, worldID, commandID, lease, line, UpDmgOptions{Now: now, Roll: roll})
}

func (o *Ownership) ExecuteUpDmgLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options UpDmgOptions) (storage.WorldReceipt, error) {
	command, ok := ParseUpDmgLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedUpDmgLine
	}
	payload, err := json.Marshal(prepareUpDmgLineRequest{Kind: "up_dmg", Line: line, Alias: command.Alias, Now: options.Now})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanUpDmg(actorID, options.Now, options.Roll)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyUpDmg(proposal)
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

func (o *Ownership) ExecutePreparationLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32) (storage.WorldReceipt, error) {
	return o.ExecutePrepareLine(ctx, store, worldID, commandID, lease, line, now)
}

func (o *Ownership) ExecuteUpDamageLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32, roll func(int, int) int) (storage.WorldReceipt, error) {
	return o.ExecuteUpDmgLine(ctx, store, worldID, commandID, lease, line, now, roll)
}
