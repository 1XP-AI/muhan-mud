package transport

import "github.com/1XP-Inc/muhan-mud/server/internal/world"

// publishItemRename delivers the committed room announcement to current
// observers. The actor receives the durable command response; a replay never
// calls this method, so the room event cannot be duplicated.
func (g *WorldConnector) publishItemRename(after world.State, event world.ItemRenameEvent) {
	if event.ActorID == "" || event.RoomID == 0 || event.Text == "" {
		return
	}
	actor, ok := after.Players[event.ActorID]
	if !ok || !actor.Online || actor.Body.RoomID != event.RoomID {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for connection := range g.connections {
		player, ok := after.Players[connection.lease.ActorID]
		if !ok || !player.Online || player.Body.RoomID != event.RoomID || connection.lease.ActorID == event.ExcludeActorID || connection.events == nil {
			continue
		}
		select {
		case connection.events <- event.Text:
		default:
		}
	}
}
