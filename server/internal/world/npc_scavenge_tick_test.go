package world

import (
	"reflect"
	"testing"
)

func npcScavengeTickFixture(t *testing.T) State {
	t.Helper()
	s := npcScavengeFixture(t)
	scavenger := s.NPCs["scavenger"]
	second := NPCState{
		Body: LegacyMonster{
			Name: "second", Type: 1, RoomID: 1,
			Flags:  scavenger.Body.Flags,
			Timers: [45]LegacyTimer{{}},
		},
		Items:   &ItemCollection{Items: map[string]Item{}},
		Enemies: []NPCEnemy{},
	}
	s.NPCs["second"] = second
	s.Rooms[1] = func() RoomState {
		room := s.Rooms[1]
		room.NPCIDs = []string{"second", "scavenger"}
		return room
	}()
	s.ActiveNPCIDs = []string{"second", "scavenger"}
	for _, id := range s.ActiveNPCIDs {
		npc := s.NPCs[id]
		npc.Body.Timers[npcScavengeTimer] = LegacyTimer{LastTime: 100}
		s.NPCs[id] = npc
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPlanApplyNPCScavengeTickPreservesActiveOrderAndOneDueRollPerNPC(t *testing.T) {
	state := npcScavengeTickFixture(t)
	before := state.clone()
	calls := 0
	proposal, err := state.PlanNPCScavengeTick(200, func(low, high int) int {
		calls++
		if low != 1 || high != 100 {
			t.Fatalf("unexpected MSCAVE range %d..%d", low, high)
		}
		return 1
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(proposal.Actions) != 2 || proposal.Actions[0].NPCID != "second" || proposal.Actions[1].NPCID != "scavenger" {
		t.Fatalf("ordered proposal calls=%d actions=%+v", calls, proposal.Actions)
	}
	if !reflect.DeepEqual(state, before) {
		t.Fatal("MSCAVE tick planning mutated source state")
	}
	next, result, err := state.ApplyNPCScavengeTick(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.ActiveNPCIDs, []string{"second", "scavenger"}) || len(result.Actions) != 2 || len(result.Events) != 2 || !result.Changed || result.NoOp {
		t.Fatalf("tick result=%+v", result)
	}
	if result.Actions[0].SelectedItemID != "bag" || result.Actions[1].SelectedItemID != "later" {
		t.Fatalf("active-order selection=%+v", result.Actions)
	}
	if !reflect.DeepEqual(next.Rooms[1].Items.Inventory, []string{"protected"}) || !reflect.DeepEqual(next.NPCs["second"].Items.Inventory, []string{"bag"}) || !reflect.DeepEqual(next.NPCs["scavenger"].Items.Inventory, []string{"apple", "later"}) {
		t.Fatalf("ordered ownership floor=%v second=%v first=%v", next.Rooms[1].Items.Inventory, next.NPCs["second"].Items.Inventory, next.NPCs["scavenger"].Items.Inventory)
	}
	if result.Events[0].NPCID != "second" || result.Events[0].ItemID != "bag" || result.Events[0].Text != NPCScavengeRoomText("second", "bag") {
		t.Fatalf("first event=%+v", result.Events[0])
	}
}

func TestNPCScavengeTickNonDueNeverCallsRNGAndDueNoItemWritesTimer(t *testing.T) {
	t.Run("non-due", func(t *testing.T) {
		state := npcScavengeTickFixture(t)
		for _, id := range state.ActiveNPCIDs {
			npc := state.NPCs[id]
			npc.Body.Timers[npcScavengeTimer].LastTime = 190
			state.NPCs[id] = npc
		}
		before := state.clone()
		proposal, err := state.PlanNPCScavengeTick(200, func(int, int) int {
			t.Fatal("non-due MSCAVE tick consumed RNG")
			return 1
		})
		if err != nil || len(proposal.Actions) != 2 || proposal.Actions[0].Attempted || proposal.Actions[1].Attempted {
			t.Fatalf("non-due proposal=%+v err=%v", proposal, err)
		}
		next, result, err := state.ApplyNPCScavengeTick(proposal)
		if err != nil || !reflect.DeepEqual(next, before) || result.Changed || !result.NoOp || len(result.Events) != 0 {
			t.Fatalf("non-due next=%+v result=%+v err=%v", next, result, err)
		}
	})

	t.Run("due-no-item", func(t *testing.T) {
		state := npcScavengeTickFixture(t)
		room := state.Rooms[1]
		for id, item := range room.Items.Items {
			setNPCScavengeObjectFlag(&item.Object, npcScavengeSceneryFlag, true)
			room.Items.Items[id] = item
		}
		state.Rooms[1] = room
		calls := 0
		proposal, err := state.PlanNPCScavengeTick(200, func(low, high int) int {
			calls++
			if low != 1 || high != 100 {
				t.Fatalf("unexpected MSCAVE range %d..%d", low, high)
			}
			return 1
		})
		if err != nil || calls != 2 || len(proposal.Actions) != 2 {
			t.Fatalf("due no-item proposal=%+v calls=%d err=%v", proposal, calls, err)
		}
		next, result, err := state.ApplyNPCScavengeTick(proposal)
		if err != nil {
			t.Fatal(err)
		}
		for _, action := range result.Actions {
			if action.Decision != NPCScavengeNoEligibleItem || action.TimerAfter.LastTime != 200 || action.Transferred || action.MHASSCChanged {
				t.Fatalf("due no-item action=%+v", action)
			}
		}
		if result.NoOp || !result.Changed || len(result.Events) != 0 {
			t.Fatalf("due no-item summary=%+v", result)
		}
		for _, id := range next.ActiveNPCIDs {
			if got := next.NPCs[id].Body.Timers[npcScavengeTimer].LastTime; got != 200 {
				t.Fatalf("due no-item timer for %q=%d", id, got)
			}
			npc := next.NPCs[id]
			if flag(npc.Body.Flags[:], npcScavengedFlag) {
				t.Fatalf("due no-item set MHASSC for %q", id)
			}
		}
	})
}

func TestNPCScavengeTickHandlesKnownEmptyFloorWithoutRNGMismatch(t *testing.T) {
	state := npcScavengeFixture(t)
	room := state.Rooms[1]
	room.Items = &ItemCollection{Items: map[string]Item{}, Inventory: []string{}}
	state.Rooms[1] = room
	calls := 0
	proposal, err := state.PlanNPCScavengeTick(200, func(low, high int) int {
		calls++
		if low != 1 || high != 100 {
			t.Fatalf("unexpected MSCAVE range %d..%d", low, high)
		}
		return 1
	})
	if err != nil || calls != 1 || len(proposal.Actions) != 1 || proposal.Actions[0].Decision != NPCScavengeNoEligibleItem {
		t.Fatalf("known-empty proposal=%+v calls=%d err=%v", proposal, calls, err)
	}
	next, result, err := state.ApplyNPCScavengeTick(proposal)
	if err != nil || len(result.Actions) != 1 || result.Actions[0].TimerAfter.LastTime != 200 || !result.Changed || result.NoOp || len(next.Rooms[1].Items.Inventory) != 0 {
		t.Fatalf("known-empty apply next=%+v result=%+v err=%v", next, result, err)
	}
}

func TestNPCScavengeTickFailsClosedForUnresolvedActiveEnemyBeforeRNG(t *testing.T) {
	state := npcScavengeTickFixture(t)
	npc := state.NPCs["scavenger"]
	npc.Enemies = nil
	state.NPCs["scavenger"] = npc
	before := state.clone()
	calls := 0
	proposal, err := state.PlanNPCScavengeTick(200, func(int, int) int {
		calls++
		return 1
	})
	if err == nil || calls != 0 || !reflect.DeepEqual(proposal, NPCScavengeTickProposal{}) || !reflect.DeepEqual(state, before) {
		t.Fatalf("unresolved enemy was not fail-closed: proposal=%+v calls=%d err=%v", proposal, calls, err)
	}
}

func TestNPCScavengeTickRejectsStaleTamperedAndReplayAtomically(t *testing.T) {
	state := npcScavengeTickFixture(t)
	proposal, err := state.PlanNPCScavengeTick(200, func(int, int) int { return 1 })
	if err != nil {
		t.Fatal(err)
	}

	// A later action is deliberately invalid. The first action would transfer
	// a subtree if the wrapper leaked partial state, so the original snapshot is
	// checked after the rejection and the untouched proposal is applied again.
	tampered := proposal
	tampered.Actions = append([]NPCScavengeProposal(nil), proposal.Actions...)
	tampered.Actions[1].Roll = 101
	before := state.clone()
	if next, _, err := state.ApplyNPCScavengeTick(tampered); err == nil || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(state, before) {
		t.Fatalf("tampered action partially applied: next=%+v err=%v state=%+v", next, err, state)
	}
	if _, _, err := state.ApplyNPCScavengeTick(proposal); err != nil {
		t.Fatalf("valid proposal consumed by rejected tamper: %v", err)
	}

	state = npcScavengeTickFixture(t)
	proposal, err = state.PlanNPCScavengeTick(200, func(int, int) int { return 1 })
	if err != nil {
		t.Fatal(err)
	}
	changed := state.clone()
	changed.NPCs["scavenger"] = func() NPCState {
		npc := changed.NPCs["scavenger"]
		npc.Body.Name = "tampered"
		return npc
	}()
	if next, _, err := changed.ApplyNPCScavengeTick(proposal); err == nil || !reflect.DeepEqual(next, State{}) {
		t.Fatalf("stale proposal accepted: next=%+v err=%v", next, err)
	}

	next, _, err := state.ApplyNPCScavengeTick(proposal)
	if err != nil {
		t.Fatal(err)
	}
	replayBefore := next.clone()
	if replay, _, err := next.ApplyNPCScavengeTick(proposal); err == nil || !reflect.DeepEqual(replay, State{}) || !reflect.DeepEqual(next, replayBefore) {
		t.Fatalf("tick replay accepted or mutated state: replay=%+v err=%v", replay, err)
	}
}
