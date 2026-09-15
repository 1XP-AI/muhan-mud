package world

import (
	"strings"
	"testing"
)

func settingsFixture() State {
	return State{
		Version: 1,
		Rooms:   map[int16]RoomState{1: {Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}}, PlayerIDs: []string{"a"}}},
		Players: map[string]PlayerState{
			"a": {Body: LegacyMonster{Name: "Alice", Type: 0, RoomID: 1, Class: 4}, Online: true},
		},
	}
}

func TestSettingsSetToggleAndStaleProposal(t *testing.T) {
	s := settingsFixture()
	proposal, err := s.PlanSettings("a", "set", "색", nil)
	if err != nil || !proposal.Known || !proposal.DesiredFlag {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	next, result, err := s.ApplySettings(proposal)
	if err != nil || !settingBitSet(next.Players["a"].Body, 26) || !strings.Contains(result.Response, "색        :  사용 ") {
		t.Fatalf("next=%+v result=%q err=%v", next.Players["a"], result.Response, err)
	}
	if _, _, err := next.ApplySettings(proposal); err == nil {
		t.Fatal("stale toggle proposal was accepted")
	}
}

func TestSettingsSetOrdinaryTogglesAndFlagList(t *testing.T) {
	cases := []struct {
		key, line string
		bit       uint
	}{
		{"이야기듣기", "  이야기듣기: 미설정\n", 35},
		{"잡담듣기", "  잡담듣기  : 미설정\n", 3},
		{"환호듣기", "  환호듣기  : 미설정\n", 50},
		{"묘사보기", "  묘사보기  :  설정 \n", 63},
		{"소환", "  소환      :  불가 \n", 34},
		{"행삽입", "  행삽입    :  설정 \n", 11},
		{"상태", "  상태      :  출력 \n", 18},
		{"반향", "  반향      :  설정 \n", 46},
		{"색", "  색        :  사용 \n", 26},
		{"밝은색", "  밝은색    :  사용 \n", 51},
		{"방이름", "  방이름    : 미설정\n", 6},
		{"짧은설명", "  짧은설명  : 미설정\n", 5},
		{"긴설명", "  긴설명    : 미설정\n", 4},
		{"출구", "  출구      : 그래프\n", 7},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			s := settingsFixture()
			proposal, err := s.PlanSettings("a", "set", tc.key, nil)
			if err != nil || !proposal.Known || proposal.Kind != settingsFlag {
				t.Fatalf("proposal=%+v err=%v", proposal, err)
			}
			next, result, err := s.ApplySettings(proposal)
			if err != nil {
				t.Fatal(err)
			}
			if !settingBitSet(next.Players["a"].Body, tc.bit) {
				t.Fatalf("ordinary toggle %q did not set bit %d", tc.key, tc.bit)
			}
			if !strings.HasPrefix(result.Response, "현재 설정 상태:\n") || !strings.Contains(result.Response, tc.line) {
				t.Fatalf("ordinary toggle %q response=%q missing %q", tc.key, result.Response, tc.line)
			}
		})
	}
}

func TestSettingsSetNoArgumentAndClearResponses(t *testing.T) {
	s := settingsFixture()
	list, err := s.PlanSettings("a", "set", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	next, result, err := s.ApplySettings(list)
	if err != nil || next.Players["a"].Body.Flags != s.Players["a"].Body.Flags || !strings.HasPrefix(result.Response, "현재 설정 상태:\n") || !strings.Contains(result.Response, "[설정 도움말]") {
		t.Fatalf("list next=%+v result=%q err=%v", next.Players["a"], result.Response, err)
	}
	clear, err := s.PlanSettings("a", "clear", "방이름", nil)
	if err != nil {
		t.Fatal(err)
	}
	next, result, err = s.ApplySettings(clear)
	if err != nil || !settingBitSet(next.Players["a"].Body, 6) || result.Response != "이제부터 방이름을 출력하지 않습니다.\n" {
		t.Fatalf("clear next=%+v result=%q err=%v", next.Players["a"], result.Response, err)
	}
	help, err := s.PlanSettings("a", "clear", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, result, err = s.ApplySettings(help)
	if err != nil || result.Response != "[해제 도움말]이라고 치시면 모든 설정사항들을 볼 수 있습니다.\n" {
		t.Fatalf("help=%q err=%v", result.Response, err)
	}
}

func TestSettingsWimpyFamilyAndUnknownBranches(t *testing.T) {
	s := settingsFixture()
	value := int32(1)
	proposal, err := s.PlanSettings("a", "set", "도망수치", &value)
	if err != nil {
		t.Fatal(err)
	}
	next, result, err := s.ApplySettings(proposal)
	if err != nil || next.Players["a"].Body.WimpyValue != 10 || !settingBitSet(next.Players["a"].Body, 14) || !strings.Contains(result.Response, "도망수치  : 10") {
		t.Fatalf("wimpy next=%+v result=%q err=%v", next.Players["a"], result.Response, err)
	}
	clear, err := next.PlanSettings("a", "clear", "도망수치", nil)
	if err != nil {
		t.Fatal(err)
	}
	next, _, err = next.ApplySettings(clear)
	if err != nil || settingBitSet(next.Players["a"].Body, 14) || next.Players["a"].Body.WimpyValue != 10 {
		t.Fatalf("cleared wimpy next=%+v err=%v", next.Players["a"], err)
	}
	family, err := s.PlanSettings("a", "set", "패거리귀환", nil)
	if err != nil {
		t.Fatal(err)
	}
	unchanged, result, err := s.ApplySettings(family)
	if err != nil || settingBitSet(unchanged.Players["a"].Body, 58) || !strings.Contains(result.Response, "패거리에 가입되어 있지 않습니다") {
		t.Fatalf("family=%+v result=%q err=%v", unchanged.Players["a"], result.Response, err)
	}
	unknown, err := s.PlanSettings("a", "set", "없는설정", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, result, err = s.ApplySettings(unknown)
	if err != nil || !strings.HasPrefix(result.Response, "현재 설정 상태:\n") {
		t.Fatalf("unknown set result=%q err=%v", result.Response, err)
	}
	unknown, err = s.PlanSettings("a", "clear", "없는설정", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, result, err = s.ApplySettings(unknown)
	if err != nil || result.Response != "잘못 지정되었습니다.\n" {
		t.Fatalf("unknown clear result=%q err=%v", result.Response, err)
	}
}

func TestSettingsWimpySetMatchesLegacyNormalization(t *testing.T) {
	valueOf := func(value int32) *int32 { return &value }
	cases := []struct {
		name  string
		value *int32
		want  int32
	}{
		{name: "omitted parser default", want: 10},
		{name: "negative", value: valueOf(-3), want: 2},
		{name: "zero", value: valueOf(0), want: 2},
		{name: "one special case", value: valueOf(1), want: 10},
		{name: "two clamp boundary", value: valueOf(2), want: 2},
		{name: "ordinary value", value: valueOf(9), want: 9},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := settingsFixture()
			proposal, err := s.PlanSettings("a", "set", "도망수치", tc.value)
			if err != nil {
				t.Fatal(err)
			}
			next, result, err := s.ApplySettings(proposal)
			if err != nil {
				t.Fatal(err)
			}
			if got := next.Players["a"].Body.WimpyValue; got != tc.want {
				t.Fatalf("wimpy value=%d want=%d", got, tc.want)
			}
			if !settingBitSet(next.Players["a"].Body, 14) {
				t.Fatalf("PWIMPY cleared for resulting value %d", tc.want)
			}
			if !strings.Contains(result.Response, "도망수치  : ") {
				t.Fatalf("wimpy response=%q", result.Response)
			}
		})
	}
}

func TestSettingsSetFamilyGateAndSpecialResponses(t *testing.T) {
	family := settingsFixture()
	proposal, err := family.PlanSettings("a", "set", "패거리귀환", nil)
	if err != nil {
		t.Fatal(err)
	}
	next, result, err := family.ApplySettings(proposal)
	if err != nil || settingBitSet(next.Players["a"].Body, 58) || result.Response != "당신은 패거리에 가입되어 있지 않습니다.\n"+settingsFlagList(family.Players["a"].Body) {
		t.Fatalf("ungated family next=%+v result=%q err=%v", next.Players["a"], result.Response, err)
	}

	member := settingsFixture()
	actor := member.Players["a"]
	setSettingFlag(&actor.Body, 55, true)
	member.Players["a"] = actor
	proposal, err = member.PlanSettings("a", "set", "패거리귀환", nil)
	if err != nil {
		t.Fatal(err)
	}
	next, result, err = member.ApplySettings(proposal)
	if err != nil || !settingBitSet(next.Players["a"].Body, 58) || result.Response != "패거리 존으로 귀환을 합니다.\n"+settingsFlagList(next.Players["a"].Body) {
		t.Fatalf("gated family next=%+v result=%q err=%v", next.Players["a"], result.Response, err)
	}

	for _, tc := range []struct {
		key, prefix string
		bit         uint
	}{
		{"hexline", "Hexline enabled.\n", 13},
		{"eavesdropper", "Eavesdropper mode enabled.\n", 15},
		{"~robot~", "Robot mode on.\n", 23},
		{"수동공격", "수동으로 공격합니다.\n", 9},
	} {
		t.Run(tc.key, func(t *testing.T) {
			s := settingsFixture()
			proposal, err := s.PlanSettings("a", "set", tc.key, nil)
			if err != nil {
				t.Fatal(err)
			}
			next, result, err := s.ApplySettings(proposal)
			if err != nil || !settingBitSet(next.Players["a"].Body, tc.bit) || result.Response != tc.prefix+settingsFlagList(next.Players["a"].Body) {
				t.Fatalf("special next=%+v result=%q err=%v", next.Players["a"], result.Response, err)
			}
		})
	}
}

func TestSettingsClearResponsesAndFlags(t *testing.T) {
	cases := []struct {
		key, response string
		bit           uint
		desired       bool
	}{
		{"잡담듣기거부", "이제부터 잡담을 듣습니다.\n", 3, false},
		{"행삽입", "이제부터 메세지를 출력할때 한행을 삽입하지 않습니다.\n", 11, false},
		{"반향", "당신의 메세지가 반향되지 않습니다.\n", 46, false},
		{"방이름", "이제부터 방이름을 출력하지 않습니다.\n", 6, true},
		{"간단", "방의 간단한 설명을 보지 않습니다.\n", 5, true},
		{"일반", "방의 자세한 설명을 보지 않습니다.\n", 4, true},
		{"hexline", "Hex line disabled.\n", 13, false},
		{"도망수치", "도망수치 설정이 해제되었습니다.\n", 14, false},
		{"eavesdropper", "Eavesdropper mode disabled.\n", 15, false},
		{"상태", "당신의 상태를 보여주지 않습니다.\n", 18, false},
		{"~robot~", "Robot mode off.\n", 23, false},
		{"색", "이제부터 메세지가 모두 흑백으로 출력됩니다.\n", 26, false},
		{"소환거부", "이제부터 다른사람이 당신을 소환할 수 있습니다.\n", 34, false},
		{"이야기듣기거부", "이제부터 개인적인 이야기를 듣습니다.\n", 35, false},
		{"수동공격", "이제부터 자동으로 공격합니다.\n", 9, false},
		{"밝은색", "어두운 색으로 출력합니다.\n", 51, false},
		{"패거리귀환", "광장으로 귀환합니다.\n", 58, false},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			s := settingsFixture()
			actor := s.Players["a"]
			setSettingFlag(&actor.Body, tc.bit, !tc.desired)
			actor.Body.WimpyValue = 27
			s.Players["a"] = actor
			proposal, err := s.PlanSettings("a", "clear", tc.key, nil)
			if err != nil {
				t.Fatal(err)
			}
			next, result, err := s.ApplySettings(proposal)
			if err != nil || settingBitSet(next.Players["a"].Body, tc.bit) != tc.desired || result.Response != tc.response {
				t.Fatalf("clear next=%+v result=%q err=%v", next.Players["a"], result.Response, err)
			}
			if tc.key == "도망수치" && next.Players["a"].Body.WimpyValue != 27 {
				t.Fatalf("clear changed WIMPYVALUE=%d", next.Players["a"].Body.WimpyValue)
			}
		})
	}
}
