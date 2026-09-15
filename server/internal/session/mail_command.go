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

var (
	// ErrUnsupportedMailLine is returned before a receipt is created for any
	// line outside the bounded read/delete command surface.
	ErrUnsupportedMailLine = errors.New("line is not an implemented mail command")
	// ErrUnsupportedMailSendLine makes the intentionally omitted interactive
	// compose flow observable to a caller while remaining errors.Is-compatible
	// with ErrUnsupportedMailLine.
	ErrUnsupportedMailSendLine = errors.New("interactive mail sending is not implemented")
)

// MailCommand is the parser-facing closed command vocabulary. Compose/send is
// not returned by ParseMailLine; ExecuteMailLine still identifies the exact
// source alias so it can report the explicit unsupported boundary.
type MailCommand struct {
	Action world.MailAction `json:"action"`
	Alias  string           `json:"alias"`
}

type mailLineRequest struct {
	Kind   string           `json:"kind"`
	Line   string           `json:"line"`
	Action world.MailAction `json:"action"`
	Alias  string           `json:"alias"`
}

func validMailCommandLine(line string) bool {
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

// ParseMailLine admits only the exact bare Korean read/delete aliases from
// src/global.c. Outer spaces are accepted by the terminal command boundary;
// arguments and control/injected input are rejected.
func ParseMailLine(line string) (MailCommand, bool) {
	if !validMailCommandLine(line) {
		return MailCommand{}, false
	}
	switch alias := strings.TrimSpace(line); alias {
	case "편지받기":
		return MailCommand{Action: world.MailRead, Alias: alias}, true
	case "편지삭제":
		return MailCommand{Action: world.MailDelete, Alias: alias}, true
	default:
		return MailCommand{}, false
	}
}

// IsMailLine reports whether a line is one of the implemented mail aliases.
// In particular, 편지보내기 intentionally returns false because compose is
// an interactive continuation protocol that this receipt slice does not own.
func IsMailLine(line string) bool {
	_, ok := ParseMailLine(line)
	return ok
}

func unsupportedMailSendError() error {
	return fmt.Errorf("%w: %w", ErrUnsupportedMailLine, ErrUnsupportedMailSendLine)
}

// ExecuteMailLine connects the pure mailbox reducer to the durable command
// receipt. Read is a no-state-change receipt; delete clears the complete
// recipient mailbox in the same candidate as its typed actor response. The
// engine checks an existing receipt before invoking this reducer, so a replay
// cannot delete or alter a mailbox a second time.
func (o *Ownership) ExecuteMailLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	trimmed := strings.TrimSpace(line)
	if validMailCommandLine(line) && trimmed == "편지보내기" {
		return storage.WorldReceipt{}, unsupportedMailSendError()
	}
	command, ok := ParseMailLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedMailLine
	}
	request, err := json.Marshal(mailLineRequest{Kind: "mail", Line: line, Action: command.Action, Alias: command.Alias})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, request, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		var next world.State
		var result world.MailResult
		switch command.Action {
		case world.MailRead:
			next, result, err = state.ApplyMailRead(actorID)
			if err != nil {
				return nil, nil, err
			}
			// Preserve the original bytes for a read-only command. Re-marshaling
			// would create a semantic no-op but could alter snapshot bytes/order.
			response, marshalErr := json.Marshal(result)
			return raw, response, marshalErr
		case world.MailDelete:
			next, result, err = state.ApplyMailDelete(actorID)
			if err != nil {
				return nil, nil, err
			}
		default:
			return nil, nil, fmt.Errorf("unsupported mail action")
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

// ExecutePostLine is an explicit compatibility spelling for callers that use
// the legacy post.c vocabulary. It shares the same bounded implementation and
// does not add any compose/send behavior.
func (o *Ownership) ExecutePostLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	return o.ExecuteMailLine(ctx, store, worldID, commandID, lease, line)
}
