package world

import "strings"

// SelectExit ports find_ext's positive, one-based occurrence lookup. Secret
// exits can be selected by name even though they are absent from room display.
// Intentional correction: empty prefixes and nonpositive occurrences cannot
// select an unrelated first exit (the C match==0 path allowed that).
func SelectExit(exits []LegacyExit, prefix string, occurrence int, detectInvisible bool) int {
	if prefix == "" || occurrence < 1 {
		return -1
	}
	for i, e := range exits {
		if strings.HasPrefix(e.Name, prefix) && !flag(e.Flags[:], 19) && (detectInvisible || !flag(e.Flags[:], 1)) {
			occurrence--
			if occurrence == 0 {
				return i
			}
		}
	}
	return -1
}

type PassageOptions struct {
	Immobile, Fighting, Flying bool
	Hour                       int
}

// ExitRestriction implements ONLY go()'s ordered prefix through XDAYON.
// An empty result is not movement authorization: guards, carried weight, sex,
// climbing/falls, cooldowns, kingdom/race checks, destination admission, track,
// followers and transactional state changes still follow in the legacy path.
func ExitRestriction(e LegacyExit, v PassageOptions) string {
	switch {
	case v.Immobile:
		return "당신은 움직일 수가 없습니다."
	case v.Fighting:
		return "싸우는 중에는 이동할 수 없습니다."
	case flag(e.Flags[:], 2):
		return "그 출구는 잠겨 있습니다."
	case flag(e.Flags[:], 3):
		return "그 출구는 닫혀 있습니다."
	case flag(e.Flags[:], 11) && !v.Flying:
		return "그곳에는 날아서만 갈 수 있습니다."
	case flag(e.Flags[:], 16) && v.Hour%24 > 6 && v.Hour%24 < 20:
		return "그 출구는 밤에만 갈 수 있습니다."
	case flag(e.Flags[:], 17) && (v.Hour%24 < 6 || v.Hour%24 > 20):
		return "그 출구로는 낮에만 갈 수 있습니다."
	default:
		return ""
	}
}
