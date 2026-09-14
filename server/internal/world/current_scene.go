package world

import (
	"errors"
	"fmt"
	"strings"
)

// CurrentScene renders the no-target look snapshot of the actor's room.
// Hour is server-owned game time. display_rom combat-in-progress notices
// follow floor objects. Target inspection of creatures/objects is separate.
// command2.c look-through-exit uses SceneAt so the actor can view a
// destination without moving.
func (s State) CurrentScene(actorID string, hour int) (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	p, ok := s.Players[actorID]
	if !ok || !p.Online {
		return "", fmt.Errorf("online viewer absent")
	}
	return s.SceneAt(actorID, p.Body.RoomID, hour)
}

// SceneAt renders display_rom for roomID using actorID as the viewer. The
// actor does not have to occupy that room; command2.c look() peeks a loaded
// destination this way. Unmigrated lighting equipment still fails closed
// when the destination is actually dark.
func (s State) SceneAt(actorID string, roomID int16, hour int) (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	p, ok := s.Players[actorID]
	if !ok || !p.Online {
		return "", fmt.Errorf("online viewer absent")
	}
	room, ok := s.Rooms[roomID]
	if !ok {
		return "", fmt.Errorf("look scene room absent")
	}
	resource, err := s.ProjectRoom(roomID)
	if err != nil {
		return "", err
	}
	room.Resource = resource
	if room.Items != nil {
		objects, err := room.Items.LegacyInventory()
		if err != nil {
			return "", err
		}
		room.Resource.Objects = objects
	}
	hasLight := func(p PlayerState) (bool, error) {
		if flag(p.Body.Flags[:], 17) {
			return true, nil
		}
		if p.Items == nil {
			return false, fmt.Errorf("viewer equipment not migrated")
		}
		for _, id := range p.Items.Ready {
			if id != "" {
				o := p.Items.Items[id].Object
				if flag(o.Flags[:], 11) && (o.Type != 12 || o.ShotsCurrent > 0) {
					return true, nil
				}
			}
		}
		return false, nil
	}
	f := p.Body.Flags[:]
	base := ViewOptions{Hour: hour, Race: p.Body.Race, Class: p.Body.Class, Blind: flag(f, 42)}
	light, otherLight := false, false
	// Equipment uncertainty matters only when the actual display depends on
	// light. Blindness, daylight and intrinsic night sight need no light scan.
	if !base.Blind && !roomVisible(room.Resource, base) {
		var unresolved error
		ids := append([]string{actorID}, room.PlayerIDs...)
		for i, id := range ids {
			if i > 0 && id == actorID {
				continue
			}
			occupant, exists := s.Players[id]
			if !exists {
				unresolved = fmt.Errorf("viewer equipment not migrated")
				continue
			}
			lit, err := hasLight(occupant)
			if err != nil {
				unresolved = err
				continue
			}
			if lit {
				light, otherLight = id == actorID, id != actorID
				break
			}
		}
		if !light && !otherLight && unresolved != nil {
			return "", unresolved
		}
	}
	views, err := s.RoomPlayers(roomID)
	if err != nil {
		return "", err
	}
	options := SceneOptions{ViewOptions: ViewOptions{Hour: hour, Race: p.Body.Race, Class: p.Body.Class, Blind: flag(f, 42), HasLight: light, OtherPlayerLight: otherLight, HideName: flag(f, 6), HideShort: flag(f, 5), HideLong: flag(f, 4), ExitDiagram: flag(f, 7), DetectInvisible: flag(f, 21)}, ViewerID: actorID, KnowAlignment: flag(f, 33), DetectMagic: flag(f, 20), PlayerDescriptions: flag(f, 63)}
	output := RenderRoomScene(room.Resource, views, options)
	if !roomVisible(room.Resource, options.ViewOptions) {
		return output, nil
	}
	notices, err := s.renderDisplayRomCombatNotices(actorID, roomID, options.DetectInvisible, options.DetectMagic)
	if err != nil {
		return "", err
	}
	return output + notices, nil
}

const (
	combatInvisibleFlag    = 2  // PINVIS / MINVIS
	combatDMInvisibleFlag  = 10 // PDMINV
	combatMonsterMagicFlag = 17 // MMAGIC
	combatCaretakerClass   = 10
)

// ErrRoomCombatUnmigrated is returned when display_rom would walk first_enm
// but canonical enemy state is missing or still a migration sentinel.
var ErrRoomCombatUnmigrated = errors.New("room combat notices unmigrated")

// renderDisplayRomCombatNotices ports room.c:display_rom's trailing first_mon
// first_enm loop. find_crt is limited to this room's first_ply; a miss prints
// nothing. Nil Enemies, negative Damage, and unimported Resource.Monsters
// fail closed instead of looking peaceful.
func (s State) renderDisplayRomCombatNotices(actorID string, roomID int16, detectInvisible, detectMagic bool) (string, error) {
	room, ok := s.Rooms[roomID]
	if !ok {
		return "", fmt.Errorf("look scene room absent")
	}
	if s.NPCs == nil {
		if len(room.Resource.Monsters) > 0 {
			return "", fmt.Errorf("%w: legacy room monsters", ErrRoomCombatUnmigrated)
		}
		return "", nil
	}
	var out strings.Builder
	for _, npcID := range room.NPCIDs {
		npc, exists := s.NPCs[npcID]
		if !exists || npcID == "" {
			return "", fmt.Errorf("%w: unresolved NPC identity", ErrRoomCombatUnmigrated)
		}
		if npc.Enemies == nil {
			return "", fmt.Errorf("%w: NPC %s enemies", ErrRoomCombatUnmigrated, npcID)
		}
		if len(npc.Enemies) == 0 {
			continue
		}
		first := npc.Enemies[0]
		if first.Damage < 0 {
			return "", fmt.Errorf("%w: NPC %s enemy damage", ErrRoomCombatUnmigrated, npcID)
		}
		if first.Target.Kind != "player" {
			continue
		}
		target, found := s.findDisplayRomCombatPlayer(room, first.Target.ID, detectInvisible)
		if !found {
			continue
		}
		monster := combatMonsterLabel(npc.Body.Name, detectMagic && flag(npc.Body.Flags[:], combatMonsterMagicFlag))
		out.WriteString(monster)
		out.WriteString(combatHangulParticle(monster, "이", "가"))
		if first.Target.ID == actorID {
			out.WriteString(" 당신과 싸우고 있습니다.\n")
			continue
		}
		player := combatPlayerLabel(target.Name, flag(target.Flags[:], combatInvisibleFlag))
		out.WriteString(" ")
		out.WriteString(player)
		out.WriteString(combatHangulParticle(player, "과", "와"))
		out.WriteString(" 싸우고 있습니다.\n")
	}
	return out.String(), nil
}

func (s State) findDisplayRomCombatPlayer(room RoomState, playerID string, detectInvisible bool) (RoomPlayerView, bool) {
	inRoom := false
	for _, id := range room.PlayerIDs {
		if id == playerID {
			inRoom = true
			break
		}
	}
	if !inRoom {
		return RoomPlayerView{}, false
	}
	p, ok := s.Players[playerID]
	if !ok || !p.Online {
		return RoomPlayerView{}, false
	}
	if p.Body.Class >= combatCaretakerClass && flag(p.Body.Flags[:], combatDMInvisibleFlag) {
		return RoomPlayerView{}, false
	}
	if flag(p.Body.Flags[:], combatInvisibleFlag) && !detectInvisible {
		return RoomPlayerView{}, false
	}
	return RoomPlayerView{ID: playerID, Name: p.Body.Name, Flags: p.Body.Flags}, true
}
