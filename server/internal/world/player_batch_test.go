package world

import (
	"reflect"
	"testing"
)

func TestPlayerBatchUpdatesAllInExplicitOrder(t *testing.T) {
	s := playerDeathFixture()
	a := s.Players["a"]
	a.Body.HPCurrent = 50
	s.Players["a"] = a
	b := a
	b.Body.Name = "Bob"
	b.Items = &ItemCollection{Items: map[string]Item{}}
	s.Players["b"] = b
	r := s.Rooms[1]
	r.PlayerIDs = append(r.PlayerIDs, "b")
	s.Rooms[1] = r
	next, results, err := s.UpdatePlayers([]PlayerUpdateInput{{ActorID: "b"}, {ActorID: "a"}}, 100, nil, nil, nil)
	if err != nil || len(results) != 2 || results[0].ActorID != "b" || results[1].ActorID != "a" || next.Players["a"].Body.HPCurrent <= 50 || next.Players["b"].Body.HPCurrent <= 50 {
		t.Fatalf("%+v %v", results, err)
	}
	if s.Players["a"].Body.HPCurrent != 50 {
		t.Fatal("input changed")
	}
}

func TestPlayerBatchRejectsIncompleteOrDuplicateOrder(t *testing.T) {
	s := playerDeathFixture()
	for _, order := range [][]PlayerUpdateInput{nil, {{ActorID: "a"}, {ActorID: "a"}}, {{ActorID: "missing"}}} {
		next, results, err := s.UpdatePlayers(order, 100, nil, func(int, int) int { t.Fatal("random before order validation"); return 1 }, nil)
		if err == nil || !reflect.DeepEqual(next, State{}) || results != nil {
			t.Fatal("invalid order accepted")
		}
	}
}

func TestPlayerBatchLateFailureDiscardsEarlierPlayer(t *testing.T) {
	s := playerDeathFixture()
	a := s.Players["a"]
	a.Body.HPCurrent = 50
	s.Players["a"] = a
	b := a
	b.Body.Name = "Bob"
	b.Body.Timers[28].Interval = 2147483647
	b.Items = &ItemCollection{Items: map[string]Item{}}
	s.Players["b"] = b
	r := s.Rooms[1]
	r.PlayerIDs = append(r.PlayerIDs, "b")
	s.Rooms[1] = r
	next, results, err := s.UpdatePlayers([]PlayerUpdateInput{{ActorID: "a"}, {ActorID: "b"}}, 100, nil, nil, nil)
	if err == nil || !reflect.DeepEqual(next, State{}) || results != nil || s.Players["a"].Body.HPCurrent != 50 {
		t.Fatal("partial batch")
	}
}

func TestPlayerBatchExcludesDMAsLegacySchedulerDoes(t *testing.T) {
	s := playerDeathFixture()
	p := s.Players["a"]
	p.Body.Class = 12
	s.Players["a"] = p
	next, results, err := s.UpdatePlayers(nil, 100, nil, nil, nil)
	if err != nil || len(results) != 0 || !reflect.DeepEqual(next, s) {
		t.Fatal("DM updated")
	}
	if _, _, err := s.UpdatePlayers([]PlayerUpdateInput{{ActorID: "a"}}, 100, nil, nil, nil); err == nil {
		t.Fatal("DM admitted to periodic batch")
	}
}

func TestPlayerUpdateOrderUsesRoomOrderThenStableIdentity(t *testing.T) {
	s := playerDeathFixture()
	a := s.Players["a"]
	a.Body.HPCurrent = 10
	s.Players["a"] = a
	for _, item := range []struct {
		id, name string
		room     int16
	}{{"b", "Bob", 1}, {"c", "Carol", 2}} {
		s.Players[item.id] = PlayerState{Body: LegacyMonster{Name: item.name, RoomID: item.room, Class: 4, HPCurrent: 10}, Online: true, Items: &ItemCollection{Items: map[string]Item{}}}
	}
	r := s.Rooms[1]
	r.PlayerIDs = []string{"b", "a"}
	s.Rooms[1] = r
	r = s.Rooms[2]
	r.PlayerIDs = []string{"c"}
	s.Rooms[2] = r
	order, err := s.PlayerUpdateOrder(12)
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{order[0].ActorID, order[1].ActorID, order[2].ActorID}; !reflect.DeepEqual(got, []string{"b", "a", "c"}) {
		t.Fatalf("order=%v", got)
	}
	for _, input := range order {
		if input.View.Hour != 12 || input.View.ViewerID != input.ActorID {
			t.Fatalf("input=%+v", input)
		}
	}
}

func TestPlayerUpdateOrderRejectsUnmigratedInventory(t *testing.T) {
	s := playerDeathFixture()
	p := s.Players["a"]
	p.Items = nil
	s.Players["a"] = p
	if _, err := s.PlayerUpdateOrder(12); err == nil {
		t.Fatal("unmigrated inventory entered player phase")
	}
}
