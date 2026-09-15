package session

import "testing"

func TestParseCommandClassifiesObjectHideAndBribeSuffix(t *testing.T) {
	cases := []struct {
		line string
		kind CommandKind
	}{
		{line: "숨겨 검", kind: CommandHide},
		{line: "숨어 검 2", kind: CommandHide},
		{line: "Guard 100냥 뇌물", kind: CommandBribe},
		{line: "Guard 2 100냥 뇌물", kind: CommandBribe},
	}
	for _, tc := range cases {
		parsed, err := ParseCommand(tc.line)
		if err != nil || parsed.Kind != tc.kind {
			t.Fatalf("ParseCommand(%q)=%+v err=%v want kind %d", tc.line, parsed, err, tc.kind)
		}
	}
}

func TestParseCommandRejectsMalformedBribeBeforeDispatch(t *testing.T) {
	for _, line := range []string{"Guard 0냥 뇌물", "Guard -1냥 뇌물", "Guard 100 뇌물", "Guard 100냥 bribed"} {
		parsed, err := ParseCommand(line)
		if err != nil {
			t.Fatalf("ParseCommand(%q) err=%v", line, err)
		}
		if parsed.Kind == CommandBribe {
			t.Fatalf("malformed bribe classified as implemented: %q", line)
		}
	}
}
