package world

import "fmt"

// NPCPlayerDeathResult is the committed projection of creature.c:die's
// MONSTER -> PLAYER path.  PlayerDeathResult is embedded so the room-entry and
// broadcast signals have the same shape as the existing player/self reducer;
// NPCID/VictimID keep the canonical identities in the receipt boundary.
//
// This is deliberately a plan-only API.  The caller must publish the returned
// State and result together after its durable world receipt commits.  No room,
// player, or NPC map is mutated while the plan is being built.
type NPCPlayerDeathResult struct {
	PlayerDeathResult
	NPCID    string
	VictimID string
}

// PlanNPCPlayerDeath composes the source-backed continuation after a canonical
// NPC has already dealt lethal damage to a canonical PLAYER.  It does not roll
// NPC attack damage and it does not decide whether an attack was lethal; the
// caller supplies a snapshot where victim HP is below one.  That distinction
// prevents a non-lethal combat round from entering the irreversible death
// path.
//
// Supported source order is the PLAYER branch of creature.c:die:
//
//	progression -> LT_PLYKL reset -> NPC enemy removal -> equipment/HP/MP
//	recovery -> family-war result -> source departure -> room 1008 entry.
//
// The NPC attacker never receives a player kill timer.  The C else branch
// calls del_enm_crt, so this plan removes only the exact canonical enemy edge.
// Unresolved war state, unresolved active/enemy/follower identity, legacy item
// ownership, and non-canonical respawn state fail closed before a candidate is
// returned.  Death-description, output formatting,
// savegame, summon, and transport side effects remain outside this boundary.
func (s State) PlanNPCPlayerDeath(npcID, victimID string, now int32, view SceneOptions, catalog SpawnCatalog, roll func(int, int) int, allocate func() (string, error)) (State, NPCPlayerDeathResult, error) {
	zero := NPCPlayerDeathResult{}
	if err := s.Validate(); err != nil {
		return State{}, zero, err
	}
	if npcID == "" || victimID == "" || npcID == victimID {
		return State{}, zero, fmt.Errorf("invalid NPC/player death identities")
	}
	if s.NPCs == nil {
		return State{}, zero, fmt.Errorf("canonical NPC death attacker required")
	}
	npc, ok := s.NPCs[npcID]
	if !ok || npc.Body.Type != 1 || npc.Body.HPCurrent < 1 {
		return State{}, zero, fmt.Errorf("NPC death attacker absent or not alive")
	}
	victim, ok := s.Players[victimID]
	if !ok || !victim.Online {
		return State{}, zero, fmt.Errorf("online player death victim required")
	}
	if victim.Body.HPCurrent >= 1 {
		return State{}, zero, fmt.Errorf("NPC player death requires lethal HP")
	}
	if npc.Body.RoomID != victim.Body.RoomID {
		return State{}, zero, fmt.Errorf("NPC/player death room mismatch")
	}
	source, ok := s.Rooms[victim.Body.RoomID]
	if !ok || !containsString(source.NPCIDs, npcID) || !containsString(source.PlayerIDs, victimID) {
		return State{}, zero, fmt.Errorf("NPC/player death room membership absent")
	}
	// A nil active list is an unresolved legacy first_active chain.  It cannot
	// prove that this NPC was the attacker selected by update_active.
	if s.ActiveNPCIDs == nil || !containsString(s.ActiveNPCIDs, npcID) {
		return State{}, zero, fmt.Errorf("NPC active membership unresolved")
	}
	if npc.Enemies == nil {
		return State{}, zero, fmt.Errorf("NPC enemy relations unresolved")
	}
	enemy := EntityRef{Kind: "player", ID: victimID}
	foundEnemy := false
	for _, relation := range npc.Enemies {
		if relation.Target != enemy {
			continue
		}
		// Negative damage is the existing unresolved/recovery marker.  A zero
		// damage relation is still a valid C enemy edge; update_active can
		// attack before its contribution is persisted.
		if relation.Damage < 0 {
			return State{}, zero, fmt.Errorf("NPC enemy relation unresolved")
		}
		foundEnemy = true
	}
	if !foundEnemy {
		return State{}, zero, fmt.Errorf("NPC enemy relation absent")
	}
	// State.Validate checks reciprocal identity edges.  For a live combat
	// attacker that is also a follower, require the leader to be online in the
	// same room; otherwise the source first_fol context is not authoritative
	// enough to continue a death transaction.
	if npc.FollowingPlayerID != "" {
		leader, exists := s.Players[npc.FollowingPlayerID]
		if !exists || !leader.Online || leader.Body.RoomID != source.Resource.ID || !containsString(leader.NPCFollowerIDs, npcID) {
			return State{}, zero, fmt.Errorf("NPC follower context unresolved")
		}
	}
	if s.War == nil {
		return State{}, zero, fmt.Errorf("family-war state unresolved")
	}
	respawn, ok := s.Rooms[1008]
	if !ok || source.Items == nil || respawn.Items == nil || len(source.Resource.Objects) != 0 || len(respawn.Resource.Objects) != 0 {
		return State{}, zero, fmt.Errorf("canonical death/respawn floor required")
	}
	if victim.Items == nil {
		return State{}, zero, fmt.Errorf("canonical player equipment required")
	}

	survival := flag(source.Resource.Flags[:], 36)
	rules := DeathCharacterRules{
		AttackerPlayer: false,
		Self:           false,
		Survival:       survival,
		WarAllowsLoss:  s.War.AllowsDeathLoss(npc.Body.Daily[9].Max, victim.Body.Daily[9].Max),
	}
	// PlanDeathTimers is still the source ordering boundary.  For an NPC
	// attacker it clears the victim timer and returns RemoveEnemy=true without
	// consuming RNG or changing the NPC's timer.
	timers, err := PlanDeathTimers(victim.Body.Timers[11], npc.Body.Timers[11], false, false, survival, now, roll)
	if err != nil {
		return State{}, zero, err
	}
	character, err := PlanDeathCharacter(victim.Body, *victim.Items, rules)
	if err != nil {
		return State{}, zero, err
	}
	floor, err := TransferItemRoots(character.Dropped, *source.Items, character.Dropped.Inventory)
	if err != nil {
		return State{}, zero, err
	}

	// From this point onward all changes are made to a deep clone.  Any late
	// room-entry, allocator, or final-validation error returns the zero State
	// and zero result, never a partially-dead candidate.
	next := s.clone()
	victim = next.Players[victimID]
	victim.Body = character.Body
	victim.Body.Timers[11] = timers.Victim
	victim.Items = &character.Kept
	next.Players[victimID] = victim

	attacker := next.NPCs[npcID]
	keptEnemies := make([]NPCEnemy, 0, len(attacker.Enemies))
	removedEnemy := false
	for _, relation := range attacker.Enemies {
		if relation.Target == enemy {
			removedEnemy = true
			continue
		}
		keptEnemies = append(keptEnemies, relation)
	}
	if !removedEnemy || !timers.RemoveEnemy {
		return State{}, zero, fmt.Errorf("NPC enemy removal candidate invalid")
	}
	attacker.Enemies = keptEnemies
	next.NPCs[npcID] = attacker

	war, defeated := next.War.AfterPlayerDeath(victim.Body)
	next.War = &war
	views, err := next.RoomPlayers(source.Resource.ID)
	if err != nil {
		return State{}, zero, err
	}
	departure, err := PlanRoomDeparture(views, victimID)
	if err != nil {
		return State{}, zero, err
	}
	source = next.Rooms[source.Resource.ID]
	source.Items = &floor.Destination
	source.PlayerIDs = nil
	for _, player := range departure.Players {
		source.PlayerIDs = append(source.PlayerIDs, player.ID)
	}
	next.Rooms[source.Resource.ID] = source
	if departure.DeactivateMonsters {
		next.deactivateRoomNPCs(source.Resource.ID)
	}

	// Derive destination occupants from the candidate between departure and
	// entry, matching PlanPlayerDeath's canonical admission order.
	respawn = next.Rooms[1008]
	occupants := make([]RoomPlayerView, 0, len(respawn.PlayerIDs))
	for _, id := range respawn.PlayerIDs {
		p, exists := next.Players[id]
		if !exists {
			return State{}, zero, fmt.Errorf("respawn occupant absent")
		}
		occupants = append(occupants, RoomPlayerView{ID: id, Name: p.Body.Name, Description: p.Body.Description, Flags: p.Body.Flags})
	}
	entrant := RoomPlayerView{ID: victimID, Name: victim.Body.Name, Description: victim.Body.Description, Flags: victim.Body.Flags}
	var room RoomState
	var entry RoomEntry
	var npcDelta *npcEntryDelta
	if next.NPCs != nil {
		room, entry, npcDelta, err = planCanonicalNPCEntry(respawn, next.NPCs, occupants, entrant, view, catalog, now, roll, allocate)
	} else {
		// This branch is unreachable after canonical attacker validation, but
		// keeping it explicit avoids silently inventing a legacy NPC adapter.
		return State{}, zero, fmt.Errorf("canonical NPC respawn state required")
	}
	if err != nil {
		return State{}, zero, err
	}
	if err := next.installNPCSpawns(npcDelta); err != nil {
		return State{}, zero, err
	}
	next.Rooms[1008] = room
	next.activateEntryNPCs(1008, len(occupants) == 0, npcDelta)
	victim = next.Players[victimID]
	victim.Body.RoomID = 1008
	next.Players[victimID] = victim
	if err := next.Validate(); err != nil {
		return State{}, zero, err
	}
	entry.Scene, err = next.CurrentScene(victimID, view.Hour)
	if err != nil {
		return State{}, zero, err
	}
	return next, NPCPlayerDeathResult{
		PlayerDeathResult: PlayerDeathResult{
			Entry:                    entry,
			BroadcastDeath:           !survival,
			FamilyDefeated:           defeated,
			DeactivateSourceMonsters: departure.DeactivateMonsters,
		},
		NPCID: npcID, VictimID: victimID,
	}, nil
}
