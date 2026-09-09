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

func (g *WorldConnector) publishYell(after world.State, actorID, text string) {
	events, err := after.RoomYellEvents(actorID, text)
	if err != nil || len(events) == 0 {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for connection := range g.connections {
		player, exists := after.Players[connection.lease.ActorID]
		if !exists || !player.Online || connection.events == nil {
			continue
		}
		for _, event := range events {
			if event.ExcludeActorID == connection.lease.ActorID || player.Body.RoomID != event.RoomID {
				continue
			}
			select {
			case connection.events <- event.Text:
			default:
				// A slow client cannot block the yeller's durable command.
			}
		}
	}
}

// publishDirectMessage delivers the committed recipient projection to the
// exact online target. The target identity and rendered text come from the
// receipt, so a replay or a later name collision cannot redirect the message.
func (g *WorldConnector) publishDirectMessage(after world.State, event world.DirectMessageEvent) {
	if event.TargetID == "" || event.Text == "" {
		return
	}
	target, ok := after.Players[event.TargetID]
	if !ok || !target.Online || target.Body.Name != event.TargetName {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for connection := range g.connections {
		if connection.lease.ActorID != event.TargetID || connection.events == nil {
			continue
		}
		select {
		case connection.events <- event.Text:
		default:
			// A slow recipient must not block the sender's durable command.
		}
	}
}

// publishNPCTalk delivers the committed room projection to current occupants
// other than the actor. The actor receives the NPC response through the
// command receipt; replayed receipts never fan out the room projection again.
func (g *WorldConnector) publishNPCTalk(after world.State, event world.NPCTalkEvent) {
	if event.ActorID == "" || len(event.RoomMessages) == 0 {
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
		if !exists || !player.Online || player.Body.RoomID != event.RoomID || connection.events == nil {
			continue
		}
		for _, message := range event.RoomMessages {
			if message.Text == "" || message.ExcludeActorID == connection.lease.ActorID {
				continue
			}
			select {
			case connection.events <- message.Text:
			default:
				// A slow observer cannot block the talking actor's durable command.
			}
		}
	}
}

// publishGroupTalk sends each committed event to its exact player recipient.
// NPC follower events remain in the receipt for deterministic audit but have
// no websocket connection to receive a live projection.
func (g *WorldConnector) publishGroupTalk(after world.State, events []world.GroupTalkEvent) {
	if len(events) == 0 {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, event := range events {
		if event.RecipientKind != "player" || event.RecipientID == "" || event.Text == "" {
			continue
		}
		recipient, ok := after.Players[event.RecipientID]
		if !ok || !recipient.Online || recipient.Body.Name != event.RecipientName {
			continue
		}
		for connection := range g.connections {
			if connection.lease.ActorID != event.RecipientID || connection.events == nil {
				continue
			}
			select {
			case connection.events <- event.Text:
			default:
				// A slow group member cannot block the sender's durable command.
			}
		}
	}
}

// publishEmote fans out the committed action projection. The actor already
// received the durable receipt response; a targeted recipient gets its
// target-specific projection and everyone else in the room gets the room
// projection. Replayed receipts never call this method.
func (g *WorldConnector) publishEmote(after world.State, actorID, alias, targetName string) {
	targetID := ""
	if targetName != "" {
		var err error
		targetID, err = after.SelectPlayerInRoom(actorID, targetName)
		if err != nil {
			return
		}
	}
	event, ok, err := after.RoomEmoteEvent(actorID, alias, targetID)
	if err != nil || !ok {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for connection := range g.connections {
		player, exists := after.Players[connection.lease.ActorID]
		if !exists || !player.Online || player.Body.RoomID != event.RoomID || connection.events == nil {
			continue
		}
		if connection.lease.ActorID == event.ExcludeActorID {
			continue
		}
		message := event.Text
		if connection.lease.ActorID == event.TargetID {
			message = event.TargetText
		}
		if message == "" {
			continue
		}
		select {
		case connection.events <- message:
		default:
			// A slow client cannot block the actor's durable command.
		}
	}
}

func (g *WorldConnector) publishExpress(after world.State, actorID, text string) {
	event, ok, err := after.RoomExpressEvent(actorID, text)
	if err != nil || !ok {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for connection := range g.connections {
		player, exists := after.Players[connection.lease.ActorID]
		if !exists || !player.Online || player.Body.RoomID != event.RoomID || connection.lease.ActorID == event.ExcludeActorID || connection.events == nil {
			continue
		}
		select {
		case connection.events <- event.Text:
		default:
			// A slow client cannot block the actor's durable command.
		}
	}
}

func (g *WorldConnector) publishLookAtTarget(after world.State, actorID, targetName string, occurrence int) {
	event, ok, err := after.LookAtTargetEventForNameOccurrence(actorID, targetName, occurrence)
	if err != nil || !ok {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for connection := range g.connections {
		player, exists := after.Players[connection.lease.ActorID]
		if !exists || !player.Online || player.Body.RoomID != event.RoomID || connection.events == nil || connection.lease.ActorID == event.ExcludeActorID {
			continue
		}
		if connection.lease.ActorID == event.ExcludeTargetID {
			if event.TargetText == "" {
				continue
			}
			select {
			case connection.events <- event.TargetText:
			default:
			}
			continue
		}
		if event.Text == "" {
			continue
		}
		select {
		case connection.events <- event.Text:
		default:
			// A slow client cannot block the actor's durable command.
		}
	}
}

// publishSearch fans out the two committed search broadcasts in source order.
// The actor already receives the durable response; replayed receipts never
// call this method, so a lost response cannot duplicate room output.
func (g *WorldConnector) publishSearch(after world.State, actorID string, targets []world.SearchTarget) {
	event, ok, err := after.RoomSearchEvent(actorID, targets)
	if err != nil || !ok {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for connection := range g.connections {
		player, exists := after.Players[connection.lease.ActorID]
		if !exists || !player.Online || player.Body.RoomID != event.RoomID || connection.lease.ActorID == event.ExcludeActorID || connection.events == nil {
			continue
		}
		for _, text := range event.Texts {
			if text == "" {
				continue
			}
			select {
			case connection.events <- text:
			default:
				// A slow client cannot block the actor's durable command.
			}
		}
	}
}

func (g *WorldConnector) publishTrack(after world.State, actorID string) {
	event, ok, err := after.RoomTrackEvent(actorID)
	if err != nil || !ok {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for connection := range g.connections {
		player, exists := after.Players[connection.lease.ActorID]
		if !exists || !player.Online || player.Body.RoomID != event.RoomID || connection.lease.ActorID == event.ExcludeActorID || connection.events == nil {
			continue
		}
		select {
		case connection.events <- event.Text:
		default:
			// A slow client cannot block the durable tracker response.
		}
	}
}

// publishHide emits the committed bare-player hide projection. The actor
// already received the durable response; every other online occupant gets the
// room event, and replayed receipts never enter this path.
func (g *WorldConnector) publishHide(after world.State, actorID string, succeeded bool) {
	event, ok, err := after.RoomHideEvent(actorID, succeeded)
	if err != nil || !ok {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for connection := range g.connections {
		player, exists := after.Players[connection.lease.ActorID]
		if !exists || !player.Online || player.Body.RoomID != event.RoomID || connection.lease.ActorID == event.ExcludeActorID || connection.events == nil {
			continue
		}
		select {
		case connection.events <- event.Text:
		default:
			// A slow client cannot block the hider's durable command.
		}
	}
}

// publishFlee fans out the committed source departure and the selected
// destination arrival. The legacy handler emits the latter on both successful
// movement and destination denial; the actor receives only the durable
// response, and replayed receipts never re-enter this path.
func (g *WorldConnector) publishFlee(after world.State, result world.FleeResult) {
	event, ok, err := after.RoomFleeEvent(result)
	if err != nil || !ok {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for connection := range g.connections {
		player, exists := after.Players[connection.lease.ActorID]
		if !exists || !player.Online || connection.events == nil {
			continue
		}
		if event.SourceText != "" && player.Body.RoomID == event.SourceRoomID && connection.lease.ActorID != event.ExcludeActorID {
			select {
			case connection.events <- event.SourceText:
			default:
				// A slow client cannot block the fleeing actor's durable command.
			}
		}
		if event.DestinationText != "" && player.Body.RoomID == event.DestinationRoomID && connection.lease.ActorID != event.ExcludeActorID {
			select {
			case connection.events <- event.DestinationText:
			default:
				// A slow client cannot block the fleeing actor's durable command.
			}
		}
	}
}

// publishPeek mirrors broadcast_rom2: the actor already has the durable
// response, the target receives the private alert, and other room occupants
// receive the room alert. Both projections are receipt-bound and only run on
// the first committed command.
func (g *WorldConnector) publishPeek(after world.State, actorID string, result world.PeekResult) {
	if result.TargetID == "" || result.TargetText == "" {
		return
	}
	actor, ok := after.Players[actorID]
	if !ok || !actor.Online {
		return
	}
	roomID := actor.Body.RoomID
	g.mu.Lock()
	defer g.mu.Unlock()
	for connection := range g.connections {
		player, exists := after.Players[connection.lease.ActorID]
		if !exists || !player.Online || player.Body.RoomID != roomID || connection.events == nil || connection.lease.ActorID == actorID {
			continue
		}
		message := ""
		if connection.lease.ActorID == result.TargetID && result.TargetKind == "player" {
			message = result.TargetText
		} else if connection.lease.ActorID != result.TargetID {
			message = result.RoomText
		}
		if message == "" {
			continue
		}
		select {
		case connection.events <- message:
		default:
			// A slow client cannot block the peeker's durable command.
		}
	}
}

// publishDoor emits the committed open/close room projection. The actor
// already received the durable response; replayed receipts never call this
// path, and slow clients are dropped without blocking the command loop.
func (g *WorldConnector) publishDoor(after world.State, actorID string, result world.DoorCommandResult) {
	event, ok, err := after.RoomDoorEvent(actorID, result.Action, result.ExitName)
	if err != nil || !ok {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for connection := range g.connections {
		player, exists := after.Players[connection.lease.ActorID]
		if !exists || !player.Online || player.Body.RoomID != event.RoomID || connection.lease.ActorID == event.ExcludeActorID || connection.events == nil {
			continue
		}
		select {
		case connection.events <- event.Text:
		default:
			// A slow client cannot block the actor's durable door command.
		}
	}
}

// publishDoorKey emits unlock/lock events and the ordered picklock attempt /
// success projections. The durable actor response is sent by Submit; replayed
// receipts never enter this path.
func (g *WorldConnector) publishDoorKey(after world.State, actorID string, result world.DoorKeyCommandResult) {
	events, err := after.RoomDoorKeyEvents(actorID, result)
	if err != nil || len(events) == 0 {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for connection := range g.connections {
		player, exists := after.Players[connection.lease.ActorID]
		if !exists || !player.Online || connection.events == nil {
			continue
		}
		for _, event := range events {
			if event.ExcludeActorID == connection.lease.ActorID || player.Body.RoomID != event.RoomID {
				continue
			}
			select {
			case connection.events <- event.Text:
			default:
				// A slow client cannot block the key command's durable commit.
			}
		}
	}
}

// publishTrade mirrors command10.c's room broadcast after a committed NPC
// exchange. The actor already receives the durable response; replayed receipts
// never re-enter this path, and a slow connection is dropped without blocking
// the world command loop.
func (g *WorldConnector) publishTrade(after world.State, actorID string, result world.NPCTradeResult) {
	if result.Action != "trade-npc-item" || result.OfferedItemName == "" {
		return
	}
	actor, ok := after.Players[actorID]
	if !ok || !actor.Online {
		return
	}
	message := fmt.Sprintf("\n%s이 %s에게 %s를 교환합니다.\r\n", actor.Body.Name, result.NPCName, result.OfferedItemName)
	g.mu.Lock()
	defer g.mu.Unlock()
	for connection := range g.connections {
		player, exists := after.Players[connection.lease.ActorID]
		if !exists || !player.Online || player.Body.RoomID != actor.Body.RoomID || connection.lease.ActorID == actorID || connection.events == nil {
			continue
		}
		select {
		case connection.events <- message:
		default:
			// A slow client cannot block the trader's durable command.
		}
	}
}
