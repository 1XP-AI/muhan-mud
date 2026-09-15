package world

import "fmt"

type VitalsTransitionResult struct {
	Messages []string
	// Ordered inline deaths, interleaved after Messages[:AfterMessages].
	Deaths []VitalsDeathEvent
	// Last death convenience view; use Deaths to publish all events.
	Death *PlayerDeathResult
}

type VitalsDeathEvent struct {
	AfterMessages int
	Result        PlayerDeathResult
}

// TickPlayerVitals composes the healing/ailment phase including inline deaths
// and their continuations. This is NOT all of update_ply: other effects and
// scheduled phases still require a tick state machine.
// Do not schedule this as a complete legacy tick. Commit state/result together.
func (s State) TickPlayerVitals(actorID string, now int32, view SceneOptions, catalog SpawnCatalog, roll func(int, int) int, allocate func() (string, error)) (State, VitalsTransitionResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, VitalsTransitionResult{}, err
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online {
		return State{}, VitalsTransitionResult{}, fmt.Errorf("missing online vitals actor")
	}
	room := s.Rooms[actor.Body.RoomID].Resource
	next := s.clone()
	var deaths []PlayerDeathResult
	vitals, err := PlanVitalsWithDeath(actor.Body, &room, now, roll, func(body LegacyMonster) (LegacyMonster, *LegacyRoom, error) {
		p := next.Players[actorID]
		p.Body = body
		p.Body.Inventory = cloneObjects(body.Inventory)
		next.Players[actorID] = p
		candidate, death, err := next.PlanPlayerDeath(actorID, actorID, now, view, catalog, roll, allocate)
		if err != nil {
			return LegacyMonster{}, nil, err
		}
		next = candidate
		deaths = append(deaths, death)
		p = next.Players[actorID]
		destination := next.Rooms[p.Body.RoomID].Resource
		return p.Body, &destination, nil
	})
	if err != nil {
		return State{}, VitalsTransitionResult{}, err
	}
	actor = next.Players[actorID]
	actor.Body = vitals.Player
	actor.Body.Inventory = cloneObjects(vitals.Player.Inventory)
	next.Players[actorID] = actor
	result := VitalsTransitionResult{Messages: append([]string(nil), vitals.Messages...)}
	for i, death := range deaths {
		result.Deaths = append(result.Deaths, VitalsDeathEvent{AfterMessages: vitals.DeathMessageOffsets[i], Result: death})
	}
	if len(deaths) > 0 {
		last := deaths[len(deaths)-1]
		result.Death = &last
	}
	if err := next.Validate(); err != nil {
		return State{}, VitalsTransitionResult{}, err
	}
	return next, result, nil
}
