package world

import (
	"encoding/json"
	"reflect"
	"testing"
)

func stateFixture() State {
	return State{Version: 1,
		Rooms:   map[int16]RoomState{1: {Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}}, PlayerIDs: []string{"a"}}, 2: {Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 2}}}},
		Players: map[string]PlayerState{"a": {Body: LegacyMonster{Name: "Alice", RoomID: 1, HPCurrent: 30}, Online: true}},
	}
}

func TestStateRoundTripAndAuthoritativeViews(t *testing.T) {
	s := stateFixture()
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeState(raw)
	if err != nil || !reflect.DeepEqual(got, s) {
		t.Fatalf("%+v %v", got, err)
	}
	views, err := got.RoomPlayers(1)
	if err != nil || len(views) != 1 || views[0].ID != "a" || views[0].Name != "Alice" {
		t.Fatalf("%+v %v", views, err)
	}
	views[0].Name = "changed"
	if got.Players["a"].Body.Name != "Alice" {
		t.Fatal("view mutated authority")
	}
}

func TestStateRejectsInconsistentMembership(t *testing.T) {
	for _, change := range []func(*State){
		func(s *State) { s.Version = 2 },
		func(s *State) { p := s.Players["a"]; p.Body.Type = 1; s.Players["a"] = p },
		func(s *State) { delete(s.Players, "a") },
		func(s *State) { r := s.Rooms[1]; r.PlayerIDs = nil; s.Rooms[1] = r },
		func(s *State) { r := s.Rooms[2]; r.PlayerIDs = []string{"a"}; s.Rooms[2] = r },
		func(s *State) { p := s.Players["a"]; p.Online = false; s.Players["a"] = p },
		func(s *State) { p := s.Players["a"]; p.Body.RoomID = 2; s.Players["a"] = p },
		func(s *State) { r := s.Rooms[1]; r.Resource.ID = 2; s.Rooms[1] = r },
	} {
		s := stateFixture()
		change(&s)
		if s.Validate() == nil {
			t.Fatalf("accepted %+v", s)
		}
		raw, _ := json.Marshal(s)
		got, err := DecodeState(raw)
		if err == nil || !reflect.DeepEqual(got, State{}) {
			t.Fatal("partial invalid state")
		}
	}
}

func TestStateOfflinePlayerHasLocationButNoMembership(t *testing.T) {
	s := stateFixture()
	r := s.Rooms[1]
	r.PlayerIDs = nil
	s.Rooms[1] = r
	p := s.Players["a"]
	p.Online = false
	s.Players["a"] = p
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestStateDecoderRejectsUnknownOrTrailingData(t *testing.T) {
	for _, raw := range []string{`null`, `{}`, `{"Version":1,"Rooms":{},"Players":{},"Unexpected":true}`, `{"Version":1,"Rooms":{},"Players":{}} {}`} {
		if _, err := DecodeState([]byte(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestStateApplyTransferKeepsCanonicalPlayerAndIndependentSnapshot(t *testing.T) {
	s := stateFixture()
	in := transferFixture()
	proposal, err := PlanTransfer(in, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	next, err := s.ApplyTransfer("a", proposal)
	if err != nil || next.Players["a"].Body.RoomID != 2 || next.Players["a"].Body.HPCurrent != 30 || len(next.Rooms[1].PlayerIDs) != 0 || !reflect.DeepEqual(next.Rooms[2].PlayerIDs, []string{"a"}) {
		t.Fatalf("%+v %v", next, err)
	}
	if s.Players["a"].Body.RoomID != 1 || len(s.Rooms[1].PlayerIDs) != 1 {
		t.Fatal("mutated previous snapshot")
	}
	next.Rooms[2].PlayerIDs[0] = "changed"
	if proposal.Entry.Players[0].ID != "a" {
		t.Fatal("proposal alias")
	}
}

func TestStateRejectsMalformedTransferWithoutPartialState(t *testing.T) {
	for _, mutate := range []func(*TransferProposal){
		func(p *TransferProposal) { p.Movement.HP = 32768 },
		func(p *TransferProposal) { p.Entry = nil },
		func(p *TransferProposal) { p.SourcePlayers = []RoomPlayerView{{ID: "a"}} },
		func(p *TransferProposal) { p.Entry.Players[0].Name = "another" },
		func(p *TransferProposal) { p.Movement.RoomID = 99 },
	} {
		s := stateFixture()
		p, err := PlanTransfer(transferFixture(), nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		mutate(&p)
		got, err := s.ApplyTransfer("a", p)
		if err == nil || !reflect.DeepEqual(got, State{}) || s.Players["a"].Body.RoomID != 1 {
			t.Fatalf("partial application: %+v %v", got, err)
		}
	}
}
