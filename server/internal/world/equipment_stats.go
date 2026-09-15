package world

type CombatStats struct{ Armor, Thaco int8 }

// CombatStats projects canonical ready IDs into the legacy-compatible pure
// calculators. Inventory and container contents never contribute as equipment.
// Invalid ownership/stat data returns no partially calculated stats.
func (c ItemCollection) CombatStats(player LegacyMonster) (CombatStats, error) {
	if err := c.Validate(); err != nil {
		return CombatStats{}, err
	}
	var ready [20]*LegacyObject
	for slot, id := range c.Ready {
		if id != "" {
			object := c.Items[id].Object
			ready[slot] = &object
		}
	}
	armor, err := ComputeArmorClass(player.Stats[1], ready, flag(player.Flags[:], 8))
	if err != nil {
		return CombatStats{}, err
	}
	thaco, err := ComputeThaco(player, ready[19])
	if err != nil {
		return CombatStats{}, err
	}
	return CombatStats{Armor: armor, Thaco: thaco}, nil
}
