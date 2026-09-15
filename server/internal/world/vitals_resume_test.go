package world

import (
	"errors"
	"reflect"
	"testing"
)

func TestVitalsResumeUsesNewRoomButOriginalIll(t *testing.T) {
	p := LegacyMonster{Class: 4, HPMax: 100, MPMax: 80, HPCurrent: 1, MPCurrent: 2, Stats: [5]byte{10, 10, 10, 10, 10}}
	p.Flags[2] = 1
	source := LegacyRoom{}
	source.Flags[3] = 1         // RPHARM
	destination := LegacyRoom{} // no hazard flags: original generic drain applies
	deaths := 0
	got, err := PlanVitalsWithDeath(p, &source, 100, func(lo, hi int) int { return hi }, func(dead LegacyMonster) (LegacyMonster, *LegacyRoom, error) {
		deaths++
		dead.HPCurrent = 100
		dead.MPCurrent = 8
		dead.Flags[2] &^= 1
		return dead, &destination, nil
	})
	if err != nil || deaths != 1 || got.Death || got.Player.HPCurrent != 92 || got.Player.MPCurrent != 8 || got.Player.Timers[8].LastTime != 100 || got.Player.Timers[8].Interval != 5 {
		t.Fatalf("%+v %v deaths%d", got, err, deaths)
	}
}

func TestVitalsResumeFailureReturnsNoPartialResult(t *testing.T) {
	p := LegacyMonster{HPMax: 100, HPCurrent: 1, Stats: [5]byte{10, 10, 10, 10, 10}}
	p.Flags[2] = 1
	got, err := PlanVitalsWithDeath(p, &LegacyRoom{}, 100, func(lo, hi int) int { return hi }, func(LegacyMonster) (LegacyMonster, *LegacyRoom, error) {
		return LegacyMonster{}, nil, errors.New("entry failure")
	})
	if err == nil || !reflect.DeepEqual(got, VitalResult{}) {
		t.Fatal("partial continuation")
	}
}
