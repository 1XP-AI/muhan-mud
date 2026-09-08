package world

import (
	"reflect"
	"testing"
)

func TestTransferItemRootsPreservesOwnership(t *testing.T) {
	src := ItemCollection{Items: map[string]Item{"bag": {Object: LegacyObject{Name: "bag"}, Contents: []string{"coin"}}, "coin": {Object: LegacyObject{Name: "coin"}}, "stay": {Object: LegacyObject{Name: "stay"}}}, Inventory: []string{"bag", "stay"}}
	dst := ItemCollection{Items: map[string]Item{"old": {Object: LegacyObject{Name: "apple"}}}, Inventory: []string{"old"}}
	beforeSrc, beforeDst := src.clone(), dst.clone()
	got, err := TransferItemRoots(src, dst, []string{"bag"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Source.Items) != 1 || len(got.Destination.Items) != 3 || !reflect.DeepEqual(got.Destination.Inventory, []string{"old", "bag"}) || got.Destination.Items["bag"].Contents[0] != "coin" {
		t.Fatalf("%+v", got)
	}
	if !reflect.DeepEqual(src, beforeSrc) || !reflect.DeepEqual(dst, beforeDst) {
		t.Fatal("mutated input")
	}
	item := got.Destination.Items["bag"]
	item.Contents[0] = "changed"
	if src.Items["bag"].Contents[0] != "coin" {
		t.Fatal("aliased child list")
	}
}

func TestTransferItemRootsRejectsAmbiguity(t *testing.T) {
	src := ItemCollection{Items: map[string]Item{"a": {Contents: []string{"child"}}, "child": {}}, Inventory: []string{"a"}}
	empty := ItemCollection{Items: map[string]Item{}}
	for _, roots := range [][]string{{"missing"}, {"child"}, {"a", "a"}} {
		got, err := TransferItemRoots(src, empty, roots)
		if err == nil || !reflect.DeepEqual(got, ItemTransferPlan{}) {
			t.Fatalf("roots%v %+v %v", roots, got, err)
		}
	}
	got, err := TransferItemRoots(src, src, []string{"a"})
	if err == nil || !reflect.DeepEqual(got, ItemTransferPlan{}) {
		t.Fatal("accepted duplicate ownership")
	}
	src.Ready[0] = "a"
	src.Inventory = nil
	if _, err := TransferItemRoots(src, empty, []string{"a"}); err == nil {
		t.Fatal("accepted equipped root")
	}
}

func TestDeathDroppedItemsMergeWithoutMintingIDs(t *testing.T) {
	items := ItemCollection{Items: map[string]Item{"weapon": {Object: LegacyObject{Name: "blade"}, Contents: []string{"gem"}}, "gem": {}}}
	items.Ready[19] = "weapon"
	death, err := PlanDeathEquipment(items, DeathEquipmentRules{AttackerPlayer: true, WarAllowsLoss: true})
	if err != nil {
		t.Fatal(err)
	}
	floor := ItemCollection{Items: map[string]Item{"existing": {Object: LegacyObject{Name: "blade"}}}, Inventory: []string{"existing"}}
	merged, err := TransferItemRoots(death.Dropped, floor, death.Dropped.Inventory)
	if err != nil {
		t.Fatal(err)
	}
	if len(death.Kept.Items) != 0 || len(merged.Source.Items) != 0 || len(merged.Destination.Items) != 3 {
		t.Fatal("ownership count")
	}
	if !reflect.DeepEqual(merged.Destination.Inventory, []string{"existing", "weapon"}) {
		t.Fatal("equal item order")
	}
	for _, id := range []string{"weapon", "gem"} {
		if merged.Destination.Items[id].Object.Flags[1]&3 != 3 {
			t.Fatal("lost temporary flags")
		}
	}
}
