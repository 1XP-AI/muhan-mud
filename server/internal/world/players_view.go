package world

import "strings"

// RoomPlayerView is an ordered snapshot supplied by the world loop. ID denotes
// actor identity (not display name); no credentials or persistence pointers.
type RoomPlayerView struct {
	ID, Name, Description string
	Flags                 [8]byte
}

// RenderRoomPlayers ports display_rom's first_ply section, with ANSI omitted.
// Room visibility must be checked before rendering any occupant section.
func RenderRoomPlayers(players []RoomPlayerView, selfID string, detectInvisible bool, viewerClass byte, descriptions bool) string {
	var out strings.Builder
	var names []string
	for _, p := range players {
		if p.ID == selfID || flag(p.Flags[:], 1) || (!detectInvisible && flag(p.Flags[:], 2)) || (viewerClass < 12 && flag(p.Flags[:], 10)) {
			continue
		}
		if descriptions {
			out.WriteString(p.Name + "님이 " + p.Description + "서 있습니다.\n")
		} else {
			names = append(names, p.Name)
		}
	}
	if len(names) > 0 {
		out.WriteString(strings.Join(names, ", ") + "님이 서 있습니다.\n")
	}
	return out.String()
}
