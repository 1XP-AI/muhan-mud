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
		{"무리", CommandSocial},
		{"주워 가방", CommandItemMutation},
		{"입어 갑옷", CommandEquipment},
		{"입금 100냥", CommandBank},
		{"끝", CommandQuit},
		{"시간", CommandRead},
		{"저장", CommandSave},
		{"save", CommandSave},
		{"편지받기", CommandMail},
		{"편지삭제", CommandMail},
		{"게시판", CommandBoard},
		{"읽어 게시판 1", CommandBoard},
		{"글삭제 게시판 1", CommandBoard},
		{"읽어 두루마리", CommandUnknown},
		{"정보", CommandInfo},
		{"도움말", CommandHelp},
		{"? 정보", CommandHelp},
		{"외쳐 큰 소리", CommandYell},
		{"잡담 큰 소리", CommandBroadcast},
		{"잡 긴 전역 메시지", CommandBroadcast},
		{"환호 모두 힘내", CommandBroadcast},
		{"환영", CommandWelcome},
		{"미소 Bob", CommandEmote},
		{"감정표현 Bob", CommandEmote},
		{"보아 Bob", CommandLookAtTarget},
		{"표현 긴 문장", CommandExpress},
		{"추적", CommandTrack},
		{"숨겨", CommandHide},
		{"숨어", CommandHide},
		{"듣기거부", CommandIgnore},
		{"듣기거부 Bob", CommandIgnore},
		{"훔쳐 주머니 고블린", CommandSteal},
		{"가르쳐 Bob 삭풍", CommandTeach},
		{"기습 고블린", CommandBackstab},
		{"먹어 회복약", CommandDrink},
		{"엿봐 Bob", CommandPeek},
		{"설정 색", CommandSettings},
		{"해제 방이름", CommandSettings},
		{"열어 북", CommandDoor},
		{"닫아 북", CommandDoor},
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
