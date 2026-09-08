package world

import (
	"reflect"
	"testing"
)

func TestLeavePlayerPreservesGameStateAndRemovesOccupancy(t *testing.T) {
	s := playerDeathFixture()
	next, departure, err := s.LeavePlayer("a")
	if err != nil || next.Players["a"].Online || len(next.Rooms[1].PlayerIDs) != 0 || !departure.DeactivateMonsters {
		t.Fatalf("%+v %v", departure, err)
	}
	want := s.Players["a"]
	want.Online = false
	if !reflect.DeepEqual(next.Players["a"], want) || !reflect.DeepEqual(next.Rooms[1].Items, s.Rooms[1].Items) || !s.Players["a"].Online {
		t.Fatal("game state changed")
	}
	next.Players["a"].Items.Items["changed"] = Item{}
	if len(s.Players["a"].Items.Items) != 1 {
		t.Fatal("aliased items")
	}
}

func TestLeavePlayerKeepsOtherOccupants(t *testing.T) {
	s := playerDeathFixture()
	s.Players["b"] = PlayerState{Body: LegacyMonster{Name: "Bob", RoomID: 1}, Online: true}
	r := s.Rooms[1]
	r.PlayerIDs = append(r.PlayerIDs, "b")
	s.Rooms[1] = r
	next, departure, err := s.LeavePlayer("a")
	if err != nil || departure.DeactivateMonsters || !reflect.DeepEqual(next.Rooms[1].PlayerIDs, []string{"b"}) {
		t.Fatalf("%+v %v", departure, err)
	}
	if got, _, err := next.LeavePlayer("a"); err == nil || !reflect.DeepEqual(got, State{}) {
		t.Fatal("repeated logout accepted without receipt")
	}
}

func TestLeavePlayerDetachesFollowerEdgesBeforeCommit(t *testing.T) {
	s, err := followFixture().FollowPlayer("one", "leader")
	if err != nil {
		t.Fatal(err)
	}
	next, _, err := s.LeavePlayer("leader")
	if err != nil || next.Players["leader"].FollowerIDs != nil || next.Players["one"].FollowingID != "" || !next.Players["one"].Online {
		t.Fatalf("next=%+v err=%v", next, err)
	}
}
