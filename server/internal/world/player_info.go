package world

import (
	"fmt"
	"strings"
)

// These labels are the immutable tables used by command4.c. They are kept
// here only for the deterministic first page of PlayerInfo; title_ply is not
// reproduced because the current canonical player model has no stored title.
var legacyInfoClassNames = [...]string{
	"제작", "자객", "권법가", "불제자", "검사", "도술사", "무사",
	"포졸", "도둑", "무적", "초인", "운영자", "관리자",
}

var legacyInfoRaceNames = [...]string{
	"Unknown", "난장이족", "용신족", "요괴족", "토신족",
	"인간족", "도깨비족", "거인족", "땅귀신족", "개구리족",
}

// PlayerInfo renders the deterministic first page of command4.c's info().
// It reads only canonical State and never updates timers, flags, equipment or
// prompts. The legacy title and info_2 spell continuation are deliberately not
// included: neither has an admitted canonical projection at this boundary.
func (s State) PlayerInfo(actorID string) (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	p, ok := s.Players[actorID]
	if !ok || !p.Online || p.Items == nil {
		return "", fmt.Errorf("online player with migrated items required")
	}
	if p.Body.Level == 0 || int(p.Body.Class) >= len(legacyInfoClassNames) || int(p.Body.Race) >= len(legacyInfoRaceNames) {
		return "", fmt.Errorf("player info has unsupported class, race or level")
	}
	if p.Body.Experience < 0 || p.Body.Gold < 0 || p.Body.Timers[28].Interval < 0 {
		return "", fmt.Errorf("player info has negative legacy value")
	}

	weight, err := p.Items.Weight()
	if err != nil {
		return "", err
	}
	count, err := p.Items.CapacityCount()
	if err != nil {
		return "", err
	}
	if weight < 0 {
		return "", fmt.Errorf("player info has negative carried weight")
	}

	weapon := [5]int{}
	for i, value := range p.Body.Proficiency {
		weapon[i], err = WeaponProficiency(p.Body.Class, value)
		if err != nil {
			return "", err
		}
	}
	magic := [4]int{}
	for i, value := range p.Body.Realm {
		magic[i], err = magicProficiency(p.Body.Class, value)
		if err != nil {
			return "", err
		}
	}

	need := int64(ExperienceThreshold(p.Body.Level)) - int64(p.Body.Experience)
	if need < 0 {
		need = 0
	}
	alignment := "평범합니다"
	if p.Body.Alignment < -100 {
		alignment = "악합니다"
	} else if p.Body.Alignment > 100 {
		alignment = "선합니다"
	}
	ethos := "선"
	if legacyInfoFlag(p.Body.Flags[:], 28) {
		ethos = "악"
	}

	var out strings.Builder
	fmt.Fprintf(&out, "\n[이름] %s\n", p.Body.Name)
	fmt.Fprintf(&out, "[레벨] %d [종족] %s\n", p.Body.Level, legacyInfoRaceNames[p.Body.Race])
	fmt.Fprintf(&out, "[직업] %s [성향] %s (%s)\n", legacyInfoClassNames[p.Body.Class], ethos, alignment)
	fmt.Fprintf(&out, "접속시간 : %s\n\n", legacyInfoDuration(int64(p.Body.Timers[28].Interval)))
	fmt.Fprintf(&out, "[힘] %-2d [민첩] %-2d [맷집] %-2d\n", legacyInfoStat(p.Body.Stats[0]), legacyInfoStat(p.Body.Stats[1]), legacyInfoStat(p.Body.Stats[2]))
	fmt.Fprintf(&out, "[지식] %-2d [신앙심] %-2d\n\n", legacyInfoStat(p.Body.Stats[3]), legacyInfoStat(p.Body.Stats[4]))
	fmt.Fprintf(&out, "[체력] %-5d/%-5d          [경험치] %d ( %d의 경험치가 필요합니다. )\n", p.Body.HPCurrent, p.Body.HPMax, p.Body.Experience, need)
	fmt.Fprintf(&out, "[도력] %-5d/%-5d          [돈] %-7d\n", p.Body.MPCurrent, p.Body.MPMax, p.Body.Gold)
	fmt.Fprintf(&out, "[방어력] %-5d                [소지품 무게] %d 근 (총 %d개).\n\n", 100-int(int8(p.Body.Armor)), weight, count)

	out.WriteString("## 무기사용능력 ##\n\n")
	fmt.Fprintf(&out, "[ 도 ] %2d%%         [ 검 ] %2d%%         [ 봉 ] %2d%%\n", weapon[0], weapon[1], weapon[2])
	fmt.Fprintf(&out, "[ 창 ] %2d%%         [ 궁 ] %2d%%\n\n", weapon[3], weapon[4])
	out.WriteString("## 주 술 계 열 ##\n")
	fmt.Fprintf(&out, "[ 땅 ] %2d%%      [바람] %2d%%    [ 불 ] %2d%%   [ 물 ] %2d%%\n\n", magic[0], magic[1], magic[2], magic[3])
	return out.String(), nil
}

func legacyInfoDuration(seconds int64) string {
	var out strings.Builder
	if seconds > 86400 {
		fmt.Fprintf(&out, "%d일 ", seconds/86400)
	}
	if seconds > 3600 {
		fmt.Fprintf(&out, "%d시간 ", (seconds%86400)/3600)
	}
	fmt.Fprintf(&out, "%d분", (seconds%3600)/60)
	return out.String()
}

func legacyInfoFlag(bits []byte, n uint) bool {
	return n/8 < uint(len(bits)) && bits[n/8]&(1<<(n%8)) != 0
}

func legacyInfoStat(value byte) int {
	return int(int8(value))
}

func magicProficiency(class byte, xp int32) (int, error) {
	var thresholds [12]int64
	switch class {
	case 5, 9, 10, 11, 12: // MAGE, INVINCIBLE, CARETAKER, SUB_DM, DM
		thresholds = [12]int64{0, 1024, 2048, 4096, 8192, 16384, 35768, 85536, 140000, 459410, 2073306, 500000000}
	case 3: // CLERIC
		thresholds = [12]int64{0, 1024, 4092, 8192, 16384, 32768, 70536, 119000, 226410, 709410, 2973307, 500000000}
	case 6, 7: // PALADIN, RANGER
		thresholds = [12]int64{0, 1024, 8192, 16384, 32768, 65536, 105000, 165410, 287306, 809410, 3538232, 500000000}
	case 1, 2, 4, 8: // ASSASSIN, BARBARIAN, FIGHTER, THIEF
		thresholds = [12]int64{0, 1024, 40000, 80000, 120000, 160000, 205000, 222000, 380000, 965410, 5495000, 500000000}
	default:
		return 0, fmt.Errorf("invalid magic proficiency class")
	}
	if xp < 0 || xp >= 500000000 {
		return 0, fmt.Errorf("magic proficiency outside defined legacy range")
	}
	for i := 0; i < 11; i++ {
		if int64(xp) < thresholds[i+1] {
			return 10*i + int((int64(xp)-thresholds[i])*10/(thresholds[i+1]-thresholds[i])), nil
		}
	}
	return 0, fmt.Errorf("unreachable magic proficiency interval")
}
