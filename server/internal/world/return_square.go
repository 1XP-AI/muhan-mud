package world

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// These values are the source-backed constants used by command1.c's
// return_square branch.  They intentionally remain local to this bounded
// reducer; they are not a new interpretation of the legacy flag table.
const (
	returnSquareFamilyReturnFlag   = 58 // PFRTUN
	returnSquareDMInvisibleFlag    = 10 // PDMINV
	returnSquareExpansionDailySlot = 9  // DL_EXPND
	returnSquareInvincibleClass    = 9  // INVINCIBLE
	returnSquareSquareRoom         = int16(1001)
	returnSquareFamilyRoomBase     = int16(3300)
)

// Exported aliases let a transport or an integration test name the imported
// legacy values without selecting a different flag numbering scheme.
const (
	ReturnSquareFamilyReturnFlag   = returnSquareFamilyReturnFlag
	ReturnSquareDMInvisibleFlag    = returnSquareDMInvisibleFlag
	ReturnSquareExpansionDailySlot = returnSquareExpansionDailySlot
	ReturnSquareInvincibleClass    = returnSquareInvincibleClass
	ReturnSquareSquareRoom         = returnSquareSquareRoom
	ReturnSquareFamilyRoomBase     = returnSquareFamilyRoomBase
)

// ReturnSquareEvent is the committed room projection for a successful
// return. The actor receives Result.Response; the two room texts are sent to
// the source and destination rooms only after the receipt commits.
type ReturnSquareEvent struct {
	ActorID           string `json:"actor_id"`
	ActorName         string `json:"actor_name"`
	SourceRoomID      int16  `json:"source_room_id"`
	DestinationRoomID int16  `json:"destination_room_id"`
	ExcludeActorID    string `json:"exclude_actor_id"`
	SourceText        string `json:"source_text"`
	DestinationText   string `json:"destination_text"`
}

// ReturnSquareResult is the deterministic actor response and post-commit
// event metadata for command1.c:return_square. Denials are successful no-op
// receipts (Moved=false); malformed state or an unresolved destination is an
// error and therefore produces no receipt.
type ReturnSquareResult struct {
	Action            string             `json:"action"`
	ActorID           string             `json:"actor_id"`
	ActorName         string             `json:"actor_name"`
	SourceRoomID      int16              `json:"source_room_id"`
	DestinationRoomID int16              `json:"destination_room_id"`
	Response          string             `json:"response"`
	Moved             bool               `json:"moved"`
	MPDrained         bool               `json:"mp_drained"`
	Broadcast         bool               `json:"broadcast"`
	Event             *ReturnSquareEvent `json:"event,omitempty"`
}

func returnSquareActorName(name string) bool {
	if name == "" || !utf8.ValidString(name) || strings.TrimSpace(name) != name {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

func returnSquareResult(actorID, actorName string, source int16, response string) ReturnSquareResult {
	return ReturnSquareResult{
		Action:       "return-square",
		ActorID:      actorID,
		ActorName:    actorName,
		SourceRoomID: source,
		Response:     response,
	}
}

// returnSquareCombatState mirrors command1.c's ply_is_attacking ordering. A
// pre-migration legacy monster list is readable only as an unresolved combat
// boundary; it is never silently treated as peaceful.
func (s State) returnSquareCombatState(actorID string, room RoomState) (bool, error) {
	if s.NPCs == nil {
		if len(room.Resource.Monsters) != 0 {
			return false, fmt.Errorf("return-square enemy relations unresolved")
		}
		return false, nil
	}
	fighting, _, err := s.NPCMovementRelations(actorID)
	return fighting, err
}

func returnSquareDestination(body LegacyMonster) int16 {
	if !flag(body.Flags[:], returnSquareFamilyReturnFlag) {
		return returnSquareSquareRoom
	}
	return returnSquareFamilyRoomBase + int16(body.Daily[returnSquareExpansionDailySlot].Max)
}

func (s State) returnSquareFollowers(actorID string, actor PlayerState) (bool, error) {
	leaderID := actorID
	if actor.FollowingID != "" {
		leaderID = actor.FollowingID
	}
	leader, ok := s.Players[leaderID]
	if !ok || !leader.Online || leader.Body.Type != 0 {
		return false, fmt.Errorf("return-square follower leader absent")
	}
	// When the actor follows somebody, C examines the leader's first_fol head,
	// not the actor's own list. FollowerRefs is authoritative when imported;
	// the category lists remain a valid empty/non-empty index for old snapshots.
	return len(leader.FollowerRefs) != 0 || len(leader.FollowerIDs) != 0 || len(leader.NPCFollowerIDs) != 0, nil
}

func returnSquareInsertPlayerIDs(s State, ids []string, actorID string) ([]string, error) {
	actor, ok := s.Players[actorID]
	if !ok || !returnSquareActorName(actor.Body.Name) {
		return nil, fmt.Errorf("return-square actor identity absent")
	}
	for _, id := range ids {
		if id == actorID {
			return nil, fmt.Errorf("return-square actor already in destination")
		}
		other, ok := s.Players[id]
		if !ok || !other.Online || other.Body.Type != 0 || !returnSquareActorName(other.Body.Name) {
			return nil, fmt.Errorf("return-square destination player identity unresolved")
		}
	}
	at := len(ids)
	for i, id := range ids {
		if s.Players[id].Body.Name > actor.Body.Name {
			at = i
			break
		}
	}
	out := make([]string, 0, len(ids)+1)
	out = append(out, ids[:at]...)
	out = append(out, actorID)
	out = append(out, ids[at:]...)
	return out, nil
}

// PlanReturnSquare ports the bounded player branch of command1.c. It clones
// the complete validated snapshot only after every denial, destination and
// overflow precondition has passed, so source removal and destination entry
// are one atomic candidate. No output is emitted from this pure reducer.
func (s State) PlanReturnSquare(actorID string) (State, ReturnSquareResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, ReturnSquareResult{}, err
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Body.Type != 0 || !returnSquareActorName(actor.Body.Name) {
		return State{}, ReturnSquareResult{}, fmt.Errorf("online return-square actor absent")
	}
	sourceID := actor.Body.RoomID
	source, ok := s.Rooms[sourceID]
	if !ok || source.Resource.ID != sourceID {
		return State{}, ReturnSquareResult{}, fmt.Errorf("return-square source room identity unresolved")
	}

	fighting, err := s.returnSquareCombatState(actorID, source)
	if err != nil {
		return State{}, ReturnSquareResult{}, err
	}
	if fighting {
		return s, returnSquareResult(actorID, actor.Body.Name, sourceID, "당신은 싸우고 있는 중입니다!!\r\n"), nil
	}
	if sourceID == returnSquareSquareRoom {
		return s, returnSquareResult(actorID, actor.Body.Name, sourceID, "당신은 이미 광장에 와 있습니다!\r\n"), nil
	}
	hasFollowers, err := s.returnSquareFollowers(actorID, actor)
	if err != nil {
		return State{}, ReturnSquareResult{}, err
	}
	if hasFollowers {
		return s, returnSquareResult(actorID, actor.Body.Name, sourceID, "먼저 그룹에서 나오세요.\r\n"), nil
	}

	destinationID := returnSquareDestination(actor.Body)
	destination, ok := s.Rooms[destinationID]
	if !ok || destination.Resource.ID != destinationID {
		return State{}, ReturnSquareResult{}, fmt.Errorf("return-square destination room identity unresolved")
	}
	if destination.Resource.BeenHere == 2147483647 {
		return State{}, ReturnSquareResult{}, fmt.Errorf("return-square destination visit counter overflow")
	}

	next := s.clone()
	updatedSource, removed := removeString(next.Rooms[sourceID].PlayerIDs, actorID)
	if !removed {
		return State{}, ReturnSquareResult{}, fmt.Errorf("return-square actor absent from source membership")
	}
	nextSource := next.Rooms[sourceID]
	nextSource.PlayerIDs = updatedSource
	next.Rooms[sourceID] = nextSource

	nextDestination := next.Rooms[destinationID]
	inserted, err := returnSquareInsertPlayerIDs(next, nextDestination.PlayerIDs, actorID)
	if err != nil {
		return State{}, ReturnSquareResult{}, err
	}
	nextDestination.PlayerIDs = inserted
	nextDestination.Resource.BeenHere++
	next.Rooms[destinationID] = nextDestination

	nextActor := next.Players[actorID]
	nextActor.Body.RoomID = destinationID
	mpDrained := nextActor.Body.Level > 20 && nextActor.Body.Class < returnSquareInvincibleClass
	if mpDrained {
		nextActor.Body.MPCurrent = 0
	}
	next.Players[actorID] = nextActor
	if err := next.Validate(); err != nil {
		return State{}, ReturnSquareResult{}, err
	}

	result := returnSquareResult(actorID, actor.Body.Name, sourceID, "당신이 \"귀환!\"이라고 외치자 이상한 힘에 의해 어딘가로 빨려들어갑니다.\r\n")
	if mpDrained {
		result.Response = "당신이 귀환하려하자 흑암의 세력이 당신의 도력을 뺏습니다.\r\n" + result.Response
	}
	result.DestinationRoomID = destinationID
	result.Moved = true
	result.MPDrained = mpDrained
	result.Broadcast = !flag(actor.Body.Flags[:], returnSquareDMInvisibleFlag)
	if result.Broadcast {
		result.Event = &ReturnSquareEvent{
			ActorID:           actorID,
			ActorName:         actor.Body.Name,
			SourceRoomID:      sourceID,
			DestinationRoomID: destinationID,
			ExcludeActorID:    actorID,
			SourceText:        fmt.Sprintf("\n%s님이 갑자기 사라집니다!\r\n", actor.Body.Name),
			DestinationText:   fmt.Sprintf("\n%s님이 갑자기 자욱한 연기와 함께 나타났습니다!\r\n", actor.Body.Name),
		}
	}
	return next, result, nil
}

// ReturnSquare is a concise alias for callers that do not use Plan-prefixed
// reducers.
func (s State) ReturnSquare(actorID string) (State, ReturnSquareResult, error) {
	return s.PlanReturnSquare(actorID)
}
