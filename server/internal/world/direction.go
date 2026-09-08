package world

import "strings"

// ParseDirectionalToken is the narrow command-dispatch boundary for direct
// movement. C command() tokenizes a line and process_cmd dispatches the first
// token; it does not let later arguments change the selected handler. The
// returned value is an authoritative Korean exit name, never a room ID.
func ParseDirectionalToken(line string) (string, bool) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "", false
	}
	switch fields[0] {
	case "2", "ㄴ", "남":
		return "남", true
	case "4", "ㅅ", "서":
		return "서", true
	case "6", "ㄷ", "동":
		return "동", true
	case "8", "ㅂ", "북":
		return "북", true
	case "3", "ㅁ", "밑":
		return "밑", true
	case "9", "ㅇ", "위":
		return "위", true
	case "나가", "밖":
		return "밖", true
	case "북동", "ㅂㄷ", "북서", "ㅂㅅ", "남동", "ㄴㄷ", "남서", "ㄴㅅ":
		return fields[0], true
	default:
		return "", false
	}
}

// SelectDirectionalExit ports command2.c move's exact-name lookup, not go's
// prefix/occurrence lookup. Input is the parsed first command token. Legacy
// non-UTF-8 arrow-key literals require a separate transport decoding contract.
func SelectDirectionalExit(exits []LegacyExit, token string) int {
	if token == "" {
		return -1
	}
	name := token
	switch token[0] {
	case '2':
		name = "남"
	case '4':
		name = "서"
	case '6':
		name = "동"
	case '8':
		name = "북"
	case '3':
		name = "밑"
	case '9':
		name = "위"
	}
	switch token {
	case "ㄴ":
		name = "남"
	case "ㅅ":
		name = "서"
	case "ㄷ":
		name = "동"
	case "ㅂ":
		name = "북"
	case "ㅁ":
		name = "밑"
	case "ㅇ":
		name = "위"
	case "나가":
		name = "밖"
	}
	for i, e := range exits {
		if e.Name == name && !flag(e.Flags[:], 19) {
			return i
		}
	}
	return -1
}

// DirectionalExitRestriction is ONLY move's prefix through XDAYON. Guards,
// falls, destination restrictions, followers, traps and death still follow.
func DirectionalExitRestriction(e LegacyExit, v PassageOptions) string {
	switch {
	case v.Immobile:
		return "당신은 움직일수 없습니다."
	case v.Fighting:
		return "싸우는 중에는 이동할 수 없습니다."
	case flag(e.Flags[:], 2):
		return "문이 잠겨 있습니다."
	case flag(e.Flags[:], 3):
		return "문이 닫혀 있습니다."
	case flag(e.Flags[:], 11) && !v.Flying:
		return "그 쪽으로는 날아서 가야 될것 같군요."
	case flag(e.Flags[:], 16) && v.Hour%24 > 6 && v.Hour%24 < 20:
		return "그 출구는 밤에만 열려 있습니다."
	case flag(e.Flags[:], 17) && (v.Hour%24 < 6 || v.Hour%24 > 20):
		return "그 출구는 밤에는 닫혀 있습니다."
	default:
		return ""
	}
}
