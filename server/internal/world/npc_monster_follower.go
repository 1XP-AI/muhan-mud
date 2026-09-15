package world

import (
	"fmt"
	"reflect"
)

// NPCMonsterFollowerInput is the post-player-move context for command6's
// MDMFOL branch. The leader is already in the destination; SourceRoomID is
// captured before the player transfer.
type NPCMonsterFollowerInput struct {
	ActorID      string
	SourceRoomID int16
	Now          int32
}

type NPCMonsterFollowerMove struct {
	NPCID             string
	SourceRoomID      int16
	DestinationRoomID int16
	Body              LegacyMonster
}

type NPCMonsterFollowerProposal struct {
	ActorID           string
	SourceRoomID      int16
	DestinationRoomID int16
	Now               int32
	Moves             []NPCMonsterFollowerMove
}

// PlanNPCMonsterFollowers ports command6.c's first_fol monster branch. Only
// MDMFOL monsters attached to the actor move; ordinary MFOLLO/temporary NPCs
// remain for the separate hostile chase reducer. The actor's NPCFollowerIDs
// list preserves first_fol head order.
func (s State) PlanNPCMonsterFollowers(in NPCMonsterFollowerInput) (NPCMonsterFollowerProposal, error) {
	if err := s.Validate(); err != nil {
		return NPCMonsterFollowerProposal{}, err
	}
	actor, ok := s.Players[in.ActorID]
	if !ok || !actor.Online {
		return NPCMonsterFollowerProposal{}, fmt.Errorf("missing online monster-follower leader")
	}
	destinationID := actor.Body.RoomID
	if in.SourceRoomID == destinationID {
		return NPCMonsterFollowerProposal{}, fmt.Errorf("monster follower source equals destination")
	}
	source, ok := s.Rooms[in.SourceRoomID]
	if !ok {
		return NPCMonsterFollowerProposal{}, fmt.Errorf("monster follower source absent")
	}
	if _, ok := s.Rooms[destinationID]; !ok || !containsString(s.Rooms[destinationID].PlayerIDs, in.ActorID) || containsString(source.PlayerIDs, in.ActorID) {
		return NPCMonsterFollowerProposal{}, fmt.Errorf("monster follower leader membership is not post-move")
	}
	if s.NPCs == nil {
		return NPCMonsterFollowerProposal{}, fmt.Errorf("canonical NPC state required")
	}
	proposal := NPCMonsterFollowerProposal{ActorID: in.ActorID, SourceRoomID: in.SourceRoomID, DestinationRoomID: destinationID, Now: in.Now}
	for _, npcID := range actor.NPCFollowerIDs {
		npc, ok := s.NPCs[npcID]
		if !ok || npc.FollowingPlayerID != in.ActorID || npc.Body.Type != 1 {
			return NPCMonsterFollowerProposal{}, fmt.Errorf("invalid monster follower identity")
		}
		if npc.Body.RoomID != in.SourceRoomID || !flag(npc.Body.Flags[:], npcDMFollowFlag) {
			continue
		}
		proposal.Moves = append(proposal.Moves, NPCMonsterFollowerMove{
			NPCID:             npcID,
			SourceRoomID:      in.SourceRoomID,
			DestinationRoomID: destinationID,
			Body:              cloneNPCBody(npc.Body),
		})
	}
	if len(proposal.Moves) != 0 && s.ActiveNPCIDs == nil {
		return NPCMonsterFollowerProposal{}, fmt.Errorf("monster follower active order unresolved")
	}
	return proposal, nil
}

// ApplyNPCMonsterFollowers moves the planned MDMFOL identities atomically.
// Unlike command2's hostile chase, command6 does not call die_perm_crt before
// moving a permanent monster; only MPERMT is cleared after add_active.
func (s State) ApplyNPCMonsterFollowers(proposal NPCMonsterFollowerProposal) (State, error) {
	if err := s.Validate(); err != nil {
		return State{}, err
	}
	if proposal.ActorID == "" || proposal.SourceRoomID == proposal.DestinationRoomID {
		return State{}, fmt.Errorf("invalid monster follower proposal")
	}
	actor, ok := s.Players[proposal.ActorID]
	if !ok || !actor.Online || actor.Body.RoomID != proposal.DestinationRoomID {
		return State{}, fmt.Errorf("monster follower leader changed")
	}
	source, ok := s.Rooms[proposal.SourceRoomID]
	if !ok || !containsString(s.Rooms[proposal.DestinationRoomID].PlayerIDs, proposal.ActorID) || containsString(source.PlayerIDs, proposal.ActorID) {
		return State{}, fmt.Errorf("monster follower membership changed")
	}
	if s.NPCs == nil || (len(proposal.Moves) != 0 && s.ActiveNPCIDs == nil) {
		return State{}, fmt.Errorf("monster follower canonical state unavailable")
	}
	roomIndex := map[string]bool{}
	for _, id := range source.NPCIDs {
		roomIndex[id] = true
	}
	seen := map[string]bool{}
	for _, move := range proposal.Moves {
		if !roomIndex[move.NPCID] || seen[move.NPCID] || move.SourceRoomID != proposal.SourceRoomID || move.DestinationRoomID != proposal.DestinationRoomID {
			return State{}, fmt.Errorf("monster follower order or room changed")
		}
		npc, ok := s.NPCs[move.NPCID]
		if !ok || npc.Body.RoomID != proposal.SourceRoomID || npc.FollowingPlayerID != proposal.ActorID || !flag(npc.Body.Flags[:], npcDMFollowFlag) || !reflect.DeepEqual(npc.Body, move.Body) {
			return State{}, fmt.Errorf("monster follower identity changed")
		}
		seen[move.NPCID] = true
	}

	next := s.clone()
	for _, move := range proposal.Moves {
		npc := next.NPCs[move.NPCID]
		sourceRoom := next.Rooms[proposal.SourceRoomID]
		sourceRoom.NPCIDs, _ = removeString(sourceRoom.NPCIDs, move.NPCID)
		next.Rooms[proposal.SourceRoomID] = sourceRoom
		npc.Body.RoomID = proposal.DestinationRoomID
		npc.Body.Flags[npcPermanentFlag/8] &^= 1 << (npcPermanentFlag % 8)
		next.NPCs[move.NPCID] = npc
		destinationRoom := next.Rooms[proposal.DestinationRoomID]
		at := len(destinationRoom.NPCIDs)
		for i, id := range destinationRoom.NPCIDs {
			other, exists := next.NPCs[id]
			if !exists {
				return State{}, fmt.Errorf("monster follower destination NPC absent")
			}
			if other.Body.Name > npc.Body.Name {
				at = i
				break
			}
		}
		destinationRoom.NPCIDs = append(destinationRoom.NPCIDs, "")
		copy(destinationRoom.NPCIDs[at+1:], destinationRoom.NPCIDs[at:])
		destinationRoom.NPCIDs[at] = move.NPCID
		next.Rooms[proposal.DestinationRoomID] = destinationRoom
		next.prependActiveNPC(move.NPCID)
	}
	if err := next.Validate(); err != nil {
		return State{}, err
	}
	return next, nil
}
