package world

// Helpers operate only on an isolated candidate. Unknown legacy active lists
// remain unknown; membership alone cannot recover their historical tick order.
func (s *State) deactivateRoomNPCs(roomID int16) {
	if s.ActiveNPCIDs == nil {
		return
	}
	remove := map[string]bool{}
	for _, id := range s.Rooms[roomID].NPCIDs {
		remove[id] = true
	}
	kept := make([]string, 0, len(s.ActiveNPCIDs))
	for _, id := range s.ActiveNPCIDs {
		if !remove[id] {
			kept = append(kept, id)
		}
	}
	s.ActiveNPCIDs = kept
}

func (s *State) activateEntryNPCs(roomID int16, firstOccupant bool, delta *npcEntryDelta) {
	if s.ActiveNPCIDs == nil {
		return
	}
	var ids []string
	if firstOccupant {
		ids = s.Rooms[roomID].NPCIDs
	} else if delta != nil {
		ids = delta.SpawnOrder
	}
	for _, id := range ids {
		// add_active removes then prepends, even for an already active NPC.
		ordered := make([]string, 1, len(s.ActiveNPCIDs)+1)
		ordered[0] = id
		for _, old := range s.ActiveNPCIDs {
			if old != id {
				ordered = append(ordered, old)
			}
		}
		s.ActiveNPCIDs = ordered
	}
}
