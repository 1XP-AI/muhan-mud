package world

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
)

func npcLegacyMonster(name string, hp int16, inventory ...LegacyObject) LegacyMonster {
	return LegacyMonster{
		Name:      name,
		Type:      1,
		RoomID:    -99,
		HPMax:     hp,
		HPCurrent: hp,
		Inventory: append([]LegacyObject(nil), inventory...),
	}
}

// The map order is intentionally not the room order. ImportNPCs must sort
// room IDs, but must retain the legacy slice order within each room. Several
// bodies share a name so name-based matching would lose instance identity.
func npcLegacyImportFixture() State {
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			20: {Resource: LegacyRoom{
				LegacyRoomHeader: LegacyRoomHeader{ID: 20, Name: "스무번째"},
				Monsters: []LegacyMonster{
					npcLegacyMonster("Twin", 200),
					npcLegacyMonster("Twin", 201),
				},
			}},
			2: {Resource: LegacyRoom{
				LegacyRoomHeader: LegacyRoomHeader{ID: 2, Name: "두번째"},
				Monsters: []LegacyMonster{
					npcLegacyMonster("Twin", 20, LegacyObject{
						Name:     "가방",
						Contents: []LegacyObject{{Name: "보석", Weight: 3}},
					}),
				},
			}},
			11: {Resource: LegacyRoom{
				LegacyRoomHeader: LegacyRoomHeader{ID: 11, Name: "열한번째"},
				Monsters:         []LegacyMonster{npcLegacyMonster("Guard", 110)},
			}},
		},
		Players: map[string]PlayerState{},
	}
}

func npcCanonicalFixture() State {
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Name: "전투방"}},
				PlayerIDs: []string{"player"},
				NPCIDs:    []string{"npc-a", "npc-b"},
			},
			2: {Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 2, Name: "빈방"}}},
		},
		Players: map[string]PlayerState{
			"player": {Body: LegacyMonster{Name: "Hero", Type: 0, RoomID: 1}, Online: true},
		},
		NPCs: map[string]NPCState{
			"npc-a": {Body: LegacyMonster{
				Name: "Wolf", Type: 1, RoomID: 1,
				Inventory: []LegacyObject{{Name: "satchel", Contents: []LegacyObject{{Name: "fang", Weight: 4}}}},
			}},
			"npc-b": {Body: LegacyMonster{Name: "Ogre", Type: 1, RoomID: 1}},
		},
	}
}

func TestImportNPCsPreservesSortedRoomAndInstanceOrder(t *testing.T) {
	s := npcLegacyImportFixture()
	allocated := []string{}
	next, err := s.ImportNPCs(func() (string, error) {
		id := fmt.Sprintf("npc-%d", len(allocated)+1)
		allocated = append(allocated, id)
		return id, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(allocated, []string{"npc-1", "npc-2", "npc-3", "npc-4"}) {
		t.Fatalf("allocation order %v", allocated)
	}
	if s.NPCs != nil || len(s.Rooms[2].Resource.Monsters) != 1 {
		t.Fatal("import mutated source snapshot")
	}
	if len(next.NPCs) != 4 {
		t.Fatalf("NPC count %d", len(next.NPCs))
	}
	if !reflect.DeepEqual(next.Rooms[2].NPCIDs, []string{"npc-1"}) ||
		!reflect.DeepEqual(next.Rooms[11].NPCIDs, []string{"npc-2"}) ||
		!reflect.DeepEqual(next.Rooms[20].NPCIDs, []string{"npc-3", "npc-4"}) {
		t.Fatalf("room NPC order: %+v", next.Rooms)
	}
	if next.Rooms[2].Resource.Monsters != nil || next.Rooms[11].Resource.Monsters != nil || next.Rooms[20].Resource.Monsters != nil {
		t.Fatal("legacy monsters not cleared")
	}
	if next.NPCs["npc-3"].Body.Name != "Twin" || next.NPCs["npc-4"].Body.Name != "Twin" || next.NPCs["npc-3"].Body.HPMax != 200 || next.NPCs["npc-4"].Body.HPMax != 201 {
		t.Fatal("identical-name instances lost their order or identity")
	}
	for id, npc := range next.NPCs {
		if npc.Body.RoomID == -99 || npc.Body.Type != 1 || npc.Enemies != nil {
			t.Fatalf("imported NPC %s has invalid body/unknown relations: %+v", id, npc)
		}
	}
	if err := next.Validate(); err != nil {
		t.Fatal(err)
	}

	before := npcLegacyImportFixture()
	for _, roomID := range []int16{2, 11, 20} {
		projected, err := next.ProjectRoom(roomID)
		if err != nil {
			t.Fatalf("project room %d: %v", roomID, err)
		}
		want := append([]LegacyMonster(nil), before.Rooms[roomID].Resource.Monsters...)
		for i := range want {
			want[i].RoomID = roomID
		}
		if !reflect.DeepEqual(projected.Monsters, want) {
			t.Fatalf("room %d projection %#v want %#v", roomID, projected.Monsters, want)
		}
	}
	projected, err := next.ProjectRoom(2)
	if err != nil {
		t.Fatal(err)
	}
	projected.Monsters[0].Inventory[0].Contents[0].Name = "변경됨"
	if next.NPCs["npc-1"].Body.Inventory[0].Contents[0].Name != "보석" {
		t.Fatal("room projection aliased canonical NPC inventory")
	}
	if _, err := next.ProjectRoom(999); err == nil {
		t.Fatal("missing room projected successfully")
	}
}

func TestImportNPCsAllocationFailureReturnsNoPartialSnapshot(t *testing.T) {
	s := npcLegacyImportFixture()
	wantSource := s.clone()
	calls := 0
	next, err := s.ImportNPCs(func() (string, error) {
		calls++
		if calls == 2 {
			return "", errors.New("allocator stopped")
		}
		return fmt.Sprintf("npc-%d", calls), nil
	})
	if err == nil || !reflect.DeepEqual(next, State{}) {
		t.Fatalf("partial result on allocation failure: %+v %v", next, err)
	}
	if !reflect.DeepEqual(s, wantSource) {
		t.Fatal("allocation failure mutated source snapshot")
	}
}

func TestNPCCloneDeepCopiesNestedInventoryAndEnemyRelations(t *testing.T) {
	s := npcCanonicalFixture()
	a := s.NPCs["npc-a"]
	a.Enemies = []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "player"}, Damage: -7}}
	s.NPCs["npc-a"] = a
	b := s.NPCs["npc-b"]
	b.Enemies = []NPCEnemy{}
	s.NPCs["npc-b"] = b
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}

	next := s.clone()
	if next.NPCs["npc-a"].Enemies == nil || next.NPCs["npc-b"].Enemies == nil {
		t.Fatal("clone changed nil/empty enemy distinction")
	}
	npcA := next.NPCs["npc-a"]
	npcA.Body.Inventory[0].Contents[0].Name = "changed gem"
	npcA.Enemies[0].Target.ID = "changed-player"
	npcA.Enemies[0].Damage = 42
	next.NPCs["npc-a"] = npcA
	npcB := next.NPCs["npc-b"]
	npcB.Enemies = append(npcB.Enemies, NPCEnemy{Target: EntityRef{Kind: "player", ID: "player"}, Damage: 1})
	next.NPCs["npc-b"] = npcB
	next.Rooms[1].NPCIDs[0] = "npc-b"

	if s.NPCs["npc-a"].Body.Inventory[0].Contents[0].Name != "fang" ||
		s.NPCs["npc-a"].Enemies[0] != (NPCEnemy{Target: EntityRef{Kind: "player", ID: "player"}, Damage: -7}) ||
		len(s.NPCs["npc-b"].Enemies) != 0 || s.Rooms[1].NPCIDs[0] != "npc-a" {
		t.Fatal("clone mutation changed source NPC graph")
	}
}

func TestNPCValidateAcceptsTypedEnemyReferences(t *testing.T) {
	s := npcCanonicalFixture()
	a := s.NPCs["npc-a"]
	a.Enemies = []NPCEnemy{
		{Target: EntityRef{Kind: "player", ID: "player"}, Damage: -1},
		{Target: EntityRef{Kind: "npc", ID: "npc-b"}, Damage: 4},
	}
	s.NPCs["npc-a"] = a
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestNPCValidateRejectsInvalidOrDuplicateGraphs(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*State)
	}{
		{"wrong NPC type", func(s *State) {
			npc := s.NPCs["npc-a"]
			npc.Body.Type = 0
			s.NPCs["npc-a"] = npc
		}},
		{"missing room membership", func(s *State) {
			r := s.Rooms[1]
			r.NPCIDs = []string{"npc-a"}
			s.Rooms[1] = r
		}},
		{"duplicate room membership", func(s *State) {
			r := s.Rooms[1]
			r.NPCIDs = append(r.NPCIDs, "npc-a")
			s.Rooms[1] = r
		}},
		{"NPC listed in two rooms", func(s *State) {
			r := s.Rooms[2]
			r.NPCIDs = []string{"npc-a"}
			s.Rooms[2] = r
		}},
		{"body room mismatch", func(s *State) {
			npc := s.NPCs["npc-a"]
			npc.Body.RoomID = 2
			s.NPCs["npc-a"] = npc
		}},
		{"legacy and canonical monsters", func(s *State) {
			r := s.Rooms[1]
			r.Resource.Monsters = []LegacyMonster{{Name: "duplicate", Type: 1}}
			s.Rooms[1] = r
		}},
		{"empty NPC identity", func(s *State) {
			r := s.Rooms[1]
			r.NPCIDs = append(r.NPCIDs, "")
			s.Rooms[1] = r
		}},
		{"dangling player target", func(s *State) {
			npc := s.NPCs["npc-a"]
			npc.Enemies = []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "missing"}}}
			s.NPCs["npc-a"] = npc
		}},
		{"dangling NPC target", func(s *State) {
			npc := s.NPCs["npc-a"]
			npc.Enemies = []NPCEnemy{{Target: EntityRef{Kind: "npc", ID: "missing"}}}
			s.NPCs["npc-a"] = npc
		}},
		{"unknown target kind", func(s *State) {
			npc := s.NPCs["npc-a"]
			npc.Enemies = []NPCEnemy{{Target: EntityRef{Kind: "monster", ID: "npc-b"}}}
			s.NPCs["npc-a"] = npc
		}},
		{"duplicate enemy target", func(s *State) {
			npc := s.NPCs["npc-a"]
			target := EntityRef{Kind: "player", ID: "player"}
			npc.Enemies = []NPCEnemy{{Target: target}, {Target: target}}
			s.NPCs["npc-a"] = npc
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := npcCanonicalFixture()
			test.mutate(&s)
			if err := s.Validate(); err == nil {
				t.Fatal("invalid NPC graph accepted")
			}
		})
	}
}

func TestNPCJSONPreservesNilAndEmptyEnemyRelations(t *testing.T) {
	s := npcCanonicalFixture()
	a := s.NPCs["npc-a"]
	a.Enemies = nil
	s.NPCs["npc-a"] = a
	b := s.NPCs["npc-b"]
	b.Enemies = []NPCEnemy{}
	s.NPCs["npc-b"] = b
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeState(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, s) || decoded.NPCs["npc-a"].Enemies != nil || decoded.NPCs["npc-b"].Enemies == nil {
		t.Fatalf("nil/empty enemy state was not preserved: %#v", decoded.NPCs)
	}

	unimported := npcLegacyImportFixture()
	raw, err = json.Marshal(unimported)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err = DecodeState(raw)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.NPCs != nil || !reflect.DeepEqual(decoded, unimported) {
		t.Fatalf("nil unimported NPC map was not preserved: %#v", decoded.NPCs)
	}
}

func TestNPCMovementRelationsSeparateFightingFromMembership(t *testing.T) {
	s := npcCanonicalFixture()
	a := s.NPCs["npc-a"]
	a.Enemies = []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "player"}, Damage: -3}}
	s.NPCs["npc-a"] = a
	b := s.NPCs["npc-b"]
	b.Enemies = []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "player"}, Damage: 0}, {Target: EntityRef{Kind: "npc", ID: "npc-a"}, Damage: 8}}
	s.NPCs["npc-b"] = b

	fighting, aligned, err := s.NPCMovementRelations("player")
	if err != nil || !fighting || !reflect.DeepEqual(aligned, []bool{true, true}) {
		t.Fatalf("relations with nonnegative damage: fighting=%v aligned=%v err=%v", fighting, aligned, err)
	}

	b = s.NPCs["npc-b"]
	b.Enemies = []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "player"}, Damage: -9}}
	s.NPCs["npc-b"] = b
	fighting, aligned, err = s.NPCMovementRelations("player")
	if err != nil || fighting || !reflect.DeepEqual(aligned, []bool{true, true}) {
		t.Fatalf("negative damage changed membership: fighting=%v aligned=%v err=%v", fighting, aligned, err)
	}

	b = s.NPCs["npc-b"]
	b.Enemies = []NPCEnemy{}
	s.NPCs["npc-b"] = b
	fighting, aligned, err = s.NPCMovementRelations("player")
	if err != nil || fighting || !reflect.DeepEqual(aligned, []bool{true, false}) {
		t.Fatalf("empty known relation: fighting=%v aligned=%v err=%v", fighting, aligned, err)
	}
}

func TestNPCMovementRelationsRejectsUnresolvedEnemyState(t *testing.T) {
	s := npcCanonicalFixture()
	b := s.NPCs["npc-b"]
	b.Enemies = []NPCEnemy{}
	s.NPCs["npc-b"] = b
	if _, _, err := s.NPCMovementRelations("player"); err == nil {
		t.Fatal("unresolved nil enemy relation accepted")
	}
}
