package world

import (
	"reflect"
	"testing"
)

func followFixture() State {
	s := State{Version: 1, Rooms: map[int16]RoomState{
		1: {Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}}, PlayerIDs: []string{"leader", "one", "two"}},
	}, Players: map[string]PlayerState{
		"leader": {Body: LegacyMonster{Name: "Leader", RoomID: 1}, Online: true},
		"one":    {Body: LegacyMonster{Name: "One", RoomID: 1}, Online: true},
		"two":    {Body: LegacyMonster{Name: "Two", RoomID: 1}, Online: true},
	}}
	return s
}

func TestFollowPlayerPrependsAndSwitchesOneLeader(t *testing.T) {
	s := followFixture()
	first, err := s.FollowPlayer("one", "leader")
	if err != nil || !reflect.DeepEqual(first.Players["leader"].FollowerIDs, []string{"one"}) || first.Players["one"].FollowingID != "leader" {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := first.FollowPlayer("two", "leader")
	if err != nil || !reflect.DeepEqual(second.Players["leader"].FollowerIDs, []string{"two", "one"}) {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	third, err := second.FollowPlayer("one", "two")
	if err != nil || !reflect.DeepEqual(third.Players["leader"].FollowerIDs, []string{"two"}) || !reflect.DeepEqual(third.Players["two"].FollowerIDs, []string{"one"}) || third.Players["one"].FollowingID != "two" {
		t.Fatalf("switch=%+v err=%v", third, err)
	}
	if s.Players["leader"].FollowerIDs != nil || s.Players["one"].FollowingID != "" {
		t.Fatal("input mutated")
	}
}

func TestFollowPlayerRejectsSelfCycleAndDifferentRoomAtomically(t *testing.T) {
	s := followFixture()
	for _, mutate := range []func(State) (State, error){
		func(in State) (State, error) { return in.FollowPlayer("one", "one") },
		func(in State) (State, error) {
			next, err := in.FollowPlayer("one", "leader")
			if err != nil {
				return State{}, err
			}
			return next.FollowPlayer("leader", "one")
		},
		func(in State) (State, error) {
			p := in.Players["two"]
			p.Body.RoomID = 2
			in.Players["two"] = p
			return in.FollowPlayer("two", "leader")
		},
	} {
		if got, err := mutate(s); err == nil || !reflect.DeepEqual(got, State{}) {
			t.Fatalf("accepted invalid follow got=%+v err=%v", got, err)
		}
	}
}

func TestUnfollowAndDetachPreserveRemainingOrder(t *testing.T) {
	s, err := followFixture().FollowPlayer("one", "leader")
	if err != nil {
		t.Fatal(err)
	}
	s, err = s.FollowPlayer("two", "leader")
	if err != nil {
		t.Fatal(err)
	}
	s, err = s.UnfollowPlayer("two")
	if err != nil || !reflect.DeepEqual(s.Players["leader"].FollowerIDs, []string{"one"}) || s.Players["two"].FollowingID != "" {
		t.Fatalf("unfollow=%+v err=%v", s, err)
	}
	s, err = s.DetachPlayerRelationships("leader")
	if err != nil || s.Players["leader"].FollowerIDs != nil || s.Players["one"].FollowingID != "" {
		t.Fatalf("detach=%+v err=%v", s, err)
	}
}

func TestDetachPlayerRelationshipsClearsMDMFOLFlagsAndEdges(t *testing.T) {
	s := resolveDMFollowLogoutDomain(dmFollowFixture(t))
	proposal, err := s.PlanDMFollow("dm", "*따르기", "늑대", 1)
	if err != nil {
		t.Fatal(err)
	}
	attached, _, err := s.ApplyDMFollow(proposal)
	if err != nil {
		t.Fatal(err)
	}
	next, err := attached.DetachPlayerRelationships("dm")
	if err != nil {
		t.Fatal(err)
	}
	npc := next.NPCs["wolf-1"]
	dm := next.Players["dm"]
	if npc.FollowingPlayerID != "" || PlayerFlagSet(npc.Body, npcDMFollowFlag) || dm.NPCFollowerIDs != nil || dm.FollowerRefs != nil || !dm.Online {
		t.Fatalf("detach npc=%+v dm=%+v", npc, dm)
	}
	if attached.NPCs["wolf-1"].FollowingPlayerID != "dm" || !PlayerFlagSet(attached.NPCs["wolf-1"].Body, npcDMFollowFlag) {
		t.Fatal("DetachPlayerRelationships mutated the attached snapshot")
	}
}
