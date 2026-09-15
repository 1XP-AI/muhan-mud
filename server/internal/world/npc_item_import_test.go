package world

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
)

func importedNPCFixture(t *testing.T) State {
	t.Helper()
	n := 0
	s, err := npcLegacyImportFixture().ImportNPCs(func() (string, error) {
		n++
		return fmt.Sprintf("npc-%d", n), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestImportNPCItemsPreservesCanonicalNPCIdentityAndOrder(t *testing.T) {
	s := importedNPCFixture(t)
	allocated := 0
	next, err := s.ImportNPCItems(func() (string, error) {
		allocated++
		return fmt.Sprintf("item-%d", allocated), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if allocated != 2 {
		t.Fatalf("expected two nested inventory objects, allocated %d", allocated)
	}
	for id, npc := range next.NPCs {
		if npc.Items == nil || len(npc.Body.Inventory) != 0 {
			t.Fatalf("NPC %s was not canonicalized: %+v", id, npc)
		}
	}
	items := next.NPCs["npc-1"].Items
	if !reflect.DeepEqual(items.Inventory, []string{"item-1"}) || !reflect.DeepEqual(items.Items["item-1"].Contents, []string{"item-2"}) {
		t.Fatalf("unexpected NPC item graph: %+v", items)
	}
	projected, err := next.ProjectRoom(2)
	if err != nil || len(projected.Monsters) != 1 || projected.Monsters[0].Inventory[0].Contents[0].Name != "보석" {
		t.Fatalf("projection lost NPC inventory: %+v err=%v", projected, err)
	}
	if err := next.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestImportNPCItemsReturnsNoPartialStateOnAllocatorFailure(t *testing.T) {
	s := importedNPCFixture(t)
	want := s.clone()
	calls := 0
	next, err := s.ImportNPCItems(func() (string, error) {
		calls++
		if calls == 2 {
			return "", errors.New("allocator stopped")
		}
		return fmt.Sprintf("item-%d", calls), nil
	})
	if err == nil || !reflect.DeepEqual(next, State{}) {
		t.Fatalf("partial NPC item migration: %+v %v", next, err)
	}
	if !reflect.DeepEqual(s, want) {
		t.Fatal("source NPC state changed after allocation failure")
	}
}

func TestNPCDeathTransfersCanonicalNPCItemsWithoutReallocatingThem(t *testing.T) {
	s := npcCanonicalFixture()
	npc := s.NPCs["npc-a"]
	items := itemFixture(t)
	npc.Body.Inventory = nil
	npc.Items = &items
	npc.Body.HPCurrent = 0
	npc.Body.Experience = 9
	npc.Enemies = []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "player"}, Damage: 1}}
	s.NPCs["npc-a"] = npc
	room := s.Rooms[1]
	room.Items = &ItemCollection{Items: map[string]Item{}, Inventory: []string{}}
	s.Rooms[1] = room
	next, _, err := s.PlanNPCDeath("npc-a", "player", 100, func() (string, error) {
		t.Fatal("existing canonical NPC items must not be reallocated")
		return "", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := next.NPCs["npc-a"]; ok {
		t.Fatalf("NPC identity was not removed")
	}
	if !reflect.DeepEqual(next.Rooms[1].Items.Items, items.Items) ||
		!reflect.DeepEqual(map[string]bool{next.Rooms[1].Items.Inventory[0]: true, next.Rooms[1].Items.Inventory[1]: true}, map[string]bool{items.Inventory[0]: true, items.Inventory[1]: true}) {
		t.Fatalf("NPC item graph was not transferred: got=%+v want=%+v", next.Rooms[1].Items, items)
	}
}
