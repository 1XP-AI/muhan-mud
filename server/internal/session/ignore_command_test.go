package session

import (
	"errors"
	"strings"
	"testing"
)

func TestParseIgnoreLineAdmitsBareListAndCanonicalizesOnlineTarget(t *testing.T) {
	tests := []struct {
		line   string
		kind   IgnoreCommandKind
		target string
	}{
		{line: "듣기거부", kind: IgnoreList},
		{line: "  듣기거부  ", kind: IgnoreList},
		{line: "듣기거부 alice", kind: IgnoreToggle, target: "Alice"},
		{line: "듣기거부 한글", kind: IgnoreToggle, target: "한글"},
		// The legacy tokenizer accepts a quoted single-token name. It is still
		// bounded by the same no-whitespace target contract.
		{line: `듣기거부 "alice"`, kind: IgnoreToggle, target: "Alice"},
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			got, ok := ParseIgnoreLine(tt.line)
			if !ok {
				t.Fatalf("ParseIgnoreLine(%q) rejected a valid line", tt.line)
			}
			if got.Kind != tt.kind || got.Target != tt.target {
				t.Fatalf("ParseIgnoreLine(%q) = %+v, want kind=%v target=%q", tt.line, got, tt.kind, tt.target)
			}
		})
	}
}

func TestParseIgnoreLineRejectsAmbiguousOrUnsafeTargetInput(t *testing.T) {
	invalid := []string{
		"",
		"듣기거부 alice bob",
		"듣기거부 \"alice bob\"",
		"듣기거부 alice\tmore",
		"듣기거부 .",
		"듣기거부 ..",
		"듣기거부 a/b",
		"듣기거부 a\\b",
		"듣기거부 a:b",
		"듣기거부 " + strings.Repeat("a", IgnoreTargetNameMaxBytes+1),
		"듣기거부 \x00alice",
		"듣기거부 " + string([]byte{0xff}),
		"다른명령 alice",
	}
	for _, line := range invalid {
		t.Run(strings.ReplaceAll(line, "\x00", "NUL"), func(t *testing.T) {
			if got, ok := ParseIgnoreLine(line); ok || got != (IgnoreCommand{}) {
				t.Fatalf("ParseIgnoreLine(%q) = %+v, %v, want rejected zero value", line, got, ok)
			}
		})
	}
}

func TestValidateIgnoreTargetUsesLegacyNameByteAndCodepointBounds(t *testing.T) {
	if err := ValidateIgnoreTargetName("ééééééé"); err != nil { // 14 bytes, 7 runes
		t.Fatalf("14-byte UTF-8 name rejected: %v", err)
	}
	for _, test := range []struct {
		name string
		want error
	}{
		{name: "", want: ErrIgnoreTargetRequired},
		{name: strings.Repeat("a", IgnoreTargetNameMaxBytes+1), want: ErrIgnoreTargetTooLong},
		{name: strings.Repeat("한", IgnoreTargetNameMaxRunes+1), want: ErrIgnoreTargetTooLong},
		{name: " alice", want: ErrIgnoreTargetInvalid},
		{name: "alice ", want: ErrIgnoreTargetInvalid},
		{name: "alice bob", want: ErrIgnoreTargetInvalid},
		{name: "alice\n", want: ErrIgnoreTargetInvalid},
		{name: "alice\u200b", want: ErrIgnoreTargetInvalid},
		{name: "alice/alice", want: ErrIgnoreTargetInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateIgnoreTargetName(test.name); !errors.Is(err, test.want) {
				t.Fatalf("ValidateIgnoreTargetName(%q) = %v, want %v", test.name, err, test.want)
			}
		})
	}
}

func TestCanonicalIgnoreTargetNameMatchesLegacyFirstByteUp(t *testing.T) {
	for _, tt := range []struct {
		input string
		want  string
	}{
		{input: "alice", want: "Alice"},
		{input: "aLICE", want: "ALICE"},
		{input: "한a", want: "한a"},
	} {
		got, err := CanonicalIgnoreTargetName(tt.input)
		if err != nil || got != tt.want {
			t.Errorf("CanonicalIgnoreTargetName(%q) = %q, %v, want %q", tt.input, got, err, tt.want)
		}
	}
}
