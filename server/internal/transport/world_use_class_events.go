package transport

import "github.com/1XP-Inc/muhan-mud/server/internal/world"

// publishUse sends the post-commit dispatcher announcement to room observers.
// The actor receives the durable receipt synchronously, so its connection is
// excluded and a receipt replay never emits a second event.
func (g *WorldConnector) publishUse(after world.State, event world.UseEvent) {
	publishWorldRoomEvent(g, after, event.RoomID, event.ActorID, event.ExcludeActorID, event.Text)
}
