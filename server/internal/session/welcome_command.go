package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"unicode/utf8"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

// ErrUnsupportedWelcomeLine is returned before a receipt is created when the
// line is not the implemented legacy welcome command or includes arguments.
// The command is intentionally kept separate from the login welcome flow: it
// is a post-admission, document-backed read-only projection.
var ErrUnsupportedWelcomeLine = errors.New("line is not an implemented welcome command")

// ErrWelcomeDocument is returned when the immutable welcome document source
// cannot provide a valid UTF-8 document. A missing or malformed source must
// fail closed rather than committing a partial or guessed response.
var ErrWelcomeDocument = errors.New("welcome document is unavailable")

type welcomeLineRequest struct {
	Kind       string
	Actor      string
	Generation uint64
	Line       string
}

func readWelcomeDocument(source fs.FS) (string, error) {
	if source == nil {
		return "", ErrWelcomeDocument
	}
	data, err := fs.ReadFile(source, "welcome")
	if err != nil {
		return "", fmt.Errorf("%w: read welcome document: %v", ErrWelcomeDocument, err)
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("welcome document is not UTF-8: %w", ErrWelcomeDocument)
	}
	return string(data), nil
}

// ExecuteWelcomeLine projects command4.c's welcome() onto the durable
// command/receipt boundary. The document source is injected by the process
// boundary; clients cannot select a path. The reducer validates the actor is
// still online, reads the fixed welcome document, and returns the exact input
// snapshot unchanged so the command has no world-state side effects.
func (o *Ownership) ExecuteWelcomeLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, source fs.FS) (storage.WorldReceipt, error) {
	if strings.TrimSpace(line) != "환영" {
		return storage.WorldReceipt{}, ErrUnsupportedWelcomeLine
	}
	request, err := json.Marshal(welcomeLineRequest{
		Kind: "welcome", Actor: lease.ActorID, Generation: lease.Generation, Line: "환영",
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, request, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		player, ok := state.Players[actorID]
		if !ok || !player.Online {
			return nil, nil, fmt.Errorf("online welcome actor required")
		}
		text, err := readWelcomeDocument(source)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(text)
		return raw, response, err
	})
}
