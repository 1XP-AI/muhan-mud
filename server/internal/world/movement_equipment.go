package world

import "fmt"

type MovementEquipment struct {
	CarriedWeight             int
	FallSkill, DexterityBonus int32
}

// MovementStats ports weight_ply/weight_obj/fall_ply using canonical item IDs.
// Weightless inventory roots and child subtrees are skipped. A weightless READY
// root is still counted, as in C. Only ready OCLIMB objects affect fall skill.
// Unsupported bonus indexes and signed-32 overflow are errors, not C UB/wrap.
func (c ItemCollection) MovementStats(dexterity byte) (MovementEquipment, error) {
	if err := c.Validate(); err != nil {
		return MovementEquipment{}, err
	}
	if dexterity >= 64 {
		return MovementEquipment{}, fmt.Errorf("movement dexterity outside bonus table")
	}
	bonus := int32(legacyStatBonus[dexterity])
	result := MovementEquipment{DexterityBonus: bonus, FallSkill: bonus * 5}
	var stack []string
	for _, id := range c.Inventory {
		object := c.Items[id].Object
		if !flag(object.Flags[:], 7) {
			stack = append(stack, id)
		}
	}
	for _, id := range c.Ready {
		if id != "" {
			stack = append(stack, id)
			object := c.Items[id].Object
			if flag(object.Flags[:], 16) {
				result.FallSkill += int32(object.DicePlus) * 3
			}
		}
	}
	var weight int64
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		item := c.Items[id]
		weight += int64(item.Object.Weight)
		if weight < -2147483648 || weight > 2147483647 {
			return MovementEquipment{}, fmt.Errorf("carried weight overflow")
		}
		for _, child := range item.Contents {
			object := c.Items[child].Object
			if !flag(object.Flags[:], 7) {
				stack = append(stack, child)
			}
		}
	}
	result.CarriedWeight = int(weight)
	return result, nil
}
