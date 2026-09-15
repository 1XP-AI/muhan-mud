package world

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// SelectPlayerInRoom resolves a display name against authoritative room
// membership. It preserves the existing player-target contract by excluding
// the actor and selecting the first exact occurrence.
func (s State) SelectPlayerInRoom(actorID, name string) (string, error) {
	return s.selectPlayerInRoom(actorID, name, 1, false)
}

// SelectPlayerInRoomWithOccurrence is the occurrence-aware form of
// SelectPlayerInRoom. The room's PlayerIDs slice is authoritative; no map
// iteration or display-name sorting is used to resolve a duplicate name.
func (s State) SelectPlayerInRoomWithOccurrence(actorID, name string, occurrence int) (string, error) {
	return s.selectPlayerInRoom(actorID, name, occurrence, false)
}

// SelectFollowTarget resolves 따라's player target in C first_ply order. C
// includes the actor in first_ply, so the actor is deliberately retained here
// and FollowPlayer performs the self/cycle rejection without mutating state.
func (s State) SelectFollowTarget(actorID, name string, occurrence int) (string, error) {
	return s.selectPlayerInRoom(actorID, name, occurrence, true)
}

func validRoomPlayerTargetName(name string) bool {
	if name == "" || !utf8.ValidString(name) {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || unicode.IsSpace(r) || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

func (s State) selectPlayerInRoom(actorID, name string, occurrence int, includeActor bool) (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	if occurrence < 1 {
		return "", fmt.Errorf("player target occurrence must be positive")
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online {
		return "", fmt.Errorf("online player target context required")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("player target required")
	}
	if !validRoomPlayerTargetName(name) {
		return "", fmt.Errorf("invalid player target")
	}
	found := 0
	for _, id := range s.Rooms[actor.Body.RoomID].PlayerIDs {
		if !includeActor && id == actorID {
			continue
		}
		candidate, ok := s.Players[id]
		if !ok || !candidate.Online || candidate.Body.RoomID != actor.Body.RoomID {
			return "", fmt.Errorf("invalid room player target")
		}
		if !strings.EqualFold(candidate.Body.Name, name) {
			continue
		}
		found++
		if found == occurrence {
			return id, nil
		}
	}
	return "", fmt.Errorf("player target absent")
}

// SelectFollowerInOrder resolves 내보내's named target from the actor's
// authoritative first_fol projection. FollowerRefs is preferred when the C
// mixed list was imported; otherwise FollowerIDs remains authoritative for
// player followers. NPC entries are never guessed as player targets.
func (s State) SelectFollowerInOrder(actorID, name string, occurrence int) (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	if occurrence < 1 {
		return "", fmt.Errorf("follower target occurrence must be positive")
	}
	if !validRoomPlayerTargetName(strings.TrimSpace(name)) {
		return "", fmt.Errorf("invalid follower target")
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online {
		return "", fmt.Errorf("online follower target context required")
	}

	found := 0
	for _, ref := range mixedFollowerRefs(actor) {
		if ref.Kind != "player" {
			continue
		}
		follower, ok := s.Players[ref.ID]
		if !ok || !follower.Online || follower.FollowingID != actorID {
			continue
		}
		if !strings.EqualFold(follower.Body.Name, strings.TrimSpace(name)) {
			continue
		}
		found++
		if found == occurrence {
			return ref.ID, nil
		}
	}
	return "", fmt.Errorf("follower target absent")
}
