package world

import (
	"reflect"
	"strings"
	"testing"
)

func propertyInviteFixture(t *testing.T) State {
	t.Helper()
	var roomFlags [8]byte
	roomFlags[propertyInviteRoomFlag/8] |= 1 << (propertyInviteRoomFlag % 8)
	s := State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Name: "앨리스의 집", Special: 7, Flags: roomFlags}},
				PlayerIDs: []string{"actor", "bob", "cara"},
			},
		},
		Players: map[string]PlayerState{
			"actor": {Body: LegacyMonster{Name: "Alice", Type: 0, Class: 4, RoomID: 1, Daily: [10]LegacyDaily{{}, {}, {}, {}, {}, {}, {}, {}, {Max: 7}, {}}}, Online: true},
			"bob":   {Body: LegacyMonster{Name: "Bob", Type: 0, Class: 4, RoomID: 1}, Online: true},
			"cara":  {Body: LegacyMonster{Name: "Cara", Type: 0, Class: 4, RoomID: 1}, Online: true},
		},
		Invitations: map[int16][]string{7: nil},
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	return s
}

func propertyInviteFlag(body *LegacyMonster, bit uint) {
	body.Flags[bit/8] |= 1 << (bit % 8)
}

func TestPropertyInviteAddRemoveAndListPreserveCanonicalOrder(t *testing.T) {
	s := propertyInviteFixture(t)
	original := s.clone()

	proposal, err := s.PlanPropertyInvite("actor", "Bob")
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Action != PropertyInviteAdd || proposal.TargetID != "bob" || proposal.Target != "bob" || proposal.TargetName != "Bob" || proposal.Property != 7 || proposal.PropertyID != 7 || proposal.Changed != true {
		t.Fatalf("proposal=%+v", proposal)
	}
	if !reflect.DeepEqual(s, original) {
		t.Fatal("planning mutated state")
	}

	next, result, err := s.ApplyPropertyInvite(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.Invitations, map[int16][]string{7: nil}) {
		t.Fatalf("source invitations mutated: %+v", s.Invitations)
	}
	if result.Action != PropertyInviteAdd || !result.Changed || result.TargetID != "bob" || result.Target != "bob" || result.TargetName != "Bob" || result.Property != 7 || result.PropertyID != 7 || !reflect.DeepEqual(result.OrderedInviteIDs, []string{"bob"}) || !reflect.DeepEqual(result.InviteIDs, []string{"bob"}) || result.Response != "초대 대상에 추가하였습니다.\r\n" {
		t.Fatalf("result=%+v", result)
	}
	if !reflect.DeepEqual(next.Invitations[7], []string{"bob"}) {
		t.Fatalf("next invitations=%+v", next.Invitations)
	}

	listed, err := next.ListPropertyInvitations("actor")
	if err != nil {
		t.Fatal(err)
	}
	if listed.Action != PropertyInviteList || listed.Changed || listed.Response != "당신이 초대한 사람들 : \r\nBob\r\n" || !reflect.DeepEqual(listed.OrderedInviteIDs, []string{"bob"}) || !reflect.DeepEqual(listed.InviteNames, []string{"Bob"}) {
		t.Fatalf("listed=%+v", listed)
	}

	remove, err := next.PlanPropertyInvite("actor", "Bob")
	if err != nil {
		t.Fatal(err)
	}
	if remove.Action != PropertyInviteRemove {
		t.Fatalf("remove proposal=%+v", remove)
	}
	empty, removed, err := next.ApplyPropertyInvite(remove)
	if err != nil {
		t.Fatal(err)
	}
	if removed.Action != PropertyInviteRemove || !removed.Changed || !reflect.DeepEqual(removed.OrderedInviteIDs, []string{}) || len(empty.Invitations) != 0 {
		t.Fatalf("removed=%+v invitations=%+v", removed, empty.Invitations)
	}
	if err := empty.Validate(); err != nil {
		t.Fatalf("empty invitation map no longer validates: %v", err)
	}
	listed, err = empty.ListPropertyInvitations("actor")
	if err != nil || listed.Response != "초대한 사람이 없습니다.\r\n" || !reflect.DeepEqual(listed.OrderedInviteIDs, []string{}) {
		t.Fatalf("empty list=%+v err=%v", listed, err)
	}
}

func TestPropertyInviteListPreservesStoredIDOrderAndOfflineNames(t *testing.T) {
	s := propertyInviteFixture(t)
	offline := PlayerState{Body: LegacyMonster{Name: "Offline", Type: 0, RoomID: 1}, Online: false}
	s.Players["offline"] = offline
	s.Invitations[7] = []string{"cara", "offline", "bob"}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListPropertyInvitations("actor")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.OrderedInviteIDs, []string{"cara", "offline", "bob"}) || !reflect.DeepEqual(got.InviteNames, []string{"Cara", "Offline", "Bob"}) {
		t.Fatalf("list order ids=%v names=%v", got.OrderedInviteIDs, got.InviteNames)
	}
	if got.Response != "당신이 초대한 사람들 : \r\nCara\r\nOffline\r\nBob\r\n" {
		t.Fatalf("response=%q", got.Response)
	}
}

func TestPropertyInviteRejectsUnmigratedAndUnauthorizedContexts(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*State)
	}{
		{name: "unmigrated invitations", mutate: func(s *State) { s.Invitations = nil }},
		{name: "not in property room", mutate: func(s *State) {
			r := s.Rooms[1]
			r.Resource.Flags = [8]byte{}
			s.Rooms[1] = r
		}},
		{name: "no property id", mutate: func(s *State) {
			p := s.Players["actor"]
			p.Body.Daily[8].Max = 0
			s.Players["actor"] = p
		}},
		{name: "actor missing", mutate: func(s *State) {
			delete(s.Players, "actor")
			s.Rooms[1] = RoomState{Resource: s.Rooms[1].Resource, PlayerIDs: []string{"bob", "cara"}}
		}},
		{name: "actor offline", mutate: func(s *State) {
			p := s.Players["actor"]
			p.Online = false
			s.Players["actor"] = p
			r := s.Rooms[1]
			r.PlayerIDs = []string{"bob", "cara"}
			s.Rooms[1] = r
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := propertyInviteFixture(t)
			tc.mutate(&s)
			if _, err := s.PlanPropertyInvite("actor", "Bob"); err == nil {
				t.Fatal("accepted unauthorized context")
			}
			if _, err := s.ListPropertyInvitations("actor"); err == nil {
				t.Fatal("listed unauthorized context")
			}
		})
	}
}

func TestPropertyInviteRejectsExactLookupAmbiguitySelfInvisibleAndBadNames(t *testing.T) {
	base := propertyInviteFixture(t)
	tests := []struct {
		name   string
		query  string
		mutate func(*State)
	}{
		{name: "case sensitive missing", query: "bob"},
		{name: "missing", query: "Nobody"},
		{name: "self", query: "Alice"},
		{name: "offline", query: "Offline", mutate: func(s *State) {
			s.Players["offline"] = PlayerState{Body: LegacyMonster{Name: "Offline", Type: 0, RoomID: 1}, Online: false}
		}},
		{name: "ordinary invisible", query: "Bob", mutate: func(s *State) {
			p := s.Players["bob"]
			propertyInviteFlag(&p.Body, propertyInviteInvisibleFlag)
			s.Players["bob"] = p
		}},
		{name: "dm invisible", query: "Bob", mutate: func(s *State) {
			p := s.Players["bob"]
			propertyInviteFlag(&p.Body, propertyInviteDMInvisibleFlag)
			s.Players["bob"] = p
		}},
		{name: "ambiguous exact names", query: "Bob", mutate: func(s *State) {
			s.Players["other"] = PlayerState{Body: LegacyMonster{Name: "Bob", Type: 0, RoomID: 1}, Online: true}
			r := s.Rooms[1]
			r.PlayerIDs = append(r.PlayerIDs, "other")
			s.Rooms[1] = r
		}},
		{name: "control", query: "Bob\n"},
		{name: "invalid utf8", query: string([]byte{0xff})},
		{name: "too many bytes", query: strings.Repeat("a", propertyInviteNameMaxBytes+1)},
		{name: "too many codepoints", query: strings.Repeat("가", propertyInviteNameMaxCodepoints+1)},
		{name: "whitespace", query: "Bob Name"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := base.clone()
			if tc.mutate != nil {
				tc.mutate(&s)
			}
			if _, err := s.PlanPropertyInvite("actor", tc.query); err == nil {
				t.Fatalf("accepted query %q", tc.query)
			}
		})
	}
}

func TestPropertyInviteUsesSocialStatusVisibilityBoundaries(t *testing.T) {
	s := propertyInviteFixture(t)
	bob := s.Players["bob"]
	propertyInviteFlag(&bob.Body, propertyInviteInvisibleFlag)
	s.Players["bob"] = bob
	if _, err := s.PlanPropertyInvite("actor", "Bob"); err == nil {
		t.Fatal("ordinary actor saw invisible target")
	}
	actor := s.Players["actor"]
	propertyInviteFlag(&actor.Body, propertyInviteDetectFlag)
	s.Players["actor"] = actor
	if _, err := s.PlanPropertyInvite("actor", "Bob"); err != nil {
		t.Fatalf("detector could not invite invisible target: %v", err)
	}

	s = propertyInviteFixture(t)
	bob = s.Players["bob"]
	propertyInviteFlag(&bob.Body, propertyInviteDMInvisibleFlag)
	s.Players["bob"] = bob
	actor = s.Players["actor"]
	actor.Body.Class = propertyInviteSubDMClass
	s.Players["actor"] = actor
	if _, err := s.PlanPropertyInvite("actor", "Bob"); err != nil {
		t.Fatalf("sub-DM could not see non-DM PDMINV target: %v", err)
	}

	s = propertyInviteFixture(t)
	bob = s.Players["bob"]
	bob.Body.Class = propertyInviteDMClass
	propertyInviteFlag(&bob.Body, propertyInviteDMInvisibleFlag)
	s.Players["bob"] = bob
	actor = s.Players["actor"]
	actor.Body.Class = propertyInviteDMClass
	s.Players["actor"] = actor
	if _, err := s.PlanPropertyInvite("actor", "Bob"); err == nil {
		t.Fatal("DM target's PDMINV was exposed")
	}
}

func TestPropertyInviteApplyRejectsStaleSnapshotsAtomically(t *testing.T) {
	mutations := []func(*State){
		func(s *State) { p := s.Players["actor"]; p.Body.RoomID = 2; s.Players["actor"] = p },
		func(s *State) { p := s.Players["actor"]; p.Body.Daily[8].Max = 8; s.Players["actor"] = p },
		func(s *State) { p := s.Players["bob"]; p.Body.Name = "Bobby"; s.Players["bob"] = p },
		func(s *State) {
			p := s.Players["bob"]
			propertyInviteFlag(&p.Body, propertyInviteInvisibleFlag)
			s.Players["bob"] = p
		},
		func(s *State) { r := s.Rooms[1]; r.Resource.Flags = [8]byte{}; s.Rooms[1] = r },
		func(s *State) { s.Invitations[7] = []string{"cara"} },
		func(s *State) { s.Invitations[8] = []string{"cara"} },
	}
	for i, mutate := range mutations {
		t.Run(strings.Join([]string{"mutation", string(rune('0' + i))}, "-"), func(t *testing.T) {
			s := propertyInviteFixture(t)
			proposal, err := s.PlanPropertyInvite("actor", "Bob")
			if err != nil {
				t.Fatal(err)
			}
			mutate(&s)
			before := s.clone()
			got, _, err := s.ApplyPropertyInvite(proposal)
			if err == nil || !reflect.DeepEqual(got, State{}) || !reflect.DeepEqual(s, before) {
				t.Fatalf("stale apply got=%+v err=%v state changed=%v", got, err, !reflect.DeepEqual(s, before))
			}
		})
	}

	s := propertyInviteFixture(t)
	proposal, err := s.PlanPropertyInvite("actor", "Bob")
	if err != nil {
		t.Fatal(err)
	}
	proposal.Response = "tampered\r\n"
	if got, _, err := s.ApplyPropertyInvite(proposal); err == nil || !reflect.DeepEqual(got, State{}) {
		t.Fatalf("tampered proposal accepted: got=%+v err=%v", got, err)
	}
}

func TestPropertyInviteRejectsCapacityWithoutMutation(t *testing.T) {
	s := propertyInviteFixture(t)
	for i := 0; i < maxPropertyInvitations; i++ {
		id := "invite-" + string(rune('a'+i))
		name := "N" + string(rune('A'+i))
		s.Players[id] = PlayerState{Body: LegacyMonster{Name: name, Type: 0, RoomID: 1}, Online: true}
		r := s.Rooms[1]
		r.PlayerIDs = append(r.PlayerIDs, id)
		s.Rooms[1] = r
		s.Invitations[7] = append(s.Invitations[7], id)
	}
	s.Players["extra"] = PlayerState{Body: LegacyMonster{Name: "Extra", Type: 0, RoomID: 1}, Online: true}
	r := s.Rooms[1]
	r.PlayerIDs = append(r.PlayerIDs, "extra")
	s.Rooms[1] = r
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	before := s.clone()
	if _, err := s.PlanPropertyInvite("actor", "Extra"); err == nil {
		t.Fatal("accepted an eleventh invitation")
	}
	if !reflect.DeepEqual(s, before) {
		t.Fatal("capacity failure mutated state")
	}
}
