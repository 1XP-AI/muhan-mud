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
	ErrUnsupportedNotepadLine            = errors.New("line is not an implemented notepad command")
	ErrNotepadAppendContinuationRequired = world.ErrNotepadAppendContinuation
)

const (
	NotepadAppendInvalidLineResponse = "메모 내용을 확인해 주세요.\n->"
	NotepadAppendRetryResponse       = "메모를 저장하지 못했습니다. 다시 시도해 주세요.\n->"
)

// NotepadCommand is the exact command-table projection of post.c:notepad.
// More than one option token follows C's cmnd->num != 2 view branch; only the
// one-token `a`/`d` branches select a mutation.
type NotepadCommand struct {
	Verb   string
	Action world.NotepadAction
	Option string
}

type notepadLineRequest struct {
	Kind   string              `json:"kind"`
	Line   string              `json:"line"`
	Verb   string              `json:"verb"`
	Action world.NotepadAction `json:"action"`
	Option string              `json:"option,omitempty"`
}

type notepadAppendRequest struct {
	Kind  string   `json:"kind"`
	Verb  string   `json:"verb"`
	Lines []string `json:"lines"`
}

func validNotepadCommandLine(line string) bool {
	if !utf8.ValidString(line) {
		return false
	}
	for _, r := range line {
		if unicode.IsControl(r) || (unicode.IsSpace(r) && r != ' ') || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

func notepadTokens(line string) []string {
	parts := strings.Split(strings.TrimSpace(line), " ")
	tokens := parts[:0]
	for _, part := range parts {
		if part != "" {
			tokens = append(tokens, part)
		}
	}
	return tokens
}

func notepadOptionAction(option string) world.NotepadAction {
	if option == "" {
		return world.NotepadView
	}
	r, _ := utf8.DecodeRuneInString(option)
	if r >= 'A' && r <= 'Z' {
		r += 'a' - 'A'
	}
	switch r {
	case 'a':
		return world.NotepadAppend
	case 'd':
		return world.NotepadClear
	default:
		return world.NotepadInvalid
	}
}

// ParseNotepadLine recognizes only the registered aliases. The option's
// first ASCII character follows low(cmnd->str[1][0]); extra tokens fall
// through to the source's bare-view branch.
func ParseNotepadLine(line string) (NotepadCommand, bool) {
	if !validNotepadCommandLine(line) || strings.ContainsAny(line, "'\"\\") {
		return NotepadCommand{}, false
	}
	tokens := notepadTokens(line)
	if len(tokens) == 0 || len(tokens) > 7 {
		return NotepadCommand{}, false
	}
	if tokens[0] != "*notepad" && tokens[0] != "*메모" {
		return NotepadCommand{}, false
	}
	command := NotepadCommand{Verb: tokens[0], Action: world.NotepadView}
	if len(tokens) == 2 {
		command.Option = tokens[1]
		command.Action = notepadOptionAction(tokens[1])
	}
	return command, true
}

func IsNotepadLine(line string) bool {
	_, ok := ParseNotepadLine(line)
	return ok
}

func ParseNotepadAppendStartLine(line string) bool {
	command, ok := ParseNotepadLine(line)
	return ok && command.Action == world.NotepadAppend
}

func IsNotepadAppendStartLine(line string) bool {
	return ParseNotepadAppendStartLine(line)
}

// ExecuteNotepadLine connects view, clear and invalid-option branches to one
// durable receipt. The `a` branch remains connection-local until its editor
// submits the complete buffer at ExecuteNotepadAppend's boundary.
func (o *Ownership) ExecuteNotepadLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	command, ok := ParseNotepadLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedNotepadLine
	}
	if command.Action == world.NotepadAppend {
		return storage.WorldReceipt{}, ErrNotepadAppendContinuationRequired
	}
	request, err := json.Marshal(notepadLineRequest{
		Kind: "notepad", Line: line, Verb: command.Verb, Action: command.Action, Option: command.Option,
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, request, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanNotepad(actorID, command.Verb, command.Option)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyNotepad(proposal)
		if err != nil {
			return nil, nil, err
		}
		stateBytes := raw
		if result.Changed {
			stateBytes, err = json.Marshal(next)
			if err != nil {
				return nil, nil, err
			}
		}
		response, err := json.Marshal(result)
		return stateBytes, response, err
	})
}

func canonicalNotepadLines(lines []string) ([]string, error) {
	canonical := make([]string, len(lines))
	for i, line := range lines {
		if err := world.ValidateNotepadLine(line); err != nil {
			return nil, err
		}
		canonical[i] = world.TruncateNotepadLine(line)
	}
	return canonical, nil
}

// ExecuteNotepadAppend commits the full connection-local editor buffer. The
// caller must reuse commandID and the exact lines when the commit outcome is
// uncertain; the durable request then remains replayable without a second
// append.
func (o *Ownership) ExecuteNotepadAppend(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, lines []string) (storage.WorldReceipt, error) {
	return o.ExecuteNotepadAppendWithVerb(ctx, store, worldID, commandID, lease, "*notepad", lines)
}

// ExecuteNotepadAppendWithVerb retains the exact alias used to start the
// editor, making the durable request identity stable for both aliases.
func (o *Ownership) ExecuteNotepadAppendWithVerb(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, verb string, lines []string) (storage.WorldReceipt, error) {
	if verb != "*notepad" && verb != "*메모" {
		return storage.WorldReceipt{}, ErrUnsupportedNotepadLine
	}
	canonical, err := canonicalNotepadLines(lines)
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	request, err := json.Marshal(notepadAppendRequest{Kind: "notepad-append", Verb: verb, Lines: canonical})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, request, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanNotepadAppendWithVerb(actorID, verb, canonical)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyNotepad(proposal)
		if err != nil {
			return nil, nil, err
		}
		stateBytes := raw
		if result.Changed {
			stateBytes, err = json.Marshal(next)
			if err != nil {
				return nil, nil, err
			}
		}
		response, err := json.Marshal(result)
		return stateBytes, response, err
	})
}

// ExecuteNotepadAppendLine is the single-line compatibility spelling. Live
// terminal continuations should prefer ExecuteNotepadAppend with the complete
// connection-local buffer.
func (o *Ownership) ExecuteNotepadAppendLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	return o.ExecuteNotepadAppend(ctx, store, worldID, commandID, lease, []string{line})
}

func (o *Ownership) ExecuteNotepadCommandLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	return o.ExecuteNotepadLine(ctx, store, worldID, commandID, lease, line)
}
