package world

import (
	"errors"
	"reflect"
	"testing"
)

func familyWarFlag(body *LegacyMonster, bit uint, enabled bool) {
	if enabled {
		body.Flags[bit/8] |= 1 << (bit % 8)
		return
	}
	body.Flags[bit/8] &^= 1 << (bit % 8)
}

func familyWarFixture(t *testing.T) (State, FamilyCatalog) {
	t.Helper()
	dragon := LegacyMonster{Name: "Boss", Type: 0, Class: 4, RoomID: 1}
	dragon.Daily[FamilyDailySlot].Max = 2
	familyWarFlag(&dragon, FamilyMemberFlag, true)
	familyWarFlag(&dragon, FamilyBossFlag, true)
	tiger := LegacyMonster{Name: "Tiger", Type: 0, Class: 4, RoomID: 1}
	tiger.Daily[FamilyDailySlot].Max = 3
	familyWarFlag(&tiger, FamilyMemberFlag, true)
	familyWarFlag(&tiger, FamilyBossFlag, true)
	s := State{
		Version: 1,
		Rooms:   map[int16]RoomState{1: {Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}}, PlayerIDs: []string{"dragon-boss", "tiger-boss"}}},
		Players: map[string]PlayerState{
			"dragon-boss": {Body: dragon, Online: true},
			"tiger-boss":  {Body: tiger, Online: true},
		},
		War: &FamilyWar{},
	}
	catalog := FamilyCatalog{Families: map[int16]FamilyDefinition{
		2: {ID: 2, Name: "청룡", Boss: "Boss"},
		3: {ID: 3, Name: "백호", Boss: "Tiger"},
	}}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	return s, catalog
}

func TestFamilyWarDeclareCancelAcceptAndReplay(t *testing.T) {
	s, catalog := familyWarFixture(t)
	original := s.clone()

	proposal, err := s.PlanFamilyWar("dragon-boss", "백호", catalog)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Action != FamilyWarDeclare || !proposal.Changed || proposal.After != (FamilyWar{CalledBy: 2, CalledAgainst: 3}) {
		t.Fatalf("declare=%+v", proposal)
	}
	if proposal.Response != FamilyWarDeclareText("청룡", "백호") || len(proposal.Events) != 1 || !proposal.Events[0].AllPlayers {
		t.Fatalf("declare events=%+v", proposal)
	}
	if !reflect.DeepEqual(s, original) {
		t.Fatal("planning mutated state")
	}

	next, result, err := s.ApplyFamilyWar(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if next.War == nil || *next.War != proposal.After || !result.Changed {
		t.Fatalf("after declare war=%+v result=%+v", next.War, result)
	}
	if !reflect.DeepEqual(s, original) {
		t.Fatal("apply mutated source")
	}
	if _, _, err := next.ApplyFamilyWar(proposal); !errors.Is(err, ErrFamilyWarStaleProposal) {
		t.Fatalf("declare replay err=%v", err)
	}

	accept, err := next.PlanFamilyWar("tiger-boss", "청룡", catalog)
	if err != nil {
		t.Fatal(err)
	}
	if accept.Action != FamilyWarAccept || accept.After.Active != 3*16+2 || accept.After.CalledBy != 2 || accept.After.CalledAgainst != 3 {
		t.Fatalf("accept=%+v", accept)
	}
	if accept.Events[0].AllPlayers || accept.Response != FamilyWarAcceptText("백호") {
		t.Fatalf("accept events=%+v", accept)
	}
	wartime, _, err := next.ApplyFamilyWar(accept)
	if err != nil {
		t.Fatal(err)
	}
	if wartime.War.Active != 3*16+2 {
		t.Fatalf("active=%+v", wartime.War)
	}
	already, err := wartime.PlanFamilyWar("dragon-boss", "백호", catalog)
	if err != nil || already.Changed || already.Response != FamilyWarAlreadyResponse {
		t.Fatalf("already=%+v err=%v", already, err)
	}
}

func TestFamilyWarCancelByOriginalCaller(t *testing.T) {
	s, catalog := familyWarFixture(t)
	declared, _, err := s.ApplyFamilyWar(mustPlanFamilyWar(t, s, "dragon-boss", "백호", catalog))
	if err != nil {
		t.Fatal(err)
	}
	cancel, err := declared.PlanFamilyWar("dragon-boss", "백호", catalog)
	if err != nil {
		t.Fatal(err)
	}
	if cancel.Action != FamilyWarCancel || cancel.After != (FamilyWar{}) || cancel.Events[0].AllPlayers {
		t.Fatalf("cancel=%+v", cancel)
	}
	next, _, err := declared.ApplyFamilyWar(cancel)
	if err != nil {
		t.Fatal(err)
	}
	if next.War == nil || *next.War != (FamilyWar{}) {
		t.Fatalf("after cancel=%+v", next.War)
	}
}

func TestFamilyWarGates(t *testing.T) {
	s, catalog := familyWarFixture(t)
	if _, err := s.PlanFamilyWar("dragon-boss", "백호", FamilyCatalog{}); !errors.Is(err, ErrFamilyCatalogUnavailable) {
		t.Fatalf("catalog err=%v", err)
	}
	unmigrated := s.clone()
	unmigrated.War = nil
	if _, err := unmigrated.PlanFamilyWar("dragon-boss", "백호", catalog); !errors.Is(err, ErrFamilyWarUnresolved) {
		t.Fatalf("unmigrated err=%v", err)
	}

	member := s.Players["dragon-boss"]
	familyWarFlag(&member.Body, FamilyBossFlag, false)
	s.Players["dragon-boss"] = member
	gated, err := s.PlanFamilyWar("dragon-boss", "백호", catalog)
	if err != nil || gated.Changed || gated.Response != FamilyWarNotBossResponse {
		t.Fatalf("not boss=%+v err=%v", gated, err)
	}

	s, catalog = familyWarFixture(t)
	prompt, err := s.PlanFamilyWar("dragon-boss", "", catalog)
	if err != nil || prompt.Changed || prompt.Response != FamilyWarTargetPrompt {
		t.Fatalf("prompt=%+v err=%v", prompt, err)
	}
	unknown, err := s.PlanFamilyWar("dragon-boss", "현무", catalog)
	if err != nil || unknown.Changed || unknown.Response != FamilyWarUnknownResponse {
		t.Fatalf("unknown=%+v err=%v", unknown, err)
	}
	self, err := s.PlanFamilyWar("dragon-boss", "청룡", catalog)
	if err != nil || self.Changed || self.Response != FamilyWarSelfResponse {
		t.Fatalf("self=%+v err=%v", self, err)
	}

	offline := s.clone()
	tiger := offline.Players["tiger-boss"]
	tiger.Online = false
	offline.Players["tiger-boss"] = tiger
	room := offline.Rooms[1]
	room.PlayerIDs = []string{"dragon-boss"}
	offline.Rooms[1] = room
	missing, err := offline.PlanFamilyWar("dragon-boss", "백호", catalog)
	if err != nil || missing.Changed || missing.Response != FamilyWarBossOfflineResponse("Tiger") {
		t.Fatalf("offline=%+v err=%v", missing, err)
	}

	pending := s.clone()
	pending.War = &FamilyWar{CalledBy: 2, CalledAgainst: 3}
	busy, err := pending.PlanFamilyWar("tiger-boss", "현무", catalog)
	if err != nil {
		t.Fatal(err)
	}
	if busy.Response == FamilyWarBusyResponse {
		t.Fatal("unknown target short-circuited before war machine")
	}
	busy, err = pending.PlanFamilyWar("tiger-boss", "청룡", catalog)
	if err != nil {
		t.Fatal(err)
	}
	if busy.Action == FamilyWarAccept {
		// tiger is CALLWAR2 and targeting CALLWAR1, that is accept
		if busy.After.Active != 3*16+2 {
			t.Fatalf("accept via gate fixture=%+v", busy)
		}
	}
}

func TestFamilyWarRejectsWrongAcceptTargetAndThirdParty(t *testing.T) {
	s, catalog := familyWarFixture(t)
	phoenix := LegacyMonster{Name: "Phoenix", Type: 0, Class: 4, RoomID: 1}
	phoenix.Daily[FamilyDailySlot].Max = 4
	familyWarFlag(&phoenix, FamilyMemberFlag, true)
	familyWarFlag(&phoenix, FamilyBossFlag, true)
	s.Players["phoenix-boss"] = PlayerState{Body: phoenix, Online: true}
	room := s.Rooms[1]
	room.PlayerIDs = append(room.PlayerIDs, "phoenix-boss")
	s.Rooms[1] = room
	catalog.Families[4] = FamilyDefinition{ID: 4, Name: "주작", Boss: "Phoenix"}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	declared, _, err := s.ApplyFamilyWar(mustPlanFamilyWar(t, s, "dragon-boss", "백호", catalog))
	if err != nil {
		t.Fatal(err)
	}
	wrong, err := declared.PlanFamilyWar("tiger-boss", "주작", catalog)
	if err != nil || wrong.Changed || wrong.Response != FamilyWarOtherChallengeResponse {
		t.Fatalf("wrong accept=%+v err=%v", wrong, err)
	}
	third, err := declared.PlanFamilyWar("phoenix-boss", "청룡", catalog)
	if err != nil || third.Changed || third.Response != FamilyWarBusyResponse {
		t.Fatalf("third=%+v err=%v", third, err)
	}
}

func mustPlanFamilyWar(t *testing.T, s State, actorID, target string, catalog FamilyCatalog) FamilyWarProposal {
	t.Helper()
	proposal, err := s.PlanFamilyWar(actorID, target, catalog)
	if err != nil || !proposal.Changed {
		t.Fatalf("plan err=%v proposal=%+v", err, proposal)
	}
	return proposal
}
