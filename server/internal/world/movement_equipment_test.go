package world

import (
	"strconv"
	"testing"
)

func TestMovementEquipmentPreservesWeightlessRootRules(t *testing.T) {
	c := ItemCollection{Items: map[string]Item{
		"bag":   {Object: LegacyObject{Weight: 5, Flags: [8]byte{128}}, Contents: []string{"heavy"}},
		"heavy": {Object: LegacyObject{Weight: 100}},
		"rope":  {Object: LegacyObject{Weight: 2, DicePlus: 3, Flags: [8]byte{0, 0, 1}}},
	}, Inventory: []string{"bag"}, Ready: [20]string{0: "rope"}}
	stats, err := c.MovementStats(14)
	if err != nil || stats.CarriedWeight != 2 || stats.FallSkill != 14 || stats.DexterityBonus != 1 {
		t.Fatalf("%+v %v", stats, err)
	}
	c.Inventory = nil
	c.Ready[1] = "bag"
	stats, err = c.MovementStats(14)
	if err != nil || stats.CarriedWeight != 107 {
		t.Fatalf("ready weightless root ignored: %+v %v", stats, err)
	}
	item := c.Items["heavy"]
	item.Object.Flags[0] |= 128
	c.Items["heavy"] = item
	stats, err = c.MovementStats(14)
	if err != nil || stats.CarriedWeight != 7 {
		t.Fatalf("weightless child counted: %+v %v", stats, err)
	}
}
func TestMovementEquipmentRejectsUnknownDexterityAndOwnership(t *testing.T) {
	c := ItemCollection{Items: map[string]Item{}}
	for _, dex := range []byte{64, 255} {
		got, err := c.MovementStats(dex)
		if err == nil || got != (MovementEquipment{}) {
			t.Fatal("invalid bonus index accepted")
		}
	}
	c.Inventory = []string{"missing"}
	if _, err := c.MovementStats(10); err == nil {
		t.Fatal("invalid ownership accepted")
	}
}

func TestMovementEquipmentRejectsSignedWeightOverflow(t *testing.T) {
	for _, weight := range []int16{32767, -32768} {
		c := ItemCollection{Items: make(map[string]Item, 65539), Inventory: make([]string, 65539)}
		for i := range c.Inventory {
			id := strconv.Itoa(i)
			c.Inventory[i] = id
			c.Items[id] = Item{Object: LegacyObject{Weight: weight}}
		}
		got, err := c.MovementStats(10)
		if err == nil || got != (MovementEquipment{}) {
			t.Fatalf("signed weight %d overflow returned %+v: %v", weight, got, err)
		}
	}
}
