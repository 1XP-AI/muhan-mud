package world

import "fmt"

// EquipmentPlan references the unchanged input inventory by index. -1 means an
// empty ready slot. Remaining and Removed preserve input order. No objects are
// deleted here: the eventual entity-ID transaction must apply this partition.
type EquipmentPlan struct {
	Ready              [20]int
	Remaining, Removed []int
}

// PlanLoginEquipment ports init_ply's inventory-to-ready loop. Starting ready
// slots are empty, as after legacy loading. This is not a live wear command.
// Invalid wearable indices abort instead of reproducing C's out-of-bounds access.
func PlanLoginEquipment(inventory []LegacyObject, level, class byte) (EquipmentPlan, error) {
	var out EquipmentPlan
	for i := range out.Ready {
		out.Ready[i] = -1
	}
	for index, o := range inventory {
		if flag(o.Flags[:], 46) && !flag(o.Flags[:], 50) {
			out.Removed = append(out.Removed, index)
			continue
		}
		eligible := o.ShotsCurrent >= 1 && o.ShotsCurrent <= 1500 && o.ShotsMax <= 1500
		ac := int(int8(o.Armor)) * 5
		if o.Wear == 1 {
			ac = int(int8(o.Armor)) * 2
		}
		if ac > 150 {
			eligible = false
		}
		if ac < 30 {
			ac = 0
		}
		if o.Quest == 0 && ac > 30 && int8(class) < 9 && int(level) < ac {
			eligible = false
		}
		if int8(o.Type) < 5 {
			damage := int64(o.DiceCount)*int64(o.DiceSides) + int64(o.DicePlus)
			if damage > 100 {
				eligible = false
			}
			switch class {
			case 4:
				damage -= 7
			case 1, 8:
				damage -= 3
			case 6, 7:
				damage -= 2
			}
			if damage > 15 && int8(class) < 9 && int64(level) < damage*3 && o.Quest == 0 {
				eligible = false
			}
		}
		if !eligible || !flag(o.Flags[:], 23) {
			out.Remaining = append(out.Remaining, index)
			continue
		}
		if o.Wear < 1 || o.Wear > 20 {
			return EquipmentPlan{}, fmt.Errorf("invalid legacy wear position")
		}
		first, last := int(o.Wear)-1, int(o.Wear)-1
		switch o.Wear {
		case 9:
			first, last = 8, 15
		case 4:
			first, last = 3, 4
		case 20:
			if flag(o.Flags[:], 49) {
				first, last = 16, 16
			}
		}
		placed := false
		for slot := first; slot <= last; slot++ {
			if out.Ready[slot] < 0 {
				out.Ready[slot] = index
				placed = true
				break
			}
		}
		if !placed {
			out.Remaining = append(out.Remaining, index)
		}
	}
	return out, nil
}
