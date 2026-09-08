package session

import (
	"errors"
	"strings"
	"unicode"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

// CommandKind is the central dispatch classification for the commands that
// currently have durable Go handlers. It is deliberately separate from the
// legacy numeric command IDs: the old table has 154 positive handlers and the
// remaining kinds must not be silently treated as implemented.
type CommandKind uint8

const (
	CommandUnknown CommandKind = iota
	CommandLook
	CommandDirectional
	CommandAttack
	CommandStatus
	CommandFollow
	CommandItems
	CommandSay
	CommandSocial
	CommandItemMutation
	CommandEquipment
	CommandBank
	CommandQuit
	CommandRead
	CommandInfo
	CommandHelp
	CommandYell
	CommandWelcome
	CommandEmote
	CommandLookAtTarget
	CommandExpress
	CommandSearch
	CommandTrack
	CommandHide
	CommandPeek
	CommandSettings
	CommandDoor
)

var ErrCommandTooManyTokens = errors.New("command has more than seven tokens")

type ParsedCommand struct {
	Line   string
	Tokens []string
	Kind   CommandKind
}

// ParseCommand centralizes the first-token boundary used by the live
// connector. It preserves the original line for durable receipt identity,
// applies C's seven-token limit, and intentionally uses an explicit alias
// table. Prefix/occurrence expansion and the complete legacy command table
// remain follow-up slices instead of being guessed here.
func ParseCommand(line string) (ParsedCommand, error) {
	parsed := ParsedCommand{Line: line}
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return parsed, nil
	}
	if _, ok := SayLineText(trimmed); ok {
		parsed.Kind = CommandSay
		parsed.Tokens = legacyTokens(trimmed)
		if len(parsed.Tokens) > 7 {
			return ParsedCommand{}, ErrCommandTooManyTokens
		}
		return parsed, nil
	}
	tokens, err := tokenizeLegacy(trimmed)
	if err != nil {
		return ParsedCommand{}, err
	}
	if len(tokens) > 7 {
		return ParsedCommand{}, ErrCommandTooManyTokens
	}
	parsed.Tokens = tokens
	if len(tokens) == 0 {
		return parsed, nil
	}
	if _, ok := world.ParseDirectionalToken(trimmed); ok {
		parsed.Kind = CommandDirectional
		return parsed, nil
	}
	parsed.Kind = commandKind(tokens[0])
	if isSingleTokenKind(parsed.Kind) && len(tokens) != 1 {
		parsed.Kind = CommandUnknown
	}
	return parsed, nil
}

func legacyTokens(line string) []string {
	tokens, _ := tokenizeLegacy(line)
	return tokens
}

func tokenizeLegacy(line string) ([]string, error) {
	var tokens []string
	var current []rune
	quoted := rune(0)
	escaped := false
	flush := func() {
		if len(current) == 0 {
			return
		}
		tokens = append(tokens, string(current))
		current = nil
	}
	for _, r := range []rune(line) {
		if escaped {
			current = append(current, r)
			escaped = false
			continue
		}
		if quoted != 0 {
			switch r {
			case '\\':
				escaped = true
			case quoted:
				quoted = 0
			default:
				current = append(current, r)
			}
			continue
		}
		switch {
		case r == '\'' || r == '"':
			quoted = r
		case unicode.IsSpace(r):
			flush()
		default:
			current = append(current, r)
		}
	}
	if escaped {
		current = append(current, '\\')
	}
	if quoted != 0 {
		return nil, errors.New("unterminated quoted command token")
	}
	flush()
	return tokens, nil
}

func commandKind(first string) CommandKind {
	switch first {
	case "봐", "보다", "조사":
		return CommandLook
	case "공격", "공", "쳐", "때려":
		return CommandAttack
	case "건강", "점수":
		return CommandStatus
	case "따라", "내보내":
		return CommandFollow
	case "소지품", "장비", "장":
		return CommandItems
	case "누구", "그룹":
		return CommandSocial
	case "주워", "주", "가져", "꺼내", "버려", "넣어":
		return CommandItemMutation
	case "입어", "쥐어", "무장", "벗어":
		return CommandEquipment
	case "잔액", "보관물", "받아", "입금", "출금":
		return CommandBank
	case "끝":
		return CommandQuit
	case "시간":
		return CommandRead
	case "정보":
		return CommandInfo
	case "도움말", "?":
		return CommandHelp
	case "외쳐":
		return CommandYell
	case "환영":
		return CommandWelcome
	case "표현":
		return CommandExpress
	case "보아":
		return CommandLookAtTarget
	case "검색", "찾아":
		return CommandSearch
	case "추적":
		return CommandTrack
	case "숨겨", "숨어":
		return CommandHide
	case "엿봐":
		return CommandPeek
	case "설정", "해제":
		return CommandSettings
	case "열어", "닫아":
		return CommandDoor
	default:
		if world.IsEmoteAlias(first) {
			return CommandEmote
		}
		return CommandUnknown
	}
}

func isSingleTokenKind(kind CommandKind) bool {
	switch kind {
	case CommandLook, CommandStatus, CommandItems, CommandSocial, CommandQuit, CommandRead, CommandInfo, CommandWelcome, CommandSearch, CommandTrack, CommandHide:
		return true
	default:
		return false
	}
}
