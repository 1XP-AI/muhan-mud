package world

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestInvitationsValidateAndClone(t *testing.T) {
	s := stateFixture()
	s.Invitations = map[int16][]string{7: {"a"}}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeState(raw)
	if err != nil || !reflect.DeepEqual(got.Invitations, s.Invitations) {
		t.Fatalf("roundtrip %v", err)
	}
	copy := got.clone()
	copy.Invitations[7][0] = "changed"
	if got.Invitations[7][0] != "a" {
		t.Fatal("invitation slice aliases")
	}
	for _, ids := range [][]string{{"missing"}, {"a", "a"}, {""}, make([]string, 11)} {
		got.Invitations[7] = ids
		if got.Validate() == nil {
			t.Fatalf("accepted invalid invite IDs %v", ids)
		}
	}
	for _, entries := range []map[int16][]string{nil, {}} {
		s.Invitations = entries
		raw, _ = json.Marshal(s)
		got, err = DecodeState(raw)
		if err != nil || (got.Invitations == nil) != (entries == nil) {
			t.Fatal("unknown/known-empty lost")
		}
	}
}

func TestTransferUsesDestinationInvitationIDs(t *testing.T) {
	for _, directional := range []bool{false, true} {
		for _, invited := range []bool{false, true} {
			s, in := canonicalTransferFixture()
			p := s.Players[in.ActorID]
			p.Body.Class = 4
			s.Players[in.ActorID] = p
			dst := s.Rooms[2]
			dst.Resource.Flags[5] |= 1 // RONMAR
			dst.Resource.Special = 7
			s.Rooms[2] = dst
			s.Invitations = map[int16][]string{8: {in.ActorID}} // another property never grants entry
			if invited {
				s.Invitations[7] = []string{in.ActorID}
			}
			in.Movement.Visitor.Invited = !invited
			move := s.TransferWithIDs
			if directional {
				move = s.DirectionalTransferWithIDs
			}
			next, result, err := move(in, nil, nil, nil)
			if err != nil || result.Movement.Moved != invited {
				t.Fatalf("directional%v invited%v: %+v %v", directional, invited, result.Movement, err)
			}
			next.Invitations[8][0] = "changed"
			if s.Invitations[8][0] != in.ActorID {
				t.Fatal("move mutated invitation authority")
			}
		}
	}
}

func TestImportInvitationsResolvesExactNamesOnce(t *testing.T) {
	s := stateFixture()
	next, err := s.ImportInvitations(map[int16][]string{7: {"Alice", "Alice"}})
	if err != nil || !reflect.DeepEqual(next.Invitations[7], []string{"a"}) || s.Invitations != nil {
		t.Fatalf("import %+v %v", next.Invitations, err)
	}
	p := next.Players["a"]
	p.Body.Name = "Renamed"
	next.Players["a"] = p
	if err := next.Validate(); err != nil || next.Invitations[7][0] != "a" {
		t.Fatal("invitation lost on rename")
	}
	if _, err := next.ImportInvitations(map[int16][]string{}); err == nil {
		t.Fatal("reimport overwrote authoritative invitations")
	}
	for _, source := range []map[int16][]string{nil, {7: {"alice"}}, {7: {"missing"}}, {7: {""}}, {7: make([]string, 11)}} {
		got, err := s.ImportInvitations(source)
		if err == nil || !reflect.DeepEqual(got, State{}) {
			t.Fatalf("partial/unresolved import %v", source)
		}
	}
	p = s.Players["a"]
	p.Online = false
	s.Players["ambiguous"] = p
	if _, err := s.ImportInvitations(map[int16][]string{7: {"Alice"}}); err == nil {
		t.Fatal("ambiguous name imported")
	}
}
