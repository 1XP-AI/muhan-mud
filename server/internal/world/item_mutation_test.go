package world

import (
	"reflect"
	"strings"
	"testing"
)

func itemMutationFixture(t *testing.T) State {
	t.Helper()
	s := playerItemsFixture(t)
	roomItems := ItemCollection{Items: map[string]Item{
		"floor-bag":    {Object: LegacyObject{Name: "가방"}, Contents: []string{"floor-gem"}},
		"floor-gem":    {Object: LegacyObject{Name: "보석"}},
		"floor-hidden": {Object: LegacyObject{Name: "숨은물건", Flags: [8]byte{0: 1 << objectInvisibleFlag}}},
	}, Inventory: []string{"floor-bag", "floor-hidden"}}
	room := s.Rooms[1]
	room.PlayerIDs = []string{"a"}
	room.Items = &roomItems
	s.Rooms[1] = room
	return s
}

func TestTakeAndDropItemMoveWholeCanonicalSubtree(t *testing.T) {
	s := itemMutationFixture(t)
	next, result, err := s.TakeItem("a", "가방", 1)
	if err != nil || result.ItemName != "가방" || len(next.Rooms[1].Items.Items) != 1 || len(next.Players["a"].Items.Items) != 6 {
		t.Fatalf("take result=%+v room=%+v player=%+v err=%v", result, next.Rooms[1].Items, next.Players["a"].Items, err)
	}
	dropped, result, err := next.DropItem("a", "가방", 1)
	if err != nil || result.Action != "drop" || len(dropped.Rooms[1].Items.Items) != 3 || len(dropped.Players["a"].Items.Items) != 4 {
		t.Fatalf("drop result=%+v room=%+v player=%+v err=%v", result, dropped.Rooms[1].Items, dropped.Players["a"].Items, err)
	}
}

func TestTakeItemRespectsBlindAndInvisibleBoundaries(t *testing.T) {
	s := itemMutationFixture(t)
	if _, _, err := s.TakeItem("a", "숨은물건", 1); err == nil {
		t.Fatal("invisible floor item was taken without detection")
	}
	p := s.Players["a"]
	p.Body.Flags[playerDetectInvisibleFlag/8] |= 1 << (playerDetectInvisibleFlag % 8)
	s.Players["a"] = p
	if _, result, err := s.TakeItem("a", "숨은물건", 1); err != nil || result.ItemName != "숨은물건" {
		t.Fatalf("detected item result=%+v err=%v", result, err)
	}
	p = s.Players["a"]
	p.Body.Flags[playerBlindFlag/8] |= 1 << (playerBlindFlag % 8)
	s.Players["a"] = p
	if _, _, err := s.TakeItem("a", "가방", 1); err == nil || !strings.Contains(err.Error(), "보이지") {
		t.Fatalf("blind take err=%v", err)
	}
}

func TestItemCollectionWeightAndCapacityMatchLegacyBoundaries(t *testing.T) {
	c := ItemCollection{Items: map[string]Item{
		"bag":   {Object: LegacyObject{Name: "가방", Weight: 2, Flags: [8]byte{0: 1 << itemContainerFlag}}, Contents: []string{"gem"}},
		"gem":   {Object: LegacyObject{Name: "보석", Weight: 3}},
		"sword": {Object: LegacyObject{Name: "검", Weight: 4}},
	}, Inventory: []string{"bag"}, Ready: [20]string{19: "sword"}}
	weight, err := c.Weight()
	if err != nil || weight != 9 {
		t.Fatalf("weight=%d err=%v", weight, err)
	}
	count, err := c.CapacityCount()
	if err != nil || count != 3 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	bag := c.Items["bag"]
	bag.Object.Flags[0] |= 1 << itemWeightlessFlag
	c.Items["bag"] = bag
	weight, err = c.Weight()
	if err != nil || weight != 4 {
		t.Fatalf("weightless weight=%d err=%v", weight, err)
	}
}

func TestTakeAndDropRejectGuardsCapacityAndProtectedItems(t *testing.T) {
	guarded := itemMutationFixture(t)
	guarded.NPCs = map[string]NPCState{"guard": {Body: LegacyMonster{Name: "경비", Type: 1, RoomID: 1, Flags: [8]byte{0: 1 << monsterGuardFlag}}}}
	r := guarded.Rooms[1]
	r.NPCIDs = []string{"guard"}
	guarded.Rooms[1] = r
	if next, _, err := guarded.TakeItem("a", "가방", 1); err == nil || !strings.Contains(err.Error(), "경비") || !reflect.DeepEqual(next, State{}) {
		t.Fatalf("guard bypass: next=%+v err=%v", next, err)
	}

	heavy := itemMutationFixture(t)
	r = heavy.Rooms[1]
	floor := r.Items.Items["floor-bag"]
	floor.Object.Weight = 121
	r.Items.Items["floor-bag"] = floor
	heavy.Rooms[1] = r
	if next, _, err := heavy.TakeItem("a", "가방", 1); err == nil || !strings.Contains(err.Error(), "가질") || !reflect.DeepEqual(next, State{}) {
		t.Fatalf("heavy item accepted: next=%+v err=%v", next, err)
	}

	protected := itemMutationFixture(t)
	r = protected.Rooms[1]
	floor = r.Items.Items["floor-bag"]
	floor.Object.Flags[objectNotTakeFlag/8] |= 1 << (objectNotTakeFlag % 8)
	r.Items.Items["floor-bag"] = floor
	protected.Rooms[1] = r
	if _, _, err := protected.TakeItem("a", "가방", 1); err == nil {
		t.Fatal("not-take item accepted")
	}

	player := protected.Players["a"]
	item := player.Items.Items["cloak"]
	item.Object.Quest = 1
	player.Items.Items["cloak"] = item
	protected.Players["a"] = player
	if next, _, err := protected.DropItem("a", "망토", 1); err == nil || !strings.Contains(err.Error(), "임무") || !reflect.DeepEqual(next, State{}) {
		t.Fatalf("quest item dropped: next=%+v err=%v", next, err)
	}
}

func TestTakeAndDropAllUseCanonicalVisibilityAndProtectedRoots(t *testing.T) {
	s := itemMutationFixture(t)
	next, result, err := s.TakeAllItems("a")
	if err != nil || result.Action != "take-all" || result.Count != 1 || len(next.Rooms[1].Items.Items) != 1 || len(next.Players["a"].Items.Items) != 6 {
		t.Fatalf("take all result=%+v room=%+v player=%+v err=%v", result, next.Rooms[1].Items, next.Players["a"].Items, err)
	}
	dropped, result, err := next.DropAllItems("a")
	if err != nil || result.Action != "drop-all" || result.Count != 4 || len(dropped.Rooms[1].Items.Items) != 6 || len(dropped.Players["a"].Items.Items) != 1 {
		t.Fatalf("drop all result=%+v room=%+v player=%+v err=%v", result, dropped.Rooms[1].Items, dropped.Players["a"].Items, err)
	}
}

func TestTakeAndDropContainedRootsPreserveContainerOwnership(t *testing.T) {
	s := itemMutationFixture(t)
	r := s.Rooms[1]
	bag := r.Items.Items["floor-bag"]
	bag.Object.Flags[0] |= 1 << itemContainerFlag
	bag.Object.ShotsMax = 2
	bag.Object.ShotsCurrent = 1
	r.Items.Items["floor-bag"] = bag
	s.Rooms[1] = r

	next, result, err := s.TakeContainedItem("a", "가방", "보석", 1)
	if err != nil || result.Action != "take-contained" || result.ItemName != "보석" || len(next.Rooms[1].Items.Items) != 2 || !containsID(next.Players["a"].Items.Inventory, "floor-gem") {
		t.Fatalf("take contained result=%+v room=%+v player=%+v err=%v", result, next.Rooms[1].Items, next.Players["a"].Items, err)
	}
	if len(next.Rooms[1].Items.Items["floor-bag"].Contents) != 0 || next.Rooms[1].Items.Items["floor-bag"].Object.ShotsCurrent != 0 {
		t.Fatalf("container after take=%+v", next.Rooms[1].Items.Items["floor-bag"])
	}

	// The floor bag is not in the player's inventory yet; take the root first.
	next, _, err = next.TakeItem("a", "가방", 1)
	if err != nil {
		t.Fatal(err)
	}
	next, result, err = next.DropContainedItem("a", "보석", "가방", 1)
	if err != nil || result.Action != "drop-contained" || !containsID(next.Players["a"].Items.Inventory, "floor-bag") {
		t.Fatalf("drop contained result=%+v player=%+v err=%v", result, next.Players["a"].Items, err)
	}
	if len(next.Players["a"].Items.Items["floor-bag"].Contents) != 1 || next.Players["a"].Items.Items["floor-bag"].Contents[0] != "floor-gem" {
		t.Fatalf("container after drop=%+v", next.Players["a"].Items.Items["floor-bag"])
	}
}
