package session

import "testing"

func TestParseCommandClassifiesServiceCommandLanes(t *testing.T) {
	tests := []struct {
		line string
		kind CommandKind
	}{
		{line: "가치 검", kind: CommandValue},
		{line: "가격 검 2", kind: CommandValue},
		{line: "수리 검", kind: CommandRepair},
		{line: "얘기 Bob 안녕", kind: CommandDirectMessage},
		{line: "이야기 Bob 안녕", kind: CommandDirectMessage},
	}
	for _, test := range tests {
		parsed, err := ParseCommand(test.line)
		if err != nil || parsed.Kind != test.kind {
			t.Fatalf("line=%q parsed=%+v err=%v", test.line, parsed, err)
		}
	}
}
