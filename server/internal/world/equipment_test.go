package world

import (
	"reflect"
	"testing"
)

func worn(wear byte) LegacyObject {
	return LegacyObject{Type: 5, Wear: wear, ShotsCurrent: 1, Flags: [8]byte{0, 0, 128}}
}

func TestEquipmentFirstFitAndOverflowRetention(t *testing.T) {
	var inventory []LegacyObject
	for i := 0; i < 9; i++ {
		inventory = append(inventory, worn(9))
	}
	for i := 0; i < 3; i++ {
		inventory = append(inventory, worn(4))
	}
	got, err := PlanLoginEquipment(inventory, 1, 1)
	if err != nil || !reflect.DeepEqual(got.Remaining, []int{8, 11}) {
		t.Fatalf("%+v %v", got, err)
	}
	for i := 0; i < 8; i++ {
		if got.Ready[8+i] != i {
			t.Fatalf("ring order: %+v", got)
		}
	}
	if got.Ready[3] != 9 || got.Ready[4] != 10 || got.Ready[0] != -1 {
		t.Fatalf("%+v", got)
	}
}

func TestEquipmentHeldWeaponAndOccupiedSlots(t *testing.T) {
	held := worn(20)
	held.Flags[6] |= 2
	got, err := PlanLoginEquipment([]LegacyObject{held, worn(20), worn(20), worn(17)}, 1, 1)
	if err != nil || got.Ready[16] != 0 || got.Ready[19] != 1 || !reflect.DeepEqual(got.Remaining, []int{2, 3}) {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestEquipmentOnlyOldEventsAreRemoved(t *testing.T) {
	old := worn(1)
	old.Flags[5] |= 64
	old.ShotsCurrent = 0
	newEvent := old
	newEvent.Flags[6] |= 4
	got, err := PlanLoginEquipment([]LegacyObject{old, newEvent}, 1, 1)
	if err != nil || !reflect.DeepEqual(got.Removed, []int{0}) || !reflect.DeepEqual(got.Remaining, []int{1}) {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestEquipmentValidationRetainsRejectedItems(t *testing.T) {
	for _, change := range []func(*LegacyObject){
		func(o *LegacyObject) { o.ShotsCurrent = 0 },
		func(o *LegacyObject) { o.ShotsMax = 1501 },
		func(o *LegacyObject) { o.Armor = 76 },
		func(o *LegacyObject) { o.Armor = 20 },
		func(o *LegacyObject) { o.Type = 0; o.DicePlus = 101 },
		func(o *LegacyObject) { o.Type = 0; o.DicePlus = 30 },
	} {
		o := worn(1)
		change(&o)
		got, err := PlanLoginEquipment([]LegacyObject{o}, 1, 1)
		if err != nil || !reflect.DeepEqual(got.Remaining, []int{0}) || len(got.Removed) != 0 {
			t.Fatalf("%+v %v", got, err)
		}
	}
}

func TestEquipmentInvalidWearAbortsWholePlan(t *testing.T) {
	for _, wear := range []byte{0, 21, 255} {
		got, err := PlanLoginEquipment([]LegacyObject{worn(1), worn(wear)}, 1, 1)
		if err == nil || !reflect.DeepEqual(got, EquipmentPlan{}) {
			t.Fatalf("%+v %v", got, err)
		}
	}
}

func TestEquipmentLegacyNumericBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name         string
		object       LegacyObject
		level, class byte
		equipped     bool
	}{
		{"armor exactly thirty", func() LegacyObject { o := worn(1); o.Armor = 15; return o }(), 1, 1, true},
		{"signed negative armor", func() LegacyObject { o := worn(1); o.Armor = 255; return o }(), 1, 1, true},
		{"quest bypass level", func() LegacyObject { o := worn(1); o.Armor = 20; o.Quest = 1; return o }(), 1, 1, true},
		{"quest cannot bypass max armor", func() LegacyObject { o := worn(1); o.Armor = 76; o.Quest = 1; return o }(), 255, 9, false},
		{"fighter damage deduction", func() LegacyObject { o := worn(20); o.Type = 0; o.DicePlus = 22; return o }(), 1, 4, true},
		{"assassin damage deduction", func() LegacyObject { o := worn(20); o.Type = 0; o.DicePlus = 22; return o }(), 1, 1, false},
		{"absolute damage cap", func() LegacyObject { o := worn(20); o.Type = 0; o.DicePlus = 101; o.Quest = 1; return o }(), 255, 9, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := PlanLoginEquipment([]LegacyObject{tc.object}, tc.level, tc.class)
			if err != nil || (len(got.Remaining) == 0) != tc.equipped {
				t.Fatalf("%+v %v", got, err)
			}
		})
	}
}

func TestEquipmentPlanPartitionsEveryInputExactlyOnce(t *testing.T) {
	var inventory []LegacyObject
	for i := 0; i < 80; i++ {
		o := worn(byte(i%20 + 1))
		if i%7 == 0 {
			o.ShotsCurrent = 0
		}
		if i%11 == 0 {
			o.Flags[5] |= 64
		}
		inventory = append(inventory, o)
	}
	before := append([]LegacyObject(nil), inventory...)
	got, err := PlanLoginEquipment(inventory, 100, 4)
	if err != nil {
		t.Fatal(err)
	}
	counts := make([]int, len(inventory))
	for _, index := range got.Ready {
		if index >= 0 {
			counts[index]++
		}
	}
	for _, index := range got.Remaining {
		counts[index]++
	}
	for _, index := range got.Removed {
		counts[index]++
	}
	for index, count := range counts {
		if count != 1 {
			t.Fatalf("item %d classified %d times", index, count)
		}
	}
	if !reflect.DeepEqual(inventory, before) {
		t.Fatal("mutated inventory")
	}
}

func TestEquipmentInvalidUnusedWearIsRetained(t *testing.T) {
	unused := worn(255)
	unused.Flags[2] = 0
	exhausted := worn(0)
	exhausted.ShotsCurrent = 0
	got, err := PlanLoginEquipment([]LegacyObject{unused, exhausted}, 1, 1)
	if err != nil || !reflect.DeepEqual(got.Remaining, []int{0, 1}) {
		t.Fatalf("%+v %v", got, err)
	}
}
