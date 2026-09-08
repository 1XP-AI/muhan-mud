package world

import "fmt"

var trapSpellTimers = [...]int{1, 2, 23, 29, 30, 31, 25, 13, 17, 0, 27, 18}

// ApplyArrivalTrap applies the already planned, destination-bound trap effect
// to one authoritative snapshot.  It is intentionally separate from planning
// so a failed receipt never exposes a partially moved or partially damaged
// player.  Alarm NPC relocation is applied by ApplyArrivalAlarm immediately
// after this actor-owned effect in the DirectionalStep transaction.
func (s State) ApplyArrivalTrap(actorID string, effect ArrivalTrapResult) (State, error) {
	if err := s.Validate(); err != nil {
		return State{}, err
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online {
		return State{}, fmt.Errorf("missing online trap actor")
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok || room.Resource.Trap != effect.Trap {
		return State{}, fmt.Errorf("arrival trap does not match actor room")
	}
	next := s.clone()
	actor = next.Players[actorID]
	if effect.HP < -32768 || effect.HP > 32767 || effect.MP < -32768 || effect.MP > 32767 {
		return State{}, fmt.Errorf("arrival trap vitals outside legacy range")
	}
	actor.Body.HPCurrent = int16(effect.HP)
	actor.Body.MPCurrent = int16(effect.MP)
	if effect.PreparationCleared {
		actor.Body.Flags[24/8] &^= 1 << (24 % 8)
	}
	if effect.Poisoned {
		actor.Body.Flags[16/8] |= 1 << (16 % 8)
	}
	if effect.ClearSpells {
		for _, index := range trapSpellTimers {
			actor.Body.Timers[index].Interval = 0
		}
	}
	if effect.LoseItems {
		actor.Body.Inventory = nil
		if actor.Items != nil {
			empty := ItemCollection{Items: map[string]Item{}}
			actor.Items = &empty
		}
	}
	next.Players[actorID] = actor
	if effect.Relocate {
		if effect.Trap != TrapPit || !effect.Triggered {
			return State{}, fmt.Errorf("unsupported arrival relocation")
		}
		if effect.RelocateTo == actor.Body.RoomID {
			return State{}, fmt.Errorf("arrival trap relocation did not change room")
		}
		destination, ok := next.Rooms[effect.RelocateTo]
		if !ok || destination.Resource.BeenHere == 2147483647 {
			return State{}, fmt.Errorf("arrival trap destination unavailable")
		}
		current := next.Rooms[actor.Body.RoomID]
		ids := make([]string, 0, len(current.PlayerIDs))
		removed := false
		for _, id := range current.PlayerIDs {
			if id == actorID {
				removed = true
				continue
			}
			ids = append(ids, id)
		}
		if !removed {
			return State{}, fmt.Errorf("arrival trap actor absent from room")
		}
		current.PlayerIDs = ids
		next.Rooms[current.Resource.ID] = current
		destination.Resource.BeenHere++
		insertAt := len(destination.PlayerIDs)
		for i, id := range destination.PlayerIDs {
			p, exists := next.Players[id]
			if !exists {
				return State{}, fmt.Errorf("arrival trap room has unknown player")
			}
			if p.Body.Name > actor.Body.Name {
				insertAt = i
				break
			}
		}
		destination.PlayerIDs = append(destination.PlayerIDs, "")
		copy(destination.PlayerIDs[insertAt+1:], destination.PlayerIDs[insertAt:])
		destination.PlayerIDs[insertAt] = actorID
		next.Rooms[destination.Resource.ID] = destination
		actor = next.Players[actorID]
		actor.Body.RoomID = destination.Resource.ID
		next.Players[actorID] = actor
	}
	if err := next.Validate(); err != nil {
		return State{}, err
	}
	return next, nil
}

// ArrivalAlarmResult records the canonical NPC identities moved by C's
// TRAP_ALARM handler. It is an event projection; NPC state and timer updates
// are committed in the returned State.
type ArrivalAlarmResult struct {
	SourceRoomID      int16
	DestinationRoomID int16
	MovedNPCIDs       []string
	SourceHadPlayers  bool
}

// ApplyArrivalAlarmWithCatalog performs the explicit add_permcrt_rom phase
// before applying the existing alarm relocation. A nil catalog is still
// sufficient when every due slot already has a canonical identity; a missing
// due instance fails closed instead of being silently omitted.
func (s State) ApplyArrivalAlarmWithCatalog(actorID string, effect ArrivalTrapResult, now int32, catalog SpawnCatalog, roll func(int, int) int, allocate func() (string, error)) (State, ArrivalAlarmResult, error) {
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online {
		return State{}, ArrivalAlarmResult{}, fmt.Errorf("missing online alarm actor")
	}
	destination, ok := s.Rooms[actor.Body.RoomID]
	if !effect.Alarm || !effect.Triggered || effect.Trap != TrapAlarm || !ok || destination.Resource.Trap != TrapAlarm {
		return State{}, ArrivalAlarmResult{}, fmt.Errorf("alarm actor is not in alarm room")
	}
	next, err := s.spawnDueAlarmNPCs(destination.Resource.TrapExit, now, catalog, roll, allocate)
	if err != nil {
		return State{}, ArrivalAlarmResult{}, err
	}
	return next.ApplyArrivalAlarm(actorID, effect, now)
}

// ApplyArrivalAlarm ports the permanent-NPC portion of room.c:check_traps for
// a snapshot whose due catalog instances have already been materialized. Use
// ApplyArrivalAlarmWithCatalog at a command boundary when due slots may need
// allocation; this method remains the identity-only fail-closed reducer.
func (s State) ApplyArrivalAlarm(actorID string, effect ArrivalTrapResult, now int32) (State, ArrivalAlarmResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, ArrivalAlarmResult{}, err
	}
	if !effect.Alarm || !effect.Triggered || effect.Trap != TrapAlarm {
		return State{}, ArrivalAlarmResult{}, fmt.Errorf("invalid arrival alarm effect")
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online {
		return State{}, ArrivalAlarmResult{}, fmt.Errorf("missing online alarm actor")
	}
	destinationID := actor.Body.RoomID
	destination, ok := s.Rooms[destinationID]
	if !ok || destination.Resource.Trap != TrapAlarm {
		return State{}, ArrivalAlarmResult{}, fmt.Errorf("alarm actor is not in alarm room")
	}
	sourceID := destination.Resource.TrapExit
	if sourceID == destinationID {
		return State{}, ArrivalAlarmResult{}, fmt.Errorf("alarm source equals destination")
	}
	source, ok := s.Rooms[sourceID]
	if !ok {
		return State{}, ArrivalAlarmResult{}, fmt.Errorf("alarm source room absent")
	}
	if s.NPCs == nil {
		return State{}, ArrivalAlarmResult{}, fmt.Errorf("alarm requires canonical NPC state")
	}
	if !containsString(destination.PlayerIDs, actorID) {
		return State{}, ArrivalAlarmResult{}, fmt.Errorf("alarm actor membership absent")
	}

	// Resolve all loaded permanent NPCs by canonical origin identity. Names are
	// intentionally not used: two permanent slots may share a display name.
	type alarmMove struct {
		id     string
		origin NPCPermanentOrigin
	}
	moves := make([]alarmMove, 0)
	origins := map[NPCPermanentOrigin]bool{}
	for _, id := range source.NPCIDs {
		npc, exists := s.NPCs[id]
		if id == "" || !exists || npc.Body.RoomID != sourceID || npc.Body.Type != 1 {
			return State{}, ArrivalAlarmResult{}, fmt.Errorf("invalid alarm source NPC identity")
		}
		if !flag(npc.Body.Flags[:], npcPermanentFlag) {
			continue
		}
		if npc.PermanentOrigin == nil || npc.PermanentOrigin.RoomID != sourceID || int(npc.PermanentOrigin.Slot) >= len(source.Resource.PermanentMonsters) {
			return State{}, ArrivalAlarmResult{}, fmt.Errorf("permanent alarm NPC %q has unknown origin", id)
		}
		origin := *npc.PermanentOrigin
		if origins[origin] || source.Resource.PermanentMonsters[origin.Slot].Misc == 0 {
			return State{}, ArrivalAlarmResult{}, fmt.Errorf("invalid permanent alarm origin")
		}
		origins[origin] = true
		moves = append(moves, alarmMove{id: id, origin: origin})
	}
	// add_permcrt_rom would materialize every due slot. Do not silently omit a
	// missing guard; the caller must first run the catalog/allocator phase.
	for slot, timer := range source.Resource.PermanentMonsters {
		if timer.Misc == 0 || int64(timer.LastTime)+int64(timer.Interval) > int64(now) {
			continue
		}
		if !origins[NPCPermanentOrigin{RoomID: sourceID, Slot: uint8(slot)}] {
			return State{}, ArrivalAlarmResult{}, fmt.Errorf("alarm requires due permanent NPC spawn")
		}
	}
	if len(moves) != 0 && s.ActiveNPCIDs == nil {
		return State{}, ArrivalAlarmResult{}, fmt.Errorf("alarm active NPC order unresolved")
	}

	next := s.clone()
	result := ArrivalAlarmResult{SourceRoomID: sourceID, DestinationRoomID: destinationID, SourceHadPlayers: len(source.PlayerIDs) != 0}
	for _, move := range moves {
		npc := next.NPCs[move.id]
		timer := next.Rooms[sourceID].Resource.PermanentMonsters[move.origin.Slot]
		if int64(timer.LastTime)+int64(timer.Interval) <= int64(now) {
			timer.LastTime = now
			nextSource := next.Rooms[sourceID]
			nextSource.Resource.PermanentMonsters[move.origin.Slot] = timer
			next.Rooms[sourceID] = nextSource
		}
		npc.Body.Flags[npcPermanentFlag/8] &^= 1 << (npcPermanentFlag % 8)
		npc.Body.Flags[npcAggressiveFlag/8] |= 1 << (npcAggressiveFlag % 8)
		npc.Body.RoomID = destinationID
		next.NPCs[move.id] = npc

		sourceRoom := next.Rooms[sourceID]
		sourceRoom.NPCIDs, _ = removeString(sourceRoom.NPCIDs, move.id)
		next.Rooms[sourceID] = sourceRoom
		destinationRoom := next.Rooms[destinationID]
		at := len(destinationRoom.NPCIDs)
		for i, existingID := range destinationRoom.NPCIDs {
			existing, exists := next.NPCs[existingID]
			if !exists {
				return State{}, ArrivalAlarmResult{}, fmt.Errorf("alarm destination NPC identity absent")
			}
			if existing.Body.Name > npc.Body.Name {
				at = i
				break
			}
		}
		destinationRoom.NPCIDs = append(destinationRoom.NPCIDs, "")
		copy(destinationRoom.NPCIDs[at+1:], destinationRoom.NPCIDs[at:])
		destinationRoom.NPCIDs[at] = move.id
		next.Rooms[destinationID] = destinationRoom
		if len(source.PlayerIDs) == 0 {
			next.prependActiveNPC(move.id)
		}
		result.MovedNPCIDs = append(result.MovedNPCIDs, move.id)
	}
	if err := next.Validate(); err != nil {
		return State{}, ArrivalAlarmResult{}, err
	}
	return next, result, nil
}
