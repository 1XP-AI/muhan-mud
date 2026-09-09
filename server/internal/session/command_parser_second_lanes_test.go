package session

import "testing"

func TestParseCommandClassifiesSecondBatchServiceLanes(t *testing.T) {
	tests := []struct {
		line string
		kind CommandKind
	}{
		{line: "상인 검 구입", kind: CommandMerchantPurchase},
		{line: "대화 Guide", kind: CommandNPCTalk},
		{line: "그룹말 hello", kind: CommandGroupTalk},
		{line: "hello 그룹말", kind: CommandGroupTalk},
		{line: "= hello", kind: CommandGroupTalk},
		{line: "hello =", kind: CommandGroupTalk},
	}
	for _, tc := range tests {
		parsed, err := ParseCommand(tc.line)
		if err != nil || parsed.Kind != tc.kind {
			t.Fatalf("line=%q parsed=%+v err=%v want=%v", tc.line, parsed, err, tc.kind)
		}
	}
}
