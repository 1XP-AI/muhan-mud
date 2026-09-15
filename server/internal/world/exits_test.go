package world

import "testing"

func TestSelectExit(t *testing.T) {
	exits := []LegacyExit{{Name: "북문", Destination: 1}, {Name: "북쪽", Destination: 2, Flags: [4]byte{1}}, {Name: "북탑", Destination: 3, Flags: [4]byte{2}}, {Name: "북금지", Destination: 4, Flags: [4]byte{0, 0, 8}}}
	for _, tc := range []struct {
		prefix string
		n      int
		detect bool
		want   int
	}{{"북", 1, false, 0}, {"북", 2, false, 1}, {"북", 3, false, -1}, {"북", 3, true, 2}, {"북", 4, true, -1}, {"남", 1, false, -1}, {"북", 0, true, -1}, {"", 1, true, -1}} {
		if got := SelectExit(exits, tc.prefix, tc.n, tc.detect); got != tc.want {
			t.Fatalf("%+v got %d", tc, got)
		}
	}
}

func TestExitRestrictionOrderAndTime(t *testing.T) {
	e := LegacyExit{Flags: [4]byte{12}}
	if got := ExitRestriction(e, PassageOptions{Immobile: true, Fighting: true}); got != "당신은 움직일 수가 없습니다." {
		t.Fatal(got)
	}
	if got := ExitRestriction(e, PassageOptions{Fighting: true}); got != "싸우는 중에는 이동할 수 없습니다." {
		t.Fatal(got)
	}
	if got := ExitRestriction(e, PassageOptions{}); got != "그 출구는 잠겨 있습니다." {
		t.Fatal(got)
	}
	for _, hour := range []int{6, 20} {
		e.Flags = [4]byte{0, 0, 1}
		if got := ExitRestriction(e, PassageOptions{Hour: hour}); got != "" {
			t.Fatal("legacy night boundary", hour, got)
		}
	}
}

func TestExitDayNightFlight(t *testing.T) {
	for _, tc := range []struct {
		flags [4]byte
		v     PassageOptions
		want  string
	}{
		{[4]byte{0, 8}, PassageOptions{}, "그곳에는 날아서만 갈 수 있습니다."},
		{[4]byte{0, 8}, PassageOptions{Flying: true}, ""},
		{[4]byte{0, 0, 1}, PassageOptions{Hour: 12}, "그 출구는 밤에만 갈 수 있습니다."},
		{[4]byte{0, 0, 2}, PassageOptions{Hour: 21}, "그 출구로는 낮에만 갈 수 있습니다."},
		{[4]byte{0, 0, 2}, PassageOptions{Hour: 6}, ""},
	} {
		if got := ExitRestriction(LegacyExit{Flags: tc.flags}, tc.v); got != tc.want {
			t.Fatalf("%+v: %q", tc, got)
		}
	}
}
