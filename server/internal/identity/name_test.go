package identity

import (
	"errors"
	"strings"
	"testing"
)

func TestCanonicalNameAcceptsLegacyNamesAndAppliesASCIIOnlyCase(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{name: "Alice", want: "Alice"},
		{name: "aLICE", want: "Alice"},
		{name: "ALICE", want: "Alice"},
		{name: "Alice Smith", want: "Alice smith"},
		{name: " alice", want: " alice"},
		{name: "a ", want: "A "},
		{name: "éCLAIR", want: "éclair"},
		{name: "한A", want: "한a"},
		{name: "A한B", want: "A한b"},
		{name: "한글한글", want: "한글한글"},
		{name: "ééééééé", want: "ééééééé"}, // 14 bytes, 7 codepoints.
		{name: "é", want: "É"},           // No Unicode normalization.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CanonicalName(tt.name)
			if err != nil {
				t.Fatalf("CanonicalName(%q) error = %v", tt.name, err)
			}
			if got != tt.want {
				t.Fatalf("CanonicalName(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestCanonicalNameRejectsLegacyInvalidNames(t *testing.T) {
	invalid := []struct {
		name string
		why  string
	}{
		{name: "", why: "empty"},
		{name: strings.Repeat("a", 13), why: "more than 12 codepoints"},
		{name: "한글한글한", why: "more than 14 bytes"},
		{name: strings.Repeat("é", 8), why: "more than 14 bytes"},
		{name: ".", why: "dot path"},
		{name: "..", why: "dot-dot path"},
		{name: "name/name", why: "slash"},
		{name: "name\\name", why: "backslash"},
		{name: "name:name", why: "colon"},
		{name: "name\x00tail", why: "NUL"},
		{name: "name\x01", why: "control"},
		{name: "name\x1f", why: "control"},
		{name: "name\x7f", why: "DEL"},
		{name: string([]byte{0xff}), why: "invalid UTF-8"},
		{name: string([]byte{0xe2, 0x82}), why: "truncated UTF-8"},
	}
	for _, tt := range invalid {
		t.Run(tt.why, func(t *testing.T) {
			got, err := CanonicalName(tt.name)
			if !errors.Is(err, ErrName) || got != "" {
				t.Fatalf("CanonicalName(%q) = (%q, %v), want (empty, ErrName)", tt.name, got, err)
			}
		})
	}
}

func TestCanonicalNameAllowsSpacesButDoesNotTrim(t *testing.T) {
	for _, input := range []string{" ", "  ", " name "} {
		got, err := CanonicalName(input)
		if err != nil {
			t.Fatalf("CanonicalName(%q) error = %v", input, err)
		}
		want := input
		if input == " name " {
			want = " name "
		}
		if got != want {
			t.Fatalf("CanonicalName(%q) = %q, want %q", input, got, want)
		}
	}
}
