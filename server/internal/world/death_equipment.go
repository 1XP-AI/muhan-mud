package world

// These rules come from the authoritative death context, not a client. Self
// deaths also have AttackerPlayer=true in the original die(player,player).
type DeathEquipmentRules struct{ Survival, WarAllowsLoss, AttackerPlayer bool }
type DeathEquipmentPlan struct{ Kept, Dropped ItemCollection }

// PlanDeathEquipment partitions items without changing their IDs. Dropped is
// a transfer payload whose Inventory lists floor-bound roots, not a persisted
// second owner. Commit its merge into room items with Kept and the rest of death.
// It does not transfer gold, inventory roots or perform respawn/save/broadcast.
func PlanDeathEquipment(items ItemCollection, rules DeathEquipmentRules) (DeathEquipmentPlan, error) {
	if err := items.Validate(); err != nil {
		return DeathEquipmentPlan{}, err
	}
	r := DeathEquipmentPlan{Kept: items.clone(), Dropped: ItemCollection{Items: map[string]Item{}}}
	if rules.Survival || !rules.WarAllowsLoss {
		return r, nil
	}
	insert := func(c *ItemCollection, id string) {
		object := c.Items[id].Object
		at := len(c.Inventory)
		for i, otherID := range c.Inventory {
			other := c.Items[otherID].Object
			if other.Name > object.Name || (other.Name == object.Name && int8(other.Adjustment) > int8(object.Adjustment)) {
				at = i
				break
			}
		}
		c.Inventory = append(c.Inventory, "")
		copy(c.Inventory[at+1:], c.Inventory[at:])
		c.Inventory[at] = id
	}
	drop := func(root string, temporary bool) {
		stack := []string{root}
		for len(stack) > 0 {
			id := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			item := r.Kept.Items[id]
			if temporary {
				item.Object.Flags[1] |= 3
			}
			r.Dropped.Items[id] = item
			stack = append(stack, item.Contents...)
			delete(r.Kept.Items, id)
		}
		insert(&r.Dropped, root)
	}
	protected := func(id string) bool {
		o := r.Kept.Items[id].Object
		return o.Quest != 0 || flag(o.Flags[:], 50) || flag(o.Flags[:], 22)
	}
	if id := r.Kept.Ready[19]; id != "" && !protected(id) && r.Kept.Items[id].Object.Flags[1]&2 == 0 {
		// C uses F_ISSET(item,CONTAINER), where CONTAINER numerically equals
		// OPERM2 (bit 9). Do not replace this with item.Type==CONTAINER.
		drop(id, true)
		r.Kept.Ready[19] = ""
	}
	for slot, id := range r.Kept.Ready {
		if id == "" || protected(id) {
			continue
		}
		if rules.AttackerPlayer {
			drop(id, false)
		} else {
			insert(&r.Kept, id)
		}
		r.Kept.Ready[slot] = ""
	}
	if err := r.Kept.Validate(); err != nil {
		return DeathEquipmentPlan{}, err
	}
	if err := r.Dropped.Validate(); err != nil {
		return DeathEquipmentPlan{}, err
	}
	return r, nil
}
