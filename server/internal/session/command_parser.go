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
	CommandCast
	CommandSave
	CommandMail
	CommandMemo
	CommandBoard
	CommandInfo
	CommandHelp
	CommandPassword
	CommandYell
	CommandBroadcast
	CommandWelcome
	CommandEmote
	CommandLookAtTarget
	CommandExpress
	CommandSearch
	CommandTrack
	CommandHide
	CommandBribe
	CommandPeek
	CommandSettings
	CommandDoor
	CommandDoorKey
	CommandFlee
	CommandShopList
	CommandShopSell
	CommandShopPurchase
	CommandTrade
	CommandValue
	CommandRepair
	CommandDirectMessage
	CommandReply
	CommandMerchantPurchase
	CommandNPCTalk
	CommandGroupTalk
	CommandDescription
	CommandPlayerLookup
	CommandReturnSquare
	CommandCompare
	CommandObjectAppraisal
	CommandItemRename
	CommandRangerPray
	CommandPrepare
	CommandUpDmg
	CommandTitle
	CommandPowerAccuracy
	CommandMeditate
	CommandAlias
	CommandBurn
	CommandStudy
	CommandMailSend
	CommandBoardWrite
	CommandIgnore
	CommandSteal
	CommandTeach
	CommandBackstab
	CommandDrink
	CommandCircle
	CommandBash
	CommandMagicStop
	CommandGive
	CommandPoison
	CommandEnemyStatus
	CommandTrain
	CommandSelection
	CommandTurn
	CommandAbsorb
	CommandKick
	CommandUse
	CommandChangeClass
	// The legacy CommandRead value is retained for `시간`; readscroll has a
	// distinct kind so `읽어 게시판 <n>` can remain on the board route.
	CommandReadScroll
	CommandPropertyInvite
	CommandFamilyWho
	CommandFamilyMember
	CommandFamilyList
	CommandFamilyTalk
	CommandFamilyMutation
	CommandMarriage
	CommandMarriageSend
	CommandDivorce
	CommandVote
)

// Descriptive aliases preserve the original CommandRead value used by the
// existing parser while allowing new adapters to name the source operation
// directly. CommandTrain is the terminal-facing spelling for command7.c's
// `수련` reducer.
const (
	CommandTime     CommandKind = CommandRead
	CommandTraining CommandKind = CommandTrain
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
	// Suffix commands are checked before the legacy leading-quote speech
	// shortcut so a quoted item selector can still reach its own reducer.
	if IsItemRenameLine(trimmed) {
		parsed.Kind = CommandItemRename
		parsed.Tokens = legacyTokens(trimmed)
		if len(parsed.Tokens) > 7 {
			return ParsedCommand{}, ErrCommandTooManyTokens
		}
		return parsed, nil
	}
	if IsRangerPrayLine(trimmed) {
		parsed.Kind = CommandRangerPray
		parsed.Tokens = legacyTokens(trimmed)
		return parsed, nil
	}
	if IsPrepareLine(trimmed) {
		parsed.Kind = CommandPrepare
		parsed.Tokens = legacyTokens(trimmed)
		return parsed, nil
	}
	if IsUpDmgLine(trimmed) {
		parsed.Kind = CommandUpDmg
		parsed.Tokens = legacyTokens(trimmed)
		return parsed, nil
	}
	if IsPowerAccuracyLine(trimmed) {
		parsed.Kind = CommandPowerAccuracy
		parsed.Tokens = legacyTokens(trimmed)
		return parsed, nil
	}
	if IsMeditateLine(trimmed) {
		parsed.Kind = CommandMeditate
		parsed.Tokens = legacyTokens(trimmed)
		return parsed, nil
	}
	// Alias.c accepts both the terminal prefix form and the original suffix
	// form. Keep its process text behind the bounded parser instead of letting
	// the generic tokenizer classify an arbitrary command template.
	if IsAliasLine(trimmed) {
		parsed.Kind = CommandAlias
		parsed.Tokens = legacyTokens(trimmed)
		if len(parsed.Tokens) > 7 {
			return ParsedCommand{}, ErrCommandTooManyTokens
		}
		return parsed, nil
	}
	if IsBurnLine(trimmed) {
		parsed.Kind = CommandBurn
		parsed.Tokens = legacyTokens(trimmed)
		return parsed, nil
	}
	if IsStudyLine(trimmed) {
		parsed.Kind = CommandStudy
		parsed.Tokens = legacyTokens(trimmed)
		return parsed, nil
	}
	if IsPasswordLine(line) {
		parsed.Kind = CommandPassword
		parsed.Tokens = legacyTokens(trimmed)
		return parsed, nil
	}
	if IsBoardLine(trimmed) {
		parsed.Kind = CommandBoard
		parsed.Tokens = legacyTokens(trimmed)
		return parsed, nil
	}
	if IsReadScrollLine(trimmed) {
		parsed.Kind = CommandReadScroll
		parsed.Tokens = legacyTokens(trimmed)
		return parsed, nil
	}
	if IsCastLine(trimmed) {
		parsed.Kind = CommandCast
		parsed.Tokens = legacyTokens(trimmed)
		return parsed, nil
	}
	if IsPropertyInviteLine(trimmed) {
		parsed.Kind = CommandPropertyInvite
		parsed.Tokens = legacyTokens(trimmed)
		return parsed, nil
	}
	if IsFamilyMutationLine(trimmed) {
		parsed.Kind = CommandFamilyMutation
		parsed.Tokens = legacyTokens(trimmed)
		return parsed, nil
	}
	if IsFamilyTalkLine(trimmed) {
		parsed.Kind = CommandFamilyTalk
		parsed.Tokens = legacyTokens(trimmed)
		return parsed, nil
	}
	if IsMarriageLine(trimmed) {
		parsed.Kind = CommandMarriage
		parsed.Tokens = legacyTokens(trimmed)
		return parsed, nil
	}
	if IsMarriageSendLine(trimmed) {
		parsed.Kind = CommandMarriageSend
		parsed.Tokens = legacyTokens(trimmed)
		return parsed, nil
	}
	if IsReplyLine(trimmed) {
		parsed.Kind = CommandReply
		parsed.Tokens = legacyTokens(trimmed)
		return parsed, nil
	}
	if IsVoteLine(trimmed) {
		parsed.Kind = CommandVote
		parsed.Tokens = legacyTokens(trimmed)
		return parsed, nil
	}
	if family, ok := ParseFamilyLine(trimmed); ok {
		parsed.Tokens = legacyTokens(trimmed)
		switch family.Action {
		case FamilyWhoAction:
			parsed.Kind = CommandFamilyWho
		case FamilyMemberAction:
			parsed.Kind = CommandFamilyMember
		case FamilyListAction:
			parsed.Kind = CommandFamilyList
		}
		return parsed, nil
	}
	if IsMailSendLine(trimmed) {
		parsed.Kind = CommandMailSend
		parsed.Tokens = legacyTokens(trimmed)
		return parsed, nil
	}
	if IsMemoLine(trimmed) {
		parsed.Kind = CommandMemo
		parsed.Tokens = legacyTokens(trimmed)
		return parsed, nil
	}
	if IsBoardWriteLine(trimmed) {
		parsed.Kind = CommandBoardWrite
		parsed.Tokens = legacyTokens(trimmed)
		return parsed, nil
	}
	// command9.c's ignore list is connection-local, but it still needs to
	// pass through the shared command classifier so the transport can apply
	// the authoritative online/PDMINV checks before toggling it.
	if IsIgnoreLine(trimmed) {
		parsed.Kind = CommandIgnore
		parsed.Tokens = legacyTokens(trimmed)
		return parsed, nil
	}
	if IsStealLine(trimmed) {
		parsed.Kind = CommandSteal
		parsed.Tokens = legacyTokens(trimmed)
		return parsed, nil
	}
	if IsTeachLine(trimmed) {
		parsed.Kind = CommandTeach
		parsed.Tokens = legacyTokens(trimmed)
		return parsed, nil
	}
	if IsBackstabLine(trimmed) {
		parsed.Kind = CommandBackstab
		parsed.Tokens = legacyTokens(trimmed)
		return parsed, nil
	}
	if IsDrinkLine(trimmed) {
		parsed.Kind = CommandDrink
		parsed.Tokens = legacyTokens(trimmed)
		return parsed, nil
	}
	// These combat/spell adapters have bounded multi-token forms whose first
	// token is not enough to classify them through the generic single-token
	// table. Keep the exact line contracts in their owning session files.
	if IsCircleLine(trimmed) {
		parsed.Kind = CommandCircle
		parsed.Tokens = legacyTokens(trimmed)
		return parsed, nil
	}
	if IsBashLine(trimmed) {
		parsed.Kind = CommandBash
		parsed.Tokens = legacyTokens(trimmed)
		return parsed, nil
	}
	if IsMagicStopLine(trimmed) {
		parsed.Kind = CommandMagicStop
		parsed.Tokens = legacyTokens(trimmed)
		return parsed, nil
	}
	// These bounded legacy suffix/target commands are recognized before the
	// generic first-token table. Their exact argument order is owned by the
	// command-specific session adapters, so unsupported prefix/occurrence
	// forms remain unknown instead of being routed to a partial reducer.
	if IsGiveLine(trimmed) {
		parsed.Kind = CommandGive
		parsed.Tokens = legacyTokens(trimmed)
		return parsed, nil
	}
	if IsPoisonLine(trimmed) {
		parsed.Kind = CommandPoison
		parsed.Tokens = legacyTokens(trimmed)
		return parsed, nil
	}
	if IsEnemyStatusLine(trimmed) {
		parsed.Kind = CommandEnemyStatus
		parsed.Tokens = legacyTokens(trimmed)
		return parsed, nil
	}
	if IsTrainingLine(trimmed) {
		parsed.Kind = CommandTrain
		parsed.Tokens = legacyTokens(trimmed)
		return parsed, nil
	}
	if IsSelectionLine(trimmed) {
		parsed.Kind = CommandSelection
		parsed.Tokens = legacyTokens(trimmed)
		return parsed, nil
	}
	// These bounded combat/spell commands have target-aware parsers. Keep
	// their exact argument contracts in the owning session files instead of
	// allowing the generic tokenizer to broaden prefixes or occurrences.
	if IsTurnLine(trimmed) {
		parsed.Kind = CommandTurn
		parsed.Tokens = legacyTokens(trimmed)
		return parsed, nil
	}
	if IsAbsorbLine(trimmed) {
		parsed.Kind = CommandAbsorb
		parsed.Tokens = legacyTokens(trimmed)
		return parsed, nil
	}
	if IsKickLine(trimmed) {
		parsed.Kind = CommandKick
		parsed.Tokens = legacyTokens(trimmed)
		return parsed, nil
	}
	if IsUseLine(trimmed) {
		parsed.Kind = CommandUse
		parsed.Tokens = legacyTokens(trimmed)
		return parsed, nil
	}
	if IsChangeClassLine(trimmed) {
		parsed.Kind = CommandChangeClass
		parsed.Tokens = legacyTokens(trimmed)
		return parsed, nil
	}
	// Hide has both a bare player form and a canonical floor-object form;
	// recognize it before the generic single-token gate so `숨겨 검` is not
	// downgraded to an unknown command.
	if IsHideLine(trimmed) {
		parsed.Kind = CommandHide
		parsed.Tokens = legacyTokens(trimmed)
		return parsed, nil
	}
	// Bribe is a suffix command whose first token is the NPC display name, not
	// a fixed verb. Resolve it before the ordinary first-token table.
	if IsBribeLine(trimmed) {
		parsed.Kind = CommandBribe
		parsed.Tokens = legacyTokens(trimmed)
		return parsed, nil
	}
	// Title parsing intentionally keeps exact spacing so an empty suffix such
	// as "칭호 " is not silently converted into the read-only `칭호` command.
	if IsTitleLine(line) {
		parsed.Kind = CommandTitle
		parsed.Tokens = legacyTokens(trimmed)
		if len(parsed.Tokens) > 7 {
			return ParsedCommand{}, ErrCommandTooManyTokens
		}
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
	if _, ok := ParseBroadcastLine(trimmed); ok {
		parsed.Kind = CommandBroadcast
		parsed.Tokens = legacyTokens(trimmed)
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
	if _, ok := ParseTradeLine(trimmed); ok {
		parsed.Kind = CommandTrade
		return parsed, nil
	}
	if _, ok := ParseMerchantPurchaseLine(trimmed); ok {
		parsed.Kind = CommandMerchantPurchase
		return parsed, nil
	}
	if _, ok := ParseGroupTalkLine(trimmed); ok {
		parsed.Kind = CommandGroupTalk
		return parsed, nil
	}
	if IsDescriptionLine(trimmed) {
		parsed.Kind = CommandDescription
		return parsed, nil
	}
	if IsPlayerLookupLine(trimmed) {
		parsed.Kind = CommandPlayerLookup
		return parsed, nil
	}
	if IsReturnSquareLine(trimmed) {
		parsed.Kind = CommandReturnSquare
		return parsed, nil
	}
	if IsCompareLine(trimmed) {
		parsed.Kind = CommandCompare
		return parsed, nil
	}
	if IsObjectAppraisalLine(trimmed) {
		parsed.Kind = CommandObjectAppraisal
		return parsed, nil
	}
	parsed.Kind = commandKind(tokens[0])
	// `암호` is a continuation start, not a generic single-token command.
	// Keep malformed variants containing controls (for example a newline)
	// from becoming a password operation merely because TrimSpace hid them.
	if parsed.Kind == CommandPassword && !IsPasswordLine(line) {
		parsed.Kind = CommandUnknown
	}
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
	case "누구", "그룹", "무리":
		return CommandSocial
	case "주워", "주", "가져", "꺼내", "버려", "넣어":
		return CommandItemMutation
	case "입어", "쥐어", "무장", "벗어":
		return CommandEquipment
	case "잔액", "보관물", "받아", "입금", "출금":
		return CommandBank
	case "품목":
		return CommandShopList
	case "팔아":
		return CommandShopSell
	case "사", "구입":
		return CommandShopPurchase
	case "교환":
		// The legacy parser treats the final Korean command token as str[0],
		// so trade lines are recognized by ParseTradeLine above rather than
		// by this first-token table.
		return CommandTrade
	case "가치", "가격":
		return CommandValue
	case "수리":
		return CommandRepair
	case "얘기", "이야기":
		return CommandDirectMessage
	case "대답":
		return CommandReply
	case "대화":
		return CommandNPCTalk
	case "그룹말", "무리말", "=":
		return CommandGroupTalk
	case "끝":
		return CommandQuit
	case "시간":
		return CommandRead
	case "저장", "save":
		return CommandSave
	case "편지받기", "편지삭제":
		return CommandMail
	case "편지보내기":
		return CommandMailSend
	case "메모":
		return CommandMemo
	case "게시판":
		return CommandBoard
	case "결혼":
		return CommandMarriage
	case "사랑말":
		return CommandMarriageSend
	case "이혼":
		return CommandDivorce
	case "투표":
		return CommandVote
	case "써":
		return CommandBoardWrite
	case IgnoreCommandName:
		return CommandIgnore
	case "훔쳐":
		return CommandSteal
	case "가르쳐":
		return CommandTeach
	case "기습":
		return CommandBackstab
	case "먹어", "마셔":
		return CommandDrink
	case "정보":
		return CommandInfo
	case "암호":
		return CommandPassword
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
	case "도망", "도":
		return CommandFlee
	case "엿봐":
		return CommandPeek
	case "설정", "해제":
		return CommandSettings
	case "열어", "닫아":
		return CommandDoor
	case "풀어", "잠궈", "따":
		return CommandDoorKey
	default:
		if world.IsEmoteAlias(first) {
			return CommandEmote
		}
		return CommandUnknown
	}
}

func isSingleTokenKind(kind CommandKind) bool {
	switch kind {
	case CommandLook, CommandStatus, CommandItems, CommandSocial, CommandQuit, CommandRead, CommandSave, CommandMail, CommandInfo, CommandPassword, CommandWelcome, CommandSearch, CommandTrack, CommandHide, CommandFlee, CommandShopList, CommandIgnore, CommandVote:
		return true
	default:
		return false
	}
}
