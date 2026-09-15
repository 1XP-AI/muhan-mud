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

// publishBurn delivers the committed burn announcement to current room
// occupants. The actor already received the durable response, so the event's
// exclusion keeps the terminal output from being duplicated.
func (g *WorldConnector) publishBurn(after world.State, event world.BurnEvent) {
	publishWorldRoomEvent(g, after, event.RoomID, event.ActorID, event.ExcludeActorID, event.Text)
}

// publishStudy delivers the committed scroll-study announcement to current
// room occupants, suppressing replay and the actor's already-rendered receipt.
func (g *WorldConnector) publishStudy(after world.State, event world.StudyEvent) {
	publishWorldRoomEvent(g, after, event.RoomID, event.ActorID, event.ExcludeActorID, event.Text)
}

// publishRecall fans out magic5.c:recall's source-room broadcast after the
// caster has already left that room, then room.c:add_ply_rom dest arrival.
// publishWorldRoomEvent requires the actor to still occupy event.RoomID, so
// this path follows publishReturnSquare: current source occupants receive
// Event.Text, dest occupants receive dest arrival, the caster is excluded,
// and replayed receipts never re-enter here.
func (g *WorldConnector) publishRecall(after world.State, result world.CastResult) {
	if !result.Broadcast || result.Event == nil || !world.IsRecallCastSpell(result.SpellName) {
		return
	}
	event := *result.Event
	if event.ActorID == "" || event.Text == "" || event.ExcludeActorID == "" {
		return
	}
	if event.ExcludeTargetID != "" {
		destText := ""
		destID := int16(0)
		if target, ok := after.Players[result.TargetID]; ok && target.Online && world.RecallDestArrivalVisible(target.Body) {
			destID = target.Body.RoomID
			destText = world.RecallDestArrivalText(target.Body.Name)
		}
		g.mu.Lock()
		defer g.mu.Unlock()
		for connection := range g.connections {
			actorID := connection.lease.ActorID
			player, ok := after.Players[actorID]
			if !ok || !player.Online || connection.events == nil || actorID == event.ExcludeActorID || actorID == event.ExcludeTargetID {
				continue
			}
			if player.Body.RoomID == event.RoomID {
				select {
				case connection.events <- event.Text:
				default:
				}
			}
			if destText != "" && player.Body.RoomID == destID {
				select {
				case connection.events <- destText:
				default:
				}
			}
		}
		return
	}
	destText := ""
	destID := int16(0)
	if actor, ok := after.Players[event.ActorID]; ok && actor.Online && world.RecallDestArrivalVisible(actor.Body) {
		destID = actor.Body.RoomID
		destText = world.RecallDestArrivalText(actor.Body.Name)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for connection := range g.connections {
		player, ok := after.Players[connection.lease.ActorID]
		if !ok || !player.Online || connection.events == nil || connection.lease.ActorID == event.ExcludeActorID {
			continue
		}
		if player.Body.RoomID == event.RoomID {
			select {
			case connection.events <- event.Text:
			default:
			}
		}
		if destText != "" && player.Body.RoomID == destID {
			select {
			case connection.events <- destText:
			default:
			}
		}
	}
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
