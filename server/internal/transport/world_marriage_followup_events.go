package transport

import "github.com/1XP-Inc/muhan-mud/server/internal/world"

// publishDivorce projects only the events captured by a committed divorce
// receipt.  Recipient IDs/names are checked against the post-commit snapshot
// before delivery, so an online-name collision or reconnect cannot redirect a
// spouse notification.  The acceptance broadcast follows command11.c's
// broadcast() semantics and skips players with PNOBRD.
func (g *WorldConnector) publishDivorce(after world.State, result world.DivorceResult) {
	if result.ActorID == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	for _, event := range result.Events {
		if event.Kind != "spouse" || event.RecipientID == "" || event.RecipientName == "" || event.Text == "" {
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
				// A slow spouse cannot block the committed relationship receipt.
			}
		}
	}

	if !result.Broadcast || result.BroadcastText == "" {
		return
	}
	for connection := range g.connections {
		player, ok := after.Players[connection.lease.ActorID]
		if !ok || !player.Online || connection.events == nil || world.PlayerFlagSet(player.Body, world.MarriageDivorceNoBroadcastFlag) {
			continue
		}
		select {
		case connection.events <- result.BroadcastText:
		default:
			// A slow observer cannot block a committed divorce acceptance.
		}
	}
}
