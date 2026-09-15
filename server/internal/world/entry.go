package world

import "fmt"

type RoomDeparture struct {
	Players            []RoomPlayerView
	DeactivateMonsters bool
}

// PlanRoomDeparture ports del_ply_rom's membership and last-occupant signal.
// Apply together with destination entry and actor location, never separately.
func PlanRoomDeparture(players []RoomPlayerView, id string) (RoomDeparture, error) {
	if id == "" {
		return RoomDeparture{}, fmt.Errorf("missing departure identity")
	}
	seen := map[string]bool{}
	at := -1
	for i, p := range players {
		if p.ID == "" || seen[p.ID] {
			return RoomDeparture{}, fmt.Errorf("invalid room identities")
		}
		seen[p.ID] = true
		if p.ID == id {
			at = i
		}
	}
	if at < 0 {
		return RoomDeparture{}, fmt.Errorf("departing actor absent from room")
	}
	out := append([]RoomPlayerView(nil), players[:at]...)
	out = append(out, players[at+1:]...)
	return RoomDeparture{Players: out, DeactivateMonsters: len(out) == 0}, nil
}

type RoomEntry struct {
	Room                                  LegacyRoom
	Players                               []RoomPlayerView
	Scene                                 string
	AnnounceArrival, EnsureMonstersActive bool
}

// PlanRoomEntry composes add_ply_rom's non-combat scene and list changes. It
// assumes destination admission was checked in the same authoritative snapshot.
// Nothing is broadcast/committed here; callers publish only after durable state
// commit. EnsureMonstersActive requests whole-room activation only for the first
// occupant. C add_active removes then prepends each NPC, so repeating it for an
// already occupied room would reorder the global tick list. Newly spawned NPCs
// in an occupied room require separate, spawn-ordered activation events.
func PlanRoomEntry(room LegacyRoom, players []RoomPlayerView, entrant RoomPlayerView, view SceneOptions, catalog SpawnCatalog, now int32, roll func(int, int) int) (RoomEntry, error) {
	return planRoomEntry(room, players, entrant, view, func(room LegacyRoom) (LegacyRoom, error) {
		return RefreshRoomResources(room, catalog, now, roll)
	})
}

func planRoomEntry(room LegacyRoom, players []RoomPlayerView, entrant RoomPlayerView, view SceneOptions, refresh func(LegacyRoom) (LegacyRoom, error)) (RoomEntry, error) {
	if entrant.ID == "" {
		return RoomEntry{}, fmt.Errorf("missing entrant identity")
	}
	seen := map[string]bool{}
	for _, p := range players {
		if p.ID == "" || p.ID == entrant.ID || seen[p.ID] {
			return RoomEntry{}, fmt.Errorf("duplicate or missing room identity")
		}
		seen[p.ID] = true
	}
	if room.BeenHere == 2147483647 {
		return RoomEntry{}, fmt.Errorf("legacy visit counter overflow")
	}
	refreshed, err := refresh(room)
	if err != nil {
		return RoomEntry{}, err
	}
	refreshed.BeenHere++
	view.ViewerID = entrant.ID
	scene := RenderRoomScene(refreshed, players, view)
	updated := append([]RoomPlayerView(nil), players...)
	at := len(updated)
	for i, p := range updated {
		if p.Name > entrant.Name {
			at = i
			break
		}
	}
	updated = append(updated, RoomPlayerView{})
	copy(updated[at+1:], updated[at:])
	updated[at] = entrant
	return RoomEntry{Room: refreshed, Players: updated, Scene: scene, AnnounceArrival: !flag(entrant.Flags[:], 10) && !flag(entrant.Flags[:], 1), EnsureMonstersActive: len(players) == 0}, nil
}
