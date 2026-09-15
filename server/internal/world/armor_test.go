package world

import "testing"

func TestArmorClassStatsEquipmentProtectionAndClamp(t *testing.T) {
	for _, tc := range []struct {
		dex       byte
		armor     int8
		protected bool
		want      int8
	}{
		{0, 0, false, 120}, {10, 0, false, 100}, {63, 0, false, 65}, {127, 0, false, 65},
		{10, 20, false, 80}, {10, 20, true, 70}, {0, -128, false, 127}, {63, 127, true, -72},
	} {
		var ready [20]*LegacyObject
		ready[19] = &LegacyObject{Armor: byte(tc.armor)}
		got, err := ComputeArmorClass(tc.dex, ready, tc.protected)
		if err != nil || got != tc.want {
			t.Fatalf("%+v got%d %v", tc, got, err)
		}
	}
	var ready [20]*LegacyObject
	for i := range ready {
		ready[i] = &LegacyObject{Armor: 127}
	}
	if got, err := ComputeArmorClass(63, ready, true); err != nil || got != -127 {
		t.Fatalf("%d %v", got, err)
	}
}

func TestArmorRejectsNegativeLegacyDexterity(t *testing.T) {
	if _, err := ComputeArmorClass(255, [20]*LegacyObject{}, false); err == nil {
		t.Fatal("negative legacy dexterity used as bonus index")
	}
}

func TestArmorUsesOnlyRestoredEquipment(t *testing.T) {
	body := worn(1)
	body.Armor = 15
	spare := body
	spare.Armor = 20
	inventory := []LegacyObject{body, spare}
	plan, err := PlanLoginEquipment(inventory, 100, 4)
	if err != nil {
		t.Fatal(err)
	}
	var ready [20]*LegacyObject
	for slot, index := range plan.Ready {
		if index >= 0 {
			ready[slot] = &inventory[index]
		}
	}
	got, err := ComputeArmorClass(10, ready, false)
	if err != nil || got != 85 || len(plan.Remaining) != 1 {
		t.Fatalf("ac%d %+v %v", got, plan, err)
	}
}
