package world

import (
	"reflect"
	"testing"
)

func TestLowerLevelAppliesBuffOnceAndStatCycle(t *testing.T) {
	p := LegacyMonster{Class: 4, Level: 4, HPMax: 200, MPMax: 200, HPCurrent: 1, MPCurrent: 1, DicePlus: 10, Stats: [5]byte{10, 10, 10, 10, 10}}
	enableEffect(&p, 59)
	got, err := LowerPlayerLevel(p, 2)
	if err != nil || got.Level != 2 || got.HPMax != 94 || got.MPMax != 99 || got.HPCurrent != 94 || got.MPCurrent != 99 || got.DicePlus != 5 || got.Stats != ([5]byte{10, 9, 10, 10, 10}) || flag(got.Flags[:], 59) {
		t.Fatalf("%+v %v", got, err)
	}
	if p.HPMax != 200 || p.Level != 4 {
		t.Fatal("input changed")
	}
	unchanged, err := LowerPlayerLevel(p, 4)
	if err != nil || !reflect.DeepEqual(unchanged, p) {
		t.Fatal("no level loss changed player")
	}
}

func TestLowerLevelRejectsInvalidOrPartialResults(t *testing.T) {
	p := LegacyMonster{Class: 4, Level: 4, HPMax: 100, MPMax: 100}
	for _, target := range []int{0, 5, 3} {
		got, err := LowerPlayerLevel(p, target)
		if err == nil || !reflect.DeepEqual(got, LegacyMonster{}) {
			t.Fatalf("target%d %+v %v", target, got, err)
		}
	}
}

func TestApplyDeathProgressionIncludesLevelLoss(t *testing.T) {
	p := LegacyMonster{Class: 4, Level: 10, Experience: 1000, HPMax: 100, MPMax: 100, Stats: [5]byte{10, 10, 10, 10, 10}}
	got, err := ApplyDeathProgression(p, DeathProgressionRules{AttackerPlayer: true, Self: true})
	if err != nil || got.Experience != 950 || got.Level != 8 || got.HPMax != 94 || got.MPMax != 99 {
		t.Fatalf("%+v %v", got, err)
	}
}
