package world

import (
	"errors"
	"os"
	"testing"
)

func TestEnchantThresholdsAndOverflow(t *testing.T) {
	for _, tc := range []struct{ roll, bonus int }{{50, 0}, {51, 1}, {90, 1}, {91, 2}, {98, 2}, {99, 3}, {100, 3}} {
		o, err := EnchantObject(LegacyObject{}, func(int, int) int { return tc.roll })
		if err != nil || int(o.Adjustment) != tc.bonus || int(o.DicePlus) != tc.bonus || flag(o.Flags[:], 14) != (tc.bonus > 0) {
			t.Fatalf("%+v: %+v %v", tc, o, err)
		}
	}
	if _, err := EnchantObject(LegacyObject{DicePlus: 32767}, func(int, int) int { return 99 }); err == nil {
		t.Fatal("dice overflow accepted")
	}
}

func TestOriginalMonsterInstantiation(t *testing.T) {
	c := TemplateCatalog{FS: os.DirFS("../../../objmon")}
	template, err := c.Monster(1)
	if err != nil {
		t.Fatal(err)
	}
	m, err := SpawnPermanentMonster(template, 100, c.Object, func(lo, hi int) int { return lo })
	if err != nil || m.Name != "노동자" || m.Timers[8].LastTime != 100 || !flag(m.Flags[:], 0) {
		t.Fatalf("%+v %v", m, err)
	}
}

func TestSpawnMonsterRandomSequence(t *testing.T) {
	m := LegacyMonster{Name: "늑대", Gold: 100, Carry: [10]int16{1, 2}}
	values := []int{90, 1, 99, 0, 30}
	calls := 0
	rng := func(lo, hi int) int {
		v := values[calls]
		calls++
		if v < lo || v > hi {
			t.Fatalf("bad fixture %d outside %d..%d", v, lo, hi)
		}
		return v
	}
	load := func(id int16) (LegacyObject, error) {
		if id == 2 {
			return LegacyObject{Name: "보석", Flags: [8]byte{0, 0, 32}}, nil
		}
		return LegacyObject{Name: "검"}, nil
	}
	got, err := SpawnPermanentMonster(m, 77, load, rng)
	if err != nil || calls != 5 || got.Gold != 30 || len(got.Inventory) != 2 || got.Inventory[0].Name != "검" || got.Inventory[1].Adjustment != 3 || got.Inventory[1].DicePlus != 3 || got.Timers[8].LastTime != 77 || got.Timers[8].Interval != 60 || !flag(got.Flags[:], 0) {
		t.Fatalf("%+v %v calls%d", got, err, calls)
	}
	if m.Gold != 100 || len(m.Inventory) != 0 {
		t.Fatal("mutated template")
	}
}

func TestSpawnFixedGoldSkipsSparseCarry(t *testing.T) {
	m := LegacyMonster{Gold: 100, Flags: [8]byte{0, 0, 0, 2}}
	values := []int{96, 10, 49, 9}
	calls := 0
	got, err := SpawnPermanentMonster(m, 1, nil, func(int, int) int { v := values[calls]; calls++; return v })
	if err != nil || calls != 4 || got.Gold != 100 || len(got.Inventory) != 0 {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestSpawnMissingObjectReturnsNoPartialMonster(t *testing.T) {
	m := LegacyMonster{Name: "늑대", Carry: [10]int16{1}}
	calls := 0
	got, err := SpawnPermanentMonster(m, 1, func(int16) (LegacyObject, error) { return LegacyObject{}, errors.New("missing") }, func(int, int) int {
		calls++
		if calls == 1 {
			return 1
		}
		return 0
	})
	if err == nil || got.Name != "" {
		t.Fatal("partial spawn escaped")
	}
}
