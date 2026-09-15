package world

import (
	"reflect"
	"testing"
)

func lethalDirectionFixture() (State, TransferInput) {
	s := playerDeathFixture()
	p := s.Players["a"]
	p.Body.HPCurrent = 5
	s.Players["a"] = p
	r := s.Rooms[1]
	r.Resource.Exits = []LegacyExit{{Name: "북", Destination: 2, Flags: [4]byte{0, 1}}}
	s.Rooms[1] = r
	dest := s.Rooms[2].Resource
	return s, TransferInput{ActorID: "a", Movement: MovementInput{Prefix: "북", Now: 100, Destination: &dest, Traversal: TraversalInput{Class: 4}, Visitor: DestinationVisitor{Class: 4, Level: 1}}}
}
func TestDirectionalStepFallDeathRespawnsAndDropsOnce(t *testing.T) {
	s, in := lethalDirectionFixture()
	calls := 0
	next, result, err := s.DirectionalStep(in, nil, func(low, high int) int { calls++; return low }, nil)
	if err != nil || result.Death == nil || !result.Transfer.Movement.Traversal.Dead || calls != 3 {
		t.Fatalf("%+v %v calls%d", result, err, calls)
	}
	p := next.Players["a"]
	if p.Body.RoomID != 1008 || p.Body.HPCurrent != 100 || len(p.Items.Items) != 0 || next.Rooms[1].Items.Items["weapon"].Object.Name != "sword" || len(next.Rooms[1].PlayerIDs) != 0 || len(next.Rooms[1008].PlayerIDs) != 1 || next.Rooms[2].Resource.BeenHere != s.Rooms[2].Resource.BeenHere {
		t.Fatal("partial fall death")
	}
	if s.Players["a"].Body.HPCurrent != 5 || len(s.Players["a"].Items.Items) != 1 {
		t.Fatal("input changed")
	}
}
func TestDirectionalStepDeathFailureReturnsNoPartialCandidate(t *testing.T) {
	s, in := lethalDirectionFixture()
	r := s.Rooms[1008]
	r.Resource.BeenHere = 2147483647
	s.Rooms[1008] = r
	next, result, err := s.DirectionalStep(in, nil, func(low, high int) int { return low }, nil)
	if err == nil || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(result, DirectionalStepResult{}) {
		t.Fatal("death error leaked movement candidate")
	}
}
