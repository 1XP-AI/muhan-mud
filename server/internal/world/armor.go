package world

import "fmt"

var legacyStatBonus = [64]int{
	-4, -4, -4, -3, -3, -2, -2, -1, -1, -1, 0, 0, 0, 0, 1, 1,
	1, 2, 2, 2, 3, 3, 3, 3, 4, 4, 4, 4, 4, 5, 5, 5,
	5, 5, 5, 6, 6, 6, 6, 6, 6, 6, 6, 6, 7, 7, 7, 7,
	7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7,
}

// ComputeArmorClass ports compute_ac using a read-only ready-slot projection.
// Armor bytes are signed legacy adjustments. Negative legacy dexterity is
// rejected rather than reproducing C's negative bonus-array index.
func ComputeArmorClass(dexterity byte, ready [20]*LegacyObject, protected bool) (int8, error) {
	if int8(dexterity) < 0 {
		return 0, fmt.Errorf("negative legacy dexterity")
	}
	index := int(dexterity)
	if index > 63 {
		index = 63
	}
	ac := 100 - 5*legacyStatBonus[index]
	for _, item := range ready {
		if item != nil {
			ac -= int(int8(item.Armor))
		}
	}
	if protected {
		ac -= 10
	}
	if ac < -127 {
		ac = -127
	}
	if ac > 127 {
		ac = 127
	}
	return int8(ac), nil
}
