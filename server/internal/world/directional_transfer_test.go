package world

import (
	"reflect"
	"strings"
	"testing"
)

func TestDirectionalTransferAppliesMembershipAndPreservesFloorIDs(t *testing.T) {
	s, in := canonicalTransferFixture()
	r := s.Rooms[1]
	r.Resource.Exits = []LegacyExit{{Name: "북문", Destination: 3}, {Name: "북", Destination: 2}}
	s.Rooms[1] = r
	in.Movement.Prefix = "8"
	in.Movement.AttackReadyAt = in.Movement.Now + 99
	next, p, err := s.DirectionalTransferWithIDs(in, nil, nil, nil)
	if err != nil || !p.Movement.Moved || p.Entry == nil || !strings.Contains(p.Entry.Scene, "검") || next.Players["a"].Body.RoomID != 2 || next.Rooms[1].Resource.Track != "북" {
		t.Fatalf("%+v %v", p, err)
	}
	for _, id := range next.Rooms[1].PlayerIDs {
		if id == "a" {
			t.Fatal("source retained actor")
		}
	}
	found := false
	for _, id := range next.Rooms[2].PlayerIDs {
		found = found || id == "a"
	}
	if !found {
		t.Fatal("destination missing actor")
	}
	for _, id := range []int16{1, 2} {
		if !reflect.DeepEqual(next.Rooms[id].Items, s.Rooms[id].Items) {
			t.Fatal("floor IDs changed")
		}
	}
	if s.Players["a"].Body.RoomID != 1 {
		t.Fatal("input mutated")
	}
}
func TestDirectionalTransferDeniedKeepsDestinationUnchanged(t *testing.T) {
	s, in := canonicalTransferFixture()
	r := s.Rooms[1]
	r.Resource.Exits = []LegacyExit{{Name: "북", Destination: 2}}
	s.Rooms[1] = r
	in.Movement.Prefix = "북"
	r = s.Rooms[2]
	r.Resource.LowLevel = 99
	r.Resource.PermanentObjects[0] = LegacyTimer{Misc: 2}
	s.Rooms[2] = r
	next, p, err := s.DirectionalTransferWithIDs(in, refreshCatalog{}, nil, func() (string, error) { t.Fatal("denied move spawned item"); return "", nil })
	if err != nil || p.Movement.Moved || next.Players["a"].Body.RoomID != 1 || next.Rooms[1].Resource.Track != "북" || !reflect.DeepEqual(next.Rooms[2], s.Rooms[2]) {
		t.Fatalf("%+v %v", p, err)
	}
}
