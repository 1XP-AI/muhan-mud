package world

import "testing"

func TestDirectionalExitUsesMoveNotGoLookup(t *testing.T) {
	exits := []LegacyExit{{Name: "북문"}, {Name: "북"}, {Name: "북"}, {Name: "밑"}, {Name: "밖"}}
	exits[1].Flags[2] |= 8 // XNOSEE: excluded even by exact name
	exits[2].Flags[0] |= 2 // XINVIS: move does NOT filter this flag
	for _, input := range []string{"북", "ㅂ", "8", "84"} {
		if got := SelectDirectionalExit(exits, input); got != 2 {
			t.Fatalf("%q -> %d", input, got)
		}
	}
	if SelectDirectionalExit(exits, "3") != 3 || SelectDirectionalExit(exits, "나가") != 4 || SelectDirectionalExit(exits, "북동") != -1 || SelectDirectionalExit(exits, "") != -1 {
		t.Fatal("wrong directional aliases")
	}
	if SelectExit(exits, "북", 1, false) != 0 {
		t.Fatal("go prefix reference changed")
	}
}
func TestDirectionalRestrictionsKeepOriginalMessages(t *testing.T) {
	e := LegacyExit{}
	e.Flags[0] |= 4
	if got := DirectionalExitRestriction(e, PassageOptions{}); got != "문이 잠겨 있습니다." {
		t.Fatal(got)
	}
	if got := DirectionalExitRestriction(e, PassageOptions{Immobile: true}); got != "당신은 움직일수 없습니다." {
		t.Fatal(got)
	}
	if got := DirectionalExitRestriction(e, PassageOptions{Fighting: true}); got != "싸우는 중에는 이동할 수 없습니다." {
		t.Fatal(got)
	}
	e.Flags = [4]byte{}
	e.Flags[2] |= 1 // XNGHTO16
	if got := DirectionalExitRestriction(e, PassageOptions{Hour: 12}); got != "그 출구는 밤에만 열려 있습니다." {
		t.Fatal(got)
	}
}
