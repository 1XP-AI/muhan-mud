package world

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"testing"
)

type npcRandomCatalogResult struct {
	monster LegacyMonster
	err     error
}

type npcRandomTestCatalog struct {
	monsters       map[int16]LegacyMonster
	monsterResults map[int16][]npcRandomCatalogResult
	monsterCalls   []int16
	objects        map[int16]LegacyObject
	objectErrors   map[int16]error
	objectCalls    []int16
}

func (c *npcRandomTestCatalog) Monster(id int16) (LegacyMonster, error) {
	c.monsterCalls = append(c.monsterCalls, id)
	if results := c.monsterResults[id]; len(results) != 0 {
		result := results[0]
		c.monsterResults[id] = results[1:]
		return result.monster, result.err
	}
	monster, ok := c.monsters[id]
	if !ok {
		return LegacyMonster{}, ErrNPCRandomCatalogAbsent
	}
	return monster, nil
}

func (c *npcRandomTestCatalog) Object(id int16) (LegacyObject, error) {
	c.objectCalls = append(c.objectCalls, id)
	if err := c.objectErrors[id]; err != nil {
		return LegacyObject{}, err
	}
	object, ok := c.objects[id]
	if !ok {
		return LegacyObject{}, ErrNPCRandomCatalogAbsent
	}
	return object, nil
}

type npcRandomTestRoller struct {
	t      *testing.T
	values []int
	calls  [][2]int
	index  int
}

func (r *npcRandomTestRoller) roll(low, high int) int {
	r.calls = append(r.calls, [2]int{low, high})
	if r.index >= len(r.values) {
		r.t.Fatalf("unexpected random call %d..%d", low, high)
		return low
	}
	value := r.values[r.index]
	r.index++
	if value < low || value > high {
		r.t.Fatalf("random fixture value %d outside %d..%d", value, low, high)
	}
	return value
}

func npcRandomBaseState() State {
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}, Traffic: 100},
				Items:    &ItemCollection{Items: map[string]Item{}},
			},
		},
		Players:      map[string]PlayerState{},
		NPCs:         map[string]NPCState{},
		ActiveNPCIDs: []string{},
	}
}

func npcRandomAddPlayer(s *State, id string, roomID int16) {
	s.Players[id] = PlayerState{
		Body:   LegacyMonster{Name: id, Type: 0, RoomID: roomID},
		Online: true,
		Items:  &ItemCollection{Items: map[string]Item{}},
	}
	room := s.Rooms[roomID]
	room.PlayerIDs = append(room.PlayerIDs, id)
	s.Rooms[roomID] = room
}

func npcRandomTemplate(name string, wander, dexterity byte) LegacyMonster {
	return LegacyMonster{
		Name:   name,
		Type:   1,
		Wander: wander,
		Stats:  [5]byte{0, dexterity},
		Timers: [45]LegacyTimer{
			npcRandomAttackTimer:   {Interval: 9, LastTime: 11},
			npcRandomScavengeTimer: {Interval: 8, LastTime: 22},
			npcRandomWanderTimer:   {Interval: 7, LastTime: 33},
		},
	}
}

func npcRandomSetFlag(bits *[8]byte, bit int) {
	bits[bit/8] |= 1 << (bit % 8)
}

func npcRandomMustClone(t *testing.T, s State) State {
	t.Helper()
	return s.clone()
}

func npcRandomAllocator(ids ...string) func() (string, error) {
	index := 0
	return func() (string, error) {
		if index >= len(ids) {
			return "", errors.New("allocator exhausted")
		}
		id := ids[index]
		index++
		return id, nil
	}
}

func TestNPCRandomProducerDeduplicatesOccupiedRoomsAndNoOps(t *testing.T) {
	state := npcRandomBaseState()
	npcRandomAddPlayer(&state, "second", 1)
	npcRandomAddPlayer(&state, "first", 1)
	room := state.Rooms[1]
	room.Resource.Random[0] = 0
	state.Rooms[1] = room
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	before := npcRandomMustClone(t, state)
	roller := &npcRandomTestRoller{t: t, values: []int{1, 0}}
	proposal, err := state.PlanNPCRandomProducer(NPCRandomProducerInput{Roll: roller.roll})
	if err != nil {
		t.Fatal(err)
	}
	if len(proposal.Rooms) != 1 || proposal.Rooms[0].PlayerCount != 2 || !proposal.Rooms[0].TrafficPassed || proposal.Rooms[0].RandomIndex != 0 || proposal.Rooms[0].TemplateID != 0 || len(proposal.Rooms[0].Spawns) != 0 {
		t.Fatalf("duplicate-room proposal=%+v", proposal)
	}
	if !reflect.DeepEqual(roller.calls, [][2]int{{1, 100}, {0, 9}}) {
		t.Fatalf("duplicate-room RNG calls=%v", roller.calls)
	}
	if !reflect.DeepEqual(state, before) {
		t.Fatal("planning changed source state")
	}
	next, err := state.ApplyNPCRandomProducer(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(next, before) {
		t.Fatalf("no-op changed state: %#v", next)
	}
}

func TestNPCRandomProducerTrafficAndCatalogNoOps(t *testing.T) {
	t.Run("traffic failure consumes only traffic roll", func(t *testing.T) {
		state := npcRandomBaseState()
		npcRandomAddPlayer(&state, "player", 1)
		room := state.Rooms[1]
		room.Resource.Traffic = 0
		room.Resource.Random[0] = 1
		state.Rooms[1] = room
		roller := &npcRandomTestRoller{t: t, values: []int{1}}
		proposal, err := state.PlanNPCRandomProducer(NPCRandomProducerInput{Roll: roller.roll})
		if err != nil {
			t.Fatal(err)
		}
		decision := proposal.Rooms[0]
		if decision.TrafficPassed || decision.RandomRolled || decision.RandomIndex != -1 || decision.TemplateID != 0 || decision.SpawnCount != 0 {
			t.Fatalf("traffic decision=%+v", decision)
		}
		if !reflect.DeepEqual(roller.calls, [][2]int{{1, 100}}) {
			t.Fatalf("traffic RNG calls=%v", roller.calls)
		}
		next, err := state.ApplyNPCRandomProducer(proposal)
		if err != nil || !reflect.DeepEqual(next, state) {
			t.Fatalf("traffic no-op next=%#v err=%v", next, err)
		}
	})

	t.Run("absent monster catalog entry", func(t *testing.T) {
		state := npcRandomBaseState()
		npcRandomAddPlayer(&state, "player", 1)
		room := state.Rooms[1]
		room.Resource.Random[0] = 1
		state.Rooms[1] = room
		catalog := &npcRandomTestCatalog{monsters: map[int16]LegacyMonster{}, objects: map[int16]LegacyObject{}}
		roller := &npcRandomTestRoller{t: t, values: []int{1, 0}}
		allocatorCalled := false
		proposal, err := state.PlanNPCRandomProducer(NPCRandomProducerInput{
			Catalog:  catalog,
			Roll:     roller.roll,
			Allocate: func() (string, error) { allocatorCalled = true; return "unexpected", nil },
		})
		if err != nil {
			t.Fatal(err)
		}
		decision := proposal.Rooms[0]
		if decision.TemplateID != 1 || !decision.RandomRolled || decision.TemplatePresent || len(decision.Spawns) != 0 || allocatorCalled {
			t.Fatalf("absent catalog decision=%+v allocatorCalled=%v", decision, allocatorCalled)
		}
		if !reflect.DeepEqual(catalog.monsterCalls, []int16{1}) || !reflect.DeepEqual(roller.calls, [][2]int{{1, 100}, {0, 9}}) {
			t.Fatalf("absent catalog calls monsters=%v RNG=%v", catalog.monsterCalls, roller.calls)
		}
		next, err := state.ApplyNPCRandomProducer(proposal)
		if err != nil || !reflect.DeepEqual(next, state) {
			t.Fatalf("absent catalog no-op next=%#v err=%v", next, err)
		}
	})
}

func TestNPCRandomProducerUsesRPLWANAndNumWanderBounds(t *testing.T) {
	for _, tc := range []struct {
		name      string
		group     bool
		wander    byte
		players   int
		wantCount int
	}{
		{name: "RPLWAN player count", group: true, wander: 1, players: 3, wantCount: 2},
		{name: "template NumWander", group: false, wander: 3, players: 1, wantCount: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := npcRandomBaseState()
			for i := 0; i < tc.players; i++ {
				npcRandomAddPlayer(&state, "player-"+string(rune('a'+i)), 1)
			}
			room := state.Rooms[1]
			room.Resource.Random[0] = 1
			if tc.group {
				npcRandomSetFlag(&room.Resource.Flags, npcRandomGroupFlag)
			}
			state.Rooms[1] = room
			catalog := &npcRandomTestCatalog{
				monsters: map[int16]LegacyMonster{1: npcRandomTemplate("wanderer", tc.wander, 19)},
				objects:  map[int16]LegacyObject{},
			}
			values := []int{1, 0, tc.wantCount}
			for i := 0; i < tc.wantCount; i++ {
				// Each fresh instance draws its item-count roll and then one
				// carry-slot roll, even when Carry is sparse/empty.
				values = append(values, 1, 0)
			}
			roller := &npcRandomTestRoller{t: t, values: values}
			proposal, err := state.PlanNPCRandomProducer(NPCRandomProducerInput{
				Catalog:  catalog,
				Roll:     roller.roll,
				Allocate: npcRandomAllocator("npc-a", "npc-b", "npc-c"),
			})
			if err != nil {
				t.Fatal(err)
			}
			decision := proposal.Rooms[0]
			if decision.SpawnCount != tc.wantCount || len(decision.Spawns) != tc.wantCount || decision.GroupWander != tc.group {
				t.Fatalf("count decision=%+v", decision)
			}
			if len(catalog.monsterCalls) != tc.wantCount || len(proposal.AfterActiveNPCIDs) != tc.wantCount {
				t.Fatalf("reload/admission calls=%v active=%v", catalog.monsterCalls, proposal.AfterActiveNPCIDs)
			}
			next, err := state.ApplyNPCRandomProducer(proposal)
			if err != nil {
				t.Fatal(err)
			}
			if len(next.NPCs) != tc.wantCount || len(next.Rooms[1].NPCIDs) != tc.wantCount {
				t.Fatalf("applied count NPCs=%d room=%v", len(next.NPCs), next.Rooms[1].NPCIDs)
			}
		})
	}
}

func TestNPCRandomProducerRPLWANOnePlayerConsumesCountRoll(t *testing.T) {
	state := npcRandomBaseState()
	npcRandomAddPlayer(&state, "player", 1)
	room := state.Rooms[1]
	room.Resource.Random[0] = 1
	npcRandomSetFlag(&room.Resource.Flags, npcRandomGroupFlag)
	state.Rooms[1] = room
	catalog := &npcRandomTestCatalog{
		monsters: map[int16]LegacyMonster{1: npcRandomTemplate("single", 1, 19)},
		objects:  map[int16]LegacyObject{},
	}
	roller := &npcRandomTestRoller{t: t, values: []int{1, 0, 1, 1, 0}}
	proposal, err := state.PlanNPCRandomProducer(NPCRandomProducerInput{
		Catalog:  catalog,
		Roll:     roller.roll,
		Allocate: npcRandomAllocator("single-id"),
	})
	if err != nil {
		t.Fatal(err)
	}
	decision := proposal.Rooms[0]
	if !decision.GroupWander || decision.SpawnCount != 1 || !decision.SpawnCountRolled || decision.SpawnCountRoll != 1 {
		t.Fatalf("single-player RPLWAN decision=%+v", decision)
	}
	wantCalls := [][2]int{{1, 100}, {0, 9}, {1, 1}, {1, 100}, {0, 9}}
	if !reflect.DeepEqual(roller.calls, wantCalls) {
		t.Fatalf("single-player RPLWAN RNG calls=%v want=%v", roller.calls, wantCalls)
	}
}

func TestNPCRandomProducerResetsTimersAndDexterityAttackInterval(t *testing.T) {
	for _, tc := range []struct {
		name         string
		dexterity    byte
		wantInterval int32
	}{
		{name: "below dexterity threshold", dexterity: 19, wantInterval: 3},
		{name: "at dexterity threshold", dexterity: 20, wantInterval: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := npcRandomBaseState()
			npcRandomAddPlayer(&state, "player", 1)
			room := state.Rooms[1]
			room.Resource.Random[0] = 1
			state.Rooms[1] = room
			template := npcRandomTemplate("timer-wolf", 1, tc.dexterity)
			template.Timers[npcRandomHealTimer] = LegacyTimer{LastTime: -1, Interval: 1, Misc: 17}
			catalog := &npcRandomTestCatalog{
				monsters: map[int16]LegacyMonster{1: template},
				objects:  map[int16]LegacyObject{},
			}
			proposal, err := state.PlanNPCRandomProducer(NPCRandomProducerInput{
				Now:      9876,
				Catalog:  catalog,
				Roll:     (&npcRandomTestRoller{t: t, values: []int{1, 0, 1, 0}}).roll,
				Allocate: npcRandomAllocator("timer-id"),
			})
			if err != nil {
				t.Fatal(err)
			}
			next, err := state.ApplyNPCRandomProducer(proposal)
			if err != nil {
				t.Fatal(err)
			}
			body := next.NPCs["timer-id"].Body
			for _, timer := range []int{npcRandomAttackTimer, npcRandomScavengeTimer, npcRandomWanderTimer} {
				if body.Timers[timer].LastTime != 9876 {
					t.Fatalf("timer %d=%+v", timer, body.Timers[timer])
				}
			}
			healTimer := body.Timers[npcRandomHealTimer]
			if healTimer.LastTime != 9876 || healTimer.Interval != 60 || healTimer.Misc != 17 {
				t.Fatalf("heal timer=%+v", healTimer)
			}
			if body.Timers[npcRandomAttackTimer].Interval != tc.wantInterval || body.Timers[npcRandomScavengeTimer].Interval != template.Timers[npcRandomScavengeTimer].Interval || body.Timers[npcRandomWanderTimer].Interval != template.Timers[npcRandomWanderTimer].Interval {
				t.Fatalf("timer intervals=%+v", body.Timers)
			}
		})
	}
}

func TestNPCRandomProducerPreservesRandomCallOrderAndFixedGoldSparseCarry(t *testing.T) {
	t.Run("fixed gold keeps gold and consumes sparse fixed-slot draw", func(t *testing.T) {
		state := npcRandomBaseState()
		npcRandomAddPlayer(&state, "player", 1)
		room := state.Rooms[1]
		room.Resource.Random[0] = 1
		state.Rooms[1] = room
		template := npcRandomTemplate("fixed", 1, 19)
		template.Gold = 777
		template.Carry[1] = 10
		npcRandomSetFlag(&template.Flags, npcRandomFixedGoldFlag)
		object := LegacyObject{Name: "enchanted sword", Value: 100}
		npcRandomSetFlag(&object.Flags, npcRandomEnchantFlag)
		catalog := &npcRandomTestCatalog{
			monsters: map[int16]LegacyMonster{1: template},
			objects:  map[int16]LegacyObject{10: object},
		}
		roller := &npcRandomTestRoller{t: t, values: []int{1, 0, 90, 10, 1, 99, 100}}
		proposal, err := state.PlanNPCRandomProducer(NPCRandomProducerInput{
			Catalog:  catalog,
			Roll:     roller.roll,
			Allocate: npcRandomAllocator("fixed-id"),
		})
		if err != nil {
			t.Fatal(err)
		}
		wantCalls := [][2]int{{1, 100}, {0, 9}, {1, 100}, {0, 49}, {0, 49}, {1, 100}, {90, 110}}
		if !reflect.DeepEqual(roller.calls, wantCalls) {
			t.Fatalf("fixed-gold RNG calls=%v want=%v", roller.calls, wantCalls)
		}
		next, err := state.ApplyNPCRandomProducer(proposal)
		if err != nil {
			t.Fatal(err)
		}
		body := next.NPCs["fixed-id"].Body
		if body.Gold != 777 || len(body.Inventory) != 1 || body.Inventory[0].Value != 100 || body.Inventory[0].Adjustment != 3 || body.Inventory[0].DicePlus != 3 || !reflect.DeepEqual(catalog.objectCalls, []int16{10}) {
			t.Fatalf("fixed-gold body=%+v objectCalls=%v", body, catalog.objectCalls)
		}
	})

	t.Run("non-fixed gold is drawn after item value", func(t *testing.T) {
		state := npcRandomBaseState()
		npcRandomAddPlayer(&state, "player", 1)
		room := state.Rooms[1]
		room.Resource.Random[0] = 1
		state.Rooms[1] = room
		template := npcRandomTemplate("gold", 1, 19)
		template.Gold = 100
		template.Carry[0] = 20
		catalog := &npcRandomTestCatalog{
			monsters: map[int16]LegacyMonster{1: template},
			objects:  map[int16]LegacyObject{20: {Name: "coin", Value: 10}},
		}
		roller := &npcRandomTestRoller{t: t, values: []int{1, 0, 1, 0, 11, 55}}
		proposal, err := state.PlanNPCRandomProducer(NPCRandomProducerInput{
			Catalog:  catalog,
			Roll:     roller.roll,
			Allocate: npcRandomAllocator("gold-id"),
		})
		if err != nil {
			t.Fatal(err)
		}
		wantCalls := [][2]int{{1, 100}, {0, 9}, {1, 100}, {0, 9}, {9, 11}, {10, 100}}
		if !reflect.DeepEqual(roller.calls, wantCalls) {
			t.Fatalf("gold RNG calls=%v want=%v", roller.calls, wantCalls)
		}
		next, err := state.ApplyNPCRandomProducer(proposal)
		if err != nil || next.NPCs["gold-id"].Body.Gold != 55 || next.NPCs["gold-id"].Body.Inventory[0].Value != 11 {
			t.Fatalf("gold body=%+v err=%v", next.NPCs["gold-id"].Body, err)
		}
	})
}

func TestNPCRandomProducerReloadsTemplatesAndPreservesAdmissionOrder(t *testing.T) {
	state := npcRandomBaseState()
	npcRandomAddPlayer(&state, "player", 1)
	room := state.Rooms[1]
	room.Resource.Random[0] = 1
	state.Rooms[1] = room
	catalog := &npcRandomTestCatalog{
		monsterResults: map[int16][]npcRandomCatalogResult{1: {
			{monster: npcRandomTemplate("Zulu", 3, 19)},
			{monster: npcRandomTemplate("Alpha", 3, 19)},
			{monster: npcRandomTemplate("Mike", 3, 19)},
		}},
		monsters: map[int16]LegacyMonster{},
		objects:  map[int16]LegacyObject{},
	}
	roller := &npcRandomTestRoller{t: t, values: []int{1, 0, 3, 1, 0, 1, 0, 1, 0}}
	proposal, err := state.PlanNPCRandomProducer(NPCRandomProducerInput{
		Catalog:  catalog,
		Roll:     roller.roll,
		Allocate: npcRandomAllocator("first", "second", "third"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(catalog.monsterCalls, []int16{1, 1, 1}) {
		t.Fatalf("template reload calls=%v", catalog.monsterCalls)
	}
	spawns := proposal.Spawns()
	if len(spawns) != 3 || spawns[0].ID != "first" || spawns[0].Body.Name != "Zulu" || spawns[1].ID != "second" || spawns[1].Body.Name != "Alpha" || spawns[2].ID != "third" || spawns[2].Body.Name != "Mike" {
		t.Fatalf("source spawn order=%+v", spawns)
	}
	if !reflect.DeepEqual(proposal.Rooms[0].AfterNPCIDs, []string{"second", "third", "first"}) || !reflect.DeepEqual(proposal.AfterActiveNPCIDs, []string{"third", "second", "first"}) {
		t.Fatalf("admission order room=%v active=%v", proposal.Rooms[0].AfterNPCIDs, proposal.AfterActiveNPCIDs)
	}
	next, err := state.ApplyNPCRandomProducer(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(next.Rooms[1].NPCIDs, []string{"second", "third", "first"}) || !reflect.DeepEqual(next.ActiveNPCIDs, []string{"third", "second", "first"}) {
		t.Fatalf("applied order room=%v active=%v", next.Rooms[1].NPCIDs, next.ActiveNPCIDs)
	}
}

func TestNPCRandomProducerPreservesPlyFirstEncounterRoomOrderAndDeduplicatesTraffic(t *testing.T) {
	state := State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}, Traffic: 100},
				Items:     &ItemCollection{Items: map[string]Item{}},
				PlayerIDs: []string{"third"},
			},
			2: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 2}, Traffic: 100},
				Items:     &ItemCollection{Items: map[string]Item{}},
				PlayerIDs: []string{"first", "second"},
			},
		},
		Players: map[string]PlayerState{
			"first":  {Body: LegacyMonster{Name: "First", Type: 0, RoomID: 2}, Online: true},
			"second": {Body: LegacyMonster{Name: "Second", Type: 0, RoomID: 2}, Online: true},
			"third":  {Body: LegacyMonster{Name: "Third", Type: 0, RoomID: 1}, Online: true},
		},
		NPCs:         map[string]NPCState{},
		ActiveNPCIDs: []string{},
	}
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	roller := &npcRandomTestRoller{t: t, values: []int{1, 0, 1, 0}}
	proposal, err := state.PlanNPCRandomProducer(NPCRandomProducerInput{
		PlyOrder: []string{"second", "third", "first"},
		Roll:     roller.roll,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := []int16{proposal.Rooms[0].RoomID, proposal.Rooms[1].RoomID}; !reflect.DeepEqual(got, []int16{2, 1}) {
		t.Fatalf("Ply first-encounter room order=%v", got)
	}
	if !reflect.DeepEqual(roller.calls, [][2]int{{1, 100}, {0, 9}, {1, 100}, {0, 9}}) {
		t.Fatalf("traffic-room dedup RNG calls=%v", roller.calls)
	}
	next, err := state.ApplyNPCRandomProducer(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(next, state) {
		t.Fatalf("empty random slots changed state: %#v", next)
	}
}

func TestNPCRandomProducerRejectsAllocatorAndLoaderFailuresAtomically(t *testing.T) {
	t.Run("duplicate identity", func(t *testing.T) {
		state := npcRandomBaseState()
		npcRandomAddPlayer(&state, "player", 1)
		room := state.Rooms[1]
		room.Resource.Random[0] = 1
		state.Rooms[1] = room
		catalog := &npcRandomTestCatalog{
			monsters: map[int16]LegacyMonster{1: npcRandomTemplate("duplicate", 2, 19)},
			objects:  map[int16]LegacyObject{},
		}
		before := npcRandomMustClone(t, state)
		roller := &npcRandomTestRoller{t: t, values: []int{1, 0, 2, 1, 0, 1, 0}}
		proposal, err := state.PlanNPCRandomProducer(NPCRandomProducerInput{
			Catalog:  catalog,
			Roll:     roller.roll,
			Allocate: func() (string, error) { return "same-id", nil },
		})
		if err == nil || !reflect.DeepEqual(proposal, NPCRandomProducerProposal{}) || !reflect.DeepEqual(state, before) {
			t.Fatalf("duplicate identity proposal=%+v err=%v stateChanged=%v", proposal, err, !reflect.DeepEqual(state, before))
		}
	})

	t.Run("allocator failure", func(t *testing.T) {
		state := npcRandomBaseState()
		npcRandomAddPlayer(&state, "player", 1)
		room := state.Rooms[1]
		room.Resource.Random[0] = 1
		state.Rooms[1] = room
		catalog := &npcRandomTestCatalog{
			monsters: map[int16]LegacyMonster{1: npcRandomTemplate("allocator", 2, 19)},
			objects:  map[int16]LegacyObject{},
		}
		before := npcRandomMustClone(t, state)
		roller := &npcRandomTestRoller{t: t, values: []int{1, 0, 2, 1, 0, 1, 0}}
		allocations := 0
		proposal, err := state.PlanNPCRandomProducer(NPCRandomProducerInput{
			Catalog: catalog,
			Roll:    roller.roll,
			Allocate: func() (string, error) {
				allocations++
				if allocations == 1 {
					return "partial-id", nil
				}
				return "", errors.New("allocator unavailable")
			},
		})
		if err == nil || !reflect.DeepEqual(proposal, NPCRandomProducerProposal{}) || !reflect.DeepEqual(state, before) {
			t.Fatalf("allocator failure proposal=%+v err=%v stateChanged=%v", proposal, err, !reflect.DeepEqual(state, before))
		}
	})

	t.Run("template reload failure", func(t *testing.T) {
		state := npcRandomBaseState()
		npcRandomAddPlayer(&state, "player", 1)
		room := state.Rooms[1]
		room.Resource.Random[0] = 1
		state.Rooms[1] = room
		template := npcRandomTemplate("reload", 2, 19)
		catalog := &npcRandomTestCatalog{
			monsterResults: map[int16][]npcRandomCatalogResult{1: {
				{monster: template},
				{err: errors.New("catalog reload failed")},
			}},
			monsters: map[int16]LegacyMonster{},
			objects:  map[int16]LegacyObject{},
		}
		before := npcRandomMustClone(t, state)
		roller := &npcRandomTestRoller{t: t, values: []int{1, 0, 2, 1, 0}}
		proposal, err := state.PlanNPCRandomProducer(NPCRandomProducerInput{
			Catalog:  catalog,
			Roll:     roller.roll,
			Allocate: npcRandomAllocator("partial-id", "never"),
		})
		if err == nil || !reflect.DeepEqual(proposal, NPCRandomProducerProposal{}) || !reflect.DeepEqual(state, before) {
			t.Fatalf("reload failure proposal=%+v err=%v stateChanged=%v", proposal, err, !reflect.DeepEqual(state, before))
		}
	})

	t.Run("object loader failure", func(t *testing.T) {
		state := npcRandomBaseState()
		npcRandomAddPlayer(&state, "player", 1)
		room := state.Rooms[1]
		room.Resource.Random[0] = 1
		state.Rooms[1] = room
		template := npcRandomTemplate("object", 1, 19)
		template.Carry[0] = 10
		catalog := &npcRandomTestCatalog{
			monsters:     map[int16]LegacyMonster{1: template},
			objects:      map[int16]LegacyObject{},
			objectErrors: map[int16]error{10: errors.New("object loader failed")},
		}
		before := npcRandomMustClone(t, state)
		roller := &npcRandomTestRoller{t: t, values: []int{1, 0, 1, 0}}
		proposal, err := state.PlanNPCRandomProducer(NPCRandomProducerInput{
			Catalog:  catalog,
			Roll:     roller.roll,
			Allocate: npcRandomAllocator("never"),
		})
		if err == nil || !reflect.DeepEqual(proposal, NPCRandomProducerProposal{}) || !reflect.DeepEqual(state, before) {
			t.Fatalf("object failure proposal=%+v err=%v stateChanged=%v", proposal, err, !reflect.DeepEqual(state, before))
		}
	})
}

func TestNPCRandomProducerRejectsStaleTamperedAndReplayAtomically(t *testing.T) {
	state := npcRandomBaseState()
	npcRandomAddPlayer(&state, "player", 1)
	room := state.Rooms[1]
	room.Resource.Random[0] = 1
	state.Rooms[1] = room
	catalog := &npcRandomTestCatalog{
		monsters: map[int16]LegacyMonster{1: npcRandomTemplate("replay", 1, 19)},
		objects:  map[int16]LegacyObject{},
	}
	proposal, err := state.PlanNPCRandomProducer(NPCRandomProducerInput{
		Catalog:  catalog,
		Roll:     func(low, _ int) int { return low },
		Allocate: npcRandomAllocator("replay-id"),
	})
	if err != nil {
		t.Fatal(err)
	}
	before := npcRandomMustClone(t, state)

	tampered := proposal
	tampered.Rooms = append([]NPCRandomRoomDecision(nil), proposal.Rooms...)
	tampered.Rooms[0] = proposal.Rooms[0]
	tampered.Rooms[0].TrafficRoll = 2
	if got, err := state.ApplyNPCRandomProducer(tampered); err == nil || !reflect.DeepEqual(got, State{}) || !reflect.DeepEqual(state, before) {
		t.Fatalf("tampered apply got=%#v err=%v", got, err)
	}

	stale := npcRandomMustClone(t, state)
	staleRoom := stale.Rooms[1]
	staleRoom.Resource.Traffic = 99
	stale.Rooms[1] = staleRoom
	if got, err := stale.ApplyNPCRandomProducer(proposal); err == nil || !reflect.DeepEqual(got, State{}) || !reflect.DeepEqual(state, before) {
		t.Fatalf("stale apply got=%#v err=%v source=%#v", got, err, state)
	}

	next, err := state.ApplyNPCRandomProducer(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if replay, err := state.ApplyNPCRandomProducer(proposal); err == nil || !reflect.DeepEqual(replay, State{}) {
		t.Fatalf("replay accepted state=%#v err=%v", replay, err)
	}
	if len(next.NPCs) != 1 || next.NPCs["replay-id"].Body.Name != "replay" {
		t.Fatalf("successful candidate=%#v", next)
	}
}

func TestNPCRandomProducerConcurrentApplyConsumesSameProposalOnce(t *testing.T) {
	state := npcRandomBaseState()
	npcRandomAddPlayer(&state, "player", 1)
	room := state.Rooms[1]
	room.Resource.Random[0] = 1
	state.Rooms[1] = room
	catalog := &npcRandomTestCatalog{
		monsters: map[int16]LegacyMonster{1: npcRandomTemplate("concurrent", 1, 19)},
		objects:  map[int16]LegacyObject{},
	}
	proposal, err := state.PlanNPCRandomProducer(NPCRandomProducerInput{
		Catalog:  catalog,
		Roll:     func(low, _ int) int { return low },
		Allocate: npcRandomAllocator("concurrent-id"),
	})
	if err != nil {
		t.Fatal(err)
	}

	type applyResult struct {
		state State
		err   error
	}
	results := make(chan applyResult, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			next, applyErr := state.ApplyNPCRandomProducer(proposal)
			results <- applyResult{state: next, err: applyErr}
		}()
	}
	wg.Wait()
	close(results)

	successes := 0
	failures := 0
	for result := range results {
		if result.err == nil {
			successes++
			if len(result.state.NPCs) != 1 || result.state.NPCs["concurrent-id"].Body.Name != "concurrent" {
				t.Fatalf("successful concurrent apply state=%#v", result.state)
			}
		} else {
			failures++
			if !reflect.DeepEqual(result.state, State{}) {
				t.Fatalf("failed concurrent apply leaked state=%#v err=%v", result.state, result.err)
			}
		}
	}
	if successes != 1 || failures != 1 {
		t.Fatalf("concurrent same-proposal applies successes=%d failures=%d", successes, failures)
	}
}

func TestNPCRandomProducerFailsClosedForUnresolvedCanonicalStateAndIndexes(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*State)
	}{
		{
			name: "canonical NPC map unresolved",
			mutate: func(s *State) {
				s.NPCs = nil
				s.ActiveNPCIDs = nil
			},
		},
		{
			name:   "active order unresolved",
			mutate: func(s *State) { s.ActiveNPCIDs = nil },
		},
		{
			name: "enemy relation unresolved",
			mutate: func(s *State) {
				s.NPCs["old"] = NPCState{Body: LegacyMonster{Name: "old", Type: 1, RoomID: 1}, Enemies: nil}
				r := s.Rooms[1]
				r.NPCIDs = []string{"old"}
				s.Rooms[1] = r
				s.ActiveNPCIDs = []string{"old"}
			},
		},
		{
			name: "canonical room floor unresolved",
			mutate: func(s *State) {
				r := s.Rooms[1]
				r.Items = nil
				s.Rooms[1] = r
			},
		},
		{
			name: "player index invalid",
			mutate: func(s *State) {
				r := s.Rooms[1]
				r.PlayerIDs = []string{"missing-player"}
				s.Rooms[1] = r
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state := npcRandomBaseState()
			npcRandomAddPlayer(&state, "player", 1)
			room := state.Rooms[1]
			room.Resource.Random[0] = 1
			state.Rooms[1] = room
			tc.mutate(&state)
			before := npcRandomMustClone(t, state)
			calls := 0
			proposal, err := state.PlanNPCRandomProducer(NPCRandomProducerInput{
				Roll: func(low, high int) int {
					calls++
					return low
				},
				Catalog:  &npcRandomTestCatalog{monsters: map[int16]LegacyMonster{}, objects: map[int16]LegacyObject{}},
				Allocate: func() (string, error) { calls++; return "unexpected", nil },
			})
			if err == nil || calls != 0 || !reflect.DeepEqual(proposal, NPCRandomProducerProposal{}) || !reflect.DeepEqual(state, before) {
				t.Fatalf("fail-closed proposal=%+v err=%v calls=%d stateChanged=%v", proposal, err, calls, !reflect.DeepEqual(state, before))
			}
		})
	}
}

func TestNPCRandomProducerProtectedLegacySourceUntouched(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not resolve test source path")
	}
	raw, err := os.ReadFile(filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../src/frp.new")))
	if err != nil {
		t.Fatal(err)
	}
	got := sha256Hex(raw)
	const want = "5632aa86319829f003efa9eb75ba24646ac999b54a0d2d32e7348cd65fe03014"
	if got != want {
		t.Fatalf("protected src/frp.new hash=%s want=%s", got, want)
	}
}

func sha256Hex(raw []byte) string {
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}
