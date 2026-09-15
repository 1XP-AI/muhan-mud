package world

import (
	"fmt"
	"sort"
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

// legacyInfoSpellNames is the immutable command4.c spllist projection for
// spell bits 0..55. Keep indices aligned with src/global.c; info_2 sorts the
// selected names by strcmp before rendering them.
var legacyInfoSpellNames = [...]string{
	"회복", "삭풍", "발광", "해독", "성현진", "수호진", "화궁", "은둔법",
	"도력반", "은둔감지술", "주문감지술", "축지법", "혼동", "뇌전", "동설주", "빙의",
	"귀환", "소환", "원기회복", "완치", "추적", "부양술", "방열진", "비상술",
	"보마진", "권풍술", "지동술", "화선도", "탄수공", "풍마현", "파초식", "폭진",
	"낙석", "화풍술", "화룡대천", "토합술", "주작현", "열사천", "파천풍", "지옥패",
	"태양안", "선악감지", "저주해소", "방한진", "수생술", "지방호", "천리안", "백치술",
	"치료", "개안술", "공포", "전회복", "전송", "실명", "봉합구", "이혼대법",
}

type legacyInfoEffect struct {
	bit  uint
	name string
}

// legacyInfoEffects preserves info_2's fixed F_ISSET order and Korean labels.
var legacyInfoEffects = [...]legacyInfoEffect{
	{bit: 0, name: "성현진"},
	{bit: 17, name: "발광"},
	{bit: 8, name: "수호진"},
	{bit: 2, name: "은둔법"},
	{bit: 21, name: "은둔감지"},
	{bit: 20, name: "주문감지"},
	{bit: 25, name: "부양술"},
	{bit: 30, name: "방열진"},
	{bit: 31, name: "비상술"},
	{bit: 32, name: "보마진"},
	{bit: 33, name: "선악감지"},
	{bit: 36, name: "방한진"},
	{bit: 37, name: "수생술"},
	{bit: 38, name: "지방호"},
}

// PlayerInfo renders the deterministic first page of command4.c's info().
// It reads only canonical State and never updates timers, flags, equipment or
// prompts. The legacy title remains unprojected because the canonical player
// model has no stored title.
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

// PlayerInfoContinuation renders command4.c's info_2 page from the same
// canonical snapshot contract as PlayerInfo. It is pure: the spell/effect and
// quest projections read only actor fields and never mutate State.
func (s State) PlayerInfoContinuation(actorID string) (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	p, ok := s.Players[actorID]
	if !ok || !p.Online {
		return "", fmt.Errorf("online player required")
	}

	spells := make([]string, 0, len(legacyInfoSpellNames))
	for i, name := range legacyInfoSpellNames {
		if legacyInfoFlag(p.Body.Spells[:], uint(i)) {
			spells = append(spells, name)
		}
	}
	sort.Strings(spells)

	var out strings.Builder
	out.WriteString("\n주문: ")
	if len(spells) == 0 {
		out.WriteString("없음.")
	} else {
		out.WriteString(strings.Join(spells, ", "))
		out.WriteByte('.')
	}
	out.WriteByte('\n')

	out.WriteString("당신의 현주문: ")
	effects := make([]string, 0, len(legacyInfoEffects))
	for _, effect := range legacyInfoEffects {
		if legacyInfoFlag(p.Body.Flags[:], effect.bit) {
			effects = append(effects, effect.name)
		}
	}
	if len(effects) == 0 {
		out.WriteString("없음.")
	} else {
		out.WriteString(strings.Join(effects, ", "))
		out.WriteByte('.')
	}
	out.WriteByte('\n')

	quest := 0
	for quest < len(p.Body.Quests)*8 && legacyInfoFlag(p.Body.Quests[:], uint(quest)) {
		quest++
	}
	if quest == 0 {
		out.WriteString("당신은 현재 달성한 임무가 없습니다.")
	} else {
		fmt.Fprintf(&out, "당신은 현재 임무 %d까지 달성하였습니다.", quest)
	}
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
