package world

import "fmt"

// FollowerStepResult records one recursive C first_fol movement. Followers
// are kept as a tree because a follower may itself have followers; preserving
// that shape makes ordering and later response/broadcast work explicit.
type FollowerStepResult struct {
	ActorID   string
	Transfer  TransferProposal
	Death     *PlayerDeathResult
	Followers []FollowerStepResult
	// ArrivalTrapEvent is the follower-local projection committed after all
	// nested first_fol children, matching recursive C move(). Lethal follower
	// traps deliberately do not expose a new output event.
	ArrivalTrapEvent *ArrivalTrapEvent `json:"-"`
}

// FlattenFollowerArrivalTrapEvents returns follower trap projections in the
// exact recursive C order: descendants first, then their direct follower,
// followed by the next first_fol sibling. The values are receipt metadata and
// are copied so callers cannot mutate reducer-owned result trees.
func FlattenFollowerArrivalTrapEvents(results []FollowerStepResult) []ArrivalTrapEvent {
	var events []ArrivalTrapEvent
	for _, result := range results {
		events = append(events, FlattenFollowerArrivalTrapEvents(result.Followers)...)
		if result.ArrivalTrapEvent != nil {
			events = append(events, *result.ArrivalTrapEvent)
		}
	}
	return events
}

// Transfer is the pre-death movement event; State returned by DirectionalStep
// is the sole final candidate to persist. Never separately apply Transfer
// again.
type DirectionalStepResult struct {
	Transfer  TransferProposal
	Death     *PlayerDeathResult
	Followers []FollowerStepResult
	// FollowerOrder is the exact mixed first_fol traversal when the imported
	// snapshot retains player/NPC interleaving. Nil means the legacy import did
	// not resolve that interleaving and the category-specific reducers were used.
	FollowerOrder []EntityRef
	// NPCChase is the ordered command2 old-room MFOLLO result. It is kept in
	// the same reducer result as player movement so the caller persists one
	// receipt; nil means the actor did not complete a move or no canonical NPC
	// state was available for this legacy-compatible slice.
	NPCChase *NPCFollowerChaseProposal
	// NPCMonsterFollowers is the command6 MDMFOL first_fol movement result.
	NPCMonsterFollowers *NPCMonsterFollowerProposal
	Alarm               *ArrivalAlarmResult
	// ArrivalTrapEvent is set only for this command's leader. It captures the
	// committed trap room and source-composed text; follower trap results stay
	// in the recursive follower tree and are flattened by the command boundary.
	ArrivalTrapEvent *ArrivalTrapEvent `json:"-"`
}

// directionalStepSingle performs one actor's traversal/entry and optionally
// its arrival trap. It does not move followers; the caller controls the C
// order: leader commit, recursive followers, then leader trap.
func (s State) directionalStepSingle(in TransferInput, catalog SpawnCatalog, roll func(int, int) int, allocate func() (string, error), applyTrap bool) (State, DirectionalStepResult, error) {
	next, transfer, err := s.transferWithPlanner(in, catalog, roll, allocate, PlanDirectionalMovement)
	if err != nil {
		return State{}, DirectionalStepResult{}, err
	}
	result := DirectionalStepResult{Transfer: transfer}
	if transfer.Movement.Traversal.Dead {
		var death PlayerDeathResult
		next, death, err = next.PlanPlayerDeath(in.ActorID, in.ActorID, int32(in.Movement.Now), in.View, catalog, roll, allocate)
		if err != nil {
			return State{}, DirectionalStepResult{}, err
		}
		result.Death = &death
		return next, result, nil
	}
	if applyTrap {
		if err = next.attachArrivalTrap(in.ActorID, &result.Transfer, roll); err != nil {
			return State{}, DirectionalStepResult{}, err
		}
		next, err = applyArrivalStep(next, in.ActorID, &result, in, catalog, roll, allocate)
		if err != nil {
			return State{}, DirectionalStepResult{}, err
		}
	}
	return next, result, nil
}

func applyArrivalStep(s State, actorID string, result *DirectionalStepResult, in TransferInput, catalog SpawnCatalog, roll func(int, int) int, allocate func() (string, error)) (State, error) {
	next := s
	var trapEvent ArrivalTrapEvent
	hasTrapEvent := false
	if result.Transfer.ArrivalTrap != nil {
		var err error
		actor, ok := next.Players[actorID]
		if !ok {
			return State{}, fmt.Errorf("arrival trap actor absent before output")
		}
		trapRoomID := actor.Body.RoomID
		trapActorName := actor.Body.Name
		trapRoomRecipients := arrivalTrapRoomRecipients(next, trapRoomID, actorID)
		next, err = next.ApplyArrivalTrap(actorID, *result.Transfer.ArrivalTrap)
		if err != nil {
			return State{}, err
		}
		if result.Transfer.ArrivalTrap.Dead {
			var death PlayerDeathResult
			next, death, err = next.PlanPlayerDeath(actorID, actorID, int32(in.Movement.Now), in.View, catalog, roll, allocate)
			if err != nil {
				return State{}, err
			}
			result.Death = &death
		}
		if result.Transfer.ArrivalTrap.Alarm && result.Transfer.ArrivalTrap.Triggered && result.Death == nil {
			var alarm ArrivalAlarmResult
			next, alarm, err = next.ApplyArrivalAlarmWithCatalog(actorID, *result.Transfer.ArrivalTrap, int32(in.Movement.Now), catalog, roll, allocate)
			if err != nil {
				return State{}, err
			}
			result.Alarm = &alarm
		}
		trapEvent, hasTrapEvent = arrivalTrapEventFor(actorID, trapActorName, trapRoomID, *result.Transfer.ArrivalTrap, trapRoomRecipients)
	}
	if result.Transfer.Entry != nil {
		var err error
		result.Transfer.Entry.Scene, err = next.CurrentScene(actorID, in.View.Hour)
		if err != nil {
			return State{}, err
		}
	}
	if hasTrapEvent {
		result.ArrivalTrapEvent = &trapEvent
	}
	return next, nil
}

// arrivalTrapRoomRecipients snapshots the descriptor-visible players in the
// room immediately before check_traps mutates its actor. C broadcast_rom walks
// that room at trap time; using this snapshot avoids consulting the command's
// final state after later first_fol entries have moved or the trap relocated
// the actor. PlayerIDs preserves the canonical room order for durable replay.
func arrivalTrapRoomRecipients(s State, roomID int16, actorID string) []string {
	room, ok := s.Rooms[roomID]
	if !ok {
		return nil
	}
	seen := make(map[string]struct{}, len(room.PlayerIDs))
	recipients := make([]string, 0, len(room.PlayerIDs))
	for _, id := range room.PlayerIDs {
		if id == "" || id == actorID {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		player, exists := s.Players[id]
		if !exists || !player.Online {
			continue
		}
		seen[id] = struct{}{}
		recipients = append(recipients, id)
	}
	return recipients
}

// moveFollowerTree re-evaluates the same directional command for each follower
// after its leader is already in the destination. Destination occupancy is
// therefore authoritative at each step, and a denied follower remains behind
// without preventing later siblings from trying. A follower's own children are
// processed before that follower's arrival trap, matching recursive C move().
func (s State) moveFollowerTree(leaderID string, sourceRoomID int16, in TransferInput, catalog SpawnCatalog, roll func(int, int) int, allocate func() (string, error)) (State, []FollowerStepResult, error) {
	leader, ok := s.Players[leaderID]
	if !ok {
		return State{}, nil, fmt.Errorf("follower leader absent")
	}
	ids := append([]string(nil), leader.FollowerIDs...)
	var results []FollowerStepResult
	next := s
	for _, followerID := range ids {
		follower, exists := next.Players[followerID]
		if !exists || !follower.Online {
			return State{}, nil, fmt.Errorf("follower absent or offline")
		}
		if follower.Body.RoomID != sourceRoomID {
			continue
		}
		leaderState, exists := next.Players[leaderID]
		if !exists {
			return State{}, nil, fmt.Errorf("follower leader absent after step")
		}
		destination, err := next.ProjectRoom(leaderState.Body.RoomID)
		if err != nil {
			return State{}, nil, err
		}
		childInput := in
		childInput.ActorID = followerID
		childInput.Movement.Destination = &destination
		childInput.View.ViewerID = followerID
		childState, child, err := next.directionalStepSingle(childInput, catalog, roll, allocate, false)
		if err != nil {
			return State{}, nil, err
		}
		next = childState
		childResult := FollowerStepResult{ActorID: followerID, Transfer: child.Transfer, Death: child.Death}
		if child.Transfer.Movement.Moved && child.Death == nil {
			next, childResult.Followers, err = next.moveFollowerTree(followerID, sourceRoomID, childInput, catalog, roll, allocate)
			if err != nil {
				return State{}, nil, err
			}
			childForTrap := DirectionalStepResult{Transfer: childResult.Transfer, Death: childResult.Death, Followers: childResult.Followers}
			if err := next.attachArrivalTrap(followerID, &childForTrap.Transfer, roll); err != nil {
				return State{}, nil, err
			}
			next, err = applyArrivalStep(next, followerID, &childForTrap, childInput, catalog, roll, allocate)
			if err != nil {
				return State{}, nil, err
			}
			childResult.Transfer, childResult.Death = childForTrap.Transfer, childForTrap.Death
			if childForTrap.ArrivalTrapEvent != nil && childForTrap.Death == nil {
				event := *childForTrap.ArrivalTrapEvent
				childResult.ArrivalTrapEvent = &event
			}
		}
		results = append(results, childResult)
	}
	return next, results, nil
}

// DirectionalStep combines traversal/entry, recursive follower movement,
// destination trap effects, and lethal self-fall resolution. Original move
// calls die(player,player) and returns without entering the target.
func (s State) DirectionalStep(in TransferInput, catalog SpawnCatalog, roll func(int, int) int, allocate func() (string, error)) (State, DirectionalStepResult, error) {
	leader, exists := s.Players[in.ActorID]
	if !exists {
		// Let the canonical transfer return its authoritative error shape.
		return s.directionalStepSingle(in, catalog, roll, allocate, true)
	}
	sourceRoomID := leader.Body.RoomID
	next, result, err := s.directionalStepSingle(in, catalog, roll, allocate, false)
	if err != nil {
		return State{}, DirectionalStepResult{}, err
	}
	if result.Death != nil || !result.Transfer.Movement.Moved {
		return next, result, nil
	}
	if leader.FollowerRefs != nil {
		var npcMoves []NPCMonsterFollowerMove
		next, result.Followers, npcMoves, result.FollowerOrder, err = next.moveMixedFollowerTree(in.ActorID, sourceRoomID, in, catalog, roll, allocate)
		if err != nil {
			return State{}, DirectionalStepResult{}, err
		}
		if len(npcMoves) != 0 {
			result.NPCMonsterFollowers = &NPCMonsterFollowerProposal{
				ActorID:           in.ActorID,
				SourceRoomID:      sourceRoomID,
				DestinationRoomID: next.Players[in.ActorID].Body.RoomID,
				Now:               int32(in.Movement.Now),
				Moves:             npcMoves,
			}
		}
	} else if len(leader.FollowerIDs) > 0 {
		next, result.Followers, err = next.moveFollowerTree(in.ActorID, sourceRoomID, in, catalog, roll, allocate)
		if err != nil {
			return State{}, DirectionalStepResult{}, err
		}
	}
	if leader.FollowerRefs == nil && next.NPCs != nil {
		monsterFollowers, followerErr := next.PlanNPCMonsterFollowers(NPCMonsterFollowerInput{
			ActorID:      in.ActorID,
			SourceRoomID: sourceRoomID,
			Now:          int32(in.Movement.Now),
		})
		if followerErr != nil {
			return State{}, DirectionalStepResult{}, followerErr
		}
		next, followerErr = next.ApplyNPCMonsterFollowers(monsterFollowers)
		if followerErr != nil {
			return State{}, DirectionalStepResult{}, followerErr
		}
		result.NPCMonsterFollowers = &monsterFollowers
	}
	// C's command2 move scans the old room's monster list after recursive
	// player followers and before check_traps. Keep the chase candidate and its
	// state transition inside this same atomic directional command. The
	// standalone transfer APIs intentionally do not run this orchestration.
	if next.NPCs != nil {
		chase, chaseErr := next.PlanNPCFollowerChase(NPCFollowerChaseInput{
			ActorID:      in.ActorID,
			SourceRoomID: sourceRoomID,
			Now:          int32(in.Movement.Now),
		}, roll)
		if chaseErr != nil {
			return State{}, DirectionalStepResult{}, chaseErr
		}
		next, chaseErr = next.ApplyNPCFollowerChase(chase)
		if chaseErr != nil {
			return State{}, DirectionalStepResult{}, chaseErr
		}
		result.NPCChase = &chase
	}
	if err := next.attachArrivalTrap(in.ActorID, &result.Transfer, roll); err != nil {
		return State{}, DirectionalStepResult{}, err
	}
	next, err = applyArrivalStep(next, in.ActorID, &result, in, catalog, roll, allocate)
	if err != nil {
		return State{}, DirectionalStepResult{}, err
	}
	return next, result, nil
}
