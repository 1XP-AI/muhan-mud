package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
)

var ErrUnsupportedQuitLine = errors.New("line is not the quit command")

func IsQuitLine(line string) bool { return strings.TrimSpace(line) == "끝" }

// ExecuteQuitLine records the user's explicit terminal quit acknowledgement.
// Departure remains owned by the connection Close path so a lost response or
// socket failure still uses the same cleanup/retry receipt as an ordinary
// disconnect.
func (o *Ownership) ExecuteQuitLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	if !IsQuitLine(line) {
		return storage.WorldReceipt{}, ErrUnsupportedQuitLine
	}
	payload, err := json.Marshal(struct{ Line string }{line})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, _ string) (json.RawMessage, json.RawMessage, error) {
		response, err := json.Marshal("게임을 종료합니다. 안녕히 가십시오.\r\n")
		return raw, response, err
	})
}
