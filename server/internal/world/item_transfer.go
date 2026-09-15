package world

import "fmt"

type ItemTransferPlan struct{ Source, Destination ItemCollection }

// TransferItemRoots moves whole inventory-root subtrees without allocating IDs.
// The caller must commit BOTH returned owners together. Equipped items must be
// explicitly unequipped/partitioned first (e.g. PlanDeathEquipment).
func TransferItemRoots(source, destination ItemCollection, roots []string) (ItemTransferPlan, error) {
	if err := source.Validate(); err != nil {
		return ItemTransferPlan{}, err
	}
	if err := destination.Validate(); err != nil {
		return ItemTransferPlan{}, err
	}
	for id := range source.Items {
		if _, exists := destination.Items[id]; exists {
			return ItemTransferPlan{}, fmt.Errorf("overlapping item owners")
		}
	}
	available := make(map[string]bool, len(source.Inventory))
	for _, id := range source.Inventory {
		available[id] = true
	}
	selected := make(map[string]bool, len(roots))
	for _, id := range roots {
		if !available[id] || selected[id] {
			return ItemTransferPlan{}, fmt.Errorf("invalid transfer root")
		}
		selected[id] = true
	}
	r := ItemTransferPlan{Source: source.clone(), Destination: destination.clone()}
	r.Source.Inventory = nil
	for _, id := range source.Inventory {
		if !selected[id] {
			r.Source.Inventory = append(r.Source.Inventory, id)
		}
	}
	for _, root := range roots {
		stack := []string{root}
		for len(stack) > 0 {
			id := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			item := r.Source.Items[id]
			r.Destination.Items[id] = item
			delete(r.Source.Items, id)
			stack = append(stack, item.Contents...)
		}
		// Original add_obj_rom ordering: name, then signed adjustment;
		// insert after existing equal entries, preserving request order.
		object := r.Destination.Items[root].Object
		at := len(r.Destination.Inventory)
		for i, id := range r.Destination.Inventory {
			other := r.Destination.Items[id].Object
			if other.Name > object.Name || (other.Name == object.Name && int8(other.Adjustment) > int8(object.Adjustment)) {
				at = i
				break
			}
		}
		r.Destination.Inventory = append(r.Destination.Inventory, "")
		copy(r.Destination.Inventory[at+1:], r.Destination.Inventory[at:])
		r.Destination.Inventory[at] = root
	}
	if err := r.Source.Validate(); err != nil {
		return ItemTransferPlan{}, err
	}
	if err := r.Destination.Validate(); err != nil {
		return ItemTransferPlan{}, err
	}
	return r, nil
}
