package session

import "testing"

func TestParseCommandClassifiesTurnAbsorbAndKick(t *testing.T) {
	tests := []struct {
		line string
		want CommandKind
	}{
		{line: "방혼술 늑대", want: CommandTurn},
		{line: "흡성대법 늑대", want: CommandAbsorb},
		{line: "차기 늑대", want: CommandKick},
	}
	for _, test := range tests {
		parsed, err := ParseCommand(test.line)
		if err != nil {
			t.Fatalf("ParseCommand(%q) err=%v", test.line, err)
		}
		if parsed.Kind != test.want {
			t.Fatalf("ParseCommand(%q) kind=%d want %d", test.line, parsed.Kind, test.want)
		}
	}
}

func TestParseCommandDoesNotBroadenTurnAbsorbAndKickSelectors(t *testing.T) {
	for _, line := range []string{"방혼술 늑대 두마리", "흡성대법", "차기 늑대 두마리", "방혼술\n늑대"} {
		parsed, err := ParseCommand(line)
		if err == nil && parsed.Kind != CommandUnknown {
			t.Fatalf("ParseCommand(%q) kind=%d want unknown or parse error", line, parsed.Kind)
		}
	}
}
