package session

import "testing"

func TestParseCommandClassifiesPowerAccurateAndMeditateLanes(t *testing.T) {
	tests := []struct {
		line string
		kind CommandKind
	}{
		{line: "기공집결", kind: CommandPowerAccuracy},
		{line: "살기충전", kind: CommandPowerAccuracy},
		{line: "참선", kind: CommandMeditate},
	}
	for _, tt := range tests {
		parsed, err := ParseCommand(tt.line)
		if err != nil || parsed.Kind != tt.kind {
			t.Fatalf("ParseCommand(%q)=%+v err=%v want kind=%d", tt.line, parsed, err, tt.kind)
		}
	}
}

func TestParseCommandDoesNotBroadenPowerAccurateOrMeditateAliases(t *testing.T) {
	for _, line := range []string{
		"기공집결 대상", "살기충전 대상", "참선 대상", "기 공집결", "살기 충전", "참 선",
	} {
		parsed, err := ParseCommand(line)
		if err != nil {
			t.Fatalf("ParseCommand(%q) err=%v", line, err)
		}
		if parsed.Kind == CommandPowerAccuracy || parsed.Kind == CommandMeditate {
			t.Fatalf("line %q broadened to bounded lane: %+v", line, parsed)
		}
	}
}
