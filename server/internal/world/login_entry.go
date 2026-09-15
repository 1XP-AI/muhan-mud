package world

import "fmt"

// EnterSavedPlayer ports the room-placement phase of init_ply. actorID must be
// resolved by credential verification and refer to a fully initialized player,
// not a creation draft. This is not full init_ply: family flags,
// item repairs/equipment and global login announcements remain pending.
// Persist the returned state before sending Entry.Scene or accepting commands.
func (s State) EnterSavedPlayer(actorID string, view SceneOptions, catalog SpawnCatalog, now int32, roll func(int, int) int) (State, RoomEntry, error) {
	return s.EnterSavedPlayerWithIDs(actorID, view, catalog, now, roll, nil)
}

// EnterSavedPlayerWithIDs accepts a command-owned allocator for newly spawned
// floor items. A nil allocator is valid when no new canonical items spawn.
func (s State) EnterSavedPlayerWithIDs(actorID string, view SceneOptions, catalog SpawnCatalog, now int32, roll func(int, int) int, allocate func() (string, error)) (State, RoomEntry, error) {
	if err := s.Validate(); err != nil {
		return State{}, RoomEntry{}, err
	}
	player, ok := s.Players[actorID]
	if !ok || player.Online {
		return State{}, RoomEntry{}, fmt.Errorf("missing or already online player")
	}
	room := s.Rooms[player.Body.RoomID].Resource
	views, err := s.RoomPlayers(room.ID)
	if err != nil {
		return State{}, RoomEntry{}, err
	}
	visible := 0
	for _, p := range views {
		if !flag(p.Flags[:], 10) {
			visible++
		}
	}
	full := (flag(room.Flags[:], 14) && visible > 0) || (flag(room.Flags[:], 15) && visible > 1) || (flag(room.Flags[:], 16) && visible > 2)
	if full || flag(room.Flags[:], 33) {
		fallback, ok := s.Rooms[1]
		if !ok {
			return State{}, RoomEntry{}, fmt.Errorf("login fallback room absent")
		}
		room = fallback.Resource
	}
	if flag(room.Flags[:], 40) && room.Special != int16(player.Body.Daily[8].Max) {
		fallback, ok := s.Rooms[1]
		if !ok {
			return State{}, RoomEntry{}, fmt.Errorf("login fallback room absent")
		}
		room = fallback.Resource
	}
	views, err = s.RoomPlayers(room.ID)
	if err != nil {
		return State{}, RoomEntry{}, err
	}
	actor := RoomPlayerView{ID: actorID, Name: player.Body.Name, Description: player.Body.Description, Flags: player.Body.Flags}
	var entry RoomEntry
	var canonical RoomState
	var npcDelta *npcEntryDelta
	if s.NPCs != nil {
		canonical, entry, npcDelta, err = planCanonicalNPCEntry(s.Rooms[room.ID], s.NPCs, views, actor, view, catalog, now, roll, allocate)
	} else if s.Rooms[room.ID].Items != nil {
		canonical, entry, err = PlanCanonicalRoomEntry(s.Rooms[room.ID], views, actor, view, catalog, now, roll, allocate)
	} else {
		entry, err = PlanRoomEntry(room, views, actor, view, catalog, now, roll)
	}
	if err != nil {
		return State{}, RoomEntry{}, err
	}
	next := s.clone()
	if err := next.installNPCSpawns(npcDelta); err != nil {
		return State{}, RoomEntry{}, err
	}
	player = next.Players[actorID]
	player.Body.Daily = LoginDaily(player.Body.Daily, player.Body.Level, player.Body.Class)
	player.Body.Timers, err = LoginTimers(player.Body.Timers, now)
	if err != nil {
		return State{}, RoomEntry{}, err
	}
	// ID-migrated equipment is already restored; never reimport it on login.
	// Nil Items remains the pre-migration placement-only path, not full admission.
	if player.Items != nil {
		stats, err := player.Items.CombatStats(player.Body)
		if err != nil {
			return State{}, RoomEntry{}, err
		}
		player.Body.Armor, player.Body.Thaco = byte(stats.Armor), byte(stats.Thaco)
	}
	player.Online = true
	player.Body.RoomID = room.ID
	next.Players[actorID] = player
	ids := make([]string, len(entry.Players))
	for i, p := range entry.Players {
		ids[i] = p.ID
	}
	if canonical.Items != nil || s.NPCs != nil {
		canonical.Resource = cloneRoom(canonical.Resource)
		canonical.PlayerIDs = ids
		next.Rooms[room.ID] = canonical
	} else {
		next.Rooms[room.ID] = RoomState{Resource: cloneRoom(entry.Room), PlayerIDs: ids}
	}
	next.activateEntryNPCs(room.ID, len(views) == 0, npcDelta)
	if err := next.Validate(); err != nil {
		return State{}, RoomEntry{}, err
	}
	if player.Items != nil {
		entry.Scene, err = next.CurrentScene(actorID, view.Hour)
		if err != nil {
			return State{}, RoomEntry{}, err
		}
	}
	return next, entry, nil
}
