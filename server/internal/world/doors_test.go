package world

import "testing"

func TestDoorOpenCloseTransitions(t *testing.T) {
	e := LegacyExit{Name: "북", Flags: [4]byte{40}, LastTime: 5}
	opened := OpenDoor(e, true, 10)
	if !opened.Changed || opened.Hidden || opened.Exit.LastTime != 10 || flag(opened.Exit.Flags[:], 3) || opened.Message != "당신은 북쪽 출구를 열었습니다." {
		t.Fatalf("%+v", opened)
	}
	closed := CloseDoor(opened.Exit, true)
	if !closed.Changed || closed.Hidden || !flag(closed.Exit.Flags[:], 3) || closed.Exit.LastTime != 10 {
		t.Fatalf("%+v", closed)
	}
	if e.LastTime != 5 || !flag(e.Flags[:], 3) {
		t.Fatal("mutated source")
	}
}

func TestDoorRejectedActionPreservesState(t *testing.T) {
	e := LegacyExit{Flags: [4]byte{12}, LastTime: 42}
	r := OpenDoor(e, true, 90)
	if r.Changed || !r.Hidden || r.Exit != e || r.Message != "그것은 잠겨져 있습니다." {
		t.Fatalf("%+v", r)
	}
	e.Flags = [4]byte{}
	r = CloseDoor(e, true)
	if r.Changed || !r.Hidden || r.Exit != e || r.Message != "당신은 그 출구를 닫을 수 없습니다." {
		t.Fatalf("%+v", r)
	}
}

func TestDoorRefreshDeadlineAndOverflow(t *testing.T) {
	exits := []LegacyExit{{Flags: [4]byte{16}, LastTime: 10, Interval: 5}, {Flags: [4]byte{32}, LastTime: 10, Interval: 5}, {Flags: [4]byte{16}, LastTime: 2147483647, Interval: 2147483647}}
	at := RefreshDoors(exits, 15)
	if flag(at[0].Flags[:], 2) || flag(at[1].Flags[:], 3) {
		t.Fatal("equality must not reset")
	}
	after := RefreshDoors(exits, 16)
	if !flag(after[0].Flags[:], 2) || !flag(after[0].Flags[:], 3) || !flag(after[1].Flags[:], 3) || flag(after[1].Flags[:], 2) || flag(after[2].Flags[:], 2) {
		t.Fatalf("%+v", after)
	}
	if flag(exits[0].Flags[:], 2) {
		t.Fatal("mutated input")
	}
}
