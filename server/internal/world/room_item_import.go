package world

import (
	"fmt"
	"sort"
)

// ImportRoomItems is the explicit one-time migration from legacy room object
// trees to canonical floor item graphs. Room IDs, root order, and nested
// preorder are stable; the caller owns durable allocator replay and global
// identity persistence. A failed migration never returns a partially applied
// snapshot.
func (s State) ImportRoomItems(allocate func() (string, error)) (State, error) {
	if err := s.Validate(); err != nil {
		return State{}, err
	}
	if allocate == nil {
		return State{}, fmt.Errorf("missing item ID allocator")
	}
	for id, room := range s.Rooms {
		if room.Items != nil {
			return State{}, fmt.Errorf("room %d already has canonical floor items", id)
		}
	}

	next := s.clone()
	roomIDs := make([]int, 0, len(next.Rooms))
	for id := range next.Rooms {
		roomIDs = append(roomIDs, int(id))
	}
	sort.Ints(roomIDs)
	for _, key := range roomIDs {
		roomID := int16(key)
		room := next.Rooms[roomID]
		items, err := ImportItems(room.Resource.Objects, allocate)
		if err != nil {
			return State{}, fmt.Errorf("room %d item migration: %w", roomID, err)
		}
		room.Resource.Objects = nil
		room.Items = &items
		next.Rooms[roomID] = room
	}
	if err := next.Validate(); err != nil {
		return State{}, err
	}
	return next, nil
}
