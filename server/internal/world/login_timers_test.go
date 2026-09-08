package world

import "testing"

func TestLoginTimersPreserveOfflineCooldowns(t *testing.T) {
	var before [45]LegacyTimer
	before[28].LastTime = 900
	before[0] = LegacyTimer{LastTime: 850, Interval: 80, Misc: 7}
	before[1].LastTime = 950
	got, err := LoginTimers(before, 1000)
	if err != nil || got[0] != (LegacyTimer{LastTime: 950, Interval: 80, Misc: 7}) || got[1].LastTime != 1000 || got[28].LastTime != 1000 || got[22].LastTime != 1000 || got[22].Interval != 600 || before[0].LastTime != 850 {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestLoginTimersClockRollbackMatchesLegacyOrder(t *testing.T) {
	var before [45]LegacyTimer
	before[28].LastTime = 1100
	before[0].LastTime = 1050
	got, err := LoginTimers(before, 1000)
	if err != nil || got[0].LastTime != 950 || got[28].LastTime != 1000 || got[22].LastTime != 900 {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestLoginTimersRejectUnderflow(t *testing.T) {
	var before [45]LegacyTimer
	before[28].LastTime = 100
	before[0].LastTime = -2147483648
	got, err := LoginTimers(before, 0)
	if err == nil || got != ([45]LegacyTimer{}) {
		t.Fatal("partial or wrapped timers")
	}
}

func TestSavedPlayerEntryAppliesLoginTimers(t *testing.T) {
	s := offlineFixture()
	p := s.Players["a"]
	p.Body.Timers[28].LastTime = 900
	p.Body.Timers[0].LastTime = 850
	s.Players["a"] = p
	next, _, err := s.EnterSavedPlayer("a", SceneOptions{}, nil, 1000, nil)
	if err != nil || next.Players["a"].Body.Timers[0].LastTime != 950 || next.Players["a"].Body.Timers[22].Interval != 600 {
		t.Fatalf("%+v %v", next, err)
	}
}
