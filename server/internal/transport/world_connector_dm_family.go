package transport

import "github.com/1XP-Inc/muhan-mud/server/internal/world"

func (g *WorldConnector) publishDMFamily(after world.State, actorID string, events []world.DMFamilyEvent) {
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
