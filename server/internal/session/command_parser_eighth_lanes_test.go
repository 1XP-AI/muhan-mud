package session

import "testing"

func TestParseCommandClassifiesCircleBashAndMagicStop(t *testing.T) {
	tests := []struct {
		line string
		kind CommandKind
	}{
		{line: "교란 늑대", kind: CommandCircle},
		{line: "맹공 늑대", kind: CommandBash},
		{line: "혈도봉쇄 늑대", kind: CommandMagicStop},
	}
	for _, tt := range tests {
		parsed, err := ParseCommand(tt.line)
		if err != nil {
			t.Fatalf("ParseCommand(%q) err=%v", tt.line, err)
		}
		if parsed.Kind != tt.kind {
			t.Fatalf("ParseCommand(%q) kind=%d want %d", tt.line, parsed.Kind, tt.kind)
		}
	}
}

func TestParseCommandLeavesUnsupportedCombatSelectorsUnknown(t *testing.T) {
	for _, line := range []string{
		"교란",
		"교란 늑대 추가",
		"맹공 늑대 추가",
		"혈도봉쇄 늑대 2",
	} {
		parsed, err := ParseCommand(line)
		if err != nil {
			t.Fatalf("ParseCommand(%q) err=%v", line, err)
		}
		if parsed.Kind != CommandUnknown {
			t.Fatalf("ParseCommand(%q) kind=%d want unknown", line, parsed.Kind)
		}
	}
}
