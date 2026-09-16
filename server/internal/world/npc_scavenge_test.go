package world

import (
	"reflect"
	"testing"
)

func npcScavengeFixture(t *testing.T) State {
	t.Helper()
	npcFlags := [8]byte{}
	npcFlags[npcScavengeFlag/8] |= 1 << (npcScavengeFlag % 8)
	protectedFlags := [8]byte{}
	protectedFlags[npcScavengePermanentFlag/8] |= 1 << (npcScavengePermanentFlag % 8)
	state := State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Name: "scavenge"}},
				PlayerIDs: []string{"player"},
				NPCIDs:    []string{"scavenger"},
				Items: &ItemCollection{
					Items: map[string]Item{
						"protected": {Object: LegacyObject{Name: "protected", Flags: protectedFlags}},
						"bag":       {Object: LegacyObject{Name: "bag"}, Contents: []string{"gem", "coin"}},
						"gem":       {Object: LegacyObject{Name: "gem"}},
						"coin":      {Object: LegacyObject{Name: "coin"}},
						"later":     {Object: LegacyObject{Name: "later"}},
					},
					Inventory: []string{"protected", "bag", "later"},
				},
			},
		},
		Players: map[string]PlayerState{
			"player": {Body: LegacyMonster{Name: "player", Type: 0, RoomID: 1}, Online: true},
		},
		NPCs: map[string]NPCState{
			"scavenger": {
				Body: LegacyMonster{
					Name: "scavenger", Type: 1, RoomID: 1, Flags: npcFlags,
					Timers: [45]LegacyTimer{{}},
				},
				Items: &ItemCollection{
					Items: map[string]Item{
						"apple": {Object: LegacyObject{Name: "apple"}},
						"held":  {Object: LegacyObject{Name: "held"}},
					},
					Inventory: []string{"apple"},
					Ready:     [20]string{"held"},
				},
				Enemies: []NPCEnemy{},
			},
		},
		ActiveNPCIDs: []string{"scavenger"},
	}
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	return state
}

func setNPCScavengeObjectFlag(object *LegacyObject, bit uint, enabled bool) {
	if enabled {
		object.Flags[bit/8] |= 1 << (bit % 8)
		return
	}
	object.Flags[bit/8] &^= 1 << (bit % 8)
}

func TestNPCScavengeNonDueNoRollOrMutation(t *testing.T) {
	state := npcScavengeFixture(t)
	npc := state.NPCs["scavenger"]
	npc.Body.Timers[npcScavengeTimer] = LegacyTimer{LastTime: 100, Interval: 7, Misc: 3}
	state.NPCs["scavenger"] = npc
	before := state.clone()
	proposal, err := state.PlanNPCScavenge("scavenger", 110, func(int, int) int {
		t.Fatal("non-due scavenger consumed RNG")
		return 1
	})
	if err != nil || proposal.Due || proposal.Attempted || proposal.Roll != 0 || proposal.Decision != NPCScavengeNotDue {
		t.Fatalf("non-due proposal=%+v err=%v", proposal, err)
	}
	next, result, err := state.ApplyNPCScavenge(proposal)
	if err != nil || !reflect.DeepEqual(next, before) || result.Roll != 0 || result.Transferred {
		t.Fatalf("non-due apply next=%+v result=%+v err=%v", next, result, err)
	}
	replayBefore := next.clone()
	if _, _, err := next.ApplyNPCScavenge(proposal); err == nil || !reflect.DeepEqual(next, replayBefore) {
		t.Fatalf("non-due no-op replay accepted or mutated state: err=%v", err)
	}
}

func TestNPCScavengeTimerBoundaryAndZeroSemantics(t *testing.T) {
	tests := []struct {
		name    string
		last    int32
		now     int32
		wantDue bool
	}{
		{name: "exactly 20 seconds is not due", last: 100, now: 120},
		{name: "21 seconds is due", last: 100, now: 121, wantDue: true},
		{name: "zero timer is absent at time zero", now: 0, wantDue: true},
		{name: "zero timer remains absent at 20 seconds", now: 20, wantDue: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state := npcScavengeFixture(t)
			npc := state.NPCs["scavenger"]
			npc.Body.Timers[npcScavengeTimer] = LegacyTimer{LastTime: tc.last, Interval: 7}
			state.NPCs["scavenger"] = npc
			calls := 0
			proposal, err := state.PlanNPCScavenge("scavenger", tc.now, func(low, high int) int {
				calls++
				if low != 1 || high != 100 {
					t.Fatalf("roll bounds %d..%d", low, high)
				}
				return 100
			})
			if err != nil || proposal.Due != tc.wantDue {
				t.Fatalf("timer boundary proposal=%+v err=%v", proposal, err)
			}
			if tc.wantDue && (calls != 1 || !proposal.Attempted || proposal.Roll != 100) {
				t.Fatalf("due attempt calls=%d proposal=%+v", calls, proposal)
			}
			if !tc.wantDue && calls != 0 {
				t.Fatalf("not-due attempt consumed RNG: calls=%d", calls)
			}
		})
	}
}

func TestNPCScavengeDueNoEligibleConsumesRollAndUpdatesTimer(t *testing.T) {
	state := npcScavengeFixture(t)
	room := state.Rooms[1]
	for id, item := range room.Items.Items {
		if item.Object.Name == "" {
			continue
		}
		setNPCScavengeObjectFlag(&item.Object, npcScavengePermanentFlag, true)
		room.Items.Items[id] = item
	}
	state.Rooms[1] = room
	rollCalls := 0
	proposal, err := state.PlanNPCScavenge("scavenger", 200, func(low, high int) int {
		rollCalls++
		if low != 1 || high != 100 {
			t.Fatalf("roll bounds %d..%d", low, high)
		}
		return 1
	})
	if err != nil || rollCalls != 1 || !proposal.Due || !proposal.Attempted || proposal.Roll != 1 || proposal.EligibleItemID != "" || proposal.Decision != NPCScavengeNoEligibleItem {
		t.Fatalf("no-item proposal=%+v calls=%d err=%v", proposal, rollCalls, err)
	}
	next, result, err := state.ApplyNPCScavenge(proposal)
	if err != nil {
		t.Fatal(err)
	}
	nextNPC := next.NPCs["scavenger"]
	if nextNPC.Body.Timers[npcScavengeTimer].LastTime != 200 || flag(nextNPC.Body.Flags[:], npcScavengedFlag) {
		t.Fatalf("no-item state=%+v", next.NPCs["scavenger"])
	}
	if !reflect.DeepEqual(next.Rooms[1].Items, state.Rooms[1].Items) || !reflect.DeepEqual(nextNPC.Items, state.NPCs["scavenger"].Items) || result.Transferred {
		t.Fatalf("no-item ownership changed result=%+v", result)
	}
}

func TestNPCScavengeFailedRollConsumesRollAndLeavesItems(t *testing.T) {
	state := npcScavengeFixture(t)
	proposal, err := state.PlanNPCScavenge("scavenger", 200, func(int, int) int { return 16 })
	if err != nil || proposal.Roll != 16 || proposal.RollSuccess || proposal.Decision != NPCScavengeRollFailed {
		t.Fatalf("failed-roll proposal=%+v err=%v", proposal, err)
	}
	next, result, err := state.ApplyNPCScavenge(proposal)
	if err != nil {
		t.Fatal(err)
	}
	nextNPC := next.NPCs["scavenger"]
	if result.Decision != NPCScavengeRollFailed || nextNPC.Body.Timers[npcScavengeTimer].LastTime != 200 ||
		len(next.Rooms[1].Items.Inventory) != 3 || len(nextNPC.Items.Inventory) != 1 || flag(nextNPC.Body.Flags[:], npcScavengedFlag) {
		t.Fatalf("failed-roll next=%+v result=%+v", next, result)
	}
}

func TestNPCScavengeRollBoundariesRecordExactlyOneAttempt(t *testing.T) {
	tests := []struct {
		name        string
		roll        int
		want        NPCScavengeDecision
		transferred bool
	}{
		{name: "15 succeeds", roll: 15, want: NPCScavengeTransferred, transferred: true},
		{name: "100 fails", roll: 100, want: NPCScavengeRollFailed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state := npcScavengeFixture(t)
			calls := 0
			proposal, err := state.PlanNPCScavenge("scavenger", 200, func(low, high int) int {
				calls++
				if low != 1 || high != 100 {
					t.Fatalf("roll bounds %d..%d", low, high)
				}
				return tc.roll
			})
			if err != nil || calls != 1 || proposal.Roll != tc.roll || proposal.Decision != tc.want {
				t.Fatalf("roll-boundary proposal=%+v calls=%d err=%v", proposal, calls, err)
			}
			next, _, err := state.ApplyNPCScavenge(proposal)
			if err != nil {
				t.Fatal(err)
			}
			if got := len(next.NPCs["scavenger"].Items.Inventory) > 1; got != tc.transferred {
				t.Fatalf("transferred=%v want %v; npc inventory=%v", got, tc.transferred, next.NPCs["scavenger"].Items.Inventory)
			}
		})
	}
}

func TestNPCScavengeSuccessSelectsFirstEligibleFloorRootAndSubtree(t *testing.T) {
	state := npcScavengeFixture(t)
	proposal, err := state.PlanNPCScavenge("scavenger", 200, func(int, int) int { return 1 })
	if err != nil || proposal.EligibleItemID != "bag" || proposal.SelectedItemID != "bag" || proposal.Decision != NPCScavengeTransferred {
		t.Fatalf("success proposal=%+v err=%v", proposal, err)
	}
	if !reflect.DeepEqual(proposal.ProtectedRootIDs, []string{"protected"}) {
		t.Fatalf("protected roots=%v", proposal.ProtectedRootIDs)
	}
	next, result, err := state.ApplyNPCScavenge(proposal)
	if err != nil {
		t.Fatal(err)
	}
	roomItems := next.Rooms[1].Items
	npcItems := next.NPCs["scavenger"].Items
	if !reflect.DeepEqual(roomItems.Inventory, []string{"protected", "later"}) || !reflect.DeepEqual(npcItems.Inventory, []string{"apple", "bag"}) {
		t.Fatalf("root order room=%v npc=%v", roomItems.Inventory, npcItems.Inventory)
	}
	if _, ok := roomItems.Items["bag"]; ok {
		t.Fatal("transferred root remains on floor")
	}
	if !reflect.DeepEqual(npcItems.Items["bag"].Contents, []string{"gem", "coin"}) || npcItems.Items["gem"].Object.Name != "gem" || npcItems.Items["coin"].Object.Name != "coin" {
		t.Fatalf("subtree not transferred: %+v", npcItems.Items)
	}
	nextNPC := next.NPCs["scavenger"]
	if npcItems.Ready[0] != "held" || !flag(nextNPC.Body.Flags[:], npcScavengedFlag) || !result.MHASSCChanged {
		t.Fatalf("ready/flag result=%+v npc=%+v", result, next.NPCs["scavenger"])
	}
}

func TestNPCScavengeExcludesHeldEquippedAndDescendantRoots(t *testing.T) {
	state := npcScavengeFixture(t)
	npc := state.NPCs["scavenger"]
	npcItems := npc.Items.clone()
	npcItems.Items["equipped"] = Item{Object: LegacyObject{Name: "equipped"}}
	npcItems.Ready[1] = "equipped"
	npc.Items = &npcItems
	state.NPCs["scavenger"] = npc
	room := state.Rooms[1]
	for _, id := range room.Items.Inventory {
		item := room.Items.Items[id]
		setNPCScavengeObjectFlag(&item.Object, npcScavengePermanentFlag, true)
		room.Items.Items[id] = item
	}
	state.Rooms[1] = room
	proposal, err := state.PlanNPCScavenge("scavenger", 200, func(int, int) int { return 1 })
	if err != nil || proposal.EligibleItemID != "" || proposal.Decision != NPCScavengeNoEligibleItem {
		t.Fatalf("held/equipped/descendant proposal=%+v err=%v", proposal, err)
	}
	next, _, err := state.ApplyNPCScavenge(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(next.Rooms[1].Items.Inventory, room.Items.Inventory) ||
		!reflect.DeepEqual(next.NPCs["scavenger"].Items.Inventory, []string{"apple"}) ||
		next.NPCs["scavenger"].Items.Ready[0] != "held" || next.NPCs["scavenger"].Items.Ready[1] != "equipped" {
		t.Fatalf("held/equipped/descendant ownership changed: room=%v npc=%+v", next.Rooms[1].Items.Inventory, next.NPCs["scavenger"].Items)
	}
}

func TestNPCScavengeProtectedFlagsAndDescendantsAreNotRoots(t *testing.T) {
	protectedBits := []uint{npcScavengePermanentFlag, npcScavengeHiddenFlag, npcScavengePermanentInventoryFlag, npcScavengeNotTakeFlag, npcScavengeSceneryFlag}
	for _, bit := range protectedBits {
		t.Run(string(rune('a'+bit)), func(t *testing.T) {
			state := npcScavengeFixture(t)
			room := state.Rooms[1]
			for id, item := range room.Items.Items {
				setNPCScavengeObjectFlag(&item.Object, bit, true)
				room.Items.Items[id] = item
			}
			state.Rooms[1] = room
			proposal, err := state.PlanNPCScavenge("scavenger", 200, func(int, int) int { return 1 })
			if err != nil || proposal.EligibleItemID != "" || proposal.Decision != NPCScavengeNoEligibleItem {
				t.Fatalf("protected bit=%d proposal=%+v err=%v", bit, proposal, err)
			}
			next, _, err := state.ApplyNPCScavenge(proposal)
			if err != nil || len(next.Rooms[1].Items.Inventory) != 3 || len(next.NPCs["scavenger"].Items.Inventory) != 1 {
				t.Fatalf("protected bit=%d next=%+v err=%v", bit, next, err)
			}
		})
	}
}

func TestNPCScavengeMHASSCOnlyChangesAfterTransfer(t *testing.T) {
	tests := []struct {
		name           string
		roll           int
		makeIneligible bool
		wantSet        bool
	}{
		{name: "failed", roll: 16},
		{name: "no item", roll: 1, makeIneligible: true},
		{name: "success", roll: 1, wantSet: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state := npcScavengeFixture(t)
			if tc.makeIneligible {
				room := state.Rooms[1]
				for id, item := range room.Items.Items {
					setNPCScavengeObjectFlag(&item.Object, npcScavengeSceneryFlag, true)
					room.Items.Items[id] = item
				}
				state.Rooms[1] = room
			}
			proposal, err := state.PlanNPCScavenge("scavenger", 200, func(int, int) int { return tc.roll })
			if err != nil {
				t.Fatal(err)
			}
			next, _, err := state.ApplyNPCScavenge(proposal)
			if err != nil {
				t.Fatal(err)
			}
			nextNPC := next.NPCs["scavenger"]
			if got := flag(nextNPC.Body.Flags[:], npcScavengedFlag); got != tc.wantSet {
				t.Fatalf("MHASSC=%v want %v", got, tc.wantSet)
			}
		})
	}
}

func TestNPCScavengeApplyRejectsStaleReplayAndInvalidInputsAtomically(t *testing.T) {
	state := npcScavengeFixture(t)
	proposal, err := state.PlanNPCScavenge("scavenger", 200, func(int, int) int { return 1 })
	if err != nil {
		t.Fatal(err)
	}
	next, _, err := state.ApplyNPCScavenge(proposal)
	if err != nil {
		t.Fatal(err)
	}
	replayBefore := next.clone()
	if _, _, err := next.ApplyNPCScavenge(proposal); err == nil || !reflect.DeepEqual(next, replayBefore) {
		t.Fatalf("replay accepted or mutated state: err=%v", err)
	}

	stale := state.clone()
	npc := stale.NPCs["scavenger"]
	npc.Body.Name = "changed"
	stale.NPCs["scavenger"] = npc
	staleBefore := stale.clone()
	if _, _, err := stale.ApplyNPCScavenge(proposal); err == nil || !reflect.DeepEqual(stale, staleBefore) {
		t.Fatalf("stale proposal accepted or mutated state: err=%v", err)
	}

	invalidRoll := proposal
	invalidRoll.Roll = 101
	invalidBefore := state.clone()
	if _, _, err := state.ApplyNPCScavenge(invalidRoll); err == nil || !reflect.DeepEqual(state, invalidBefore) {
		t.Fatalf("invalid roll accepted or mutated state: err=%v", err)
	}
	invalidTime := proposal
	invalidTime.Now = -1
	if _, _, err := state.ApplyNPCScavenge(invalidTime); err == nil || !reflect.DeepEqual(state, invalidBefore) {
		t.Fatalf("invalid timestamp accepted or mutated state: err=%v", err)
	}
}

func TestNPCScavengeRejectsTamperedPlanTimeAndPreservesProposalToken(t *testing.T) {
	state := npcScavengeFixture(t)
	proposal, err := state.PlanNPCScavenge("scavenger", 200, func(int, int) int { return 1 })
	if err != nil {
		t.Fatal(err)
	}
	tampered := proposal
	tampered.Now = 1000
	tampered.TimerAfter.LastTime = 1000
	before := state.clone()
	if _, _, err := state.ApplyNPCScavenge(tampered); err == nil || !reflect.DeepEqual(state, before) {
		t.Fatalf("tampered plan time accepted or mutated state: err=%v", err)
	}

	next, result, err := state.ApplyNPCScavenge(proposal)
	if err != nil {
		t.Fatalf("valid proposal was consumed by rejected tamper attempt: %v", err)
	}
	if result.Now != 200 || next.NPCs["scavenger"].Body.Timers[npcScavengeTimer].LastTime != 200 {
		t.Fatalf("valid proposal applied with unexpected timestamp: result=%+v npc=%+v", result, next.NPCs["scavenger"])
	}
}

func TestNPCScavengeRejectsEquivalentTargetRebindingAtomically(t *testing.T) {
	state := npcScavengeFixture(t)
	equivalent := state.NPCs["scavenger"]
	equivalent.Items = &ItemCollection{Items: map[string]Item{}}
	state.NPCs["equivalent"] = equivalent
	room := state.Rooms[1]
	room.NPCIDs = append(room.NPCIDs, "equivalent")
	state.Rooms[1] = room
	state.ActiveNPCIDs = append(state.ActiveNPCIDs, "equivalent")
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	proposal, err := state.PlanNPCScavenge("scavenger", 200, func(int, int) int { return 1 })
	if err != nil {
		t.Fatal(err)
	}
	tampered := proposal
	tampered.NPCID = "equivalent"
	before := state.clone()
	if _, _, err := state.ApplyNPCScavenge(tampered); err == nil || !reflect.DeepEqual(state, before) {
		t.Fatalf("equivalent NPC rebinding accepted or mutated state: err=%v", err)
	}
	if _, _, err := state.ApplyNPCScavenge(proposal); err != nil {
		t.Fatalf("valid original target was consumed by rejected rebinding: %v", err)
	}
}

func TestNPCScavengeRejectsTamperedCanonicalOrdersAtomically(t *testing.T) {
	tests := []struct {
		name   string
		tamper func(*NPCScavengeProposal)
	}{
		{name: "source order", tamper: func(p *NPCScavengeProposal) {
			p.SourceFloorOrder = []string{"bag", "protected", "later"}
		}},
		{name: "after room order", tamper: func(p *NPCScavengeProposal) {
			p.AfterRoomFloorOrder = []string{"protected", "later", "bag"}
		}},
		{name: "after NPC order", tamper: func(p *NPCScavengeProposal) {
			p.AfterNPCInventoryOrder = []string{"bag", "apple"}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state := npcScavengeFixture(t)
			proposal, err := state.PlanNPCScavenge("scavenger", 200, func(int, int) int { return 1 })
			if err != nil {
				t.Fatal(err)
			}
			tampered := proposal
			tc.tamper(&tampered)
			before := state.clone()
			if _, _, err := state.ApplyNPCScavenge(tampered); err == nil || !reflect.DeepEqual(state, before) {
				t.Fatalf("tampered %s accepted or mutated state: err=%v", tc.name, err)
			}
		})
	}
}

func TestNPCScavengeRejectsInvalidCanonicalOwnershipBeforeRoll(t *testing.T) {
	state := npcScavengeFixture(t)
	room := state.Rooms[1]
	item := room.Items.Items["bag"]
	item.Contents = []string{"missing-child"}
	room.Items.Items["bag"] = item
	state.Rooms[1] = room
	before := state.clone()
	called := false
	if _, err := state.PlanNPCScavenge("scavenger", 200, func(int, int) int {
		called = true
		return 1
	}); err == nil || called || !reflect.DeepEqual(state, before) {
		t.Fatalf("invalid ownership accepted/called RNG: called=%v err=%v", called, err)
	}
}

func TestNPCScavengeAttackGateDoesNotConsumeRoll(t *testing.T) {
	state := npcScavengeFixture(t)
	npc := state.NPCs["scavenger"]
	npc.Body.Timers[npcScavengeAttackTimer] = LegacyTimer{LastTime: 190, Interval: 20}
	state.NPCs["scavenger"] = npc
	proposal, err := state.PlanNPCScavenge("scavenger", 200, func(int, int) int {
		t.Fatal("attack-blocked scavenger consumed RNG")
		return 1
	})
	if err != nil || !proposal.Due || proposal.AttackReady || proposal.Attempted || proposal.Decision != NPCScavengeAttackNotReady {
		t.Fatalf("attack gate proposal=%+v err=%v", proposal, err)
	}
	next, _, err := state.ApplyNPCScavenge(proposal)
	if err != nil || !reflect.DeepEqual(next, state) {
		t.Fatalf("attack gate mutated state: err=%v", err)
	}
	replayBefore := next.clone()
	if _, _, err := next.ApplyNPCScavenge(proposal); err == nil || !reflect.DeepEqual(next, replayBefore) {
		t.Fatalf("attack-blocked no-op replay accepted or mutated state: err=%v", err)
	}
}
