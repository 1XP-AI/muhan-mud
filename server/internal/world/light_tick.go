package world

type LightTickResult struct {
	Items ItemCollection
	// Used for actor/room messages after committing the entire player update.
	ExtinguishedID string
}

// TickLight ports has_light + update_ply's final consumable-light decrement.
// PLIGHT overrides equipped lights. The first usable OLIGHT slot wins even if
// it is not consumable; only type LIGHTSOURCE loses a charge. Not a whole tick.
func TickLight(player LegacyMonster, items ItemCollection) (LightTickResult, error) {
	if err := items.Validate(); err != nil {
		return LightTickResult{}, err
	}
	r := LightTickResult{Items: items.clone()}
	if flag(player.Flags[:], 17) {
		return r, nil
	}
	for _, id := range items.Ready {
		if id == "" {
			continue
		}
		item := items.Items[id]
		if !flag(item.Object.Flags[:], 11) {
			continue
		}
		if item.Object.Type != 12 {
			return r, nil
		}
		if item.Object.ShotsCurrent <= 0 {
			continue
		}
		item.Object.ShotsCurrent--
		// Keep the cloned relationship slice, not the original input's slice.
		copyItem := r.Items.Items[id]
		copyItem.Object = item.Object
		r.Items.Items[id] = copyItem
		if item.Object.ShotsCurrent == 0 {
			r.ExtinguishedID = id
		}
		return r, nil
	}
	return r, nil
}
