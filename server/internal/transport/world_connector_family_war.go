package transport

import (
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

// publishFamilyWar delivers call_war's global announcement after the first
// commit. broadcast_all ignores PNOBRD; broadcast honors it. The actor already
// received the same text as the command response.
func (g *WorldConnector) publishFamilyWar(after world.State, actorID string, events []world.FamilyWarEvent) {
	if len(events) == 0 {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, event := range events {
		if event.Text == "" {
			continue
		}
		for connection := range g.connections {
			if connection.lease.ActorID == actorID || connection.events == nil {
				continue
			}
			player, ok := after.Players[connection.lease.ActorID]
			if !ok || !player.Online {
				continue
			}
			if !event.AllPlayers && world.PlayerFlagSet(player.Body, world.FamilyMutationNoBroadcastFlag) {
				continue
			}
			select {
			case connection.events <- event.Text:
			default:
			}
		}
	}
}

// publishFamilyDefeat delivers creature.c:die's two broadcast_all lines after
// the first death commit. Recipients include the dead boss; PNOBRD is ignored.
func (g *WorldConnector) publishFamilyDefeat(after world.State, events []world.FamilyWarEvent) {
	g.publishFamilyWar(after, "", events)
}
