package world

import "sort"

// RecoverOffline prepares a cold-start snapshot, not a live logout command.
// The process must hold exclusive world ownership, admit no sessions/ticks, and
// durably commit this candidate before admitting players. It must not run on a
// reconnect or ordinary read. engine.StartWorld owns fencing/commit ordering.
// Creature bodies/room data are preserved; occupancy and resolved NPC targets
// of players are discarded. MDMFOL is F_CLR with FollowingPlayerID so a crash
// restart cannot reintroduce *따르기. NPC follow edges require resolved
// Enemies/ActiveNPCIDs; other nil lists stay unknown. This cold-start rule is
// NOT clear_enm_crt's live logout predicate (active NPCs and nonnegative damage
// in the C runtime). Corruption is rejected, never hidden by clearing
// inconsistent membership.
func (s State) RecoverOffline() (State, []string, error) {
	if err := s.Validate(); err != nil {
		return State{}, nil, err
	}
	next := s.clone()
	for id, player := range next.Players {
		if err := next.requireResolvedNPCFollowers(id, player); err != nil {
			return State{}, nil, err
		}
	}
	if next.ActiveNPCIDs != nil {
		next.ActiveNPCIDs = []string{}
	}
	var disconnected []string
	for id, player := range next.Players {
		if player.Online {
			disconnected = append(disconnected, id)
		}
		player.Online = false
		player.FollowingID = ""
		player.FollowerIDs = nil
		player.NPCFollowerIDs = nil
		player.FollowerRefs = nil
		next.Players[id] = player
	}
	for id, room := range next.Rooms {
		room.PlayerIDs = nil
		next.Rooms[id] = room
	}
	for id, npc := range next.NPCs {
		npc = clearNPCFollowLock(npc)
		if npc.Enemies == nil {
			next.NPCs[id] = npc
			continue
		} // unknown remains unknown
		kept := make([]NPCEnemy, 0, len(npc.Enemies))
		for _, enemy := range npc.Enemies {
			if enemy.Target.Kind != "player" {
				kept = append(kept, enemy)
			}
		}
		npc.Enemies = kept
		next.NPCs[id] = npc
	}
	sort.Strings(disconnected)
	return next, disconnected, nil
}
