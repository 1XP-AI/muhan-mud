package session

import (
	"context"
	"errors"
	"github.com/1XP-Inc/muhan-mud/server/internal/game"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"strings"
	"testing"
)

type fakeStore struct {
	exists        bool
	failure       error
	registrations int
}

func (f *fakeStore) Exists(context.Context, string) (bool, error) { return f.exists, f.failure }
func (f *fakeStore) Register(context.Context, string, []byte, game.Creation) (string, error) {
	f.registrations++
	return "character-id", f.failure
}
func (f *fakeStore) Authenticate(_ context.Context, _ string, password []byte) (storage.Character, error) {
	if f.failure != nil {
		return storage.Character{}, f.failure
	}
	if string(password) != "pw1234" {
		return storage.Character{}, storage.ErrCredentials
	}
	return storage.Character{ID: "character-id"}, nil
}

func TestNewCharacterConversation(t *testing.T) {
	db := &fakeStore{}
	s := NewLogin(db)
	ctx := context.Background()
	if s.View().Secret || !strings.Contains(s.View().Text, "이름") {
		t.Fatal("missing initial name prompt")
	}
	for _, line := range []string{"타봇", "예", "", "남", "4", "12 10 12 10 10", "1", "선", "7"} {
		view := s.Submit(ctx, line)
		if view.Verified != nil || view.Closed {
			t.Fatal("premature admission")
		}
	}
	if !s.View().Secret {
		t.Fatal("password would echo")
	}
	view := s.Submit(ctx, "pw1234")
	if view.Verified == nil || view.Verified.ID != "character-id" || view.Secret || db.registrations != 1 {
		t.Fatal("signup failed")
	}
	if strings.Contains(view.Text, "pw1234") {
		t.Fatal("password echoed")
	}
	s.Submit(ctx, "pw1234")
	if db.registrations != 1 {
		t.Fatal("duplicate registration")
	}
}

func TestWrongPasswordClosesAtThirdAttempt(t *testing.T) {
	s := NewLogin(&fakeStore{exists: true})
	ctx := context.Background()
	s.Submit(ctx, "Alice")
	for i := 1; i <= 3; i++ {
		view := s.Submit(ctx, "wrong")
		if view.Verified != nil || view.Closed != (i == 3) {
			t.Fatal("wrong attempt limit")
		}
	}
	if s.Submit(ctx, "pw1234").Verified != nil {
		t.Fatal("closed session resumed")
	}
}

func TestStoreFailureIsNotNewAccount(t *testing.T) {
	s := NewLogin(&fakeStore{failure: errors.New("database unavailable")})
	view := s.Submit(context.Background(), "Alice")
	if !view.Closed || view.Verified != nil || strings.Contains(view.Text, "database unavailable") {
		t.Fatal("database failure exposed or ignored")
	}
}

func TestCreationFailureDoesNotVerify(t *testing.T) {
	db := &fakeStore{}
	s := NewLogin(db)
	ctx := context.Background()
	for _, line := range []string{"타봇", "예", "", "남", "4", "12 10 12 10 10", "1", "선", "7"} {
		s.Submit(ctx, line)
	}
	db.failure = errors.New("unknown commit outcome")
	view := s.Submit(ctx, "pw1234")
	if !view.Closed || view.Verified != nil {
		t.Fatal("failed save admitted")
	}
	s.Submit(ctx, "pw1234")
	if db.registrations != 1 {
		t.Fatal("indeterminate save blindly retried")
	}
}
