package world

import (
	"errors"
	"reflect"
	"testing"
)

func npcPlayerDeathFixture() State {
	s := playerDeathFixture()
	p := s.Players["a"]
	p.Body.HPCurrent = 0
	p.Body.Daily[9].Max = 2
	s.Players["a"] = p

	npc := NPCState{
		Body: LegacyMonster{
			Name:      "늑대",
			Type:      1,
			Level:     4,
			RoomID:    1,
			HPMax:     30,
			HPCurrent: 30,
		},
		Enemies: []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "a"}, Damage: 7}},
	}
	s.NPCs = map[string]NPCState{"wolf": npc}
	s.ActiveNPCIDs = []string{"wolf"}
	r := s.Rooms[1]
	r.NPCIDs = []string{"wolf"}
	s.Rooms[1] = r
	return s
}

func noDeathRNG(t *testing.T) func(int, int) int {
	t.Helper()
	return func(int, int) int {
		t.Fatalf("NPC death should not consume death-timer RNG")
		return 0
	}
}

func warBossNPCPlayerDeathFixture() State {
	s := npcPlayerDeathFixture()
	s.War = &FamilyWar{Active: 2*16 + 3, CalledBy: 2, CalledAgainst: 3}
	p := s.Players["a"]
	p.Body.Flags[7] |= 2
	s.Players["a"] = p
	return s
}

func TestPlanNPCPlayerDeathCommitsOneAtomicCandidate(t *testing.T) {
	s := npcPlayerDeathFixture()
	before := s.clone()
	next, result, err := s.PlanNPCPlayerDeath("wolf", "a", 100, SceneOptions{}, nil, noDeathRNG(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.NPCID != "wolf" || result.VictimID != "a" || !result.BroadcastDeath {
		t.Fatalf("result=%+v", result)
	}
	p := next.Players["a"]
	if p.Body.RoomID != 1008 || p.Body.HPCurrent != p.Body.HPMax || p.Body.MPCurrent != p.Body.MPMax || p.Body.Timers[11] != (LegacyTimer{}) {
		t.Fatalf("respawned player=%+v", p.Body)
	}
	if len(p.Items.Items) != 0 || len(next.Rooms[1].Items.Items) != 1 {
		t.Fatalf("equipment ownership player=%+v floor=%+v", p.Items, next.Rooms[1].Items)
	}
	if len(next.Rooms[1].PlayerIDs) != 0 || !reflect.DeepEqual(next.Rooms[1].NPCIDs, []string{"wolf"}) {
		t.Fatalf("source membership=%+v", next.Rooms[1])
	}
	if !reflect.DeepEqual(next.ActiveNPCIDs, []string{}) {
		t.Fatalf("source active NPCs=%+v", next.ActiveNPCIDs)
	}
	if got := next.NPCs["wolf"].Enemies; len(got) != 0 {
		t.Fatalf("dead player enemy relation remained=%+v", got)
	}
	if !reflect.DeepEqual(s, before) {
		t.Fatal("planner mutated input snapshot")
	}
}

func TestPlanNPCPlayerDeathWarBossEmitsCatalogDefeatBroadcasts(t *testing.T) {
	s := warBossNPCPlayerDeathFixture()
	before := s.clone()
	next, result, err := s.PlanNPCPlayerDeath("wolf", "a", 100, SceneOptions{}, nil, noDeathRNG(t), nil, deathFamilyCatalog())
	if err != nil {
		t.Fatal(err)
	}
	if !result.FamilyDefeated || result.NPCID != "wolf" || result.VictimID != "a" || *next.War != (FamilyWar{}) {
		t.Fatalf("result=%+v war=%+v", result, next.War)
	}
	assertFamilyDefeatBroadcasts(t, result.Events, "청룡")
	if !reflect.DeepEqual(s, before) {
		t.Fatal("planner mutated input snapshot")
	}
}

func TestPlanNPCPlayerDeathWarBossRejectsMissingCatalog(t *testing.T) {
	s := warBossNPCPlayerDeathFixture()
	next, result, err := s.PlanNPCPlayerDeath("wolf", "a", 100, SceneOptions{}, nil, noDeathRNG(t), nil, FamilyCatalog{})
	if !errors.Is(err, ErrFamilyCatalogUnavailable) || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(result, NPCPlayerDeathResult{}) {
		t.Fatalf("missing catalog next=%+v result=%+v err=%v", next, result, err)
	}
}

func TestPlanNPCPlayerDeathRejectsUnmigratedWar(t *testing.T) {
	s := npcPlayerDeathFixture()
	s.War = nil
	next, result, err := s.PlanNPCPlayerDeath("wolf", "a", 100, SceneOptions{}, nil, noDeathRNG(t), nil)
	if err == nil || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(result, NPCPlayerDeathResult{}) {
		t.Fatalf("unmigrated war next=%+v result=%+v err=%v", next, result, err)
	}
}

func TestPlanNPCPlayerDeathUsesNPCProgressionAndSurvivalRules(t *testing.T) {
	for _, survival := range []bool{false, true} {
		s := npcPlayerDeathFixture()
		p := s.Players["a"]
		p.Body.Experience = 1000
		p.Body.Timers[11] = LegacyTimer{LastTime: 8, Interval: 9, Misc: 10}
		s.Players["a"] = p
		n := s.NPCs["wolf"]
		n.Body.Timers[11] = LegacyTimer{LastTime: 18, Interval: 19, Misc: 20}
		s.NPCs["wolf"] = n
		if survival {
			r := s.Rooms[1]
			r.Resource.Flags[4] |= 1 << (36 % 8)
			s.Rooms[1] = r
		}
		next, result, err := s.PlanNPCPlayerDeath("wolf", "a", 100, SceneOptions{}, nil, noDeathRNG(t), nil)
		if err != nil {
			t.Fatal(err)
		}
		got := next.Players["a"]
		if got.Body.Experience != 950 || got.Body.MPCurrent != got.Body.MPMax || next.NPCs["wolf"].Body.Timers[11] != n.Body.Timers[11] {
			t.Fatalf("NPC progression/timers survival=%v player=%+v npc=%+v", survival, got.Body, next.NPCs["wolf"].Body)
		}
		if result.BroadcastDeath == survival {
			t.Fatalf("broadcast survival=%v result=%+v", survival, result)
		}
		if survival && len(next.Rooms[1].Items.Items) != 0 {
			t.Fatalf("survival unexpectedly dropped equipment: %+v", next.Rooms[1].Items)
		}
		if !survival && len(next.Rooms[1].Items.Items) != 1 {
			t.Fatalf("ordinary NPC death did not drop wielded equipment: %+v", next.Rooms[1].Items)
		}
	}
}

func TestPlanNPCPlayerDeathRejectsNonLethalAndSelfIdentity(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*State)
		npcID  string
		plyID  string
	}{
		{name: "non lethal", mutate: func(s *State) { p := s.Players["a"]; p.Body.HPCurrent = 1; s.Players["a"] = p }, npcID: "wolf", plyID: "a"},
		{name: "same identity", mutate: nil, npcID: "a", plyID: "a"},
		{name: "missing npc", mutate: nil, npcID: "missing", plyID: "a"},
		{name: "missing player", mutate: nil, npcID: "wolf", plyID: "missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := npcPlayerDeathFixture()
			if tc.mutate != nil {
				tc.mutate(&s)
			}
			before := s.clone()
			next, result, err := s.PlanNPCPlayerDeath(tc.npcID, tc.plyID, 100, SceneOptions{}, nil, noDeathRNG(t), nil)
			if err == nil || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(result, NPCPlayerDeathResult{}) || !reflect.DeepEqual(s, before) {
				t.Fatalf("accepted partial death next=%+v result=%+v err=%v", next, result, err)
			}
		})
	}
}

func TestPlanNPCPlayerDeathRejectsUnresolvedNPCContext(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*State)
	}{
		{name: "enemy list unresolved", mutate: func(s *State) { n := s.NPCs["wolf"]; n.Enemies = nil; s.NPCs["wolf"] = n }},
		{name: "not active", mutate: func(s *State) { s.ActiveNPCIDs = []string{} }},
		{name: "war state not imported", mutate: func(s *State) { s.War = nil }},
		{name: "respawn floor not canonical", mutate: func(s *State) { r := s.Rooms[1008]; r.Items = nil; s.Rooms[1008] = r }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := npcPlayerDeathFixture()
			tc.mutate(&s)
			before := s.clone()
			next, result, err := s.PlanNPCPlayerDeath("wolf", "a", 100, SceneOptions{}, nil, noDeathRNG(t), nil)
			if err == nil || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(result, NPCPlayerDeathResult{}) || !reflect.DeepEqual(s, before) {
				t.Fatalf("accepted unresolved context next=%+v result=%+v err=%v", next, result, err)
			}
		})
	}
}

func TestPlanNPCPlayerDeathAllowsSummonerAttacker(t *testing.T) {
	s := npcPlayerDeathFixture()
	n := s.NPCs["wolf"]
	n.Body.Flags[npcSummonFlag/8] |= 1 << (npcSummonFlag % 8)
	s.NPCs["wolf"] = n
	before := s.clone()

	next, result, err := s.PlanNPCPlayerDeath("wolf", "a", 100, SceneOptions{}, nil, noDeathRNG(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.NPCID != "wolf" || result.VictimID != "a" || !result.BroadcastDeath {
		t.Fatalf("result=%+v", result)
	}
	if next.Players["a"].Body.RoomID != 1008 || len(next.NPCs["wolf"].Enemies) != 0 {
		t.Fatalf("summoner death candidate=%+v npc=%+v", next.Players["a"].Body, next.NPCs["wolf"])
	}
	if !reflect.DeepEqual(s, before) {
		t.Fatal("planner mutated input snapshot")
	}
}

func TestPlanNPCPlayerDeathRejectsInvalidRoomOrFollowerMembership(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*State)
	}{
		{name: "room mismatch", mutate: func(s *State) { n := s.NPCs["wolf"]; n.Body.RoomID = 2; s.NPCs["wolf"] = n }},
		{name: "npc omitted from room", mutate: func(s *State) { r := s.Rooms[1]; r.NPCIDs = nil; s.Rooms[1] = r }},
		{name: "player omitted from room", mutate: func(s *State) { r := s.Rooms[1]; r.PlayerIDs = nil; s.Rooms[1] = r }},
		{name: "follower edge not reciprocal", mutate: func(s *State) { n := s.NPCs["wolf"]; n.FollowingPlayerID = "a"; s.NPCs["wolf"] = n }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := npcPlayerDeathFixture()
			tc.mutate(&s)
			before := s.clone()
			next, result, err := s.PlanNPCPlayerDeath("wolf", "a", 100, SceneOptions{}, nil, noDeathRNG(t), nil)
			if err == nil || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(result, NPCPlayerDeathResult{}) || !reflect.DeepEqual(s, before) {
				t.Fatalf("accepted invalid membership next=%+v result=%+v err=%v", next, result, err)
			}
		})
	}
}

func TestPlanNPCPlayerDeathIsAtomicOnLateRespawnFailure(t *testing.T) {
	s := npcPlayerDeathFixture()
	r := s.Rooms[1008]
	r.Resource.BeenHere = 2147483647
	s.Rooms[1008] = r
	before := s.clone()
	next, result, err := s.PlanNPCPlayerDeath("wolf", "a", 100, SceneOptions{}, nil, noDeathRNG(t), nil)
	if err == nil || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(result, NPCPlayerDeathResult{}) || !reflect.DeepEqual(s, before) {
		t.Fatalf("partial late failure next=%+v result=%+v err=%v", next, result, err)
	}
}

func TestPlanNPCPlayerDeathKeepsNPCFollowerEdgeUntilSupportedCleanup(t *testing.T) {
	s := npcPlayerDeathFixture()
	n := s.NPCs["wolf"]
	n.FollowingPlayerID = "a"
	s.NPCs["wolf"] = n
	p := s.Players["a"]
	p.NPCFollowerIDs = []string{"wolf"}
	p.FollowerRefs = []EntityRef{{Kind: "npc", ID: "wolf"}}
	s.Players["a"] = p
	next, _, err := s.PlanNPCPlayerDeath("wolf", "a", 100, SceneOptions{}, nil, noDeathRNG(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if next.NPCs["wolf"].FollowingPlayerID != "a" || !reflect.DeepEqual(next.Players["a"].NPCFollowerIDs, []string{"wolf"}) {
		t.Fatalf("unsupported follower cleanup was guessed: npc=%+v player=%+v", next.NPCs["wolf"], next.Players["a"])
	}
}
