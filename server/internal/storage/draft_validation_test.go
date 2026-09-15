package storage

import (
	"context"
	"errors"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/game"
)

func TestCreateRejectsInvalidDraftBeforeDatabase(t *testing.T) {
	// No database: invalid drafts must fail before opening a transaction.
	p := NewPostgres(nil)
	id, err := p.Create(context.Background(), "Alice", []byte("opaque-test-hash"), game.Creation{})
	if id != "" || !errors.Is(err, game.ErrCreation) {
		t.Fatalf("%q %v", id, err)
	}
}
