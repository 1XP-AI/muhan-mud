package world

import (
	"errors"
	"fmt"
	"reflect"
)

// ErrRoomResourceRefreshNPC marks the deliberately closed boundary around
// permanent-NPC refresh. A room refresh may update canonical floor objects and
// door deadlines, but creating an NPC also needs its identity origin and the
// global active-list insertion order. Those effects belong to a separate
// identity-aware phase and must not be inferred here.
var ErrRoomResourceRefreshNPC = errors.New("room resource refresh requires an NPC identity delta")

// RoomResourceRefreshDelta is an isolated replacement for one canonical room.
// Before captures the exact room snapshot used by planning; After contains
// only the floor/door refresh candidate. Apply rejects a delta against any
// changed room so callers can commit it as one atomic state candidate.
type RoomResourceRefreshDelta struct {
	RoomID int16
	Before RoomState
	After  RoomState
}

func cloneRoomState(room RoomState) RoomState {
	out := room
	out.Resource = cloneRoom(room.Resource)
	out.PlayerIDs = append([]string(nil), room.PlayerIDs...)
	out.NPCIDs = append([]string(nil), room.NPCIDs...)
	if room.Items != nil {
		items := room.Items.clone()
		out.Items = &items
	}
	return out
}

func roomHasDuePermanentMonster(slots [10]LegacyTimer, now int32) bool {
	for _, slot := range slots {
		if slot.Misc != 0 && int64(slot.LastTime)+int64(slot.Interval) <= int64(now) {
			return true
		}
	}
	return false
}

// PlanRoomResourceRefresh plans only the resource phase that can be applied
// without discovering or creating canonical NPC identities. It shares the
// existing deterministic object/door implementation, including injected RNG
// and item-ID allocation. A due permanent-monster timer rejects the entire
// plan before any catalog, RNG, or allocator call, rather than publishing a
// partial floor refresh.
func (s State) PlanRoomResourceRefresh(roomID int16, catalog SpawnCatalog, now int32, roll func(int, int) int, allocate func() (string, error)) (RoomResourceRefreshDelta, error) {
	if err := s.Validate(); err != nil {
		return RoomResourceRefreshDelta{}, err
	}
	room, ok := s.Rooms[roomID]
	if !ok {
		return RoomResourceRefreshDelta{}, fmt.Errorf("room absent")
	}
	if room.Items == nil || len(room.Resource.Objects) != 0 {
		return RoomResourceRefreshDelta{}, fmt.Errorf("canonical floor required")
	}
	if roomHasDuePermanentMonster(room.Resource.PermanentMonsters, now) {
		return RoomResourceRefreshDelta{}, ErrRoomResourceRefreshNPC
	}

	// Work from a deep-isolated snapshot. ProjectRoom supplies the canonical
	// NPC bodies to the legacy refresh helper only so it can count existing
	// occupants; the identity-owned representation is restored before After is
	// returned. No NPC can be created or reordered in this phase.
	snapshot := s.clone()
	room = snapshot.Rooms[roomID]
	if snapshot.NPCs != nil {
		projected, err := snapshot.ProjectRoom(roomID)
		if err != nil {
			return RoomResourceRefreshDelta{}, err
		}
		room.Resource = projected
	}
	refreshed, err := RefreshCanonicalRoom(room, catalog, now, roll, allocate)
	if err != nil {
		return RoomResourceRefreshDelta{}, err
	}
	if snapshot.NPCs != nil {
		if len(refreshed.Resource.Monsters) != len(room.NPCIDs) {
			return RoomResourceRefreshDelta{}, fmt.Errorf("NPC refresh changed canonical room membership")
		}
		refreshed.Resource.Monsters = nil
		refreshed.NPCIDs = append([]string(nil), room.NPCIDs...)
	}
	refreshed.PlayerIDs = append([]string(nil), room.PlayerIDs...)
	refreshed = cloneRoomState(refreshed)
	return RoomResourceRefreshDelta{
		RoomID: roomID,
		Before: cloneRoomState(snapshot.Rooms[roomID]),
		After:  refreshed,
	}, nil
}

func validateRoomResourceRefreshDelta(delta RoomResourceRefreshDelta) error {
	if delta.Before.Resource.ID != delta.RoomID || delta.After.Resource.ID != delta.RoomID {
		return fmt.Errorf("room refresh ID mismatch")
	}
	if delta.After.Items == nil || len(delta.After.Resource.Objects) != 0 {
		return fmt.Errorf("room refresh candidate is not canonical")
	}
	if !reflect.DeepEqual(delta.After.PlayerIDs, delta.Before.PlayerIDs) || !reflect.DeepEqual(delta.After.NPCIDs, delta.Before.NPCIDs) {
		return fmt.Errorf("room refresh changed room membership")
	}
	// RefreshDoors is the only legacy resource mutation admitted here. Keep
	// every other resource field, including NPC bodies and respawn timers,
	// byte-for-byte equal to the planned source.
	resource := delta.Before.Resource
	resource.Exits = append([]LegacyExit(nil), delta.After.Resource.Exits...)
	resource.Objects = nil
	if !reflect.DeepEqual(delta.After.Resource, resource) {
		return fmt.Errorf("room refresh changed unsupported resource state")
	}
	return nil
}

// ApplyRoomResourceRefresh applies a previously planned room delta to the
// exact source room. Any stale or tampered candidate returns a zero State;
// neither the input state nor a partial clone is published.
func (s State) ApplyRoomResourceRefresh(delta RoomResourceRefreshDelta) (State, error) {
	if err := s.Validate(); err != nil {
		return State{}, err
	}
	if err := validateRoomResourceRefreshDelta(delta); err != nil {
		return State{}, err
	}
	room, ok := s.Rooms[delta.RoomID]
	if !ok || !reflect.DeepEqual(room, delta.Before) {
		return State{}, fmt.Errorf("stale room refresh")
	}
	next := s.clone()
	next.Rooms[delta.RoomID] = cloneRoomState(delta.After)
	if err := next.Validate(); err != nil {
		return State{}, err
	}
	return next, nil
}
