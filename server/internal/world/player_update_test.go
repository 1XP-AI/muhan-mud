package world

import (
	"reflect"
	"testing"
)

func TestPlayerUpdateExpiresSpellBeforeLightConsumption(t *testing.T) {
	s := playerDeathFixture()
	p := s.Players["a"]
	p.Body.HPCurrent = 50
	p.Body.Flags[2] |= 2
	p.Body.Timers[13] = LegacyTimer{LastTime: 0, Interval: 99}
	p.Body.Timers[28] = LegacyTimer{LastTime: 90, Interval: 20}
	p.Items = &ItemCollection{Items: map[string]Item{"torch": {Object: LegacyObject{Name: "torch", Type: 12, ShotsCurrent: 1, Flags: [8]byte{1: 8}}}}, Ready: [20]string{0: "torch"}}
	s.Players["a"] = p
	next, result, err := s.UpdatePlayer("a", 100, SceneOptions{}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if next.Players["a"].Body.Timers[28].Interval != 30 || next.Players["a"].Body.Timers[22].LastTime != 100 || !result.SaveDue || result.ExtinguishedID != "torch" || len(result.Effects) == 0 || next.Players["a"].Items.Items["torch"].Object.ShotsCurrent != 0 {
		t.Fatalf("%+v", result)
	}
	if s.Players["a"].Items.Items["torch"].Object.ShotsCurrent != 1 {
		t.Fatal("input mutated")
	}
}

func TestPlayerUpdateHoursOverflowIsAtomic(t *testing.T) {
	s := playerDeathFixture()
	p := s.Players["a"]
	p.Body.Timers[28] = LegacyTimer{Interval: 2147483647}
	s.Players["a"] = p
	next, result, err := s.UpdatePlayer("a", 1, SceneOptions{}, nil, nil, nil)
	if err == nil || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(result, PlayerUpdateResult{}) {
		t.Fatal("partial overflow")
	}
}

func TestPlayerUpdateDoesNotConsumeLightDroppedDuringDeath(t *testing.T) {
	s := playerDeathFixture()
	p := s.Players["a"]
	p.Body.HPCurrent = 1
	p.Body.Flags[2] |= 1
	p.Items = &ItemCollection{Items: map[string]Item{"torch": {Object: LegacyObject{Name: "torch", Type: 12, ShotsCurrent: 1, Flags: [8]byte{1: 8}}}}, Ready: [20]string{0: "torch"}}
	s.Players["a"] = p
	next, result, err := s.UpdatePlayer("a", 100, SceneOptions{}, nil, func(lo, hi int) int { return hi }, nil)
	if err != nil || len(result.Vitals.Deaths) != 1 || result.ExtinguishedID != "" || next.Rooms[1].Items.Items["torch"].Object.ShotsCurrent != 1 || len(next.Players["a"].Items.Items) != 0 {
		t.Fatalf("%+v %v", result, err)
	}
}
