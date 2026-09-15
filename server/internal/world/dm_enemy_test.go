package world

import (
	"errors"
	"reflect"
	"testing"
)

func dmEnemyFixture(t *testing.T) State {
	t.Helper()
	s := State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"dm", "subdm", "caretaker", "mortal", "alice"},
				NPCIDs:    []string{"orc-first", "orc-second", "guardian"},
			},
		},
		Players: map[string]PlayerState{
			"dm":        {Body: LegacyMonster{Name: "운영자", Type: 0, Class: playerDMClass, RoomID: 1}, Online: true},
			"subdm":     {Body: LegacyMonster{Name: "부운영자", Type: 0, Class: playerSubDMClass, RoomID: 1}, Online: true},
			"caretaker": {Body: LegacyMonster{Name: "관리자", Type: 0, Class: playerCaretakerClass, RoomID: 1}, Online: true},
			"mortal":    {Body: LegacyMonster{Name: "Alice", Type: 0, Class: 4, RoomID: 1}, Online: true},
			"alice":     {Body: LegacyMonster{Name: "Alice", Type: 0, Class: 4, RoomID: 1}, Online: true},
		},
		NPCs: map[string]NPCState{
			"orc-first": {
				Body: LegacyMonster{Name: "오크", Type: 1, RoomID: 1},
				Enemies: []NPCEnemy{
					{Target: EntityRef{Kind: "player", ID: "alice"}},
					{Target: EntityRef{Kind: "npc", ID: "guardian"}},
				},
			},
			"orc-second": {
				Body:    LegacyMonster{Name: "오크", Type: 1, RoomID: 1},
				Enemies: []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "mortal"}}},
			},
			"guardian": {
				Body:    LegacyMonster{Name: "수호자", Type: 1, RoomID: 1},
				Enemies: []NPCEnemy{},
			},
		},
		ActiveNPCIDs: []string{},
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPlanApplyDMEnemyUsesCanonicalRoomOrderAndStoredRelationOrder(t *testing.T) {
	s := dmEnemyFixture(t)
	proposal, err := s.PlanDMEnemy("dm", "*enemy", "오크", 2)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Action != DMEnemyList || proposal.TargetID != "orc-second" || proposal.TargetName != "오크" || proposal.Occurrence != 2 || proposal.Changed {
		t.Fatalf("proposal=%+v", proposal)
	}
	if !reflect.DeepEqual(proposal.EnemyIDs, []string{"mortal"}) || !reflect.DeepEqual(proposal.EnemyNames, []string{"Alice"}) {
		t.Fatalf("relation order=%v names=%v", proposal.EnemyIDs, proposal.EnemyNames)
	}
	if proposal.Response != "오크의 적들:\nAlice.\n" {
		t.Fatalf("response=%q", proposal.Response)
	}

	next, result, err := s.ApplyDMEnemy(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(next, s) || !reflect.DeepEqual(result.EnemyIDs, proposal.EnemyIDs) || result.Response != proposal.Response || result.Changed {
		t.Fatalf("next=%+v result=%+v source=%+v", next, result, s)
	}
	if response, err := s.EnemyList("dm", "*적", "오크", 1); err != nil || response != "오크의 적들:\nAlice.\n수호자.\n" {
		t.Fatalf("first response=%q err=%v", response, err)
	}
}

func TestPlanDMEnemyAllowsSubDMAndKeepsUnauthorizedNoOpOpaque(t *testing.T) {
	s := dmEnemyFixture(t)
	for _, actorID := range []string{"subdm"} {
		proposal, err := s.PlanDMEnemy(actorID, "*적", "오크", 1)
		if err != nil || proposal.Action != DMEnemyList || proposal.TargetID != "orc-first" {
			t.Fatalf("authorized actor=%s proposal=%+v err=%v", actorID, proposal, err)
		}
	}

	valid := dmEnemyFixture(t)
	for _, actorID := range []string{"caretaker", "mortal"} {
		proposal, err := valid.PlanDMEnemy(actorID, "*enemy", "오크", 1)
		if err != nil || proposal.Action != DMEnemyUnknown || proposal.Response != DMEnemyUnknownResponse("*enemy") || proposal.TargetID != "" || len(proposal.EnemyNames) != 0 || proposal.Changed {
			t.Fatalf("unauthorized actor=%s proposal=%+v err=%v", actorID, proposal, err)
		}
		next, result, err := valid.ApplyDMEnemy(proposal)
		if err != nil || !reflect.DeepEqual(next, valid) || result.Response != proposal.Response || result.TargetName != "" {
			t.Fatalf("unauthorized apply actor=%s next=%+v result=%+v err=%v", actorID, next, result, err)
		}
	}
}

func TestPlanDMEnemyResolvesPlayerAndNPCReferencesAndKnownEmptyRelation(t *testing.T) {
	s := dmEnemyFixture(t)
	proposal, err := s.PlanDMEnemy("dm", "*enemy", "오크", 1)
	if err != nil || !reflect.DeepEqual(proposal.EnemyNames, []string{"Alice", "수호자"}) {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	empty, err := s.PlanDMEnemy("dm", "*enemy", "수호자", 1)
	if err != nil || empty.Response != "수호자의 적들:\n없음.\n" || len(empty.EnemyNames) != 0 {
		t.Fatalf("empty=%+v err=%v", empty, err)
	}
}

func TestPlanDMEnemyFailsClosedForMissingOrUnresolvedCanonicalState(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*State)
		want   error
	}{
		{name: "missing target", mutate: func(*State) {}, want: ErrDMEnemyTargetAbsent},
		{name: "nil NPC map", mutate: func(s *State) { s.NPCs = nil }, want: ErrDMEnemyNPCUnresolved},
		{name: "nil room order", mutate: func(s *State) { room := s.Rooms[1]; room.NPCIDs = nil; s.Rooms[1] = room }, want: ErrDMEnemyNPCUnresolved},
		{name: "nil relation list", mutate: func(s *State) { npc := s.NPCs["orc-first"]; npc.Enemies = nil; s.NPCs["orc-first"] = npc }, want: ErrDMEnemyRelationsUnresolved},
		{name: "missing relation identity", mutate: func(s *State) {
			npc := s.NPCs["orc-first"]
			npc.Enemies = []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "missing"}}}
			s.NPCs["orc-first"] = npc
		}, want: ErrDMEnemyStateInvalid},
		{name: "legacy room fallback", mutate: func(s *State) {
			room := s.Rooms[1]
			room.Resource.Monsters = []LegacyMonster{{Name: "legacy-only", Type: 1, RoomID: 1}}
			s.Rooms[1] = room
		}, want: ErrDMEnemyStateInvalid},
		{name: "player target is not NPC", mutate: func(s *State) {
			room := s.Rooms[1]
			room.NPCIDs = []string{"guardian"}
			s.Rooms[1] = room
			s.NPCs = map[string]NPCState{"guardian": s.NPCs["guardian"]}
		}, want: ErrDMEnemyTargetAbsent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := dmEnemyFixture(t)
			test.mutate(&s)
			target := "오크"
			if test.name == "missing target" {
				target = "고블린"
			} else if test.name == "player target is not NPC" {
				target = "Alice"
			}
			if _, err := s.PlanDMEnemy("dm", "*enemy", target, 1); !errors.Is(err, test.want) {
				t.Fatalf("err=%v want errors.Is(%v)", err, test.want)
			}
		})
	}
	if _, err := dmEnemyFixture(t).PlanDMEnemy("dm", "*bogus", "오크", 1); !errors.Is(err, ErrDMEnemyInvalidVerb) {
		t.Fatalf("invalid verb err=%v", err)
	}
	if _, err := dmEnemyFixture(t).PlanDMEnemy("dm", "*enemy", "오크", 0); !errors.Is(err, ErrDMEnemyInvalidOccurrence) {
		t.Fatalf("invalid occurrence err=%v", err)
	}
	if _, err := dmEnemyFixture(t).PlanDMEnemy("dm", "*enemy", "", 1); !errors.Is(err, ErrDMEnemyTargetRequired) {
		t.Fatalf("missing name err=%v", err)
	}
}

func TestApplyDMEnemyRejectsStaleProjection(t *testing.T) {
	s := dmEnemyFixture(t)
	proposal, err := s.PlanDMEnemy("dm", "*enemy", "오크", 1)
	if err != nil {
		t.Fatal(err)
	}
	npc := s.NPCs["orc-first"]
	npc.Enemies = []NPCEnemy{}
	s.NPCs["orc-first"] = npc
	if _, _, err := s.ApplyDMEnemy(proposal); !errors.Is(err, ErrDMEnemyStaleProposal) {
		t.Fatalf("stale err=%v", err)
	}
}
