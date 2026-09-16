package world

import (
	"reflect"
	"testing"
)

func npcChaseBody(name string, roomID int16, dex byte) LegacyMonster {
	return LegacyMonster{
		Name:      name,
		Type:      1,
		RoomID:    roomID,
		Stats:     [5]byte{0, dex},
		HPMax:     40,
		HPCurrent: 40,
	}
}

func npcChaseFixture() State {
	n1 := npcChaseBody("Alpha", 1, 5)
	n1.Flags[1] |= 1 << (npcFollowFlag % 8)
	n1.Flags[0] |= 1 << (npcPermanentFlag % 8)
	n2 := npcChaseBody("Beta", 1, 5)
	n3 := npcChaseBody("Gamma", 1, 5)
	dst := npcChaseBody("Zulu", 2, 5)
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource: LegacyRoom{
					LegacyRoomHeader:  LegacyRoomHeader{ID: 1},
					PermanentMonsters: [10]LegacyTimer{{Misc: 77, LastTime: 90, Interval: 10}},
				},
				NPCIDs: []string{"n1", "n2", "n3"},
			},
			2: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 2}},
				PlayerIDs: []string{"a"},
				NPCIDs:    []string{"dst"},
			},
		},
		Players: map[string]PlayerState{
			"a": {Body: LegacyMonster{Name: "Renamed Player", Type: 0, RoomID: 2, Stats: [5]byte{0, 5}}, Online: true},
		},
		NPCs: map[string]NPCState{
			"n1":  {Body: n1, Enemies: []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "a"}}}, PermanentOrigin: &NPCPermanentOrigin{RoomID: 1, Slot: 0}},
			"n2":  {Body: n2, Enemies: []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "a"}}}},
			"n3":  {Body: n3, Enemies: []NPCEnemy{}},
			"dst": {Body: dst, Enemies: []NPCEnemy{}},
		},
		ActiveNPCIDs: []string{"n3", "dst"},
	}
}

func TestPlanAndApplyNPCFollowerChasePreservesCOrderAndAtomicState(t *testing.T) {
	s := npcChaseFixture()
	before := s.clone()
	plan, err := s.PlanNPCFollowerChase(NPCFollowerChaseInput{ActorID: "a", SourceRoomID: 1, Now: 100}, func(low, high int) int {
		if low != 1 || high != 50 {
			t.Fatalf("unexpected chase roll %d..%d", low, high)
		}
		return 15 // 15 - player dexterity + NPC dexterity == 15; C succeeds on <=.
	})
	if err != nil || len(plan.Moves) != 2 || plan.Moves[0].NPCID != "n1" || plan.Moves[1].NPCID != "n2" {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	if !plan.Moves[0].PermanentTimerWrite || plan.Moves[1].PermanentOrigin != nil {
		t.Fatalf("permanent plan metadata=%+v", plan.Moves)
	}
	if !reflect.DeepEqual(s, before) {
		t.Fatal("planning mutated source state")
	}

	next, err := s.ApplyNPCFollowerChase(plan)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s, before) {
		t.Fatal("apply mutated source state")
	}
	if got, want := next.Rooms[1].NPCIDs, []string{"n3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("source NPC order=%v want=%v", got, want)
	}
	if got, want := next.Rooms[2].NPCIDs, []string{"n1", "n2", "dst"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("destination NPC order=%v want=%v", got, want)
	}
	if got, want := next.ActiveNPCIDs, []string{"n2", "n1", "n3", "dst"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("active order=%v want=%v", got, want)
	}
	n1 := next.NPCs["n1"]
	if n1.Body.RoomID != 2 || flag(n1.Body.Flags[:], npcPermanentFlag) || n1.PermanentOrigin != nil {
		t.Fatalf("permanent NPC not moved/cleared: %+v", next.NPCs["n1"])
	}
	if next.NPCs["n2"].Body.RoomID != 2 {
		t.Fatalf("ordinary NPC not moved: %+v", next.NPCs["n2"])
	}
	if got := next.Rooms[1].Resource.PermanentMonsters[0].LastTime; got != 100 {
		t.Fatalf("origin timer=%d want=100", got)
	}
}

func TestPlanNPCFollowerChaseUsesFirstEnemyIDNotPlayerName(t *testing.T) {
	s := npcChaseFixture()
	for id, npc := range s.NPCs {
		if id == "n3" || id == "dst" {
			continue
		}
		npc.Enemies = []NPCEnemy{{Target: EntityRef{Kind: "npc", ID: "dst"}}, {Target: EntityRef{Kind: "player", ID: "a"}}}
		s.NPCs[id] = npc
	}
	plan, err := s.PlanNPCFollowerChase(NPCFollowerChaseInput{ActorID: "a", SourceRoomID: 1, Now: 100}, func(int, int) int { return 1 })
	if err != nil || len(plan.Moves) != 0 {
		t.Fatalf("first enemy was not authoritative: plan=%+v err=%v", plan, err)
	}

	for id, npc := range s.NPCs {
		if id == "n3" || id == "dst" {
			continue
		}
		npc.Enemies = []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "a"}}}
		s.NPCs[id] = npc
	}
	plan, err = s.PlanNPCFollowerChase(NPCFollowerChaseInput{ActorID: "a", SourceRoomID: 1, Now: 100}, func(int, int) int { return 1 })
	if err != nil || len(plan.Moves) != 2 {
		t.Fatalf("ID target did not chase after display-name change: plan=%+v err=%v", plan, err)
	}
}

func TestPlanNPCFollowerChaseMatchesCommand2VisibilityPredicate(t *testing.T) {
	tests := []struct {
		name       string
		npcFlags   []int
		playerFlag int
		wantMoves  int
	}{
		{name: "visible non-following NPC still follows command2", wantMoves: 1},
		{name: "invisible player blocks ordinary NPC", playerFlag: npcInvisibleFlag, wantMoves: 0},
		{name: "MFOLLO bypasses invisible player gate", npcFlags: []int{npcFollowFlag}, playerFlag: npcInvisibleFlag, wantMoves: 1},
		{name: "detect-invisible bypasses invisible player gate", npcFlags: []int{21}, playerFlag: npcInvisibleFlag, wantMoves: 1},
		{name: "MFOLLO bypasses DM invisible player gate", npcFlags: []int{npcFollowFlag}, playerFlag: npcPlayerDMInvisible, wantMoves: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := npcChaseFixture()
			npc := s.NPCs["n2"]
			for _, bit := range tc.npcFlags {
				npc.Body.Flags[bit/8] |= 1 << (bit % 8)
			}
			s.NPCs["n2"] = npc
			player := s.Players["a"]
			if tc.playerFlag != 0 {
				player.Body.Flags[tc.playerFlag/8] |= 1 << (tc.playerFlag % 8)
			}
			s.Players["a"] = player
			plan, err := s.PlanNPCFollowerChase(NPCFollowerChaseInput{ActorID: "a", SourceRoomID: 1, Now: 100}, func(int, int) int { return 1 })
			if err != nil {
				t.Fatal(err)
			}
			// n1 is always a visible candidate unless the player is DM invisible;
			// this assertion isolates n2's result by making n1 non-candidate.
			got := 0
			for _, move := range plan.Moves {
				if move.NPCID == "n2" {
					got++
				}
			}
			if got != tc.wantMoves {
				t.Fatalf("n2 moves=%d want=%d plan=%+v", got, tc.wantMoves, plan)
			}
		})
	}
}

func TestNPCFollowerChaseActorTextPreservesCommand2LeadingNewline(t *testing.T) {
	if got, want := NPCFollowerChaseActorText("Alpha"), "\nAlpha가 당신을 따라옵니다.\r\n"; got != want {
		t.Fatalf("directional chase actor text=%q want=%q", got, want)
	}
	if got := NPCGoChaseActorText("Alpha"); got == NPCFollowerChaseActorText("Alpha") {
		t.Fatalf("directional and go chase actor text unexpectedly identical: %q", got)
	}
}

func TestPlanNPCFollowerChaseRejectsUnresolvedOrInvalidInputsAtomically(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*State)
		roll   func(int, int) int
	}{
		{name: "unresolved enemies", mutate: func(s *State) { npc := s.NPCs["n2"]; npc.Enemies = nil; s.NPCs["n2"] = npc }, roll: func(int, int) int { return 1 }},
		{name: "missing permanent origin", mutate: func(s *State) { npc := s.NPCs["n1"]; npc.PermanentOrigin = nil; s.NPCs["n1"] = npc }, roll: func(int, int) int { return 1 }},
		{name: "invalid random value", mutate: func(*State) {}, roll: func(int, int) int { return 51 }},
		{name: "unknown active ordering", mutate: func(s *State) { s.ActiveNPCIDs = nil }, roll: func(int, int) int { return 1 }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := npcChaseFixture()
			tc.mutate(&s)
			before := s.clone()
			plan, err := s.PlanNPCFollowerChase(NPCFollowerChaseInput{ActorID: "a", SourceRoomID: 1, Now: 100}, tc.roll)
			if err == nil || !reflect.DeepEqual(plan, NPCFollowerChaseProposal{}) || !reflect.DeepEqual(s, before) {
				t.Fatalf("plan=%+v err=%v source changed=%v", plan, err, !reflect.DeepEqual(s, before))
			}
		})
	}
}

func TestApplyNPCFollowerChaseRejectsStaleProposalWithoutPartialState(t *testing.T) {
	s := npcChaseFixture()
	plan, err := s.PlanNPCFollowerChase(NPCFollowerChaseInput{ActorID: "a", SourceRoomID: 1, Now: 100}, func(int, int) int { return 1 })
	if err != nil {
		t.Fatal(err)
	}
	changed := s.clone()
	npc := changed.NPCs["n1"]
	npc.Body.Name = "changed"
	changed.NPCs["n1"] = npc
	before := changed.clone()
	next, err := changed.ApplyNPCFollowerChase(plan)
	if err == nil || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(changed, before) {
		t.Fatalf("stale proposal leaked: next=%+v err=%v changed=%v", next, err, !reflect.DeepEqual(changed, before))
	}
}

func TestPlanNPCGoChaseRequiresMFOLLOSkipsMDMFOLAndUsesThreshold10(t *testing.T) {
	s := npcChaseFixture()
	mdm := s.NPCs["n2"]
	mdm.Body.Flags[npcFollowFlag/8] |= 1 << (npcFollowFlag % 8)
	mdm.Body.Flags[npcDMFollowFlag/8] |= 1 << (npcDMFollowFlag % 8)
	s.NPCs["n2"] = mdm

	plan, err := s.PlanNPCGoChase(NPCFollowerChaseInput{ActorID: "a", SourceRoomID: 1, Now: 100}, func(int, int) int { return 1 })
	if err != nil || !plan.SkipPermanentTimer || len(plan.Moves) != 1 || plan.Moves[0].NPCID != "n1" || plan.Moves[0].PermanentTimerWrite {
		t.Fatalf("go chase plan=%+v err=%v", plan, err)
	}

	miss, err := s.PlanNPCGoChase(NPCFollowerChaseInput{ActorID: "a", SourceRoomID: 1, Now: 100}, func(int, int) int { return 11 })
	if err != nil || len(miss.Moves) != 0 {
		t.Fatalf("threshold 10 still chased: plan=%+v err=%v", miss, err)
	}
	command2, err := s.PlanNPCFollowerChase(NPCFollowerChaseInput{ActorID: "a", SourceRoomID: 1, Now: 100}, func(int, int) int { return 11 })
	if err != nil || len(command2.Moves) == 0 {
		t.Fatalf("command2 threshold 15 should still chase: plan=%+v err=%v", command2, err)
	}
}

func TestPlanAndApplyNPCGoChaseClearsPermanentWithoutTimerWrite(t *testing.T) {
	s := npcChaseFixture()
	before := s.clone()
	plan, err := s.PlanNPCGoChase(NPCFollowerChaseInput{ActorID: "a", SourceRoomID: 1, Now: 100}, func(int, int) int { return 1 })
	if err != nil || len(plan.Moves) != 1 || plan.Moves[0].PermanentTimerWrite || !plan.SkipPermanentTimer {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	next, err := s.ApplyNPCFollowerChase(plan)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s, before) {
		t.Fatal("apply mutated source state")
	}
	moved := next.NPCs["n1"]
	if moved.Body.RoomID != 2 || flag(moved.Body.Flags[:], npcPermanentFlag) {
		t.Fatalf("go chase did not move/clear permanent: %+v", moved)
	}
	if moved.PermanentOrigin != nil {
		t.Fatalf("F_CLR(MPERMT) left PermanentOrigin=%+v", moved.PermanentOrigin)
	}
	if got := next.Rooms[1].Resource.PermanentMonsters[0].LastTime; got != 90 {
		t.Fatalf("die_perm_crt timer wrote on go chase: last=%d", got)
	}
	replay, err := next.ApplyNPCFollowerChase(plan)
	if err == nil || !reflect.DeepEqual(replay, State{}) {
		t.Fatalf("same chase proposal recommitted: next=%+v err=%v", replay, err)
	}
}

func TestNPCGoChaseFanoutEventsUsesVacatedRoomFirstMonOrder(t *testing.T) {
	post := npcChaseFixture()
	plan, err := post.PlanNPCGoChase(NPCFollowerChaseInput{ActorID: "a", SourceRoomID: 1, Now: 100}, func(int, int) int { return 1 })
	if err != nil {
		t.Fatal(err)
	}
	after, err := post.ApplyNPCFollowerChase(plan)
	if err != nil {
		t.Fatal(err)
	}
	pre := post.clone()
	actor := pre.Players["a"]
	actor.Body.RoomID = 1
	pre.Players["a"] = actor
	src := pre.Rooms[1]
	src.PlayerIDs = []string{"a"}
	dst := pre.Rooms[2]
	dst.PlayerIDs = nil
	pre.Rooms[1], pre.Rooms[2] = src, dst
	events := NPCGoChaseFanoutEvents(pre, after, "a")
	if len(events) != 1 || events[0].RoomID != 1 || events[0].Text != NPCGoChaseRoomText("Alpha", "Renamed Player") {
		t.Fatalf("events=%+v", events)
	}
}

func TestApplyNPCGoChaseClearsPermanentOriginSoNextDirectionalChaseSucceeds(t *testing.T) {
	s := npcChaseFixture()
	n1 := s.NPCs["n1"]
	n1.Enemies[0].Damage = -1
	s.NPCs["n1"] = n1
	player := s.Players["a"]
	player.Body.Class = 4
	player.Body.HPMax = 30
	player.Body.HPCurrent = 30
	s.Players["a"] = player

	plan, err := s.PlanNPCGoChase(NPCFollowerChaseInput{ActorID: "a", SourceRoomID: 1, Now: 100}, func(int, int) int { return 1 })
	if err != nil {
		t.Fatal(err)
	}
	chased, err := s.ApplyNPCFollowerChase(plan)
	if err != nil {
		t.Fatal(err)
	}
	moved := chased.NPCs["n1"]
	if flag(moved.Body.Flags[:], npcPermanentFlag) || moved.PermanentOrigin != nil {
		t.Fatalf("go chase left permanence npc=%+v origin=%+v", moved.Body.Flags, moved.PermanentOrigin)
	}

	room2 := chased.Rooms[2]
	room2.Resource.Name = "중간"
	room2.Resource.Exits = []LegacyExit{{Name: "북", Destination: 3}}
	chased.Rooms[2] = room2
	chased.Rooms[3] = RoomState{Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 3, Name: "광장"}}}

	next, result, err := chased.DirectionalStep(TransferInput{
		ActorID: "a",
		Movement: MovementInput{
			Prefix:      "북",
			Occurrence:  1,
			Destination: &LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 3, Name: "광장"}},
			Now:         200,
		},
	}, nil, func(low, high int) int {
		if low != 1 || high != 50 {
			t.Fatalf("unexpected chase roll %d..%d", low, high)
		}
		return 1
	}, nil)
	if err != nil {
		t.Fatalf("next directional step fail-closed after leftover origin: %v", err)
	}
	if next.Players["a"].Body.RoomID != 3 || next.NPCs["n1"].Body.RoomID != 3 {
		t.Fatalf("MFOLLO did not chase after MPERMT clear players=%+v npcs=%+v", next.Players["a"].Body.RoomID, next.NPCs["n1"].Body.RoomID)
	}
	if result.NPCChase == nil || len(result.NPCChase.Moves) != 1 || result.NPCChase.Moves[0].NPCID != "n1" {
		t.Fatalf("directional chase missing: %+v", result.NPCChase)
	}
	followed := next.NPCs["n1"]
	if followed.PermanentOrigin != nil || flag(followed.Body.Flags[:], npcPermanentFlag) {
		t.Fatalf("second chase restored permanence: %+v", followed)
	}
}

func TestPlanNPCGoChaseRejectsNilEnemiesAndActiveOrder(t *testing.T) {
	s := npcChaseFixture()
	npc := s.NPCs["n3"]
	npc.Enemies = nil
	s.NPCs["n3"] = npc
	if _, err := s.PlanNPCGoChase(NPCFollowerChaseInput{ActorID: "a", SourceRoomID: 1, Now: 100}, func(int, int) int { return 1 }); err == nil {
		t.Fatal("nil Enemies did not fail closed")
	}

	s = npcChaseFixture()
	s.ActiveNPCIDs = nil
	if _, err := s.PlanNPCGoChase(NPCFollowerChaseInput{ActorID: "a", SourceRoomID: 1, Now: 100}, func(int, int) int { return 1 }); err == nil {
		t.Fatal("nil ActiveNPCIDs did not fail closed")
	}
}

func TestDirectionalStepCommitsNPCFollowerChaseWithPlayerMove(t *testing.T) {
	s, in := npcMovementFixture(t)
	npc := s.NPCs["guard-id"]
	npc.Body.Flags[npcFollowFlag/8] |= 1 << (npcFollowFlag % 8)
	npc.Body.Flags[npcPermanentFlag/8] |= 1 << (npcPermanentFlag % 8)
	npc.Enemies = []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "a"}, Damage: -1}}
	npc.PermanentOrigin = &NPCPermanentOrigin{RoomID: 1, Slot: 0}
	s.NPCs["guard-id"] = npc
	s.ActiveNPCIDs = []string{"guard-id"}
	source := s.Rooms[1]
	source.Resource.PermanentMonsters[0] = LegacyTimer{Misc: 77, LastTime: 1, Interval: 10}
	s.Rooms[1] = source
	in.Movement.Now = 100

	next, result, err := s.DirectionalStep(in, nil, func(low, high int) int {
		if low != 1 || high != 50 {
			t.Fatalf("unexpected chase roll %d..%d", low, high)
		}
		return 1
	}, nil)
	if err != nil || result.NPCChase == nil || len(result.NPCChase.Moves) != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if next.Players["a"].Body.RoomID != 2 || next.NPCs["guard-id"].Body.RoomID != 2 ||
		len(next.Rooms[1].NPCIDs) != 0 || !reflect.DeepEqual(next.Rooms[2].NPCIDs, []string{"guard-id"}) {
		t.Fatalf("movement membership players=%+v rooms=%+v npcs=%+v", next.Players, next.Rooms, next.NPCs)
	}
	moved := next.NPCs["guard-id"]
	if flag(moved.Body.Flags[:], npcPermanentFlag) || next.Rooms[1].Resource.PermanentMonsters[0].LastTime != 100 {
		t.Fatalf("permanent chase effects npc=%+v timer=%+v", moved, next.Rooms[1].Resource.PermanentMonsters[0])
	}
}
