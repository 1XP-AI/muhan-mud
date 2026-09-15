package world

import (
	"reflect"
	"testing"
)

func deathItems() ItemCollection {
	return ItemCollection{Items: map[string]Item{
		"weapon": {Object: LegacyObject{Name: "weapon"}, Contents: []string{"child"}}, "child": {Object: LegacyObject{Name: "child"}}, "armor": {Object: LegacyObject{Name: "armor"}}, "bag": {Object: LegacyObject{Name: "bag"}},
	}, Inventory: []string{"bag"}, Ready: [20]string{0: "armor", 19: "weapon"}}
}

func TestDeathEquipmentNPCDropsWeaponReturnsArmorToInventory(t *testing.T) {
	c := deathItems()
	got, err := PlanDeathEquipment(c, DeathEquipmentRules{WarAllowsLoss: true})
	if err != nil || !reflect.DeepEqual(got.Kept.Inventory, []string{"armor", "bag"}) || !reflect.DeepEqual(got.Dropped.Inventory, []string{"weapon"}) || len(got.Dropped.Items) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	for _, id := range []string{"weapon", "child"} {
		if got.Dropped.Items[id].Object.Flags[1]&1 == 0 {
			t.Fatal("missing recursive temporary flag")
		}
	}
	if c.Ready[19] != "weapon" || len(c.Items) != 4 {
		t.Fatal("changed source")
	}
}

func TestDeathEquipmentPlayerAndSurvivalRules(t *testing.T) {
	c := deathItems()
	got, err := PlanDeathEquipment(c, DeathEquipmentRules{WarAllowsLoss: true, AttackerPlayer: true})
	if err != nil || len(got.Kept.Items) != 1 || !reflect.DeepEqual(got.Dropped.Inventory, []string{"armor", "weapon"}) {
		t.Fatalf("%+v %v", got, err)
	}
	for _, rules := range []DeathEquipmentRules{{Survival: true, WarAllowsLoss: true}, {WarAllowsLoss: false}} {
		got, err := PlanDeathEquipment(c, rules)
		if err != nil || !reflect.DeepEqual(got.Kept, c) || len(got.Dropped.Items) != 0 {
			t.Fatalf("%+v %v", got, err)
		}
	}
}

func TestDeathEquipmentProtectedAndLegacyContainerFlag(t *testing.T) {
	for _, bit := range []uint{22, 50} {
		c := deathItems()
		i := c.Items["armor"]
		i.Object.Flags[bit/8] |= 1 << (bit % 8)
		c.Items["armor"] = i
		got, err := PlanDeathEquipment(c, DeathEquipmentRules{WarAllowsLoss: true, AttackerPlayer: true})
		if err != nil || got.Kept.Ready[0] != "armor" {
			t.Fatalf("%+v %v", got, err)
		}
	}
	c := deathItems()
	i := c.Items["weapon"]
	i.Object.Flags[1] |= 2
	c.Items["weapon"] = i // source tests bit CONTAINER=9, not type
	got, err := PlanDeathEquipment(c, DeathEquipmentRules{WarAllowsLoss: true})
	if err != nil || len(got.Dropped.Items) != 0 || len(got.Kept.Inventory) != 3 {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestDeathEquipmentPartitionConservesEveryIdentity(t *testing.T) {
	for _, player := range []bool{false, true} {
		c := deathItems()
		got, err := PlanDeathEquipment(c, DeathEquipmentRules{WarAllowsLoss: true, AttackerPlayer: player})
		if err != nil {
			t.Fatal(err)
		}
		for id := range c.Items {
			_, kept := got.Kept.Items[id]
			_, dropped := got.Dropped.Items[id]
			if kept == dropped {
				t.Fatalf("item %s duplicated or lost", id)
			}
		}
		if len(got.Kept.Items)+len(got.Dropped.Items) != len(c.Items) {
			t.Fatal("identity count changed")
		}
	}
	c := deathItems()
	item := c.Items["weapon"]
	item.Object.Quest = 1
	c.Items["weapon"] = item
	got, err := PlanDeathEquipment(c, DeathEquipmentRules{WarAllowsLoss: true, AttackerPlayer: true})
	if err != nil || got.Kept.Ready[19] != "weapon" || len(got.Kept.Items["weapon"].Contents) != 1 {
		t.Fatalf("quest equipment lost: %+v %v", got, err)
	}
}
