package world

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestRoomItemsRoundTripAndRecovery(t *testing.T) {
	s := stateFixture()
	r := s.Rooms[1]
	r.Items = &ItemCollection{Items: map[string]Item{"bag": {Contents: []string{"gem"}}, "gem": {}}, Inventory: []string{"bag"}}
	s.Rooms[1] = r
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeState(raw)
	if err != nil {
		t.Fatal(err)
	}
	recovered, _, err := decoded.RecoverOffline()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(recovered.Rooms[1].Items, s.Rooms[1].Items) {
		t.Fatal("lost room items")
	}
	child := recovered.Rooms[1].Items.Items["bag"]
	child.Contents[0] = "changed"
	if decoded.Rooms[1].Items.Items["bag"].Contents[0] != "gem" {
		t.Fatal("aliased recovery")
	}
}

func TestRoomItemsRejectDuplicateOrInvalidOwnership(t *testing.T) {
	for _, kind := range []string{"player", "room", "raw", "equipped"} {
		s := stateFixture()
		r := s.Rooms[1]
		r.Items = &ItemCollection{Items: map[string]Item{"item": {}}, Inventory: []string{"item"}}
		switch kind {
		case "player":
			p := s.Players["a"]
			p.Items = r.Items
			s.Players["a"] = p
		case "room":
			other := s.Rooms[2]
			other.Items = r.Items
			s.Rooms[2] = other
		case "raw":
			r.Resource.Objects = []LegacyObject{{Name: "duplicate"}}
		case "equipped":
			r.Items.Inventory = nil
			r.Items.Ready[0] = "item"
		}
		s.Rooms[1] = r
		if s.Validate() == nil {
			t.Fatalf("accepted %s", kind)
		}
	}
}

func TestRoomItemsCannotBeDiscardedByLegacyEntry(t *testing.T) {
	s := stateFixture()
	r := s.Rooms[2]
	r.Items = &ItemCollection{Items: map[string]Item{"floor": {}}, Inventory: []string{"floor"}}
	s.Rooms[2] = r
	proposal, err := PlanTransfer(transferFixture(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.ApplyTransfer("a", proposal)
	if err == nil || !reflect.DeepEqual(got, State{}) {
		t.Fatal("legacy transfer discarded canonical items")
	}
	p := s.Players["a"]
	p.Online = false
	p.Body.RoomID = 2
	s.Players["a"] = p
	r = s.Rooms[1]
	r.PlayerIDs = nil
	s.Rooms[1] = r
	got, _, err = s.EnterSavedPlayer("a", SceneOptions{}, nil, 100, nil)
	if err != nil || !reflect.DeepEqual(got.Rooms[2].Items, s.Rooms[2].Items) {
		t.Fatalf("login lost canonical items: %v", err)
	}
}
