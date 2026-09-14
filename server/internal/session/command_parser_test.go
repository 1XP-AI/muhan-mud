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
		{"봐 동", CommandLook},
		{"동 봐", CommandLook},
		{"북 보다", CommandLook},
		{"조사 동굴 2", CommandLook},
		{"동 junk 봐", CommandLook},
		{"북 foo 보다", CommandLook},
		{"서 bar 조사", CommandLook},
		{"동", CommandDirectional},
		{"  8  북문", CommandDirectional},
		{"공 늑대", CommandAttack},
		{"점수", CommandStatus},
		{"따라 Alice", CommandFollow},
		{"장", CommandItems},
		{"\"안녕 세계", CommandSay},
		{"누구", CommandSocial},
		{"무리", CommandSocial},
		{"주워 가방", CommandItemMutation},
		{"검 주워", CommandItemMutation},
		{"검 주", CommandItemMutation},
		{"검 가져", CommandItemMutation},
		{"검 꺼내", CommandItemMutation},
		{"검 버려", CommandItemMutation},
		{"검 넣어", CommandItemMutation},
		{"동 버려", CommandItemMutation},
		{"동 주워", CommandItemMutation},
		{"동 junk 버려", CommandItemMutation},
		{"동 junk extra 버려", CommandItemMutation},
		{"북 foo extra 주워", CommandItemMutation},
		{"모두 주워", CommandItemMutation},
		{"모두 버려", CommandItemMutation},
		{"가방 보석 꺼내", CommandItemMutation},
		{"보석 가방 넣어", CommandItemMutation},
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
		{"읽어 두루마리", CommandReadScroll},
		{"초대", CommandPropertyInvite},
		{"초대 Bob", CommandPropertyInvite},
		{"패거리누구", CommandFamilyWho},
		{"패거리누구 Bob", CommandFamilyWho},
		{"패거리원", CommandFamilyMember},
		{"모든패거리", CommandFamilyList},
		{"패거리말 안녕하세요", CommandFamilyTalk},
		{"] 안녕하세요", CommandFamilyTalk},
		{"패거리공지", CommandFamilyNews},
		{"패거리공지 a", CommandFamilyNews},
		{"패거리공지 d", CommandFamilyNews},
		{"선전포고", CommandFamilyWar},
		{"선전포고 백호", CommandFamilyWar},
		{"*떨어져라", CommandDMFamily},
		{"*침공", CommandDMFamily},
		{"*따르기", CommandDMFollow},
		{"늑대 *따르기", CommandDMFollow},
		{"오크 2 *cfollow", CommandDMFollow},
		{"기억", CommandMoonSet},
		{"초인의 돌 기억", CommandMoonSet},
		{"zap", CommandZap},
		{"회복봉 zap", CommandZap},
		{"회복봉 늑대 zap", CommandZap},
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
		{"무기만들기", CommandNewForge},
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
	if err != nil || got.Kind != CommandLook {
		t.Fatalf("parsed=%+v err=%v", got, err)
	}
	command, ok := ParseLookLine("봐 늑대")
	if !ok || command.Target != "늑대" || command.Occurrence != 1 {
		t.Fatalf("look command=%+v ok=%v", command, ok)
	}
}

func TestParseCommandLastTokenLookWithExtraTokensIsNeverDirectional(t *testing.T) {
	for _, tt := range []struct {
		line   string
		target string
	}{
		{"동 junk 봐", "동"},
		{"북 foo 보다", "북"},
		{"서 bar 조사", "서"},
	} {
		got, err := ParseCommand(tt.line)
		if err != nil || got.Kind != CommandLook {
			t.Fatalf("ParseCommand(%q)=%+v err=%v want CommandLook", tt.line, got, err)
		}
		command, ok := ParseLookLine(tt.line)
		if !ok || command.Target != tt.target || command.Occurrence != 1 {
			t.Fatalf("ParseLookLine(%q)=%+v ok=%v want target %q", tt.line, command, ok, tt.target)
		}
	}
}

func TestParseCommandLastTokenItemMutationIsNeverUnknownOrLook(t *testing.T) {
	for _, line := range []string{"검 주워", "검 주", "검 가져", "검 꺼내", "검 버려", "검 넣어"} {
		got, err := ParseCommand(line)
		if err != nil || got.Kind != CommandItemMutation {
			t.Fatalf("ParseCommand(%q)=%+v err=%v want CommandItemMutation", line, got, err)
		}
	}
	look, err := ParseCommand("검 봐")
	if err != nil || look.Kind != CommandLook {
		t.Fatalf("ParseCommand(%q)=%+v err=%v want CommandLook", "검 봐", look, err)
	}
	got, err := ParseCommand("동 버려")
	if err != nil || got.Kind != CommandItemMutation {
		t.Fatalf("ParseCommand(%q)=%+v err=%v want CommandItemMutation not directional", "동 버려", got, err)
	}
}

func TestParseCommandLastTokenItemMutationWithExtraTokensIsNeverDirectional(t *testing.T) {
	for _, tt := range []struct {
		line      string
		verb      string
		name      string
		nested    bool
		container string
	}{
		{"동 버려", "drop", "동", false, ""},
		{"동 주워", "take", "동", false, ""},
		{"동 junk extra 버려", "drop", "동", true, "junk"},
		{"북 foo extra 주워", "take", "foo", true, "북"},
		{"검 junk extra 주워", "take", "junk", true, "검"},
	} {
		got, err := ParseCommand(tt.line)
		if err != nil || got.Kind != CommandItemMutation {
			t.Fatalf("ParseCommand(%q)=%+v err=%v want CommandItemMutation not directional", tt.line, got, err)
		}
		action, ok := parseItemMutationLine(tt.line)
		if !ok || action.verb != tt.verb || action.name != tt.name || action.nested != tt.nested || action.container != tt.container {
			t.Fatalf("parseItemMutationLine(%q)=%+v ok=%v want verb=%s name=%s nested=%t container=%s", tt.line, action, ok, tt.verb, tt.name, tt.nested, tt.container)
		}
	}
	nested, err := ParseCommand("동 junk 버려")
	if err != nil || nested.Kind != CommandItemMutation {
		t.Fatalf("ParseCommand(%q)=%+v err=%v want CommandItemMutation not directional", "동 junk 버려", nested, err)
	}
	action, ok := parseItemMutationLine("동 junk 버려")
	if !ok || action.verb != "drop" || action.name != "동" || !action.nested || action.container != "junk" {
		t.Fatalf("parseItemMutationLine(%q)=%+v ok=%v want nested drop of 동 into junk", "동 junk 버려", action, ok)
	}
}

func TestParseCommandItemMutationVerbInMiddleWithNonMutationLastTokenIsNeverDirectional(t *testing.T) {
	for _, line := range []string{"동 버려 extra", "북 주워 junk"} {
		got, err := ParseCommand(line)
		if err != nil {
			t.Fatalf("ParseCommand(%q) err=%v", line, err)
		}
		if got.Kind == CommandDirectional {
			t.Fatalf("ParseCommand(%q)=%+v want fail-closed unknown or item mutation, not CommandDirectional", line, got)
		}
		if got.Kind != CommandUnknown && got.Kind != CommandItemMutation {
			t.Fatalf("ParseCommand(%q)=%+v want CommandUnknown or CommandItemMutation", line, got)
		}
	}
}

func TestParseCommandLookVerbInMiddleWithNonLookLastTokenIsNeverDirectional(t *testing.T) {
	for _, line := range []string{"동 봐 extra", "북 보다 junk"} {
		got, err := ParseCommand(line)
		if err != nil {
			t.Fatalf("ParseCommand(%q) err=%v", line, err)
		}
		if got.Kind == CommandDirectional {
			t.Fatalf("ParseCommand(%q)=%+v want fail-closed unknown or look, not CommandDirectional", line, got)
		}
		if got.Kind != CommandUnknown && got.Kind != CommandLook {
			t.Fatalf("ParseCommand(%q)=%+v want CommandUnknown or CommandLook", line, got)
		}
	}
}
