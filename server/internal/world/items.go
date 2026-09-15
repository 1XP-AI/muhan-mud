package world

import "fmt"

const (
	itemContainerFlag  = 6 // OCONTN
	itemWeightlessFlag = 7 // OWTLES
)

type Item struct {
	Object   LegacyObject
	Contents []string
}

// ItemCollection gives each object one stable identity and exactly one owner:
// inventory root, ready slot, or containing item. Object.Contents must be empty;
// relationships live only in the ID lists, never duplicated nested values.
type ItemCollection struct {
	Items     map[string]Item
	Inventory []string
	Ready     [20]string
}

func (c ItemCollection) Validate() error {
	if c.Items == nil || len(c.Items) > 100000 {
		return fmt.Errorf("invalid item collection size")
	}
	stack := append([]string(nil), c.Inventory...)
	for _, id := range c.Ready {
		if id != "" {
			stack = append(stack, id)
		}
	}
	seen := make(map[string]bool, len(c.Items))
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		item, ok := c.Items[id]
		if id == "" || !ok || seen[id] || len(item.Object.Contents) != 0 {
			return fmt.Errorf("invalid or duplicate item ownership")
		}
		seen[id] = true
		stack = append(stack, item.Contents...)
	}
	if len(seen) != len(c.Items) {
		return fmt.Errorf("orphaned item")
	}
	return nil
}

// Weight ports weight_obj/weight_ply for canonical item trees. Weightless
// containers suppress the weight of their complete child subtree, while ready
// roots are always counted just as the legacy ready loop does.
func (c ItemCollection) Weight() (int, error) {
	if err := c.Validate(); err != nil {
		return 0, err
	}
	var weight func(string, map[string]bool) (int, error)
	weight = func(id string, stack map[string]bool) (int, error) {
		if stack[id] {
			return 0, fmt.Errorf("cyclic item tree")
		}
		item, ok := c.Items[id]
		if !ok {
			return 0, fmt.Errorf("missing item")
		}
		nextStack := make(map[string]bool, len(stack)+1)
		for key, active := range stack {
			nextStack[key] = active
		}
		nextStack[id] = true
		total := int(item.Object.Weight)
		if !flag(item.Object.Flags[:], itemWeightlessFlag) {
			for _, child := range item.Contents {
				childWeight, err := weight(child, nextStack)
				if err != nil {
					return 0, err
				}
				total += childWeight
			}
		}
		return total, nil
	}
	total := 0
	for _, id := range c.Inventory {
		itemWeight, err := weight(id, nil)
		if err != nil {
			return 0, err
		}
		item := c.Items[id]
		if !flag(item.Object.Flags[:], itemWeightlessFlag) {
			total += itemWeight
		}
	}
	for _, id := range c.Ready {
		if id == "" {
			continue
		}
		itemWeight, err := weight(id, nil)
		if err != nil {
			return 0, err
		}
		total += itemWeight
	}
	return total, nil
}

func (c ItemCollection) objectWeight(root string) (int, error) {
	if err := c.Validate(); err != nil {
		return 0, err
	}
	var weight func(string, map[string]bool) (int, error)
	weight = func(id string, stack map[string]bool) (int, error) {
		if stack[id] {
			return 0, fmt.Errorf("cyclic item tree")
		}
		item, ok := c.Items[id]
		if !ok {
			return 0, fmt.Errorf("missing item")
		}
		nextStack := make(map[string]bool, len(stack)+1)
		for key, active := range stack {
			nextStack[key] = active
		}
		nextStack[id] = true
		total := int(item.Object.Weight)
		if !flag(item.Object.Flags[:], itemWeightlessFlag) {
			for _, child := range item.Contents {
				childWeight, err := weight(child, nextStack)
				if err != nil {
					return 0, err
				}
				total += childWeight
			}
		}
		return total, nil
	}
	return weight(root, nil)
}

// CapacityCount matches count_inv(..., -1) plus MAXWEAR occupancy. It counts
// direct roots and direct children of containers, not every deeply nested
// descendant, and is capped at the legacy 200-item reporting limit.
func (c ItemCollection) CapacityCount() (int, error) {
	if err := c.Validate(); err != nil {
		return 0, err
	}
	count := len(c.Inventory)
	for _, id := range c.Inventory {
		item := c.Items[id]
		if flag(item.Object.Flags[:], itemContainerFlag) {
			count += len(item.Contents)
		}
	}
	for _, id := range c.Ready {
		if id != "" {
			count++
		}
	}
	if count > 200 {
		count = 200
	}
	return count, nil
}

// ImportItems is an explicit migration step. The caller owns deterministic ID
// allocation/retry mapping; this function does not mint identities on login.
func ImportItems(objects []LegacyObject, allocate func() (string, error)) (ItemCollection, error) {
	if allocate == nil {
		return ItemCollection{}, fmt.Errorf("missing item ID allocator")
	}
	c := ItemCollection{Items: map[string]Item{}}
	var visit func(LegacyObject, int) (string, error)
	visit = func(object LegacyObject, depth int) (string, error) {
		if depth > 64 || len(c.Items) >= 100000 {
			return "", fmt.Errorf("item migration limit exceeded")
		}
		id, err := allocate()
		if err != nil {
			return "", err
		}
		if _, exists := c.Items[id]; exists || id == "" {
			return "", fmt.Errorf("invalid allocated item ID")
		}
		children := object.Contents
		object.Contents = nil
		item := Item{Object: object}
		c.Items[id] = item
		for _, child := range children {
			childID, err := visit(child, depth+1)
			if err != nil {
				return "", err
			}
			item.Contents = append(item.Contents, childID)
		}
		c.Items[id] = item
		return id, nil
	}
	for _, object := range objects {
		id, err := visit(object, 0)
		if err != nil {
			return ItemCollection{}, err
		}
		c.Inventory = append(c.Inventory, id)
	}
	return c, nil
}

func (c ItemCollection) clone() ItemCollection {
	next := ItemCollection{Items: make(map[string]Item, len(c.Items)), Inventory: append([]string(nil), c.Inventory...), Ready: c.Ready}
	for id, item := range c.Items {
		item.Contents = append([]string(nil), item.Contents...)
		next.Items[id] = item
	}
	return next
}

// RestoreEquipment applies legacy import classification once, to empty ready
// slots. Returns removed subtree IDs for a durable migration audit. It neither
// deletes external data nor mutates c. Live wear/unwear is a separate command.
func (c ItemCollection) RestoreEquipment(level, class byte) (ItemCollection, []string, error) {
	if err := c.Validate(); err != nil {
		return ItemCollection{}, nil, err
	}
	for _, id := range c.Ready {
		if id != "" {
			return ItemCollection{}, nil, fmt.Errorf("equipment already restored")
		}
	}
	objects := make([]LegacyObject, len(c.Inventory))
	for i, id := range c.Inventory {
		objects[i] = c.Items[id].Object
	}
	plan, err := PlanLoginEquipment(objects, level, class)
	if err != nil {
		return ItemCollection{}, nil, err
	}
	next := c.clone()
	next.Inventory = nil
	for _, index := range plan.Remaining {
		next.Inventory = append(next.Inventory, c.Inventory[index])
	}
	for slot, index := range plan.Ready {
		if index >= 0 {
			next.Ready[slot] = c.Inventory[index]
		}
	}
	var removed []string
	for _, index := range plan.Removed {
		stack := []string{c.Inventory[index]}
		for len(stack) > 0 {
			id := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			item := next.Items[id]
			for i := len(item.Contents) - 1; i >= 0; i-- {
				stack = append(stack, item.Contents[i])
			}
			removed = append(removed, id)
			delete(next.Items, id)
		}
	}
	if err := next.Validate(); err != nil {
		return ItemCollection{}, nil, err
	}
	return next, removed, nil
}
