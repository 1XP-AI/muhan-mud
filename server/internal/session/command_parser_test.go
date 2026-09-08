package session

import (
	"errors"
	"testing"
)

func TestParseCommandClassifiesImplementedAliases(t *testing.T) {
	tests := []struct {
		line string
		kind CommandKind
	}{
		{"봐", CommandLook},
		{"  8  북문", CommandDirectional},
		{"공 늑대", CommandAttack},
		{"점수", CommandStatus},
		{"따라 Alice", CommandFollow},
		{"장", CommandItems},
		{"\"안녕 세계", CommandSay},
		{"누구", CommandSocial},
		{"주워 가방", CommandItemMutation},
		{"입어 갑옷", CommandEquipment},
		{"입금 100냥", CommandBank},
		{"끝", CommandQuit},
		{"시간", CommandRead},
		{"마법", CommandUnknown},
	}
	for _, tt := range tests {
		got, err := ParseCommand(tt.line)
		if err != nil || got.Kind != tt.kind {
			t.Fatalf("ParseCommand(%q)=%+v err=%v want kind=%d", tt.line, got, err, tt.kind)
		}
	}
}

func TestParseCommandKeepsOriginalLineAndQuotedTokens(t *testing.T) {
	line := `  말 "긴 문장"  `
	got, err := ParseCommand(line)
	if err != nil || got.Kind != CommandSay || got.Line != line {
		t.Fatalf("parsed=%+v err=%v", got, err)
	}
	if len(got.Tokens) != 2 || got.Tokens[0] != "말" || got.Tokens[1] != "긴 문장" {
		t.Fatalf("tokens=%q", got.Tokens)
	}
}

func TestParseCommandAppliesSevenTokenLimitAndRejectsUnterminatedNonSayQuote(t *testing.T) {
	if _, err := ParseCommand("말 하나 둘 셋 넷 다섯 여섯 일곱"); !errors.Is(err, ErrCommandTooManyTokens) {
		t.Fatalf("too many tokens err=%v", err)
	}
	if _, err := ParseCommand(`공격 "늑대`); err == nil {
		t.Fatal("unterminated quote accepted")
	}
}

func TestParseCommandDoesNotTreatExtraTokensAsBareLook(t *testing.T) {
	got, err := ParseCommand("봐 늑대")
	if err != nil || got.Kind != CommandUnknown {
		t.Fatalf("parsed=%+v err=%v", got, err)
	}
}
