package world

import (
	"fmt"
	"reflect"
)

// NPCResourceTickSpawn is one identity allocation made by an NPC resource
// tick.  Origin is the permanent-monster slot that owns the identity; it is
// deliberately carried in the proposal so Apply never has to infer ownership
// from a display name.
type NPCResourceTickSpawn struct {
	ID         string
	TemplateID int16
	Origin     NPCPermanentOrigin
	Body       LegacyMonster
}

// NPCResourceTickProposal is an isolated, room-scoped candidate for due
// permanent NPC materialization. Before is a complete snapshot rather than a
// room-only revision: an origin can be alive in another room after chasing or
// an alarm relocation, so applying against a changed global NPC map would be
// unsafe. The two ordered slices are the exact post-apply identities that the
// caller can persist alongside the candidate.
type NPCResourceTickProposal struct {
	RoomID            int16
	Now               int32
	Before            State
	Spawns            []NPCResourceTickSpawn
	AfterRoomNPCIDs   []string
	AfterActiveNPCIDs []string
}

// NPCResourceTickInput is the input-shaped form for callers that prefer to
// keep scheduler arguments together. PlanNPCResourceTick accepts the same
// values as positional arguments; this type is only a small adapter for a
// future scheduler and has no transport or clock behavior.
type NPCResourceTickInput struct {
	RoomID   int16
	Now      int32
	Catalog  SpawnCatalog
	Roll     func(int, int) int
	Allocate func() (string, error)
}

// PlanNPCResourceTick plans due permanent NPC respawns in one canonical room.
// It is intentionally separate from RefreshCanonicalRoom: that legacy helper
// counts permanent monsters by name, while canonical runtime state owns each
// instance by NPC ID and NPCPermanentOrigin. No name is consulted to decide
// whether a slot is occupied.
//
// The plan consumes the injected catalog/RNG/allocator while constructing an
// isolated candidate. A failure returns a zero proposal and never publishes a
// partially allocated state. External allocators and RNGs cannot themselves
// be rewound, so callers must retain the command seed/ID when retrying.
func (s State) PlanNPCResourceTick(roomID int16, catalog SpawnCatalog, now int32, roll func(int, int) int, allocate func() (string, error)) (NPCResourceTickProposal, error) {
	if err := s.Validate(); err != nil {
		return NPCResourceTickProposal{}, err
	}
	if s.NPCs == nil {
		return NPCResourceTickProposal{}, fmt.Errorf("NPC resource tick requires canonical NPC state")
	}
	room, ok := s.Rooms[roomID]
	if !ok {
		return NPCResourceTickProposal{}, fmt.Errorf("NPC resource tick room absent")
	}
	if room.Items == nil || len(room.Resource.Objects) != 0 {
		return NPCResourceTickProposal{}, fmt.Errorf("NPC resource tick requires canonical room")
	}

	occupied, err := permanentOriginOccupancy(s)
	if err != nil {
		return NPCResourceTickProposal{}, err
	}

	// Determine all missing identities before calling any external dependency.
	// This makes an unresolved active order or allocator fail closed before
	// catalog/RNG work, and lets a due slot already represented by an exact
	// origin remain a no-op without loading its template.
	missing := make([]int, 0, len(room.Resource.PermanentMonsters))
	for slot, timer := range room.Resource.PermanentMonsters {
		if !npcResourceTimerDue(timer, now) {
			continue
		}
		origin := NPCPermanentOrigin{RoomID: roomID, Slot: uint8(slot)}
		if _, exists := occupied[origin]; !exists {
			missing = append(missing, slot)
		}
	}

	proposal := NPCResourceTickProposal{
		RoomID:            roomID,
		Now:               now,
		Before:            s.clone(),
		AfterRoomNPCIDs:   cloneNPCResourceIDs(room.NPCIDs),
		AfterActiveNPCIDs: cloneNPCResourceIDs(s.ActiveNPCIDs),
	}
	if len(missing) == 0 {
		return proposal, nil
	}
	if len(room.PlayerIDs) != 0 && s.ActiveNPCIDs == nil {
		return NPCResourceTickProposal{}, fmt.Errorf("NPC resource tick active order unresolved")
	}
	if catalog == nil {
		return NPCResourceTickProposal{}, fmt.Errorf("NPC resource tick requires spawn catalog")
	}
	if allocate == nil {
		return NPCResourceTickProposal{}, fmt.Errorf("NPC resource tick requires ID allocator")
	}

	// Keep a local identity map for deterministic insertion planning. The
	// authoritative State is never mutated during this phase.
	staged := make(map[string]NPCState, len(missing))
	roomIDs := cloneNPCResourceIDs(room.NPCIDs)
	seenIDs := make(map[string]bool, len(s.NPCs)+len(missing))
	for id := range s.NPCs {
		seenIDs[id] = true
	}

	for _, slot := range missing {
		timer := room.Resource.PermanentMonsters[slot]
		template, err := catalog.Monster(timer.Misc)
		if err != nil {
			return NPCResourceTickProposal{}, fmt.Errorf("NPC resource tick template %d: %w", timer.Misc, err)
		}
		if template.Type != 1 || template.Name == "" {
			return NPCResourceTickProposal{}, fmt.Errorf("invalid permanent NPC template %d", timer.Misc)
		}
		body, err := SpawnPermanentMonster(template, now, catalog.Object, roll)
		if err != nil {
			return NPCResourceTickProposal{}, fmt.Errorf("NPC resource tick template %d spawn: %w", timer.Misc, err)
		}
		body.RoomID = roomID
		if body.Type != 1 || body.Name == "" {
			return NPCResourceTickProposal{}, fmt.Errorf("invalid spawned permanent NPC template %d", timer.Misc)
		}
		id, err := allocate()
		if err != nil {
			return NPCResourceTickProposal{}, fmt.Errorf("NPC resource tick allocate slot %d: %w", slot, err)
		}
		if id == "" || seenIDs[id] {
			return NPCResourceTickProposal{}, fmt.Errorf("duplicate or empty NPC resource tick identity %q", id)
		}
		seenIDs[id] = true
		origin := NPCPermanentOrigin{RoomID: roomID, Slot: uint8(slot)}
		spawn := NPCResourceTickSpawn{
			ID:         id,
			TemplateID: timer.Misc,
			Origin:     origin,
			Body:       cloneNPCBody(body),
		}
		proposal.Spawns = append(proposal.Spawns, spawn)
		staged[id] = NPCState{Body: cloneNPCBody(body), Enemies: []NPCEnemy{}, PermanentOrigin: cloneNPCPermanentOrigin(&origin)}
		var insertErr error
		roomIDs, insertErr = insertNPCResourceID(roomIDs, id, s.NPCs, staged)
		if insertErr != nil {
			return NPCResourceTickProposal{}, insertErr
		}
	}
	proposal.AfterRoomNPCIDs = roomIDs
	if len(room.PlayerIDs) != 0 {
		active := cloneNPCResourceIDs(s.ActiveNPCIDs)
		// add_active is called in successful spawn order. Each call prepends,
		// therefore the final active prefix is the reverse of slot order.
		for _, spawn := range proposal.Spawns {
			active = prependNPCResourceID(active, spawn.ID)
		}
		proposal.AfterActiveNPCIDs = active
	}
	return proposal, nil
}

// PlanNPCResourceTickWithInput is an argument-bundled adapter for future
// world schedulers. It remains a pure world operation and does not run a
// goroutine or touch transport/session state.
func (s State) PlanNPCResourceTickWithInput(in NPCResourceTickInput) (NPCResourceTickProposal, error) {
	return s.PlanNPCResourceTick(in.RoomID, in.Catalog, in.Now, in.Roll, in.Allocate)
}

// ApplyNPCResourceTick applies a previously planned candidate atomically. It
// checks the complete source snapshot, all origin/slot identities, and both
// ordered identity outputs before cloning the state. Any tampering or stale
// snapshot returns State{} and cannot leak a partial NPC or active-list entry.
func (s State) ApplyNPCResourceTick(proposal NPCResourceTickProposal) (State, error) {
	if err := s.Validate(); err != nil {
		return State{}, err
	}
	if proposal.Before.Version == 0 {
		return State{}, fmt.Errorf("empty NPC resource tick proposal")
	}
	if !reflect.DeepEqual(s, proposal.Before) {
		return State{}, fmt.Errorf("stale NPC resource tick")
	}
	room, ok := s.Rooms[proposal.RoomID]
	if !ok {
		return State{}, fmt.Errorf("NPC resource tick room absent")
	}
	if s.NPCs == nil || room.Items == nil || len(room.Resource.Objects) != 0 {
		return State{}, fmt.Errorf("NPC resource tick canonical state unavailable")
	}
	if !reflect.DeepEqual(room.NPCIDs, proposal.Before.Rooms[proposal.RoomID].NPCIDs) {
		return State{}, fmt.Errorf("NPC resource tick source room changed")
	}
	if !reflect.DeepEqual(s.ActiveNPCIDs, proposal.Before.ActiveNPCIDs) {
		return State{}, fmt.Errorf("NPC resource tick source active order changed")
	}

	occupied, err := permanentOriginOccupancy(s)
	if err != nil {
		return State{}, err
	}
	missing := make([]int, 0, len(room.Resource.PermanentMonsters))
	for slot, timer := range room.Resource.PermanentMonsters {
		if !npcResourceTimerDue(timer, proposal.Now) {
			continue
		}
		origin := NPCPermanentOrigin{RoomID: proposal.RoomID, Slot: uint8(slot)}
		if _, exists := occupied[origin]; !exists {
			missing = append(missing, slot)
		}
	}
	if len(missing) != len(proposal.Spawns) {
		return State{}, fmt.Errorf("NPC resource tick spawn set changed")
	}
	// Validate and bind every spawn to the exact missing slot in ascending
	// order. This rejects a proposal that swaps two same-name templates or uses
	// an ID whose current owner was changed, without any name lookup.
	seenOrigins := make(map[NPCPermanentOrigin]bool, len(proposal.Spawns))
	seenIDs := make(map[string]bool, len(s.NPCs)+len(proposal.Spawns))
	for id := range s.NPCs {
		seenIDs[id] = true
	}
	for i, spawn := range proposal.Spawns {
		slot := missing[i]
		wantOrigin := NPCPermanentOrigin{RoomID: proposal.RoomID, Slot: uint8(slot)}
		if spawn.ID == "" || seenIDs[spawn.ID] || spawn.Origin != wantOrigin || spawn.TemplateID != room.Resource.PermanentMonsters[slot].Misc || seenOrigins[spawn.Origin] {
			return State{}, fmt.Errorf("NPC resource tick spawn identity mismatch")
		}
		if spawn.Body.Type != 1 || spawn.Body.Name == "" || spawn.Body.RoomID != proposal.RoomID || !flag(spawn.Body.Flags[:], npcPermanentFlag) {
			return State{}, fmt.Errorf("NPC resource tick spawn body mismatch")
		}
		if spawn.Body.Timers[8].LastTime != proposal.Now || spawn.Body.Timers[8].Interval != 60 {
			return State{}, fmt.Errorf("NPC resource tick spawn timer mismatch")
		}
		seenIDs[spawn.ID] = true
		seenOrigins[spawn.Origin] = true
	}

	next := s.clone()
	nextRoom := next.Rooms[proposal.RoomID]
	for _, spawn := range proposal.Spawns {
		origin := spawn.Origin
		next.NPCs[spawn.ID] = NPCState{
			Body:            cloneNPCBody(spawn.Body),
			Enemies:         []NPCEnemy{},
			PermanentOrigin: cloneNPCPermanentOrigin(&origin),
		}
		var insertErr error
		nextRoom.NPCIDs, insertErr = insertNPCResourceID(nextRoom.NPCIDs, spawn.ID, next.NPCs, nil)
		if insertErr != nil {
			return State{}, insertErr
		}
	}
	next.Rooms[proposal.RoomID] = nextRoom
	if len(room.PlayerIDs) != 0 {
		if next.ActiveNPCIDs == nil && len(proposal.Spawns) != 0 {
			return State{}, fmt.Errorf("NPC resource tick active order unresolved")
		}
		for _, spawn := range proposal.Spawns {
			next.prependActiveNPC(spawn.ID)
		}
	}
	if !reflect.DeepEqual(next.Rooms[proposal.RoomID].NPCIDs, proposal.AfterRoomNPCIDs) || !reflect.DeepEqual(next.ActiveNPCIDs, proposal.AfterActiveNPCIDs) {
		return State{}, fmt.Errorf("NPC resource tick ordered result mismatch")
	}
	if err := next.Validate(); err != nil {
		return State{}, err
	}
	return next, nil
}

// ApplyNPCResourceTickProposal is a descriptive alias for callers that name
// the value a proposal rather than a tick. It intentionally delegates to the
// single checked implementation above.
func (s State) ApplyNPCResourceTickProposal(proposal NPCResourceTickProposal) (State, error) {
	return s.ApplyNPCResourceTick(proposal)
}

func npcResourceTimerDue(timer LegacyTimer, now int32) bool {
	return timer.Misc != 0 && int64(timer.LastTime)+int64(timer.Interval) <= int64(now)
}

// permanentOriginOccupancy is identity-only. A permanent bit without an
// origin is unresolved legacy state; guessing its slot by name would permit a
// duplicate instance when two templates share a display name.
func permanentOriginOccupancy(s State) (map[NPCPermanentOrigin]string, error) {
	occupied := make(map[NPCPermanentOrigin]string)
	for id, npc := range s.NPCs {
		if flag(npc.Body.Flags[:], npcPermanentFlag) && npc.PermanentOrigin == nil {
			return nil, fmt.Errorf("permanent NPC %q origin unresolved", id)
		}
		if npc.PermanentOrigin == nil {
			continue
		}
		origin := *npc.PermanentOrigin
		room, ok := s.Rooms[origin.RoomID]
		if !ok || int(origin.Slot) >= len(room.Resource.PermanentMonsters) {
			return nil, fmt.Errorf("NPC %q has invalid permanent origin", id)
		}
		if room.Resource.PermanentMonsters[origin.Slot].Misc == 0 {
			return nil, fmt.Errorf("NPC %q has empty permanent origin slot", id)
		}
		if old, exists := occupied[origin]; exists {
			return nil, fmt.Errorf("duplicate permanent origin %v for NPCs %q and %q", origin, old, id)
		}
		occupied[origin] = id
	}
	return occupied, nil
}

func cloneNPCResourceIDs(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func prependNPCResourceID(ids []string, id string) []string {
	out := make([]string, 1, len(ids)+1)
	out[0] = id
	for _, old := range ids {
		if old != id {
			out = append(out, old)
		}
	}
	return out
}

// insertNPCResourceID follows add_crt_rom's deterministic room order: names
// sort ascending, while equal display names retain the prior ordered identity
// sequence. Name comparison here is presentation order only; slot occupancy
// and ownership never use it.
func insertNPCResourceID(ids []string, id string, existing, staged map[string]NPCState) ([]string, error) {
	lookup := func(candidate string) (NPCState, bool) {
		if staged != nil {
			if npc, ok := staged[candidate]; ok {
				return npc, true
			}
		}
		npc, ok := existing[candidate]
		return npc, ok
	}
	newNPC, ok := lookup(id)
	if !ok {
		return nil, fmt.Errorf("NPC resource tick identity %q absent during room insertion", id)
	}
	at := len(ids)
	for i, existingID := range ids {
		npc, ok := lookup(existingID)
		if !ok || npc.Body.Type != 1 {
			return nil, fmt.Errorf("NPC resource tick room identity %q absent", existingID)
		}
		if npc.Body.Name > newNPC.Body.Name {
			at = i
			break
		}
	}
	out := make([]string, len(ids)+1)
	copy(out, ids[:at])
	out[at] = id
	copy(out[at+1:], ids[at:])
	return out, nil
}
