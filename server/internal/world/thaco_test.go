package world

import "testing"

func TestWeaponProficiencyThresholds(t *testing.T) {
	for _, tc := range []struct {
		class byte
		xp    int32
		want  int
	}{{4, 0, 0}, {4, 384, 5}, {4, 768, 10}, {4, 1024, 20}, {2, 1536, 10}, {1, 5076, 30}, {5, 5376, 10}, {4, 499999999, 109}} {
		got, err := WeaponProficiency(tc.class, tc.xp)
		if err != nil || got != tc.want {
			t.Fatalf("%+v got%d %v", tc, got, err)
		}
	}
	for _, tc := range []struct {
		class byte
		xp    int32
	}{{0, 0}, {13, 0}, {4, -1}, {4, 500000000}} {
		if _, err := WeaponProficiency(tc.class, tc.xp); err == nil {
			t.Fatalf("accepted %+v", tc)
		}
	}
}

func TestThacoWeaponStatsFloorAndLegacyBlessBug(t *testing.T) {
	p := LegacyMonster{Class: 4, Level: 1}
	p.Stats[0] = 10
	if got, err := ComputeThaco(p, nil); err != nil || got != 20 {
		t.Fatalf("%d %v", got, err)
	}
	w := LegacyObject{Adjustment: 2, Type: 0}
	p.Proficiency[0] = 1024 // 20 percent / fighter divisor 20 = 1
	if got, err := ComputeThaco(p, &w); err != nil || got != 17 {
		t.Fatalf("%d %v", got, err)
	}
	p.Flags[0] |= 1
	if got, err := ComputeThaco(p, &w); err != nil || got != 17 {
		t.Fatalf("bless changed stored value %d %v", got, err)
	}
	p.Level = 255
	p.Stats[0] = 63
	p.Proficiency[0] = 499999999
	if got, err := ComputeThaco(p, &w); err != nil || got != -5 {
		t.Fatalf("%d %v", got, err)
	}
	p.Class = 10
	if got, err := ComputeThaco(p, &w); err != nil || got != -10 {
		t.Fatalf("%d %v", got, err)
	}
}

func TestThacoRejectsUndefinedLegacyIndices(t *testing.T) {
	for _, p := range []LegacyMonster{{Class: 4, Level: 0}, {Class: 0, Level: 1}, {Class: 4, Level: 1, Stats: [5]byte{64}}} {
		if _, err := ComputeThaco(p, nil); err == nil {
			t.Fatalf("accepted %+v", p)
		}
	}
}
