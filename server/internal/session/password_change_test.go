package session

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/identity"
)

type passwordChangeFakeStore struct {
	accountID     string
	hash          []byte
	lookup        int
	updates       int
	failure       error
	updateFailure error
	ids           []string
	oldHashes     [][]byte
	newHashes     [][]byte
}

func (f *passwordChangeFakeStore) LookupCredential(_ context.Context, name string) (string, []byte, error) {
	f.lookup++
	f.ids = append(f.ids, name)
	if f.failure != nil {
		return "", nil, f.failure
	}
	return f.accountID, append([]byte(nil), f.hash...), nil
}

func (f *passwordChangeFakeStore) UpdateCredential(_ context.Context, accountID string, expected, replacement []byte) error {
	f.updates++
	f.ids = append(f.ids, accountID)
	f.oldHashes = append(f.oldHashes, append([]byte(nil), expected...))
	f.newHashes = append(f.newHashes, append([]byte(nil), replacement...))
	if f.updateFailure != nil {
		err := f.updateFailure
		// The fake models one uncertain write: only the first attempt fails,
		// while a caller retry still sees the same operation intent.
		f.updateFailure = nil
		return err
	}
	f.hash = append([]byte(nil), replacement...)
	return nil
}

func passwordChangeTestStore(t *testing.T) *passwordChangeFakeStore {
	t.Helper()
	hash, err := identity.HashPassword([]byte("old-password"))
	if err != nil {
		t.Fatal(err)
	}
	return &passwordChangeFakeStore{accountID: "account-17", hash: hash}
}

func TestPasswordChangeCompletesCurrentNewConfirmationWithoutExposingSecrets(t *testing.T) {
	store := passwordChangeTestStore(t)
	changer := NewPasswordChanger(store, "ALICE")

	view := changer.View()
	if !view.Secret || view.Done || !strings.Contains(view.Text, "현재 암호") {
		t.Fatalf("unexpected initial view: %+v", view)
	}
	view = changer.Submit(context.Background(), "old-password")
	if !view.Secret || view.Done || !strings.Contains(view.Text, "새 암호") {
		t.Fatalf("unexpected new-password view: %+v", view)
	}
	view = changer.Submit(context.Background(), "new-password")
	if !view.Secret || view.Done || !strings.Contains(view.Text, "다시") {
		t.Fatalf("unexpected confirmation view: %+v", view)
	}
	view = changer.Submit(context.Background(), "new-password")
	if !view.Done || view.Secret || view.Cancelled || !strings.Contains(view.Text, "변경되었습니다") {
		t.Fatalf("unexpected success view: %+v", view)
	}
	if store.lookup != 1 || store.updates != 1 || store.ids[0] != "Alice" || store.ids[1] != "account-17" {
		t.Fatalf("unexpected store calls: lookup=%d updates=%d ids=%v", store.lookup, store.updates, store.ids)
	}
	if !identity.CheckPassword(store.hash, []byte("new-password")) || identity.CheckPassword(store.hash, []byte("old-password")) {
		t.Fatal("password was not changed exactly once")
	}
	if strings.Contains(view.Text, "old-password") || strings.Contains(view.Text, "new-password") {
		t.Fatal("password appeared in output")
	}
}

func TestPasswordChangeRetriesSameIntentWithoutRegeneratingHash(t *testing.T) {
	store := passwordChangeTestStore(t)
	store.updateFailure = errors.New("transient persistence failure")
	hashCalls := 0
	changer := NewPasswordChangerWithHasher(store, "Alice", func(password []byte) ([]byte, error) {
		hashCalls++
		if string(password) != "new-password" {
			t.Fatal("hasher received unexpected password")
		}
		return identity.HashPassword(password)
	})

	changer.Submit(context.Background(), "old-password")
	changer.Submit(context.Background(), "new-password")
	failed := changer.Submit(context.Background(), "new-password")
	if failed.Done || !failed.Secret || changer.Stage() != "confirm" {
		t.Fatalf("uncertain save did not remain retryable: %+v stage=%s", failed, changer.Stage())
	}
	succeeded := changer.Submit(context.Background(), "new-password")
	if !succeeded.Done || succeeded.Secret || store.updates != 2 {
		t.Fatalf("retry did not complete: view=%+v updates=%d", succeeded, store.updates)
	}
	if hashCalls != 1 {
		t.Fatalf("new hash generated %d times, want once", hashCalls)
	}
	if len(store.newHashes) != 2 || !bytes.Equal(store.newHashes[0], store.newHashes[1]) {
		t.Fatal("retry changed the replacement hash intent")
	}
	if string(store.newHashes[0]) == "new-password" {
		t.Fatal("plaintext replacement was passed to persistence")
	}
}

func TestPasswordChangeWrongCurrentPasswordDoesNotUpdate(t *testing.T) {
	store := passwordChangeTestStore(t)
	changer := NewPasswordChanger(store, "Alice")
	view := changer.Submit(context.Background(), "wrong-password")
	if !view.Done || view.Secret || store.updates != 0 || changer.Stage() != "done" {
		t.Fatalf("wrong password was not terminal: view=%+v updates=%d stage=%s", view, store.updates, changer.Stage())
	}
	if strings.Contains(view.Text, "wrong-password") || strings.Contains(view.Text, "old-password") {
		t.Fatal("credential appeared in wrong-password output")
	}
}

func TestPasswordChangeMismatchDoesNotUpdate(t *testing.T) {
	store := passwordChangeTestStore(t)
	changer := NewPasswordChanger(store, "Alice")
	changer.Submit(context.Background(), "old-password")
	changer.Submit(context.Background(), "new-password")
	view := changer.Submit(context.Background(), "different-password")
	if !view.Done || view.Secret || store.updates != 0 || changer.Stage() != "done" {
		t.Fatalf("mismatch was not terminal: view=%+v updates=%d stage=%s", view, store.updates, changer.Stage())
	}
	if strings.Contains(view.Text, "new-password") || strings.Contains(view.Text, "different-password") {
		t.Fatal("credential appeared in mismatch output")
	}
}

func TestPasswordChangeCancelHasNoStoreMutationAtEveryStage(t *testing.T) {
	for _, stage := range []struct {
		name  string
		setup func(*PasswordChange)
	}{{
		name: "current",
	}, {
		name: "new",
		setup: func(changer *PasswordChange) {
			changer.Submit(context.Background(), "old-password")
		},
	}, {
		name: "confirm",
		setup: func(changer *PasswordChange) {
			changer.Submit(context.Background(), "old-password")
			changer.Submit(context.Background(), "new-password")
		},
	}} {
		t.Run(stage.name, func(t *testing.T) {
			store := passwordChangeTestStore(t)
			changer := NewPasswordChanger(store, "Alice")
			if stage.setup != nil {
				stage.setup(changer)
			}
			view := changer.Submit(context.Background(), "취소")
			if !view.Done || !view.Cancelled || store.lookup != map[string]int{"current": 0, "new": 1, "confirm": 1}[stage.name] || store.updates != 0 {
				t.Fatalf("cancel mutated or wrong state: view=%+v lookup=%d updates=%d", view, store.lookup, store.updates)
			}
			if changer.accountID != "" || len(changer.expectedHash) != 0 || len(changer.newHash) != 0 {
				t.Fatal("cancel retained account credential material")
			}
		})
	}
}

func TestPasswordChangeRejectsInvalidNewPasswordWithoutUpdate(t *testing.T) {
	for _, input := range []string{"", "ab", strings.Repeat("x", 15), "ab\x00", "ab\n", string([]byte{0xff, 0xff, 0xff})} {
		t.Run(input, func(t *testing.T) {
			store := passwordChangeTestStore(t)
			changer := NewPasswordChanger(store, "Alice")
			changer.Submit(context.Background(), "old-password")
			view := changer.Submit(context.Background(), input)
			if !view.Done || view.Secret || store.updates != 0 {
				t.Fatalf("invalid password accepted: input=%q view=%+v updates=%d", input, view, store.updates)
			}
		})
	}
}

func TestPasswordChangeCancelledContextDoesNotLookupOrUpdate(t *testing.T) {
	store := passwordChangeTestStore(t)
	changer := NewPasswordChanger(store, "Alice")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	view := changer.Submit(ctx, "old-password")
	if !view.Done || store.lookup != 0 || store.updates != 0 {
		t.Fatalf("cancelled context mutated operation: view=%+v lookup=%d updates=%d", view, store.lookup, store.updates)
	}
}

func TestPasswordChangeRetryWithLiveContextKeepsIntentAfterCancelledSave(t *testing.T) {
	store := passwordChangeTestStore(t)
	changer := NewPasswordChanger(store, "Alice")
	changer.Submit(context.Background(), "old-password")
	changer.Submit(context.Background(), "new-password")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	view := changer.Submit(ctx, "new-password")
	if view.Done || changer.Stage() != "confirm" || store.updates != 0 {
		t.Fatalf("cancelled save did not remain retryable: view=%+v stage=%s updates=%d", view, changer.Stage(), store.updates)
	}
	view = changer.Submit(context.Background(), "new-password")
	if !view.Done || store.updates != 1 {
		t.Fatalf("live retry did not commit: view=%+v updates=%d", view, store.updates)
	}
}
