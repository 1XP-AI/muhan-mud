package world

import (
	"reflect"
	"testing"
)

func followerMovementFixture(t *testing.T) (State, TransferInput) {
	t.Helper()
	s, in := canonicalTransferFixture()
	s.Players["b"] = PlayerState{Body: LegacyMonster{Name: "Bob", RoomID: 1, Class: 4, Level: 1, HPCurrent: 30, Stats: [5]byte{10, 10, 10, 10, 10}}, Online: true, Items: &ItemCollection{Items: map[string]Item{}}}
	r := s.Rooms[1]
	r.PlayerIDs = append(r.PlayerIDs, "b")
	s.Rooms[1] = r
	var err error
	s, err = s.FollowPlayer("b", "a")
	if err != nil {
		t.Fatal(err)
	}
	return s, in
}

func TestDirectionalStepMovesFollowersInCHeadOrder(t *testing.T) {
	s, in := followerMovementFixture(t)
	next, result, err := s.DirectionalStep(in, nil, nil, nil)
	if err != nil || len(result.Followers) != 1 || result.Followers[0].ActorID != "b" || !result.Followers[0].Transfer.Movement.Moved {
		t.Fatalf("next=%+v result=%+v err=%v", next, result, err)
	}
	if next.Players["a"].Body.RoomID != 2 || next.Players["b"].Body.RoomID != 2 || len(next.Rooms[1].PlayerIDs) != 0 || !reflect.DeepEqual(next.Rooms[2].PlayerIDs, []string{"a", "b"}) {
		t.Fatalf("membership=%+v players=%+v", next.Rooms, next.Players)
	}
}

func TestDirectionalStepRechecksCapacityForFollowersAfterLeader(t *testing.T) {
	s, in := followerMovementFixture(t)
	r := s.Rooms[2]
	r.Resource.Flags[14/8] |= 1 << (14 % 8) // RONEPL
	s.Rooms[2] = r
	next, result, err := s.DirectionalStep(in, nil, nil, nil)
	if err != nil || len(result.Followers) != 1 || result.Followers[0].Transfer.Movement.Moved || next.Players["a"].Body.RoomID != 2 || next.Players["b"].Body.RoomID != 1 {
		t.Fatalf("next=%+v result=%+v err=%v", next, result, err)
	}
	if !reflect.DeepEqual(next.Rooms[1].PlayerIDs, []string{"b"}) || !reflect.DeepEqual(next.Rooms[2].PlayerIDs, []string{"a"}) {
		t.Fatalf("capacity membership=%+v", next.Rooms)
	}
}

func TestDirectionalStepMovesNestedFollowersDepthFirst(t *testing.T) {
	s, in := followerMovementFixture(t)
	s.Players["c"] = PlayerState{Body: LegacyMonster{Name: "Carol", RoomID: 1, Class: 4, Level: 1, HPCurrent: 30, Stats: [5]byte{10, 10, 10, 10, 10}}, Online: true, Items: &ItemCollection{Items: map[string]Item{}}}
	r := s.Rooms[1]
	r.PlayerIDs = append(r.PlayerIDs, "c")
	s.Rooms[1] = r
	var err error
	s, err = s.FollowPlayer("c", "b")
	if err != nil {
		t.Fatal(err)
	}
	next, result, err := s.DirectionalStep(in, nil, nil, nil)
	if err != nil || len(result.Followers) != 1 || len(result.Followers[0].Followers) != 1 || result.Followers[0].Followers[0].ActorID != "c" {
		t.Fatalf("next=%+v result=%+v err=%v", next, result, err)
	}
	if next.Players["a"].Body.RoomID != 2 || next.Players["b"].Body.RoomID != 2 || next.Players["c"].Body.RoomID != 2 {
		t.Fatalf("nested follower rooms: %+v", next.Players)
	}
}

func TestDirectionalStepRunsFollowerTrapBeforeLeaderTrap(t *testing.T) {
	s, in := followerMovementFixture(t)
	r := s.Rooms[2]
	r.Resource.Trap = TrapDart
	s.Rooms[2] = r
	leader := s.Players["a"]
	leader.Body.Stats[1] = 1
	s.Players["a"] = leader
	follower := s.Players["b"]
	follower.Body.Stats[1] = 1
	s.Players["b"] = follower
	rolls := []int{100, 3, 100, 5}
	next, result, err := s.DirectionalStep(in, nil, func(low, high int) int {
		if low != 1 || (high != 100 && high != 10) {
			t.Fatalf("unexpected roll %d..%d", low, high)
		}
		value := rolls[0]
		rolls = rolls[1:]
		return value
	}, nil)
	if err != nil || len(rolls) != 0 || len(result.Followers) != 1 || result.Followers[0].Transfer.ArrivalTrap == nil || result.Transfer.ArrivalTrap == nil {
		t.Fatalf("next=%+v result=%+v rolls=%v err=%v", next, result, rolls, err)
	}
	if next.Players["b"].Body.HPCurrent != 27 || next.Players["a"].Body.HPCurrent != 25 {
		t.Fatalf("trap order/effect players=%+v", next.Players)
	}
}
