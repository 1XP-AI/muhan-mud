package transport

import (
	"strings"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

// publishTurn and publishAbsorb project the post-commit NPC spell event to
// room observers. The actor receives its durable response synchronously.
func (g *WorldConnector) publishTurn(after world.State, event world.TurnEvent) {
	text := event.Text
	if text == "" {
		text = strings.Join(event.Texts, "")
	}
	publishWorldRoomEvent(g, after, event.RoomID, event.ActorID, event.ExcludeActorID, text)
}

func (g *WorldConnector) publishAbsorb(after world.State, event world.AbsorbEvent) {
	text := event.Text
	if text == "" {
		text = strings.Join(event.Texts, "")
	}
	publishWorldRoomEvent(g, after, event.RoomID, event.ActorID, event.ExcludeActorID, text)
}

// publishKick uses the existing targeted combat projection. NPC kicks are
// room-wide; player kicks keep the private target acknowledgement isolated.
func (g *WorldConnector) publishKick(after world.State, event world.KickEvent) {
	publishTargetedCombatEvent(g, after, event.RoomID, event.ActorID, event.ExcludeActorID,
		event.TargetKind == world.KickTargetPlayer, event.ExcludeTargetID, event.TargetText,
		event.Texts)
}
