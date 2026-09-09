package world

import (
	"errors"
	"reflect"
	"testing"
)

func familyApprovalFixture(t *testing.T, activeApplicant bool) (State, FamilyCatalog) {
	t.Helper()
	room := RoomState{Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}}, PlayerIDs: []string{"applicant", "boss"}}
	applicant := LegacyMonster{Name: "Alice", Type: 0, Class: 4, RoomID: 1, Gold: 100000}
	boss := LegacyMonster{Name: "Boss", Type: 0, Class: 4, RoomID: 1, Gold: 100000}
	applicant.Daily[FamilyDailySlot].Max = 2
	boss.Daily[FamilyDailySlot].Max = 2
	if activeApplicant {
		familyMutationFlag(&applicant, FamilyMemberFlag, true)
	} else {
		familyMutationFlag(&applicant, FamilyPendingFlag, true)
	}
	familyMutationFlag(&boss, FamilyMemberFlag, true)
	familyMutationFlag(&boss, FamilyBossFlag, true)
	state := State{
		Version: 1,
		Rooms:   map[int16]RoomState{1: room},
		Players: map[string]PlayerState{"applicant": {Body: applicant, Online: true}, "boss": {Body: boss, Online: true}},
		Family:  &FamilyState{Members: map[int16][]FamilyMember{2: {{ID: "boss", Name: "Boss", Class: 4}}}},
	}
	catalog := FamilyCatalog{Families: map[int16]FamilyDefinition{2: {ID: 2, Name: "청룡", Boss: "Boss", Fee: 3}}}
	if activeApplicant {
		members := append([]FamilyMember(nil), state.Family.Members[2]...)
		members = append(members, FamilyMember{ID: "applicant", Name: "Alice", Class: 4})
		state.Family.Members[2] = members
	}
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	return state, catalog
}

func TestFamilyApprovalTransfersFeeAndAppendsCanonicalMember(t *testing.T) {
	state, catalog := familyApprovalFixture(t, false)
	proposal, err := state.PlanFamilyApproval("boss", "Alice", catalog)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Action != FamilyMutationApprove || proposal.ActorID != "boss" || proposal.TargetID != "applicant" || proposal.FamilyID != 2 || proposal.Fee != 3 || proposal.GoldTransferred != 30000 {
		t.Fatalf("proposal=%+v", proposal)
	}
	next, result, err := state.ApplyFamilyMutation(proposal)
	if err != nil {
		t.Fatal(err)
	}
	target := next.Players["applicant"].Body
	boss := next.Players["boss"].Body
	if !flag(target.Flags[:], FamilyMemberFlag) || flag(target.Flags[:], FamilyPendingFlag) || target.Daily[FamilyDailySlot].Max != 2 || target.Gold != 130000 || boss.Gold != 70000 {
		t.Fatalf("target=%+v boss=%+v", target, boss)
	}
	if len(next.Family.Members[2]) != 2 || next.Family.Members[2][1].ID != "applicant" || result.TargetID != "applicant" || result.TargetAfterGold != 130000 {
		t.Fatalf("family=%+v result=%+v", next.Family, result)
	}
	if _, _, err := next.ApplyFamilyMutation(proposal); !errors.Is(err, ErrFamilyMutationStaleProposal) {
		t.Fatalf("replay err=%v", err)
	}
}

func TestFamilyActiveWithdrawalChargesFeeAndRemovesCanonicalMember(t *testing.T) {
	state, catalog := familyApprovalFixture(t, true)
	proposal, err := state.PlanFamilyWithdrawal("applicant", catalog)
	if err != nil {
		t.Fatal(err)
	}
	next, result, err := state.ApplyFamilyWithdrawal(proposal)
	if err != nil {
		t.Fatal(err)
	}
	actor := next.Players["applicant"].Body
	if flag(actor.Flags[:], FamilyMemberFlag) || flag(actor.Flags[:], FamilyPendingFlag) || actor.Daily[FamilyDailySlot].Max != 0 || actor.Gold != 40000 {
		t.Fatalf("actor=%+v", actor)
	}
	if len(next.Family.Members[2]) != 1 || next.Family.Members[2][0].ID != "boss" || result.GoldTransferred != 60000 {
		t.Fatalf("family=%+v result=%+v", next.Family, result)
	}
}

func TestFamilyApprovalAndWithdrawalRequireExactAuthorityAndLedger(t *testing.T) {
	state, catalog := familyApprovalFixture(t, false)
	for name, mutate := range map[string]func(*State){
		"noncanonical target": func(s *State) { _ = s },
		"nonboss actor": func(s *State) {
			boss := s.Players["boss"]
			familyMutationFlag(&boss.Body, FamilyBossFlag, false)
			s.Players["boss"] = boss
		},
		"offline target": func(s *State) {
			target := s.Players["applicant"]
			target.Online = false
			s.Players["applicant"] = target
			r := s.Rooms[1]
			r.PlayerIDs = []string{"boss"}
			s.Rooms[1] = r
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := state.clone()
			mutate(&candidate)
			if name == "noncanonical target" {
				if _, err := candidate.PlanFamilyApproval("boss", "alice", catalog); !errors.Is(err, ErrFamilyMutationIdentityUnresolved) {
					t.Fatalf("err=%v", err)
				}
				return
			}
			if _, err := candidate.PlanFamilyApproval("boss", "Alice", catalog); err == nil {
				t.Fatal("authority failure accepted")
			}
		})
	}

	missing := state.clone()
	missing.Family.Members = map[int16][]FamilyMember{}
	if _, err := missing.PlanFamilyApproval("boss", "Alice", catalog); !errors.Is(err, ErrFamilyMutationMemberLedgerMissing) {
		t.Fatalf("missing ledger err=%v", err)
	}
	active, _ := familyApprovalFixture(t, true)
	active.Family.Members[2] = active.Family.Members[2][:1]
	if _, err := active.PlanFamilyWithdrawal("applicant", catalog); !errors.Is(err, ErrFamilyMutationIdentityUnresolved) {
		t.Fatalf("missing active member err=%v", err)
	}
}

func TestFamilyMutationRollbackRejectsStaleLedgerOrGoldConflict(t *testing.T) {
	state, catalog := familyApprovalFixture(t, false)
	proposal, err := state.PlanFamilyApproval("boss", "Alice", catalog)
	if err != nil {
		t.Fatal(err)
	}
	original := state.clone()
	changed := state.clone()
	changed.Family.Members[2][0].Class++
	changedBefore := changed.clone()
	if _, _, err := changed.ApplyFamilyMutation(proposal); !errors.Is(err, ErrFamilyMutationStaleProposal) {
		t.Fatalf("ledger conflict err=%v", err)
	}
	if !reflect.DeepEqual(changed, changedBefore) {
		t.Fatal("stale ledger apply mutated source")
	}
	changed = state.clone()
	boss := changed.Players["boss"]
	boss.Body.Gold--
	changed.Players["boss"] = boss
	changedBefore = changed.clone()
	if _, _, err := changed.ApplyFamilyMutation(proposal); !errors.Is(err, ErrFamilyMutationStaleProposal) {
		t.Fatalf("gold conflict err=%v", err)
	}
	if !reflect.DeepEqual(changed, changedBefore) {
		t.Fatal("stale gold apply mutated source")
	}
	if !reflect.DeepEqual(state, original) {
		t.Fatal("planning mutated source")
	}
}
