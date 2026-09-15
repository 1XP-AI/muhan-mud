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

func swordFloorFixture(t *testing.T) State {
	t.Helper()
	s := npcAttackFixture(t)
	s.NPCs = nil
	room := s.Rooms[1]
	room.NPCIDs = nil
	room.Items = &ItemCollection{Items: map[string]Item{
		"floor-sword": {Object: LegacyObject{Name: "검"}},
	}, Inventory: []string{"floor-sword"}}
	s.Rooms[1] = room
	player := s.Players["a"]
	player.Items = &ItemCollection{Items: map[string]Item{}}
	s.Players["a"] = player
	return s
}

func TestSelectInventoryRootUsesLegacyEqualFieldsAndStoredOrder(t *testing.T) {
	c := ItemCollection{
		Items: map[string]Item{
			"display": {Object: LegacyObject{Name: "DisplayName"}},
			"key0":    {Object: LegacyObject{Name: "OtherZero", Keys: [3]string{"ZeroAlias", "", ""}}},
			"key1":    {Object: LegacyObject{Name: "OtherOne", Keys: [3]string{"", "OneAlias", ""}}},
			"key2":    {Object: LegacyObject{Name: "OtherTwo", Keys: [3]string{"", "", "TwoAlias"}}},
			"mixed":   {Object: LegacyObject{Name: "MixedRoot", Keys: [3]string{"MixedKey", "", ""}}},
			"mixed-2": {Object: LegacyObject{Name: "LaterRoot", Keys: [3]string{"MixedLater", "", ""}}},
			"hidden":  {Object: LegacyObject{Name: "HiddenRoot", Keys: [3]string{"ShieldAlias", "", ""}}},
			"visible": {Object: LegacyObject{Name: "VisibleRoot", Keys: [3]string{"ShieldVisible", "", ""}}},
			"parent":  {Object: LegacyObject{Name: "ParentRoot"}, Contents: []string{"nested"}},
			"nested":  {Object: LegacyObject{Name: "NestedRoot", Keys: [3]string{"NestedAlias", "", ""}}},
			"ready":   {Object: LegacyObject{Name: "ReadyRoot", Keys: [3]string{"ReadyAlias", "", ""}}},
		},
		Inventory: []string{"display", "key0", "key1", "key2", "mixed", "mixed-2", "hidden", "visible", "parent"},
		Ready:     [20]string{0: "ready"},
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("fixture invalid: %v", err)
	}

	for _, tc := range []struct {
		query      string
		occurrence int
		want       string
	}{
		{query: "display", occurrence: 1, want: "display"},
		{query: "zero", occurrence: 1, want: "key0"},
		{query: "one", occurrence: 1, want: "key1"},
		{query: "two", occurrence: 1, want: "key2"},
		{query: "mixed", occurrence: 1, want: "mixed"},
		{query: "mixed", occurrence: 2, want: "mixed-2"},
	} {
		got, err := selectInventoryRoot(c, tc.query, tc.occurrence, nil)
		if err != nil || got != tc.want {
			t.Fatalf("selectInventoryRoot(%q, %d)=%q err=%v want=%q", tc.query, tc.occurrence, got, err, tc.want)
		}
	}

	visibleCalls := make([]string, 0, 2)
	got, err := selectInventoryRoot(c, "shield", 1, func(object LegacyObject) bool {
		visibleCalls = append(visibleCalls, object.Name)
		return object.Name != "HiddenRoot"
	})
	if err != nil || got != "visible" {
		t.Fatalf("visibility selector=%q err=%v", got, err)
	}
	if want := []string{"HiddenRoot", "VisibleRoot"}; !reflect.DeepEqual(visibleCalls, want) {
		t.Fatalf("visibility callback calls=%v want=%v", visibleCalls, want)
	}

	for _, tc := range []struct {
		query      string
		occurrence int
	}{
		{query: "mixed", occurrence: 0},
		{query: "mixed", occurrence: -1},
		{query: "mixed", occurrence: 3},
		{query: "ready", occurrence: 1},
		{query: "nested", occurrence: 1},
	} {
		if got, err := selectInventoryRoot(c, tc.query, tc.occurrence, nil); err == nil {
			t.Fatalf("selectInventoryRoot(%q, %d)=%q unexpectedly succeeded", tc.query, tc.occurrence, got)
		}
	}
}

func TestTakeAndDropUseLegacyInventoryPrefixes(t *testing.T) {
	s := swordFloorFixture(t)
	room := s.Rooms[1]
	floorSword := room.Items.Items["floor-sword"]
	floorSword.Object.Keys[0] = "BladeAlias"
	room.Items.Items["floor-sword"] = floorSword
	s.Rooms[1] = room

	taken, result, err := s.TakeItem("a", "blade", 1)
	if err != nil || result.Action != "take" || result.ItemName != "검" {
		t.Fatalf("take by key result=%+v err=%v", result, err)
	}

	player := taken.Players["a"]
	item := player.Items.Items["floor-sword"]
	item.Object.Keys[1] = "DropAlias"
	player.Items.Items["floor-sword"] = item
	taken.Players["a"] = player
	dropped, result, err := taken.DropItem("a", "drop", 1)
	if err != nil || result.Action != "drop" || result.ItemName != "검" {
		t.Fatalf("drop by key result=%+v err=%v", result, err)
	}
	if !containsID(dropped.Rooms[1].Items.Inventory, "floor-sword") || containsID(dropped.Players["a"].Items.Inventory, "floor-sword") {
		t.Fatalf("drop by key locations room=%+v player=%+v", dropped.Rooms[1].Items.Inventory, dropped.Players["a"].Items.Inventory)
	}
}

func TestTakeAndDropSwordKeepsActorRoomAndFailsClosed(t *testing.T) {
	s := swordFloorFixture(t)
	taken, result, err := s.TakeItem("a", "검", 1)
	if err != nil || result.Action != "take" || result.ItemName != "검" {
		t.Fatalf("take result=%+v err=%v", result, err)
	}
	if taken.Players["a"].Body.RoomID != 1 {
		t.Fatalf("take moved actor room=%d", taken.Players["a"].Body.RoomID)
	}
	if containsID(taken.Rooms[1].Items.Inventory, "floor-sword") || !containsID(taken.Players["a"].Items.Inventory, "floor-sword") {
		t.Fatalf("take locations room=%+v player=%+v", taken.Rooms[1].Items, taken.Players["a"].Items)
	}

	dropped, result, err := taken.DropItem("a", "검", 1)
	if err != nil || result.Action != "drop" || result.ItemName != "검" {
		t.Fatalf("drop result=%+v err=%v", result, err)
	}
	if dropped.Players["a"].Body.RoomID != 1 {
		t.Fatalf("drop moved actor room=%d", dropped.Players["a"].Body.RoomID)
	}
	if !containsID(dropped.Rooms[1].Items.Inventory, "floor-sword") || containsID(dropped.Players["a"].Items.Inventory, "floor-sword") {
		t.Fatalf("drop locations room=%+v player=%+v", dropped.Rooms[1].Items, dropped.Players["a"].Items)
	}

	if next, _, err := s.TakeItem("a", "없는검", 1); err == nil || !reflect.DeepEqual(next, State{}) {
		t.Fatalf("missing take committed next=%+v err=%v", next, err)
	}
	if next, _, err := taken.DropItem("a", "없는검", 1); err == nil || !reflect.DeepEqual(next, State{}) {
		t.Fatalf("missing drop committed next=%+v err=%v", next, err)
	}

	unmigratedPlayer := swordFloorFixture(t)
	player := unmigratedPlayer.Players["a"]
	player.Items = nil
	unmigratedPlayer.Players["a"] = player
	if next, _, err := unmigratedPlayer.TakeItem("a", "검", 1); err == nil || !reflect.DeepEqual(next, State{}) {
		t.Fatalf("unmigrated player take committed next=%+v err=%v", next, err)
	}

	unmigratedRoom := swordFloorFixture(t)
	room := unmigratedRoom.Rooms[1]
	room.Items = nil
	unmigratedRoom.Rooms[1] = room
	if next, _, err := unmigratedRoom.TakeItem("a", "검", 1); err == nil || !reflect.DeepEqual(next, State{}) {
		t.Fatalf("unmigrated room take committed next=%+v err=%v", next, err)
	}
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

	next, result, err := s.TakeContainedItem("a", "가방", "보석", 1, 1)
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
	next, result, err = next.DropContainedItem("a", "보석", "가방", 1, 1)
	if err != nil || result.Action != "drop-contained" || !containsID(next.Players["a"].Items.Inventory, "floor-bag") {
		t.Fatalf("drop contained result=%+v player=%+v err=%v", result, next.Players["a"].Items, err)
	}
	if len(next.Players["a"].Items.Items["floor-bag"].Contents) != 1 || next.Players["a"].Items.Items["floor-bag"].Contents[0] != "floor-gem" {
		t.Fatalf("container after drop=%+v", next.Players["a"].Items.Items["floor-bag"])
	}
}
