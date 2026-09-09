package session

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// PasswordCommand identifies the bare connection-local password change
// command. The command itself never carries a credential; the following
// lines are consumed by PasswordChange after the transport has entered its
// continuation state.
type PasswordCommand struct{}

// ParsePasswordLine admits only the original Korean command-table spelling.
// Terminal whitespace is harmless, while arguments, controls, and malformed
// UTF-8 are rejected before a connection can enter the continuation.
func ParsePasswordLine(line string) (PasswordCommand, bool) {
	if !utf8.ValidString(line) {
		return PasswordCommand{}, false
	}
	for _, r := range line {
		if r == '\t' || r == '\v' || r == '\f' {
			// Match the legacy token boundary's surrounding whitespace while
			// still rejecting line separators below.
			continue
		}
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return PasswordCommand{}, false
		}
	}
	if strings.TrimSpace(line) != "암호" {
		return PasswordCommand{}, false
	}
	return PasswordCommand{}, true
}

// IsPasswordLine reports whether line starts the exact password-change
// continuation. It intentionally does not accept an inline password.
func IsPasswordLine(line string) bool {
	_, ok := ParsePasswordLine(line)
	return ok
}
