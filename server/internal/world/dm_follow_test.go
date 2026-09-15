package world

import (
	"errors"
	"reflect"
	"testing"
)

func dmFollowFixture(t *testing.T) State {
	t.Helper()
	s := State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"dm", "caretaker", "mortal"},
				NPCIDs:    []string{"orc-1", "wolf-1", "orc-2"},
			},
		},
		Players: map[string]PlayerState{
			"dm":        {Body: LegacyMonster{Name: "운영자", Type: 0, Class: playerDMClass, RoomID: 1}, Online: true},
			"caretaker": {Body: LegacyMonster{Name: "관리자", Type: 0, Class: playerCaretakerClass, RoomID: 1}, Online: true},
			"mortal":    {Body: LegacyMonster{Name: "Alice", Type: 0, Class: 4, RoomID: 1}, Online: true},
		},
		NPCs: map[string]NPCState{
			"orc-1":  {Body: LegacyMonster{Name: "오크", Type: 1, RoomID: 1}},
			"wolf-1": {Body: LegacyMonster{Name: "늑대", Type: 1, RoomID: 1}},
			"orc-2":  {Body: LegacyMonster{Name: "오크", Type: 1, RoomID: 1}},
		},
		ActiveNPCIDs: []string{},
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPlanDMFollowRejectsMortalAndCaretakerWithoutMutation(t *testing.T) {
	s := dmFollowFixture(t)
	mortal, err := s.PlanDMFollow("mortal", "*따르기", "오크", 1)
	if err != nil || mortal.Changed || mortal.Action != DMFollowUnknown || mortal.Response != DMFollowUnknownResponse("*따르기") {
		t.Fatalf("mortal=%+v err=%v", mortal, err)
	}
	caretaker, err := s.PlanDMFollow("caretaker", "*cfollow", "늑대", 1)
	if err != nil || caretaker.Changed || caretaker.Action != DMFollowPrompt || caretaker.Response != "" {
		t.Fatalf("caretaker=%+v err=%v", caretaker, err)
	}
	if _, _, err := s.ApplyDMFollow(mortal); !errors.Is(err, ErrDMFollowInvalidProposal) {
		t.Fatalf("apply mortal err=%v", err)
	}
	if s.NPCs["orc-1"].FollowingPlayerID != "" || PlayerFlagSet(s.NPCs["orc-1"].Body, npcDMFollowFlag) {
		t.Fatal("input mutated")
	}
}

func TestPlanDMFollowUsageMissingPermanentAndExactName(t *testing.T) {
	s := dmFollowFixture(t)
	usage, err := s.PlanDMFollow("dm", "*따르기", "", 1)
	if err != nil || usage.Changed || usage.Action != DMFollowUsage || usage.Response != DMFollowUsageResponse {
		t.Fatalf("usage=%+v err=%v", usage, err)
	}
	missing, err := s.PlanDMFollow("dm", "*따르기", "고블린", 1)
	if err != nil || missing.Changed || missing.Action != DMFollowMissing || missing.Response != DMFollowMissingResponse {
		t.Fatalf("missing=%+v err=%v", missing, err)
	}
	prefix, err := s.PlanDMFollow("dm", "*따르기", "오", 1)
	if err != nil || prefix.Action != DMFollowMissing {
		t.Fatalf("prefix=%+v err=%v", prefix, err)
	}
	perm := s.NPCs["wolf-1"]
	perm.Body.Flags[npcPermanentFlag/8] |= 1 << (npcPermanentFlag % 8)
	s.NPCs["wolf-1"] = perm
	blocked, err := s.PlanDMFollow("dm", "*따르기", "늑대", 1)
	if err != nil || blocked.Changed || blocked.Action != DMFollowPermanent || blocked.Response != DMFollowPermanentResponse {
		t.Fatalf("permanent=%+v err=%v", blocked, err)
	}
}

func TestPlanApplyDMFollowAttachesThenDetachesMDMFOL(t *testing.T) {
	s := dmFollowFixture(t)
	proposal, err := s.PlanDMFollow("dm", "*따르기", "늑대", 1)
	if err != nil || !proposal.Changed || proposal.Action != DMFollowAttach || proposal.NPCID != "wolf-1" {
		t.Fatalf("plan=%+v err=%v", proposal, err)
	}
	next, result, err := s.ApplyDMFollow(proposal)
	if err != nil || !result.Changed || result.Response != DMFollowAttachResponse("늑대") {
		t.Fatalf("apply=%+v err=%v", result, err)
	}
	npc := next.NPCs["wolf-1"]
	if npc.FollowingPlayerID != "dm" || !PlayerFlagSet(npc.Body, npcDMFollowFlag) {
		t.Fatalf("attached npc=%+v", npc)
	}
	if !reflect.DeepEqual(next.Players["dm"].NPCFollowerIDs, []string{"wolf-1"}) {
		t.Fatalf("followers=%v", next.Players["dm"].NPCFollowerIDs)
	}
	if s.NPCs["wolf-1"].FollowingPlayerID != "" {
		t.Fatal("input mutated")
	}

	detachPlan, err := next.PlanDMFollow("dm", "*cfollow", "늑대", 1)
	if err != nil || !detachPlan.Changed || detachPlan.Action != DMFollowDetach {
		t.Fatalf("detach plan=%+v err=%v", detachPlan, err)
	}
	cleared, detach, err := next.ApplyDMFollow(detachPlan)
	if err != nil || detach.Response != DMFollowDetachResponse("늑대") {
		t.Fatalf("detach=%+v err=%v", detach, err)
	}
	npc = cleared.NPCs["wolf-1"]
	if npc.FollowingPlayerID != "" || PlayerFlagSet(npc.Body, npcDMFollowFlag) || len(cleared.Players["dm"].NPCFollowerIDs) != 0 {
		t.Fatalf("cleared npc=%+v followers=%v", npc, cleared.Players["dm"].NPCFollowerIDs)
	}
}

func TestPlanDMFollowSelectsExactOccurrenceAndFailsClosedWithoutCanonicalNPCs(t *testing.T) {
	s := dmFollowFixture(t)
	second, err := s.PlanDMFollow("dm", "*따르기", "오크", 2)
	if err != nil || second.NPCID != "orc-2" || second.Action != DMFollowAttach {
		t.Fatalf("occurrence=%+v err=%v", second, err)
	}
	missingOcc, err := s.PlanDMFollow("dm", "*따르기", "오크", 3)
	if err != nil || missingOcc.Action != DMFollowMissing {
		t.Fatalf("missing occ=%+v err=%v", missingOcc, err)
	}

	unresolved := s
	unresolved.NPCs = nil
	unresolved.ActiveNPCIDs = nil
	unresolved.Rooms[1] = RoomState{
		Resource:  unresolved.Rooms[1].Resource,
		PlayerIDs: unresolved.Rooms[1].PlayerIDs,
	}
	if _, err := unresolved.PlanDMFollow("dm", "*따르기", "오크", 1); !errors.Is(err, ErrDMFollowNPCUnresolved) {
		t.Fatalf("unresolved err=%v", err)
	}
}

func TestApplyDMFollowRejectsStaleProposal(t *testing.T) {
	s := dmFollowFixture(t)
	proposal, err := s.PlanDMFollow("dm", "*따르기", "늑대", 1)
	if err != nil {
		t.Fatal(err)
	}
	npc := s.NPCs["wolf-1"]
	npc.Body.Name = "와르그"
	s.NPCs["wolf-1"] = npc
	if _, _, err := s.ApplyDMFollow(proposal); !errors.Is(err, ErrDMFollowStaleProposal) {
		t.Fatalf("stale err=%v", err)
	}
}

func TestApplyDMFollowDoesNotRecommitAfterLeavePlayer(t *testing.T) {
	s := resolveDMFollowLogoutDomain(dmFollowFixture(t))
	proposal, err := s.PlanDMFollow("dm", "*따르기", "늑대", 1)
	if err != nil {
		t.Fatal(err)
	}
	attached, _, err := s.ApplyDMFollow(proposal)
	if err != nil {
		t.Fatal(err)
	}
	loggedOut, _, err := attached.LeavePlayer("dm")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := loggedOut.ApplyDMFollow(proposal); err == nil {
		t.Fatal("logout left a committable MDMFOL proposal")
	}
	if loggedOut.NPCs["wolf-1"].FollowingPlayerID != "" || PlayerFlagSet(loggedOut.NPCs["wolf-1"].Body, npcDMFollowFlag) {
		t.Fatal("failed apply after logout mutated leftover MDMFOL")
	}
}
