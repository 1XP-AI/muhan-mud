package world

import "fmt"

type alarmSpawnSlot struct {
	templateID int16
	slot       uint8
}

// spawnDueAlarmNPCs is the catalog/allocator phase that C's
// add_permcrt_rom performs immediately before the TRAP_ALARM relocation. It
// only creates missing permanent monster instances; the caller still applies
// the alarm movement and timer writes as one later candidate.
func (s State) spawnDueAlarmNPCs(roomID int16, now int32, catalog SpawnCatalog, roll func(int, int) int, allocate func() (string, error)) (State, error) {
	if err := s.Validate(); err != nil {
		return State{}, err
	}
	if s.NPCs == nil {
		return State{}, fmt.Errorf("alarm spawn requires canonical NPC state")
	}
	room, ok := s.Rooms[roomID]
	if !ok {
		return State{}, fmt.Errorf("alarm spawn room absent")
	}
	permanentCounts := PermanentMonsterCounts(func() []LegacyMonster {
		monsters := make([]LegacyMonster, 0, len(room.NPCIDs))
		for _, id := range room.NPCIDs {
			npc, exists := s.NPCs[id]
			if !exists {
				continue
			}
			monsters = append(monsters, npc.Body)
		}
		return monsters
	}())
	occupied := map[NPCPermanentOrigin]bool{}
	for _, id := range room.NPCIDs {
		npc, exists := s.NPCs[id]
		if !exists || npc.PermanentOrigin == nil {
			continue
		}
		origin := *npc.PermanentOrigin
		if origin.RoomID == roomID && int(origin.Slot) < len(room.Resource.PermanentMonsters) {
			occupied[origin] = true
		}
	}

	var checked [10]bool
	var slots []alarmSpawnSlot
	templateNames := map[int16]string{}
	for i, timer := range room.Resource.PermanentMonsters {
		if checked[i] || timer.Misc == 0 || int64(timer.LastTime)+int64(timer.Interval) > int64(now) {
			continue
		}
		group := []int{i}
		for j := i + 1; j < len(room.Resource.PermanentMonsters); j++ {
			other := room.Resource.PermanentMonsters[j]
			if other.Misc == timer.Misc && int64(other.LastTime)+int64(other.Interval) < int64(now) {
				group = append(group, j)
				checked[j] = true
			}
		}
		name, exists := templateNames[timer.Misc]
		if !exists {
			// A canonical permanent instance already occupying this slot gives
			// us the exact template name without requiring a catalog just to
			// prove that no spawn is needed.
			for _, id := range room.NPCIDs {
				npc, ok := s.NPCs[id]
				if !ok || npc.PermanentOrigin == nil || npc.PermanentOrigin.RoomID != roomID || int(npc.PermanentOrigin.Slot) != i || !flag(npc.Body.Flags[:], npcPermanentFlag) {
					continue
				}
				name = npc.Body.Name
				break
			}
			if name == "" {
				if catalog == nil {
					return State{}, fmt.Errorf("alarm spawn requires catalog")
				}
				template, err := catalog.Monster(timer.Misc)
				if err != nil {
					return State{}, err
				}
				if template.Type != 1 || template.Name == "" {
					return State{}, fmt.Errorf("invalid alarm monster template %d", timer.Misc)
				}
				name = template.Name
			}
			templateNames[timer.Misc] = name
		}
		missing := len(group) - permanentCounts[name]
		if missing <= 0 {
			continue
		}
		for _, index := range group {
			origin := NPCPermanentOrigin{RoomID: roomID, Slot: uint8(index)}
			if occupied[origin] {
				continue
			}
			slots = append(slots, alarmSpawnSlot{templateID: timer.Misc, slot: uint8(index)})
			occupied[origin] = true
			permanentCounts[name]++
			missing--
			if missing == 0 {
				break
			}
		}
		if missing != 0 {
			return State{}, fmt.Errorf("alarm spawn slots unavailable for template %d", timer.Misc)
		}
	}
	if len(slots) == 0 {
		return s, nil
	}
	if allocate == nil {
		return State{}, fmt.Errorf("alarm spawn requires ID allocator")
	}
	next := s.clone()
	nextRoom := next.Rooms[roomID]
	for _, request := range slots {
		template, err := catalog.Monster(request.templateID)
		if err != nil {
			return State{}, err
		}
		body, err := SpawnPermanentMonster(template, now, catalog.Object, roll)
		if err != nil {
			return State{}, err
		}
		id, err := allocate()
		if err != nil || id == "" {
			if err == nil {
				err = fmt.Errorf("empty alarm NPC ID")
			}
			return State{}, err
		}
		if _, exists := next.NPCs[id]; exists {
			return State{}, fmt.Errorf("duplicate alarm NPC ID %q", id)
		}
		body.RoomID = roomID
		origin := &NPCPermanentOrigin{RoomID: roomID, Slot: request.slot}
		next.NPCs[id] = NPCState{Body: body, Enemies: []NPCEnemy{}, PermanentOrigin: origin}
		at := len(nextRoom.NPCIDs)
		for i, existingID := range nextRoom.NPCIDs {
			existing, exists := next.NPCs[existingID]
			if !exists {
				return State{}, fmt.Errorf("alarm source NPC identity absent")
			}
			if existing.Body.Name > body.Name {
				at = i
				break
			}
		}
		nextRoom.NPCIDs = append(nextRoom.NPCIDs, "")
		copy(nextRoom.NPCIDs[at+1:], nextRoom.NPCIDs[at:])
		nextRoom.NPCIDs[at] = id
	}
	next.Rooms[roomID] = nextRoom
	if err := next.Validate(); err != nil {
		return State{}, err
	}
	return next, nil
}
