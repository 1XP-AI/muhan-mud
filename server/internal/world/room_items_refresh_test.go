package world

import (
	"reflect"
	"strings"
	"testing"
)

func TestCanonicalRoomSpawnIDsOnlyForNewObjects(t *testing.T) {
	room := RoomState{Resource: LegacyRoom{PermanentObjects: [10]LegacyTimer{{Misc: 2}}}, Items: &ItemCollection{Items: map[string]Item{"old": {Object: LegacyObject{Name: "bag"}, Contents: []string{"gem"}}, "gem": {}}, Inventory: []string{"old"}}}
	calls := 0
	allocate := func() (string, error) { calls++; return "new", nil }
	got, err := RefreshCanonicalRoom(room, refreshCatalog{}, 10, nil, allocate)
	if err != nil || calls != 1 || len(got.Items.Items) != 3 || len(got.Resource.Objects) != 0 {
		t.Fatalf("%+v %v calls%d", got, err, calls)
	}
	again, err := RefreshCanonicalRoom(got, refreshCatalog{}, 11, nil, allocate)
	if err != nil || calls != 1 || !reflect.DeepEqual(got.Items, again.Items) {
		t.Fatalf("duplicate allocation %v %d", err, calls)
	}
	if len(room.Items.Items) != 2 {
		t.Fatal("mutated input")
	}
}

func TestCanonicalRoomRejectsAllocatedCollision(t *testing.T) {
	room := RoomState{Resource: LegacyRoom{PermanentObjects: [10]LegacyTimer{{Misc: 2}}}, Items: &ItemCollection{Items: map[string]Item{"old": {}}, Inventory: []string{"old"}}}
	for _, allocate := range []func() (string, error){nil, func() (string, error) { return "old", nil }} {
		got, err := RefreshCanonicalRoom(room, refreshCatalog{}, 10, nil, allocate)
		if err == nil || !reflect.DeepEqual(got, RoomState{}) {
			t.Fatal("partial or conflicting floor")
		}
	}
}

func TestCanonicalRoomEntryDisplaysFloor(t *testing.T) {
	room := RoomState{Items: &ItemCollection{Items: map[string]Item{"sword": {Object: LegacyObject{Name: "검"}}}, Inventory: []string{"sword"}}}
	next, entry, err := PlanCanonicalRoomEntry(room, nil, RoomPlayerView{ID: "a", Name: "Alice"}, SceneOptions{}, nil, 10, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(entry.Scene, "검") || next.Resource.BeenHere != 1 || len(next.Items.Items) != 1 || len(next.Resource.Objects) != 0 {
		t.Fatalf("%+v %+v", next, entry)
	}
}

func TestCanonicalLoginChecksWorldWideAllocatedIDs(t *testing.T) {
	s := stateFixture()
	r := s.Rooms[1]
	r.PlayerIDs = nil
	r.Items = &ItemCollection{Items: map[string]Item{}}
	r.Resource.PermanentObjects[0] = LegacyTimer{Misc: 2}
	s.Rooms[1] = r
	p := s.Players["a"]
	p.Online = false
	p.Body.Level = 1
	p.Body.Class = 4
	p.Body.Stats = [5]byte{10, 10, 10, 10, 10}
	p.Items = &ItemCollection{Items: map[string]Item{"taken": {}}, Inventory: []string{"taken"}}
	s.Players["a"] = p
	for _, id := range []string{"taken", "fresh"} {
		next, _, err := s.EnterSavedPlayerWithIDs("a", SceneOptions{}, refreshCatalog{}, 10, nil, func() (string, error) { return id, nil })
		if id == "taken" {
			if err == nil || !reflect.DeepEqual(next, State{}) {
				t.Fatal("accepted cross-owner collision")
			}
		} else if err != nil || len(next.Rooms[1].Items.Items) != 1 || !next.Players["a"].Online {
			t.Fatalf("login %+v %v", next, err)
		}
	}
	if len(s.Rooms[1].Items.Items) != 0 || s.Players["a"].Online {
		t.Fatal("input changed")
	}
}
