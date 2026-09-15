package session

import "testing"

func TestParseUseAndChangeClassBoundedForms(t *testing.T) {
	tests := []struct {
		line string
		kind CommandKind
	}{
		{line: "사용 검", kind: CommandUse},
		{line: "직업전환", kind: CommandChangeClass},
		{line: "직업전환 예", kind: CommandChangeClass},
	}
	for _, tc := range tests {
		parsed, err := ParseCommand(tc.line)
		if err != nil || parsed.Kind != tc.kind {
			t.Fatalf("line=%q parsed=%+v err=%v", tc.line, parsed, err)
		}
	}
}

func TestParseUseAndChangeClassRejectWidenedForms(t *testing.T) {
	for _, line := range []string{
		"사용", "사용 모두", "사용 검 2", `사용 "긴 검"`,
		"직업전환 아니오", "직업전환 예 추가", "직업전환  예",
	} {
		parsed, err := ParseCommand(line)
		if err != nil {
			t.Fatalf("line=%q parse error=%v", line, err)
		}
		if parsed.Kind == CommandUse || parsed.Kind == CommandChangeClass {
			t.Fatalf("line=%q widened into bounded kind=%v", line, parsed.Kind)
		}
	}
}
