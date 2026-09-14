package transport

import "github.com/1XP-Inc/muhan-mud/server/internal/world"

// publishMoonSet delivers moon_set's two broadcast_rom lines after the first
// commit. Recipients are current same-room observers; the actor already
// received the command response. A replay never calls this method.
func (g *WorldConnector) publishMoonSet(after world.State, events []world.MoonSetEvent) {
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
			if !ok || !player.Online || player.Body.RoomID != event.RoomID || connection.lease.ActorID == event.ExcludeActorID || connection.events == nil {
				continue
			}
			select {
			case connection.events <- event.Text:
			default:
			}
		}
	}
}
