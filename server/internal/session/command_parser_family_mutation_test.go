package session

import "testing"

func TestParseCommandClassifiesFamilyMutationAliases(t *testing.T) {
	for _, line := range []string{"패거리가입 청룡", "패거리탈퇴", "가입허가 신청자"} {
		parsed, err := ParseCommand(line)
		if err != nil {
			t.Fatalf("ParseCommand(%q) err=%v", line, err)
		}
		if parsed.Kind != CommandFamilyMutation {
			t.Fatalf("ParseCommand(%q) kind=%d want family mutation", line, parsed.Kind)
		}
	}
}

func TestParseCommandDoesNotBroadenFamilyMutationForms(t *testing.T) {
	for _, line := range []string{
		"패거리가입",
		"패거리가입 청룡 extra",
		"패거리탈퇴 extra",
		"가입허가 신청자 extra",
	} {
		parsed, err := ParseCommand(line)
		if err != nil {
			t.Fatalf("ParseCommand(%q) err=%v", line, err)
		}
		if parsed.Kind == CommandFamilyMutation {
			t.Fatalf("ParseCommand(%q) unexpectedly broadened family mutation", line)
		}
	}
}
