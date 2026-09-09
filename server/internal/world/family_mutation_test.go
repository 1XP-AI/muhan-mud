package world

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func familyMutationFlag(body *LegacyMonster, bit uint, enabled bool) {
	if enabled {
		body.Flags[bit/8] |= 1 << (bit % 8)
		return
	}
	body.Flags[bit/8] &^= 1 << (bit % 8)
}

func familyMutationFixture(t *testing.T) (State, FamilyCatalog) {
	t.Helper()
	room := RoomState{Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}}}
	applicant := LegacyMonster{Name: "Alice", Type: 0, Class: 4, RoomID: 1}
	boss := LegacyMonster{Name: "Boss", Type: 0, Class: 4, RoomID: 1}
	boss.Daily[FamilyDailySlot].Max = 2
	familyMutationFlag(&boss, FamilyMemberFlag, true)
	familyMutationFlag(&boss, FamilyBossFlag, true)
	room.PlayerIDs = []string{"applicant", "boss"}
	s := State{
		Version: 1,
		Rooms:   map[int16]RoomState{1: room},
		Players: map[string]PlayerState{
			"applicant": {Body: applicant, Online: true},
			"boss":      {Body: boss, Online: true},
		},
	}
	catalog := FamilyCatalog{Families: map[int16]FamilyDefinition{
		2: {ID: 2, Name: "청룡", Boss: "Boss"},
	}}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	return s, catalog
}

func TestFamilyMutationJoinApplicationSetsPendingAndResolvesCanonicalBoss(t *testing.T) {
	s, catalog := familyMutationFixture(t)
	original := s.clone()

	proposal, err := s.PlanFamilyJoin("applicant", 2, catalog)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Action != FamilyMutationApply || proposal.ActorID != "applicant" || proposal.BossID != "boss" || proposal.BossName != "Boss" || proposal.FamilyID != 2 || proposal.FamilyName != "청룡" || !proposal.Changed || !proposal.BossNotificationPending {
		t.Fatalf("proposal=%+v", proposal)
	}
	if proposal.BeforeFamilyID != 0 || proposal.AfterFamilyID != 2 || proposal.BeforeFlags != s.Players["applicant"].Body.Flags || !flag(proposal.AfterFlags[:], FamilyPendingFlag) || flag(proposal.AfterFlags[:], FamilyMemberFlag) {
		t.Fatalf("proposal transition=%+v actor=%+v", proposal, s.Players["applicant"])
	}
	if !reflect.DeepEqual(s, original) {
		t.Fatal("planning mutated state")
	}

	next, result, err := s.ApplyFamilyMutation(proposal)
	if err != nil {
		t.Fatal(err)
	}
	applicant := next.Players["applicant"].Body
	if !flag(applicant.Flags[:], FamilyPendingFlag) || flag(applicant.Flags[:], FamilyMemberFlag) || applicant.Daily[FamilyDailySlot].Max != 2 {
		t.Fatalf("applicant after join=%+v", applicant)
	}
	if result.Action != FamilyMutationApply || result.ActorID != "applicant" || result.BossID != "boss" || result.FamilyID != 2 || !result.Changed || !result.BossNotificationPending || !strings.Contains(result.Response, "가입 신청을 하였습니다") {
		t.Fatalf("result=%+v", result)
	}
	if !reflect.DeepEqual(s.Players["applicant"], original.Players["applicant"]) {
		t.Fatal("source applicant mutated")
	}
	if err := next.Validate(); err != nil {
		t.Fatalf("next invalid: %v", err)
	}

	if _, _, err := next.ApplyFamilyMutation(proposal); !errors.Is(err, ErrFamilyMutationStaleProposal) {
		t.Fatalf("replay err=%v", err)
	}
}

func TestFamilyMutationJoinByNameStoresNumericFamilyID(t *testing.T) {
	s, catalog := familyMutationFixture(t)
	proposal, err := s.PlanFamilyJoinByName("applicant", "청룡", catalog)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.FamilyID != 2 || proposal.FamilyName != "청룡" {
		t.Fatalf("proposal=%+v", proposal)
	}
	if _, err := s.PlanFamilyJoinByName("applicant", "청", catalog); !errors.Is(err, ErrFamilyMutationFamilyUnavailable) {
		t.Fatalf("prefix accepted: %v", err)
	}
}

func TestFamilyMutationWithdrawCancelsPendingApplicationWithoutGold(t *testing.T) {
	s, catalog := familyMutationFixture(t)
	applicant := s.Players["applicant"]
	applicant.Body.Daily[FamilyDailySlot].Max = 2
	familyMutationFlag(&applicant.Body, FamilyPendingFlag, true)
	applicant.Body.Gold = 1234
	s.Players["applicant"] = applicant
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}

	proposal, err := s.PlanFamilyWithdrawal("applicant", catalog)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Action != FamilyMutationWithdraw || proposal.FamilyID != 2 || proposal.AfterFamilyID != 0 || !proposal.Changed || proposal.BossNotificationPending {
		t.Fatalf("proposal=%+v", proposal)
	}
	next, result, err := s.ApplyFamilyWithdrawal(proposal)
	if err != nil {
		t.Fatal(err)
	}
	got := next.Players["applicant"].Body
	if flag(got.Flags[:], FamilyMemberFlag) || flag(got.Flags[:], FamilyPendingFlag) || got.Daily[FamilyDailySlot].Max != 0 || got.Gold != 1234 {
		t.Fatalf("applicant after withdraw=%+v", got)
	}
	if result.Action != FamilyMutationWithdraw || result.FamilyID != 2 || result.AfterFamilyID != 0 || result.Response != FamilyWithdrawalResponse {
		t.Fatalf("result=%+v", result)
	}
	if err := next.Validate(); err != nil {
		t.Fatalf("next invalid: %v", err)
	}
}

func TestFamilyMutationRejectsActiveWithdrawalAndApprovalUntilLedgersExist(t *testing.T) {
	s, catalog := familyMutationFixture(t)
	active := s.Players["applicant"]
	active.Body.Daily[FamilyDailySlot].Max = 2
	familyMutationFlag(&active.Body, FamilyMemberFlag, true)
	s.Players["applicant"] = active
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PlanFamilyWithdrawal("applicant", catalog); !errors.Is(err, ErrFamilyMutationFeeUnavailable) {
		t.Fatalf("active withdrawal err=%v", err)
	}

	pending := s.Players["applicant"]
	familyMutationFlag(&pending.Body, FamilyMemberFlag, false)
	familyMutationFlag(&pending.Body, FamilyPendingFlag, true)
	s.Players["applicant"] = pending
	if _, err := s.PlanFamilyApproval("boss", "Alice", catalog); !errors.Is(err, ErrFamilyMutationApprovalUnsupported) {
		t.Fatalf("approval err=%v", err)
	}
	if _, err := s.PlanFamilyMutation("boss", FamilyMutationApprove, 2, "Alice", catalog); !errors.Is(err, ErrFamilyMutationApprovalUnsupported) {
		t.Fatalf("generic approval err=%v", err)
	}
}

func TestFamilyMutationFailsClosedForCatalogOwnerAndStateBoundaries(t *testing.T) {
	base, catalog := familyMutationFixture(t)
	tests := []struct {
		name   string
		mutate func(State, FamilyCatalog) (State, FamilyCatalog)
		want   error
	}{
		{
			name: "unavailable catalog",
			mutate: func(s State, c FamilyCatalog) (State, FamilyCatalog) {
				return s, FamilyCatalog{}
			},
			want: ErrFamilyCatalogUnavailable,
		},
		{
			name: "missing canonical boss",
			mutate: func(s State, c FamilyCatalog) (State, FamilyCatalog) {
				delete(s.Players, "boss")
				r := s.Rooms[1]
				r.PlayerIDs = []string{"applicant"}
				s.Rooms[1] = r
				return s, c
			},
			want: ErrFamilyMutationBossUnavailable,
		},
		{
			name: "deactivated family",
			mutate: func(s State, c FamilyCatalog) (State, FamilyCatalog) {
				c.Families[2] = FamilyDefinition{ID: 2, Name: "청룡", Boss: familyMutationDeactivatedBoss}
				return s, c
			},
			want: ErrFamilyMutationFamilyUnavailable,
		},
		{
			name: "already active",
			mutate: func(s State, c FamilyCatalog) (State, FamilyCatalog) {
				p := s.Players["applicant"]
				p.Body.Daily[FamilyDailySlot].Max = 2
				familyMutationFlag(&p.Body, FamilyMemberFlag, true)
				s.Players["applicant"] = p
				return s, c
			},
			want: ErrFamilyMutationAlreadyMember,
		},
		{
			name: "already pending",
			mutate: func(s State, c FamilyCatalog) (State, FamilyCatalog) {
				p := s.Players["applicant"]
				p.Body.Daily[FamilyDailySlot].Max = 2
				familyMutationFlag(&p.Body, FamilyPendingFlag, true)
				s.Players["applicant"] = p
				return s, c
			},
			want: ErrFamilyMutationAlreadyPending,
		},
		{
			name: "mixed family flags",
			mutate: func(s State, c FamilyCatalog) (State, FamilyCatalog) {
				p := s.Players["applicant"]
				p.Body.Daily[FamilyDailySlot].Max = 2
				familyMutationFlag(&p.Body, FamilyMemberFlag, true)
				familyMutationFlag(&p.Body, FamilyPendingFlag, true)
				s.Players["applicant"] = p
				return s, c
			},
			want: ErrFamilyMutationStateInvalid,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, c := tc.mutate(base.clone(), cloneFamilyCatalog(catalog))
			if _, err := s.PlanFamilyJoin("applicant", 2, c); !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want %v", err, tc.want)
			}
		})
	}

	missing := base.clone()
	missing.Players["boss"] = PlayerState{Body: LegacyMonster{Name: "Different", Type: 0, RoomID: 1}, Online: true}
	if _, err := missing.PlanFamilyJoin("applicant", 2, catalog); !errors.Is(err, ErrFamilyMutationBossUnavailable) {
		t.Fatalf("wrong boss err=%v", err)
	}

	offline := base.clone()
	boss := offline.Players["boss"]
	boss.Online = false
	offline.Players["boss"] = boss
	r := offline.Rooms[1]
	r.PlayerIDs = []string{"applicant"}
	offline.Rooms[1] = r
	if _, err := offline.PlanFamilyJoin("applicant", 2, catalog); !errors.Is(err, ErrFamilyMutationBossUnavailable) {
		t.Fatalf("offline boss err=%v", err)
	}
}

func TestFamilyMutationRejectsStaleAndTamperedProposalAtomically(t *testing.T) {
	s, catalog := familyMutationFixture(t)
	proposal, err := s.PlanFamilyJoin("applicant", 2, catalog)
	if err != nil {
		t.Fatal(err)
	}
	original := s.clone()

	tampered := proposal
	tampered.FamilyID = 1
	if _, _, err := s.ApplyFamilyMutation(tampered); !errors.Is(err, ErrFamilyMutationStaleProposal) {
		t.Fatalf("tampered err=%v", err)
	}
	if !reflect.DeepEqual(s, original) {
		t.Fatal("tampered apply mutated source")
	}

	changed := s.clone()
	actor := changed.Players["applicant"]
	actor.Body.Gold++
	changed.Players["applicant"] = actor
	if _, _, err := changed.ApplyFamilyMutation(proposal); !errors.Is(err, ErrFamilyMutationStaleProposal) {
		t.Fatalf("changed-state err=%v", err)
	}
	if !reflect.DeepEqual(changed, func() State {
		c := s.clone()
		a := c.Players["applicant"]
		a.Body.Gold++
		c.Players["applicant"] = a
		return c
	}()) {
		t.Fatal("stale apply mutated changed source")
	}

	if _, _, err := s.ApplyFamilyMutation(FamilyMutationProposal{}); !errors.Is(err, ErrFamilyMutationStaleProposal) {
		t.Fatalf("empty proposal err=%v", err)
	}
}

func familyMutationExpelFixture(t *testing.T) (State, FamilyCatalog) {
	t.Helper()
	s, catalog := familyMutationFixture(t)
	target := s.Players["applicant"]
	target.Body.Daily[FamilyDailySlot].Max = 2
	familyMutationFlag(&target.Body, FamilyMemberFlag, true)
	target.Body.Gold = 0 // fm_out has no fee or insufficient-gold gate.
	s.Players["applicant"] = target
	s.Family = &FamilyState{Members: map[int16][]FamilyMember{
		2: {{ID: "boss", Name: "Boss", Class: 4}, {ID: "applicant", Name: "Alice", Class: 4}},
	}}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	return s, catalog
}

func TestFamilyMutationExpelRemovesTargetAtomicallyAndNotifiesOnlyTarget(t *testing.T) {
	s, catalog := familyMutationExpelFixture(t)
	proposal, err := s.PlanFamilyExpulsion("boss", "Alice", catalog)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Action != FamilyMutationExpel || proposal.Fee != 0 || proposal.GoldTransferred != 0 || len(proposal.Events) != 1 || proposal.Events[0].RecipientID != "applicant" {
		t.Fatalf("proposal=%+v", proposal)
	}
	next, result, err := s.ApplyFamilyExpulsion(proposal)
	if err != nil {
		t.Fatal(err)
	}
	target := next.Players["applicant"].Body
	if flag(target.Flags[:], FamilyMemberFlag) || target.Daily[FamilyDailySlot].Max != 0 || target.Gold != 0 || len(next.Family.Members[2]) != 1 || next.Family.Members[2][0].ID != "boss" {
		t.Fatalf("target/family after expel=%+v family=%+v", target, next.Family)
	}
	if result.Response != "Alice님을 패거리에서 추방하였습니다.\r\n" || len(result.Events) != 1 || result.Events[0].Text != FamilyExpulsionNotification {
		t.Fatalf("result=%+v", result)
	}
	if _, _, err := next.ApplyFamilyExpulsion(proposal); !errors.Is(err, ErrFamilyMutationStaleProposal) {
		t.Fatalf("replay err=%v", err)
	}
}

func TestFamilyMutationExpelFailsClosedForSelfVisibilityLedgerAndDuplicate(t *testing.T) {
	s, catalog := familyMutationExpelFixture(t)
	if _, err := s.PlanFamilyExpulsion("boss", "Boss", catalog); !errors.Is(err, ErrFamilyMutationTargetSelf) {
		t.Fatalf("self err=%v", err)
	}
	hidden := s.clone()
	target := hidden.Players["applicant"]
	familyMutationFlag(&target.Body, playerInvisibleFlag, true)
	hidden.Players["applicant"] = target
	if _, err := hidden.PlanFamilyExpulsion("boss", "Alice", catalog); !errors.Is(err, ErrFamilyMutationTargetUnavailable) {
		t.Fatalf("hidden err=%v", err)
	}
	missing := s.clone()
	missing.Family.Members[2] = []FamilyMember{{ID: "boss", Name: "Boss", Class: 4}}
	if _, err := missing.PlanFamilyExpulsion("boss", "Alice", catalog); !errors.Is(err, ErrFamilyMutationTargetNotMember) {
		t.Fatalf("missing row err=%v", err)
	}
	duplicate := s.clone()
	duplicate.Family.Members[2] = append(duplicate.Family.Members[2], FamilyMember{ID: "applicant", Name: "Alice", Class: 4})
	if _, err := duplicate.PlanFamilyExpulsion("boss", "Alice", catalog); !errors.Is(err, ErrFamilyMemberDuplicate) {
		t.Fatalf("duplicate err=%v", err)
	}
}

func TestFamilyMutationRejectsNonCanonicalOnlineIdentityAndAmbiguousBoss(t *testing.T) {
	s, catalog := familyMutationFixture(t)
	bad := s.Players["applicant"]
	bad.Body.Name = "alice"
	s.Players["applicant"] = bad
	if _, err := s.PlanFamilyJoin("applicant", 2, catalog); !errors.Is(err, ErrFamilyMutationIdentityUnresolved) {
		t.Fatalf("noncanonical actor err=%v", err)
	}

	s, catalog = familyMutationFixture(t)
	duplicate := s.Players["boss"]
	duplicate.Body.Name = "Boss"
	s.Players["second-boss"] = duplicate
	r := s.Rooms[1]
	r.PlayerIDs = append(r.PlayerIDs, "second-boss")
	s.Rooms[1] = r
	if _, err := s.PlanFamilyJoin("applicant", 2, catalog); !errors.Is(err, ErrFamilyMutationBossAmbiguous) {
		t.Fatalf("ambiguous boss err=%v", err)
	}
}
