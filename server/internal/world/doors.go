package world

import (
	"fmt"
	"strings"
)

type DoorResult struct {
	Exit            LegacyExit
	Hidden, Changed bool
	Message         string
}

const (
	DoorOpen  = "open"
	DoorClose = "close"
)

// DoorProposal is the durable command candidate for command6.c:openexit and
// closeexit. ExitIndex and Expected bind it to one exact room snapshot; a
// later state change cannot make the command silently mutate a different
// exit. Prefix/occurrence expansion remains deliberately limited to the
// source's first occurrence until the full command parser is admitted.
type DoorProposal struct {
	ActorID   string
	Action    string
	Target    string
	RoomID    int16
	ExitIndex int
	Expected  LegacyExit
	Now       int32
	Changed   bool
	Broadcast bool
	Response  string
}

type DoorCommandResult struct {
	Response  string `json:"response"`
	Broadcast bool   `json:"broadcast"`
	RoomID    int16  `json:"room_id,omitempty"`
	ExitName  string `json:"exit_name,omitempty"`
	Action    string `json:"action,omitempty"`
}

type DoorEvent struct {
	RoomID         int16
	ExcludeActorID string
	Text           string
}

func doorActor(s State, actorID string) (PlayerState, error) {
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Body.Type != 0 || actor.Body.Name == "" {
		return PlayerState{}, fmt.Errorf("online door actor absent")
	}
	if _, ok := s.Rooms[actor.Body.RoomID]; !ok {
		return PlayerState{}, fmt.Errorf("door actor room absent")
	}
	return actor, nil
}

func doorResponse(action, target string) string {
	if target != "" {
		return "그런 출구는 없습니다.\r\n"
	}
	if action == DoorOpen {
		return "무엇을 열고 싶으세요?\r\n"
	}
	return "무엇을 닫고 싶으세요?\r\n"
}

// PlanDoor resolves the same-room exit and runs the source-backed open/close
// transition without mutating the snapshot. The actor's detect-invisible flag
// controls whether an invisible exit can be selected; secret exits remain
// unavailable to this bounded command just as they are to SelectExit.
func (s State) PlanDoor(actorID, action, target string, now int32) (DoorProposal, error) {
	if err := s.Validate(); err != nil {
		return DoorProposal{}, err
	}
	if action != DoorOpen && action != DoorClose {
		return DoorProposal{}, fmt.Errorf("unknown door action")
	}
	if now < 0 || strings.TrimSpace(target) != target {
		return DoorProposal{}, fmt.Errorf("invalid door target or clock")
	}
	actor, err := doorActor(s, actorID)
	if err != nil {
		return DoorProposal{}, err
	}
	proposal := DoorProposal{ActorID: actorID, Action: action, Target: target, RoomID: actor.Body.RoomID, ExitIndex: -1, Now: now}
	if target == "" {
		proposal.Response = doorResponse(action, target)
		return proposal, nil
	}
	room := s.Rooms[actor.Body.RoomID]
	index := SelectExit(room.Resource.Exits, target, 1, flag(actor.Body.Flags[:], playerDetectInvisibleFlag))
	if index < 0 {
		proposal.Response = doorResponse(action, target)
		return proposal, nil
	}
	proposal.ExitIndex = index
	proposal.Expected = room.Resource.Exits[index]
	var result DoorResult
	if action == DoorOpen {
		result = OpenDoor(proposal.Expected, flag(actor.Body.Flags[:], playerHiddenStateFlag), now)
	} else {
		result = CloseDoor(proposal.Expected, flag(actor.Body.Flags[:], playerHiddenStateFlag))
	}
	proposal.Changed = result.Changed
	proposal.Broadcast = result.Changed
	proposal.Response = result.Message + "\r\n"
	return proposal, nil
}

// ApplyDoor atomically applies a proposal from the same snapshot. A failed
// source transition is still a durable response, but does not clear hiding or
// rewrite the exit. A stale room/exit/actor is rejected instead of reselecting.
func (s State) ApplyDoor(proposal DoorProposal) (State, DoorCommandResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, DoorCommandResult{}, err
	}
	actor, err := doorActor(s, proposal.ActorID)
	if err != nil || actor.Body.RoomID != proposal.RoomID {
		if err == nil {
			err = fmt.Errorf("door actor room changed")
		}
		return State{}, DoorCommandResult{}, err
	}
	if proposal.Action != DoorOpen && proposal.Action != DoorClose || proposal.Now < 0 || strings.TrimSpace(proposal.Target) != proposal.Target {
		return State{}, DoorCommandResult{}, fmt.Errorf("invalid door proposal")
	}
	if proposal.ExitIndex < 0 {
		if proposal.Target != "" {
			room, ok := s.Rooms[proposal.RoomID]
			if !ok || SelectExit(room.Resource.Exits, proposal.Target, 1, flag(actor.Body.Flags[:], playerDetectInvisibleFlag)) >= 0 {
				return State{}, DoorCommandResult{}, fmt.Errorf("stale missing-door proposal")
			}
			if proposal.Response != doorResponse(proposal.Action, proposal.Target) {
				return State{}, DoorCommandResult{}, fmt.Errorf("stale missing-door response")
			}
			return s, DoorCommandResult{Response: proposal.Response}, nil
		}
		if proposal.Response != doorResponse(proposal.Action, proposal.Target) {
			return State{}, DoorCommandResult{}, fmt.Errorf("stale door missing-target response")
		}
		return s, DoorCommandResult{Response: proposal.Response}, nil
	}
	room, ok := s.Rooms[proposal.RoomID]
	if !ok || proposal.ExitIndex >= len(room.Resource.Exits) || room.Resource.Exits[proposal.ExitIndex] != proposal.Expected {
		return State{}, DoorCommandResult{}, fmt.Errorf("stale door exit proposal")
	}
	current := room.Resource.Exits[proposal.ExitIndex]
	var result DoorResult
	if proposal.Action == DoorOpen {
		result = OpenDoor(current, flag(actor.Body.Flags[:], playerHiddenStateFlag), proposal.Now)
	} else {
		result = CloseDoor(current, flag(actor.Body.Flags[:], playerHiddenStateFlag))
	}
	if result.Changed != proposal.Changed || result.Message+"\r\n" != proposal.Response || result.Changed != proposal.Broadcast {
		return State{}, DoorCommandResult{}, fmt.Errorf("stale door transition")
	}
	if !proposal.Changed {
		return s, DoorCommandResult{Response: proposal.Response}, nil
	}
	next := s.clone()
	nextRoom := next.Rooms[proposal.RoomID]
	nextRoom.Resource.Exits[proposal.ExitIndex] = result.Exit
	next.Rooms[proposal.RoomID] = nextRoom
	nextActor := next.Players[proposal.ActorID]
	setSettingFlag(&nextActor.Body, playerHiddenStateFlag, false)
	next.Players[proposal.ActorID] = nextActor
	if err := next.Validate(); err != nil {
		return State{}, DoorCommandResult{}, err
	}
	return next, DoorCommandResult{
		Response:  proposal.Response,
		Broadcast: true,
		RoomID:    proposal.RoomID,
		ExitName:  result.Exit.Name,
		Action:    proposal.Action,
	}, nil
}

func (s State) RoomDoorEvent(actorID, action, exitName string) (DoorEvent, bool, error) {
	if action != DoorOpen && action != DoorClose || exitName == "" {
		return DoorEvent{}, false, fmt.Errorf("invalid door event")
	}
	actor, err := doorActor(s, actorID)
	if err != nil {
		return DoorEvent{}, false, err
	}
	verb := "닫습니다"
	if action == DoorOpen {
		verb = "열었습니다"
	}
	return DoorEvent{
		RoomID:         actor.Body.RoomID,
		ExcludeActorID: actorID,
		Text:           fmt.Sprintf("\n%s이 %s쪽 출구를 %s.\r\n", actor.Body.Name, exitName, verb),
	}, true, nil
}

// OpenDoor/CloseDoor operate on an already selected exit. The command layer
// uses SelectExit first. On success the caller must atomically commit the exit
// and actor visibility and emit the room broadcast; failure changes neither.
func OpenDoor(e LegacyExit, hidden bool, now int32) DoorResult {
	r := DoorResult{Exit: e, Hidden: hidden}
	if flag(e.Flags[:], 2) {
		r.Message = "그것은 잠겨져 있습니다."
		return r
	}
	if !flag(e.Flags[:], 3) {
		r.Message = "벌써 열려져 있습니다."
		return r
	}
	r.Exit.Flags[0] &= ^byte(8)
	r.Exit.LastTime = now
	r.Hidden = false
	r.Changed = true
	r.Message = fmt.Sprintf("당신은 %s쪽 출구를 열었습니다.", e.Name)
	return r
}

func CloseDoor(e LegacyExit, hidden bool) DoorResult {
	r := DoorResult{Exit: e, Hidden: hidden}
	if flag(e.Flags[:], 3) {
		r.Message = "벌써 닫혀져 있습니다."
		return r
	}
	if !flag(e.Flags[:], 5) {
		r.Message = "당신은 그 출구를 닫을 수 없습니다."
		return r
	}
	r.Exit.Flags[0] |= 8
	r.Hidden = false
	r.Changed = true
	r.Message = fmt.Sprintf("당신은 %s쪽 출구를 닫습니다.", e.Name)
	return r
}

// RefreshDoors ports check_exits, run on room entry before the scene is shown.
// Equality is not due. Widen saved ILP32 operands before adding; never wrap.
// Returns a fresh slice; door deadlines are unchanged by automatic resets.
func RefreshDoors(exits []LegacyExit, now int64) []LegacyExit {
	result := append([]LegacyExit(nil), exits...)
	for i, e := range result {
		if int64(e.LastTime)+int64(e.Interval) >= now {
			continue
		}
		if flag(e.Flags[:], 4) {
			result[i].Flags[0] |= 12
		} else if flag(e.Flags[:], 5) {
			result[i].Flags[0] |= 8
		}
	}
	return result
}
