package world

import "fmt"

// LeavePlayer applies occupancy and resolved active-NPC enemy cleanup, not
// legacy quit's full command/effect handling. Caller verifies generation and commits
// before releasing ownership; a stale socket must never disconnect a new login.
// Retries use the executor receipt, not another reduction of this candidate.
func (s State) LeavePlayer(actorID string) (State, RoomDeparture, error) {
	if err := s.Validate(); err != nil {
		return State{}, RoomDeparture{}, err
	}
	p, ok := s.Players[actorID]
	if !ok || !p.Online {
		return State{}, RoomDeparture{}, fmt.Errorf("online departure actor absent")
	}
	views, err := s.RoomPlayers(p.Body.RoomID)
	if err != nil {
		return State{}, RoomDeparture{}, err
	}
	departure, err := PlanRoomDeparture(views, actorID)
	if err != nil {
		return State{}, RoomDeparture{}, err
	}
	next := s.clone()
	p = next.Players[actorID]
	p.Online = false
	next.Players[actorID] = p
	room := next.Rooms[p.Body.RoomID]
	room.PlayerIDs = nil
	for _, other := range departure.Players {
		room.PlayerIDs = append(room.PlayerIDs, other.ID)
	}
	next.Rooms[p.Body.RoomID] = room
	next, err = next.DetachPlayerRelationships(actorID)
	if err != nil {
		return State{}, RoomDeparture{}, err
	}
	if departure.DeactivateMonsters {
		next.deactivateRoomNPCs(p.Body.RoomID)
	}
	if err := next.clearLogoutNPCEnemies(actorID); err != nil {
		return State{}, RoomDeparture{}, err
	}
	if err := next.Validate(); err != nil {
		return State{}, RoomDeparture{}, err
	}
	return next, departure, nil
}

// uninit_ply calls clear_enm_crt AFTER del_ply_rom removes last-room NPCs from
// first_active. Only nonnegative damage is removed; negative membership stays.
// Nil active state keeps the pre-migration occupancy-only path, not a claim
// that full logout cleanup ran. Known active NPCs require resolved enemy lists.
func (s *State) clearLogoutNPCEnemies(actorID string) error {
	for _, id := range s.ActiveNPCIDs {
		npc := s.NPCs[id]
		if npc.Enemies == nil {
			return fmt.Errorf("logout requires resolved active NPC enemies")
		}
		kept := make([]NPCEnemy, 0, len(npc.Enemies))
		for _, enemy := range npc.Enemies {
			if enemy.Target == (EntityRef{Kind: "player", ID: actorID}) && enemy.Damage >= 0 {
				continue
			}
			kept = append(kept, enemy)
		}
		npc.Enemies = kept
		s.NPCs[id] = npc
	}
	return nil
}
