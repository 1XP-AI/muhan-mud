package world

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

// npcMaintenanceFixture is a direct C update_active fixture. The timer and
// flag indexes come from src/mtype.h; the expected values below follow
// src/update.c:291-365, including the strict expiry checks and the raw
// LT_MWAND timestamp used by the 20-second traffic roll.
func npcMaintenanceFixture(t *testing.T) State {
	t.Helper()
	confused := [8]byte{}
	confused[npcMaintenanceConfusedFlag/8] |= 1 << (npcMaintenanceConfusedFlag % 8)
	confused[npcMaintenanceCharmedFlag/8] |= 1 << (npcMaintenanceCharmedFlag % 8)
	state := State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Name: "회복실"}, Traffic: 100},
				PlayerIDs: []string{"hero"},
				NPCIDs:    []string{"healer"},
			},
			2: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 2, Name: "방황실"}, Traffic: 100},
				PlayerIDs: []string{"scout"},
				NPCIDs:    []string{"wanderer"},
			},
			3: {
				Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 3, Name: "빈방"}},
				NPCIDs:   []string{"sleeping"},
			},
		},
		Players: map[string]PlayerState{
			"hero":  {Body: LegacyMonster{Name: "영웅", Type: 0, RoomID: 1}, Online: true},
			"scout": {Body: LegacyMonster{Name: "정찰자", Type: 0, RoomID: 2}, Online: true},
		},
		NPCs: map[string]NPCState{
			"healer": {
				Body: LegacyMonster{
					Name: "회복 늑대", Type: 1, RoomID: 1, HPMax: 100, HPCurrent: 50,
					MPMax: 60, MPCurrent: 10, Stats: [5]byte{10, 19}, Flags: confused,
					Timers: [45]LegacyTimer{
						3:  {LastTime: 0, Interval: 0},
						6:  {LastTime: 0, Interval: 0},
						8:  {LastTime: 0, Interval: 60},
						36: {LastTime: 100, Interval: 20},
						40: {LastTime: 100, Interval: 20},
					},
				},
				Enemies: []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "hero"}, Damage: 0}},
			},
			"wanderer": {
				Body: LegacyMonster{
					Name: "방황 늑대", Type: 1, RoomID: 2, HPMax: 10, HPCurrent: 10,
					MPMax: 10, MPCurrent: 10, Stats: [5]byte{10, 20},
					Timers: [45]LegacyTimer{3: {LastTime: 0}, 6: {LastTime: 0}},
				},
				Enemies: []NPCEnemy{},
			},
			"sleeping": {
				Body:    LegacyMonster{Name: "잠든 늑대", Type: 1, RoomID: 3, HPMax: 10, HPCurrent: 10},
				Enemies: nil, // empty rooms are removed before C inspects first_enm
			},
		},
		// C's first_active order is intentionally not room order.
		ActiveNPCIDs: []string{"healer", "wanderer", "sleeping"},
	}
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	return state
}

func TestPlanApplyNPCMaintenanceMatchesCOrderAndRestart(t *testing.T) {
	state := npcMaintenanceFixture(t)
	before := state.clone()
	var calls [][2]int
	rolls := []int{50, 1}
	roll := func(low, high int) int {
		calls = append(calls, [2]int{low, high})
		if len(calls) > len(rolls) {
			return low - 1
		}
		return rolls[len(calls)-1]
	}

	proposal, err := state.PlanNPCMaintenance(200, roll)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(state, before) {
		t.Fatal("planning mutated source state")
	}
	ids := make([]string, 0, len(proposal.Actions))
	for _, action := range proposal.Actions {
		ids = append(ids, action.NPCID)
	}
	if want := []string{"healer", "wanderer", "healer", "sleeping"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("C active traversal after wander reset: got=%v want=%v", ids, want)
	}
	if len(calls) != 2 || calls[0] != [2]int{1, 100} || calls[1] != [2]int{1, 100} {
		t.Fatalf("C mrand order=%v", calls)
	}
	first := proposal.Actions[0]
	if first.HPBefore != 50 || first.HPAfter != 80 || first.MPBefore != 10 || first.MPAfter != 40 || !first.ConfusedCleared || !first.CharmedCleared || !first.AttackReady || first.AttackInterval != 3 || first.WanderRoll != 50 || first.Wandered {
		t.Fatalf("healer maintenance=%+v", first)
	}
	wander := proposal.Actions[1]
	if !wander.Wandered || !wander.RemovedFromActive || wander.WanderRoll != 1 || !wander.AttackReady || wander.AttackInterval != 2 {
		t.Fatalf("wander maintenance=%+v", wander)
	}
	if !reflect.DeepEqual(proposal.AfterActiveNPCIDs, []string{"healer"}) {
		t.Fatalf("active result=%v", proposal.AfterActiveNPCIDs)
	}

	next, result, err := state.ApplyNPCMaintenance(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if result.Now != 200 || !reflect.DeepEqual(result.ActiveNPCIDs, []string{"healer"}) || len(result.Actions) != 4 {
		t.Fatalf("result=%+v", result)
	}
	if next.NPCs["healer"].Body.HPCurrent != 80 || next.NPCs["healer"].Body.MPCurrent != 40 || next.NPCs["healer"].Body.Timers[npcMaintenanceHealTimer].LastTime != 200 || next.NPCs["healer"].Body.Timers[npcMaintenanceAttackTimer].Interval != 3 {
		t.Fatalf("healer state=%+v", next.NPCs["healer"].Body)
	}
	if _, ok := next.NPCs["wanderer"]; ok || len(next.Rooms[2].NPCIDs) != 0 || next.NPCs["sleeping"].Body.RoomID != 3 {
		t.Fatalf("wander/inactive indexes rooms=%+v npcs=%+v", next.Rooms, next.NPCs)
	}
	healerAfter := next.NPCs["healer"]
	if flag(healerAfter.Body.Flags[:], npcMaintenanceConfusedFlag) || flag(healerAfter.Body.Flags[:], npcMaintenanceCharmedFlag) {
		t.Fatal("expired NPC flags remain")
	}
}

func TestNPCMaintenanceUsesStrictDeadlinesAndDoesNotRerollCooldown(t *testing.T) {
	state := npcMaintenanceFixture(t)
	healer := state.NPCs["healer"]
	healer.Body.Timers[npcMaintenanceAttackTimer] = LegacyTimer{LastTime: 200, Interval: 0}
	healer.Body.Timers[npcMaintenanceHealTimer] = LegacyTimer{LastTime: 140, Interval: 60}     // deadline == now
	healer.Body.Timers[npcMaintenanceConfusedTimer] = LegacyTimer{LastTime: 180, Interval: 20} // deadline == now
	healer.Body.Timers[npcMaintenanceCharmedTimer] = LegacyTimer{LastTime: 180, Interval: 20}
	state.NPCs["healer"] = healer
	// Keep only the occupied healer so the fixture's unrelated RNG calls do
	// not obscure the strict boundary assertions.
	state.ActiveNPCIDs = []string{"healer"}
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	rollCalls := 0
	proposal, err := state.PlanNPCMaintenance(200, func(int, int) int {
		rollCalls++
		return 99
	})
	if err != nil {
		t.Fatal(err)
	}
	action := proposal.Actions[0]
	if action.ConfusedCleared || action.CharmedCleared || action.HPAfter != 60 || action.MPAfter != 20 || !action.AttackReady || action.AttackInterval != 3 {
		t.Fatalf("strict deadline action=%+v", action)
	}
	if rollCalls != 1 || action.WanderRoll != 99 {
		t.Fatalf("wander RNG calls=%d action=%+v", rollCalls, action)
	}
}

func TestNPCMaintenanceRejectsUnresolvedOrInvalidRNGAtomically(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*State)
		roll   func(int, int) int
	}{
		{
			name: "unresolved enemy relation",
			mutate: func(s *State) {
				npc := s.NPCs["healer"]
				npc.Enemies = nil
				s.NPCs["healer"] = npc
			},
			roll: func(int, int) int { t.Fatal("unresolved relation consumed RNG"); return 1 },
		},
		{
			name: "invalid wander random",
			mutate: func(s *State) {
				s.ActiveNPCIDs = []string{"wanderer"}
			},
			roll: func(low, _ int) int { return low - 1 },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state := npcMaintenanceFixture(t)
			tc.mutate(&state)
			before, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			proposal, err := state.PlanNPCMaintenance(200, tc.roll)
			if err == nil || !reflect.DeepEqual(proposal, NPCMaintenanceProposal{}) {
				t.Fatalf("accepted invalid maintenance proposal=%+v err=%v", proposal, err)
			}
			after, marshalErr := json.Marshal(state)
			if marshalErr != nil || !bytes.Equal(before, after) {
				t.Fatalf("invalid plan mutated state before=%s after=%s err=%v", before, after, marshalErr)
			}
		})
	}
}

func TestNPCMaintenanceApplyRejectsStaleOrTamperedProposalAtomically(t *testing.T) {
	state := npcMaintenanceFixture(t)
	proposal, err := state.PlanNPCMaintenance(200, func(low, _ int) int { return low })
	if err != nil {
		t.Fatal(err)
	}
	mutated := state.clone()
	healer := mutated.NPCs["healer"]
	healer.Body.HPCurrent++
	mutated.NPCs["healer"] = healer
	if _, _, err := mutated.ApplyNPCMaintenance(proposal); err == nil {
		t.Fatal("stale proposal accepted")
	}

	tampered := proposal
	tampered.Actions = append([]NPCMaintenanceAction(nil), proposal.Actions...)
	tampered.Actions[1].WanderRoll = 101
	before, _ := json.Marshal(state)
	if _, _, err := state.ApplyNPCMaintenance(tampered); err == nil {
		t.Fatal("tampered roll accepted")
	}
	after, _ := json.Marshal(state)
	if !bytes.Equal(before, after) {
		t.Fatal("failed apply mutated source")
	}
}
