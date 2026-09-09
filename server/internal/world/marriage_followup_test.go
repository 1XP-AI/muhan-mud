package world

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func divorceFollowupState() State {
	s := marriageTestState()
	for id, spouse := range map[string]string{"alice": "mBob", "bob": "mAlice"} {
		player := s.Players[id]
		player.Body.Flags[MarriageActiveFlag/8] |= 1 << (MarriageActiveFlag % 8)
		player.Body.Keys[MarriageSpouseKeyIndex] = spouse
		s.Players[id] = player
	}
	return s
}

func setDivorceFollowupFlag(s *State, id string, bit uint, on bool) {
	player := s.Players[id]
	marriageSetFlag(&player.Body.Flags, bit, on)
	s.Players[id] = player
}

func TestPlanAndApplyDivorceRequestAcceptClearsBothRelationships(t *testing.T) {
	s := divorceFollowupState()
	original := s
	proposal, err := s.PlanDivorce("alice")
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Action != DivorceRequest || proposal.ActorID != "alice" || proposal.ActorName != "Alice" || proposal.TargetID != "bob" || proposal.TargetName != "Bob" || !proposal.Changed {
		t.Fatalf("request proposal=%+v", proposal)
	}
	if !flag(proposal.AfterActorFlags[:], MarriageDivorcePendingFlag) || len(proposal.Events) != 1 || proposal.Events[0].RecipientID != "bob" || proposal.Events[0].RecipientName != "Bob" {
		t.Fatalf("request projection=%+v", proposal)
	}
	if !reflect.DeepEqual(s, original) {
		t.Fatal("planning mutated the source snapshot")
	}
	next, result, err := s.ApplyDivorce(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != DivorceRequest || result.ActorID != "alice" || result.TargetID != "bob" || result.Response != "당신은 당신의 배우자에게 이혼신청을 하였습니다.\r\n" || !result.Changed || result.Broadcast || len(result.Events) != 1 {
		t.Fatalf("request result=%+v", result)
	}
	nextAlice := next.Players["alice"].Body
	if !flag(nextAlice.Flags[:], MarriageDivorcePendingFlag) || !flag(nextAlice.Flags[:], MarriageActiveFlag) || nextAlice.Keys[MarriageSpouseKeyIndex] != "mBob" {
		t.Fatalf("request actor=%+v", nextAlice)
	}
	if !reflect.DeepEqual(next.Players["bob"], s.Players["bob"]) {
		t.Fatal("request changed the recipient relationship")
	}

	accept, err := next.PlanDivorce("bob")
	if err != nil {
		t.Fatal(err)
	}
	if accept.Action != DivorceAccept || accept.ActorID != "bob" || accept.ActorName != "Bob" || accept.TargetID != "alice" || accept.TargetName != "Alice" || !accept.Broadcast || len(accept.Events) != 2 {
		t.Fatalf("accept proposal=%+v", accept)
	}
	if accept.Events[0].RecipientID != "alice" || !accept.Events[1].Broadcast || accept.Events[1].RecipientID != "" || !strings.Contains(accept.BroadcastText, "Bob님과 Alice님이 이혼을 하였습니다.") {
		t.Fatalf("accept events=%+v broadcast=%q", accept.Events, accept.BroadcastText)
	}
	married, accepted, err := next.ApplyDivorce(accept)
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Action != DivorceAccept || !accepted.Changed || !accepted.Broadcast || accepted.ActorID != "bob" || accepted.TargetID != "alice" || accepted.Response != "당신은 Alice님의 이혼신청을 받아들입니다.\r\n" {
		t.Fatalf("accepted result=%+v", accepted)
	}
	for id, wantKey := range map[string]string{"alice": "mBob", "bob": "mAlice"} {
		body := married.Players[id].Body
		if flag(body.Flags[:], MarriageActiveFlag) || flag(body.Flags[:], MarriagePendingFlag) || flag(body.Flags[:], MarriageDivorcePendingFlag) || body.Keys[MarriageSpouseKeyIndex] != wantKey {
			t.Fatalf("divorce acceptance did not clear/preserve %s: %+v", id, body)
		}
	}
	if !reflect.DeepEqual(s, original) {
		t.Fatal("ApplyDivorce mutated the original snapshot")
	}
}

func TestPlanDivorceNoTargetBranchesAreOrderedAndReceiptSafe(t *testing.T) {
	tests := []struct {
		name       string
		configure  func(*State)
		wantAction DivorceAction
		wantText   string
		wantChange bool
		wantFlags  func(LegacyMonster) bool
	}{
		{
			name: "pending marriage cancellation precedes spouse resolution",
			configure: func(s *State) {
				setDivorceFollowupFlag(s, "alice", MarriageActiveFlag, false)
				setDivorceFollowupFlag(s, "alice", MarriagePendingFlag, true)
				player := s.Players["alice"]
				player.Body.Keys[MarriageSpouseKeyIndex] = "not-a-valid-mkey"
				s.Players["alice"] = player
			},
			wantAction: DivorceCancelMarriage, wantText: MarriageCancelResponse, wantChange: true,
			wantFlags: func(body LegacyMonster) bool {
				return !flag(body.Flags[:], MarriagePendingFlag) && body.Keys[MarriageSpouseKeyIndex] == "not-a-valid-mkey"
			},
		},
		{
			name: "unmarried",
			configure: func(s *State) {
				player := s.Players["alice"]
				marriageSetFlag(&player.Body.Flags, MarriageActiveFlag, false)
				player.Body.Keys[MarriageSpouseKeyIndex] = ""
				s.Players["alice"] = player
			},
			wantAction: DivorceUnmarried, wantText: "당신은 결혼하지 않았습니다.\r\n", wantChange: false,
			wantFlags: func(body LegacyMonster) bool {
				return !flag(body.Flags[:], MarriageActiveFlag) && !flag(body.Flags[:], MarriagePendingFlag) && !flag(body.Flags[:], MarriageDivorcePendingFlag)
			},
		},
		{
			name: "divorce request cancellation",
			configure: func(s *State) {
				setDivorceFollowupFlag(s, "alice", MarriageActiveFlag, true)
				setDivorceFollowupFlag(s, "alice", MarriageDivorcePendingFlag, true)
			},
			wantAction: DivorceCancelRequest, wantText: "이혼 신청을 취소합니다.\r\n", wantChange: true,
			wantFlags: func(body LegacyMonster) bool {
				return flag(body.Flags[:], MarriageActiveFlag) && !flag(body.Flags[:], MarriageDivorcePendingFlag)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := divorceFollowupState()
			tt.configure(&s)
			before := s
			proposal, err := s.PlanDivorce("alice")
			if err != nil {
				t.Fatal(err)
			}
			if proposal.Action != tt.wantAction || proposal.Response != tt.wantText || proposal.Changed != tt.wantChange || proposal.TargetID != "" || len(proposal.Events) != 0 || proposal.Broadcast {
				t.Fatalf("proposal=%+v", proposal)
			}
			next, result, err := s.ApplyDivorce(proposal)
			if err != nil {
				t.Fatal(err)
			}
			if result.Action != tt.wantAction || result.Response != tt.wantText || result.Changed != tt.wantChange || len(result.Events) != 0 || !tt.wantFlags(next.Players["alice"].Body) {
				t.Fatalf("next=%+v result=%+v", next.Players["alice"].Body, result)
			}
			if !reflect.DeepEqual(before, s) {
				t.Fatal("planning mutated the source snapshot")
			}
		})
	}
}

func TestPlanDivorceOfflineOrMissingSpouseFailsClosedWithoutMutation(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "offline", true: "missing"}[missing], func(t *testing.T) {
			s := divorceFollowupState()
			if missing {
				delete(s.Players, "bob")
				room := s.Rooms[1]
				room.PlayerIDs = []string{"alice"}
				s.Rooms[1] = room
			} else {
				bob := s.Players["bob"]
				bob.Online = false
				s.Players["bob"] = bob
				room := s.Rooms[1]
				room.PlayerIDs = []string{"alice"}
				s.Rooms[1] = room
			}
			before := s
			_, err := s.PlanDivorce("alice")
			if !errors.Is(err, ErrDivorceSpouseUnavailable) || !errors.Is(err, ErrDivorcePlayerStoreUnavailable) {
				t.Fatalf("err=%v", err)
			}
			if !reflect.DeepEqual(s, before) {
				t.Fatal("offline/missing spouse plan mutated state")
			}
		})
	}
}

func TestApplyDivorceRejectsStaleAndMalformedRelationship(t *testing.T) {
	s := divorceFollowupState()
	proposal, err := s.PlanDivorce("alice")
	if err != nil {
		t.Fatal(err)
	}
	changed := s
	player := changed.Players["alice"]
	player.Body.Gold++
	changed.Players["alice"] = player
	if _, _, err := changed.ApplyDivorce(proposal); !errors.Is(err, ErrDivorceStaleProposal) {
		t.Fatalf("stale err=%v", err)
	}

	broken := divorceFollowupState()
	player = broken.Players["alice"]
	player.Body.Keys[MarriageSpouseKeyIndex] = "broken"
	broken.Players["alice"] = player
	if _, err := broken.PlanDivorce("alice"); !errors.Is(err, ErrDivorceSpouseKeyInvalid) {
		t.Fatalf("broken spouse key err=%v", err)
	}

	broken = divorceFollowupState()
	player = broken.Players["bob"]
	player.Body.Keys[MarriageSpouseKeyIndex] = "mOther"
	broken.Players["bob"] = player
	if _, err := broken.PlanDivorce("alice"); !errors.Is(err, ErrDivorceStateInvalid) {
		t.Fatalf("broken reciprocal relation err=%v", err)
	}
}

func TestPlanMarriageSendFailsClosedAtCanonicalAndRendererBoundaries(t *testing.T) {
	base := divorceFollowupState()
	if _, err := base.PlanMarriageSend("alice", "hello"); !errors.Is(err, ErrMarriageSendDescriptorFormat) {
		t.Fatalf("descriptor boundary err=%v", err)
	}

	plecho := base.clone()
	actor := plecho.Players["alice"]
	actor.Body.Flags[playerLocalEchoFlag/8] |= 1 << (playerLocalEchoFlag % 8)
	plecho.Players["alice"] = actor
	if _, err := plecho.PlanMarriageSend("alice", "hello"); !errors.Is(err, ErrMarriageSendPLECHOUnsupported) {
		t.Fatalf("PLECHO boundary err=%v", err)
	}

	empty := base.clone()
	if _, err := empty.PlanMarriageSend("alice", "  \t"); !errors.Is(err, ErrMarriageSendMessageEmpty) {
		t.Fatalf("empty message err=%v", err)
	}
	invalid := base.clone()
	if _, err := invalid.PlanMarriageSend("alice", "bad\x1b"); !errors.Is(err, ErrMarriageSendMessageInvalid) {
		t.Fatalf("invalid message err=%v", err)
	}

	unmarried := base.clone()
	actor = unmarried.Players["alice"]
	marriageSetFlag(&actor.Body.Flags, MarriageActiveFlag, false)
	unmarried.Players["alice"] = actor
	if _, err := unmarried.PlanMarriageSend("alice", "hello"); !errors.Is(err, ErrMarriageSendNotMarried) {
		t.Fatalf("unmarried err=%v", err)
	}

	offline := base.clone()
	actor = offline.Players["bob"]
	actor.Online = false
	offline.Players["bob"] = actor
	room := offline.Rooms[1]
	room.PlayerIDs = []string{"alice"}
	offline.Rooms[1] = room
	if _, err := offline.PlanMarriageSend("alice", "hello"); !errors.Is(err, ErrMarriageSendSpouseUnavailable) {
		t.Fatalf("offline spouse err=%v", err)
	}
}

func TestMarriageSendProposalAndResultTypesDoNotAliasMarriageContract(t *testing.T) {
	var _ MarriageProposal
	var _ MarriageResult
	var _ DivorceProposal
	var _ DivorceResult
	var _ MarriageSendProposal
	var _ MarriageSendResult
	if reflect.TypeOf(MarriageProposal{}).Name() == reflect.TypeOf(DivorceProposal{}).Name() || reflect.TypeOf(MarriageResult{}).Name() == reflect.TypeOf(DivorceResult{}).Name() {
		t.Fatal("follow-up contracts collide with existing marriage types")
	}
}
