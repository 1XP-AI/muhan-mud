package world

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
)

func TestImportRoomItemsUsesDeterministicRoomAndObjectOrder(t *testing.T) {
	s := stateFixture()
	late := s.Rooms[2]
	late.Resource.Objects = []LegacyObject{{Name: "late", Contents: []LegacyObject{{Name: "gem"}}}}
	s.Rooms[2] = late
	early := s.Rooms[1]
	early.Resource.Objects = []LegacyObject{{Name: "early"}}
	s.Rooms[1] = early

	n := 0
	got, err := s.ImportRoomItems(func() (string, error) {
		n++
		return fmt.Sprintf("floor-%d", n), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Rooms[1].Items == nil || got.Rooms[2].Items == nil {
		t.Fatal("empty canonical floor was not materialized")
	}
	if !reflect.DeepEqual(got.Rooms[1].Items.Inventory, []string{"floor-1"}) ||
		!reflect.DeepEqual(got.Rooms[2].Items.Inventory, []string{"floor-2"}) {
		t.Fatalf("unexpected root order: room1=%v room2=%v", got.Rooms[1].Items.Inventory, got.Rooms[2].Items.Inventory)
	}
	if !reflect.DeepEqual(got.Rooms[2].Items.Items["floor-2"].Contents, []string{"floor-3"}) {
		t.Fatalf("unexpected child graph: %+v", got.Rooms[2].Items)
	}
	if got.Rooms[1].Items.Items["floor-1"].Object.Name != "early" || got.Rooms[2].Items.Items["floor-2"].Object.Name != "late" {
		t.Fatal("room/object order was not preserved")
	}
	for _, room := range got.Rooms {
		if len(room.Resource.Objects) != 0 {
			t.Fatal("legacy floor objects were retained")
		}
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(s.Rooms[1].Resource.Objects) != 1 || s.Rooms[1].Items != nil {
		t.Fatal("source snapshot was mutated")
	}
}

func TestImportRoomItemsReturnsNoPartialStateOnAllocatorFailure(t *testing.T) {
	s := stateFixture()
	room := s.Rooms[1]
	room.Resource.Objects = []LegacyObject{{Name: "first"}, {Name: "second"}}
	s.Rooms[1] = room
	before := s.clone()
	n := 0
	got, err := s.ImportRoomItems(func() (string, error) {
		n++
		if n == 2 {
			return "", errors.New("allocator unavailable")
		}
		return fmt.Sprintf("floor-%d", n), nil
	})
	if err == nil || !reflect.DeepEqual(got, State{}) {
		t.Fatalf("expected failed migration without a partial state: got=%+v err=%v", got, err)
	}
	if !reflect.DeepEqual(s, before) {
		t.Fatal("source snapshot changed after failed migration")
	}
}

func TestImportRoomItemsRejectsRepeatAndDuplicateGlobalIDs(t *testing.T) {
	s := stateFixture()
	room := s.Rooms[1]
	room.Resource.Objects = []LegacyObject{{Name: "legacy"}}
	s.Rooms[1] = room
	canonical := ItemCollection{Items: map[string]Item{"existing": {Object: LegacyObject{Name: "existing"}}}, Inventory: []string{"existing"}}
	p := s.Players["a"]
	p.Items = &canonical
	s.Players["a"] = p
	if _, err := s.ImportRoomItems(func() (string, error) { return "existing", nil }); err == nil {
		t.Fatal("accepted item ID already owned by a player")
	}

	room = s.Rooms[1]
	room.Items = &canonical
	room.Resource.Objects = nil
	s.Rooms[1] = room
	if _, err := s.ImportRoomItems(func() (string, error) { return "new", nil }); err == nil {
		t.Fatal("accepted repeat room item migration")
	}
}
