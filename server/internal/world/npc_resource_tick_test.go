package world

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

type npcResourceTickCatalog struct {
	monsters map[int16]LegacyMonster
	objects  map[int16]LegacyObject
	mu       sync.Mutex
	monCalls []int16
}

func (c *npcResourceTickCatalog) Monster(id int16) (LegacyMonster, error) {
	c.mu.Lock()
	c.monCalls = append(c.monCalls, id)
	c.mu.Unlock()
	m, ok := c.monsters[id]
	if !ok {
		return LegacyMonster{}, errors.New("missing monster")
	}
	return m, nil
}

func (c *npcResourceTickCatalog) Object(id int16) (LegacyObject, error) {
	o, ok := c.objects[id]
	if !ok {
		return LegacyObject{}, errors.New("missing object")
	}
	return o, nil
}

func npcResourceTickState() State {
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource: LegacyRoom{
					LegacyRoomHeader: LegacyRoomHeader{ID: 1},
					PermanentMonsters: [10]LegacyTimer{
						{LastTime: 0, Interval: 10, Misc: 11},
						{LastTime: 0, Interval: 10, Misc: 12},
						{LastTime: 100, Interval: 10, Misc: 13},
					},
				},
				Items:     &ItemCollection{Items: map[string]Item{}},
				PlayerIDs: []string{"player"},
				NPCIDs:    []string{"old"},
			},
			2: {
				Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 2}},
				Items:    &ItemCollection{Items: map[string]Item{}},
			},
		},
		Players: map[string]PlayerState{
			"player": {Body: LegacyMonster{Name: "영웅", Type: 0, RoomID: 1}, Online: true},
		},
		NPCs: map[string]NPCState{
			"old": {
				Body:    LegacyMonster{Name: "기존", Type: 1, RoomID: 1},
				Enemies: []NPCEnemy{},
			},
		},
		ActiveNPCIDs: []string{"old"},
	}
}

func npcResourceTickCatalogFixture() *npcResourceTickCatalog {
	// Both due templates deliberately share a display name. The runtime must
	// bind them to slot identities instead of suppressing one by name.
	return &npcResourceTickCatalog{
		monsters: map[int16]LegacyMonster{
			11: {Name: "쌍둥이", Type: 1, HPMax: 10, HPCurrent: 10},
			12: {Name: "쌍둥이", Type: 1, HPMax: 11, HPCurrent: 11},
		},
		objects: map[int16]LegacyObject{},
	}
}

func npcResourceTickRoll(low, _ int) int { return low }

func TestPlanApplyNPCResourceTickPreservesOriginAndOrders(t *testing.T) {
	s := npcResourceTickState()
	catalog := npcResourceTickCatalogFixture()
	allocated := []string{"npc-slot-0", "npc-slot-1"}
	allocAt := 0
	before := s.clone()
	proposal, err := s.PlanNPCResourceTick(1, catalog, 10, npcResourceTickRoll, func() (string, error) {
		id := allocated[allocAt]
		allocAt++
		return id, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s, before) {
		t.Fatal("planning mutated source state")
	}
	if !reflect.DeepEqual(proposal.AfterRoomNPCIDs, []string{"old", "npc-slot-0", "npc-slot-1"}) {
		t.Fatalf("room order: %#v", proposal.AfterRoomNPCIDs)
	}
	if !reflect.DeepEqual(proposal.AfterActiveNPCIDs, []string{"npc-slot-1", "npc-slot-0", "old"}) {
		t.Fatalf("active order: %#v", proposal.AfterActiveNPCIDs)
	}
	if len(proposal.Spawns) != 2 || proposal.Spawns[0].Origin != (NPCPermanentOrigin{RoomID: 1, Slot: 0}) || proposal.Spawns[1].Origin != (NPCPermanentOrigin{RoomID: 1, Slot: 1}) {
		t.Fatalf("spawn origins: %#v", proposal.Spawns)
	}
	if !reflect.DeepEqual(catalog.monCalls, []int16{11, 12}) {
		t.Fatalf("catalog order: %#v", catalog.monCalls)
	}

	next, err := s.ApplyNPCResourceTick(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(next.Rooms[1].NPCIDs, []string{"old", "npc-slot-0", "npc-slot-1"}) || !reflect.DeepEqual(next.ActiveNPCIDs, []string{"npc-slot-1", "npc-slot-0", "old"}) {
		t.Fatalf("applied order room=%#v active=%#v", next.Rooms[1].NPCIDs, next.ActiveNPCIDs)
	}
	for slot, id := range []string{"npc-slot-0", "npc-slot-1"} {
		npc := next.NPCs[id]
		want := NPCPermanentOrigin{RoomID: 1, Slot: uint8(slot)}
		if npc.PermanentOrigin == nil || *npc.PermanentOrigin != want || npc.Body.RoomID != 1 || !flag(npc.Body.Flags[:], npcPermanentFlag) {
			t.Fatalf("slot %d identity: %+v", slot, npc)
		}
	}
	if err := next.Validate(); err != nil {
		t.Fatal(err)
	}
	// The due timers remain due, but their exact origins now suppress a
	// duplicate without consulting the shared display name.
	again, err := next.PlanNPCResourceTick(1, catalog, 10, func(int, int) int { t.Fatal("duplicate consumed RNG"); return 0 }, func() (string, error) { t.Fatal("duplicate allocated ID"); return "", nil })
	if err != nil || len(again.Spawns) != 0 {
		t.Fatalf("duplicate respawn: proposal=%+v err=%v", again, err)
	}
}

func TestNPCResourceTickUsesOriginNotNameAndFindsMovedIdentity(t *testing.T) {
	s := npcResourceTickState()
	room := s.Rooms[2]
	room.NPCIDs = []string{"moved"}
	s.Rooms[2] = room
	npc := NPCState{
		Body:            LegacyMonster{Name: "쌍둥이", Type: 1, RoomID: 2, Flags: [8]byte{1}},
		Enemies:         []NPCEnemy{},
		PermanentOrigin: &NPCPermanentOrigin{RoomID: 1, Slot: 0},
	}
	s.NPCs["moved"] = npc
	// The same-name permanent identity is in room 2, but its origin occupies
	// room 1 slot 0. Only slot 1 may respawn.
	proposal, err := s.PlanNPCResourceTick(1, npcResourceTickCatalogFixture(), 10, npcResourceTickRoll, func() (string, error) { return "fresh", nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(proposal.Spawns) != 1 || proposal.Spawns[0].Origin != (NPCPermanentOrigin{RoomID: 1, Slot: 1}) {
		t.Fatalf("origin matching used names or room location: %#v", proposal.Spawns)
	}
}

func TestNPCResourceTickRejectsUnresolvedOriginBeforeDependencies(t *testing.T) {
	s := npcResourceTickState()
	npc := s.NPCs["old"]
	npc.Body.Flags[0] |= 1
	s.NPCs["old"] = npc
	called := false
	proposal, err := s.PlanNPCResourceTick(1, npcResourceTickCatalogFixture(), 10, func(int, int) int {
		called = true
		return 1
	}, func() (string, error) {
		called = true
		return "id", nil
	})
	if err == nil || !reflect.DeepEqual(proposal, NPCResourceTickProposal{}) || called {
		t.Fatalf("unresolved origin was not fail-closed: proposal=%+v err=%v called=%v", proposal, err, called)
	}
}

func TestNPCResourceTickAllocatorFailureIsAtomic(t *testing.T) {
	s := npcResourceTickState()
	before := s.clone()
	allocCalls, rollCalls := 0, 0
	proposal, err := s.PlanNPCResourceTick(1, npcResourceTickCatalogFixture(), 10, func(low, high int) int {
		rollCalls++
		return low
	}, func() (string, error) {
		allocCalls++
		if allocCalls == 2 {
			return "", errors.New("allocator stopped")
		}
		return "partial", nil
	})
	if err == nil || !reflect.DeepEqual(proposal, NPCResourceTickProposal{}) {
		t.Fatalf("partial allocator result escaped: proposal=%+v err=%v", proposal, err)
	}
	if allocCalls != 2 || rollCalls == 0 || !reflect.DeepEqual(s, before) {
		t.Fatalf("atomicity calls=%d rolls=%d stateChanged=%v", allocCalls, rollCalls, !reflect.DeepEqual(s, before))
	}
}

func TestNPCResourceTickRejectsStaleOrTamperedProposal(t *testing.T) {
	s := npcResourceTickState()
	allocAt := 0
	proposal, err := s.PlanNPCResourceTick(1, npcResourceTickCatalogFixture(), 10, npcResourceTickRoll, func() (string, error) {
		allocAt++
		return "fresh-" + string(rune('0'+allocAt)), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	changed := s.clone()
	r := changed.Rooms[1]
	r.Resource.PermanentMonsters[0].LastTime = 1
	changed.Rooms[1] = r
	if next, err := changed.ApplyNPCResourceTick(proposal); err == nil || !reflect.DeepEqual(next, State{}) {
		t.Fatalf("stale proposal accepted: next=%+v err=%v", next, err)
	}

	tampered := proposal
	tampered.Spawns = append([]NPCResourceTickSpawn(nil), proposal.Spawns...)
	tampered.Spawns[0].Origin.Slot = 9
	if next, err := s.ApplyNPCResourceTick(tampered); err == nil || !reflect.DeepEqual(next, State{}) {
		t.Fatalf("tampered origin accepted: next=%+v err=%v", next, err)
	}
}

func TestNPCResourceTickNoPlayersLeavesUnknownActiveOrderUntouched(t *testing.T) {
	s := npcResourceTickState()
	r := s.Rooms[1]
	r.PlayerIDs = nil
	s.Rooms[1] = r
	player := s.Players["player"]
	player.Online = false
	s.Players["player"] = player
	s.ActiveNPCIDs = nil
	allocAt := 0
	proposal, err := s.PlanNPCResourceTick(1, npcResourceTickCatalogFixture(), 10, npcResourceTickRoll, func() (string, error) {
		allocAt++
		return "fresh-" + string(rune('0'+allocAt)), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	next, err := s.ApplyNPCResourceTick(proposal)
	if err != nil || next.ActiveNPCIDs != nil || len(next.Rooms[1].NPCIDs) != 3 {
		t.Fatalf("unknown inactive order changed: active=%#v room=%#v err=%v", next.ActiveNPCIDs, next.Rooms[1].NPCIDs, err)
	}
}

func TestNPCResourceTickPlanApplyRace(t *testing.T) {
	s := npcResourceTickState()
	catalog := npcResourceTickCatalogFixture()
	const workers = 8
	var wg sync.WaitGroup
	errCh := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			allocAt := 0
			proposal, err := s.PlanNPCResourceTick(1, catalog, 10, npcResourceTickRoll, func() (string, error) {
				id := string(rune('a'+worker)) + string(rune('0'+allocAt))
				allocAt++
				return id, nil
			})
			if err != nil {
				errCh <- err
				return
			}
			if _, err := s.ApplyNPCResourceTick(proposal); err != nil {
				errCh <- err
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
}
