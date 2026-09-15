package world

import (
	"reflect"
	"testing"
)

func offlineFixture() State {
	s := stateFixture()
	p := s.Players["a"]
	p.Online = false
	p.Body.RoomID = 2
	s.Players["a"] = p
	r := s.Rooms[1]
	r.PlayerIDs = nil
	s.Rooms[1] = r
	return s
}

func TestSavedPlayerEntryAndDuplicate(t *testing.T) {
	s := offlineFixture()
	next, entry, err := s.EnterSavedPlayer("a", SceneOptions{}, nil, 1, nil)
	if err != nil || !next.Players["a"].Online || next.Players["a"].Body.RoomID != 2 || entry.Room.BeenHere != 1 || !reflect.DeepEqual(next.Rooms[2].PlayerIDs, []string{"a"}) {
		t.Fatalf("%+v %+v %v", next, entry, err)
	}
	if s.Players["a"].Online || s.Rooms[2].Resource.BeenHere != 0 {
		t.Fatal("mutated source")
	}
	if _, _, err = next.EnterSavedPlayer("a", SceneOptions{}, nil, 1, nil); err == nil {
		t.Fatal("duplicate login accepted")
	}
}

func TestSavedPlayerEntryRedirectsRestrictedRoom(t *testing.T) {
	for _, bit := range []uint{33, 40, 14} {
		s := offlineFixture()
		r := s.Rooms[2]
		r.Resource.Flags[bit/8] |= 1 << (bit % 8)
		r.Resource.Special = 7
		if bit == 14 {
			r.PlayerIDs = []string{"b"}
			s.Players["b"] = PlayerState{Body: LegacyMonster{Name: "Bob", RoomID: 2}, Online: true}
		}
		s.Rooms[2] = r
		next, entry, err := s.EnterSavedPlayer("a", SceneOptions{}, nil, 1, nil)
		if err != nil || entry.Room.ID != 1 || next.Players["a"].Body.RoomID != 1 {
			t.Fatalf("bit %d: %+v %v", bit, entry, err)
		}
	}
}

func TestSavedPlayerEntryFailureKeepsOffline(t *testing.T) {
	s := offlineFixture()
	r := s.Rooms[2]
	r.Resource.PermanentObjects[0] = LegacyTimer{Misc: 1}
	s.Rooms[2] = r
	next, entry, err := s.EnterSavedPlayer("a", SceneOptions{}, refreshCatalog{failObject: true}, 1, nil)
	if err == nil || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(entry, RoomEntry{}) || s.Players["a"].Online {
		t.Fatal("failed entry published")
	}
}

func TestSavedPlayerEntryDMInvisibleDoesNotFillRoom(t *testing.T) {
	s := offlineFixture()
	r := s.Rooms[2]
	r.Resource.Flags[1] |= 64
	r.PlayerIDs = []string{"b"}
	s.Rooms[2] = r
	s.Players["b"] = PlayerState{Body: LegacyMonster{Name: "Bob", RoomID: 2, Flags: [8]byte{0, 4}}, Online: true}
	_, entry, err := s.EnterSavedPlayer("a", SceneOptions{}, nil, 1, nil)
	if err != nil || entry.Room.ID != 2 {
		t.Fatalf("%+v %v", entry, err)
	}
}

func TestSavedPlayerEntryMissingFallbackDoesNotInventRoom(t *testing.T) {
	s := offlineFixture()
	delete(s.Rooms, 1)
	r := s.Rooms[2]
	r.Resource.Flags[4] |= 2
	s.Rooms[2] = r
	_, _, err := s.EnterSavedPlayer("a", SceneOptions{}, nil, 1, nil)
	if err == nil {
		t.Fatal("missing fallback accepted")
	}
}
