package transport

import "github.com/1XP-Inc/muhan-mud/server/internal/world"

// publishTeach projects the committed teacher announcement to the target and
// other same-room players. The actor receives the durable receipt response,
// so it is excluded from the room fan-out and the target gets its private
// projection instead of the public room text.
func (g *WorldConnector) publishTeach(after world.State, event world.TeachEvent) {
	if event.ActorID == "" || event.TargetID == "" {
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
		if !ok || !player.Online || player.Body.RoomID != event.RoomID || connection.events == nil {
			continue
		}
		var text string
		switch connection.lease.ActorID {
		case event.ActorID:
			continue
		case event.TargetID:
			text = event.TargetText
		default:
			text = event.Text
		}
		if text == "" {
			continue
		}
		select {
		case connection.events <- text:
		default:
		}
	}
}
