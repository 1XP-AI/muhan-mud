package world

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
)

// command6.c:go / cmdlist-30 가·들어가. Cardinal move() stays on DirectionalStep.
const (
	goDetectInvisibleFlag = 21 // PDINVI
	goAttackTimer         = 3  // LT_ATTCK
	goSilentFlag          = 44 // PSILNC; TransferWithIDs maps this to Immobile
	goLockedExitFlag      = 2  // XLOCKD; ExitRestriction owns the locked message
	goClosedExitFlag      = 3  // XCLOSD
	goClosableExitFlag    = 5  // XCLOSS; check_exits / RefreshDoors
	goFlyExitFlag         = 11 // XFLYSP
	goFemaleExitFlag      = 12 // XFEMAL
	goMaleExitFlag        = 13 // XMALES
	goNightExitFlag       = 16 // XNGHTO
	goDayExitFlag         = 17 // XDAYON
	goMalePlayerFlag      = 12 // PMALES
	goFlyPlayerFlag       = 31 // PFLYSP

	GoUsageResponse      = "어디로 가고 싶으세요?"
	GoMissingResponse    = "그런 출구는 없습니다."
	GoMapMissingResponse = "그 방향의 지도가 없습니다."
	GoSilentResponse     = "당신은 움직일 수가 없습니다."
	GoCombatResponse     = "싸우는 중에는 이동할 수 없습니다."
	GoLockedResponse     = "그 출구는 잠겨 있습니다."
	GoClosedResponse     = "그 출구는 닫혀 있습니다."
	GoFlyResponse        = "그곳에는 날아서만 갈 수 있습니다."
	GoNightResponse      = "그 출구는 밤에만 갈 수 있습니다."
	GoDayResponse        = "그 출구로는 낮에만 갈 수 있습니다."
	GoFemaleResponse     = "여성만 들어갈 수 있습니다. 여탕인가~~"
	GoMaleResponse       = "남성만 들어갈 수 있습니다."
)

var (
	ErrGoActorAbsent           = errors.New("online go actor absent")
	ErrGoRoomAbsent            = errors.New("go actor room absent")
	ErrGoInvalidOccurrence     = errors.New("invalid go occurrence")
	ErrGoDestinationUnresolved = errors.New("go destination or exit graph unresolved")
	ErrGoStaleProposal         = errors.New("stale or invalid go proposal")
	ErrGoInvalidProposal       = errors.New("invalid go proposal")
)

// GoOptions are server-owned clock/RNG/catalog inputs, never client claims.
type GoOptions struct {
	Now      int32
	Hour     int
	Roll     func(int, int) int
	Catalog  SpawnCatalog
	Allocate func() (string, error)
}

// GoProposal is one command6.c:go candidate against one snapshot.
type GoProposal struct {
	ActorID           string
	Prefix            string
	Occurrence        int
	ExitName          string
	SourceRoomID      int16
	DestinationRoomID int16
	Response          string
	Moved             bool
	Transfer          TransferProposal
	Death             *PlayerDeathResult
	// NPCChase is the exact command6.c:go chase proposal committed with this
	// movement. Its ordered move IDs are receipt-only projection metadata; the
	// durable GoResult/response shape remains unchanged.
	NPCChase *NPCFollowerChaseProposal `json:"-"`
	// ArrivalTrapEvent is receipt-only projection metadata. It is copied from
	// the leader step after reducer application and never enters GoResult.
	ArrivalTrapEvent *ArrivalTrapEvent `json:"-"`

	before State
	next   State
}

// GoResult is the durable receipt projection for 가/들어가.
type GoResult struct {
	Response          string `json:"response"`
	Moved             bool   `json:"moved"`
	ActorID           string `json:"actor_id"`
	SourceRoomID      int16  `json:"source_room_id"`
	DestinationRoomID int16  `json:"destination_room_id,omitempty"`
	ExitName          string `json:"exit_name,omitempty"`
}

func (s State) PlanGo(actorID, prefix string, occurrence int, options GoOptions) (GoProposal, error) {
	if err := s.Validate(); err != nil {
		return GoProposal{}, err
	}
	if actorID == "" {
		return GoProposal{}, ErrGoActorAbsent
	}
	if occurrence < 1 {
		return GoProposal{}, ErrGoInvalidOccurrence
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Body.Type != 0 {
		return GoProposal{}, ErrGoActorAbsent
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok {
		return GoProposal{}, ErrGoRoomAbsent
	}
	proposal := GoProposal{
		ActorID:      actorID,
		Prefix:       prefix,
		Occurrence:   occurrence,
		SourceRoomID: actor.Body.RoomID,
		before:       s,
	}
	if prefix == "" {
		proposal.Response = GoUsageResponse
		proposal.next = s
		return proposal, nil
	}
	index := SelectExit(room.Resource.Exits, prefix, occurrence, flag(actor.Body.Flags[:], goDetectInvisibleFlag))
	if index < 0 {
		proposal.Response = GoMissingResponse
		proposal.next = s
		return proposal, nil
	}
	exit := room.Resource.Exits[index]
	proposal.ExitName = exit.Name
	_, destExists := s.Rooms[exit.Destination]
	var destination *LegacyRoom
	if destExists {
		projected, err := s.ProjectRoom(exit.Destination)
		if err != nil {
			return GoProposal{}, err
		}
		destination = &projected
		proposal.DestinationRoomID = exit.Destination
	}
	input := TransferInput{
		ActorID: actorID,
		Movement: MovementInput{
			Prefix:        prefix,
			Occurrence:    occurrence,
			Destination:   destination,
			Now:           int64(options.Now),
			AttackReadyAt: int64(actor.Body.Timers[goAttackTimer].LastTime) + int64(actor.Body.Timers[goAttackTimer].Interval),
			Traversal: TraversalInput{
				PassageOptions: PassageOptions{Hour: options.Hour},
			},
		},
		View: SceneOptions{Hour: options.Hour, ViewerID: actorID},
	}
	input.View.ViewOptions.Hour = options.Hour
	next, step, err := s.goStep(input, options.Catalog, options.Roll, options.Allocate)
	if err != nil {
		return GoProposal{}, err
	}
	// Closed/fly/time/sex (and other Stop denials) speak first. An unmigrated
	// destination after those gates fail-closed instead of writing track.
	if !destExists && (step.Transfer.Movement.Moved || (!step.Transfer.Movement.Traversal.Stop && step.Death == nil)) {
		return GoProposal{}, ErrGoDestinationUnresolved
	}
	proposal.Transfer = step.Transfer
	proposal.Death = step.Death
	proposal.NPCChase = step.NPCChase
	proposal.ArrivalTrapEvent = step.ArrivalTrapEvent
	proposal.Moved = step.Transfer.Movement.Moved
	proposal.next = next
	if step.Transfer.Movement.Moved {
		proposal.DestinationRoomID = step.Transfer.Movement.RoomID
	} else if step.Death != nil {
		proposal.DestinationRoomID = next.Players[actorID].Body.RoomID
	}
	response := strings.Join(step.Transfer.Movement.Messages, "")
	if step.Death != nil {
		response += step.Death.Entry.Scene
	} else if step.Transfer.Entry != nil {
		response += step.Transfer.Entry.Scene
	}
	if response == "" && step.Transfer.Movement.Moved {
		response, err = next.CurrentScene(actorID, options.Hour)
		if err != nil {
			return GoProposal{}, err
		}
	}
	if step.NPCChase != nil {
		for _, move := range step.NPCChase.Moves {
			response += NPCGoChaseActorText(move.Body.Name)
		}
	}
	if step.ArrivalTrapEvent != nil {
		response += step.ArrivalTrapEvent.ActorText
	}
	proposal.Response = response
	return proposal, nil
}

// goStep is command6.c:go's post-admission wrapper: PlanMovement (not move()),
// recursive first_fol go(), MDMFOL, MFOLLO chase, check_traps, and die() on a
// lethal climb. DirectionalStep owns the same orchestration for cardinals.
func (s State) goStep(in TransferInput, catalog SpawnCatalog, roll func(int, int) int, allocate func() (string, error)) (State, DirectionalStepResult, error) {
	leader, exists := s.Players[in.ActorID]
	if !exists {
		return s.goStepSingle(in, catalog, roll, allocate, true)
	}
	sourceRoomID := leader.Body.RoomID
	next, result, err := s.goStepSingle(in, catalog, roll, allocate, false)
	if err != nil {
		return State{}, DirectionalStepResult{}, err
	}
	if result.Death != nil || !result.Transfer.Movement.Moved {
		return next, result, nil
	}
	if leader.FollowerRefs != nil {
		var npcMoves []NPCMonsterFollowerMove
		next, result.Followers, npcMoves, result.FollowerOrder, err = next.moveGoMixedFollowerTree(in.ActorID, sourceRoomID, in, catalog, roll, allocate)
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
		next, result.Followers, err = next.moveGoFollowerTree(in.ActorID, in, catalog, roll, allocate)
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
	if next.NPCs != nil {
		chase, chaseErr := next.PlanNPCGoChase(NPCFollowerChaseInput{
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

func (s State) goStepSingle(in TransferInput, catalog SpawnCatalog, roll func(int, int) int, allocate func() (string, error), applyTrap bool) (State, DirectionalStepResult, error) {
	next, transfer, err := s.transferWithPlanner(in, catalog, roll, allocate, PlanMovement)
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

func (s State) moveGoFollowerTree(leaderID string, in TransferInput, catalog SpawnCatalog, roll func(int, int) int, allocate func() (string, error)) (State, []FollowerStepResult, error) {
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
		childState, child, err := next.goStepSingle(childInput, catalog, roll, allocate, false)
		if err != nil {
			return State{}, nil, err
		}
		next = childState
		childResult := FollowerStepResult{ActorID: followerID, Transfer: child.Transfer, Death: child.Death}
		if child.Transfer.Movement.Moved && child.Death == nil {
			next, childResult.Followers, err = next.moveGoFollowerTree(followerID, childInput, catalog, roll, allocate)
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
		}
		results = append(results, childResult)
	}
	return next, results, nil
}

func (s State) moveGoMixedFollowerTree(leaderID string, sourceRoomID int16, in TransferInput, catalog SpawnCatalog, roll func(int, int) int, allocate func() (string, error)) (State, []FollowerStepResult, []NPCMonsterFollowerMove, []EntityRef, error) {
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
			childState, child, err := next.goStepSingle(childInput, catalog, roll, allocate, false)
			if err != nil {
				return State{}, nil, nil, nil, err
			}
			next = childState
			childResult := FollowerStepResult{ActorID: ref.ID, Transfer: child.Transfer, Death: child.Death}
			if child.Transfer.Movement.Moved && child.Death == nil {
				var childNPCMoves []NPCMonsterFollowerMove
				var childOrder []EntityRef
				next, childResult.Followers, childNPCMoves, childOrder, err = next.moveGoMixedFollowerTree(ref.ID, sourceRoomID, childInput, catalog, roll, allocate)
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

func (s State) ApplyGo(proposal GoProposal) (State, GoResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, GoResult{}, err
	}
	if proposal.ActorID == "" || proposal.Occurrence < 1 || !reflect.DeepEqual(s, proposal.before) {
		return State{}, GoResult{}, ErrGoStaleProposal
	}
	if err := proposal.next.Validate(); err != nil {
		return State{}, GoResult{}, ErrGoInvalidProposal
	}
	if proposal.Moved != proposal.Transfer.Movement.Moved {
		return State{}, GoResult{}, ErrGoInvalidProposal
	}
	result := GoResult{
		Response:          proposal.Response,
		Moved:             proposal.Moved,
		ActorID:           proposal.ActorID,
		SourceRoomID:      proposal.SourceRoomID,
		DestinationRoomID: proposal.DestinationRoomID,
		ExitName:          proposal.ExitName,
	}
	return proposal.next, result, nil
}
