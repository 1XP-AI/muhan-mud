package world

import (
	"errors"
	"reflect"
	"testing"
)

func dmCharmFixture(t *testing.T, class byte) State {
	t.Helper()
	s := State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"actor", "mortal"},
			},
			2: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 2, Name: "먼방"}},
				PlayerIDs: []string{"target"},
			},
			3: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 3, Name: "NPC방"}},
				PlayerIDs: []string{"player-charmer"},
				NPCIDs:    []string{"npc-charmer"},
			},
		},
		Players: map[string]PlayerState{
			"actor":          {Body: LegacyMonster{Name: "운영자", Type: 0, Class: class, RoomID: 1}, Online: true},
			"mortal":         {Body: LegacyMonster{Name: "필멸자", Type: 0, Class: 4, RoomID: 1}, Online: true},
			"target":         {Body: LegacyMonster{Name: "대상자", Type: 0, Class: 4, RoomID: 2}, Online: true, CharmRefs: []EntityRef{{Kind: "npc", ID: "npc-charmer"}, {Kind: "player", ID: "player-charmer"}}},
			"player-charmer": {Body: LegacyMonster{Name: "플레이어최면", Type: 0, Class: 4, RoomID: 3}, Online: true},
		},
		NPCs: map[string]NPCState{
			"npc-charmer": {Body: LegacyMonster{Name: "NPC최면", Type: 1, RoomID: 3}},
		},
		ActiveNPCIDs: []string{},
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPlanApplyDMCharmUsesGlobalPlayerAndCanonicalRelationOrder(t *testing.T) {
	s := dmCharmFixture(t, playerSubDMClass)
	proposal, err := s.PlanDMCharm("actor", "*charm", "대상자")
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Action != DMCharmList || proposal.TargetID != "target" || proposal.TargetName != "대상자" || proposal.Changed {
		t.Fatalf("proposal=%+v", proposal)
	}
	if !reflect.DeepEqual(proposal.CharmerIDs, []string{"npc-charmer", "player-charmer"}) || !reflect.DeepEqual(proposal.CharmerNames, []string{"NPC최면", "플레이어최면"}) {
		t.Fatalf("relation order ids=%v names=%v", proposal.CharmerIDs, proposal.CharmerNames)
	}
	want := "대상자의 피최면자:\nNPC최면.\n플레이어최면.\n"
	if proposal.Response != want {
		t.Fatalf("response=%q want=%q", proposal.Response, want)
	}
	next, result, err := s.ApplyDMCharm(proposal)
	if err != nil || !reflect.DeepEqual(next, s) || result.Response != want || !reflect.DeepEqual(result.CharmerIDs, proposal.CharmerIDs) || result.Changed {
		t.Fatalf("next=%+v result=%+v err=%v", next, result, err)
	}
	if response, err := s.CharmList("actor", "*최면", "대상자"); err != nil || response != want {
		t.Fatalf("response=%q err=%v", response, err)
	}
}

func TestPlanDMCharmSubDMGateIsOpaqueAndDoesNotResolveTarget(t *testing.T) {
	s := dmCharmFixture(t, 4)
	proposal, err := s.PlanDMCharm("actor", "*최면", "없는대상")
	if err != nil || proposal.Action != DMCharmUnknown || proposal.Response != DMCharmUnknownResponse("*최면") || proposal.TargetID != "" || len(proposal.CharmerNames) != 0 || proposal.Changed {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	next, result, err := s.ApplyDMCharm(proposal)
	if err != nil || !reflect.DeepEqual(next, s) || result.Response != proposal.Response || result.Changed {
		t.Fatalf("next=%+v result=%+v err=%v", next, result, err)
	}
}

func TestPlanDMCharmKnownEmptyRelationRendersNone(t *testing.T) {
	s := dmCharmFixture(t, playerDMClass)
	target := s.Players["target"]
	target.CharmRefs = []EntityRef{}
	s.Players["target"] = target
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	proposal, err := s.PlanDMCharm("actor", "*charm", "대상자")
	if err != nil || proposal.Response != "대상자의 피최면자:\n없음.\n" || len(proposal.CharmerNames) != 0 || proposal.CharmerNames == nil {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
}

func TestPlanDMCharmFailsClosedForMissingTargetAndUnresolvedRelation(t *testing.T) {
	s := dmCharmFixture(t, playerDMClass)
	if _, err := s.PlanDMCharm("actor", "*charm", "없는대상"); !errors.Is(err, ErrDMCharmTargetAbsent) {
		t.Fatalf("missing target err=%v", err)
	}
	target := s.Players["target"]
	target.CharmRefs = nil
	s.Players["target"] = target
	if _, err := s.PlanDMCharm("actor", "*charm", "대상자"); !errors.Is(err, ErrDMCharmRelationsUnresolved) {
		t.Fatalf("nil relation err=%v", err)
	}
	if _, err := s.PlanDMCharm("actor", "*charm", "대상자 대상"); !errors.Is(err, ErrDMCharmTargetNameInvalid) {
		t.Fatalf("invalid target err=%v", err)
	}
}

func TestValidateCharmRelationsRejectsDuplicateOrUnknownIdentity(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*State)
	}{
		{name: "duplicate in list", mutate: func(s *State) {
			target := s.Players["target"]
			target.CharmRefs = append(target.CharmRefs, target.CharmRefs[0])
			s.Players["target"] = target
		}},
		{name: "same charmer has two owners", mutate: func(s *State) {
			mortal := s.Players["mortal"]
			mortal.CharmRefs = []EntityRef{{Kind: "npc", ID: "npc-charmer"}}
			s.Players["mortal"] = mortal
		}},
		{name: "unknown immutable identity", mutate: func(s *State) {
			target := s.Players["target"]
			target.CharmRefs = []EntityRef{{Kind: "npc", ID: "legacy-room-only"}}
			s.Players["target"] = target
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := dmCharmFixture(t, playerDMClass)
			test.mutate(&s)
			if err := s.Validate(); err == nil {
				t.Fatal("invalid charm relation unexpectedly validated")
			}
			if _, err := s.PlanDMCharm("actor", "*charm", "대상자"); !errors.Is(err, ErrDMCharmStateInvalid) {
				t.Fatalf("plan err=%v", err)
			}
		})
	}
}

func TestApplyDMCharmRejectsStaleProjection(t *testing.T) {
	s := dmCharmFixture(t, playerDMClass)
	proposal, err := s.PlanDMCharm("actor", "*charm", "대상자")
	if err != nil {
		t.Fatal(err)
	}
	target := s.Players["target"]
	target.CharmRefs = []EntityRef{}
	s.Players["target"] = target
	if _, _, err := s.ApplyDMCharm(proposal); !errors.Is(err, ErrDMCharmStaleProposal) {
		t.Fatalf("stale err=%v", err)
	}
}
