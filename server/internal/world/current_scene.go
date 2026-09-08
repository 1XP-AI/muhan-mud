package world

import "fmt"

// CurrentScene renders the no-target look snapshot. Hour is server-owned game
// time. Target inspection and combat notices remain separate pending work.
func (s State) CurrentScene(actorID string, hour int) (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	p, ok := s.Players[actorID]
	if !ok || !p.Online {
		return "", fmt.Errorf("online viewer absent")
	}
	room := s.Rooms[p.Body.RoomID]
	resource, err := s.ProjectRoom(p.Body.RoomID)
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
			lit, err := hasLight(s.Players[id])
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
	views, err := s.RoomPlayers(p.Body.RoomID)
	if err != nil {
		return "", err
	}
	options := SceneOptions{ViewOptions: ViewOptions{Hour: hour, Race: p.Body.Race, Class: p.Body.Class, Blind: flag(f, 42), HasLight: light, OtherPlayerLight: otherLight, HideName: flag(f, 6), HideShort: flag(f, 5), HideLong: flag(f, 4), ExitDiagram: flag(f, 7), DetectInvisible: flag(f, 21)}, ViewerID: actorID, KnowAlignment: flag(f, 33), DetectMagic: flag(f, 20), PlayerDescriptions: flag(f, 63)}
	return RenderRoomScene(room.Resource, views, options), nil
}
