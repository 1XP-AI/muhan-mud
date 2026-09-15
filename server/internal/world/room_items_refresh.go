package world

import "fmt"

// PlanCanonicalRoomEntry renders a temporary object projection but returns a
// persistence candidate with only canonical item ownership. Entry.Room.Objects
// is also cleared after rendering so it cannot accidentally be saved twice.
func PlanCanonicalRoomEntry(room RoomState, players []RoomPlayerView, entrant RoomPlayerView, view SceneOptions, catalog SpawnCatalog, now int32, roll func(int, int) int, allocate func() (string, error)) (RoomState, RoomEntry, error) {
	return planCanonicalRoomEntryWithNPCEvents(room, players, entrant, view, catalog, now, roll, allocate, nil)
}

func planCanonicalRoomEntryWithNPCEvents(room RoomState, players []RoomPlayerView, entrant RoomPlayerView, view SceneOptions, catalog SpawnCatalog, now int32, roll func(int, int) int, allocate func() (string, error), onNPC func(int, LegacyMonster) error) (RoomState, RoomEntry, error) {
	var next RoomState
	entry, err := planRoomEntry(room.Resource, players, entrant, view, func(_ LegacyRoom) (LegacyRoom, error) {
		var err error
		next, err = refreshCanonicalRoomWithNPCEvents(room, catalog, now, roll, allocate, onNPC)
		if err != nil {
			return LegacyRoom{}, err
		}
		projection := next.Resource
		projection.Objects, err = next.Items.LegacyInventory()
		return projection, err
	})
	if err != nil {
		return RoomState{}, RoomEntry{}, err
	}
	entry.Room.Objects = nil
	next.Resource = entry.Room
	next.PlayerIDs = nil
	for _, p := range entry.Players {
		next.PlayerIDs = append(next.PlayerIDs, p.ID)
	}
	return next, entry, nil
}

// LegacyInventory is an ephemeral display/oracle projection, never a second
// persisted owner. Iterative postorder avoids recursion on deeply nested items.
func (c ItemCollection) LegacyInventory() ([]LegacyObject, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	order := append([]string(nil), c.Inventory...)
	for i := 0; i < len(order); i++ {
		order = append(order, c.Items[order[i]].Contents...)
	}
	objects := make(map[string]LegacyObject, len(order))
	for i := len(order) - 1; i >= 0; i-- {
		id := order[i]
		item := c.Items[id]
		object := item.Object
		for _, child := range item.Contents {
			object.Contents = append(object.Contents, objects[child])
		}
		objects[id] = object
	}
	var roots []LegacyObject
	for _, id := range c.Inventory {
		roots = append(roots, objects[id])
	}
	return roots, nil
}

// RefreshCanonicalRoom shares the legacy spawn rules but allocates identities
// only for observed NEW floor objects. The caller owns allocator replay and
// global uniqueness; State.Validate checks collisions outside this room.
func RefreshCanonicalRoom(room RoomState, catalog SpawnCatalog, now int32, roll func(int, int) int, allocate func() (string, error)) (RoomState, error) {
	return refreshCanonicalRoomWithNPCEvents(room, catalog, now, roll, allocate, nil)
}

func refreshCanonicalRoomWithNPCEvents(room RoomState, catalog SpawnCatalog, now int32, roll func(int, int) int, allocate func() (string, error), onNPC func(int, LegacyMonster) error) (RoomState, error) {
	if room.Items == nil || len(room.Resource.Objects) != 0 {
		return RoomState{}, fmt.Errorf("canonical floor required")
	}
	for _, id := range room.Items.Ready {
		if id != "" {
			return RoomState{}, fmt.Errorf("room cannot equip items")
		}
	}
	projected, err := room.Items.LegacyInventory()
	if err != nil {
		return RoomState{}, err
	}
	resource := room.Resource
	resource.Objects = projected
	var spawned []LegacyObject
	refreshed, err := refreshRoomResourcesWithNPCEvents(resource, catalog, now, roll, func(o LegacyObject) { spawned = append(spawned, o) }, onNPC)
	if err != nil {
		return RoomState{}, err
	}
	items := room.Items.clone()
	if len(spawned) > 0 {
		fresh, err := ImportItems(spawned, allocate)
		if err != nil {
			return RoomState{}, err
		}
		merged, err := TransferItemRoots(fresh, items, fresh.Inventory)
		if err != nil {
			return RoomState{}, err
		}
		items = merged.Destination
	}
	refreshed.Objects = nil
	return RoomState{Resource: refreshed, Items: &items, PlayerIDs: append([]string(nil), room.PlayerIDs...)}, nil
}
