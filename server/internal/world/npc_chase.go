package world

import (
	"fmt"
	"reflect"
)

const (
	// Monster flag numbers are the stable values from src/mtype.h.  Keep the
	// chase reducer on the raw legacy body so importing a creature does not
	// require a second, partially authoritative flag model.
	npcPermanentFlag     = 0
	npcInvisibleFlag     = 2
	npcAggressiveFlag    = 6
	npcFollowFlag        = 9
	npcDMFollowFlag      = 46
	npcPlayerDMInvisible = 10
)

// NPCFollowerChaseInput describes the post-move context used by command2.c.
// The actor is already in its destination room; SourceRoomID is the room
// whose ordered NPC list is scanned.  Neither room contents nor player
// capabilities are accepted from the client.
type NPCFollowerChaseInput struct {
	ActorID      string
	SourceRoomID int16
	Now          int32
}

// NPCFollowerChaseMove is one successful MFOLLO chase.  Moves are emitted in
// source RoomState.NPCIDs order, matching command2.c's first_mon traversal.
// PermanentOrigin is copied into the proposal so Apply can reject a stale or
// identity-substituted candidate without matching by display name.
type NPCFollowerChaseMove struct {
	NPCID             string
	SourceRoomID      int16
	DestinationRoomID int16
	// Body is the immutable identity snapshot used to reject applying a plan
	// against a newer NPC revision.  It is not a second persisted owner.
	Body                LegacyMonster
	PermanentOrigin     *NPCPermanentOrigin
	PermanentTimerWrite bool
}

// NPCFollowerChaseProposal is an isolated, deterministic replacement plan.
// It is intentionally separate from TransferProposal: command2's player
// transfer must be applied before this source-room scan, while the caller
// still persists both as one command candidate.
type NPCFollowerChaseProposal struct {
	ActorID           string
	SourceRoomID      int16
	DestinationRoomID int16
	Now               int32
	Moves             []NPCFollowerChaseMove
}

// PlanNPCFollowerChase ports the NPC-following block immediately after a
// successful command2 move (command2.c:589-624).  The first enemy entry is
// authoritative and targets the canonical player ID; no display-name lookup
// is performed.  A nil Enemies slice is unresolved legacy state, not peace,
// and therefore aborts the whole plan.
func (s State) PlanNPCFollowerChase(in NPCFollowerChaseInput, roll func(int, int) int) (NPCFollowerChaseProposal, error) {
	if err := s.Validate(); err != nil {
		return NPCFollowerChaseProposal{}, err
	}
	if in.ActorID == "" {
		return NPCFollowerChaseProposal{}, fmt.Errorf("missing chase actor")
	}
	actor, ok := s.Players[in.ActorID]
	if !ok || !actor.Online {
		return NPCFollowerChaseProposal{}, fmt.Errorf("missing online chase actor")
	}
	destinationRoomID := actor.Body.RoomID
	if destinationRoomID == in.SourceRoomID {
		return NPCFollowerChaseProposal{}, fmt.Errorf("chase source equals destination")
	}
	source, ok := s.Rooms[in.SourceRoomID]
	if !ok {
		return NPCFollowerChaseProposal{}, fmt.Errorf("chase source room absent")
	}
	if _, ok := s.Rooms[destinationRoomID]; !ok {
		return NPCFollowerChaseProposal{}, fmt.Errorf("chase destination room absent")
	}
	if !containsString(s.Rooms[destinationRoomID].PlayerIDs, in.ActorID) || containsString(source.PlayerIDs, in.ActorID) {
		return NPCFollowerChaseProposal{}, fmt.Errorf("chase actor membership is not post-move")
	}
	if s.NPCs == nil {
		return NPCFollowerChaseProposal{}, fmt.Errorf("canonical NPC state required")
	}

	proposal := NPCFollowerChaseProposal{
		ActorID:           in.ActorID,
		SourceRoomID:      in.SourceRoomID,
		DestinationRoomID: destinationRoomID,
		Now:               in.Now,
	}
	seenOrigins := map[NPCPermanentOrigin]bool{}
	playerDexterity := int(int8(actor.Body.Stats[1]))
	playerInvisible := flag(actor.Body.Flags[:], npcInvisibleFlag)
	playerDMInvisible := flag(actor.Body.Flags[:], npcPlayerDMInvisible)

	for _, npcID := range source.NPCIDs {
		npc, exists := s.NPCs[npcID]
		if npcID == "" || !exists || npc.Body.RoomID != in.SourceRoomID || npc.Body.Type != 1 {
			return NPCFollowerChaseProposal{}, fmt.Errorf("unknown source NPC identity %q", npcID)
		}
		if npc.Enemies == nil {
			return NPCFollowerChaseProposal{}, fmt.Errorf("unresolved enemy relation for NPC %q", npcID)
		}
		if len(npc.Enemies) == 0 {
			continue
		}

		// command2.c intentionally applies this visibility gate only when the
		// NPC is not MFOLLO or is MDMFOL.  Thus a plain non-MFOLLO NPC can still
		// chase a visible player, matching the legacy predicate exactly.
		if (!flag(npc.Body.Flags[:], npcFollowFlag) || flag(npc.Body.Flags[:], npcDMFollowFlag)) &&
			((!flag(npc.Body.Flags[:], 21) && playerInvisible) || playerDMInvisible) {
			continue
		}
		if npc.Enemies[0].Target != (EntityRef{Kind: "player", ID: in.ActorID}) {
			continue
		}

		origin := cloneNPCPermanentOrigin(npc.PermanentOrigin)
		if flag(npc.Body.Flags[:], npcPermanentFlag) {
			if origin == nil || origin.RoomID != in.SourceRoomID || int(origin.Slot) >= len(source.Resource.PermanentMonsters) {
				return NPCFollowerChaseProposal{}, fmt.Errorf("permanent NPC %q has unknown origin", npcID)
			}
			key := *origin
			if seenOrigins[key] {
				return NPCFollowerChaseProposal{}, fmt.Errorf("duplicate permanent origin for NPC %q", npcID)
			}
			seenOrigins[key] = true
			timer := source.Resource.PermanentMonsters[origin.Slot]
			if timer.Misc == 0 {
				return NPCFollowerChaseProposal{}, fmt.Errorf("permanent NPC %q has empty origin slot", npcID)
			}
		}

		threshold := 15 - playerDexterity + int(int8(npc.Body.Stats[1]))
		value, err := randomIn(roll, 1, 50)
		if err != nil {
			return NPCFollowerChaseProposal{}, err
		}
		if value > threshold {
			continue
		}
		move := NPCFollowerChaseMove{
			NPCID:             npcID,
			SourceRoomID:      in.SourceRoomID,
			DestinationRoomID: destinationRoomID,
			Body:              cloneNPCBody(npc.Body),
			PermanentOrigin:   origin,
		}
		if origin != nil {
			timer := source.Resource.PermanentMonsters[origin.Slot]
			move.PermanentTimerWrite = int64(timer.LastTime)+int64(timer.Interval) <= int64(in.Now)
		}
		proposal.Moves = append(proposal.Moves, move)
	}

	// A nil active list means the legacy first_active order is unresolved.  Do
	// not silently move an NPC and lose the add_active ordering that C would
	// have persisted; a no-op plan with no chase remains valid.
	if len(proposal.Moves) != 0 && s.ActiveNPCIDs == nil {
		return NPCFollowerChaseProposal{}, fmt.Errorf("active NPC order unresolved")
	}
	return proposal, nil
}

// ApplyNPCFollowerChase applies one previously planned candidate to an
// authoritative snapshot.  All validation occurs before the caller receives
// a result; any failure returns State{} and never mutates the input snapshot.
func (s State) ApplyNPCFollowerChase(proposal NPCFollowerChaseProposal) (State, error) {
	if err := s.Validate(); err != nil {
		return State{}, err
	}
	if proposal.ActorID == "" || proposal.SourceRoomID == proposal.DestinationRoomID {
		return State{}, fmt.Errorf("invalid chase proposal rooms or actor")
	}
	actor, ok := s.Players[proposal.ActorID]
	if !ok || !actor.Online || actor.Body.RoomID != proposal.DestinationRoomID {
		return State{}, fmt.Errorf("chase proposal actor changed")
	}
	source, ok := s.Rooms[proposal.SourceRoomID]
	if !ok {
		return State{}, fmt.Errorf("chase proposal source absent")
	}
	if _, ok := s.Rooms[proposal.DestinationRoomID]; !ok {
		return State{}, fmt.Errorf("chase proposal destination absent")
	}
	if !containsString(s.Rooms[proposal.DestinationRoomID].PlayerIDs, proposal.ActorID) || containsString(source.PlayerIDs, proposal.ActorID) {
		return State{}, fmt.Errorf("chase proposal actor membership changed")
	}
	if s.NPCs == nil {
		return State{}, fmt.Errorf("canonical NPC state required")
	}
	if len(proposal.Moves) != 0 && s.ActiveNPCIDs == nil {
		return State{}, fmt.Errorf("active NPC order unresolved")
	}

	indices := make(map[string]int, len(source.NPCIDs))
	for i, id := range source.NPCIDs {
		indices[id] = i
	}
	lastIndex := -1
	seen := make(map[string]bool, len(proposal.Moves))
	for _, move := range proposal.Moves {
		index, exists := indices[move.NPCID]
		if !exists || seen[move.NPCID] || index <= lastIndex {
			return State{}, fmt.Errorf("chase proposal NPC order changed")
		}
		lastIndex = index
		seen[move.NPCID] = true
		if move.SourceRoomID != proposal.SourceRoomID || move.DestinationRoomID != proposal.DestinationRoomID {
			return State{}, fmt.Errorf("chase proposal NPC room changed")
		}
		npc, exists := s.NPCs[move.NPCID]
		if !exists || npc.Body.RoomID != proposal.SourceRoomID || npc.Body.Type != 1 || npc.Enemies == nil {
			return State{}, fmt.Errorf("chase proposal NPC identity changed")
		}
		if !reflect.DeepEqual(npc.Body, move.Body) {
			return State{}, fmt.Errorf("chase proposal NPC body changed")
		}
		if !sameNPCPermanentOrigin(npc.PermanentOrigin, move.PermanentOrigin) {
			return State{}, fmt.Errorf("chase proposal NPC origin changed")
		}
		permanent := flag(npc.Body.Flags[:], npcPermanentFlag)
		if permanent != (move.PermanentOrigin != nil) {
			return State{}, fmt.Errorf("chase proposal NPC permanence changed")
		}
		if move.PermanentOrigin != nil {
			origin := move.PermanentOrigin
			if origin.RoomID != proposal.SourceRoomID || int(origin.Slot) >= len(source.Resource.PermanentMonsters) {
				return State{}, fmt.Errorf("chase proposal permanent origin invalid")
			}
			timer := source.Resource.PermanentMonsters[origin.Slot]
			if timer.Misc == 0 {
				return State{}, fmt.Errorf("chase proposal permanent slot empty")
			}
			due := int64(timer.LastTime)+int64(timer.Interval) <= int64(proposal.Now)
			if due != move.PermanentTimerWrite {
				return State{}, fmt.Errorf("chase proposal permanent timer changed")
			}
		}
	}

	next := s.clone()
	for _, move := range proposal.Moves {
		npc := next.NPCs[move.NPCID]
		if move.PermanentOrigin != nil && move.PermanentTimerWrite {
			origin := move.PermanentOrigin
			sourceRoom := next.Rooms[proposal.SourceRoomID]
			timer := sourceRoom.Resource.PermanentMonsters[origin.Slot]
			timer.LastTime = proposal.Now
			sourceRoom.Resource.PermanentMonsters[origin.Slot] = timer
			next.Rooms[proposal.SourceRoomID] = sourceRoom
		}

		sourceRoom := next.Rooms[proposal.SourceRoomID]
		sourceRoom.NPCIDs, _ = removeString(sourceRoom.NPCIDs, move.NPCID)
		next.Rooms[proposal.SourceRoomID] = sourceRoom

		npc.Body.RoomID = proposal.DestinationRoomID
		npc.Body.Flags[0] &^= 1 << (npcPermanentFlag % 8)
		next.NPCs[move.NPCID] = npc

		destinationRoom := next.Rooms[proposal.DestinationRoomID]
		insertAt := len(destinationRoom.NPCIDs)
		for i, id := range destinationRoom.NPCIDs {
			other, exists := next.NPCs[id]
			if !exists {
				return State{}, fmt.Errorf("destination NPC identity disappeared")
			}
			if other.Body.Name > npc.Body.Name {
				insertAt = i
				break
			}
		}
		destinationRoom.NPCIDs = append(destinationRoom.NPCIDs, "")
		copy(destinationRoom.NPCIDs[insertAt+1:], destinationRoom.NPCIDs[insertAt:])
		destinationRoom.NPCIDs[insertAt] = move.NPCID
		next.Rooms[proposal.DestinationRoomID] = destinationRoom

		next.prependActiveNPC(move.NPCID)
	}
	if err := next.Validate(); err != nil {
		return State{}, err
	}
	return next, nil
}

func cloneNPCBody(body LegacyMonster) LegacyMonster {
	body.Inventory = cloneObjects(body.Inventory)
	return body
}

func (s *State) prependActiveNPC(id string) {
	ordered := make([]string, 1, len(s.ActiveNPCIDs)+1)
	ordered[0] = id
	for _, old := range s.ActiveNPCIDs {
		if old != id {
			ordered = append(ordered, old)
		}
	}
	s.ActiveNPCIDs = ordered
}

func cloneNPCPermanentOrigin(origin *NPCPermanentOrigin) *NPCPermanentOrigin {
	if origin == nil {
		return nil
	}
	copy := *origin
	return &copy
}

func sameNPCPermanentOrigin(a, b *NPCPermanentOrigin) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
