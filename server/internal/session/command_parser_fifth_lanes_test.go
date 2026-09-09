package session

import "testing"

func TestParseCommandClassifiesAbilityAndTitleLanes(t *testing.T) {
	tests := []struct {
		line string
		kind CommandKind
	}{
		{line: "활보법", kind: CommandRangerPray},
		{line: "신원법", kind: CommandRangerPray},
		{line: "경계", kind: CommandPrepare},
		{line: "잠력격발", kind: CommandUpDmg},
		{line: "칭호", kind: CommandTitle},
		{line: "칭호 무명의 수호자", kind: CommandTitle},
		{line: "칭호삭제", kind: CommandTitle},
	}
	for _, tt := range tests {
		parsed, err := ParseCommand(tt.line)
		if err != nil || parsed.Kind != tt.kind {
			t.Fatalf("ParseCommand(%q)=%+v err=%v want kind=%d", tt.line, parsed, err, tt.kind)
		}
	}
}

func TestParseCommandClassifiesAliasBurnAndStudyLanes(t *testing.T) {
	tests := []struct {
		line string
		kind CommandKind
	}{
		{line: "줄임말", kind: CommandAlias},
		{line: "줄임말 북 북쪽", kind: CommandAlias},
		{line: "북쪽으로 이동 줄임말", kind: CommandAlias},
		{line: "태워 낡은검", kind: CommandBurn},
		{line: "소각 낡은검 2", kind: CommandBurn},
		{line: "배워 두루마리", kind: CommandStudy},
		{line: "연마 두루마리 2", kind: CommandStudy},
	}
	for _, tt := range tests {
		parsed, err := ParseCommand(tt.line)
		if err != nil || parsed.Kind != tt.kind {
			t.Fatalf("ParseCommand(%q)=%+v err=%v want kind=%d", tt.line, parsed, err, tt.kind)
		}
	}
}

func TestParseCommandDoesNotBroadenAliasBurnOrStudyLanes(t *testing.T) {
	for _, line := range []string{
		"줄임말 북 $1",
		"태워",
		"배워",
		"배워 두루마리 nope",
		"소각 낡은검 0",
	} {
		parsed, err := ParseCommand(line)
		if err != nil {
			t.Fatalf("ParseCommand(%q) err=%v", line, err)
		}
		if parsed.Kind == CommandAlias || parsed.Kind == CommandBurn || parsed.Kind == CommandStudy {
			t.Fatalf("line %q broadened to bounded lane: %+v", line, parsed)
		}
	}
}

func TestParseCommandDoesNotBroadenBareAbilityOrTitleAliases(t *testing.T) {
	for _, line := range []string{"활보법 대상", "신원법 지금", "경계 대상", "잠력격발 대상", "칭호 ", "칭호  leading", "칭호삭제 extra"} {
		parsed, err := ParseCommand(line)
		if err != nil {
			t.Fatalf("ParseCommand(%q) err=%v", line, err)
		}
		if parsed.Kind == CommandRangerPray || parsed.Kind == CommandPrepare || parsed.Kind == CommandUpDmg || parsed.Kind == CommandTitle {
			t.Fatalf("line %q broadened to bounded lane: %+v", line, parsed)
		}
	}
}
