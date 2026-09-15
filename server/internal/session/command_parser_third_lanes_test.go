package session

import "testing"

func TestParseCommandClassifiesThirdBatchWorldLanes(t *testing.T) {
	tests := []struct {
		line string
		kind CommandKind
	}{
		{line: "바르게 서 묘사", kind: CommandDescription},
		{line: "묘사", kind: CommandDescription},
		{line: "사용자검색 Bob", kind: CommandPlayerLookup},
		{line: "사용자정보 Bob", kind: CommandPlayerLookup},
		{line: "귀환", kind: CommandReturnSquare},
		{line: "귀", kind: CommandReturnSquare},
	}
	for _, tc := range tests {
		parsed, err := ParseCommand(tc.line)
		if err != nil || parsed.Kind != tc.kind {
			t.Fatalf("line=%q parsed=%+v err=%v want=%v", tc.line, parsed, err, tc.kind)
		}
	}
}
