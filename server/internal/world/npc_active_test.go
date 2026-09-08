package world

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestActiveNPCIDsValidationDistinguishesUnknownAndKnownState(t *testing.T) {
	if err := stateFixture().Validate(); err != nil {
		t.Fatalf("nil active list should remain an unknown, valid state: %v", err)
	}

	withoutNPCs := stateFixture()
	withoutNPCs.ActiveNPCIDs = []string{}
	if err := withoutNPCs.Validate(); err == nil {
		t.Fatal("known active list accepted without canonical NPC state")
	}

	known := npcCanonicalFixture()
	known.ActiveNPCIDs = []string{}
	if err := known.Validate(); err != nil {
		t.Fatalf("known empty active list rejected: %v", err)
	}

	for _, tc := range []struct {
		name string
		ids  []string
	}{
		{name: "duplicate", ids: []string{"npc-a", "npc-a"}},
		{name: "missing", ids: []string{"npc-a", "missing"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := npcCanonicalFixture()
			s.ActiveNPCIDs = tc.ids
			if err := s.Validate(); err == nil {
				t.Fatalf("invalid active list accepted: %v", tc.ids)
			}
		})
	}
}

func TestActiveNPCIDsClonePreservesNilEmptyAndCopiesOrder(t *testing.T) {
	unknown := stateFixture().clone()
	if unknown.ActiveNPCIDs != nil {
		t.Fatalf("clone changed unknown active list to known: %#v", unknown.ActiveNPCIDs)
	}

	empty := npcCanonicalFixture()
	empty.ActiveNPCIDs = []string{}
	knownEmpty := empty.clone()
	if knownEmpty.ActiveNPCIDs == nil || len(knownEmpty.ActiveNPCIDs) != 0 {
		t.Fatalf("clone lost known empty active list: %#v", knownEmpty.ActiveNPCIDs)
	}

	s := npcCanonicalFixture()
	s.ActiveNPCIDs = []string{"npc-b", "npc-a"}
	next := s.clone()
	if !reflect.DeepEqual(next.ActiveNPCIDs, s.ActiveNPCIDs) {
		t.Fatalf("clone changed active order: got %#v want %#v", next.ActiveNPCIDs, s.ActiveNPCIDs)
	}
	next.ActiveNPCIDs[0] = "changed"
	if s.ActiveNPCIDs[0] != "npc-b" {
		t.Fatal("clone active list aliases source")
	}
}

func TestActiveNPCIDsJSONPreservesUnknownAndKnownEmpty(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   State
		want []string
	}{
		{name: "unknown", in: stateFixture(), want: nil},
		{name: "known empty", in: func() State {
			s := npcCanonicalFixture()
			s.ActiveNPCIDs = []string{}
			return s
		}(), want: []string{}},
		{name: "known order", in: func() State {
			s := npcCanonicalFixture()
			s.ActiveNPCIDs = []string{"npc-b", "npc-a"}
			return s
		}(), want: []string{"npc-b", "npc-a"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			got, err := DecodeState(raw)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.ActiveNPCIDs, tc.want) {
				t.Fatalf("active list round trip: got %#v want %#v", got.ActiveNPCIDs, tc.want)
			}
		})
	}
}

func TestRecoverOfflineResetsKnownActiveNPCIDsButKeepsUnknown(t *testing.T) {
	known := npcCanonicalFixture()
	known.ActiveNPCIDs = []string{"npc-b", "npc-a"}
	next, disconnected, err := known.RecoverOffline()
	if err != nil || !reflect.DeepEqual(disconnected, []string{"player"}) {
		t.Fatalf("known recovery: disconnected=%v err=%v", disconnected, err)
	}
	if next.ActiveNPCIDs == nil || len(next.ActiveNPCIDs) != 0 {
		t.Fatalf("known active list was not reset to known empty: %#v", next.ActiveNPCIDs)
	}
	if !reflect.DeepEqual(known.ActiveNPCIDs, []string{"npc-b", "npc-a"}) {
		t.Fatal("recovery mutated source active list")
	}

	unknown := npcCanonicalFixture()
	next, _, err = unknown.RecoverOffline()
	if err != nil || next.ActiveNPCIDs != nil {
		t.Fatalf("unknown active list was invented during recovery: %#v err=%v", next.ActiveNPCIDs, err)
	}
}

func TestSavedPlayerEntryActivatesRoomNPCsOnlyForFirstOccupant(t *testing.T) {
	s := offlineFixture()
	s.NPCs = map[string]NPCState{
		"room-a": {Body: LegacyMonster{Name: "A", Type: 1, RoomID: 2}},
		"room-b": {Body: LegacyMonster{Name: "B", Type: 1, RoomID: 2}},
		"other":  {Body: LegacyMonster{Name: "Other", Type: 1, RoomID: 1}},
	}
	r := s.Rooms[2]
	r.NPCIDs = []string{"room-a", "room-b"}
	s.Rooms[2] = r
	r = s.Rooms[1]
	r.NPCIDs = []string{"other"}
	s.Rooms[1] = r
	// add_active removes then prepends each ID, so the room order is observed
	// in the calls and appears reversed at the front of the global list.
	s.ActiveNPCIDs = []string{"room-b", "other", "room-a"}

	next, _, err := s.EnterSavedPlayer("a", SceneOptions{}, nil, 1, nil)
	want := []string{"room-b", "room-a", "other"}
	if err != nil || !reflect.DeepEqual(next.ActiveNPCIDs, want) {
		t.Fatalf("first login active order: got %#v want %#v err=%v", next.ActiveNPCIDs, want, err)
	}
}

func TestSavedPlayerEntryActivatesOnlySpawnOrderForAdditionalOccupant(t *testing.T) {
	s := npcCanonicalFixture()
	s.Players["new"] = PlayerState{Body: LegacyMonster{Name: "New", Type: 0, RoomID: 1}}
	r := s.Rooms[1]
	r.Resource.PermanentMonsters[0] = LegacyTimer{Misc: 1}
	r.Resource.PermanentMonsters[1] = LegacyTimer{Misc: 1}
	s.Rooms[1] = r
	s.ActiveNPCIDs = []string{"npc-a", "npc-b"}
	spawnOrder := []string{"fresh-z", "fresh-a"}
	allocated := 0
	next, _, err := s.EnterSavedPlayerWithIDs("new", SceneOptions{}, npcSpawnCatalog{}, 1, func(int, int) int { return 1 }, func() (string, error) {
		id := spawnOrder[allocated]
		allocated++
		return id, nil
	})
	// The existing room occupants keep their active order. Only the two newly
	// created identities are add_active'd, in allocator/spawn order.
	want := []string{"fresh-a", "fresh-z", "npc-a", "npc-b"}
	if err != nil || allocated != len(spawnOrder) || !reflect.DeepEqual(next.ActiveNPCIDs, want) {
		t.Fatalf("additional login active order: got %#v want %#v allocations=%d err=%v", next.ActiveNPCIDs, want, allocated, err)
	}
}

func TestLeavePlayerDeactivatesRoomNPCsOnlyForLastOccupant(t *testing.T) {
	last := npcCanonicalFixture()
	last.NPCs["room-two"] = NPCState{Body: LegacyMonster{Name: "Room two", Type: 1, RoomID: 2}, Enemies: []NPCEnemy{}}
	r := last.Rooms[2]
	r.NPCIDs = []string{"room-two"}
	last.Rooms[2] = r
	last.ActiveNPCIDs = []string{"npc-b", "room-two", "npc-a"}
	next, departure, err := last.LeavePlayer("player")
	if err != nil || !departure.DeactivateMonsters || !reflect.DeepEqual(next.ActiveNPCIDs, []string{"room-two"}) {
		t.Fatalf("last departure active list: departure=%+v active=%#v err=%v", departure, next.ActiveNPCIDs, err)
	}

	nonLast := npcCanonicalFixture()
	for id, npc := range nonLast.NPCs {
		npc.Enemies = []NPCEnemy{}
		nonLast.NPCs[id] = npc
	}
	nonLast.Players["other"] = PlayerState{Body: LegacyMonster{Name: "Other", Type: 0, RoomID: 1}, Online: true}
	r = nonLast.Rooms[1]
	r.PlayerIDs = append(r.PlayerIDs, "other")
	nonLast.Rooms[1] = r
	nonLast.ActiveNPCIDs = []string{"npc-b", "npc-a"}
	next, departure, err = nonLast.LeavePlayer("player")
	if err != nil || departure.DeactivateMonsters || !reflect.DeepEqual(next.ActiveNPCIDs, nonLast.ActiveNPCIDs) {
		t.Fatalf("non-last departure changed active list: departure=%+v active=%#v err=%v", departure, next.ActiveNPCIDs, err)
	}
}

func TestDirectionalTransferDeactivatesSourceThenActivatesDestinationNPCs(t *testing.T) {
	s, in := npcMovementFixture(t)
	guard := s.NPCs["guard-id"]
	guard.Enemies = []NPCEnemy{}
	s.NPCs["guard-id"] = guard
	destination := s.Rooms[2]
	destination.NPCIDs = []string{"destination-a", "destination-b"}
	s.Rooms[2] = destination
	s.NPCs["destination-a"] = NPCState{Body: LegacyMonster{Name: "Destination A", Type: 1, RoomID: 2}}
	s.NPCs["destination-b"] = NPCState{Body: LegacyMonster{Name: "Destination B", Type: 1, RoomID: 2}}
	s.ActiveNPCIDs = []string{"guard-id"}

	next, proposal, err := s.DirectionalTransferWithIDs(in, nil, nil, nil)
	want := []string{"destination-b", "destination-a"}
	if err != nil || !proposal.Movement.Moved || !reflect.DeepEqual(next.ActiveNPCIDs, want) {
		t.Fatalf("movement active order: moved=%v active=%#v want=%#v err=%v", proposal.Movement.Moved, next.ActiveNPCIDs, want, err)
	}
}

func TestPlayerDeathDeactivatesSourceThenActivatesRespawnNPCs(t *testing.T) {
	s := playerDeathFixture()
	s.NPCs = map[string]NPCState{
		"source-a":  {Body: LegacyMonster{Name: "Source A", Type: 1, RoomID: 1}},
		"source-b":  {Body: LegacyMonster{Name: "Source B", Type: 1, RoomID: 1}},
		"respawn-a": {Body: LegacyMonster{Name: "Respawn A", Type: 1, RoomID: 1008}},
		"respawn-b": {Body: LegacyMonster{Name: "Respawn B", Type: 1, RoomID: 1008}},
	}
	r := s.Rooms[1]
	r.NPCIDs = []string{"source-a", "source-b"}
	s.Rooms[1] = r
	r = s.Rooms[1008]
	r.NPCIDs = []string{"respawn-a", "respawn-b"}
	s.Rooms[1008] = r
	s.ActiveNPCIDs = []string{"source-a", "source-b"}

	next, result, err := s.PlanPlayerDeath("a", "a", 100, SceneOptions{}, nil, func(lo, hi int) int { return lo }, nil)
	want := []string{"respawn-b", "respawn-a"}
	if err != nil || !result.DeactivateSourceMonsters || !reflect.DeepEqual(next.ActiveNPCIDs, want) {
		t.Fatalf("death active order: deactivated=%v active=%#v want=%#v err=%v", result.DeactivateSourceMonsters, next.ActiveNPCIDs, want, err)
	}
}
