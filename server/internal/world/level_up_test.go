package world

import (
	"reflect"
	"testing"
)

func TestRaiseLevelInitializesFighter(t *testing.T) {
	p := LegacyMonster{Class: 4, Stats: [5]byte{10, 10, 10, 10, 10}}
	got, err := RaisePlayerLevel(p)
	if err != nil || got.Level != 1 || got.HPMax != 56 || got.HPCurrent != 56 || got.MPMax != 50 || got.MPCurrent != 50 || got.DiceCount != 1 || got.DiceSides != 5 || got.DicePlus != 0 {
		t.Fatalf("%+v %v", got, err)
	}
	if p.Level != 0 {
		t.Fatal("input changed")
	}
}

func TestRaiseLevelPreservesOriginalFourthLevelRecalculation(t *testing.T) {
	p := LegacyMonster{Class: 4, Level: 1, HPMax: 56, MPMax: 50, HPCurrent: 12, MPCurrent: 13, Stats: [5]byte{10, 10, 10, 10, 10}}
	two, err := RaisePlayerLevel(p)
	if err != nil || two.MPMax != 51 || two.HPCurrent != 12 || two.MPCurrent != 13 {
		t.Fatalf("%+v %v", two, err)
	}
	three, err := RaisePlayerLevel(two)
	if err != nil || three.HPMax != 62 || three.HPCurrent != 12 {
		t.Fatalf("%+v %v", three, err)
	}
	four, err := RaisePlayerLevel(three)
	// C multiplies (level-1) BEFORE dividing by 2, including odd mp gains.
	if err != nil || four.HPMax != 65 || four.MPMax != 51 || four.HPCurrent != 65 || four.MPCurrent != 51 || four.Stats[1] != 11 {
		t.Fatalf("%+v %v", four, err)
	}
}

func TestRaiseLevelAllowsRepresentableNegativeLegacyStat(t *testing.T) {
	for _, test := range []struct {
		name      string
		stat      byte
		wantValue int
	}{
		{name: "minus one", stat: 255, wantValue: 0},
		{name: "minimum signed char", stat: 128, wantValue: -127},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := LegacyMonster{Class: 4, Level: 3, HPMax: 62, MPMax: 51, Stats: [5]byte{10, test.stat, 12, 13, 14}}
			got, err := RaisePlayerLevel(p)
			if err != nil {
				t.Fatal(err)
			}
			if got.Level != 4 || int(int8(got.Stats[1])) != test.wantValue {
				t.Fatalf("got level=%d dex=%d raw=%d want level=4 dex=%d", got.Level, int(int8(got.Stats[1])), got.Stats[1], test.wantValue)
			}
		})
	}
}

func TestRaiseLevelRejectsOverflow(t *testing.T) {
	for _, p := range []LegacyMonster{{Class: 4, Level: 255}, {Class: 0}, {Class: 4, Level: 2, HPMax: 32767}, {Class: 4, Level: 3, Stats: [5]byte{10, 127, 10, 10, 10}}} {
		got, err := RaisePlayerLevel(p)
		if err == nil || !reflect.DeepEqual(got, LegacyMonster{}) {
			t.Fatalf("%+v %v", got, err)
		}
	}
}
