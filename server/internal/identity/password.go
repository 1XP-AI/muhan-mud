package identity

import (
	"errors"
	"golang.org/x/crypto/bcrypt"
	"unicode/utf8"
)

var ErrPassword = errors.New("invalid password")

// Preserve the original creation byte-length limit, not a rune-count limit.
// No trimming or normalization is applied. Callers must not log password input.
func validPassword(password []byte) bool {
	if len(password) < 3 || len(password) > 14 || !utf8.Valid(password) {
		return false
	}
	for _, b := range password {
		if b < 32 || b == 127 {
			return false
		}
	}
	return true
}

func HashPassword(password []byte) ([]byte, error) {
	if !validPassword(password) {
		return nil, ErrPassword
	}
	return bcrypt.GenerateFromPassword(password, bcrypt.DefaultCost)
}

// CheckPassword only accepts the current format/cost. Legacy cleartext records
// require an explicit importer, never an automatic plaintext fallback here.
func CheckPassword(hash, password []byte) bool {
	if !validPassword(password) {
		return false
	}
	cost, err := bcrypt.Cost(hash)
	if err != nil || cost != bcrypt.DefaultCost {
		return false
	}
	return bcrypt.CompareHashAndPassword(hash, password) == nil
}
