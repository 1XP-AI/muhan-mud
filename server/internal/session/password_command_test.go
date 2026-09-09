package session

import "testing"

func TestParsePasswordLineAdmitsOnlyBareCommand(t *testing.T) {
	for _, line := range []string{"암호", " 암호 ", "\t암호\t"} {
		if _, ok := ParsePasswordLine(line); !ok {
			t.Fatalf("line=%q was not admitted", line)
		}
		parsed, err := ParseCommand(line)
		if err != nil || parsed.Kind != CommandPassword || len(parsed.Tokens) != 1 || parsed.Tokens[0] != "암호" {
			t.Fatalf("line=%q parsed=%+v err=%v", line, parsed, err)
		}
	}
}

func TestParsePasswordLineRejectsInlineSecretsAndUnsafeInput(t *testing.T) {
	for _, line := range []string{"암호 old", "암호\x00", "암호\n", string([]byte{'\xff', '\xfe'})} {
		if IsPasswordLine(line) {
			t.Fatalf("unsafe or inline line accepted: %q", line)
		}
		parsed, err := ParseCommand(line)
		if err != nil {
			t.Fatalf("line=%q parse error=%v", line, err)
		}
		if parsed.Kind == CommandPassword {
			t.Fatalf("line=%q widened into password command", line)
		}
	}
}
