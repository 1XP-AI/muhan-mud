package world

import (
	"reflect"
	"testing"
)

func TestVitalsTransitionLethalPoisonRespawnsAtomically(t *testing.T) {
	s := playerDeathFixture()
	p := s.Players["a"]
	p.Body.HPCurrent = 1
	p.Body.Flags[2] |= 1
	s.Players["a"] = p
	calls := 0
	next, result, err := s.TickPlayerVitals("a", 100, SceneOptions{}, nil, func(lo, hi int) int { calls++; return hi }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Death == nil || next.Players["a"].Body.RoomID != 1008 || next.Players["a"].Body.HPCurrent != 100 || next.Players["a"].Body.Flags[2]&1 != 0 || len(next.Rooms[1].Items.Items) != 1 || calls != 3 {
		t.Fatalf("%+v calls%d", result, calls)
	}
	if s.Players["a"].Body.HPCurrent != 1 || s.Players["a"].Body.Flags[2]&1 == 0 {
		t.Fatal("input changed")
	}
}

func TestVitalsTransitionNormalHealWithoutDeathContext(t *testing.T) {
	s := playerDeathFixture()
	s.War = nil
	p := s.Players["a"]
	p.Body.HPCurrent = 10
	s.Players["a"] = p
	next, result, err := s.TickPlayerVitals("a", 100, SceneOptions{}, nil, nil, nil)
	if err != nil || result.Death != nil || next.Players["a"].Body.HPCurrent <= 10 || next.Players["a"].Body.RoomID != 1 {
		t.Fatalf("%+v %v", result, err)
	}
}

func TestVitalsTransitionFailedDeathDoesNotSaveDamage(t *testing.T) {
	s := playerDeathFixture()
	p := s.Players["a"]
	p.Body.HPCurrent = 1
	p.Body.Flags[2] |= 1
	s.Players["a"] = p
	r := s.Rooms[1008]
	r.Resource.BeenHere = 2147483647
	s.Rooms[1008] = r
	next, result, err := s.TickPlayerVitals("a", 100, SceneOptions{}, nil, func(lo, hi int) int { return hi }, nil)
	if err == nil || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(result, VitalsTransitionResult{}) {
		t.Fatal("partial fatal tick")
	}
}

func TestVitalsTransitionContinuesAfterRespawn(t *testing.T) {
	s := playerDeathFixture()
	p := s.Players["a"]
	p.Body.HPCurrent = 1
	p.Body.Flags[2] |= 1
	s.Players["a"] = p
	r := s.Rooms[1]
	r.Resource.Flags[3] |= 1
	s.Rooms[1] = r
	next, result, err := s.TickPlayerVitals("a", 100, SceneOptions{}, nil, func(lo, hi int) int { return hi }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if next.Players["a"].Body.HPCurrent != 92 || next.Players["a"].Body.Timers[8].LastTime != 100 || len(result.Deaths) != 1 || result.Deaths[0].AfterMessages != 1 || len(result.Messages) != 2 {
		t.Fatalf("%+v", result)
	}
}

func TestVitalsTransitionKeepsRepeatedDeathsAndOrder(t *testing.T) {
	s := playerDeathFixture()
	p := s.Players["a"]
	p.Body.HPCurrent = 1
	p.Body.HPMax = 5
	p.Body.Flags[2] |= 1
	s.Players["a"] = p
	r := s.Rooms[1]
	r.Resource.Flags[3] |= 1
	s.Rooms[1] = r
	next, result, err := s.TickPlayerVitals("a", 100, SceneOptions{}, nil, func(lo, hi int) int { return hi }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Deaths) != 2 || result.Deaths[0].AfterMessages != 1 || result.Deaths[1].AfterMessages != 2 || next.Rooms[1008].Resource.BeenHere != 2 || next.Players["a"].Body.HPCurrent != 5 || len(next.Rooms[1].Items.Items) != 1 {
		t.Fatalf("%+v", result)
	}
}

func TestVitalsSecondDeathFailureDiscardsFirstDeath(t *testing.T) {
	s := playerDeathFixture()
	p := s.Players["a"]
	p.Body.HPCurrent = 1
	p.Body.HPMax = 5
	p.Body.Flags[2] |= 1
	s.Players["a"] = p
	r := s.Rooms[1]
	r.Resource.Flags[3] |= 1
	s.Rooms[1] = r
	r = s.Rooms[1008]
	r.Resource.BeenHere = 2147483646
	s.Rooms[1008] = r
	next, result, err := s.TickPlayerVitals("a", 100, SceneOptions{}, nil, func(lo, hi int) int { return hi }, nil)
	if err == nil || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(result, VitalsTransitionResult{}) || len(s.Players["a"].Items.Items) != 1 || s.Players["a"].Body.RoomID != 1 {
		t.Fatal("first death escaped second failure")
	}
}
