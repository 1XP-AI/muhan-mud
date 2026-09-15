package world

import (
	"errors"
	"reflect"
	"testing"
)

func dmActiveFixture(t *testing.T) State {
	t.Helper()
	s := State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"dm", "caretaker", "mortal"},
				NPCIDs:    []string{"room-first", "room-second"},
			},
		},
		Players: map[string]PlayerState{
			"dm":        {Body: LegacyMonster{Name: "운영자", Type: 0, Class: playerDMClass, RoomID: 1}, Online: true},
			"caretaker": {Body: LegacyMonster{Name: "관리자", Type: 0, Class: playerCaretakerClass, RoomID: 1}, Online: true},
			"mortal":    {Body: LegacyMonster{Name: "Alice", Type: 0, Class: 4, RoomID: 1}, Online: true},
		},
		NPCs: map[string]NPCState{
			"room-first":  {Body: LegacyMonster{Name: "방순서", Type: 1, RoomID: 1}},
			"room-second": {Body: LegacyMonster{Name: "활성순서", Type: 1, RoomID: 1}},
		},
		ActiveNPCIDs: []string{"room-second", "room-first"},
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPlanApplyDMActiveUsesOnlyOrderedCanonicalActiveNPCIDs(t *testing.T) {
	s := dmActiveFixture(t)
	proposal, err := s.PlanDMActive("dm", "*활성")
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Action != DMActiveList || proposal.ActorID != "dm" || proposal.Verb != "*활성" || proposal.Changed {
		t.Fatalf("proposal=%+v", proposal)
	}
	wantIDs := []string{"room-second", "room-first"}
	wantNames := []string{"활성순서", "방순서"}
	if !reflect.DeepEqual(proposal.NPCIDs, wantIDs) || !reflect.DeepEqual(proposal.NPCNames, wantNames) {
		t.Fatalf("proposal identities ids=%v names=%v", proposal.NPCIDs, proposal.NPCNames)
	}
	wantResponse := "현재 활동중인 괴물\n\n이름:\n   활성순서.\n   방순서.\n"
	if proposal.Response != wantResponse {
		t.Fatalf("response=%q want=%q", proposal.Response, wantResponse)
	}

	next, result, err := s.ApplyDMActive(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(next, s) || !reflect.DeepEqual(result.NPCIDs, wantIDs) || result.Response != wantResponse || result.Changed {
		t.Fatalf("next=%+v result=%+v source=%+v", next, result, s)
	}
	projection, err := s.ProjectDMActive("dm")
	if err != nil || projection.Response != wantResponse || !reflect.DeepEqual(projection.NPCIDs, wantIDs) {
		t.Fatalf("projection=%+v err=%v", projection, err)
	}
	if response, err := s.ActiveNPCList("dm", "*active"); err != nil || response != wantResponse {
		t.Fatalf("response=%q err=%v", response, err)
	}
}

func TestPlanDMActiveUnauthorizedAndInvalidActorAreDeterministicNoOps(t *testing.T) {
	s := dmActiveFixture(t)
	for _, test := range []struct {
		name   string
		actor  string
		action DMActiveAction
		want   string
	}{
		{name: "mortal", actor: "mortal", action: DMActiveUnknown, want: DMActiveUnknownResponse("*active")},
		{name: "caretaker", actor: "caretaker", action: DMActivePrompt, want: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			proposal, err := s.PlanDMActive(test.actor, "*active")
			if err != nil || proposal.Action != test.action || proposal.Response != test.want || proposal.Changed {
				t.Fatalf("proposal=%+v err=%v", proposal, err)
			}
			next, result, err := s.ApplyDMActive(proposal)
			if err != nil || !reflect.DeepEqual(next, s) || result.Response != test.want || result.Changed {
				t.Fatalf("next=%+v result=%+v err=%v", next, result, err)
			}
		})
	}
	offline := s.Players["dm"]
	offline.Online = false
	s.Players["dm"] = offline
	room := s.Rooms[1]
	room.PlayerIDs = []string{"caretaker", "mortal"}
	s.Rooms[1] = room
	if _, err := s.PlanDMActive("dm", "*active"); !errors.Is(err, ErrDMActiveActorAbsent) {
		t.Fatalf("offline actor err=%v", err)
	}
}

func TestPlanDMActiveFailsClosedOnUnresolvedAndInvalidCanonicalActiveIdentities(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*State)
		want   error
	}{
		{name: "nil active order", mutate: func(s *State) { s.ActiveNPCIDs = nil }, want: ErrDMActiveNPCUnresolved},
		{name: "missing identity", mutate: func(s *State) { s.ActiveNPCIDs = []string{"missing"} }, want: ErrDMActiveStateInvalid},
		{name: "duplicate identity", mutate: func(s *State) { s.ActiveNPCIDs = []string{"room-first", "room-first"} }, want: ErrDMActiveStateInvalid},
		{name: "non NPC identity", mutate: func(s *State) {
			s.NPCs["room-first"] = NPCState{Body: LegacyMonster{Name: "player-shaped", Type: 0, RoomID: 1}}
		}, want: ErrDMActiveStateInvalid},
		{name: "legacy room fallback", mutate: func(s *State) {
			r := s.Rooms[1]
			r.Resource.Monsters = []LegacyMonster{{Name: "legacy-only", Type: 1, RoomID: 1}}
			s.Rooms[1] = r
			s.ActiveNPCIDs = []string{}
		}, want: ErrDMActiveStateInvalid},
		{name: "invalid rendered name", mutate: func(s *State) {
			npc := s.NPCs["room-first"]
			npc.Body.Name = "bad\nname"
			s.NPCs["room-first"] = npc
		}, want: ErrDMActiveIdentityUnresolved},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := dmActiveFixture(t)
			test.mutate(&s)
			if _, err := s.PlanDMActive("dm", "*active"); !errors.Is(err, test.want) {
				t.Fatalf("err=%v want errors.Is(%v)", err, test.want)
			}
		})
	}
	if _, err := dmActiveFixture(t).PlanDMActive("dm", "*act"); !errors.Is(err, ErrDMActiveInvalidVerb) {
		t.Fatalf("invalid verb err=%v", err)
	}
}

func TestApplyDMActiveRejectsStaleProjection(t *testing.T) {
	s := dmActiveFixture(t)
	proposal, err := s.PlanDMActive("dm", "*active")
	if err != nil {
		t.Fatal(err)
	}
	npc := s.NPCs["room-first"]
	npc.Body.Name = "변경됨"
	s.NPCs["room-first"] = npc
	if _, _, err := s.ApplyDMActive(proposal); !errors.Is(err, ErrDMActiveStaleProposal) {
		t.Fatalf("stale err=%v", err)
	}
}
