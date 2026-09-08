package world

import (
	"errors"
	"reflect"
	"testing"
)

type roomRefreshCatalog struct {
	object       LegacyObject
	monster      LegacyMonster
	objectCalls  int
	monsterCalls int
}

func (c *roomRefreshCatalog) Monster(int16) (LegacyMonster, error) {
	c.monsterCalls++
	if c.monster.Type == 0 {
		return LegacyMonster{}, errors.New("unexpected monster refresh")
	}
	return c.monster, nil
}

func (c *roomRefreshCatalog) Object(int16) (LegacyObject, error) {
	c.objectCalls++
	return c.object, nil
}

func roomRefreshState() State {
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{
					ID:    1,
					Exits: []LegacyExit{{Name: "동", Flags: [4]byte{16}}},
				}},
				Items: &ItemCollection{Items: map[string]Item{}},
			},
		},
		Players: map[string]PlayerState{},
	}
}

func TestPlanApplyRoomResourceRefreshRefreshesFloorObjectAndDoorsAtomically(t *testing.T) {
	s := roomRefreshState()
	r := s.Rooms[1]
	r.Resource.PermanentObjects[0] = LegacyTimer{LastTime: 0, Interval: 1, Misc: 8}
	s.Rooms[1] = r
	before := s.clone()
	catalog := &roomRefreshCatalog{object: LegacyObject{Name: "검", Flags: [8]byte{0, 0, 32}}}
	rollCalls, allocateCalls := 0, 0
	proposal, err := s.PlanRoomResourceRefresh(1, catalog, 10, func(lo, hi int) int {
		rollCalls++
		if lo != 1 || hi != 100 {
			t.Fatalf("unexpected roll range %d..%d", lo, hi)
		}
		return 99
	}, func() (string, error) {
		allocateCalls++
		return "floor-1", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s, before) {
		t.Fatal("planning mutated source state")
	}
	if catalog.monsterCalls != 0 || rollCalls != 1 || allocateCalls != 1 {
		t.Fatalf("catalog/inputs calls monster=%d rolls=%d alloc=%d", catalog.monsterCalls, rollCalls, allocateCalls)
	}

	next, err := s.ApplyRoomResourceRefresh(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.Rooms[1].Items, before.Rooms[1].Items) {
		t.Fatal("apply changed source state")
	}
	room := next.Rooms[1]
	if room.Resource.Objects != nil || len(room.Items.Items) != 1 || room.Items.Inventory[0] != "floor-1" {
		t.Fatalf("canonical floor=%+v", room)
	}
	item := room.Items.Items["floor-1"]
	if item.Object.Name != "검" || item.Object.Adjustment != 3 {
		t.Fatalf("spawned item=%+v", item.Object)
	}
	if !flag(room.Resource.Exits[0].Flags[:], 2) || !flag(room.Resource.Exits[0].Flags[:], 3) {
		t.Fatalf("door was not refreshed: %+v", room.Resource.Exits[0])
	}
	if err := next.Validate(); err != nil {
		t.Fatal(err)
	}

	stale, err := next.ApplyRoomResourceRefresh(proposal)
	if err == nil || !reflect.DeepEqual(stale, State{}) {
		t.Fatalf("stale refresh accepted: state=%+v err=%v", stale, err)
	}
}

func TestPlanRoomResourceRefreshRejectsDueCanonicalNPCWithoutPartialFloorRefresh(t *testing.T) {
	s := roomRefreshState()
	r := s.Rooms[1]
	r.Resource.PermanentMonsters[0] = LegacyTimer{LastTime: 0, Interval: 1, Misc: 7}
	r.Resource.PermanentObjects[0] = LegacyTimer{LastTime: 0, Interval: 1, Misc: 8}
	r.NPCIDs = []string{"wolf"}
	s.Rooms[1] = r
	s.NPCs = map[string]NPCState{
		"wolf": {
			Body:    LegacyMonster{Name: "늑대", Type: 1, RoomID: 1, HPMax: 10, HPCurrent: 10},
			Enemies: []NPCEnemy{},
		},
	}
	s.ActiveNPCIDs = []string{"wolf"}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	before := s.clone()
	catalog := &roomRefreshCatalog{object: LegacyObject{Name: "검"}, monster: LegacyMonster{Name: "늑대", Type: 1}}
	if _, err := s.PlanRoomResourceRefresh(1, catalog, 10, func(int, int) int {
		t.Fatal("RNG consumed before unsupported NPC refresh was rejected")
		return 0
	}, func() (string, error) {
		t.Fatal("allocator consumed before unsupported NPC refresh was rejected")
		return "", nil
	}); !errors.Is(err, ErrRoomResourceRefreshNPC) {
		t.Fatalf("err=%v, want ErrRoomResourceRefreshNPC", err)
	}
	if catalog.monsterCalls != 0 || catalog.objectCalls != 0 {
		t.Fatalf("catalog called before rejection: monster=%d object=%d", catalog.monsterCalls, catalog.objectCalls)
	}
	if !reflect.DeepEqual(s, before) {
		t.Fatal("failed NPC refresh mutated source state")
	}
}

func TestPlanApplyRoomResourceRefreshPreservesCanonicalNPCOrderWhenNoNPCSpawnDue(t *testing.T) {
	s := roomRefreshState()
	r := s.Rooms[1]
	r.Resource.PermanentMonsters[0] = LegacyTimer{LastTime: 100, Interval: 100, Misc: 7}
	r.NPCIDs = []string{"wolf", "guard"}
	s.Rooms[1] = r
	s.NPCs = map[string]NPCState{
		"wolf":  {Body: LegacyMonster{Name: "늑대", Type: 1, RoomID: 1}, Enemies: []NPCEnemy{}},
		"guard": {Body: LegacyMonster{Name: "경비", Type: 1, RoomID: 1}, Enemies: []NPCEnemy{}},
	}
	s.ActiveNPCIDs = []string{"guard", "wolf"}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}

	proposal, err := s.PlanRoomResourceRefresh(1, &roomRefreshCatalog{}, 10, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	next, err := s.ApplyRoomResourceRefresh(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(next.Rooms[1].NPCIDs, s.Rooms[1].NPCIDs) || !reflect.DeepEqual(next.ActiveNPCIDs, s.ActiveNPCIDs) || len(next.Rooms[1].Resource.Monsters) != 0 {
		t.Fatalf("NPC authority changed: room=%+v active=%v", next.Rooms[1], next.ActiveNPCIDs)
	}
}
