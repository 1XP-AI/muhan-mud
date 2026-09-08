package game

// Choices validates a decoded creation draft by reconstructing its original
// choices and requiring an exact round trip through BuildCreation. This is only
// for pre-level drafts, never for evolved or imported game characters.
func (d Creation) Choices() (CreationChoices, error) {
	choice := CreationChoices{Male: d.Male, Chaotic: d.Chaotic, Class: d.Class, Stats: d.Stats}
	// Bound arithmetic before reversing racial offsets, including hostile ints.
	for _, value := range d.Stats {
		if value < 2 || value > 20 {
			return CreationChoices{}, ErrCreation
		}
	}
	for i, value := range d.Proficiency {
		if value == 0 {
			continue
		}
		if value != 1024 || choice.Weapon != 0 {
			return CreationChoices{}, ErrCreation
		}
		choice.Weapon = i + 1
	}
	// Derive offsets from the authoritative constructor, not a second race table.
	for race := 1; race <= 8; race++ {
		baseline, err := BuildCreation(CreationChoices{Class: 1, Weapon: 1, RaceChoice: race, Stats: Stats{10, 10, 10, 10, 10}})
		if err != nil {
			return CreationChoices{}, err
		}
		if baseline.RaceID != d.RaceID {
			continue
		}
		choice.RaceChoice = race
		for i, value := range baseline.Stats {
			choice.Stats[i] -= value - 10
		}
		break
	}
	rebuilt, err := BuildCreation(choice)
	if err != nil || rebuilt != d {
		return CreationChoices{}, ErrCreation
	}
	return choice, nil
}
