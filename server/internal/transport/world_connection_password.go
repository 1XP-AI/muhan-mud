package transport

import (
	"context"

	"github.com/1XP-Inc/muhan-mud/server/internal/session"
)

// SecretPromptSource is an optional GameConnection capability. The normal
// Submit contract remains string-only for existing adapters; a WebSocket
// handler can type-assert this capability to carry the connection-local
// next-input echo policy without exposing a password in a response.
type SecretPromptSource interface {
	InputIsSecret() bool
}

// InputIsSecret reports whether the next line submitted to this connection is
// a password. It returns only a boolean and never exposes prompt state,
// plaintext, or credential hashes.
func (c *worldConnection) InputIsSecret() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.passwordSecret
}

// submitPasswordLine handles the account-only interactive continuation. The
// caller holds c.mu and the connector command mutex. No world snapshot,
// receipt, or reducer is touched on any password path.
func (c *worldConnection) submitPasswordLine(ctx context.Context, line string) (string, bool, error) {
	if c.passwordChange == nil && !session.IsPasswordLine(line) {
		return "", false, nil
	}
	if c.passwordChange == nil {
		c.passwordChange = session.NewPasswordChanger(c.game.config.PasswordStore, c.accountName)
		view := c.passwordChange.View()
		if view.Done {
			c.passwordChange = nil
		}
		c.passwordSecret = view.Secret
		return view.Text, true, nil
	}
	view := c.passwordChange.Submit(ctx, line)
	if view.Done {
		// PasswordChange clears its pending hashes on every terminal path. Drop
		// the interface as well so a completed operation cannot be reused by a
		// later ordinary line or retained through a socket lifetime.
		c.passwordChange = nil
	}
	c.passwordSecret = view.Secret
	return view.Text, true, nil
}
