package transport

import (
	"strings"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

// publishPoison sends the ordered room projection after the poison receipt
// commits. The actor already receives Result.Response synchronously, so the
// event excludes that connection. Poison targets are NPCs in this bounded
// slice; all other visible players receive the same source-ordered text.
func (g *WorldConnector) publishPoison(after world.State, event world.PoisonEvent) {
	text := event.Text
	if text == "" {
		text = strings.Join(event.Texts, "")
	}
	publishWorldRoomEvent(g, after, event.RoomID, event.ActorID, event.ExcludeActorID, text)
}

// publishGive keeps the recipient's private acknowledgement separate from
// the room observer projection. The event is currently player-only; the
// helper nevertheless checks TargetKind so a future NPC branch cannot leak a
// private player message to an arbitrary room connection.
func (g *WorldConnector) publishGive(after world.State, event world.GiveEvent) {
	if event.ActorID == "" || event.ExcludeActorID == "" {
		return
	}
	actor, ok := after.Players[event.ActorID]
	if !ok || !actor.Online || actor.Body.RoomID != event.RoomID {
		return
	}
	observerText := event.ObserverText
	if observerText == "" {
		observerText = event.Text
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for connection := range g.connections {
		player, ok := after.Players[connection.lease.ActorID]
		if !ok || !player.Online || player.Body.RoomID != event.RoomID || connection.events == nil || connection.lease.ActorID == event.ExcludeActorID {
			continue
		}
		if event.TargetKind == world.GiveTargetPlayer && connection.lease.ActorID == event.TargetID {
			nonBlockingSend(connection, event.TargetText)
			continue
		}
		nonBlockingSend(connection, observerText)
	}
}
