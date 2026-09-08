package world

import (
	"fmt"
	"strconv"
	"strings"
)

// SettingsProposal is the checked candidate for command5.c:set/clear. The
// legacy command mutates only the actor's player flags (and WIMPYVALUE), so
// the proposal carries the expected values needed to reject a stale plan.
// It never carries a client-selected bit number: keys are resolved from the
// closed source-backed tables below.
type SettingsProposal struct {
	ActorID          string
	Action           string
	Key              string
	NoArgument       bool
	Known            bool
	Kind             string
	Bit              int16
	DesiredFlag      bool
	ExpectedFlag     bool
	DesiredValue     int32
	ExpectedValue    int32
	ExpectedValueSet bool
	FamilyMember     bool
}

// SettingsResult is the actor-facing response committed in the durable
// receipt. Settings have no room event; other clients learn about them only
// through their own subsequent views, matching the source command.
type SettingsResult struct {
	Response string `json:"response"`
}

const (
	settingsSet     = "set"
	settingsClear   = "clear"
	settingsWimpy   = "wimpy"
	settingsFamily  = "family-return"
	settingsFlag    = "flag"
	settingsSpecial = "special"
)

// These are the ordinary toggles in command5.c:set. Their values are the
// legacy player flag numbers from src/mtype.h, not a new Go numbering.
var settingsSetToggles = map[string]uint{
	"이야기듣기": 35, // PIGNOR
	"잡담듣기":  3,  // PNOBRD
	"환호듣기":  50, // PNOBR2
	"묘사보기":  63, // PDSCRP
	"소환":    34, // PNOSUM
	"행삽입":   11, // PNOCMP
	"상태":    18, // PPROMP
	"반향":    46, // PLECHO
	"색":     26, // PANSIC
	"밝은색":   51, // PBRIGH
	"방이름":   6,  // PNORNM
	"짧은설명":  5,  // PNOSDS
	"긴설명":   4,  // PNOLDS
	"출구":    7,  // PNOEXT
}

var settingsSpecialBits = map[string]uint{
	"hexline":      13, // PHEXLN
	"eavesdropper": 15, // PEAVES
	"~robot~":      23, // PROBOT
	"수동공격":         9,  // PNOAAT
}

// clear uses user-facing names that are intentionally not all the same as
// set. For example, C's "방이름" means suppress the room name and therefore
// sets PNORNM; "잡담듣기거부" clears PNOBRD.
var settingsClearBits = map[string]struct {
	bit     uint
	desired bool
}{
	"잡담듣기거부":       {bit: 3, desired: false},  // PNOBRD
	"행삽입":          {bit: 11, desired: false}, // PNOCMP
	"반향":           {bit: 46, desired: false}, // PLECHO
	"방이름":          {bit: 6, desired: true},   // PNORNM
	"간단":           {bit: 5, desired: true},   // PNOSDS
	"일반":           {bit: 4, desired: true},   // PNOLDS
	"hexline":      {bit: 13, desired: false}, // PHEXLN
	"도망수치":         {bit: 14, desired: false}, // PWIMPY
	"eavesdropper": {bit: 15, desired: false}, // PEAVES
	"상태":           {bit: 18, desired: false}, // PPROMP
	"~robot~":      {bit: 23, desired: false}, // PROBOT
	"색":            {bit: 26, desired: false}, // PANSIC
	"소환거부":         {bit: 34, desired: false}, // PNOSUM
	"이야기듣기거부":      {bit: 35, desired: false}, // PIGNOR
	"수동공격":         {bit: 9, desired: false},  // PNOAAT
	"밝은색":          {bit: 51, desired: false}, // PBRIGH
	"패거리귀환":        {bit: 58, desired: false}, // PFRTUN
}

func settingBitSet(body LegacyMonster, bit uint) bool {
	return flag(body.Flags[:], bit)
}

func settingsActor(s State, actorID string) (PlayerState, error) {
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Body.Type != 0 || actor.Body.Name == "" {
		return PlayerState{}, fmt.Errorf("online settings actor absent")
	}
	if _, ok := s.Rooms[actor.Body.RoomID]; !ok {
		return PlayerState{}, fmt.Errorf("settings actor room absent")
	}
	return actor, nil
}

// PlanSettings resolves the original set/clear vocabulary and captures the
// exact precondition required by ApplySettings. Unknown set keys intentionally
// remain a read-only flag-list response because command5.c:set does the same;
// unknown clear keys are rejected by its explicit error branch.
func (s State) PlanSettings(actorID, action, key string, value *int32) (SettingsProposal, error) {
	if err := s.Validate(); err != nil {
		return SettingsProposal{}, err
	}
	if action != settingsSet && action != settingsClear {
		return SettingsProposal{}, fmt.Errorf("unknown settings action")
	}
	actor, err := settingsActor(s, actorID)
	if err != nil {
		return SettingsProposal{}, err
	}
	if strings.TrimSpace(key) != key {
		return SettingsProposal{}, fmt.Errorf("invalid settings key")
	}
	proposal := SettingsProposal{ActorID: actorID, Action: action, Key: key}
	if key == "" {
		proposal.NoArgument = true
		return proposal, nil
	}

	if action == settingsSet {
		if bit, ok := settingsSetToggles[key]; ok {
			proposal.Known, proposal.Kind, proposal.Bit = true, settingsFlag, int16(bit)
			proposal.ExpectedFlag = settingBitSet(actor.Body, bit)
			proposal.DesiredFlag = !proposal.ExpectedFlag
			return proposal, nil
		}
		if key == "도망수치" {
			v := int32(0)
			if value != nil {
				v = *value
			}
			if v == 1 {
				v = 10
			} else if v < 2 && v != 0 {
				v = 2
			}
			proposal.Known, proposal.Kind, proposal.Bit = true, settingsWimpy, 14
			proposal.ExpectedFlag = settingBitSet(actor.Body, 14)
			proposal.ExpectedValueSet = true
			proposal.ExpectedValue = actor.Body.WimpyValue
			proposal.DesiredValue = v
			proposal.DesiredFlag = v != 0
			return proposal, nil
		}
		if key == "패거리귀환" {
			proposal.Known, proposal.Kind, proposal.Bit = true, settingsFamily, 58
			proposal.FamilyMember = settingBitSet(actor.Body, 55) // PFAMIL
			proposal.ExpectedFlag = settingBitSet(actor.Body, 58)
			proposal.DesiredFlag = proposal.FamilyMember
			return proposal, nil
		}
		if bit, ok := settingsSpecialBits[key]; ok {
			proposal.Known, proposal.Kind, proposal.Bit = true, settingsSpecial, int16(bit)
			proposal.ExpectedFlag = settingBitSet(actor.Body, bit)
			proposal.DesiredFlag = true
			return proposal, nil
		}
		return proposal, nil // source set() falls through to flag_list
	}

	if key == "도망수치" {
		entry := settingsClearBits[key]
		proposal.Known, proposal.Kind, proposal.Bit = true, settingsWimpy, int16(entry.bit)
		proposal.ExpectedFlag = settingBitSet(actor.Body, entry.bit)
		proposal.ExpectedValueSet = true
		proposal.ExpectedValue = actor.Body.WimpyValue
		proposal.DesiredFlag = entry.desired
		proposal.DesiredValue = actor.Body.WimpyValue // C clear leaves WIMPYVALUE untouched.
		return proposal, nil
	}
	if entry, ok := settingsClearBits[key]; ok {
		proposal.Known, proposal.Kind, proposal.Bit = true, settingsFlag, int16(entry.bit)
		proposal.ExpectedFlag = settingBitSet(actor.Body, entry.bit)
		proposal.DesiredFlag = entry.desired
		return proposal, nil
	}
	return proposal, nil
}

func setSettingFlag(body *LegacyMonster, bit uint, enabled bool) {
	if enabled {
		body.Flags[bit/8] |= 1 << (bit % 8)
	} else {
		body.Flags[bit/8] &^= 1 << (bit % 8)
	}
}

func settingsFlagList(body LegacyMonster) string {
	var out strings.Builder
	out.WriteString("현재 설정 상태:\n")
	f := func(bit uint) bool { return settingBitSet(body, bit) }
	fmt.Fprintf(&out, "  이야기듣기: %s\n", map[bool]string{true: "미설정", false: " 설정 "}[f(35)])
	fmt.Fprintf(&out, "  잡담듣기  : %s\n", map[bool]string{true: "미설정", false: " 설정 "}[f(3)])
	fmt.Fprintf(&out, "  환호듣기  : %s\n", map[bool]string{true: "미설정", false: " 설정 "}[f(50)])
	fmt.Fprintf(&out, "  묘사보기  : %s\n", map[bool]string{true: " 설정 ", false: "미설정"}[f(63)])
	fmt.Fprintf(&out, "  소환      : %s\n", map[bool]string{true: " 불가 ", false: " 가능 "}[f(34)])
	out.WriteString("  도망수치  : ")
	if f(14) {
		fmt.Fprintf(&out, "%-6d\n", body.WimpyValue)
	} else {
		out.WriteString("미설정\n")
	}
	fmt.Fprintf(&out, "  행삽입    : %s\n", map[bool]string{true: " 설정 ", false: "미설정"}[f(11)])
	fmt.Fprintf(&out, "  상태      : %s\n", map[bool]string{true: " 출력 ", false: "미설정"}[f(18)])
	fmt.Fprintf(&out, "  반향      : %s\n", map[bool]string{true: " 설정 ", false: "미설정"}[f(46)])
	fmt.Fprintf(&out, "  색        : %s\n", map[bool]string{true: " 사용 ", false: "미사용"}[f(26)])
	fmt.Fprintf(&out, "  밝은색    : %s\n", map[bool]string{true: " 사용 ", false: "미사용"}[f(51)])
	fmt.Fprintf(&out, "  방이름    : %s\n", map[bool]string{true: "미설정", false: " 출력 "}[f(6)])
	fmt.Fprintf(&out, "  짧은설명  : %s\n", map[bool]string{true: "미설정", false: " 출력 "}[f(5)])
	fmt.Fprintf(&out, "  긴설명    : %s\n", map[bool]string{true: "미설정", false: " 출력 "}[f(4)])
	fmt.Fprintf(&out, "  출구      : %s\n", map[bool]string{true: "그래프", false: "텍스트"}[f(7)])
	fmt.Fprintf(&out, "  패거리귀환: %s\n", map[bool]string{true: " 설정 ", false: "미설정"}[f(58)])
	out.WriteString("[설정 도움말]이라고 치시면 자세한 설정사항을 볼 수 있습니다.\n")
	return out.String()
}

func settingsClearResponse(key string) string {
	switch key {
	case "잡담듣기거부":
		return "이제부터 잡담을 듣습니다.\n"
	case "행삽입":
		return "이제부터 메세지를 출력할때 한행을 삽입하지 않습니다.\n"
	case "반향":
		return "당신의 메세지가 반향되지 않습니다.\n"
	case "방이름":
		return "이제부터 방이름을 출력하지 않습니다.\n"
	case "간단":
		return "방의 간단한 설명을 보지 않습니다.\n"
	case "일반":
		return "방의 자세한 설명을 보지 않습니다.\n"
	case "hexline":
		return "Hex line disabled.\n"
	case "도망수치":
		return "도망수치 설정이 해제되었습니다.\n"
	case "eavesdropper":
		return "Eavesdropper mode disabled.\n"
	case "상태":
		return "당신의 상태를 보여주지 않습니다.\n"
	case "~robot~":
		return "Robot mode off.\n"
	case "색":
		return "이제부터 메세지가 모두 흑백으로 출력됩니다.\n"
	case "소환거부":
		return "이제부터 다른사람이 당신을 소환할 수 있습니다.\n"
	case "이야기듣기거부":
		return "이제부터 개인적인 이야기를 듣습니다.\n"
	case "수동공격":
		return "이제부터 자동으로 공격합니다.\n"
	case "밝은색":
		return "어두운 색으로 출력합니다.\n"
	case "패거리귀환":
		return "광장으로 귀환합니다.\n"
	default:
		return "잘못 지정되었습니다.\n"
	}
}

func settingsSpecialResponse(key string) string {
	switch key {
	case "hexline":
		return "Hexline enabled.\n"
	case "eavesdropper":
		return "Eavesdropper mode enabled.\n"
	case "~robot~":
		return "Robot mode on.\n"
	case "수동공격":
		return "수동으로 공격합니다.\n"
	default:
		return ""
	}
}

// ApplySettings rechecks the actor and every captured precondition before it
// changes a bit. The resulting response is computed from the committed
// candidate, so receipt replay is a pure response replay and never toggles a
// flag a second time.
func (s State) ApplySettings(proposal SettingsProposal) (State, SettingsResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, SettingsResult{}, err
	}
	actor, err := settingsActor(s, proposal.ActorID)
	if err != nil {
		return State{}, SettingsResult{}, err
	}
	if proposal.Action != settingsSet && proposal.Action != settingsClear {
		return State{}, SettingsResult{}, fmt.Errorf("unknown settings action")
	}
	if proposal.NoArgument {
		if proposal.Key != "" {
			return State{}, SettingsResult{}, fmt.Errorf("invalid settings no-argument proposal")
		}
		if proposal.Action == settingsSet {
			return s, SettingsResult{Response: settingsFlagList(actor.Body)}, nil
		}
		return s, SettingsResult{Response: "[해제 도움말]이라고 치시면 모든 설정사항들을 볼 수 있습니다.\n"}, nil
	}
	if proposal.Action == settingsClear && !proposal.Known {
		return s, SettingsResult{Response: settingsClearResponse(proposal.Key)}, nil
	}
	if proposal.Action == settingsSet && !proposal.Known {
		return s, SettingsResult{Response: settingsFlagList(actor.Body)}, nil
	}
	if proposal.Bit < 0 || proposal.Bit >= 64 || proposal.Kind == "" {
		return State{}, SettingsResult{}, fmt.Errorf("invalid settings proposal")
	}
	bit := uint(proposal.Bit)
	if proposal.ExpectedFlag != settingBitSet(actor.Body, bit) {
		return State{}, SettingsResult{}, fmt.Errorf("stale settings flag proposal")
	}
	if proposal.ExpectedValueSet && proposal.ExpectedValue != actor.Body.WimpyValue {
		return State{}, SettingsResult{}, fmt.Errorf("stale settings value proposal")
	}
	if proposal.Kind == settingsFamily && proposal.FamilyMember != settingBitSet(actor.Body, 55) {
		return State{}, SettingsResult{}, fmt.Errorf("stale family membership")
	}

	next := s.clone()
	updated := next.Players[proposal.ActorID]
	if proposal.Kind == settingsWimpy {
		updated.Body.WimpyValue = proposal.DesiredValue
	}
	if proposal.Kind == settingsFamily && !proposal.FamilyMember {
		// C prints the membership warning but still emits flag_list for set.
		return s, SettingsResult{Response: "당신은 패거리에 가입되어 있지 않습니다.\n" + settingsFlagList(actor.Body)}, nil
	}
	setSettingFlag(&updated.Body, bit, proposal.DesiredFlag)
	next.Players[proposal.ActorID] = updated
	if err := next.Validate(); err != nil {
		return State{}, SettingsResult{}, err
	}

	if proposal.Action == settingsSet {
		prefix := settingsSpecialResponse(proposal.Key)
		if proposal.Kind == settingsFamily {
			prefix = "패거리 존으로 귀환을 합니다.\n"
		}
		return next, SettingsResult{Response: prefix + settingsFlagList(updated.Body)}, nil
	}
	return next, SettingsResult{Response: settingsClearResponse(proposal.Key)}, nil
}

// ParseSettingsValue is kept small and strict so command parsing cannot turn
// a malformed numeric setting into an accidental wimpy threshold.
func ParseSettingsValue(raw string) (int32, error) {
	value, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid settings value")
	}
	return int32(value), nil
}
