package session

import "testing"

func TestParseCommandClassifiesGivePoisonAndEnemyStatusLanes(t *testing.T) {
	tests := []struct {
		line string
		want CommandKind
	}{
		{line: "검 밥 줘", want: CommandGive},
		{line: "100냥 밥 줘", want: CommandGive},
		{line: "독살포 늑대", want: CommandPoison},
		{line: "상태 늑대", want: CommandEnemyStatus},
	}
	for _, test := range tests {
		parsed, err := ParseCommand(test.line)
		if err != nil || parsed.Kind != test.want {
			t.Fatalf("line=%q parsed=%+v err=%v want=%v", test.line, parsed, err, test.want)
		}
	}
}

func TestParseCommandKeepsUnsupportedGivePoisonEnemyStatusFormsUnknown(t *testing.T) {
	for _, line := range []string{
		"검 밥",
		"검 밥 줘 추가",
		"독살포",
		"독살포 늑대 2",
		"상태",
		"상태 늑대 2",
	} {
		parsed, err := ParseCommand(line)
		if err != nil || parsed.Kind != CommandUnknown {
			t.Fatalf("line=%q parsed=%+v err=%v", line, parsed, err)
		}
	}
}
