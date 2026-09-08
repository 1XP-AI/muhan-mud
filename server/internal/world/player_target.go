package world

import (
	"fmt"
	"strings"
)

// SelectPlayerInRoom resolves a display name against authoritative room
// membership. Exact matching keeps a terminal command from silently choosing
// a different same-prefix player while the legacy occurrence parser is still
// being ported.
func (s State) SelectPlayerInRoom(actorID, name string) (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online {
		return "", fmt.Errorf("online player target context required")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("player target required")
	}
	for _, id := range s.Rooms[actor.Body.RoomID].PlayerIDs {
		if id != actorID && strings.EqualFold(s.Players[id].Body.Name, name) {
			return id, nil
		}
	}
	return "", fmt.Errorf("player target absent")
}
