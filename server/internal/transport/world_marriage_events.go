package transport

import "github.com/1XP-Inc/muhan-mud/server/internal/world"

// publishMarriage delivers the post-commit projections captured by a marriage
// receipt. Recipient IDs/names are part of the durable result, so a later
// online-name collision cannot redirect an event. The legacy marriage handler
// also broadcasts the completed marriage to every connected descriptor,
// including both spouses; that projection is intentionally kept separate from
// the actor's typed receipt and is emitted only after a first commit.
func (g *WorldConnector) publishMarriage(after world.State, result world.MarriageResult) {
	if result.ActorID == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	for _, event := range result.Events {
		if event.RecipientID == "" || event.RecipientName == "" || event.Text == "" {
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
				// A slow spouse cannot block the committed actor receipt.
			}
		}
	}

	if !result.Broadcast || result.BroadcastText == "" {
		return
	}
	for connection := range g.connections {
		player, ok := after.Players[connection.lease.ActorID]
		if !ok || !player.Online || connection.events == nil {
			continue
		}
		select {
		case connection.events <- result.BroadcastText:
		default:
			// A slow observer cannot block a committed marriage transition.
		}
	}
}
