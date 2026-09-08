package world

type SceneOptions struct {
	ViewOptions
	ViewerID                                       string
	KnowAlignment, DetectMagic, PlayerDescriptions bool
}

// RenderRoomScene joins the non-combat room sections in legacy order. It is
// an immutable snapshot renderer, not a command handler or a live world loop.
// Combat notices and target-specific look remain to be ported.
func RenderRoomScene(room LegacyRoom, players []RoomPlayerView, v SceneOptions) string {
	output := RenderRoomEnvironment(room, v.ViewOptions)
	if !roomVisible(room, v.ViewOptions) {
		return output
	}
	output += RenderRoomPlayers(players, v.ViewerID, v.DetectInvisible, v.Class, v.PlayerDescriptions)
	output += RenderRoomMonsters(room.Monsters, v.DetectInvisible, v.KnowAlignment)
	output += RenderRoomObjects(room.Objects, v.DetectInvisible, v.DetectMagic)
	return output
}
