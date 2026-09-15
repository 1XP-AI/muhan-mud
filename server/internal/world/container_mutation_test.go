package world

import "testing"

func twoGemBagState(t *testing.T) State {
	t.Helper()
	s := itemMutationFixture(t)
	r := s.Rooms[1]
	r.Items = &ItemCollection{Items: map[string]Item{
		"floor-bag":   {Object: LegacyObject{Name: "가방", Flags: [8]byte{0: 1 << itemContainerFlag}, ShotsMax: 2, ShotsCurrent: 2}, Contents: []string{"floor-gem-1", "floor-gem-2"}},
		"floor-gem-1": {Object: LegacyObject{Name: "보석"}},
		"floor-gem-2": {Object: LegacyObject{Name: "보석"}},
	}, Inventory: []string{"floor-bag"}}
	s.Rooms[1] = r
	return s
}

func twoBagState(t *testing.T) State {
	t.Helper()
	s := itemMutationFixture(t)
	r := s.Rooms[1]
	r.Items = &ItemCollection{Items: map[string]Item{
		"floor-bag-1": {Object: LegacyObject{Name: "가방", Flags: [8]byte{0: 1 << itemContainerFlag}, ShotsMax: 2, ShotsCurrent: 1}, Contents: []string{"floor-gem-1"}},
		"floor-bag-2": {Object: LegacyObject{Name: "가방", Flags: [8]byte{0: 1 << itemContainerFlag}, ShotsMax: 2, ShotsCurrent: 1}, Contents: []string{"floor-gem-2"}},
		"floor-gem-1": {Object: LegacyObject{Name: "보석"}},
		"floor-gem-2": {Object: LegacyObject{Name: "보석"}},
	}, Inventory: []string{"floor-bag-1", "floor-bag-2"}}
	s.Rooms[1] = r
	return s
}

func twoBagDropState(t *testing.T) State {
	t.Helper()
	s := itemMutationFixture(t)
	r := s.Rooms[1]
	r.Items = &ItemCollection{Items: map[string]Item{}}
	s.Rooms[1] = r
	p := s.Players["a"]
	p.Items = &ItemCollection{Items: map[string]Item{
		"bag-1": {Object: LegacyObject{Name: "가방", Flags: [8]byte{0: 1 << itemContainerFlag}, ShotsMax: 2, ShotsCurrent: 0}},
		"bag-2": {Object: LegacyObject{Name: "가방", Flags: [8]byte{0: 1 << itemContainerFlag}, ShotsMax: 2, ShotsCurrent: 0}},
		"gem":   {Object: LegacyObject{Name: "보석"}},
	}, Inventory: []string{"bag-1", "bag-2", "gem"}}
	s.Players["a"] = p
	return s
}

func TestTakeContainedItemSelectsNthChild(t *testing.T) {
	s := twoGemBagState(t)
	next, result, err := s.TakeContainedItem("a", "가방", "보석", 2, 1)
	if err != nil || result.Action != "take-contained" || result.ItemName != "보석" {
		t.Fatalf("take 2nd gem result=%+v err=%v", result, err)
	}
	if containsID(next.Players["a"].Items.Inventory, "floor-gem-1") || !containsID(next.Players["a"].Items.Inventory, "floor-gem-2") {
		t.Fatalf("took first gem player=%+v", next.Players["a"].Items)
	}
	if !containsID(next.Rooms[1].Items.Items["floor-bag"].Contents, "floor-gem-1") || containsID(next.Rooms[1].Items.Items["floor-bag"].Contents, "floor-gem-2") {
		t.Fatalf("bag after take=%+v", next.Rooms[1].Items.Items["floor-bag"])
	}
}

func TestTakeContainedItemSelectsNthContainer(t *testing.T) {
	s := twoBagState(t)
	next, result, err := s.TakeContainedItem("a", "가방", "보석", 1, 2)
	if err != nil || result.Action != "take-contained" {
		t.Fatalf("take from 2nd bag result=%+v err=%v", result, err)
	}
	if containsID(next.Players["a"].Items.Inventory, "floor-gem-1") || !containsID(next.Players["a"].Items.Inventory, "floor-gem-2") {
		t.Fatalf("took from first bag player=%+v", next.Players["a"].Items)
	}
	if !containsID(next.Rooms[1].Items.Items["floor-bag-1"].Contents, "floor-gem-1") || containsID(next.Rooms[1].Items.Items["floor-bag-2"].Contents, "floor-gem-2") {
		t.Fatalf("2nd bag still held gem room=%+v", next.Rooms[1].Items)
	}
}

func TestDropContainedItemSelectsNthContainer(t *testing.T) {
	s := twoBagDropState(t)
	next, result, err := s.DropContainedItem("a", "보석", "가방", 1, 2)
	if err != nil || result.Action != "drop-contained" {
		t.Fatalf("drop into 2nd bag result=%+v err=%v", result, err)
	}
	if containsID(next.Players["a"].Items.Inventory, "gem") || containsID(next.Players["a"].Items.Items["bag-1"].Contents, "gem") || !containsID(next.Players["a"].Items.Items["bag-2"].Contents, "gem") {
		t.Fatalf("dropped into first bag player=%+v", next.Players["a"].Items)
	}
}

func twoBagThreeGemState(t *testing.T) State {
	t.Helper()
	s := itemMutationFixture(t)
	r := s.Rooms[1]
	r.Items = &ItemCollection{Items: map[string]Item{
		"floor-bag-1":   {Object: LegacyObject{Name: "가방", Flags: [8]byte{0: 1 << itemContainerFlag}, ShotsMax: 3, ShotsCurrent: 1}, Contents: []string{"floor-gem-1-1"}},
		"floor-bag-2":   {Object: LegacyObject{Name: "가방", Flags: [8]byte{0: 1 << itemContainerFlag}, ShotsMax: 3, ShotsCurrent: 3}, Contents: []string{"floor-gem-2-1", "floor-gem-2-2", "floor-gem-2-3"}},
		"floor-gem-1-1": {Object: LegacyObject{Name: "보석"}},
		"floor-gem-2-1": {Object: LegacyObject{Name: "보석"}},
		"floor-gem-2-2": {Object: LegacyObject{Name: "보석"}},
		"floor-gem-2-3": {Object: LegacyObject{Name: "보석"}},
	}, Inventory: []string{"floor-bag-1", "floor-bag-2"}}
	s.Rooms[1] = r
	return s
}

func threeGemTwoBagDropState(t *testing.T) State {
	t.Helper()
	s := itemMutationFixture(t)
	r := s.Rooms[1]
	r.Items = &ItemCollection{Items: map[string]Item{}}
	s.Rooms[1] = r
	p := s.Players["a"]
	p.Items = &ItemCollection{Items: map[string]Item{
		"bag-1": {Object: LegacyObject{Name: "가방", Flags: [8]byte{0: 1 << itemContainerFlag}, ShotsMax: 3, ShotsCurrent: 0}},
		"bag-2": {Object: LegacyObject{Name: "가방", Flags: [8]byte{0: 1 << itemContainerFlag}, ShotsMax: 3, ShotsCurrent: 0}},
		"gem-1": {Object: LegacyObject{Name: "보석"}},
		"gem-2": {Object: LegacyObject{Name: "보석"}},
		"gem-3": {Object: LegacyObject{Name: "보석"}},
	}, Inventory: []string{"bag-1", "bag-2", "gem-1", "gem-2", "gem-3"}}
	s.Players["a"] = p
	return s
}

func TestTakeContainedItemSelectsNthChildFromNthContainer(t *testing.T) {
	s := twoBagThreeGemState(t)
	next, result, err := s.TakeContainedItem("a", "가방", "보석", 3, 2)
	if err != nil || result.Action != "take-contained" || result.ItemName != "보석" {
		t.Fatalf("take 3rd gem from 2nd bag result=%+v err=%v", result, err)
	}
	if containsID(next.Players["a"].Items.Inventory, "floor-gem-1-1") || containsID(next.Players["a"].Items.Inventory, "floor-gem-2-1") || containsID(next.Players["a"].Items.Inventory, "floor-gem-2-2") || !containsID(next.Players["a"].Items.Inventory, "floor-gem-2-3") {
		t.Fatalf("took wrong gem player=%+v", next.Players["a"].Items)
	}
	if !containsID(next.Rooms[1].Items.Items["floor-bag-1"].Contents, "floor-gem-1-1") || containsID(next.Rooms[1].Items.Items["floor-bag-2"].Contents, "floor-gem-2-3") || !containsID(next.Rooms[1].Items.Items["floor-bag-2"].Contents, "floor-gem-2-1") {
		t.Fatalf("bags after take=%+v", next.Rooms[1].Items)
	}
}

func TestDropContainedItemSelectsNthItemIntoNthContainer(t *testing.T) {
	s := threeGemTwoBagDropState(t)
	next, result, err := s.DropContainedItem("a", "보석", "가방", 3, 2)
	if err != nil || result.Action != "drop-contained" {
		t.Fatalf("drop 3rd gem into 2nd bag result=%+v err=%v", result, err)
	}
	if !containsID(next.Players["a"].Items.Inventory, "gem-1") || !containsID(next.Players["a"].Items.Inventory, "gem-2") || containsID(next.Players["a"].Items.Inventory, "gem-3") {
		t.Fatalf("dropped wrong gem inventory=%+v", next.Players["a"].Items)
	}
	if containsID(next.Players["a"].Items.Items["bag-1"].Contents, "gem-3") || !containsID(next.Players["a"].Items.Items["bag-2"].Contents, "gem-3") {
		t.Fatalf("dropped into wrong bag player=%+v", next.Players["a"].Items)
	}
}

func containerBag(contents []string) Item {
	return Item{Object: LegacyObject{Name: "가방", Flags: [8]byte{0: 1 << itemContainerFlag}, ShotsMax: 3, ShotsCurrent: int16(len(contents))}, Contents: append([]string(nil), contents...)}
}

func inventoryAndWornBagTakeState(t *testing.T) State {
	t.Helper()
	s := itemMutationFixture(t)
	r := s.Rooms[1]
	r.Items = &ItemCollection{Items: map[string]Item{}}
	s.Rooms[1] = r
	p := s.Players["a"]
	p.Items = &ItemCollection{Items: map[string]Item{
		"inv-bag":    containerBag([]string{"inv-gem-1", "inv-gem-2", "inv-gem-3"}),
		"worn-bag":   containerBag([]string{"worn-gem-1", "worn-gem-2", "worn-gem-3"}),
		"inv-gem-1":  {Object: LegacyObject{Name: "보석"}},
		"inv-gem-2":  {Object: LegacyObject{Name: "보석"}},
		"inv-gem-3":  {Object: LegacyObject{Name: "보석"}},
		"worn-gem-1": {Object: LegacyObject{Name: "보석"}},
		"worn-gem-2": {Object: LegacyObject{Name: "보석"}},
		"worn-gem-3": {Object: LegacyObject{Name: "보석"}},
	}, Inventory: []string{"inv-bag"}, Ready: [20]string{0: "worn-bag"}}
	s.Players["a"] = p
	return s
}

func inventoryAndWornBagDropState(t *testing.T) State {
	t.Helper()
	s := itemMutationFixture(t)
	r := s.Rooms[1]
	r.Items = &ItemCollection{Items: map[string]Item{}}
	s.Rooms[1] = r
	p := s.Players["a"]
	p.Items = &ItemCollection{Items: map[string]Item{
		"inv-bag":  containerBag(nil),
		"worn-bag": containerBag(nil),
		"gem-1":    {Object: LegacyObject{Name: "보석"}},
		"gem-2":    {Object: LegacyObject{Name: "보석"}},
		"gem-3":    {Object: LegacyObject{Name: "보석"}},
	}, Inventory: []string{"inv-bag", "gem-1", "gem-2", "gem-3"}, Ready: [20]string{0: "worn-bag"}}
	s.Players["a"] = p
	return s
}

func roomAndWornBagTakeState(t *testing.T) State {
	t.Helper()
	s := itemMutationFixture(t)
	r := s.Rooms[1]
	r.Items = &ItemCollection{Items: map[string]Item{
		"floor-bag":   containerBag([]string{"floor-gem-1", "floor-gem-2", "floor-gem-3"}),
		"floor-gem-1": {Object: LegacyObject{Name: "보석"}},
		"floor-gem-2": {Object: LegacyObject{Name: "보석"}},
		"floor-gem-3": {Object: LegacyObject{Name: "보석"}},
	}, Inventory: []string{"floor-bag"}}
	s.Rooms[1] = r
	p := s.Players["a"]
	p.Items = &ItemCollection{Items: map[string]Item{
		"worn-bag":   containerBag([]string{"worn-gem-1", "worn-gem-2", "worn-gem-3"}),
		"worn-gem-1": {Object: LegacyObject{Name: "보석"}},
		"worn-gem-2": {Object: LegacyObject{Name: "보석"}},
		"worn-gem-3": {Object: LegacyObject{Name: "보석"}},
	}, Ready: [20]string{0: "worn-bag"}}
	s.Players["a"] = p
	return s
}

func twoWornBagTakeState(t *testing.T) State {
	t.Helper()
	s := itemMutationFixture(t)
	r := s.Rooms[1]
	r.Items = &ItemCollection{Items: map[string]Item{}}
	s.Rooms[1] = r
	p := s.Players["a"]
	p.Items = &ItemCollection{Items: map[string]Item{
		"worn-bag-1":   containerBag([]string{"worn-gem-1-1"}),
		"worn-bag-2":   containerBag([]string{"worn-gem-2-1", "worn-gem-2-2", "worn-gem-2-3"}),
		"worn-gem-1-1": {Object: LegacyObject{Name: "보석"}},
		"worn-gem-2-1": {Object: LegacyObject{Name: "보석"}},
		"worn-gem-2-2": {Object: LegacyObject{Name: "보석"}},
		"worn-gem-2-3": {Object: LegacyObject{Name: "보석"}},
	}, Ready: [20]string{0: "worn-bag-1", 1: "worn-bag-2"}}
	s.Players["a"] = p
	return s
}

func twoWornBagDropState(t *testing.T) State {
	t.Helper()
	s := itemMutationFixture(t)
	r := s.Rooms[1]
	r.Items = &ItemCollection{Items: map[string]Item{}}
	s.Rooms[1] = r
	p := s.Players["a"]
	p.Items = &ItemCollection{Items: map[string]Item{
		"worn-bag-1": containerBag(nil),
		"worn-bag-2": containerBag(nil),
		"gem-1":      {Object: LegacyObject{Name: "보석"}},
		"gem-2":      {Object: LegacyObject{Name: "보석"}},
		"gem-3":      {Object: LegacyObject{Name: "보석"}},
	}, Inventory: []string{"gem-1", "gem-2", "gem-3"}, Ready: [20]string{0: "worn-bag-1", 1: "worn-bag-2"}}
	s.Players["a"] = p
	return s
}

func TestSelectContainerRootCountsInventorySeparatelyFromReady(t *testing.T) {
	c := ItemCollection{Items: map[string]Item{
		"inv-bag":  containerBag(nil),
		"worn-bag": containerBag(nil),
	}, Inventory: []string{"inv-bag"}, Ready: [20]string{0: "worn-bag"}}
	id, err := selectContainerRoot(c, "가방", 1, nil)
	if err != nil || id != "inv-bag" {
		t.Fatalf("inventory occ 1=%s err=%v", id, err)
	}
	if id, err := selectContainerRoot(c, "가방", 2, nil); err == nil {
		t.Fatalf("inventory occ 2 combined Ready id=%s", id)
	}
}

func TestTakeContainedItemDoesNotTreatWornAsInventoryOccurrenceTwo(t *testing.T) {
	s := inventoryAndWornBagTakeState(t)
	if _, result, err := s.TakeContainedItem("a", "가방", "보석", 3, 2); err == nil {
		t.Fatalf("took worn as inventory #2 result=%+v player=%+v", result, s.Players["a"].Items)
	}
}

func TestTakeContainedItemSelectsInventoryBeforeWorn(t *testing.T) {
	s := inventoryAndWornBagTakeState(t)
	next, result, err := s.TakeContainedItem("a", "가방", "보석", 3, 1)
	if err != nil || result.Action != "take-contained" {
		t.Fatalf("take from inventory bag result=%+v err=%v", result, err)
	}
	if !containsID(next.Players["a"].Items.Inventory, "inv-gem-3") || containsID(next.Players["a"].Items.Inventory, "worn-gem-3") {
		t.Fatalf("took worn gem player=%+v", next.Players["a"].Items)
	}
	if containsID(next.Players["a"].Items.Items["inv-bag"].Contents, "inv-gem-3") || !containsID(next.Players["a"].Items.Items["worn-bag"].Contents, "worn-gem-3") {
		t.Fatalf("bags after take=%+v", next.Players["a"].Items)
	}
}

func TestTakeContainedItemSelectsRoomBeforeWorn(t *testing.T) {
	s := roomAndWornBagTakeState(t)
	next, result, err := s.TakeContainedItem("a", "가방", "보석", 3, 1)
	if err != nil || result.Action != "take-contained" {
		t.Fatalf("take from room bag result=%+v err=%v", result, err)
	}
	if !containsID(next.Players["a"].Items.Inventory, "floor-gem-3") || containsID(next.Players["a"].Items.Inventory, "worn-gem-3") {
		t.Fatalf("took worn gem player=%+v", next.Players["a"].Items)
	}
	if containsID(next.Rooms[1].Items.Items["floor-bag"].Contents, "floor-gem-3") || !containsID(next.Players["a"].Items.Items["worn-bag"].Contents, "worn-gem-3") {
		t.Fatalf("room/worn after take room=%+v player=%+v", next.Rooms[1].Items, next.Players["a"].Items)
	}
}

func TestTakeContainedItemSelectsNthWornAfterInventoryAndRoomMiss(t *testing.T) {
	s := twoWornBagTakeState(t)
	next, result, err := s.TakeContainedItem("a", "가방", "보석", 3, 2)
	if err != nil || result.Action != "take-contained" {
		t.Fatalf("take from 2nd worn bag result=%+v err=%v", result, err)
	}
	if containsID(next.Players["a"].Items.Inventory, "worn-gem-1-1") || containsID(next.Players["a"].Items.Inventory, "worn-gem-2-1") || containsID(next.Players["a"].Items.Inventory, "worn-gem-2-2") || !containsID(next.Players["a"].Items.Inventory, "worn-gem-2-3") {
		t.Fatalf("took wrong worn gem player=%+v", next.Players["a"].Items)
	}
	if !containsID(next.Players["a"].Items.Items["worn-bag-1"].Contents, "worn-gem-1-1") || containsID(next.Players["a"].Items.Items["worn-bag-2"].Contents, "worn-gem-2-3") {
		t.Fatalf("worn bags after take=%+v", next.Players["a"].Items)
	}
}

func TestDropContainedItemDoesNotTreatWornAsInventoryOccurrenceTwo(t *testing.T) {
	s := inventoryAndWornBagDropState(t)
	if _, result, err := s.DropContainedItem("a", "보석", "가방", 3, 2); err == nil {
		t.Fatalf("dropped into worn as inventory #2 result=%+v player=%+v", result, s.Players["a"].Items)
	}
}

func TestDropContainedItemSelectsInventoryBeforeWorn(t *testing.T) {
	s := inventoryAndWornBagDropState(t)
	next, result, err := s.DropContainedItem("a", "보석", "가방", 3, 1)
	if err != nil || result.Action != "drop-contained" {
		t.Fatalf("drop into inventory bag result=%+v err=%v", result, err)
	}
	if containsID(next.Players["a"].Items.Inventory, "gem-3") || !containsID(next.Players["a"].Items.Items["inv-bag"].Contents, "gem-3") || containsID(next.Players["a"].Items.Items["worn-bag"].Contents, "gem-3") {
		t.Fatalf("dropped into worn bag player=%+v", next.Players["a"].Items)
	}
}

func TestDropContainedItemSelectsNthWornAfterInventoryAndRoomMiss(t *testing.T) {
	s := twoWornBagDropState(t)
	next, result, err := s.DropContainedItem("a", "보석", "가방", 3, 2)
	if err != nil || result.Action != "drop-contained" {
		t.Fatalf("drop into 2nd worn bag result=%+v err=%v", result, err)
	}
	if !containsID(next.Players["a"].Items.Inventory, "gem-1") || !containsID(next.Players["a"].Items.Inventory, "gem-2") || containsID(next.Players["a"].Items.Inventory, "gem-3") {
		t.Fatalf("dropped wrong gem inventory=%+v", next.Players["a"].Items)
	}
	if containsID(next.Players["a"].Items.Items["worn-bag-1"].Contents, "gem-3") || !containsID(next.Players["a"].Items.Items["worn-bag-2"].Contents, "gem-3") {
		t.Fatalf("dropped into first worn bag player=%+v", next.Players["a"].Items)
	}
}
