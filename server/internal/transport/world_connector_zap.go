package transport

import "github.com/1XP-Inc/muhan-mud/server/internal/world"

// publishZap delivers zap's post-commit room/target lines after the first
// commit. A replay never calls this method.
func (g *WorldConnector) publishZap(after world.State, events []world.ZapEvent) {
	if len(events) == 0 {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, event := range events {
		if event.ActorID == "" || event.RoomID == 0 || event.Text == "" {
			continue
		}
		actor, ok := after.Players[event.ActorID]
		if !ok || !actor.Online || actor.Body.RoomID != event.RoomID {
			continue
		}
		for connection := range g.connections {
			player, ok := after.Players[connection.lease.ActorID]
			if !ok || !player.Online || player.Body.RoomID != event.RoomID || connection.events == nil {
				continue
			}
			if connection.lease.ActorID == event.ExcludeActorID {
				continue
			}
			if event.ExcludeTarget && event.TargetID != "" && connection.lease.ActorID == event.TargetID {
				continue
			}
			if event.TargetID != "" && !event.ExcludeTarget && connection.lease.ActorID != event.TargetID {
				continue
			}
			select {
			case connection.events <- event.Text:
			default:
			}
		}
	}
}
