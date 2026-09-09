package transport

import "github.com/1XP-Inc/muhan-mud/server/internal/world"

// publishRangerPray delivers a committed self-ability announcement to room
// observers. The actor already receives the typed receipt response, and the
// connector calls this only for a first execution (never for replay).
func (g *WorldConnector) publishRangerPray(after world.State, event world.RangerPrayEvent) {
	publishWorldRoomEvent(g, after, event.RoomID, event.ActorID, event.ExcludeActorID, event.Text)
}

func (g *WorldConnector) publishPrepare(after world.State, event world.PrepareEvent) {
	publishWorldRoomEvent(g, after, event.RoomID, event.ActorID, event.ExcludeActorID, event.Text)
}

func (g *WorldConnector) publishUpDmg(after world.State, event world.UpDmgEvent) {
	publishWorldRoomEvent(g, after, event.RoomID, event.ActorID, event.ExcludeActorID, event.Text)
}

func (g *WorldConnector) publishPowerAccuracy(after world.State, event world.PowerAccuracyEvent) {
	publishWorldRoomEvent(g, after, event.RoomID, event.ActorID, event.ExcludeActorID, event.Text)
}

func (g *WorldConnector) publishMeditate(after world.State, event world.MeditateEvent) {
	publishWorldRoomEvent(g, after, event.RoomID, event.ActorID, event.ExcludeActorID, event.Text)
}

// publishBribe delivers the committed NPC-bribe announcement to room
// observers. The actor receives the typed receipt response, and the connector
// calls this only for a first execution so a receipt replay cannot rebroadcast
// the same room event.
func (g *WorldConnector) publishBribe(after world.State, event world.BribeEvent) {
	publishWorldRoomEvent(g, after, event.RoomID, event.ActorID, event.ExcludeActorID, event.Text)
}

func publishWorldRoomEvent(g *WorldConnector, after world.State, roomID int16, actorID, excludeActorID, text string) {
	if actorID == "" || text == "" || excludeActorID == "" {
		return
	}
	actor, ok := after.Players[actorID]
	if !ok || !actor.Online || actor.Body.RoomID != roomID {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for connection := range g.connections {
		player, ok := after.Players[connection.lease.ActorID]
		if !ok || !player.Online || player.Body.RoomID != roomID || connection.lease.ActorID == excludeActorID || connection.events == nil {
			continue
		}
		select {
		case connection.events <- text:
		default:
		}
	}
}
