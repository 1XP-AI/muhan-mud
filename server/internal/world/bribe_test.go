package world

import (
	"reflect"
	"strings"
	"testing"
)

func bribeFixture(t *testing.T) State {
	t.Helper()
	var hidden [8]byte
	hidden[npcInvisibleFlag/8] |= 1 << (npcInvisibleFlag % 8)
	s := State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"actor"},
				NPCIDs:    []string{"guard-one", "guard-two", "hidden"},
			},
		},
		Players: map[string]PlayerState{
			"actor": {Body: LegacyMonster{Name: "Alice", Type: 0, RoomID: 1, Gold: 500}, Online: true},
		},
		NPCs: map[string]NPCState{
			"guard-one": {Body: LegacyMonster{Name: "Guard", Type: 1, RoomID: 1, Level: 8, Gold: 20}},
			"guard-two": {Body: LegacyMonster{Name: "Guard", Type: 1, RoomID: 1, Level: 0, Gold: 30}},
			"hidden":    {Body: LegacyMonster{Name: "Guard", Type: 1, RoomID: 1, Flags: hidden}},
		},
		ActiveNPCIDs: []string{"guard-one", "guard-two", "hidden"},
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestParseBribeAmountRejectsMalformedAndBounds(t *testing.T) {
	for _, tc := range []struct {
		token string
		want  int32
		ok    bool
	}{
		{token: "1냥", want: 1, ok: true},
		{token: "2147483647냥", want: 2147483647, ok: true},
		{token: "0냥"},
		{token: "-1냥"},
		{token: "+1냥"},
		{token: "1"},
		{token: "2147483648냥"},
		{token: "1냥냥"},
		{token: " 냥"},
	} {
		got, err := ParseBribeAmount(tc.token)
		if tc.ok {
			if err != nil || got != tc.want {
				t.Fatalf("ParseBribeAmount(%q)=%d,%v want %d,nil", tc.token, got, err, tc.want)
			}
		} else if err == nil {
			t.Fatalf("ParseBribeAmount(%q) accepted %d", tc.token, got)
		}
	}
}

func TestPlanAndApplyBribeStaysCreditsNPCAndUsesOrderedOccurrence(t *testing.T) {
	s := bribeFixture(t)
	original := s
	proposal, err := s.PlanBribe("actor", "gUaRd", 1, 100)
	if err != nil || !proposal.Found || proposal.NPCID != "guard-one" || !proposal.Stayed {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	next, result, err := s.ApplyBribe(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "bribe" || result.NPCID != "guard-one" || !result.Stayed || result.Amount != 100 || result.GoldBefore != 500 || result.GoldAfter != 400 || !result.Broadcast || result.Event == nil {
		t.Fatalf("result=%+v", result)
	}
	if next.Players["actor"].Body.Gold != 400 || next.NPCs["guard-one"].Body.Gold != 120 || next.NPCs["guard-two"].Body.Gold != 30 {
		t.Fatalf("money state=%+v", next)
	}
	if !strings.Contains(result.Response, "Guard") || result.Event.ExcludeActorID != "actor" || result.Event.RoomID != 1 {
		t.Fatalf("response/event=%q/%+v", result.Response, result.Event)
	}
	if !reflect.DeepEqual(s, original) {
		t.Fatal("planning/apply mutated source snapshot")
	}
	if err := next.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestApplyBribeLeavesNPCWithoutRefundAndRemovesAllCanonicalIndexes(t *testing.T) {
	s := bribeFixture(t)
	proposal, err := s.PlanBribe("actor", "Guard", 2, 1)
	if err != nil || !proposal.Found || proposal.NPCID != "guard-two" || proposal.Stayed {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	next, result, err := s.ApplyBribe(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if result.Stayed || result.GoldBefore != 500 || result.GoldAfter != 499 || result.Event == nil {
		t.Fatalf("result=%+v", result)
	}
	actor := next.Players["actor"]
	_, npcPresent := next.NPCs["guard-two"]
	if actor.Body.Gold != 499 || npcPresent || containsString(next.Rooms[1].NPCIDs, "guard-two") || containsString(next.ActiveNPCIDs, "guard-two") {
		t.Fatalf("leave state=%+v", next)
	}
	if err := next.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestPlanBribeHonorsVisibilityAndRejectsStaleOrUnresolvedLeave(t *testing.T) {
	s := bribeFixture(t)
	proposal, err := s.PlanBribe("actor", "Guard", 3, 1)
	if err != nil || !proposal.Rejected || proposal.Found || proposal.Response != "그런 것은 여기 없습니다.\r\n" {
		t.Fatalf("hidden target result=%+v err=%v", proposal, err)
	}
	actor := s.Players["actor"]
	actor.Body.Flags[playerDetectFlag/8] |= 1 << (playerDetectFlag % 8)
	s.Players["actor"] = actor
	proposal, err = s.PlanBribe("actor", "Guard", 3, 1)
	if err != nil || !proposal.Found || proposal.NPCID != "hidden" {
		t.Fatalf("detected target proposal=%+v err=%v", proposal, err)
	}
	changed := s.clone()
	changedActor := changed.Players["actor"]
	changedActor.Body.Gold++
	changed.Players["actor"] = changedActor
	if _, _, err := changed.ApplyBribe(proposal); err == nil {
		t.Fatal("stale bribe proposal accepted")
	}

	leave, err := s.PlanBribe("actor", "Guard", 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	unresolved := s.clone()
	unresolved.ActiveNPCIDs = nil
	if _, _, err := unresolved.ApplyBribe(leave); err == nil {
		t.Fatal("leave without active ordering accepted")
	}
}
