package session

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// IgnoreCommandName is the exact command9.c/global.c command word.
	IgnoreCommandName = "듣기거부"
	// Player names are stored in the legacy player-name field. The parser
	// applies the same byte/codepoint boundary before a target reaches a
	// connection-local list or a later canonical world lookup.
	IgnoreTargetNameMaxBytes = 14
	IgnoreTargetNameMaxRunes = 12
)

var (
	ErrUnsupportedIgnoreLine = errors.New("line is not an implemented ignore command")
	ErrIgnoreTargetRequired  = errors.New("ignore target name is required")
	ErrIgnoreTargetTooLong   = errors.New("ignore target name exceeds the byte or codepoint limit")
	ErrIgnoreTargetInvalid   = errors.New("ignore target name is invalid")
)

// IgnoreCommandKind separates command9.c's no-argument list operation from
// its online-player toggle operation. Target is a display-name selector only;
// the eventual connector/world integration must resolve the actor and target
// again from its authoritative online registry before adding a name.
type IgnoreCommandKind uint8

const (
	IgnoreList IgnoreCommandKind = iota + 1
	IgnoreToggle
)

type IgnoreCommand struct {
	Kind   IgnoreCommandKind
	Target string
}

// ParseIgnoreLine accepts the exact legacy command word with no argument, or
// one bounded online-player name. Quoted single-token names are allowed by
// the shared legacy tokenizer; quotes cannot be used to smuggle whitespace or
// a second command token into Target. Prefix matching and target authority
// deliberately remain outside this parser.
func ParseIgnoreLine(line string) (IgnoreCommand, bool) {
	if !validIgnoreLineText(line) {
		return IgnoreCommand{}, false
	}
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || len(tokens) < 1 || len(tokens) > 2 || tokens[0] != IgnoreCommandName {
		return IgnoreCommand{}, false
	}
	if len(tokens) == 1 {
		return IgnoreCommand{Kind: IgnoreList}, true
	}
	target, err := CanonicalIgnoreTargetName(tokens[1])
	if err != nil {
		return IgnoreCommand{}, false
	}
	return IgnoreCommand{Kind: IgnoreToggle, Target: target}, true
}

func IsIgnoreLine(line string) bool {
	_, ok := ParseIgnoreLine(line)
	return ok
}

// ValidateIgnoreTargetName enforces the legacy player-name envelope plus the
// stricter one-token selector contract. Player names containing spaces may be
// retained by the migration identity layer, but cannot be selected by this
// command's cmnd->str[1] boundary without ambiguity.
func ValidateIgnoreTargetName(name string) error {
	if name == "" {
		return ErrIgnoreTargetRequired
	}
	if !utf8.ValidString(name) {
		return ErrIgnoreTargetInvalid
	}
	if len(name) > IgnoreTargetNameMaxBytes || utf8.RuneCountInString(name) > IgnoreTargetNameMaxRunes {
		return ErrIgnoreTargetTooLong
	}
	if strings.TrimSpace(name) != name || name == "." || name == ".." {
		return ErrIgnoreTargetInvalid
	}
	for _, r := range name {
		if unicode.IsControl(r) || unicode.IsSpace(r) || unicode.Is(unicode.Cf, r) || r == '/' || r == '\\' || r == ':' {
			return ErrIgnoreTargetInvalid
		}
	}
	return nil
}

// CanonicalIgnoreTargetName mirrors command9.c's up(cmnd->str[1][0]): only
// the first ASCII byte is upper-cased. It does not resolve a target to a
// player ID; callers must perform that check against the current world.
func CanonicalIgnoreTargetName(name string) (string, error) {
	if err := ValidateIgnoreTargetName(name); err != nil {
		return "", err
	}
	canonical := []byte(name)
	if canonical[0] >= 'a' && canonical[0] <= 'z' {
		canonical[0] -= 'a' - 'A'
	}
	return string(canonical), nil
}

func validIgnoreLineText(line string) bool {
	if !utf8.ValidString(line) {
		return false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return strings.TrimSpace(line) != ""
}
