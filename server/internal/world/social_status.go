package world

import (
	"fmt"
	"sort"
)

const (
	playerInvisibleFlag   = 2
	playerDMInvisibleFlag = 10
	playerDetectFlag      = 21
	playerDMClass         = 12
	playerSubDMClass      = 11
)

func orderedOnlinePlayers(s State) []string {
	seen := make(map[string]bool, len(s.Players))
	ids := make([]string, 0, len(s.Players))
	roomIDs := make([]int, 0, len(s.Rooms))
	for id := range s.Rooms {
		roomIDs = append(roomIDs, int(id))
	}
	sort.Ints(roomIDs)
	for _, roomID := range roomIDs {
		for _, id := range s.Rooms[int16(roomID)].PlayerIDs {
			if p, ok := s.Players[id]; ok && p.Online && !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	remaining := make([]string, 0, len(s.Players))
	for id, p := range s.Players {
		if p.Online && !seen[id] {
			remaining = append(remaining, id)
		}
	}
	sort.Strings(remaining)
	return append(ids, remaining...)
}

func visibleToWho(viewer, target PlayerState, viewerID, targetID string) bool {
	if targetID == viewerID {
		return true
	}
	if flag(target.Body.Flags[:], playerDMInvisibleFlag) && (target.Body.Class == playerDMClass || viewer.Body.Class < playerSubDMClass) {
		return false
	}
	if flag(target.Body.Flags[:], playerInvisibleFlag) && !flag(viewer.Body.Flags[:], playerDetectFlag) && viewer.Body.Class < playerSubDMClass {
		return false
	}
	return true
}

// PlayerWho renders a deterministic semantic online-player list. Descriptor
// order from C is unavailable in a snapshot, so room membership order followed
// by sorted residual IDs is explicit rather than map iteration.
func (s State) PlayerWho(actorID string) (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	viewer, ok := s.Players[actorID]
	if !ok || !viewer.Online {
		return "", fmt.Errorf("online who viewer absent")
	}
	if flag(viewer.Body.Flags[:], playerBlindFlag) {
		return "당신은 눈이 멀어 있습니다!\r\n", nil
	}
	lines := []string{"사용자          레벨 직업\r\n", "------------------------------\r\n"}
	for _, id := range orderedOnlinePlayers(s) {
		p := s.Players[id]
		if !visibleToWho(viewer, p, actorID, id) {
			continue
		}
		marker := "   "
		if flag(p.Body.Flags[:], playerInvisibleFlag) || flag(p.Body.Flags[:], playerDMInvisibleFlag) {
			marker = "(*)"
		}
		lines = append(lines, fmt.Sprintf("%-14s%s [레벨 %d 직업 %d]\r\n", p.Body.Name, marker, p.Body.Level, p.Body.Class))
	}
	return joinStrings(lines), nil
}

func joinStrings(values []string) string {
	var out string
	for _, value := range values {
		out += value
	}
	return out
}

// PlayerGroup renders the player/NPC first_fol group around the actor. It is
// read-only and keeps the canonical mixed follower order when it is available.
func (s State) PlayerGroup(actorID string) (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online {
		return "", fmt.Errorf("online group viewer absent")
	}
	leaderID := actorID
	if actor.FollowingID != "" {
		leaderID = actor.FollowingID
	}
	leader, ok := s.Players[leaderID]
	if !ok {
		return "", fmt.Errorf("group leader absent")
	}
	refs := mixedFollowerRefs(leader)
	if len(refs) == 0 {
		return "당신은 그룹에 속해 있지 않습니다.\r\n", nil
	}
	var out string
	out += "그룹원:\r\n"
	out += fmt.Sprintf("  %-14s  체력:%4d 도력:%4d (대장)\r\n", leader.Body.Name, leader.Body.HPCurrent, leader.Body.MPCurrent)
	shown := 0
	for _, ref := range refs {
		var name string
		var hp, mp int16
		switch ref.Kind {
		case "player":
			follower, exists := s.Players[ref.ID]
			if !exists || flag(follower.Body.Flags[:], playerDMInvisibleFlag) {
				continue
			}
			name, hp, mp = follower.Body.Name, follower.Body.HPCurrent, follower.Body.MPCurrent
		case "npc":
			npc, exists := s.NPCs[ref.ID]
			if !exists {
				return "", fmt.Errorf("group NPC follower absent")
			}
			name, hp, mp = npc.Body.Name, npc.Body.HPCurrent, npc.Body.MPCurrent
		default:
			return "", fmt.Errorf("unknown group follower kind")
		}
		out += fmt.Sprintf("  %-14s  체력:%4d 도력:%4d\r\n", name, hp, mp)
		shown++
	}
	if shown == 0 {
		return "당신은 그룹에 속해 있지 않습니다.\r\n", nil
	}
	return out, nil
}
