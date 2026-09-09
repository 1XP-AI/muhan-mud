package world

import (
	"fmt"
	"reflect"
)

// The values below are the legacy update_active indexes/bits from
// src/mtype.h.  This file deliberately owns only the pre-combat maintenance
// part of update_active; attack, flee, death, and transport scheduling remain
// separate reducers.
const (
	npcMaintenanceHealTimer     = 8  // LT_HEALS
	npcMaintenanceWanderTimer   = 6  // LT_MWAND
	npcMaintenanceAttackTimer   = 3  // LT_ATTCK
	npcMaintenanceConfusedTimer = 40 // LT_BEFUD
	npcMaintenanceCharmedTimer  = 36 // LT_CHRMD
	npcMaintenanceCharmedFlag   = 50 // MCHARM
	npcMaintenanceConfusedFlag  = 51 // MBEFUD
)

// NPCMaintenanceAction is the deterministic projection of one pass through
// update_active.  Actions are kept in traversal order, including an NPC that
// is visited again after a wandering NPC resets C's cursor to first_active.
// WanderRoll is recorded so Apply can verify the RNG decision without calling
// an RNG a second time.
type NPCMaintenanceAction struct {
	NPCID             string `json:"npc_id"`
	RoomID            int16  `json:"room_id"`
	RemovedFromActive bool   `json:"removed_from_active,omitempty"`
	Wandered          bool   `json:"wandered,omitempty"`
	WanderRoll        int    `json:"wander_roll,omitempty"`
	HPBefore          int16  `json:"hp_before"`
	HPAfter           int16  `json:"hp_after"`
	MPBefore          int16  `json:"mp_before"`
	MPAfter           int16  `json:"mp_after"`
	ConfusedCleared   bool   `json:"confused_cleared,omitempty"`
	CharmedCleared    bool   `json:"charmed_cleared,omitempty"`
	AttackReady       bool   `json:"attack_ready,omitempty"`
	AttackInterval    int32  `json:"attack_interval,omitempty"`
}

// NPCMaintenanceResult is the durable receipt response for the bounded
// pre-combat update_active slice. ActiveNPCIDs is included to make active-list
// ordering observable without replaying the world reducer.
type NPCMaintenanceResult struct {
	Now          int32                  `json:"now"`
	Actions      []NPCMaintenanceAction `json:"actions,omitempty"`
	ActiveNPCIDs []string               `json:"active_npc_ids"`
}

// NPCMaintenanceProposal is a complete-snapshot candidate. The unexported
// before value prevents callers from serializing or substituting a partial
// room view; Apply requires the exact world snapshot that was planned.
type NPCMaintenanceProposal struct {
	Now               int32
	Actions           []NPCMaintenanceAction
	AfterActiveNPCIDs []string
	before            State
}

// PlanNPCMaintenance ports the non-combat prefix of src/update.c:update_active:
//
//   - remove active NPCs from rooms with no players;
//   - expire MBEFUD/MCHARM using LT(last)+interval;
//   - catch up HP/MP regeneration at the 60-second LT_HEALS cadence;
//   - record the next attack cadence (2s for dexterity >= 20, otherwise 3s);
//   - perform the source MWAND traffic roll and remove a wandering NPC.
//
// It intentionally stops before scavenging, spells, melee, player flee, and
// death. A nil enemy slice is unresolved migration state, not confirmed peace,
// so an occupied room fails closed before consuming randomness.
func (s State) PlanNPCMaintenance(now int32, roll func(int, int) int) (NPCMaintenanceProposal, error) {
	if err := s.Validate(); err != nil {
		return NPCMaintenanceProposal{}, err
	}
	if s.NPCs == nil {
		return NPCMaintenanceProposal{}, fmt.Errorf("NPC maintenance requires canonical NPC state")
	}
	if s.ActiveNPCIDs == nil {
		return NPCMaintenanceProposal{}, fmt.Errorf("NPC maintenance active order unresolved")
	}
	if roll == nil {
		return NPCMaintenanceProposal{}, fmt.Errorf("NPC maintenance requires RNG")
	}

	proposal := NPCMaintenanceProposal{
		Now:               now,
		before:            s.clone(),
		AfterActiveNPCIDs: append([]string{}, s.ActiveNPCIDs...),
	}
	next := s.clone()

	// C's wandering branch sets cp = first_active after a successful delete.
	// Replaying from index zero preserves that order while avoiding a second
	// source of iteration state in the durable request.
	for index := 0; index < len(next.ActiveNPCIDs); {
		npcID := next.ActiveNPCIDs[index]
		action, err := planNPCMaintenanceAction(next, npcID, now, roll)
		if err != nil {
			return NPCMaintenanceProposal{}, err
		}
		candidate, err := applyNPCMaintenanceAction(next, action, now)
		if err != nil {
			return NPCMaintenanceProposal{}, err
		}
		proposal.Actions = append(proposal.Actions, action)
		next = candidate
		if action.Wandered {
			index = 0
			continue
		}
		if action.RemovedFromActive {
			// Removing the current active entry shifts the next entry into the
			// same index. C advances cp to that next tag in this branch.
			continue
		}
		index++
	}
	proposal.AfterActiveNPCIDs = append([]string{}, next.ActiveNPCIDs...)
	return proposal, nil
}

// ApplyNPCMaintenance applies a proposal without calling RNG. Every action
// carries its source-bound roll and is checked against the current candidate,
// so a tampered response cannot publish a partial update.
func (s State) ApplyNPCMaintenance(proposal NPCMaintenanceProposal) (State, NPCMaintenanceResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, NPCMaintenanceResult{}, err
	}
	if proposal.before.Version == 0 || !reflect.DeepEqual(s, proposal.before) {
		return State{}, NPCMaintenanceResult{}, fmt.Errorf("stale NPC maintenance proposal")
	}
	next := s.clone()
	for _, action := range proposal.Actions {
		var err error
		next, err = applyNPCMaintenanceAction(next, action, proposal.Now)
		if err != nil {
			return State{}, NPCMaintenanceResult{}, err
		}
	}
	if !reflect.DeepEqual(next.ActiveNPCIDs, proposal.AfterActiveNPCIDs) {
		return State{}, NPCMaintenanceResult{}, fmt.Errorf("NPC maintenance active order mismatch")
	}
	if err := next.Validate(); err != nil {
		return State{}, NPCMaintenanceResult{}, err
	}
	result := NPCMaintenanceResult{
		Now:          proposal.Now,
		Actions:      append([]NPCMaintenanceAction{}, proposal.Actions...),
		ActiveNPCIDs: append([]string{}, next.ActiveNPCIDs...),
	}
	return next, result, nil
}

func planNPCMaintenanceAction(s State, npcID string, now int32, roll func(int, int) int) (NPCMaintenanceAction, error) {
	npc, ok := s.NPCs[npcID]
	if !ok || npcID == "" || npc.Body.Type != 1 {
		return NPCMaintenanceAction{}, fmt.Errorf("unknown active NPC %q", npcID)
	}
	room, ok := s.Rooms[npc.Body.RoomID]
	if !ok || !containsString(room.NPCIDs, npcID) {
		return NPCMaintenanceAction{}, fmt.Errorf("active NPC %q room membership absent", npcID)
	}
	action := NPCMaintenanceAction{
		NPCID:    npcID,
		RoomID:   npc.Body.RoomID,
		HPBefore: npc.Body.HPCurrent,
		HPAfter:  npc.Body.HPCurrent,
		MPBefore: npc.Body.MPCurrent,
		MPAfter:  npc.Body.MPCurrent,
	}
	if len(room.PlayerIDs) == 0 {
		action.RemovedFromActive = true
		return action, nil
	}
	if npc.Enemies == nil {
		return NPCMaintenanceAction{}, fmt.Errorf("NPC %q enemy relations unresolved", npcID)
	}

	body := npc.Body
	applyNPCMaintenanceBody(&body, now, &action)
	attackBlocked := int64(npc.Body.Timers[npcMaintenanceAttackTimer].LastTime)+int64(npc.Body.Timers[npcMaintenanceAttackTimer].Interval) > int64(now)
	if attackBlocked {
		// C continues before scavenging/wandering when the attack timer is in
		// the future. Status expiry/healing above still applies.
		return action, nil
	}

	// C evaluates mrand before first_enm in this short-circuit expression.
	// Preserve that random-call order even when the NPC already has a target.
	wanderDue := int64(now)-int64(npc.Body.Timers[npcMaintenanceWanderTimer].LastTime) > 20
	if !wanderDue || flag(npc.Body.Flags[:], npcScavengedFlagForMaintenance) || flag(npc.Body.Flags[:], npcPermanentFlag) || flag(npc.Body.Flags[:], npcDMFollowFlag) {
		return action, nil
	}
	wanderRoll, err := randomIn(roll, 1, 100)
	if err != nil {
		return NPCMaintenanceAction{}, err
	}
	action.WanderRoll = wanderRoll
	if wanderRoll <= int(room.Resource.Traffic) && len(npc.Enemies) == 0 {
		action.Wandered = true
		action.RemovedFromActive = true
	}
	// update_active refreshes the raw wander timestamp whenever the 20s
	// window was reached, including a failed roll or an NPC with an enemy.
	body.Timers[npcMaintenanceWanderTimer].LastTime = now
	npc.Body = body
	return action, nil
}

// npcScavengedFlagForMaintenance is MHASSC. It is intentionally local to
// this bounded prefix; the actual MSCAVE object-transfer reducer is deferred.
const npcScavengedFlagForMaintenance = 18

func applyNPCMaintenanceBody(body *LegacyMonster, now int32, action *NPCMaintenanceAction) {
	if flag(body.Flags[:], npcMaintenanceConfusedFlag) && int64(body.Timers[npcMaintenanceConfusedTimer].LastTime)+int64(body.Timers[npcMaintenanceConfusedTimer].Interval) < int64(now) {
		body.Flags[npcMaintenanceConfusedFlag/8] &^= 1 << (npcMaintenanceConfusedFlag % 8)
		action.ConfusedCleared = true
	}
	if healed := npcMaintenanceHeal(body, now); healed {
		action.HPAfter = body.HPCurrent
		action.MPAfter = body.MPCurrent
	}
	if flag(body.Flags[:], npcMaintenanceCharmedFlag) && int64(body.Timers[npcMaintenanceCharmedTimer].LastTime)+int64(body.Timers[npcMaintenanceCharmedTimer].Interval) < int64(now) {
		body.Flags[npcMaintenanceCharmedFlag/8] &^= 1 << (npcMaintenanceCharmedFlag % 8)
		action.CharmedCleared = true
	}
	attackDeadline := int64(body.Timers[npcMaintenanceAttackTimer].LastTime) + int64(body.Timers[npcMaintenanceAttackTimer].Interval)
	if attackDeadline <= int64(now) {
		body.Timers[npcMaintenanceAttackTimer].LastTime = now
		if int(int8(body.Stats[1])) < 20 {
			body.Timers[npcMaintenanceAttackTimer].Interval = 3
		} else {
			body.Timers[npcMaintenanceAttackTimer].Interval = 2
		}
		action.AttackReady = true
		action.AttackInterval = body.Timers[npcMaintenanceAttackTimer].Interval
	}
}

func npcMaintenanceHeal(body *LegacyMonster, now int32) bool {
	if body.HPCurrent >= body.HPMax && body.MPCurrent >= body.MPMax {
		return false
	}
	deadline := int64(body.Timers[npcMaintenanceHealTimer].LastTime) + int64(body.Timers[npcMaintenanceHealTimer].Interval)
	if deadline > int64(now) {
		return false
	}
	steps := (int64(now)-deadline)/60 + 1
	if steps < 1 {
		return false
	}
	hpStep := int(body.HPMax) / 10
	if hpStep < 1 {
		hpStep = 1
	}
	mpStep := int(body.MPMax) / 6
	if mpStep < 1 {
		mpStep = 1
	}
	hp := int(body.HPCurrent) + int(steps)*hpStep
	if hp > int(body.HPMax) {
		hp = int(body.HPMax)
	}
	mp := int(body.MPCurrent) + int(steps)*mpStep
	if mp > int(body.MPMax) {
		mp = int(body.MPMax)
	}
	if hp < -32768 || hp > 32767 || mp < -32768 || mp > 32767 {
		return false
	}
	body.HPCurrent = int16(hp)
	body.MPCurrent = int16(mp)
	body.Timers[npcMaintenanceHealTimer].LastTime = now
	body.Timers[npcMaintenanceHealTimer].Interval = 60
	return true
}

func applyNPCMaintenanceAction(s State, action NPCMaintenanceAction, now int32) (State, error) {
	npc, ok := s.NPCs[action.NPCID]
	if !ok || action.NPCID == "" || npc.Body.RoomID != action.RoomID {
		return State{}, fmt.Errorf("NPC maintenance identity changed for %q", action.NPCID)
	}
	room, ok := s.Rooms[action.RoomID]
	if !ok || !containsString(room.NPCIDs, action.NPCID) {
		return State{}, fmt.Errorf("NPC maintenance room membership changed for %q", action.NPCID)
	}
	if action.HPBefore != npc.Body.HPCurrent || action.MPBefore != npc.Body.MPCurrent {
		return State{}, fmt.Errorf("NPC maintenance vital snapshot changed for %q", action.NPCID)
	}
	active := containsString(s.ActiveNPCIDs, action.NPCID)
	if !active {
		return State{}, fmt.Errorf("NPC maintenance active identity absent for %q", action.NPCID)
	}
	if len(room.PlayerIDs) == 0 {
		if !action.RemovedFromActive || action.Wandered || action.WanderRoll != 0 || action.ConfusedCleared || action.CharmedCleared || action.AttackReady || action.HPAfter != action.HPBefore || action.MPAfter != action.MPBefore {
			return State{}, fmt.Errorf("invalid inactive-room NPC maintenance action")
		}
		next := s.clone()
		next.ActiveNPCIDs, _ = removeNPCID(next.ActiveNPCIDs, action.NPCID)
		return next, nil
	}
	if npc.Enemies == nil {
		return State{}, fmt.Errorf("NPC %q enemy relations unresolved", action.NPCID)
	}

	body := npc.Body
	expected := action
	expected.HPAfter = body.HPCurrent
	expected.MPAfter = body.MPCurrent
	expected.ConfusedCleared = false
	expected.CharmedCleared = false
	expected.AttackReady = false
	expected.AttackInterval = 0
	applyNPCMaintenanceBody(&body, now, &expected)
	if action.HPAfter != expected.HPAfter || action.MPAfter != expected.MPAfter || action.ConfusedCleared != expected.ConfusedCleared || action.CharmedCleared != expected.CharmedCleared || action.AttackReady != expected.AttackReady || action.AttackInterval != expected.AttackInterval {
		return State{}, fmt.Errorf("NPC maintenance action does not match C prefix for %q", action.NPCID)
	}
	attackBlocked := int64(npc.Body.Timers[npcMaintenanceAttackTimer].LastTime)+int64(npc.Body.Timers[npcMaintenanceAttackTimer].Interval) > int64(now)
	wanderDue := !attackBlocked && int64(now)-int64(npc.Body.Timers[npcMaintenanceWanderTimer].LastTime) > 20 && !flag(npc.Body.Flags[:], npcScavengedFlagForMaintenance) && !flag(npc.Body.Flags[:], npcPermanentFlag) && !flag(npc.Body.Flags[:], npcDMFollowFlag)
	if !wanderDue {
		if action.Wandered || action.WanderRoll != 0 || action.RemovedFromActive {
			return State{}, fmt.Errorf("unexpected NPC wander action for %q", action.NPCID)
		}
	} else {
		if action.WanderRoll < 1 || action.WanderRoll > 100 {
			return State{}, fmt.Errorf("invalid NPC wander roll for %q", action.NPCID)
		}
		wantWander := action.WanderRoll <= int(room.Resource.Traffic) && len(npc.Enemies) == 0
		if action.Wandered != wantWander || action.RemovedFromActive != wantWander {
			return State{}, fmt.Errorf("NPC wander decision changed for %q", action.NPCID)
		}
		body.Timers[npcMaintenanceWanderTimer].LastTime = now
	}
	if action.RemovedFromActive && !action.Wandered {
		return State{}, fmt.Errorf("NPC active removal without inactive room or wander")
	}

	next := s.clone()
	nextNPC := next.NPCs[action.NPCID]
	nextNPC.Body = body
	if action.Wandered {
		nextRoom := next.Rooms[action.RoomID]
		nextRoom.NPCIDs, _ = removeNPCID(nextRoom.NPCIDs, action.NPCID)
		next.Rooms[action.RoomID] = nextRoom
		next.ActiveNPCIDs, _ = removeNPCID(next.ActiveNPCIDs, action.NPCID)
		if nextNPC.FollowingPlayerID != "" {
			leader, exists := next.Players[nextNPC.FollowingPlayerID]
			if !exists {
				return State{}, fmt.Errorf("NPC wander follower leader absent")
			}
			leader.NPCFollowerIDs, _ = removeString(leader.NPCFollowerIDs, action.NPCID)
			if leader.FollowerRefs != nil {
				leader.FollowerRefs, _ = removeFollowerRef(leader.FollowerRefs, EntityRef{Kind: "npc", ID: action.NPCID})
			}
			next.Players[nextNPC.FollowingPlayerID] = leader
		}
		delete(next.NPCs, action.NPCID)
		removeNPCEnemyReferences(&next, action.NPCID)
	} else {
		next.NPCs[action.NPCID] = nextNPC
	}
	if err := next.Validate(); err != nil {
		return State{}, err
	}
	return next, nil
}
