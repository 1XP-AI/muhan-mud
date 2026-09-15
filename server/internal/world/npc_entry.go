package world

import "fmt"

type npcEntryDelta struct {
	IDs     []string
	Spawned map[string]NPCState
	// Permanent-slot creation order, distinct from alphabetic room membership.
	// Occupied-room activation must use this order, never Go map iteration.
	SpawnOrder []string
}

// Caller owns an isolated candidate; errors must discard that whole candidate.
func (s *State) installNPCSpawns(delta *npcEntryDelta) error {
	if delta == nil {
		return nil
	}
	if s.NPCs == nil {
		return fmt.Errorf("NPC delta requires canonical state")
	}
	for id := range delta.Spawned {
		if _, exists := s.NPCs[id]; exists {
			return fmt.Errorf("duplicate spawned NPC identity")
		}
	}
	for id, npc := range delta.Spawned {
		npc.Body.Inventory = cloneObjects(npc.Body.Inventory)
		if npc.Items != nil {
			items := npc.Items.clone()
			npc.Items = &items
		}
		npc.Enemies = append([]NPCEnemy{}, npc.Enemies...)
		if npc.PermanentOrigin != nil {
			origin := *npc.PermanentOrigin
			npc.PermanentOrigin = &origin
		}
		s.NPCs[id] = npc
	}
	return nil
}

// Returns an isolated identity delta alongside the existing entry candidate.
// No existing NPC identity is discovered through name/body matching.
func planCanonicalNPCEntry(room RoomState, npcs map[string]NPCState, players []RoomPlayerView, entrant RoomPlayerView, view SceneOptions, catalog SpawnCatalog, now int32, roll func(int, int) int, allocate func() (string, error)) (RoomState, RoomEntry, *npcEntryDelta, error) {
	if npcs == nil || len(room.Resource.Monsters) != 0 {
		return RoomState{}, RoomEntry{}, nil, fmt.Errorf("canonical NPC room required")
	}
	delta := &npcEntryDelta{IDs: append([]string(nil), room.NPCIDs...), Spawned: map[string]NPCState{}}
	room.Resource = cloneRoom(room.Resource)
	for _, id := range room.NPCIDs {
		npc, ok := npcs[id]
		if !ok || npc.Body.RoomID != room.Resource.ID {
			return RoomState{}, RoomEntry{}, nil, fmt.Errorf("invalid NPC entry projection")
		}
		body := npc.Body
		if npc.Items != nil {
			inventory, err := npc.Items.LegacyInventory()
			if err != nil {
				return RoomState{}, RoomEntry{}, nil, err
			}
			body.Inventory = inventory
		} else {
			body.Inventory = cloneObjects(body.Inventory)
		}
		room.Resource.Monsters = append(room.Resource.Monsters, body)
	}
	onNPC := func(at int, body LegacyMonster) error {
		if allocate == nil {
			return fmt.Errorf("NPC spawn requires ID allocator")
		}
		id, err := allocate()
		if err != nil {
			return err
		}
		_, old := npcs[id]
		_, fresh := delta.Spawned[id]
		if id == "" || old || fresh || body.Type != 1 || at < 0 || at > len(delta.IDs) {
			return fmt.Errorf("invalid or duplicate NPC spawn identity")
		}
		body.Inventory = cloneObjects(body.Inventory)
		delta.Spawned[id] = NPCState{Body: body, Enemies: []NPCEnemy{}}
		delta.SpawnOrder = append(delta.SpawnOrder, id)
		delta.IDs = append(delta.IDs, "")
		copy(delta.IDs[at+1:], delta.IDs[at:])
		delta.IDs[at] = id
		return nil
	}
	var next RoomState
	var entry RoomEntry
	var err error
	if room.Items != nil {
		next, entry, err = planCanonicalRoomEntryWithNPCEvents(room, players, entrant, view, catalog, now, roll, allocate, onNPC)
	} else {
		entry, err = planRoomEntry(room.Resource, players, entrant, view, func(resource LegacyRoom) (LegacyRoom, error) {
			return refreshRoomResourcesWithNPCEvents(resource, catalog, now, roll, nil, onNPC)
		})
		next = room
		next.Resource = entry.Room
		next.PlayerIDs = nil
		for _, p := range entry.Players {
			next.PlayerIDs = append(next.PlayerIDs, p.ID)
		}
	}
	if err != nil {
		return RoomState{}, RoomEntry{}, nil, err
	}
	next.NPCIDs = append([]string(nil), delta.IDs...)
	next.Resource.Monsters = nil
	return next, entry, delta, nil
}
