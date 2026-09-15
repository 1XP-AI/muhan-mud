package world

import (
	"fmt"
	"os"
	"testing"
)

func TestLegacyRoomCatalogImportsCanonicalResourceGraphs(t *testing.T) {
	catalog, err := LoadLegacyRoomCatalog(os.DirFS("../../../rooms"), LegacyRoomCompatibilityPolicy)
	if err != nil {
		t.Fatal(err)
	}
	s, err := catalog.NewState()
	if err != nil {
		t.Fatal(err)
	}
	npcNext := 0
	s, err = s.ImportNPCs(func() (string, error) {
		npcNext++
		return fmt.Sprintf("npc-%d", npcNext), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	itemNext := 0
	allocateItem := func() (string, error) {
		itemNext++
		return fmt.Sprintf("item-%d", itemNext), nil
	}
	s, err = s.ImportRoomItems(allocateItem)
	if err != nil {
		t.Fatal(err)
	}
	s, err = s.ImportNPCItems(allocateItem)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(s.NPCs) == 0 || npcNext != len(s.NPCs) || itemNext == 0 {
		t.Fatalf("canonical resource graph is unexpectedly empty: NPCs=%d allocatedNPC=%d items=%d", len(s.NPCs), npcNext, itemNext)
	}
	for id, room := range s.Rooms {
		if room.Items == nil || len(room.Resource.Objects) != 0 || len(room.Resource.Monsters) != 0 {
			t.Fatalf("room %d retained legacy ownership: %+v", id, room)
		}
	}
}
