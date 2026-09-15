package world

import (
	"reflect"
	"strings"
	"testing"
)

func canonicalTransferFixture() (State, TransferInput) {
	s := stateFixture()
	in := transferFixture()
	r := s.Rooms[1]
	r.Resource = in.Movement.Source
	r.Items = &ItemCollection{Items: map[string]Item{"source": {Object: LegacyObject{Name: "bag"}}}, Inventory: []string{"source"}}
	s.Rooms[1] = r
	r = s.Rooms[2]
	r.Resource = *in.Movement.Destination
	r.Items = &ItemCollection{Items: map[string]Item{"floor": {Object: LegacyObject{Name: "검"}}}, Inventory: []string{"floor"}}
	s.Rooms[2] = r
	return s, in
}

func TestCanonicalTransferPreservesFloorAndDisplaysDestination(t *testing.T) {
	s, in := canonicalTransferFixture()
	next, p, err := s.TransferWithIDs(in, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Movement.Moved || !strings.Contains(p.Entry.Scene, "검") || next.Players["a"].Body.RoomID != 2 {
		t.Fatalf("%+v", p)
	}
	for _, id := range []int16{1, 2} {
		if !reflect.DeepEqual(next.Rooms[id].Items, s.Rooms[id].Items) {
			t.Fatal("floor changed")
		}
	}
	next.Rooms[1].Items.Items["changed"] = Item{}
	if len(s.Rooms[1].Items.Items) != 1 {
		t.Fatal("floor alias")
	}
}

func TestCanonicalTransferDeniedDoesNotSpawn(t *testing.T) {
	s, in := canonicalTransferFixture()
	r := s.Rooms[2]
	r.Resource.LowLevel = 10
	r.Resource.PermanentObjects[0] = LegacyTimer{Misc: 2}
	s.Rooms[2] = r
	next, p, err := s.TransferWithIDs(in, refreshCatalog{}, nil, func() (string, error) { t.Fatal("allocated on denied move"); return "", nil })
	if err != nil || p.Movement.Moved || next.Players["a"].Body.RoomID != 1 || !reflect.DeepEqual(next.Rooms[2], s.Rooms[2]) {
		t.Fatalf("%+v %v", p, err)
	}
}

func TestCanonicalTransferSpawnCollisionAbortsEverything(t *testing.T) {
	s, in := canonicalTransferFixture()
	r := s.Rooms[2]
	r.Resource.PermanentObjects[0] = LegacyTimer{Misc: 2}
	s.Rooms[2] = r
	for _, id := range []string{"source", "spawned"} {
		next, p, err := s.TransferWithIDs(in, refreshCatalog{}, nil, func() (string, error) { return id, nil })
		if id == "source" {
			if err == nil || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(p, TransferProposal{}) {
				t.Fatal("partial collision")
			}
		} else if err != nil || len(next.Rooms[2].Items.Items) != 2 {
			t.Fatalf("%+v %v", p, err)
		}
	}
	if s.Players["a"].Body.RoomID != 1 || s.Rooms[1].Resource.Track != "" {
		t.Fatal("changed input")
	}
}

func TestCanonicalTransferUsesStoredMovementEquipment(t *testing.T) {
	for _, directional := range []bool{false, true} {
		s, in := canonicalTransferFixture()
		actor := s.Players[in.ActorID]
		actor.Body.Stats[1] = 14
		actor.Items = &ItemCollection{Items: map[string]Item{"pack": {Object: LegacyObject{Weight: 9}}}, Inventory: []string{"pack"}}
		s.Players[in.ActorID] = actor
		room := s.Rooms[1]
		room.Resource.Exits[0].Flags[0] |= 128 // XNAKED
		s.Rooms[1] = room
		in.Movement.Prefix = room.Resource.Exits[0].Name
		in.Movement.Traversal.CarriedWeight = 0 // stale/untrusted projection
		move := s.TransferWithIDs
		if directional {
			move = s.DirectionalTransferWithIDs
		}
		next, proposal, err := move(in, nil, nil, nil)
		if err != nil || proposal.Movement.Moved || next.Players[in.ActorID].Body.RoomID != 1 {
			t.Fatalf("directional=%v bypassed stored weight: %+v %v", directional, proposal, err)
		}
		actor.Body.Stats[1] = 64
		s.Players[in.ActorID] = actor
		move = s.TransferWithIDs
		if directional {
			move = s.DirectionalTransferWithIDs
		}
		next, proposal, err = move(in, nil, nil, nil)
		if err == nil || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(proposal, TransferProposal{}) {
			t.Fatal("invalid stored equipment stats exposed partial candidate")
		}
	}
}

func TestCanonicalTransferUsesStoredFallSkill(t *testing.T) {
	s, in := canonicalTransferFixture()
	actor := s.Players[in.ActorID]
	actor.Body.Stats[1] = 14 // +5 fall skill
	actor.Items = &ItemCollection{Items: map[string]Item{"rope": {Object: LegacyObject{DicePlus: 10, Flags: [8]byte{0, 0, 1}}}}, Ready: [20]string{"rope"}}
	s.Players[in.ActorID] = actor
	room := s.Rooms[1]
	room.Resource.Exits[0].Flags[1] |= 1 // XCLIMB
	s.Rooms[1] = room
	in.Movement.Prefix = room.Resource.Exits[0].Name
	in.Movement.Traversal.FallSkill = -1000
	calls := 0
	next, proposal, err := s.DirectionalTransferWithIDs(in, nil, func(low, high int) int {
		calls++
		if low != 1 || high != 100 {
			t.Fatalf("unexpected damage roll %d..%d", low, high)
		}
		return 20 // stored fall chance 15: succeeds; supplied projection would fall
	}, nil)
	if err != nil || !proposal.Movement.Moved || proposal.Movement.Traversal.Fell || calls != 1 || next.Players[in.ActorID].Body.HPCurrent != actor.Body.HPCurrent {
		t.Fatalf("stored climbing equipment not applied: %+v %v", proposal, err)
	}
}

func TestCanonicalTransferUsesStoredDexterityForStealth(t *testing.T) {
	s, in := canonicalTransferFixture()
	actor := s.Players[in.ActorID]
	actor.Body.Class, actor.Body.Level = 1, 1
	actor.Body.Stats[1] = 14
	actor.Body.Flags[0] |= 2
	actor.Items = &ItemCollection{Items: map[string]Item{}}
	s.Players[in.ActorID] = actor
	in.Movement.Traversal.Class = 1
	in.Movement.Visitor.Class = 1
	in.Movement.Visitor.Level = 1
	in.Movement.DexterityBonus = 1000 // stored bonus=1 gives a 14% chance
	calls := 0
	next, proposal, err := s.DirectionalTransferWithIDs(in, nil, func(low, high int) int {
		calls++
		if low != 1 || high != 100 {
			t.Fatalf("unexpected stealth roll %d..%d", low, high)
		}
		return 15
	}, nil)
	result := next.Players[in.ActorID].Body
	if err != nil || !proposal.Movement.Moved || proposal.Movement.Hidden || calls != 1 || flag(result.Flags[:], 1) {
		t.Fatalf("stored dexterity not applied: %+v %v", proposal, err)
	}
}
