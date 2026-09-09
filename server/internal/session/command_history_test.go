package session

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestExpandHistoryLineRepeatsAndAppends(t *testing.T) {
	tests := []struct {
		name       string
		previous   string
		line       string
		expanded   string
		remembered string
	}{
		{name: "repeat", previous: "봐", line: "!", expanded: "봐", remembered: "봐"},
		{name: "append", previous: "공격 늑대", line: "! 북", expanded: "공격 늑대 북", remembered: "공격 늑대 북"},
		{name: "ordinary", previous: "봐", line: "  시간", expanded: "  시간", remembered: "시간"},
		{name: "empty repeat keeps history", previous: "봐", line: "!", expanded: "봐", remembered: "봐"},
		{name: "empty without history", line: "!", expanded: "", remembered: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			expanded, remembered := ExpandHistoryLine(test.previous, test.line)
			if expanded != test.expanded || remembered != test.remembered {
				t.Fatalf("got expanded=%q remembered=%q", expanded, remembered)
			}
		})
	}
}

func TestExpandHistoryLineDoesNotTreatBareBangAsWorldCommand(t *testing.T) {
	expanded, remembered := ExpandHistoryLine("", "!")
	if expanded != "" || remembered != "" {
		t.Fatalf("empty history should remain empty: expanded=%q remembered=%q", expanded, remembered)
	}
}

func TestExpandHistoryLineKeepsHistoryWithinLegacyByteBudget(t *testing.T) {
	line := strings.Repeat("가", 40)
	_, remembered := ExpandHistoryLine("", line)
	if len(remembered) > CommandHistoryLimitBytes {
		t.Fatalf("history bytes=%d want <=%d", len(remembered), CommandHistoryLimitBytes)
	}
	if !utf8.ValidString(remembered) {
		t.Fatalf("history was truncated in the middle of UTF-8: %q", remembered)
	}
}
