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

// ErrUnsupportedChangeClassLine is returned before a receipt for command7.c
// cmdno 86 forms outside the bounded bare/prompt and one-line confirmation
// forms.
var ErrUnsupportedChangeClassLine = errors.New("line is not an implemented change class command")

// ChangeClassCancelResponse mirrors change_class_ok's non-affirmative
// continuation. The connection-local prompt consumes this response before
// ordinary command parsing, so cancellation never creates a world receipt.
const ChangeClassCancelResponse = "직업전환이 되지 않았습니다"

// ChangeClassRetryResponse is returned when the durable confirmation may have
// committed but the store/response was inconclusive. The draft keeps its
// command ID so the next exact confirmation is idempotent.
const ChangeClassRetryResponse = "직업전환을 저장하지 못했습니다. 다시 시도해 주세요.\r\n"

// ChangeClassCommand carries only parser-owned confirmation state.  Actor
// identity and every mutable value are derived from the authenticated lease
// and committed world snapshot.
type ChangeClassCommand struct {
	Alias     string
	Confirmed bool
}

// ChangeClassOptions intentionally has no client-controlled progression data.
// It leaves room for a future canonical continuation token without widening
// the current one-line reducer.
type ChangeClassOptions struct{}

type ClassChangeOptions = ChangeClassOptions
type ChangeClassCommandOptions = ChangeClassOptions

type changeClassLineRequest struct {
	Kind      string `json:"kind"`
	Line      string `json:"line"`
	Alias     string `json:"alias"`
	Confirmed bool   `json:"confirmed"`
}

func validChangeClassLine(line string) bool {
	if !utf8.ValidString(line) {
		return false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\r' || r == '\n' || r == '\x00' || r == '\x1b' || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

// ParseChangeClassLine recognizes the original Korean alias and the explicit
// bounded confirmation form `직업전환 예`.  Bare `직업전환` is admitted as a
// prompt/no-op candidate because the source asks for an interactive
// continuation; a standalone continuation is not canonical in this slice.
// Other confirmations, aliases and arguments remain unsupported.
func ParseChangeClassLine(line string) (ChangeClassCommand, bool) {
	if !validChangeClassLine(line) {
		return ChangeClassCommand{}, false
	}
	// Keep the confirmation form one-line and exact. C's continuation uses
	// strncmp, but this bounded adapter intentionally does not broaden it to
	// arbitrary whitespace/tokenization; outer terminal whitespace remains
	// harmless as with the other exact command aliases.
	switch strings.TrimSpace(line) {
	case "직업전환":
		return ChangeClassCommand{Alias: "직업전환"}, true
	case "직업전환 예":
		return ChangeClassCommand{Alias: "직업전환", Confirmed: true}, true
	}
	return ChangeClassCommand{}, false
}

// ParseChangeClassStartLine recognizes only the bare interactive entry point.
// The connection-local continuation owns the following 예/아니오 line and
// crosses the durable receipt boundary only after an affirmative response.
func ParseChangeClassStartLine(line string) bool {
	command, ok := ParseChangeClassLine(line)
	return ok && !command.Confirmed
}

func IsChangeClassStartLine(line string) bool { return ParseChangeClassStartLine(line) }

func IsChangeClassLine(line string) bool {
	_, ok := ParseChangeClassLine(line)
	return ok
}

func ParseClassChangeLine(line string) (ChangeClassCommand, bool) { return ParseChangeClassLine(line) }
func IsClassChangeLine(line string) bool                          { return IsChangeClassLine(line) }

// ExecuteChangeClassLine applies the source reducer through the authenticated
// session and durable receipt boundary. A replay returns the stored typed
// result without re-running level-down or any future random source.
func (o *Ownership) ExecuteChangeClassLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	return o.ExecuteChangeClassLineWithOptions(ctx, store, worldID, commandID, lease, line, ChangeClassOptions{})
}

func (o *Ownership) ExecuteChangeClassLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, _ ChangeClassOptions) (storage.WorldReceipt, error) {
	command, ok := ParseChangeClassLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedChangeClassLine
	}
	request, err := json.Marshal(changeClassLineRequest{Kind: "change_class", Line: line, Alias: command.Alias, Confirmed: command.Confirmed})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, request, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanChangeClass(actorID, command.Confirmed)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyChangeClass(proposal)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		if err != nil {
			return nil, nil, err
		}
		// Bare `직업전환` is a typed no-op. Keep the loaded bytes exactly as
		// they were so a prompt receipt cannot rewrite unrelated state.
		if !result.Changed {
			return raw, response, nil
		}
		nextRaw, err := json.Marshal(next)
		return nextRaw, response, err
	})
}

func (o *Ownership) ExecuteClassChangeLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	return o.ExecuteChangeClassLine(ctx, store, worldID, commandID, lease, line)
}

func (o *Ownership) ExecuteClassChangeLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options ClassChangeOptions) (storage.WorldReceipt, error) {
	return o.ExecuteChangeClassLineWithOptions(ctx, store, worldID, commandID, lease, line, options)
}

func (o *Ownership) ExecuteChangeClassCommandLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	return o.ExecuteChangeClassLine(ctx, store, worldID, commandID, lease, line)
}
