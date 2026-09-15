package world

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Explicit inputs to the environment portion of display_rom. Dynamic players,
// NPCs and items are separate sections. Combat-in-progress notices are
// appended by CurrentScene/SceneAt after these non-combat blocks.
// Time/light are supplied by the world loop. command2.c look-through-exit
// uses the same filters via SceneAt.
type ViewOptions struct {
	Hour                                                        int
	Race, Class                                                 byte
	Blind, HasLight, OtherPlayerLight                           bool
	HideName, HideShort, HideLong, ExitDiagram, DetectInvisible bool
}

func flag(bits []byte, n uint) bool { return bits[n/8]&(1<<(n%8)) != 0 }

func roomVisible(room LegacyRoom, v ViewOptions) bool {
	dark := flag(room.Flags[:], 8) || (flag(room.Flags[:], 9) && (v.Hour%24 < 6 || v.Hour%24 > 20))
	light := v.HasLight || v.OtherPlayerLight || v.Race == 1 || v.Race == 2 || v.Class >= 10
	return !v.Blind && (!dark || light)
}

// RenderRoomEnvironment returns semantic UTF-8 without ANSI styling. Terminal
// wire formatting is separate. This does not authorize room admission.
func RenderRoomEnvironment(room LegacyRoom, v ViewOptions) string {
	var out strings.Builder
	out.WriteByte('\n')
	if !roomVisible(room, v) {
		if v.Blind {
			out.WriteString("당신은 눈이 멀어 아무것도 볼 수 없습니다.\n")
		}
		out.WriteString("너무 어두워서 볼 수가 없습니다.\n")
		return out.String()
	}
	if !v.HideName {
		out.WriteString("== " + strings.TrimRight(room.Name, " ") + " ==\n\n")
	}
	if !v.HideShort && (room.ShortDescription != "" || room.ShortDescriptionPresent) {
		out.WriteString(room.ShortDescription + "\n")
	}
	if !v.HideLong && (room.LongDescription != "" || room.LongDescriptionPresent) {
		out.WriteString(room.LongDescription + "\n")
	}
	var exits []string
	for _, exit := range room.Exits {
		if !flag(exit.Flags[:], 0) && !flag(exit.Flags[:], 19) && (v.DetectInvisible || !flag(exit.Flags[:], 1)) {
			exits = append(exits, exit.Name)
		}
	}
	if !v.ExitDiagram {
		if len(exits) == 0 {
			out.WriteString("[ 출구 : 없음  ]\n")
		} else {
			out.WriteString("[ 출구 : " + strings.Join(exits, ", ") + " ]\n")
		}
		return out.String()
	}
	if len(exits) == 0 {
		out.WriteString("[ 출구 : 없음 ]\n")
		return out.String()
	}
	rows := [3][]rune{[]rune("       "), []rune("   O   "), []rune("       ")}
	var other []string
	for _, name := range exits {
		switch name {
		case "동":
			rows[1][5] = '-'
			rows[1][6] = '-'
		case "서":
			rows[1][0] = '-'
			rows[1][1] = '-'
		case "남":
			rows[2][3] = '|'
		case "북":
			rows[0][3] = '|'
		case "위":
			rows[0] = append(rows[0][:5], '위')
		case "밑":
			rows[2] = append(rows[2][:5], '밑')
		default:
			other = append(other, name)
		}
	}
	out.WriteString("[ " + string(rows[0]) + " ]\n[ " + string(rows[1]) + " ]")
	if len(other) > 0 {
		out.WriteString(" [ 출구 : " + strings.Join(other, ", ") + " ]")
	}
	out.WriteString("\n[ " + string(rows[2]) + " ]\n")
	return out.String()
}

// RenderRoomMonsters ports the description section of display_rom, preserving
// adjacent-name grouping and the LAST grouped monster's description/alignment.
// Zero alignment prints blue when aura detection is enabled, as in the source.
// Call only after the shared visibility check; darkness is not rechecked here.
func RenderRoomMonsters(monsters []LegacyMonster, detectInvisible, knowAlignment bool) string {
	visible := func(m LegacyMonster) bool { return !flag(m.Flags[:], 1) && (detectInvisible || !flag(m.Flags[:], 2)) }
	var out strings.Builder
	for i := 0; i < len(monsters); i++ {
		if !visible(monsters[i]) {
			continue
		}
		count := 1
		for i+1 < len(monsters) && monsters[i+1].Name == monsters[i].Name && visible(monsters[i+1]) {
			i++
			count++
		}
		if count > 1 {
			fmt.Fprintf(&out, "(x%d) ", count)
		}
		out.WriteString(monsters[i].Description)
		if knowAlignment {
			if monsters[i].Alignment < 0 {
				out.WriteString(" (붉은 광채)")
			} else {
				out.WriteString(" (푸른 광채)")
			}
		}
		out.WriteByte('\n')
	}
	return out.String()
}

// display_rom combat notices use %M%j after list_obj. Monster %M is the
// name plus optional (주문); player %M is name, optional (*), then 님.
func combatMonsterLabel(name string, magic bool) string {
	if !magic {
		return name
	}
	return name + "(주문)"
}

func combatPlayerLabel(name string, invisible bool) string {
	if invisible {
		return name + "(*)님"
	}
	return name + "님"
}

// combatHangulParticle matches io.c %j / under_han: 511-byte cap and a
// trailing parenthesized suffix is stripped before the last Hangul jongseong
// test. Invalid UTF-8 takes the open-syllable particle.
func combatHangulParticle(text, withFinal, withoutFinal string) string {
	if len(text) > 511 {
		text = text[:511]
	}
	if strings.HasSuffix(text, ")") {
		if at := strings.LastIndexByte(text, '('); at >= 0 {
			text = text[:at]
		}
	}
	if utf8.ValidString(text) {
		last, _ := utf8.DecodeLastRuneInString(text)
		if last >= 0xac00 && last <= 0xd7a3 && (last-0xac00)%28 != 0 {
			return withFinal
		}
	}
	return withoutFinal
}
