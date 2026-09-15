package world

import "testing"

func TestLoginDailyUpdatesLimitsNotUsage(t *testing.T) {
	var before [10]LegacyDaily
	for i := range before {
		before[i] = LegacyDaily{Max: 99, Current: 7, LastTime: 42}
	}
	got := LoginDaily(before, 1, 4)
	for i, want := range []byte{25, 10, 10, 10, 1} {
		if got[i].Max != want || got[i].Current != 7 || got[i].LastTime != 42 {
			t.Fatalf("slot%d %+v", i, got[i])
		}
	}
	for i := 5; i < 10; i++ {
		if got[i] != before[i] {
			t.Fatal("changed unrelated daily slot")
		}
	}
	if before[0].Max != 99 {
		t.Fatal("changed input")
	}
	got = LoginDaily(before, 255, 4)
	if got[0].Max != 57 || got[2].Max != 29 || got[3].Max != 24 {
		t.Fatalf("%+v", got)
	}
	if got := LoginDaily(before, 255, 10); got != before {
		t.Fatal("changed caretaker limits")
	}
}

func TestSavedPlayerEntryKeepsSpentDailyUses(t *testing.T) {
	s := offlineFixture()
	p := s.Players["a"]
	p.Body.Class = 4
	p.Body.Level = 1
	p.Body.Daily[0] = LegacyDaily{Max: 1, Current: 3, LastTime: 50}
	s.Players["a"] = p
	next, _, err := s.EnterSavedPlayer("a", SceneOptions{}, nil, 100, nil)
	if err != nil || next.Players["a"].Body.Daily[0] != (LegacyDaily{Max: 25, Current: 3, LastTime: 50}) {
		t.Fatalf("%+v %v", next, err)
	}
}
