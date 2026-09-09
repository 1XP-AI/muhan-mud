package session

import (
	"context"
	"strings"

	"github.com/1XP-Inc/muhan-mud/server/internal/identity"
)

// PasswordChangeStore is the account-only persistence boundary for a password
// change. It deliberately does not extend Accounts: login and registration
// callers must not gain write access to credentials accidentally.
//
// LookupCredential returns the immutable account identity and its current
// credential hash. The hash is an opaque value owned by this operation; it is
// never rendered, logged, or put in a game receipt. UpdateCredential must
// atomically replace the hash for accountID, accepting a retry when the
// account already contains replacementHash.
type PasswordChangeStore interface {
	LookupCredential(context.Context, string) (accountID string, credentialHash []byte, err error)
	UpdateCredential(context.Context, string, []byte, []byte) error
}

// PasswordChanger is the small interface a connector needs to drive the
// interactive command. Implementations are connection-local and serial; no
// password or hash is exposed through the view.
type PasswordChanger interface {
	View() PasswordChangeView
	Submit(context.Context, string) PasswordChangeView
	Cancel() PasswordChangeView
}

// PasswordChangeView contains only terminal-safe command state. Secret means
// the next input must not echo; it never means that Text contains a secret.
type PasswordChangeView struct {
	Text      string
	Secret    bool
	Done      bool
	Cancelled bool
}

type passwordChangeStage uint8

const (
	passwordChangeCurrent passwordChangeStage = iota
	passwordChangeNew
	passwordChangeConfirm
	passwordChangeDone
	passwordChangeCancelled
)

// PasswordChange implements C passwd/chpasswd's bounded current → new →
// confirmation flow. It keeps only an account ID, the expected old hash, and
// one pending replacement hash while the command is active. Those bytes are
// cleared on every terminal path.
type PasswordChange struct {
	store PasswordChangeStore
	name  string
	stage passwordChangeStage
	view  PasswordChangeView

	accountID    string
	expectedHash []byte
	newHash      []byte
	hasher       func([]byte) ([]byte, error)
}

// NewPasswordChanger starts an interactive password change for the canonical
// game name. Invalid names and unavailable stores produce a completed generic
// view, without echoing the invalid input or an underlying error.
func NewPasswordChanger(store PasswordChangeStore, name string) *PasswordChange {
	result := &PasswordChange{
		store:  store,
		stage:  passwordChangeCurrent,
		view:   PasswordChangeView{Text: "현재 암호를 입력하십시오: ", Secret: true},
		hasher: identity.HashPassword,
	}
	canonical, err := identity.CanonicalName(name)
	if err != nil || store == nil {
		result.finishFailure()
		return result
	}
	result.name = canonical
	return result
}

// NewPasswordChange is a descriptive alias retained for callers that prefer
// the command name over the actor name.
func NewPasswordChange(store PasswordChangeStore, name string) *PasswordChange {
	return NewPasswordChanger(store, name)
}

// NewPasswordChangerWithHasher is intended for deterministic unit tests and
// alternate password-hashing deployments. Production callers should use
// NewPasswordChanger, which uses identity.HashPassword.
func NewPasswordChangerWithHasher(store PasswordChangeStore, name string, hasher func([]byte) ([]byte, error)) *PasswordChange {
	result := NewPasswordChanger(store, name)
	if hasher != nil && result.stage == passwordChangeCurrent && !result.view.Done {
		result.hasher = hasher
	}
	return result
}

func (s *PasswordChange) View() PasswordChangeView { return s.view }

// Stage reports the connection-local state without exposing credential bytes.
func (s *PasswordChange) Stage() string {
	if s == nil {
		return "done"
	}
	switch s.stage {
	case passwordChangeCurrent:
		return "current"
	case passwordChangeNew:
		return "new"
	case passwordChangeConfirm:
		return "confirm"
	case passwordChangeCancelled:
		return "cancelled"
	default:
		return "done"
	}
}

// Submit advances one prompt. A failed persistence call leaves the operation
// at confirmation, so the connector can retry with the same confirmation and
// the exact same account identity/hash intent. No retry regenerates a hash.
func (s *PasswordChange) Submit(ctx context.Context, line string) PasswordChangeView {
	if s == nil {
		return PasswordChangeView{Text: "암호 변경을 처리하지 못했습니다.\r\n", Done: true}
	}
	if s.view.Done || s.view.Cancelled {
		return s.view
	}
	if isPasswordChangeCancel(line) {
		return s.Cancel()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		if s.stage == passwordChangeConfirm {
			// Keep the pending intent for a caller that retries with a live
			// context; there has been no new hash generation or mutation.
			s.view = PasswordChangeView{Text: "암호 저장을 확인할 수 없습니다. 다시 입력하십시오: ", Secret: true}
			return s.view
		}
		s.finishFailure()
		return s.view
	}

	switch s.stage {
	case passwordChangeCurrent:
		return s.submitCurrent(ctx, line)
	case passwordChangeNew:
		return s.submitNew(line)
	case passwordChangeConfirm:
		return s.submitConfirm(ctx, line)
	default:
		return s.view
	}
}

// Cancel abandons the operation without calling the store. It is safe to
// call more than once and clears any pending credential material.
func (s *PasswordChange) Cancel() PasswordChangeView {
	if s == nil {
		return PasswordChangeView{Text: "암호 변경을 취소했습니다.\r\n", Done: true, Cancelled: true}
	}
	if s.view.Done || s.view.Cancelled {
		return s.view
	}
	s.clearSecrets()
	s.stage = passwordChangeCancelled
	s.view = PasswordChangeView{Text: "암호 변경을 취소했습니다.\r\n", Done: true, Cancelled: true}
	return s.view
}

func (s *PasswordChange) submitCurrent(ctx context.Context, line string) PasswordChangeView {
	candidate := []byte(line)
	defer clear(candidate)
	accountID, storedHash, err := s.store.LookupCredential(ctx, s.name)
	if err != nil || accountID == "" || !identity.CheckPassword(storedHash, candidate) {
		// Do not distinguish a missing account, malformed hash, and wrong
		// password to the terminal. None of these paths mutates storage.
		s.finishFailure("암호가 틀렸습니다.\r\n암호가 변경되지 않았습니다.\r\n")
		return s.view
	}
	s.accountID = accountID
	s.expectedHash = cloneSecret(storedHash)
	s.stage = passwordChangeNew
	s.view = PasswordChangeView{Text: "새 암호를 입력하십시오(3~14바이트): ", Secret: true}
	return s.view
}

func (s *PasswordChange) submitNew(line string) PasswordChangeView {
	candidate := []byte(line)
	hash, err := s.hasher(candidate)
	clear(candidate)
	if err != nil {
		// identity.HashPassword enforces 3..14 bytes, UTF-8, and no controls.
		// C aborts this command on an invalid new password; leave no retryable
		// credential material behind.
		s.finishFailure("암호가 유효하지 않습니다.\r\n암호가 변경되지 않았습니다.\r\n")
		return s.view
	}
	// Keep a private copy. Clear the hasher-owned slice immediately so a
	// caller cannot accidentally retain the pending hash through its return
	// buffer, while the state machine still has the one operation intent.
	s.newHash = cloneSecret(hash)
	clear(hash)
	s.stage = passwordChangeConfirm
	s.view = PasswordChangeView{Text: "새 암호를 다시 입력하십시오: ", Secret: true}
	return s.view
}

func (s *PasswordChange) submitConfirm(ctx context.Context, line string) PasswordChangeView {
	candidate := []byte(line)
	defer clear(candidate)
	// The confirmation is the plaintext candidate for this one call only. The
	// pending state keeps only the bcrypt hash, so confirmation never requires
	// retaining or serializing a new plaintext password.
	if !identity.CheckPassword(s.newHash, candidate) {
		s.finishFailure("암호가 서로 틀립니다.\r\n암호가 변경되지 않았습니다.\r\n")
		return s.view
	}
	// Pass copies so a store cannot retain aliases into this state. The
	// original and replacement hash remain unchanged for an uncertain retry.
	err := s.store.UpdateCredential(ctx, s.accountID, cloneSecret(s.expectedHash), cloneSecret(s.newHash))
	if err != nil {
		s.view = PasswordChangeView{Text: "암호 저장 완료를 확인할 수 없습니다. 다시 입력하십시오: ", Secret: true}
		return s.view
	}
	s.clearSecrets()
	s.stage = passwordChangeDone
	s.view = PasswordChangeView{Text: "암호가 변경되었습니다.\r\n", Done: true}
	return s.view
}

func (s *PasswordChange) finishFailure(text ...string) {
	s.clearSecrets()
	s.stage = passwordChangeDone
	message := "암호 변경을 처리하지 못했습니다.\r\n"
	if len(text) != 0 && text[0] != "" {
		message = text[0]
	}
	s.view = PasswordChangeView{Text: message, Done: true}
}

func (s *PasswordChange) clearSecrets() {
	clear(s.expectedHash)
	clear(s.newHash)
	s.expectedHash = nil
	s.newHash = nil
	s.accountID = ""
}

func cloneSecret(value []byte) []byte {
	if len(value) == 0 {
		return nil
	}
	return append([]byte(nil), value...)
}

func isPasswordChangeCancel(line string) bool {
	return line == "취소" || strings.EqualFold(line, "cancel")
}

var _ PasswordChanger = (*PasswordChange)(nil)
