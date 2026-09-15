package session

import (
	"context"
	"crypto/rand"
	"errors"

	"github.com/1XP-Inc/muhan-mud/server/internal/game"
	"github.com/1XP-Inc/muhan-mud/server/internal/identity"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
)

type WorldRegistrationStore interface {
	LoadWorld(context.Context, string) (storage.WorldSnapshot, error)
	CreateInWorld(context.Context, string, string, int64, string, []byte, game.Creation) (string, error)
}

// WorldAccounts replaces draft-only Register in the terminal Accounts contract.
// Existing credential checks remain delegated. Only registration creates a
// world character; Authenticate must never reset an existing player's state.
type WorldAccounts struct {
	Accounts
	store   WorldRegistrationStore
	worldID string
}

func NewWorldAccounts(accounts Accounts, store WorldRegistrationStore, worldID string) *WorldAccounts {
	return &WorldAccounts{Accounts: accounts, store: store, worldID: worldID}
}

// Register retains one server-generated operation identity and one salted hash
// throughout bounded retries. If the outcome remains unknown, Login closes and
// the user reconnects through ordinary credential authentication, not a claim
// based only on a name. No secret or pending request survives this call.
func (a *WorldAccounts) Register(ctx context.Context, name string, password []byte, draft game.Creation) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	canonical, err := identity.CanonicalName(name)
	if err != nil {
		return "", err
	}
	if _, err = draft.Choices(); err != nil {
		return "", err
	}
	if a.store == nil || a.worldID == "" {
		return "", errors.New("world registration unavailable")
	}
	hash, err := identity.HashPassword(password)
	if err != nil {
		return "", err
	}
	defer clear(hash)
	commandID := "register-" + rand.Text()
	for attempt := 0; attempt < 3; attempt++ {
		if err = ctx.Err(); err != nil {
			return "", err
		}
		var snapshot storage.WorldSnapshot
		snapshot, err = a.store.LoadWorld(ctx, a.worldID)
		if err != nil {
			return "", err
		}
		var id string
		id, err = a.store.CreateInWorld(ctx, a.worldID, commandID, snapshot.Revision, canonical, hash, draft)
		if err == nil {
			if id == "" {
				return "", errors.New("empty registration result")
			}
			return id, nil
		}
		if errors.Is(err, storage.ErrCommandConflict) {
			return "", err
		}
	}
	return "", err
}

var _ Accounts = (*WorldAccounts)(nil)
