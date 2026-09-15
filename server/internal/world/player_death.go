package world

import "fmt"

type PlayerDeathResult struct {
	Entry                                                    RoomEntry
	BroadcastDeath, FamilyDefeated, DeactivateSourceMonsters bool
	Events                                                   []FamilyWarEvent
}

// PlanPlayerDeath handles local PLAYER attackers, including self. NPC attackers
// require canonical NPC/enemy identities and are not impersonated as players.
// Caller resolves the lethal event and commits state + result atomically before
// broadcasting or accepting another command. An optional FamilyCatalog binds
// creature.c:die's two broadcast_all strings when a war boss dies.
func (s State) PlanPlayerDeath(victimID, attackerID string, now int32, view SceneOptions, catalog SpawnCatalog, roll func(int, int) int, allocate func() (string, error), families ...FamilyCatalog) (State, PlayerDeathResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, PlayerDeathResult{}, err
	}
	victim, vok := s.Players[victimID]
	attacker, aok := s.Players[attackerID]
	if !vok || !aok || !victim.Online || !attacker.Online || victim.Body.RoomID != attacker.Body.RoomID || victim.Items == nil || s.War == nil {
		return State{}, PlayerDeathResult{}, fmt.Errorf("incomplete player death context")
	}
	source := s.Rooms[victim.Body.RoomID]
	respawn, ok := s.Rooms[1008]
	if !ok || source.Items == nil || respawn.Items == nil {
		return State{}, PlayerDeathResult{}, fmt.Errorf("canonical death/respawn floor required")
	}
	survival := flag(source.Resource.Flags[:], 36)
	rules := DeathCharacterRules{AttackerPlayer: true, Self: victimID == attackerID, Survival: survival, WarAllowsLoss: s.War.AllowsDeathLoss(attacker.Body.Daily[9].Max, victim.Body.Daily[9].Max)}
	// Original RNG order: killer timer before remaining death/entry rules.
	timers, err := PlanDeathTimers(victim.Body.Timers[11], attacker.Body.Timers[11], true, rules.Self, survival, now, roll)
	if err != nil {
		return State{}, PlayerDeathResult{}, err
	}
	character, err := PlanDeathCharacter(victim.Body, *victim.Items, rules)
	if err != nil {
		return State{}, PlayerDeathResult{}, err
	}
	floor, err := TransferItemRoots(character.Dropped, *source.Items, character.Dropped.Inventory)
	if err != nil {
		return State{}, PlayerDeathResult{}, err
	}
	next := s.clone()
	victim = next.Players[victimID]
	victim.Body = character.Body
	victim.Body.Timers[11] = timers.Victim
	victim.Items = &character.Kept
	next.Players[victimID] = victim
	attacker = next.Players[attackerID]
	attacker.Body.Timers[11] = timers.Attacker
	if timers.RemoveEnemy {
		var enemies []string
		for _, id := range attacker.PlayerEnemies {
			if id != victimID {
				enemies = append(enemies, id)
			}
		}
		attacker.PlayerEnemies = enemies
	}
	next.Players[attackerID] = attacker
	war, defeated := next.War.AfterPlayerDeath(victim.Body)
	events, err := familyDefeatEvents(defeated, victim.Body.Daily[9].Max, families...)
	if err != nil {
		return State{}, PlayerDeathResult{}, err
	}
	next.War = &war
	views, err := next.RoomPlayers(source.Resource.ID)
	if err != nil {
		return State{}, PlayerDeathResult{}, err
	}
	departure, err := PlanRoomDeparture(views, victimID)
	if err != nil {
		return State{}, PlayerDeathResult{}, err
	}
	source = next.Rooms[source.Resource.ID]
	source.Items = &floor.Destination
	source.PlayerIDs = nil
	for _, p := range departure.Players {
		source.PlayerIDs = append(source.PlayerIDs, p.ID)
	}
	next.Rooms[source.Resource.ID] = source
	if departure.DeactivateMonsters {
		next.deactivateRoomNPCs(source.Resource.ID)
	}
	// Derive destination occupants directly while the candidate is between
	// departure/arrival; Validate is intentionally postponed until both finish.
	respawn = next.Rooms[1008]
	var occupants []RoomPlayerView
	for _, id := range respawn.PlayerIDs {
		p := next.Players[id].Body
		occupants = append(occupants, RoomPlayerView{ID: id, Name: p.Name, Description: p.Description, Flags: p.Flags})
	}
	entrant := RoomPlayerView{ID: victimID, Name: victim.Body.Name, Description: victim.Body.Description, Flags: victim.Body.Flags}
	var room RoomState
	var entry RoomEntry
	var npcDelta *npcEntryDelta
	if next.NPCs != nil {
		room, entry, npcDelta, err = planCanonicalNPCEntry(respawn, next.NPCs, occupants, entrant, view, catalog, now, roll, allocate)
	} else {
		room, entry, err = PlanCanonicalRoomEntry(respawn, occupants, entrant, view, catalog, now, roll, allocate)
	}
	if err != nil {
		return State{}, PlayerDeathResult{}, err
	}
	if err := next.installNPCSpawns(npcDelta); err != nil {
		return State{}, PlayerDeathResult{}, err
	}
	next.Rooms[1008] = room
	next.activateEntryNPCs(1008, len(occupants) == 0, npcDelta)
	victim = next.Players[victimID]
	victim.Body.RoomID = 1008
	next.Players[victimID] = victim
	if err := next.Validate(); err != nil {
		return State{}, PlayerDeathResult{}, err
	}
	// Dropped equipment and cleared effects must no longer affect respawn sight.
	entry.Scene, err = next.CurrentScene(victimID, view.Hour)
	if err != nil {
		return State{}, PlayerDeathResult{}, err
	}
	return next, PlayerDeathResult{Entry: entry, BroadcastDeath: !survival, FamilyDefeated: defeated, DeactivateSourceMonsters: departure.DeactivateMonsters, Events: events}, nil
}
