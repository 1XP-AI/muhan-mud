package world

import "fmt"

// moveMixedFollowerTree replays the single C first_fol chain when the import
// has preserved its player/NPC interleaving. Player followers recurse through
// their own chain immediately; MDMFOL NPC entries move at the exact position
// at which they occur. A failed later entry discards the complete candidate.
// The caller supplies the original source room because all top-level entries
// are tested against the room left by the leader, just as command6.c does.
func (s State) moveMixedFollowerTree(leaderID string, sourceRoomID int16, in TransferInput, catalog SpawnCatalog, roll func(int, int) int, allocate func() (string, error)) (State, []FollowerStepResult, []NPCMonsterFollowerMove, []EntityRef, error) {
	leader, ok := s.Players[leaderID]
	if !ok {
		return State{}, nil, nil, nil, fmt.Errorf("mixed follower leader absent")
	}
	next := s
	var results []FollowerStepResult
	var npcMoves []NPCMonsterFollowerMove
	var order []EntityRef
	for _, ref := range mixedFollowerRefs(leader) {
		order = append(order, ref)
		switch ref.Kind {
		case "player":
			follower, exists := next.Players[ref.ID]
			if !exists || !follower.Online {
				return State{}, nil, nil, nil, fmt.Errorf("mixed player follower absent or offline")
			}
			// C's go() checks the follower's current room before recursing. A
			// previous capacity denial therefore leaves that edge intact but does
			// not teleport the follower on a later leader move.
			if follower.Body.RoomID != sourceRoomID {
				continue
			}
			leaderState, exists := next.Players[leaderID]
			if !exists {
				return State{}, nil, nil, nil, fmt.Errorf("mixed follower leader absent after step")
			}
			destination, err := next.ProjectRoom(leaderState.Body.RoomID)
			if err != nil {
				return State{}, nil, nil, nil, err
			}
			childInput := in
			childInput.ActorID = ref.ID
			childInput.Movement.Destination = &destination
			childInput.View.ViewerID = ref.ID
			childState, child, err := next.directionalStepSingle(childInput, catalog, roll, allocate, false)
			if err != nil {
				return State{}, nil, nil, nil, err
			}
			next = childState
			childResult := FollowerStepResult{ActorID: ref.ID, Transfer: child.Transfer, Death: child.Death}
			if child.Transfer.Movement.Moved && child.Death == nil {
				var childNPCMoves []NPCMonsterFollowerMove
				var childOrder []EntityRef
				next, childResult.Followers, childNPCMoves, childOrder, err = next.moveMixedFollowerTree(ref.ID, sourceRoomID, childInput, catalog, roll, allocate)
				if err != nil {
					return State{}, nil, nil, nil, err
				}
				childForTrap := DirectionalStepResult{
					Transfer:      childResult.Transfer,
					Death:         childResult.Death,
					Followers:     childResult.Followers,
					FollowerOrder: childOrder,
				}
				if err := next.attachArrivalTrap(ref.ID, &childForTrap.Transfer, roll); err != nil {
					return State{}, nil, nil, nil, err
				}
				next, err = applyArrivalStep(next, ref.ID, &childForTrap, childInput, catalog, roll, allocate)
				if err != nil {
					return State{}, nil, nil, nil, err
				}
				childResult.Transfer, childResult.Death = childForTrap.Transfer, childForTrap.Death
				npcMoves = append(npcMoves, childNPCMoves...)
				order = append(order, childOrder...)
			}
			results = append(results, childResult)
		case "npc":
			if next.NPCs == nil {
				return State{}, nil, nil, nil, fmt.Errorf("mixed NPC follower requires canonical NPC state")
			}
			npc, exists := next.NPCs[ref.ID]
			if !exists || npc.FollowingPlayerID != leaderID || npc.Body.Type != 1 {
				return State{}, nil, nil, nil, fmt.Errorf("mixed NPC follower identity changed")
			}
			if npc.Body.RoomID != sourceRoomID || !flag(npc.Body.Flags[:], npcDMFollowFlag) {
				continue
			}
			leaderState, exists := next.Players[leaderID]
			if !exists {
				return State{}, nil, nil, nil, fmt.Errorf("mixed NPC follower leader absent")
			}
			move := NPCMonsterFollowerMove{
				NPCID:             ref.ID,
				SourceRoomID:      sourceRoomID,
				DestinationRoomID: leaderState.Body.RoomID,
				Body:              cloneNPCBody(npc.Body),
			}
			proposal := NPCMonsterFollowerProposal{
				ActorID:           leaderID,
				SourceRoomID:      sourceRoomID,
				DestinationRoomID: leaderState.Body.RoomID,
				Now:               int32(in.Movement.Now),
				Moves:             []NPCMonsterFollowerMove{move},
			}
			var err error
			next, err = next.ApplyNPCMonsterFollowers(proposal)
			if err != nil {
				return State{}, nil, nil, nil, err
			}
			npcMoves = append(npcMoves, move)
		default:
			return State{}, nil, nil, nil, fmt.Errorf("unknown mixed follower kind")
		}
	}
	return next, results, npcMoves, order, nil
}
