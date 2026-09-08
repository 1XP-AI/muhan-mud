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
