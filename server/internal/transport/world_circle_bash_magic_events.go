package transport

import (
	"strings"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

// publishCircle projects the committed room attempt. Player targets receive
// only the reveal (when present) and their private warning; NPC targets use
// the ordinary room fan-out. The actor is excluded because its durable
// receipt is rendered synchronously by Submit.
func (g *WorldConnector) publishCircle(after world.State, event world.CircleEvent) {
	publishTargetedCombatEvent(g, after, event.RoomID, event.ActorID, event.ExcludeActorID,
		event.TargetKind == world.CircleTargetPlayer, event.TargetID, event.TargetText,
		event.Texts)
}

// publishBash follows the same room/private projection used by backstab and
// steal, while retaining the ordered reveal/attempt lines in Texts.
func (g *WorldConnector) publishBash(after world.State, event world.BashEvent) {
	publishTargetedCombatEvent(g, after, event.RoomID, event.ActorID, event.ExcludeActorID,
		event.TargetKind == world.BashTargetPlayer, event.ExcludeTargetID, event.TargetText,
		event.Texts)
}

// publishMagicStop is NPC-only in the bounded command contract, so every
// same-room observer receives the ordered spell projection except the actor.
func (g *WorldConnector) publishMagicStop(after world.State, event world.MagicStopEvent) {
	text := event.Text
	if text == "" {
		text = strings.Join(event.Texts, "")
	}
	publishWorldRoomEvent(g, after, event.RoomID, event.ActorID, event.ExcludeActorID, text)
}

func publishTargetedCombatEvent(g *WorldConnector, after world.State, roomID int16, actorID, excludeActorID string, playerTarget bool, targetID, targetText string, texts []string) {
	if actorID == "" || excludeActorID == "" || !playerTarget && len(texts) == 0 {
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
		if !ok || !player.Online || player.Body.RoomID != roomID || connection.events == nil || connection.lease.ActorID == excludeActorID {
			continue
		}
		if playerTarget && connection.lease.ActorID == targetID {
			// A hidden/invisible reveal is part of the public source event and
			// must remain visible to the target before its private warning.
			for _, text := range texts {
				if strings.Contains(text, "모습이 서서히 드러납니다") || strings.Contains(text, "모습이 나타나기 시작합니다") {
					nonBlockingSend(connection, text)
				}
			}
			nonBlockingSend(connection, targetText)
			continue
		}
		for _, text := range texts {
			nonBlockingSend(connection, text)
		}
	}
}

func nonBlockingSend(connection *worldConnection, text string) {
	if connection == nil || connection.events == nil || text == "" {
		return
	}
	select {
	case connection.events <- text:
	default:
	}
}
