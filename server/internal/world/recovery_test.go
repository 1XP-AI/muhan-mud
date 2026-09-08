package world

import (
	"reflect"
	"testing"
)

func TestRecoverOfflinePreservesGameStateAndIsIdempotent(t *testing.T) {
	s := stateFixture()
	p := s.Players["a"]
	p.Body.Gold = 412
	p.Body.Inventory = []LegacyObject{{Name: "bag", Contents: []LegacyObject{{Name: "coin"}}}}
	s.Players["a"] = p
	next, disconnected, err := s.RecoverOffline()
	if err != nil || !reflect.DeepEqual(disconnected, []string{"a"}) || next.Players["a"].Online || len(next.Rooms[1].PlayerIDs) != 0 || !reflect.DeepEqual(next.Players["a"].Body, p.Body) {
		t.Fatalf("%+v %v %v", next, disconnected, err)
	}
	again, ids, err := next.RecoverOffline()
	if err != nil || len(ids) != 0 || !reflect.DeepEqual(again, next) {
		t.Fatal("recovery is not idempotent")
	}
	next.Players["a"].Body.Inventory[0].Contents[0].Name = "changed"
	if !s.Players["a"].Online || len(s.Rooms[1].PlayerIDs) != 1 || s.Players["a"].Body.Inventory[0].Contents[0].Name != "coin" {
		t.Fatal("recovery mutated original")
	}
}

func TestRecoverOfflineRejectsCorruptionInsteadOfRepairingIt(t *testing.T) {
	s := stateFixture()
	r := s.Rooms[2]
	r.PlayerIDs = []string{"a"}
	s.Rooms[2] = r
	next, ids, err := s.RecoverOffline()
	if err == nil || !reflect.DeepEqual(next, State{}) || ids != nil {
		t.Fatal("corrupt snapshot silently repaired")
	}
}

func TestRecoverOfflineOrdersDisconnectedIDs(t *testing.T) {
	s := stateFixture()
	s.Players["b"] = PlayerState{Body: LegacyMonster{Name: "Bob", RoomID: 2}, Online: true}
	r := s.Rooms[2]
	r.PlayerIDs = []string{"b"}
	s.Rooms[2] = r
	_, ids, err := s.RecoverOffline()
	if err != nil || !reflect.DeepEqual(ids, []string{"a", "b"}) {
		t.Fatalf("%v %v", ids, err)
	}
}
