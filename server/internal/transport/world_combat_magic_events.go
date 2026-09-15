package transport

import (
	"strings"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

// publishBackstab preserves the legacy split between room output and a
// player-target's private damage warning. A target receives only a reveal
// projection (when present) and its private warning; the public strike text
// is reserved for other room occupants.
func (g *WorldConnector) publishBackstab(after world.State, event world.BackstabEvent) {
	if event.ActorID == "" {
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
		if connection.lease.ActorID == event.ExcludeActorID {
			continue
		}
		if event.TargetKind == world.BackstabTargetPlayer && connection.lease.ActorID == event.TargetID {
			for _, text := range event.Texts {
				if strings.Contains(text, "모습이 서서히 드러납니다") {
					select {
					case connection.events <- text:
					default:
					}
				}
			}
			if event.TargetText != "" {
				select {
				case connection.events <- event.TargetText:
				default:
				}
			}
			continue
		}
		for _, text := range event.Texts {
			if text == "" {
				continue
			}
			select {
			case connection.events <- text:
			default:
			}
		}
	}
}

// publishDrink projects a committed self-target potion announcement to room
// observers. The actor already receives the durable response and is excluded.
func (g *WorldConnector) publishDrink(after world.State, event world.DrinkEvent) {
	publishWorldRoomEvent(g, after, event.RoomID, event.ActorID, event.ExcludeActorID, event.Text)
}
