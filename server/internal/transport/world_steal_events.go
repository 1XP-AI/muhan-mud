package transport

import "github.com/1XP-Inc/muhan-mud/server/internal/world"

// publishSteal projects the committed command6.c failure/reveal messages.
// The actor gets the durable receipt response, so room fan-out excludes it;
// a player target additionally receives its private warning. Replay callers
// never invoke this method.
func (g *WorldConnector) publishSteal(after world.State, event world.StealEvent) {
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
		player, exists := after.Players[connection.lease.ActorID]
		if !exists || !player.Online || connection.events == nil {
			continue
		}
		isPlayerTarget := event.TargetKind == world.StealTargetPlayer && connection.lease.ActorID == event.TargetID && player.Body.Name == event.TargetName
		if isPlayerTarget {
			// command6.c's broadcast_rom2 excludes the player target. A reveal
			// was broadcast earlier through broadcast_rom, so preserve that
			// one room-visible line before the target-only warning.
			if event.RevealText != "" {
				select {
				case connection.events <- event.RevealText:
				default:
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
		if connection.lease.ActorID == event.ExcludeActorID || player.Body.RoomID != event.RoomID {
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
