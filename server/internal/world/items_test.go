package world

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

func itemFixture(t *testing.T) ItemCollection {
	t.Helper()
	n := 0
	got, err := ImportItems([]LegacyObject{{Name: "bag", Contents: []LegacyObject{{Name: "coin"}}}, worn(1)}, func() (string, error) { n++; return fmt.Sprintf("item-%d", n), nil })
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestItemsImportPreservesIdentityAndNesting(t *testing.T) {
	c := itemFixture(t)
	if len(c.Items) != 3 || !reflect.DeepEqual(c.Inventory, []string{"item-1", "item-3"}) || !reflect.DeepEqual(c.Items["item-1"].Contents, []string{"item-2"}) || len(c.Items["item-1"].Object.Contents) != 0 {
		t.Fatalf("%+v", c)
	}
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var roundtrip ItemCollection
	if err = json.Unmarshal(raw, &roundtrip); err != nil {
		t.Fatal(err)
	}
	if err = roundtrip.Validate(); err != nil || !reflect.DeepEqual(c, roundtrip) {
		t.Fatalf("%+v %v", roundtrip, err)
	}
}

func TestItemsRejectInvalidOwnership(t *testing.T) {
	for _, mutate := range []func(*ItemCollection){
		func(c *ItemCollection) { c.Ready[0] = "item-3" },
		func(c *ItemCollection) { delete(c.Items, "item-2") },
		func(c *ItemCollection) { c.Inventory = nil },
		func(c *ItemCollection) {
			item := c.Items["item-2"]
			item.Contents = []string{"item-1"}
			c.Items["item-2"] = item
		},
		func(c *ItemCollection) {
			item := c.Items["item-1"]
			item.Object.Contents = []LegacyObject{{Name: "duplicate"}}
			c.Items["item-1"] = item
		},
	} {
		c := itemFixture(t)
		mutate(&c)
		if err := c.Validate(); err == nil {
			t.Fatalf("accepted %+v", c)
		}
	}
}

func TestItemImportDuplicateIDsReturnsNoPartialData(t *testing.T) {
	got, err := ImportItems([]LegacyObject{{}, {}}, func() (string, error) { return "same", nil })
	if err == nil || !reflect.DeepEqual(got, ItemCollection{}) {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestItemEquipmentKeepsIDsAndRemovesEventSubtree(t *testing.T) {
	c := itemFixture(t)
	item := c.Items["item-1"]
	item.Object.Flags[5] |= 64
	c.Items["item-1"] = item
	next, removed, err := c.RestoreEquipment(100, 4)
	if err != nil || next.Ready[0] != "item-3" || len(next.Inventory) != 0 || len(next.Items) != 1 || !reflect.DeepEqual(removed, []string{"item-1", "item-2"}) {
		t.Fatalf("%+v %v %v", next, removed, err)
	}
	if len(c.Items) != 3 || c.Ready[0] != "" {
		t.Fatal("mutated source")
	}
	if _, _, err := next.RestoreEquipment(100, 4); err == nil {
		t.Fatal("reimport into occupied ready slots")
	}
}

func TestStateCanonicalItemsSurviveRecoveryAndRejectDuplicates(t *testing.T) {
	s := stateFixture()
	p := s.Players["a"]
	items := itemFixture(t)
	p.Items = &items
	s.Players["a"] = p
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	next, _, err := s.RecoverOffline()
	if err != nil {
		t.Fatal(err)
	}
	item := next.Players["a"].Items.Items["item-1"]
	item.Contents[0] = "changed"
	if s.Players["a"].Items.Items["item-1"].Contents[0] != "item-2" {
		t.Fatal("nested ID list alias")
	}
	p.Online = false
	s.Players["b"] = p
	if err := s.Validate(); err == nil {
		t.Fatal("cross-player item duplication accepted")
	}
	delete(s.Players, "b")
	p.Online = true
	p.Body.Inventory = []LegacyObject{{}}
	s.Players["a"] = p
	if err := s.Validate(); err == nil {
		t.Fatal("duplicate legacy inventory accepted")
	}
}
