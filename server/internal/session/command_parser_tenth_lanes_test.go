package session

import "testing"

func TestParseCommandClassifiesTimeTrainingAndSelectionLanes(t *testing.T) {
	for _, test := range []struct {
		line string
		want CommandKind
	}{
		{line: "시간", want: CommandRead},
		{line: "수련", want: CommandTrain},
		{line: "선택 상인", want: CommandSelection},
		{line: "선택 상인 2", want: CommandSelection},
	} {
		parsed, err := ParseCommand(test.line)
		if err != nil || parsed.Kind != test.want {
			t.Fatalf("line=%q parsed=%+v err=%v want=%v", test.line, parsed, err, test.want)
		}
	}
}

func TestParseCommandKeepsUnsupportedTimeTrainingSelectionFormsUnknown(t *testing.T) {
	for _, line := range []string{
		"시간 내일",
		"수련 더",
		"선택",
		"선택 상인 0",
		"선택 상인 x",
	} {
		parsed, err := ParseCommand(line)
		if err != nil || parsed.Kind != CommandUnknown {
			t.Fatalf("line=%q parsed=%+v err=%v", line, parsed, err)
		}
	}
}
