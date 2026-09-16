package world

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func npcAITargetFixture(t *testing.T) State {
	t.Helper()
	state := State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"one", "two", "three"},
				NPCIDs:    []string{"wolf"},
			},
		},
		Players: map[string]PlayerState{
			"one": {
				Body:   LegacyMonster{Name: "하나", Type: 0, RoomID: 1, Level: 12, Stats: [5]byte{10, 10, 10, 10, 24}},
				Online: true,
			},
			"two": {
				Body:   LegacyMonster{Name: "둘", Type: 0, RoomID: 1, Level: 12, Stats: [5]byte{10, 10, 10, 10, 0}},
				Online: true,
			},
			"three": {
				Body:   LegacyMonster{Name: "셋", Type: 0, RoomID: 1, Level: 12, Stats: [5]byte{10, 10, 10, 10, 24}},
				Online: true,
			},
		},
		NPCs: map[string]NPCState{
			"wolf": {
				Body: LegacyMonster{
					Name: "늑대", Type: 1, RoomID: 1, Level: 8, Stats: [5]byte{10, 10, 10, 10, 10},
					Timers: [45]LegacyTimer{3: {LastTime: 9, Interval: 2, Misc: 7}},
				},
				Enemies: []NPCEnemy{},
			},
		},
		ActiveNPCIDs: []string{"wolf"},
	}
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	return state
}

func npcAISetFlag(body *LegacyMonster, bit uint) {
	body.Flags[bit/8] |= 1 << (bit % 8)
}

func TestPlanApplyNPCAggressiveTargetUsesCanonicalOrderAndPietyWeight(t *testing.T) {
	state := npcAITargetFixture(t)
	npc := state.NPCs["wolf"]
	npcAISetFlag(&npc.Body, npcAIAggressiveFlag)
	state.NPCs["wolf"] = npc
	before := state.clone()
	var calls [][2]int
	proposal, err := state.PlanNPCAggressiveTargetAcquisition(100, func(low, high int) int {
		calls = append(calls, [2]int{low, high})
		return 27
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(state, before) {
		t.Fatal("planning mutated source state")
	}
	if len(calls) != 1 || calls[0] != [2]int{1, 27} {
		t.Fatalf("random calls=%v; want one weighted 1..27 draw", calls)
	}
	if len(proposal.Actions) != 1 {
		t.Fatalf("actions=%+v", proposal.Actions)
	}
	action := proposal.Actions[0]
	if action.Status != npcAITargetAcquired || action.NPCID != "wolf" || action.RoomID != 1 || action.TargetID != "three" || action.TargetName != "셋" || action.TotalWeight != 27 || action.SelectionRoll != 27 || action.TargetWeight != 1 || !action.TimerWrite || action.EnemyAdded != true {
		t.Fatalf("action=%+v", action)
	}
	if proposal.NoOp || !proposal.Changed {
		t.Fatalf("proposal=%+v", proposal)
	}

	next, result, err := state.ApplyNPCAggressiveTargetAcquisition(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if result.Now != 100 || result.NoOp || !result.Changed || len(result.Actions) != 1 {
		t.Fatalf("result=%+v", result)
	}
	got := next.NPCs["wolf"]
	if got.Body.Timers[3] != (LegacyTimer{LastTime: 100, Interval: 0, Misc: 7}) {
		t.Fatalf("attack timer=%+v", got.Body.Timers[3])
	}
	if !reflect.DeepEqual(got.Enemies, []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "three"}, Damage: 0}}) {
		t.Fatalf("enemy list=%+v", got.Enemies)
	}
	if !reflect.DeepEqual(state, before) {
		t.Fatal("apply mutated source state")
	}
	if err := next.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestNPCAggressiveTargetUsesActiveThenRoomPlayerOrder(t *testing.T) {
	state := npcAITargetFixture(t)
	room := state.Rooms[1]
	room.PlayerIDs = []string{"three", "one", "two"}
	room.NPCIDs = []string{"second", "first"}
	state.Rooms[1] = room
	delete(state.NPCs, "wolf")
	state.NPCs["first"] = NPCState{
		Body:    LegacyMonster{Name: "첫째", Type: 1, RoomID: 1, Level: 8, Stats: [5]byte{10, 10, 10, 10, 10}},
		Enemies: []NPCEnemy{},
	}
	state.NPCs["second"] = NPCState{
		Body:    LegacyMonster{Name: "둘째", Type: 1, RoomID: 1, Level: 8, Stats: [5]byte{10, 10, 10, 10, 10}},
		Enemies: []NPCEnemy{},
	}
	for _, id := range []string{"first", "second"} {
		npc := state.NPCs[id]
		npcAISetFlag(&npc.Body, npcAIAggressiveFlag)
		state.NPCs[id] = npc
	}
	state.ActiveNPCIDs = []string{"second", "first"}
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}

	proposal, err := state.PlanNPCAggressiveTargetAcquisition(100, func(low, _ int) int { return low })
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{proposal.Actions[0].NPCID, proposal.Actions[1].NPCID}; !reflect.DeepEqual(got, []string{"second", "first"}) {
		t.Fatalf("active traversal=%v", got)
	}
	for _, action := range proposal.Actions {
		if action.TargetID != "three" {
			t.Fatalf("room player traversal action=%+v", action)
		}
	}
}

func TestNPCAggressiveTargetVisibilityAndMDINVI(t *testing.T) {
	state := npcAITargetFixture(t)
	room := state.Rooms[1]
	room.PlayerIDs = []string{"hidden", "invisible", "dm", "visible"}
	state.Rooms[1] = room
	for _, id := range []string{"one", "two", "three"} {
		delete(state.Players, id)
	}
	state.Players["hidden"] = PlayerState{Body: LegacyMonster{Name: "숨김", Type: 0, RoomID: 1, Stats: [5]byte{10, 10, 10, 10, 24}}, Online: true}
	state.Players["invisible"] = PlayerState{Body: LegacyMonster{Name: "투명", Type: 0, RoomID: 1, Stats: [5]byte{10, 10, 10, 10, 24}}, Online: true}
	state.Players["dm"] = PlayerState{Body: LegacyMonster{Name: "운영", Type: 0, RoomID: 1, Stats: [5]byte{10, 10, 10, 10, 24}}, Online: true}
	state.Players["visible"] = PlayerState{Body: LegacyMonster{Name: "보임", Type: 0, RoomID: 1, Stats: [5]byte{10, 10, 10, 10, 24}}, Online: true}
	npc := state.NPCs["wolf"]
	npcAISetFlag(&npc.Body, npcAIAggressiveFlag)
	hidden := state.Players["hidden"]
	npcAISetFlag(&hidden.Body, npcAIHiddenPlayerFlag)
	state.Players["hidden"] = hidden
	invisible := state.Players["invisible"]
	npcAISetFlag(&invisible.Body, npcAIInvisiblePlayerFlag)
	state.Players["invisible"] = invisible
	dm := state.Players["dm"]
	npcAISetFlag(&dm.Body, npcAIDMInvisiblePlayerFlag)
	state.Players["dm"] = dm
	state.NPCs["wolf"] = npc
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}

	proposal, err := state.PlanNPCAggressiveTargetAcquisition(100, func(low, high int) int {
		if low != 1 || high != 1 {
			t.Fatalf("visibility total=%d..%d; want only visible player", low, high)
		}
		return 1
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := proposal.Actions[0].TargetID; got != "visible" {
		t.Fatalf("without MDINVI target=%q", got)
	}

	npc = state.NPCs["wolf"]
	npcAISetFlag(&npc.Body, npcAIDetectInvisibleFlag)
	state.NPCs["wolf"] = npc
	proposal, err = state.PlanNPCAggressiveTargetAcquisition(100, func(low, high int) int {
		if low != 1 || high != 2 {
			t.Fatalf("MDINVI visibility total=%d..%d; want invisible+visible", low, high)
		}
		return 1
	})
	if err != nil || proposal.Actions[0].TargetID != "invisible" {
		t.Fatalf("with MDINVI action=%+v err=%v", proposal.Actions[0], err)
	}
}

func TestNPCAggressiveTargetAlignmentAndLevelBoundaries(t *testing.T) {
	state := npcAITargetFixture(t)
	room := state.Rooms[1]
	room.PlayerIDs = []string{"good-boundary", "good-reject"}
	state.Rooms[1] = room
	for _, id := range []string{"one", "two", "three"} {
		delete(state.Players, id)
	}
	state.Players["good-boundary"] = PlayerState{Body: LegacyMonster{Name: "선", Type: 0, RoomID: 1, Level: 8, Alignment: 100, Stats: [5]byte{10, 10, 10, 10, 0}}, Online: true}
	state.Players["good-reject"] = PlayerState{Body: LegacyMonster{Name: "악", Type: 0, RoomID: 1, Level: 8, Alignment: 99, Stats: [5]byte{10, 10, 10, 10, 0}}, Online: true}
	npc := state.NPCs["wolf"]
	npcAISetFlag(&npc.Body, npcAIGoodAggressiveFlag)
	state.NPCs["wolf"] = npc
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	proposal, err := state.PlanNPCAggressiveTargetAcquisition(100, func(low, high int) int {
		if low != 1 || high != 30 {
			t.Fatalf("good total=%d..%d; want alignment >=100 only", low, high)
		}
		return 30
	})
	if err != nil || proposal.Actions[0].TargetID != "good-boundary" {
		t.Fatalf("good alignment action=%+v err=%v", proposal.Actions[0], err)
	}

	state = npcAITargetFixture(t)
	room = state.Rooms[1]
	room.PlayerIDs = []string{"evil-boundary", "evil-reject"}
	state.Rooms[1] = room
	for _, id := range []string{"one", "two", "three"} {
		delete(state.Players, id)
	}
	state.Players["evil-boundary"] = PlayerState{Body: LegacyMonster{Name: "악선", Type: 0, RoomID: 1, Level: 8, Alignment: -100, Stats: [5]byte{10, 10, 10, 10, 0}}, Online: true}
	state.Players["evil-reject"] = PlayerState{Body: LegacyMonster{Name: "악근처", Type: 0, RoomID: 1, Level: 8, Alignment: -99, Stats: [5]byte{10, 10, 10, 10, 0}}, Online: true}
	npc = state.NPCs["wolf"]
	npcAISetFlag(&npc.Body, npcAIEvilAggressiveFlag)
	state.NPCs["wolf"] = npc
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	proposal, err = state.PlanNPCAggressiveTargetAcquisition(100, func(low, high int) int {
		if low != 1 || high != 30 {
			t.Fatalf("evil total=%d..%d; want alignment <=-100 only", low, high)
		}
		return 1
	})
	if err != nil || proposal.Actions[0].TargetID != "evil-boundary" {
		t.Fatalf("evil alignment action=%+v err=%v", proposal.Actions[0], err)
	}
}

func TestNPCAggressiveTargetPreservesLegacyLowPietySecondPassLevelQuirk(t *testing.T) {
	state := npcAITargetFixture(t)
	room := state.Rooms[1]
	room.PlayerIDs = []string{"low-level", "high-level"}
	state.Rooms[1] = room
	for _, id := range []string{"one", "two", "three"} {
		delete(state.Players, id)
	}
	state.Players["low-level"] = PlayerState{Body: LegacyMonster{Name: "낮은레벨", Type: 0, RoomID: 1, Level: 4, Alignment: 100, Stats: [5]byte{10, 10, 10, 10, 0}}, Online: true}
	state.Players["high-level"] = PlayerState{Body: LegacyMonster{Name: "높은레벨", Type: 0, RoomID: 1, Level: 12, Alignment: 100, Stats: [5]byte{10, 10, 10, 10, 29}}, Online: true}
	npc := state.NPCs["wolf"]
	npc.Body.Level = 8
	npcAISetFlag(&npc.Body, npcAIGoodAggressiveFlag)
	state.NPCs["wolf"] = npc
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	proposal, err := state.PlanNPCAggressiveTargetAcquisition(100, func(low, high int) int {
		if low != 1 || high != 1 {
			t.Fatalf("first-pass level total=%d..%d; want high-level weight only", low, high)
		}
		return 1
	})
	if err != nil || proposal.Actions[0].TargetID != "low-level" {
		t.Fatalf("legacy second-pass action=%+v err=%v", proposal.Actions[0], err)
	}
}

func TestNPCAggressiveTargetUsesStrictDexterityEscapeBoundary(t *testing.T) {
	for _, tc := range []struct {
		name       string
		escapeRoll int
		escaped    bool
	}{
		{name: "three escapes", escapeRoll: 3, escaped: true},
		{name: "four attacks", escapeRoll: 4, escaped: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := npcAITargetFixture(t)
			room := state.Rooms[1]
			room.PlayerIDs = []string{"fast"}
			state.Rooms[1] = room
			for _, id := range []string{"one", "two", "three"} {
				delete(state.Players, id)
			}
			state.Players["fast"] = PlayerState{Body: LegacyMonster{Name: "빠름", Type: 0, RoomID: 1, Stats: [5]byte{10, 11, 10, 10, 24}}, Online: true}
			npc := state.NPCs["wolf"]
			npc.Body.Stats[1] = 10
			npcAISetFlag(&npc.Body, npcAIAggressiveFlag)
			state.NPCs["wolf"] = npc
			if err := state.Validate(); err != nil {
				t.Fatal(err)
			}
			calls := 0
			proposal, err := state.PlanNPCAggressiveTargetAcquisition(100, func(low, high int) int {
				calls++
				if calls == 1 {
					if low != 1 || high != 1 {
						t.Fatalf("selection draw=%d..%d", low, high)
					}
					return 1
				}
				if low != 1 || high != 10 {
					t.Fatalf("escape draw=%d..%d", low, high)
				}
				return tc.escapeRoll
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(proposal.Actions) != 1 || proposal.Actions[0].Escaped != tc.escaped || proposal.Actions[0].EscapeRoll != tc.escapeRoll {
				t.Fatalf("action=%+v", proposal.Actions[0])
			}
			if proposal.Actions[0].TimerWrite == tc.escaped {
				// The source timer write is the complement of the escape decision.
				t.Fatalf("timer write=%v escaped=%v", proposal.Actions[0].TimerWrite, tc.escaped)
			}
		})
	}
}

func TestNPCAggressiveTargetHonorsAttackTimerGateAndReadyBoundary(t *testing.T) {
	state := npcAITargetFixture(t)
	npc := state.NPCs["wolf"]
	npcAISetFlag(&npc.Body, npcAIAggressiveFlag)
	npc.Body.Timers[npcAIAttackTimerIndex] = LegacyTimer{LastTime: 100, Interval: 10, Misc: 7}
	state.NPCs["wolf"] = npc
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}

	before := state.clone()
	calls := 0
	notReady, err := state.PlanNPCAggressiveTargetAcquisition(109, func(int, int) int {
		calls++
		return 1
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 || !reflect.DeepEqual(state, before) {
		t.Fatalf("not-ready plan consumed RNG or mutated state: calls=%d state=%+v before=%+v", calls, state, before)
	}
	if !notReady.NoOp || notReady.Changed || len(notReady.Actions) != 1 || notReady.Actions[0].Status != npcAINotReady || notReady.Actions[0].TimerWrite || notReady.Actions[0].Event != nil {
		t.Fatalf("not-ready proposal=%+v", notReady)
	}
	next, result, err := state.ApplyNPCAggressiveTargetAcquisition(notReady)
	if err != nil || result.Now != 109 || !result.NoOp || result.Changed || !reflect.DeepEqual(next, state) {
		t.Fatalf("not-ready apply next=%+v result=%+v err=%v", next, result, err)
	}

	calls = 0
	ready, err := state.PlanNPCAggressiveTargetAcquisition(110, func(low, high int) int {
		calls++
		if low != 1 || high != 27 {
			t.Fatalf("ready selection draw=%d..%d", low, high)
		}
		return 1
	})
	if err != nil || calls != 1 || len(ready.Actions) != 1 || ready.Actions[0].Status != npcAITargetAcquired {
		t.Fatalf("ready proposal=%+v calls=%d err=%v", ready, calls, err)
	}
	readyNext, readyResult, err := state.ApplyNPCAggressiveTargetAcquisition(ready)
	if err != nil || readyResult.NoOp || !readyResult.Changed || readyNext.NPCs["wolf"].Body.Timers[npcAIAttackTimerIndex] != (LegacyTimer{LastTime: 110, Interval: 0, Misc: 7}) {
		t.Fatalf("ready apply next=%+v result=%+v err=%v", readyNext, readyResult, err)
	}
}

func TestNPCAggressiveTargetDoesNotRerollEqualDexterityOrDuplicateEnemy(t *testing.T) {
	state := npcAITargetFixture(t)
	room := state.Rooms[1]
	room.PlayerIDs = []string{"fast"}
	state.Rooms[1] = room
	for _, id := range []string{"one", "two", "three"} {
		delete(state.Players, id)
	}
	state.Players["fast"] = PlayerState{Body: LegacyMonster{Name: "같음", Type: 0, RoomID: 1, Stats: [5]byte{10, 10, 10, 10, 24}}, Online: true}
	npc := state.NPCs["wolf"]
	npcAISetFlag(&npc.Body, npcAIAggressiveFlag)
	state.NPCs["wolf"] = npc
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	selectionCalls := 0
	proposal, err := state.PlanNPCAggressiveTargetAcquisition(100, func(low, high int) int {
		selectionCalls++
		if low != 1 || high != 1 {
			t.Fatalf("selection draw=%d..%d", low, high)
		}
		return 1
	})
	if err != nil || selectionCalls != 1 || proposal.Actions[0].EscapeRoll != 0 || !proposal.Actions[0].TimerWrite {
		t.Fatalf("equal dex action=%+v calls=%d err=%v", proposal.Actions[0], selectionCalls, err)
	}
	next, _, err := state.ApplyNPCAggressiveTargetAcquisition(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.NPCs["wolf"].Enemies) != 1 {
		t.Fatalf("enemy=%+v", next.NPCs["wolf"].Enemies)
	}

	// Existing first_enm prevents update_active from entering the acquisition
	// block; the canonical reducer must preserve it and never append a duplicate.
	state.NPCs["wolf"] = NPCState{
		Body:    state.NPCs["wolf"].Body,
		Enemies: []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "fast"}, Damage: 4}},
	}
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	proposal, err = state.PlanNPCAggressiveTargetAcquisition(100, func(int, int) int {
		t.Fatal("existing enemy must not consume target RNG")
		return 0
	})
	if err != nil || proposal.Changed || proposal.Actions[0].Status != npcAIExistingEnemy {
		t.Fatalf("existing enemy action=%+v err=%v", proposal.Actions[0], err)
	}
	next, _, err = state.ApplyNPCAggressiveTargetAcquisition(proposal)
	if err != nil || !reflect.DeepEqual(next.NPCs["wolf"].Enemies, state.NPCs["wolf"].Enemies) {
		t.Fatalf("duplicate enemy changed state=%+v err=%v", next.NPCs["wolf"].Enemies, err)
	}
}

func TestNPCAggressiveTargetRejectsUnresolvedAndInvalidPlansAtomically(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*State)
		roll   func(int, int) int
	}{
		{
			name: "active order unresolved",
			mutate: func(s *State) {
				s.ActiveNPCIDs = nil
			},
			roll: func(int, int) int {
				t.Fatal("unresolved active order consumed RNG")
				return 0
			},
		},
		{
			name: "enemy relation unresolved",
			mutate: func(s *State) {
				npc := s.NPCs["wolf"]
				npc.Enemies = nil
				s.NPCs["wolf"] = npc
			},
			roll: func(int, int) int {
				t.Fatal("unresolved enemy relation consumed RNG")
				return 0
			},
		},
		{
			name: "invalid weighted draw",
			mutate: func(s *State) {
				npc := s.NPCs["wolf"]
				npcAISetFlag(&npc.Body, npcAIAggressiveFlag)
				s.NPCs["wolf"] = npc
			},
			roll: func(low, _ int) int { return low - 1 },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state := npcAITargetFixture(t)
			tc.mutate(&state)
			before, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			proposal, err := state.PlanNPCAggressiveTargetAcquisition(100, tc.roll)
			if err == nil || !reflect.DeepEqual(proposal, NPCAggressiveTargetAcquisitionProposal{}) {
				t.Fatalf("accepted invalid proposal=%+v err=%v", proposal, err)
			}
			after, marshalErr := json.Marshal(state)
			if marshalErr != nil || !bytes.Equal(before, after) {
				t.Fatalf("invalid plan mutated state before=%s after=%s err=%v", before, after, marshalErr)
			}
		})
	}
}

func TestApplyNPCAggressiveTargetRejectsStaleAndTamperedAtomically(t *testing.T) {
	state := npcAITargetFixture(t)
	npc := state.NPCs["wolf"]
	npcAISetFlag(&npc.Body, npcAIAggressiveFlag)
	state.NPCs["wolf"] = npc
	proposal, err := state.PlanNPCAggressiveTargetAcquisition(100, func(low, _ int) int { return low })
	if err != nil {
		t.Fatal(err)
	}
	stale := state.clone()
	changed := stale.NPCs["wolf"]
	changed.Body.Level++
	stale.NPCs["wolf"] = changed
	if got, _, err := stale.ApplyNPCAggressiveTargetAcquisition(proposal); err == nil || !reflect.DeepEqual(got, State{}) {
		t.Fatalf("stale proposal accepted got=%+v err=%v", got, err)
	}

	tampered := proposal
	tampered.Actions = append([]NPCAggressiveTargetAction(nil), proposal.Actions...)
	tampered.Actions[0].SelectionRoll = 0
	before := state.clone()
	if got, _, err := state.ApplyNPCAggressiveTargetAcquisition(tampered); err == nil || !reflect.DeepEqual(got, State{}) {
		t.Fatalf("tampered proposal accepted got=%+v err=%v", got, err)
	}
	if !reflect.DeepEqual(state, before) {
		t.Fatal("failed apply mutated source")
	}
}

func TestApplyNPCAggressiveTargetBindsNoOpTimestampAtomically(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*State)
	}{
		{
			name:   "peaceful active NPC",
			mutate: func(*State) {},
		},
		{
			name:   "known empty active order",
			mutate: func(s *State) { s.ActiveNPCIDs = []string{} },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state := npcAITargetFixture(t)
			tc.mutate(&state)
			proposal, err := state.PlanNPCAggressiveTargetAcquisition(100, func(int, int) int {
				t.Fatal("no-op consumed RNG")
				return 0
			})
			if err != nil {
				t.Fatal(err)
			}
			if !proposal.NoOp || proposal.Changed {
				t.Fatalf("proposal=%+v", proposal)
			}
			tampered := proposal
			tampered.Now++
			before := state.clone()
			if got, _, err := state.ApplyNPCAggressiveTargetAcquisition(tampered); err == nil || !reflect.DeepEqual(got, State{}) {
				t.Fatalf("tampered no-op timestamp accepted got=%+v err=%v", got, err)
			}
			if !reflect.DeepEqual(state, before) {
				t.Fatal("failed no-op apply mutated source")
			}
		})
	}
}

func TestNPCAggressiveTargetExposesDurableActorAndRoomEvent(t *testing.T) {
	state := npcAITargetFixture(t)
	npc := state.NPCs["wolf"]
	npcAISetFlag(&npc.Body, npcAIAggressiveFlag)
	state.NPCs["wolf"] = npc
	proposal, err := state.PlanNPCAggressiveTargetAcquisition(100, func(low, high int) int {
		if low != 1 || high != 27 {
			t.Fatalf("selection draw=%d..%d", low, high)
		}
		return 1
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(proposal.Actions) != 1 || proposal.Actions[0].Event == nil {
		t.Fatalf("proposal action event=%+v", proposal.Actions)
	}
	event := proposal.Actions[0].Event
	wantRoomText := "\n늑대가 하나를 공격합니다."
	if event.RoomID != 1 || event.NPCID != "wolf" || event.NPCName != "늑대" || event.TargetID != "one" || event.TargetName != "하나" || event.ExcludeTargetID != "one" || event.TargetText != NPCAggressiveTargetActorText("늑대") || event.RoomText != wantRoomText {
		t.Fatalf("proposal event=%+v", event)
	}

	before := state.clone()
	next, result, err := state.ApplyNPCAggressiveTargetAcquisition(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 1 || !reflect.DeepEqual(result.Events[0], *event) || result.Events[0].RoomText != wantRoomText {
		t.Fatalf("durable events=%+v want=%+v", result.Events, event)
	}
	if result.Now != 100 || result.Actions[0].Event == nil || !reflect.DeepEqual(*result.Actions[0].Event, result.Events[0]) {
		t.Fatalf("result=%+v", result)
	}
	if !reflect.DeepEqual(next.NPCs["wolf"].Enemies, []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "one"}, Damage: 0}}) {
		t.Fatalf("enemy state=%+v", next.NPCs["wolf"].Enemies)
	}
	replay, replayResult, replayErr := state.ApplyNPCAggressiveTargetAcquisition(proposal)
	if replayErr != nil || !reflect.DeepEqual(replay, next) || !reflect.DeepEqual(replayResult, result) {
		t.Fatalf("event replay state=%+v result=%+v err=%v", replay, replayResult, replayErr)
	}
	if len(replayResult.Events) != 1 || replayResult.Events[0].RoomText != wantRoomText {
		t.Fatalf("replayed durable room text=%q want=%q", replayResult.Events[0].RoomText, wantRoomText)
	}

	tampered := proposal
	tampered.Actions = cloneNPCAIActions(proposal.Actions)
	tampered.Actions[0].Event.RoomText = "위조된 방 알림"
	if got, _, err := state.ApplyNPCAggressiveTargetAcquisition(tampered); err == nil || !reflect.DeepEqual(got, State{}) {
		t.Fatalf("tampered event accepted got=%+v err=%v", got, err)
	}
	if !reflect.DeepEqual(state, before) {
		t.Fatal("event apply mutated source")
	}
}

func TestNPCAggressiveTargetNoOpReplayIsPureAndTamperChecked(t *testing.T) {
	state := npcAITargetFixture(t)
	proposal, err := state.PlanNPCAggressiveTargetAcquisition(100, func(int, int) int {
		t.Fatal("peaceful NPC no-op consumed RNG")
		return 0
	})
	if err != nil {
		t.Fatal(err)
	}
	if !proposal.NoOp || proposal.Changed || len(proposal.Actions) != 1 || proposal.Actions[0].Status != npcAINotAggressive {
		t.Fatalf("no-op proposal=%+v", proposal)
	}
	next, result, err := state.ApplyNPCAggressiveTargetAcquisition(proposal)
	if err != nil || !result.NoOp || result.Changed || !reflect.DeepEqual(next, state) {
		t.Fatalf("no-op apply next=%+v result=%+v err=%v", next, result, err)
	}
	if replay, replayResult, replayErr := state.ApplyNPCAggressiveTargetAcquisition(proposal); replayErr != nil || !reflect.DeepEqual(replay, next) || !reflect.DeepEqual(replayResult, result) {
		t.Fatalf("no-op replay state=%+v result=%+v err=%v", replay, replayResult, replayErr)
	}

	tampered := proposal
	tampered.Actions = append([]NPCAggressiveTargetAction(nil), proposal.Actions...)
	tampered.Actions[0].Status = npcAITargetAcquired
	if got, _, err := state.ApplyNPCAggressiveTargetAcquisition(tampered); err == nil || !reflect.DeepEqual(got, State{}) {
		t.Fatalf("tampered no-op accepted got=%+v err=%v", got, err)
	}
}

func TestNPCAggressiveTargetNoPlayersNoOpDoesNotRequireEnemyResolution(t *testing.T) {
	state := npcAITargetFixture(t)
	room := state.Rooms[1]
	room.PlayerIDs = nil
	state.Rooms[1] = room
	for _, id := range []string{"one", "two", "three"} {
		delete(state.Players, id)
	}
	npc := state.NPCs["wolf"]
	npc.Enemies = nil
	npcAISetFlag(&npc.Body, npcAIAggressiveFlag)
	state.NPCs["wolf"] = npc
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	proposal, err := state.PlanNPCAggressiveTargetAcquisition(100, func(int, int) int {
		t.Fatal("empty room consumed RNG")
		return 0
	})
	if err != nil || proposal.Actions[0].Status != npcAINoPlayers || !proposal.NoOp {
		t.Fatalf("empty-room action=%+v err=%v", proposal.Actions[0], err)
	}
}
