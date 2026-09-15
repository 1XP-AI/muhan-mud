package world

import "fmt"

// Publication order: Effects, Vitals (including inline Deaths), light message.
type PlayerUpdateResult struct {
	Effects        []EffectEvent
	Vitals         VitalsTransitionResult
	SaveDue        bool
	ExtinguishedID string
}

// UpdatePlayer composes the implemented update_ply phases. All changes, including
// light consumed AFTER the legacy save checkpoint, belong to one durable commit.
// SaveDue is compatibility metadata, never permission to skip persisting a tick.
// Scheduler, actor-specific rendering and full C-function comparison are pending.
func (s State) UpdatePlayer(actorID string, now int32, view SceneOptions, catalog SpawnCatalog, roll func(int, int) int, allocate func() (string, error)) (State, PlayerUpdateResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, PlayerUpdateResult{}, err
	}
	p, ok := s.Players[actorID]
	if !ok || !p.Online || p.Items == nil {
		return State{}, PlayerUpdateResult{}, fmt.Errorf("canonical online player required")
	}
	hours := int64(p.Body.Timers[28].Interval) + int64(now) - int64(p.Body.Timers[28].LastTime)
	if hours < -2147483648 || hours > 2147483647 {
		return State{}, PlayerUpdateResult{}, fmt.Errorf("played time overflow")
	}
	p.Body.Timers[28].LastTime = now
	p.Body.Timers[28].Interval = int32(hours)
	var ready [20]*LegacyObject
	for slot, id := range p.Items.Ready {
		if id != "" {
			object := p.Items.Items[id].Object
			ready[slot] = &object
		}
	}
	effects, err := ExpireEffects(p.Body, ready, now)
	if err != nil {
		return State{}, PlayerUpdateResult{}, err
	}
	next := s.clone()
	p = next.Players[actorID]
	p.Body = effects.Player
	next.Players[actorID] = p
	next, vitals, err := next.TickPlayerVitals(actorID, now, view, catalog, roll, allocate)
	if err != nil {
		return State{}, PlayerUpdateResult{}, err
	}
	p = next.Players[actorID]
	saveDue := int64(now) > int64(p.Body.Timers[22].LastTime)+int64(p.Body.Timers[22].Interval)
	if saveDue {
		p.Body.Timers[22].LastTime = now
	}
	light, err := TickLight(p.Body, *p.Items)
	if err != nil {
		return State{}, PlayerUpdateResult{}, err
	}
	p.Items = &light.Items
	next.Players[actorID] = p
	if err := next.Validate(); err != nil {
		return State{}, PlayerUpdateResult{}, err
	}
	return next, PlayerUpdateResult{Effects: effects.Events, Vitals: vitals, SaveDue: saveDue, ExtinguishedID: light.ExtinguishedID}, nil
}
