package world

import (
	"fmt"
	"strings"
)

// YellResult is the durable response for command6.c:yell. Room fan-out is
// derived only after the candidate state commits, just like SayResult.
type YellResult struct {
	Response  string
	Broadcast bool
}

// YellEvent is a committed-state projection. The current room identifies the
// speaker; exits receive the anonymous legacy message. Exit order is retained
// so duplicate/ordered legacy links cannot be silently normalized.
type YellEvent struct {
	RoomID         int16
	ExcludeActorID string
	Text           string
}

// PlanYell ports the stateful part of command6.c:yell. Empty and silenced
// cries do not reveal the player or produce events. A successful yell clears
// the hidden bit and stores no event in canonical state; callers derive the
// event list from the committed snapshot and suppress it on receipt replay.
func (s State) PlanYell(actorID, text string) (State, YellResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, YellResult{}, err
	}
	p, ok := s.Players[actorID]
	if !ok || !p.Online {
		return State{}, YellResult{}, fmt.Errorf("online yeller absent")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return s, YellResult{Response: "무슨말을 외치려구요?\r\n"}, nil
	}
	if flag(p.Body.Flags[:], playerSilentStateFlag) {
		return s, YellResult{Response: "당신의 목소리가 너무 약해서 외칠수 없습니다.\r\n"}, nil
	}
	next := s.clone()
	p = next.Players[actorID]
	p.Body.Flags[playerHiddenStateFlag/8] &^= 1 << (playerHiddenStateFlag % 8)
	next.Players[actorID] = p
	if _, err := next.RoomYellEvents(actorID, text); err != nil {
		return State{}, YellResult{}, err
	}
	return next, YellResult{Response: "예. 좋습니다.\r\n", Broadcast: true}, nil
}

// RoomYellEvents derives the current and adjacent-room messages from the
// authoritative committed graph. An exit to an unknown room is an invalid
// topology, not an invitation to invent a recipient, and therefore fails
// closed. Repeated exits remain repeated events to preserve C iteration order.
func (s State) RoomYellEvents(actorID, text string) ([]YellEvent, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	p, ok := s.Players[actorID]
	if !ok || !p.Online {
		return nil, fmt.Errorf("online yeller absent")
	}
	text = strings.TrimSpace(text)
	if text == "" || flag(p.Body.Flags[:], playerSilentStateFlag) {
		return nil, nil
	}
	room, ok := s.Rooms[p.Body.RoomID]
	if !ok {
		return nil, fmt.Errorf("yeller room missing")
	}
	events := []YellEvent{{
		RoomID:         p.Body.RoomID,
		ExcludeActorID: actorID,
		Text:           fmt.Sprintf("\n%s님이 \"%s!\"라고 외칩니다.\r\n", p.Body.Name, text),
	}}
	for _, exit := range room.Resource.Exits {
		if _, exists := s.Rooms[exit.Destination]; !exists {
			return nil, fmt.Errorf("yell exit destination %d missing", exit.Destination)
		}
		events = append(events, YellEvent{
			RoomID: exit.Destination,
			Text:   fmt.Sprintf("\n누군가가 \"%s!\"라고 외쳤습니다.\r\n", text),
		})
	}
	return events, nil
}
