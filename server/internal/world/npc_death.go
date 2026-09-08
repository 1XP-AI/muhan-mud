package world

import "fmt"

const (
	npcTradeFlag  = 37 // MTRADE: inventory is handled by the trade path.
	npcSummonFlag = 61 // MSUMMO: summon_crt is not part of this reducer yet.
)

// NPCDeathResult describes the committed part of creature.c:die for a
// monster. The state transition removes the dead identity, awards every
// eligible online player exactly once, moves legacy drops into the canonical
// room graph, and updates the permanent-monster respawn timer.
type NPCDeathResult struct {
	TargetID              string
	TargetName            string
	ExperienceAward       int32
	QuestExperienceAward  int32
	DroppedObjectCount    int
	DroppedGold           int32
	PermanentTimerUpdated bool
}

func npcQuestExperience(quest byte) (int32, bool) {
	if quest == 0 || quest > 128 {
		return 0, false
	}
	// quest_exp[] in src/global.c. Entries after 24 are intentionally the
	// stable 125-point tail used by the original table.
	known := [...]int32{
		120, 500, 1000, 1000, 5000, 8000, 10000, 20000,
		50000, 80000, 100000, 200000, 500000, 800000, 2500, 2500,
		2500, 5, 5, 5, 125, 125, 125, 125,
	}
	if int(quest) <= len(known) {
		return known[quest-1], true
	}
	return 125, true
}

func addUnassignedProficiency(body *LegacyMonster, experience int32) error {
	if experience < 0 {
		return fmt.Errorf("negative proficiency award")
	}
	amount := int64(experience) / 9
	for i := 0; i < 9; i++ {
		if i < len(body.Proficiency) {
			value := int64(body.Proficiency[i]) + amount
			if value > int64(^uint32(0)>>1) || value < 0 {
				return fmt.Errorf("proficiency overflow")
			}
			body.Proficiency[i] = int32(value)
			continue
		}
		index := i - len(body.Proficiency)
		value := int64(body.Realm[index]) + amount
		if value > int64(^uint32(0)>>1) || value < 0 {
			return fmt.Errorf("realm proficiency overflow")
		}
		body.Realm[index] = int32(value)
	}
	return nil
}

func addNPCExperience(body *LegacyMonster, award int32) error {
	if award < 0 || int64(body.Experience)+int64(award) > int64(^uint32(0)>>1) {
		return fmt.Errorf("NPC experience award overflow")
	}
	body.Experience += award
	return nil
}

func removeNPCID(ids []string, want string) ([]string, bool) {
	removed := false
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == want {
			removed = true
			continue
		}
		out = append(out, id)
	}
	return out, removed
}

func removeNPCFollowerRef(refs []EntityRef, want string) []EntityRef {
	out := make([]EntityRef, 0, len(refs))
	for _, ref := range refs {
		if ref.Kind == "npc" && ref.ID == want {
			continue
		}
		out = append(out, ref)
	}
	return out
}

func npcDropCollection(body LegacyMonster, allocate func() (string, error)) (ItemCollection, error) {
	if body.Gold < 0 {
		return ItemCollection{}, fmt.Errorf("negative NPC gold")
	}
	objects := cloneObjects(body.Inventory)
	if body.Gold > 0 {
		objects = append(objects, LegacyObject{
			Name:  fmt.Sprintf("%d냥", body.Gold),
			Value: body.Gold,
			Type:  10, // C's load_obj(0) currency object.
		})
	}
	if len(objects) == 0 {
		return ItemCollection{Items: map[string]Item{}}, nil
	}
	if allocate == nil {
		return ItemCollection{}, fmt.Errorf("NPC death drops require item ID allocator")
	}
	return ImportItems(objects, allocate)
}

func npcDropCollectionState(npc NPCState, allocate func() (string, error)) (ItemCollection, error) {
	if npc.Items == nil {
		return npcDropCollection(npc.Body, allocate)
	}
	if err := npc.Items.Validate(); err != nil {
		return ItemCollection{}, err
	}
	for _, id := range npc.Items.Ready {
		if id != "" {
			return ItemCollection{}, fmt.Errorf("NPC canonical inventory cannot equip items")
		}
	}
	drops := npc.Items.clone()
	if npc.Body.Gold == 0 {
		return drops, nil
	}
	if npc.Body.Gold < 0 || allocate == nil {
		return ItemCollection{}, fmt.Errorf("NPC gold drop requires item ID allocator")
	}
	fresh, err := ImportItems([]LegacyObject{{
		Name:  fmt.Sprintf("%d냥", npc.Body.Gold),
		Value: npc.Body.Gold,
		Type:  10,
	}}, allocate)
	if err != nil {
		return ItemCollection{}, err
	}
	plan, err := TransferItemRoots(fresh, drops, fresh.Inventory)
	if err != nil {
		return ItemCollection{}, err
	}
	return plan.Destination, nil
}

// PlanNPCDeath ports the durable, identity-based portion of creature.c:die
// for a canonical NPC. It is intentionally all-or-nothing: unsupported
// summon behavior, missing permanent provenance, unresolved enemy lists, and
// unallocated item drops return an error before the source snapshot changes.
// The caller must commit the returned state before publishing output.
func (s State) PlanNPCDeath(targetID, attackerID string, now int32, allocate func() (string, error)) (State, NPCDeathResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, NPCDeathResult{}, err
	}
	target, ok := s.NPCs[targetID]
	if s.NPCs == nil || !ok || target.Body.Type != 1 || target.Body.HPCurrent >= 1 {
		return State{}, NPCDeathResult{}, fmt.Errorf("NPC is not in a lethal state")
	}
	if target.Enemies == nil {
		return State{}, NPCDeathResult{}, fmt.Errorf("NPC enemy relations unresolved")
	}
	attacker, ok := s.Players[attackerID]
	if !ok || !attacker.Online || attacker.Body.RoomID != target.Body.RoomID {
		return State{}, NPCDeathResult{}, fmt.Errorf("online NPC death attacker required")
	}
	room, ok := s.Rooms[target.Body.RoomID]
	if !ok || !containsString(room.NPCIDs, targetID) {
		return State{}, NPCDeathResult{}, fmt.Errorf("NPC room membership absent")
	}
	if flag(target.Body.Flags[:], npcSummonFlag) {
		return State{}, NPCDeathResult{}, fmt.Errorf("NPC summon death transition pending")
	}
	permanent := flag(target.Body.Flags[:], npcPermanentFlag)
	if permanent {
		if target.PermanentOrigin == nil || target.PermanentOrigin.RoomID != target.Body.RoomID || target.PermanentOrigin.Slot >= 10 {
			return State{}, NPCDeathResult{}, fmt.Errorf("permanent NPC origin unresolved")
		}
		if room.Resource.PermanentMonsters[target.PermanentOrigin.Slot].Misc == 0 {
			return State{}, NPCDeathResult{}, fmt.Errorf("permanent NPC timer absent")
		}
	}
	if target.Body.Experience < 0 || target.Body.Gold < 0 {
		return State{}, NPCDeathResult{}, fmt.Errorf("negative NPC reward")
	}
	trade := flag(target.Body.Flags[:], npcTradeFlag)
	drops := ItemCollection{Items: map[string]Item{}}
	if !trade {
		var err error
		drops, err = npcDropCollectionState(target, allocate)
		if err != nil {
			return State{}, NPCDeathResult{}, err
		}
	} else if target.Body.Gold > 0 {
		// MTRADE suppresses the inventory handoff, not C's gold object.
		var err error
		drops, err = npcDropCollection(LegacyMonster{Gold: target.Body.Gold}, allocate)
		if err != nil {
			return State{}, NPCDeathResult{}, err
		}
	}
	if len(drops.Items) != 0 && room.Items == nil {
		return State{}, NPCDeathResult{}, fmt.Errorf("NPC drops require canonical room items")
	}

	// Resolve C's first_enm order before cloning. Negative damage is the
	// preserved unresolved-membership marker used by logout/recovery and must
	// not mint experience.
	type award struct {
		playerID string
		amount   int32
	}
	awards := make([]award, 0, len(target.Enemies))
	remaining := int64(target.Body.Experience)
	hpMax := int64(target.Body.HPMax)
	if hpMax < 1 {
		hpMax = 1
	}
	for _, enemy := range target.Enemies {
		if enemy.Target.Kind != "player" || enemy.Damage <= 0 || remaining == 0 {
			continue
		}
		player, exists := s.Players[enemy.Target.ID]
		if !exists || !player.Online {
			continue
		}
		value := int64(target.Body.Experience) * int64(enemy.Damage) / hpMax
		if value > remaining {
			value = remaining
		}
		if value <= 0 {
			continue
		}
		awards = append(awards, award{playerID: enemy.Target.ID, amount: int32(value)})
		remaining -= value
	}
	attackerAward := int32(0)
	for _, item := range awards {
		if item.playerID == attackerID {
			attackerAward += item.amount
		}
	}
	questAward := int32(0)
	if target.Body.Quest != 0 {
		var valid bool
		questAward, valid = npcQuestExperience(target.Body.Quest)
		if !valid {
			return State{}, NPCDeathResult{}, fmt.Errorf("NPC quest number out of range")
		}
		questIndex := int(target.Body.Quest) - 1
		if flag(attacker.Body.Quests[:], uint(questIndex)) {
			questAward = 0
		}
	}

	next := s.clone()
	nextRoom := next.Rooms[target.Body.RoomID]
	nextRoom.NPCIDs, _ = removeNPCID(nextRoom.NPCIDs, targetID)
	if len(drops.Items) != 0 {
		plan, err := TransferItemRoots(drops, *nextRoom.Items, drops.Inventory)
		if err != nil {
			return State{}, NPCDeathResult{}, err
		}
		nextRoom.Items = &plan.Destination
	}
	next.Rooms[target.Body.RoomID] = nextRoom
	if next.ActiveNPCIDs != nil {
		next.ActiveNPCIDs, _ = removeNPCID(next.ActiveNPCIDs, targetID)
	}
	if permanent {
		origin := *target.PermanentOrigin
		originRoom := next.Rooms[origin.RoomID]
		originRoom.Resource.PermanentMonsters[origin.Slot].LastTime = now
		next.Rooms[origin.RoomID] = originRoom
	}
	if target.FollowingPlayerID != "" {
		leader, exists := next.Players[target.FollowingPlayerID]
		if !exists {
			return State{}, NPCDeathResult{}, fmt.Errorf("NPC follower leader absent")
		}
		leader.NPCFollowerIDs, _ = removeNPCID(leader.NPCFollowerIDs, targetID)
		leader.FollowerRefs = removeNPCFollowerRef(leader.FollowerRefs, targetID)
		next.Players[target.FollowingPlayerID] = leader
	}
	for id, npc := range next.NPCs {
		if id == targetID || npc.Enemies == nil {
			continue
		}
		kept := make([]NPCEnemy, 0, len(npc.Enemies))
		for _, enemy := range npc.Enemies {
			if enemy.Target.Kind == "npc" && enemy.Target.ID == targetID {
				continue
			}
			kept = append(kept, enemy)
		}
		npc.Enemies = kept
		next.NPCs[id] = npc
	}
	delete(next.NPCs, targetID)
	for _, item := range awards {
		player := next.Players[item.playerID]
		if err := addNPCExperience(&player.Body, item.amount); err != nil {
			return State{}, NPCDeathResult{}, err
		}
		player.Body.Alignment -= target.Body.Alignment / 5
		if player.Body.Alignment > 1000 {
			player.Body.Alignment = 1000
		}
		if player.Body.Alignment < -1000 {
			player.Body.Alignment = -1000
		}
		next.Players[item.playerID] = player
	}
	if target.Body.Quest != 0 && questAward > 0 {
		player := next.Players[attackerID]
		questIndex := int(target.Body.Quest) - 1
		player.Body.Quests[questIndex/8] |= 1 << (questIndex % 8)
		if err := addNPCExperience(&player.Body, questAward); err != nil {
			return State{}, NPCDeathResult{}, err
		}
		if err := addUnassignedProficiency(&player.Body, questAward); err != nil {
			return State{}, NPCDeathResult{}, err
		}
		next.Players[attackerID] = player
	}
	if err := next.Validate(); err != nil {
		return State{}, NPCDeathResult{}, err
	}
	return next, NPCDeathResult{
		TargetID: targetID, TargetName: target.Body.Name,
		ExperienceAward: attackerAward, QuestExperienceAward: questAward,
		DroppedObjectCount: len(drops.Items), DroppedGold: target.Body.Gold,
		PermanentTimerUpdated: permanent,
	}, nil
}
