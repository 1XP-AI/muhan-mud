package transport

import (
	"fmt"
	"sort"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

type roomEvent struct {
	RoomID         int16
	ExcludeActorID string
	Text           string
}

// movementEvents is a committed-state projection, not an authorization
// source. It deliberately emits only identities whose room changed between
// the serialized before/after snapshots; replayed receipts never call it.
// Exact legacy color/formatting remains a later renderer concern, but room
// fan-out is now explicit and deterministic.
func movementEvents(before, after world.State, actorID string) []roomEvent {
	actorBefore, beforeExists := before.Players[actorID]
	actorAfter, afterExists := after.Players[actorID]
	if !beforeExists || !afterExists || actorBefore.Body.RoomID == actorAfter.Body.RoomID {
		return nil
	}
	name := actorAfter.Body.Name
	events := []roomEvent{
		{RoomID: actorBefore.Body.RoomID, ExcludeActorID: actorID, Text: fmt.Sprintf("\n%s님이 떠났습니다.\r\n", name)},
		{RoomID: actorAfter.Body.RoomID, ExcludeActorID: actorID, Text: fmt.Sprintf("\n%s님이 도착했습니다.\r\n", name)},
	}

	playerIDs := make(map[string]struct{}, len(before.Players)+len(after.Players))
	for id := range before.Players {
		playerIDs[id] = struct{}{}
	}
	for id := range after.Players {
		playerIDs[id] = struct{}{}
	}
	orderedPlayers := make([]string, 0, len(playerIDs))
	for id := range playerIDs {
		orderedPlayers = append(orderedPlayers, id)
	}
	sort.Strings(orderedPlayers)
	for _, id := range orderedPlayers {
		if id == actorID {
			continue
		}
		oldPlayer, oldOK := before.Players[id]
		newPlayer, newOK := after.Players[id]
		if !oldOK || !newOK || oldPlayer.Body.RoomID == newPlayer.Body.RoomID {
			continue
		}
		if newPlayer.Body.RoomID != actorAfter.Body.RoomID {
			continue
		}
		events = append(events, roomEvent{
			RoomID:         newPlayer.Body.RoomID,
			ExcludeActorID: id,
			Text:           fmt.Sprintf("\n%s님이 따라왔습니다.\r\n", newPlayer.Body.Name),
		})
	}

	npcIDs := make(map[string]struct{}, len(before.NPCs)+len(after.NPCs))
	for id := range before.NPCs {
		npcIDs[id] = struct{}{}
	}
	for id := range after.NPCs {
		npcIDs[id] = struct{}{}
	}
	orderedNPCs := make([]string, 0, len(npcIDs))
	for id := range npcIDs {
		orderedNPCs = append(orderedNPCs, id)
	}
	sort.Strings(orderedNPCs)
	for _, id := range orderedNPCs {
		oldNPC, oldOK := before.NPCs[id]
		newNPC, newOK := after.NPCs[id]
		if !oldOK || !newOK || oldNPC.Body.RoomID == newNPC.Body.RoomID || newNPC.Body.RoomID != actorAfter.Body.RoomID {
			continue
		}
		events = append(events, roomEvent{
			RoomID: newNPC.Body.RoomID,
			Text:   fmt.Sprintf("\n%s이(가) 따라왔습니다.\r\n", newNPC.Body.Name),
		})
	}
	return events
}

func (g *WorldConnector) publishMovement(before, after world.State, actorID string) {
	events := movementEvents(before, after, actorID)
	if len(events) == 0 {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for connection := range g.connections {
		player, ok := after.Players[connection.lease.ActorID]
		if !ok || !player.Online {
			continue
		}
		for _, event := range events {
			if event.ExcludeActorID == connection.lease.ActorID || player.Body.RoomID != event.RoomID || connection.events == nil {
				continue
			}
			select {
			case connection.events <- event.Text:
			default:
				// A slow client cannot block the world command loop. Its next
				// explicit look/movement receives a fresh scene.
			}
		}
	}
}

func (g *WorldConnector) publishSay(after world.State, actorID, text string) {
	event, ok, err := after.RoomSayEvent(actorID, text)
	if err != nil || !ok {
		return
	}
	message := fmt.Sprintf("\n%s님이 \"%s\"라고 말합니다.\r\n", event.ActorName, event.Text)
	g.mu.Lock()
	defer g.mu.Unlock()
	for connection := range g.connections {
		player, exists := after.Players[connection.lease.ActorID]
		if !exists || !player.Online || player.Body.RoomID != event.RoomID || connection.lease.ActorID == actorID || connection.events == nil {
			continue
		}
		select {
		case connection.events <- message:
		default:
			// A slow client cannot block the speaker's durable command.
		}
	}
}
