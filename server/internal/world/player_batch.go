package world

import (
	"fmt"
	"sort"
)

type PlayerUpdateInput struct {
	ActorID string
	View    SceneOptions
}
type PlayerBatchResult struct {
	ActorID string
	Update  PlayerUpdateResult
}

// PlayerUpdateOrder derives the explicit, deterministic player phase order
// used by the world loop. Legacy update.c walks the live descriptor table; the
// Go runtime does not expose descriptors, so imported room membership order is
// preferred and remaining canonical players are appended by identity. This is
// an ordering policy, not a claim of complete update.c parity.
func (s State) PlayerUpdateOrder(hour int) ([]PlayerUpdateInput, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	ids := make([]int16, 0, len(s.Rooms))
	for id := range s.Rooms {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	seen := make(map[string]bool)
	order := make([]PlayerUpdateInput, 0, len(s.Players))
	appendPlayer := func(id string) error {
		p, ok := s.Players[id]
		if !ok || !p.Online || p.Body.Class == 12 || seen[id] {
			return nil
		}
		if p.Items == nil {
			return fmt.Errorf("online player %q inventory migration incomplete", id)
		}
		seen[id] = true
		order = append(order, PlayerUpdateInput{
			ActorID: id,
			View: SceneOptions{
				ViewOptions: ViewOptions{Hour: hour},
				ViewerID:    id,
			},
		})
		return nil
	}
	for _, roomID := range ids {
		for _, id := range s.Rooms[roomID].PlayerIDs {
			if err := appendPlayer(id); err != nil {
				return nil, err
			}
		}
	}
	remaining := make([]string, 0)
	for id, p := range s.Players {
		if p.Online && p.Body.Class != 12 && !seen[id] {
			remaining = append(remaining, id)
		}
	}
	sort.Strings(remaining)
	for _, id := range remaining {
		if err := appendPlayer(id); err != nil {
			return nil, err
		}
	}
	return order, nil
}

// UpdatePlayers follows an explicit server-owned session order, never Go map
// order. It requires every online non-DM player exactly once (update.c skips DM).
// The legacy scheduler's
// descriptor-order capture remains a runtime responsibility. One failure rejects
// the whole candidate; RNG/ID streams must be command-owned for safe replay.
// This is only the player phase, not update.c's NPC/room/global scheduler.
func (s State) UpdatePlayers(order []PlayerUpdateInput, now int32, catalog SpawnCatalog, roll func(int, int) int, allocate func() (string, error)) (State, []PlayerBatchResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, nil, err
	}
	seen := make(map[string]bool, len(order))
	for _, input := range order {
		p, ok := s.Players[input.ActorID]
		if !ok || !p.Online || p.Body.Class == 12 || p.Items == nil || seen[input.ActorID] {
			return State{}, nil, fmt.Errorf("invalid player update order")
		}
		seen[input.ActorID] = true
	}
	for id, p := range s.Players {
		if p.Online && p.Body.Class != 12 && !seen[id] {
			return State{}, nil, fmt.Errorf("online player omitted from update")
		}
	}
	next := s.clone()
	var results []PlayerBatchResult
	for _, input := range order {
		candidate, result, err := next.UpdatePlayer(input.ActorID, now, input.View, catalog, roll, allocate)
		if err != nil {
			return State{}, nil, fmt.Errorf("update player %q: %w", input.ActorID, err)
		}
		next = candidate
		results = append(results, PlayerBatchResult{ActorID: input.ActorID, Update: result})
	}
	return next, results, nil
}
