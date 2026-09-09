package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

// ErrUnsupportedMemoLine is returned before a receipt exists for a line that
// is not exactly the bounded memo command. In particular, the parser never
// accepts a client-supplied path or credential continuation.
var ErrUnsupportedMemoLine = errors.New("line is not an implemented memo command")

// MemoCommand is the closed parser result for `메모 <character> <body>`. The
// target remains a display-name selector until the world reducer resolves it
// against canonical player identity; no client-owned player ID is accepted.
type MemoCommand struct {
	Target string `json:"target"`
	Body   string `json:"body"`
}

// memoLineRequest is deliberately free of raw paths and passwords. The
// optional clock field is only included by the explicit-options API; the
// convenience API keeps its request hash stable across receipt retries while
// still binding its host-owned timestamp in the reducer closure.
type memoLineRequest struct {
	Kind   string     `json:"kind"`
	Line   string     `json:"line"`
	Target string     `json:"target"`
	Body   string     `json:"body"`
	Now    *time.Time `json:"now,omitempty"`
}

func validMemoLine(line string) bool {
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

// ParseMemoLine recognizes the exact two-argument command shape. The target
// is one whitespace-delimited character token; the remainder is retained as
// one body so ordinary spaces in a memo are not lost by the central tokenizer.
// World validation owns the UTF-8/length/control and canonical-identity
// policy, while this parser rejects malformed command boundaries early.
func ParseMemoLine(line string) (MemoCommand, bool) {
	if !validMemoLine(line) {
		return MemoCommand{}, false
	}
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return MemoCommand{}, false
	}
	aliasEnd := strings.IndexFunc(trimmed, unicode.IsSpace)
	if aliasEnd < 0 || trimmed[:aliasEnd] != "메모" {
		return MemoCommand{}, false
	}
	remainder := strings.TrimLeftFunc(trimmed[aliasEnd:], unicode.IsSpace)
	if remainder == "" {
		return MemoCommand{}, false
	}
	targetEnd := strings.IndexFunc(remainder, unicode.IsSpace)
	if targetEnd < 0 {
		return MemoCommand{}, false
	}
	target := remainder[:targetEnd]
	body := strings.TrimSpace(remainder[targetEnd:])
	if target == "" || body == "" {
		return MemoCommand{}, false
	}
	if err := world.ValidateMemoTargetName(target); err != nil {
		return MemoCommand{}, false
	}
	if err := world.ValidateMemoBody(body); err != nil {
		return MemoCommand{}, false
	}
	return MemoCommand{Target: target, Body: body}, true
}

func IsMemoLine(line string) bool {
	_, ok := ParseMemoLine(line)
	return ok
}

// ExecuteMemoLine binds the convenience command to a host-owned wall clock.
// The timestamp is not part of the request identity, so repeating the same
// command ID and line after a lost response still recovers the durable receipt
// instead of conflicting on a newly sampled clock value.
func (o *Ownership) ExecuteMemoLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	command, ok := ParseMemoLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedMemoLine
	}
	return o.executeMemoLine(ctx, store, worldID, commandID, lease, line, command, world.MemoOptions{Now: time.Now().UTC()}, false)
}

// ExecuteMemoLineWithOptions is the deterministic form used by connectors,
// tests and recovery callers. Its explicit timestamp participates in the
// receipt request hash and must be reused when a commit outcome is uncertain.
func (o *Ownership) ExecuteMemoLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options world.MemoOptions) (storage.WorldReceipt, error) {
	command, ok := ParseMemoLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedMemoLine
	}
	return o.executeMemoLine(ctx, store, worldID, commandID, lease, line, command, options, true)
}

func (o *Ownership) executeMemoLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, command MemoCommand, options world.MemoOptions, includeNow bool) (storage.WorldReceipt, error) {
	request := memoLineRequest{Kind: "memo", Line: line, Target: command.Target, Body: command.Body}
	if includeNow {
		now := options.Now
		request.Now = &now
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		options.ID = commandID
		proposal, err := state.PlanMemoWithOptions(actorID, command.Target, command.Body, options)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyMemo(proposal)
		if err != nil {
			return nil, nil, err
		}
		stateBytes, err := json.Marshal(next)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		if err != nil {
			return nil, nil, err
		}
		return stateBytes, response, nil
	})
}

// ExecuteMemoCommandLine is a source-neutral compatibility spelling.
func (o *Ownership) ExecuteMemoCommandLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	return o.ExecuteMemoLine(ctx, store, worldID, commandID, lease, line)
}

func (o *Ownership) ExecuteMemoCommandLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options world.MemoOptions) (storage.WorldReceipt, error) {
	return o.ExecuteMemoLineWithOptions(ctx, store, worldID, commandID, lease, line, options)
}
