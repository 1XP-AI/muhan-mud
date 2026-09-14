package world

import (
	"errors"
	"reflect"
	"testing"
)

type dmFamilyCatalog struct {
	object  LegacyObject
	monster LegacyMonster
}

func (c dmFamilyCatalog) Object(id int16) (LegacyObject, error) {
	if id != dmMoonstoneObjectID {
		return LegacyObject{}, errors.New("unknown object")
	}
	return c.object, nil
}

func (c dmFamilyCatalog) Monster(id int16) (LegacyMonster, error) {
	if id < dmInvasionMonsterLo || id > dmInvasionMonsterHi {
		return LegacyMonster{}, errors.New("unknown monster")
	}
	return c.monster, nil
}

func dmFamilyScriptedRoll(values ...int) func(int, int) int {
	i := 0
	return func(lo, hi int) int {
		if i >= len(values) {
			return lo
		}
		n := values[i]
		i++
		if n < lo {
			return lo
		}
		if n > hi {
			return hi
		}
		return n
	}
}

func dmFamilyFixture(t *testing.T) (State, dmFamilyCatalog) {
	t.Helper()
	dm := LegacyMonster{Name: "운영자", Type: 0, Class: playerDMClass, RoomID: 1}
	s := State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"dm"},
				Items:     &ItemCollection{Items: map[string]Item{}},
			},
			3601: {
				Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 3601, Name: "무적존"}},
				Items:    &ItemCollection{Items: map[string]Item{}},
			},
		},
		Players:      map[string]PlayerState{"dm": {Body: dm, Online: true}},
		NPCs:         map[string]NPCState{},
		ActiveNPCIDs: []string{},
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	return s, dmFamilyCatalog{
		object:  LegacyObject{Name: "초인의 돌", Weight: 1, ShotsMax: 10, ShotsCurrent: 10},
		monster: LegacyMonster{Name: "침략자", Type: 1},
	}
}

func TestDMMoonstoneRejectsNonDMAndDropsStone(t *testing.T) {
	s, catalog := dmFamilyFixture(t)
	mortal := s.Players["dm"]
	mortal.Body.Class = 4
	s.Players["dm"] = mortal
	gated, err := s.PlanDMMoonstone("dm", catalog, dmFamilyScriptedRoll(1, 5), func() (string, error) { return "item-1", nil })
	if err != nil || gated.Changed || gated.Response != DMMoonstoneUnknownResponse {
		t.Fatalf("gated=%+v err=%v", gated, err)
	}

	s, catalog = dmFamilyFixture(t)
	seq := 0
	proposal, err := s.PlanDMMoonstone("dm", catalog, dmFamilyScriptedRoll(1, 7), func() (string, error) {
		seq++
		return "stone-1", nil
	})
	if err != nil || !proposal.Changed || proposal.RoomID != 1 || proposal.ItemID != "stone-1" {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	if proposal.Response != DMMoonstoneBroadcast("광장") {
		t.Fatalf("response=%q", proposal.Response)
	}
	next, result, err := s.ApplyDMFamily(proposal)
	if err != nil {
		t.Fatal(err)
	}
	item := next.Rooms[1].Items.Items["stone-1"]
	if item.Object.ShotsMax != 17 || item.Object.ShotsCurrent != 17 || result.ItemID != "stone-1" {
		t.Fatalf("item=%+v result=%+v", item, result)
	}
	if _, _, err := next.ApplyDMFamily(proposal); !errors.Is(err, ErrDMFamilyStaleProposal) {
		t.Fatalf("replay err=%v", err)
	}
}

func TestDMInvasionSpawnsTenAndReplays(t *testing.T) {
	s, catalog := dmFamilyFixture(t)
	n := 0
	proposal, err := s.PlanDMInvasion("dm", catalog, dmFamilyScriptedRoll(), func() (string, error) {
		n++
		return "inv-" + string(rune('a'+n-1)), nil
	})
	if err != nil || !proposal.Changed || len(proposal.NPCIDs) != 10 {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	next, result, err := s.ApplyDMFamily(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.NPCs) != 10 || len(next.Rooms[3601].NPCIDs) != 10 || len(result.NPCIDs) != 10 {
		t.Fatalf("npcs=%d room=%d result=%d", len(next.NPCs), len(next.Rooms[3601].NPCIDs), len(result.NPCIDs))
	}
	if _, _, err := next.ApplyDMFamily(proposal); !errors.Is(err, ErrDMFamilyStaleProposal) {
		t.Fatalf("replay err=%v", err)
	}
}

func TestDMFamilyFailClosedWithoutCatalogOrNPCs(t *testing.T) {
	s, catalog := dmFamilyFixture(t)
	if _, err := s.PlanDMMoonstone("dm", nil, dmFamilyScriptedRoll(1, 1), func() (string, error) { return "x", nil }); !errors.Is(err, ErrDMFamilyCatalog) {
		t.Fatalf("catalog err=%v", err)
	}
	s.NPCs = nil
	s.ActiveNPCIDs = nil
	if _, err := s.PlanDMInvasion("dm", catalog, dmFamilyScriptedRoll(), func() (string, error) { return "x", nil }); !errors.Is(err, ErrDMFamilyNPCUnresolved) {
		t.Fatalf("npc err=%v", err)
	}
}

func TestDMInvasionFailClosedOnNilActiveNPCIDs(t *testing.T) {
	s, catalog := dmFamilyFixture(t)
	s.ActiveNPCIDs = nil
	if _, err := s.PlanDMInvasion("dm", catalog, dmFamilyScriptedRoll(), func() (string, error) { return "x", nil }); !errors.Is(err, ErrDMFamilyNPCUnresolved) {
		t.Fatalf("nil ActiveNPCIDs err=%v", err)
	}
}

func dmInvasionAllocate(t *testing.T) func() (string, error) {
	t.Helper()
	n := 0
	return func() (string, error) {
		n++
		return "inv-" + string(rune('a'+n-1)), nil
	}
}

func TestDMInvasionSpawnsCombatReadyNPCs(t *testing.T) {
	s, catalog := dmFamilyFixture(t)
	s.ActiveNPCIDs = []string{}
	catalog.monster = LegacyMonster{
		Name: "침략자", Type: 1, Class: 4, Level: 1, Armor: 100,
		HPMax: 50, HPCurrent: 50, DiceCount: 1, DiceSides: 4,
	}
	attacker := LegacyMonster{
		Name: "Alice", Type: 0, Class: 4, Level: 1, RoomID: 3601,
		Stats: [5]byte{10, 10, 10, 10, 10}, HPMax: 100, HPCurrent: 40,
		DiceCount: 1, DiceSides: 5,
	}
	s.Players["a"] = PlayerState{Body: attacker, Online: true, Items: &ItemCollection{Items: map[string]Item{}}}
	room := s.Rooms[3601]
	room.PlayerIDs = []string{"a"}
	s.Rooms[3601] = room
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}

	proposal, err := s.PlanDMInvasion("dm", catalog, dmFamilyScriptedRoll(), dmInvasionAllocate(t))
	if err != nil || !proposal.Changed || len(proposal.NPCIDs) != 10 {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	next, _, err := s.ApplyDMFamily(proposal)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range proposal.NPCIDs {
		npc, ok := next.NPCs[id]
		if !ok || npc.Enemies == nil {
			t.Fatalf("invasion NPC %q enemies=%v ok=%t", id, npc.Enemies, ok)
		}
	}
	if _, _, err := next.PlanNPCMeleeAttack("a", proposal.NPCIDs[0], func(_, hi int) int { return hi }); err != nil {
		t.Fatalf("melee against invasion NPC: %v", err)
	}
}

func TestDMInvasionDoesNotInventActiveNPCIDsForEmptyRooms(t *testing.T) {
	s, catalog := dmFamilyFixture(t)
	s.NPCs["old"] = NPCState{Body: LegacyMonster{Name: "기존", Type: 1, RoomID: 1, HPCurrent: 1}, Enemies: []NPCEnemy{}}
	s.ActiveNPCIDs = []string{"old"}
	room := s.Rooms[1]
	room.NPCIDs = []string{"old"}
	s.Rooms[1] = room
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}

	proposal, err := s.PlanDMInvasion("dm", catalog, dmFamilyScriptedRoll(), dmInvasionAllocate(t))
	if err != nil {
		t.Fatal(err)
	}
	next, _, err := s.ApplyDMFamily(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(next.ActiveNPCIDs, []string{"old"}) {
		t.Fatalf("empty invasion room invented active order: %q", next.ActiveNPCIDs)
	}
	if len(next.Rooms[3601].NPCIDs) != 10 {
		t.Fatalf("room npcs=%q", next.Rooms[3601].NPCIDs)
	}
}

func TestDMInvasionPrependsActiveOnlyWhenRoomOccupied(t *testing.T) {
	s, catalog := dmFamilyFixture(t)
	s.NPCs["old"] = NPCState{Body: LegacyMonster{Name: "기존", Type: 1, RoomID: 1, HPCurrent: 1}, Enemies: []NPCEnemy{}}
	s.ActiveNPCIDs = []string{"old"}
	home := s.Rooms[1]
	home.NPCIDs = []string{"old"}
	s.Rooms[1] = home
	s.Players["scout"] = PlayerState{Body: LegacyMonster{Name: "Scout", Type: 0, Class: 4, RoomID: 3601}, Online: true}
	invaded := s.Rooms[3601]
	invaded.PlayerIDs = []string{"scout"}
	s.Rooms[3601] = invaded
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}

	proposal, err := s.PlanDMInvasion("dm", catalog, dmFamilyScriptedRoll(), dmInvasionAllocate(t))
	if err != nil {
		t.Fatal(err)
	}
	next, _, err := s.ApplyDMFamily(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.ActiveNPCIDs) != 11 || next.ActiveNPCIDs[len(next.ActiveNPCIDs)-1] != "old" {
		t.Fatalf("occupied active=%q", next.ActiveNPCIDs)
	}
	for i, id := range proposal.NPCIDs {
		wantAt := len(proposal.NPCIDs) - 1 - i
		if next.ActiveNPCIDs[wantAt] != id {
			t.Fatalf("active=%q spawned=%q", next.ActiveNPCIDs, proposal.NPCIDs)
		}
	}
}

func TestDMInvasionInsertsRoomNPCsByName(t *testing.T) {
	s, catalog := dmFamilyFixture(t)
	s.ActiveNPCIDs = []string{}
	s.NPCs["rear"] = NPCState{Body: LegacyMonster{Name: "후위", Type: 1, RoomID: 3601}, Enemies: []NPCEnemy{}}
	room := s.Rooms[3601]
	room.NPCIDs = []string{"rear"}
	s.Rooms[3601] = room
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}

	proposal, err := s.PlanDMInvasion("dm", catalog, dmFamilyScriptedRoll(), dmInvasionAllocate(t))
	if err != nil {
		t.Fatal(err)
	}
	next, _, err := s.ApplyDMFamily(proposal)
	if err != nil {
		t.Fatal(err)
	}
	got := next.Rooms[3601].NPCIDs
	if len(got) != 11 || got[len(got)-1] != "rear" {
		t.Fatalf("name order=%q", got)
	}
}
