// Package identity contains game-name identity rules shared by sessions and
// persistence adapters.
package identity

import (
	"errors"
	"unicode/utf8"
)

const (
	nameMaxBytes = 14
	nameMinRunes = 1
	nameMaxRunes = 12
)

// ErrName reports a name that cannot be represented by the legacy player-name
// contract. It intentionally does not include the input value.
var ErrName = errors.New("invalid character name")

// CanonicalName validates a legacy player name and applies the legacy
// lowercize(name, 1) transform. The transform is deliberately byte-oriented:
// ASCII letters are changed, while non-ASCII UTF-8 bytes are preserved. It
// does not trim or Unicode-normalize the name.
func CanonicalName(name string) (string, error) {
	if !validName(name) {
		return "", ErrName
	}

	canonical := []byte(name)
	for i, b := range canonical {
		if b >= 'A' && b <= 'Z' {
			canonical[i] = b + ('a' - 'A')
		}
	}
	if canonical[0] >= 'a' && canonical[0] <= 'z' {
		canonical[0] -= 'a' - 'A'
	}
	return string(canonical), nil
}

func validName(name string) bool {
	if name == "" || !utf8.ValidString(name) || len(name) > nameMaxBytes {
		return false
	}
	runes := utf8.RuneCountInString(name)
	if runes < nameMinRunes || runes > nameMaxRunes || name == "." || name == ".." {
		return false
	}
	for i := 0; i < len(name); i++ {
		b := name[i]
		if b < 32 || b == 127 || b == '/' || b == '\\' || b == ':' {
			return false
		}
	}
	return true
}
