package world

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func goNamedExitState() State {
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{
					ID: 1, Name: "출발지",
					Exits: []LegacyExit{
						{Name: "동굴", Destination: 2},
						{Name: "동굴입구", Destination: 2},
					},
				}},
				PlayerIDs: []string{"actor", "observer"},
				Items:     &ItemCollection{Items: map[string]Item{}},
			},
			2: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 2, Name: "도착지"}},
				PlayerIDs: []string{"away"},
				Items:     &ItemCollection{Items: map[string]Item{}},
			},
		},
		Players: map[string]PlayerState{
			"actor": {
				Body:   LegacyMonster{Name: "Alice", Type: 0, RoomID: 1, Class: 4, Level: 1, HPMax: 30, HPCurrent: 30, Stats: [5]byte{10, 10, 10, 10, 10}},
				Online: true,
				Items:  &ItemCollection{Items: map[string]Item{}},
			},
			"observer": {
				Body:   LegacyMonster{Name: "Bob", Type: 0, RoomID: 1, Class: 4, Level: 1, HPMax: 20, HPCurrent: 20},
				Online: true,
				Items:  &ItemCollection{Items: map[string]Item{}},
			},
			"away": {
				Body:   LegacyMonster{Name: "Carol", Type: 0, RoomID: 2, Class: 4, Level: 1, HPMax: 20, HPCurrent: 20},
				Online: true,
				Items:  &ItemCollection{Items: map[string]Item{}},
			},
		},
	}
}

func TestPlanApplyGoMovesThroughNamedExitAndOccurrence(t *testing.T) {
	s := goNamedExitState()
	proposal, err := s.PlanGo("actor", "동굴", 1, GoOptions{Now: 100, Hour: 12})
	if err != nil {
		t.Fatal(err)
	}
	next, result, err := s.ApplyGo(proposal)
	if err != nil || !result.Moved || result.ExitName != "동굴" || result.DestinationRoomID != 2 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if !strings.Contains(result.Response, "도착지") {
		t.Fatalf("response=%q", result.Response)
	}
	if next.Players["actor"].Body.RoomID != 2 || len(next.Rooms[1].PlayerIDs) != 1 || next.Rooms[1].PlayerIDs[0] != "observer" {
		t.Fatalf("membership source=%q dest=%q", next.Rooms[1].PlayerIDs, next.Rooms[2].PlayerIDs)
	}
	if _, _, err := next.ApplyGo(proposal); !errors.Is(err, ErrGoStaleProposal) {
		t.Fatalf("stale err=%v", err)
	}

	second, err := s.PlanGo("actor", "동굴", 2, GoOptions{Now: 100, Hour: 12})
	if err != nil || second.ExitName != "동굴입구" {
		t.Fatalf("occurrence proposal=%+v err=%v", second, err)
	}
}

func TestPlanGoArrivalSceneIncludesDisplayRomCombatNotice(t *testing.T) {
	s := goNamedExitState()
	dest := s.Rooms[2]
	dest.NPCIDs = []string{"npc-wolf"}
	s.Rooms[2] = dest
	s.NPCs = map[string]NPCState{
		"npc-wolf": {Body: LegacyMonster{Name: "늑대", Description: "회색 늑대다.", Type: 1, RoomID: 2}, Enemies: []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "actor"}, Damage: 0}}},
	}
	proposal, err := s.PlanGo("actor", "동굴", 1, GoOptions{Now: 100, Hour: 12})
	if err != nil || !proposal.Moved || proposal.DestinationRoomID != 2 {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	if !strings.Contains(proposal.Response, "도착지") || !strings.Contains(proposal.Response, "늑대가 당신과 싸우고 있습니다.\n") {
		t.Fatalf("arrival scene=%q", proposal.Response)
	}
	next, result, err := s.ApplyGo(proposal)
	if err != nil || next.Players["actor"].Body.RoomID != 2 || !strings.Contains(result.Response, "늑대가 당신과 싸우고 있습니다.\n") {
		t.Fatalf("apply=%+v err=%v", result, err)
	}
	unresolved := s
	wolf := unresolved.NPCs["npc-wolf"]
	wolf.Enemies = nil
	unresolved.NPCs["npc-wolf"] = wolf
	if _, err := unresolved.PlanGo("actor", "동굴", 1, GoOptions{Now: 100, Hour: 12}); !errors.Is(err, ErrRoomCombatUnmigrated) {
		t.Fatalf("unmigrated dest combat: %v", err)
	}
}

func TestPlanGoUsageMissingSilentCombatAndLock(t *testing.T) {
	s := goNamedExitState()
	usage, err := s.PlanGo("actor", "", 1, GoOptions{Now: 100, Hour: 12})
	if err != nil || usage.Moved || usage.Response != GoUsageResponse {
		t.Fatalf("usage=%+v err=%v", usage, err)
	}
	if _, _, err := s.ApplyGo(usage); err != nil {
		t.Fatal(err)
	}

	missing, err := s.PlanGo("actor", "없는문", 1, GoOptions{Now: 100, Hour: 12})
	if err != nil || missing.Moved || missing.Response != GoMissingResponse {
		t.Fatalf("missing=%+v err=%v", missing, err)
	}

	silent := goNamedExitState()
	actor := silent.Players["actor"]
	actor.Body.Flags[goSilentFlag/8] |= 1 << (goSilentFlag % 8)
	silent.Players["actor"] = actor
	blocked, err := silent.PlanGo("actor", "동굴", 1, GoOptions{Now: 100, Hour: 12})
	if err != nil || blocked.Moved || blocked.Response != GoSilentResponse {
		t.Fatalf("silent=%+v err=%v", blocked, err)
	}

	locked := goNamedExitState()
	room := locked.Rooms[1]
	room.Resource.Exits[0].Flags[goLockedExitFlag/8] |= 1 << (goLockedExitFlag % 8)
	locked.Rooms[1] = room
	lockedMove, err := locked.PlanGo("actor", "동굴", 1, GoOptions{Now: 100, Hour: 12})
	if err != nil || lockedMove.Moved || lockedMove.Response != GoLockedResponse {
		t.Fatalf("locked=%+v err=%v", lockedMove, err)
	}

	combat := goNamedExitState()
	src := combat.Rooms[1]
	src.NPCIDs = []string{"wolf"}
	combat.Rooms[1] = src
	combat.NPCs = map[string]NPCState{
		"wolf": {
			Body:    LegacyMonster{Name: "늑대", Type: 1, RoomID: 1, HPMax: 10, HPCurrent: 10},
			Enemies: []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "actor"}, Damage: 0}},
		},
	}
	fighting, err := combat.PlanGo("actor", "동굴", 1, GoOptions{Now: 100, Hour: 12})
	if err != nil || fighting.Moved || fighting.Response != GoCombatResponse {
		t.Fatalf("combat=%+v err=%v", fighting, err)
	}
}

func TestPlanGoClosedFlyTimeAndSexGates(t *testing.T) {
	for _, tc := range []struct {
		name     string
		setup    func(State) State
		hour     int
		want     string
		passFlag uint
	}{
		{
			name: "closed",
			setup: func(s State) State {
				room := s.Rooms[1]
				room.Resource.Exits[0].Flags[goClosedExitFlag/8] |= 1 << (goClosedExitFlag % 8)
				s.Rooms[1] = room
				return s
			},
			hour: 12,
			want: GoClosedResponse,
		},
		{
			name: "fly",
			setup: func(s State) State {
				room := s.Rooms[1]
				room.Resource.Exits[0].Flags[goFlyExitFlag/8] |= 1 << (goFlyExitFlag % 8)
				s.Rooms[1] = room
				return s
			},
			hour:     12,
			want:     GoFlyResponse,
			passFlag: goFlyPlayerFlag,
		},
		{
			name: "night-only-by-day",
			setup: func(s State) State {
				room := s.Rooms[1]
				room.Resource.Exits[0].Flags[goNightExitFlag/8] |= 1 << (goNightExitFlag % 8)
				s.Rooms[1] = room
				return s
			},
			hour: 12,
			want: GoNightResponse,
		},
		{
			name: "day-only-by-night",
			setup: func(s State) State {
				room := s.Rooms[1]
				room.Resource.Exits[0].Flags[goDayExitFlag/8] |= 1 << (goDayExitFlag % 8)
				s.Rooms[1] = room
				return s
			},
			hour: 21,
			want: GoDayResponse,
		},
		{
			name: "female-only-male",
			setup: func(s State) State {
				room := s.Rooms[1]
				room.Resource.Exits[0].Flags[goFemaleExitFlag/8] |= 1 << (goFemaleExitFlag % 8)
				s.Rooms[1] = room
				actor := s.Players["actor"]
				actor.Body.Flags[goMalePlayerFlag/8] |= 1 << (goMalePlayerFlag % 8)
				s.Players["actor"] = actor
				return s
			},
			hour: 12,
			want: GoFemaleResponse,
		},
		{
			name: "male-only-female",
			setup: func(s State) State {
				room := s.Rooms[1]
				room.Resource.Exits[0].Flags[goMaleExitFlag/8] |= 1 << (goMaleExitFlag % 8)
				s.Rooms[1] = room
				return s
			},
			hour: 12,
			want: GoMaleResponse,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.setup(goNamedExitState())
			proposal, err := s.PlanGo("actor", "동굴", 1, GoOptions{Now: 100, Hour: tc.hour})
			if err != nil || proposal.Moved || proposal.Response != tc.want || proposal.Death != nil {
				t.Fatalf("proposal=%+v err=%v", proposal, err)
			}
			if s.Players["actor"].Body.RoomID != 1 {
				t.Fatal("planning mutated actor room")
			}
			next, result, err := s.ApplyGo(proposal)
			if err != nil || result.Moved || result.Response != tc.want {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if next.Players["actor"].Body.RoomID != 1 || next.Rooms[1].Resource.Track != "" {
				t.Fatalf("gate moved or wrote track room=%d track=%q", next.Players["actor"].Body.RoomID, next.Rooms[1].Resource.Track)
			}
			again, err := next.PlanGo("actor", "동굴", 1, GoOptions{Now: 100, Hour: tc.hour})
			if err != nil || again.Moved || again.Response != tc.want {
				t.Fatalf("replay plan=%+v err=%v", again, err)
			}
			if tc.passFlag == 0 {
				return
			}
			actor := s.Players["actor"]
			actor.Body.Flags[tc.passFlag/8] |= 1 << (tc.passFlag % 8)
			s.Players["actor"] = actor
			allowed, err := s.PlanGo("actor", "동굴", 1, GoOptions{Now: 100, Hour: tc.hour})
			if err != nil || !allowed.Moved || allowed.DestinationRoomID != 2 {
				t.Fatalf("pass proposal=%+v err=%v", allowed, err)
			}
		})
	}

	nightOK := goNamedExitState()
	nightRoom := nightOK.Rooms[1]
	nightRoom.Resource.Exits[0].Flags[goNightExitFlag/8] |= 1 << (goNightExitFlag % 8)
	nightOK.Rooms[1] = nightRoom
	nightMove, err := nightOK.PlanGo("actor", "동굴", 1, GoOptions{Now: 100, Hour: 21})
	if err != nil || !nightMove.Moved {
		t.Fatalf("night open=%+v err=%v", nightMove, err)
	}
	dayOK := goNamedExitState()
	dayRoom := dayOK.Rooms[1]
	dayRoom.Resource.Exits[0].Flags[goDayExitFlag/8] |= 1 << (goDayExitFlag % 8)
	dayOK.Rooms[1] = dayRoom
	dayMove, err := dayOK.PlanGo("actor", "동굴", 1, GoOptions{Now: 100, Hour: 12})
	if err != nil || !dayMove.Moved {
		t.Fatalf("day open=%+v err=%v", dayMove, err)
	}
}

func TestPlanGoArrivalRefreshDoorsThenClosedGate(t *testing.T) {
	s := goNamedExitState()
	dest := s.Rooms[2]
	dest.Resource.Exits = []LegacyExit{{
		Name: "동굴", Destination: 1, Flags: [4]byte{}, LastTime: 10, Interval: 5,
	}}
	dest.Resource.Exits[0].Flags[goClosableExitFlag/8] |= 1 << (goClosableExitFlag % 8)
	s.Rooms[2] = dest

	proposal, err := s.PlanGo("actor", "동굴", 1, GoOptions{Now: 16, Hour: 12})
	if err != nil || !proposal.Moved || proposal.DestinationRoomID != 2 {
		t.Fatalf("arrival=%+v err=%v", proposal, err)
	}
	next, result, err := s.ApplyGo(proposal)
	if err != nil || !result.Moved || next.Players["actor"].Body.RoomID != 2 {
		t.Fatalf("apply=%+v err=%v", result, err)
	}
	if !flag(next.Rooms[2].Resource.Exits[0].Flags[:], goClosedExitFlag) {
		t.Fatalf("check_exits did not close dest door: %+v", next.Rooms[2].Resource.Exits[0])
	}
	if flag(s.Rooms[2].Resource.Exits[0].Flags[:], goClosedExitFlag) {
		t.Fatal("planning mutated source dest door")
	}

	blocked, err := next.PlanGo("actor", "동굴", 1, GoOptions{Now: 16, Hour: 12})
	if err != nil || blocked.Moved || blocked.Response != GoClosedResponse {
		t.Fatalf("return=%+v err=%v", blocked, err)
	}
	stayed, _, err := next.ApplyGo(blocked)
	if err != nil || stayed.Players["actor"].Body.RoomID != 2 {
		t.Fatalf("closed return moved actor err=%v room=%d", err, stayed.Players["actor"].Body.RoomID)
	}
}

func TestPlanGoMissingDestinationFailsClosedAfterGates(t *testing.T) {
	s := goNamedExitState()
	room := s.Rooms[1]
	room.Resource.Exits[0].Destination = 99
	s.Rooms[1] = room
	if _, err := s.PlanGo("actor", "동굴", 1, GoOptions{Now: 100, Hour: 12}); !errors.Is(err, ErrGoDestinationUnresolved) {
		t.Fatalf("missing dest err=%v", err)
	}
	if s.Players["actor"].Body.RoomID != 1 || s.Rooms[1].Resource.Track != "" {
		t.Fatal("fail-closed planning mutated snapshot")
	}

	closed := s
	closedRoom := closed.Rooms[1]
	closedRoom.Resource.Exits[0].Flags[goClosedExitFlag/8] |= 1 << (goClosedExitFlag % 8)
	closed.Rooms[1] = closedRoom
	proposal, err := closed.PlanGo("actor", "동굴", 1, GoOptions{Now: 100, Hour: 12})
	if err != nil || proposal.Moved || proposal.Response != GoClosedResponse {
		t.Fatalf("closed missing dest=%+v err=%v", proposal, err)
	}
	next, result, err := closed.ApplyGo(proposal)
	if err != nil || result.Moved || next.Players["actor"].Body.RoomID != 1 || next.Rooms[1].Resource.Track != "" {
		t.Fatalf("closed missing dest applied=%+v err=%v", result, err)
	}
}

func TestPlanGoMovesPlayerFollowersAndMDMFOLMonsters(t *testing.T) {
	s := goNamedExitState()
	leader := s.Players["actor"]
	leader.FollowerIDs = []string{"observer"}
	leader.NPCFollowerIDs = []string{"guard"}
	s.Players["actor"] = leader
	follower := s.Players["observer"]
	follower.FollowingID = "actor"
	s.Players["observer"] = follower
	src := s.Rooms[1]
	src.NPCIDs = []string{"guard"}
	s.Rooms[1] = src
	guard := LegacyMonster{Name: "guard", Type: 1, RoomID: 1, HPMax: 10, HPCurrent: 10}
	guard.Flags[npcDMFollowFlag/8] |= 1 << (npcDMFollowFlag % 8)
	s.NPCs = map[string]NPCState{
		"guard": {Body: guard, FollowingPlayerID: "actor", Enemies: []NPCEnemy{}},
	}
	s.ActiveNPCIDs = []string{"guard"}

	proposal, err := s.PlanGo("actor", "동굴", 1, GoOptions{Now: 100, Hour: 12})
	if err != nil || !proposal.Moved || proposal.Death != nil {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	next, result, err := s.ApplyGo(proposal)
	if err != nil || !result.Moved {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if next.Players["actor"].Body.RoomID != 2 || next.Players["observer"].Body.RoomID != 2 {
		t.Fatalf("followers stayed behind players=%+v", next.Players)
	}
	if next.NPCs["guard"].Body.RoomID != 2 || len(next.Rooms[1].NPCIDs) != 0 {
		t.Fatalf("MDMFOL stayed behind npcs=%+v rooms=%+v", next.NPCs, next.Rooms)
	}
}

func TestPlanGoSkipsStaleLegacyFollowersOutsideSourceRoom(t *testing.T) {
	s := goNamedExitState()
	s.Rooms[3] = RoomState{
		Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 3}},
		Items:    &ItemCollection{Items: map[string]Item{}},
		PlayerIDs: []string{
			"stale",
			"nested-stale",
		},
	}
	s.Players["stale"] = PlayerState{
		Body:        LegacyMonster{Name: "Stale", Type: 0, RoomID: 3, Class: 4, Level: 1, HPMax: 30, HPCurrent: 30},
		Online:      true,
		Items:       &ItemCollection{Items: map[string]Item{}},
		FollowingID: "actor",
		FollowerIDs: []string{"nested-stale"},
	}
	s.Players["nested-stale"] = PlayerState{
		Body:        LegacyMonster{Name: "Nested stale", Type: 0, RoomID: 3, Class: 4, Level: 1, HPMax: 30, HPCurrent: 30},
		Online:      true,
		Items:       &ItemCollection{Items: map[string]Item{}},
		FollowingID: "stale",
	}
	actor := s.Players["actor"]
	actor.FollowerIDs = []string{"stale", "observer"}
	actor.FollowerRefs = nil
	s.Players["actor"] = actor
	observer := s.Players["observer"]
	observer.FollowingID = "actor"
	s.Players["observer"] = observer

	proposal, err := s.PlanGo("actor", "동굴", 1, GoOptions{Now: 100, Hour: 12})
	if err != nil || !proposal.Moved {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	next, result, err := s.ApplyGo(proposal)
	if err != nil || !result.Moved {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if next.Players["actor"].Body.RoomID != 2 || next.Players["observer"].Body.RoomID != 2 || next.Players["stale"].Body.RoomID != 3 || next.Players["nested-stale"].Body.RoomID != 3 {
		t.Fatalf("player rooms=%+v", next.Players)
	}
	if !reflect.DeepEqual(next.Rooms[3].PlayerIDs, []string{"stale", "nested-stale"}) || !containsString(next.Rooms[2].PlayerIDs, "actor") || !containsString(next.Rooms[2].PlayerIDs, "observer") {
		t.Fatalf("room membership=%+v", next.Rooms)
	}
}

func TestPlanGoChasesMFOLLOFromSourceRoom(t *testing.T) {
	s := goNamedExitState()
	src := s.Rooms[1]
	src.NPCIDs = []string{"wolf"}
	s.Rooms[1] = src
	wolf := LegacyMonster{Name: "늑대", Type: 1, RoomID: 1, HPMax: 10, HPCurrent: 10, Stats: [5]byte{0, 5}}
	wolf.Flags[npcFollowFlag/8] |= 1 << (npcFollowFlag % 8)
	s.NPCs = map[string]NPCState{
		"wolf": {Body: wolf, Enemies: []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "actor"}, Damage: -1}}},
	}
	s.ActiveNPCIDs = []string{}
	proposal, err := s.PlanGo("actor", "동굴", 1, GoOptions{
		Now: 100, Hour: 12,
		Roll: func(low, high int) int {
			if low != 1 || high != 50 {
				t.Fatalf("unexpected chase roll %d..%d", low, high)
			}
			return 1
		},
	})
	if err != nil || !proposal.Moved {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	next, _, err := s.ApplyGo(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if next.NPCs["wolf"].Body.RoomID != 2 || !containsString(next.Rooms[2].NPCIDs, "wolf") {
		t.Fatalf("MFOLLO did not chase npcs=%+v rooms=%+v", next.NPCs, next.Rooms)
	}
	if !strings.Contains(proposal.Response, NPCGoChaseActorText("늑대")) {
		t.Fatalf("missing actor chase text: %q", proposal.Response)
	}
}

func TestPlanGoDoesNotChaseNonFollowOrMDMFOLAndFailsClosedOnNilRelations(t *testing.T) {
	s := goNamedExitState()
	src := s.Rooms[1]
	src.NPCIDs = []string{"wolf"}
	s.Rooms[1] = src
	wolf := LegacyMonster{Name: "늑대", Type: 1, RoomID: 1, HPMax: 10, HPCurrent: 10, Stats: [5]byte{0, 5}}
	s.NPCs = map[string]NPCState{
		"wolf": {Body: wolf, Enemies: []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "actor"}, Damage: -1}}},
	}
	s.ActiveNPCIDs = []string{}
	roll := func(int, int) int { return 1 }
	proposal, err := s.PlanGo("actor", "동굴", 1, GoOptions{Now: 100, Hour: 12, Roll: roll})
	if err != nil {
		t.Fatal(err)
	}
	next, _, err := s.ApplyGo(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if next.NPCs["wolf"].Body.RoomID != 1 {
		t.Fatalf("non-MFOLLO chased on go: %+v", next.NPCs["wolf"])
	}

	unresolved := goNamedExitState()
	room := unresolved.Rooms[1]
	room.NPCIDs = []string{"wolf"}
	unresolved.Rooms[1] = room
	body := wolf
	body.Flags[npcFollowFlag/8] |= 1 << (npcFollowFlag % 8)
	unresolved.NPCs = map[string]NPCState{"wolf": {Body: body, Enemies: nil}}
	unresolved.ActiveNPCIDs = []string{}
	if _, err := unresolved.PlanGo("actor", "동굴", 1, GoOptions{Now: 100, Hour: 12, Roll: roll}); err == nil {
		t.Fatal("nil Enemies did not fail closed")
	}
	unresolved.NPCs = map[string]NPCState{"wolf": {Body: body, Enemies: []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "actor"}, Damage: -1}}}}
	unresolved.ActiveNPCIDs = nil
	if _, err := unresolved.PlanGo("actor", "동굴", 1, GoOptions{Now: 100, Hour: 12, Roll: roll}); err == nil {
		t.Fatal("nil ActiveNPCIDs did not fail closed")
	}
}

func TestPlanGoAppliesDestinationArrivalTrap(t *testing.T) {
	s := goNamedExitState()
	dest := s.Rooms[2]
	dest.Resource.Trap = TrapDart
	s.Rooms[2] = dest
	rolls := []int{100, 7}
	proposal, err := s.PlanGo("actor", "동굴", 1, GoOptions{
		Now: 100, Hour: 12,
		Roll: func(low, high int) int {
			if len(rolls) == 0 {
				t.Fatalf("extra trap roll %d..%d", low, high)
			}
			value := rolls[0]
			rolls = rolls[1:]
			return value
		},
	})
	if err != nil || !proposal.Moved || proposal.Transfer.ArrivalTrap == nil || !proposal.Transfer.ArrivalTrap.Triggered {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	next, _, err := s.ApplyGo(proposal)
	if err != nil {
		t.Fatal(err)
	}
	actor := next.Players["actor"]
	if actor.Body.RoomID != 2 || actor.Body.HPCurrent != 23 || !flag(actor.Body.Flags[:], 16) {
		t.Fatalf("trap not applied actor=%+v", actor.Body)
	}
}

func TestPlanGoLethalClimbCallsPlayerDeath(t *testing.T) {
	s := goNamedExitState()
	s.War = &FamilyWar{}
	s.Rooms[1008] = RoomState{
		Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1008, Name: "부활"}},
		Items:    &ItemCollection{Items: map[string]Item{}},
	}
	src := s.Rooms[1]
	src.Resource.Exits[0].Flags[1] |= 1 // XCLIMB
	src.Items = &ItemCollection{Items: map[string]Item{}}
	s.Rooms[1] = src
	actor := s.Players["actor"]
	actor.Body.HPCurrent = 5
	actor.Body.HPMax = 30
	s.Players["actor"] = actor

	proposal, err := s.PlanGo("actor", "동굴", 1, GoOptions{
		Now: 100, Hour: 12,
		Roll: func(low, high int) int { return low },
	})
	if err != nil || proposal.Moved || proposal.Death == nil || !proposal.Transfer.Movement.Traversal.Dead {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	next, result, err := s.ApplyGo(proposal)
	if err != nil || result.Moved {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	actor = next.Players["actor"]
	if actor.Body.RoomID != 1008 || actor.Body.HPCurrent != actor.Body.HPMax || !actor.Online {
		t.Fatalf("lethal climb left 0-HP ghost actor=%+v", actor.Body)
	}
	if next.Players["observer"].Body.RoomID != 1 || containsString(next.Rooms[2].PlayerIDs, "actor") {
		t.Fatal("dead actor entered destination or dragged followers")
	}
}

func TestPlanGoRejectsInvalidOccurrenceAndAbsentActor(t *testing.T) {
	s := goNamedExitState()
	if _, err := s.PlanGo("actor", "동굴", 0, GoOptions{}); !errors.Is(err, ErrGoInvalidOccurrence) {
		t.Fatalf("occurrence err=%v", err)
	}
	if _, err := s.PlanGo("missing", "동굴", 1, GoOptions{}); !errors.Is(err, ErrGoActorAbsent) {
		t.Fatalf("actor err=%v", err)
	}
}
