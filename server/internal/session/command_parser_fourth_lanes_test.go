package session

import "testing"

func TestParseCommandClassifiesCompareAndAppraisalLanes(t *testing.T) {
	for _, tc := range []struct {
		line string
		kind CommandKind
	}{
		{line: "비교", kind: CommandCompare},
		{line: `비교 "긴 검" 2`, kind: CommandCompare},
		{line: "감정 검", kind: CommandObjectAppraisal},
		{line: `감정 "긴 검" 2`, kind: CommandObjectAppraisal},
		{line: "검 새검 명명", kind: CommandItemRename},
		{line: `"긴 검" 2 새 이름 명명`, kind: CommandItemRename},
	} {
		parsed, err := ParseCommand(tc.line)
		if err != nil || parsed.Kind != tc.kind {
			t.Fatalf("line=%q parsed=%+v err=%v want=%v", tc.line, parsed, err, tc.kind)
		}
	}
}
