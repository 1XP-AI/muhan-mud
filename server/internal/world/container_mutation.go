package world

import (
	"fmt"
	"strings"
)

func selectNamedContainer(c ItemCollection, ids []string, name string, occurrence int, visible func(LegacyObject) bool) (string, error) {
	if occurrence < 1 {
		return "", fmt.Errorf("invalid container occurrence")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("container name required")
	}
	found := 0
	for _, id := range ids {
		if id == "" {
			continue
		}
		item, ok := c.Items[id]
		if !ok || !flag(item.Object.Flags[:], itemContainerFlag) || !strings.EqualFold(item.Object.Name, name) || (visible != nil && !visible(item.Object)) {
			continue
		}
		found++
		if found == occurrence {
			return id, nil
		}
	}
	return "", fmt.Errorf("container not found")
}

// selectContainerRoot walks one inventory list, matching C find_obj on a
// single otag chain. Ready/worn is a separate match space.
func selectContainerRoot(c ItemCollection, name string, occurrence int, visible func(LegacyObject) bool) (string, error) {
	return selectNamedContainer(c, c.Inventory, name, occurrence, visible)
}

func selectReadyContainerRoot(c ItemCollection, name string, occurrence int, visible func(LegacyObject) bool) (string, error) {
	return selectNamedContainer(c, c.Ready[:], name, occurrence, visible)
}

// resolveContainerRoot matches C get/drop container lookup: player inventory
// find_obj(val), then room inventory find_obj(val), then worn ready[] with a
// fresh match counter.
func resolveContainerRoot(player, room ItemCollection, name string, occurrence int, visible func(LegacyObject) bool) (string, string, error) {
	if id, err := selectContainerRoot(player, name, occurrence, visible); err == nil {
		return id, "player", nil
	}
	if id, err := selectContainerRoot(room, name, occurrence, visible); err == nil {
		return id, "room", nil
	}
	if id, err := selectReadyContainerRoot(player, name, occurrence, visible); err == nil {
		return id, "player", nil
	}
	return "", "", fmt.Errorf("container not found")
}

func selectContainedRoot(c ItemCollection, containerID, name string, occurrence int, visible func(LegacyObject) bool) (string, error) {
	if occurrence < 1 {
		return "", fmt.Errorf("invalid item occurrence")
	}
	container, ok := c.Items[containerID]
	if !ok || !flag(container.Object.Flags[:], itemContainerFlag) {
		return "", fmt.Errorf("container not found")
	}
	name = strings.TrimSpace(name)
	found := 0
	for _, id := range container.Contents {
		item, ok := c.Items[id]
		if !ok || !strings.EqualFold(item.Object.Name, name) || (visible != nil && !visible(item.Object)) {
			continue
		}
		found++
		if found == occurrence {
			return id, nil
		}
	}
	return "", fmt.Errorf("item not found")
}

func removeChildRoot(children []string, childID string) ([]string, error) {
	for i, id := range children {
		if id != childID {
			continue
		}
		out := append([]string(nil), children[:i]...)
		out = append(out, children[i+1:]...)
		return out, nil
	}
	return nil, fmt.Errorf("container child absent")
}

func moveCanonicalSubtree(source, destination *ItemCollection, root string) error {
	if source == destination {
		return nil
	}
	for id := range source.Items {
		if _, exists := destination.Items[id]; exists {
			return fmt.Errorf("overlapping item owners")
		}
	}
	stack := []string{root}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		item, ok := source.Items[id]
		if !ok {
			return fmt.Errorf("missing item subtree")
		}
		destination.Items[id] = item
		delete(source.Items, id)
		stack = append(stack, item.Contents...)
	}
	return nil
}

func containerCapacityAvailable(c ItemCollection, containerID string) error {
	container := c.Items[containerID]
	if container.Object.ShotsMax > 0 && container.Object.ShotsCurrent >= container.Object.ShotsMax {
		return fmt.Errorf("더이상 담을 수 없습니다")
	}
	return nil
}

func adjustContainerCount(c *ItemCollection, containerID string, delta int16) {
	container := c.Items[containerID]
	if container.Object.ShotsMax > 0 {
		container.Object.ShotsCurrent += delta
		if container.Object.ShotsCurrent < 0 {
			container.Object.ShotsCurrent = 0
		}
	}
	c.Items[containerID] = container
}

// TakeContainedItem ports get <container> <item> for direct canonical child
// roots. Container lookup is player inventory, then room inventory, then worn
// ready[] — each a separate occurrence space (C find_obj then worn fallback).
// Item selection uses str[2]/val[2]; the move preserves IDs and nested descendants.
func (s State) TakeContainedItem(actorID, containerName, itemName string, occurrence, containerOccurrence int) (State, ItemMutationResult, error) {
	s, p, room, err := s.itemMutationContext(actorID)
	if err != nil {
		return State{}, ItemMutationResult{}, err
	}
	if flag(p.Body.Flags[:], playerBlindFlag) {
		return State{}, ItemMutationResult{}, fmt.Errorf("그런 건 보이지 않습니다")
	}
	if p.Body.Class < playerCaretakerClass && roomHasGuard(s, room) {
		return State{}, ItemMutationResult{}, fmt.Errorf("경비가 물건을 줍지 못하게 합니다")
	}
	detect := flag(p.Body.Flags[:], playerDetectInvisibleFlag)
	visible := func(object LegacyObject) bool {
		return detect || (!flag(object.Flags[:], objectInvisibleFlag) && !flag(object.Flags[:], objectHiddenFlag))
	}
	containerID, owner, err := resolveContainerRoot(*p.Items, *room.Items, containerName, containerOccurrence, visible)
	if err != nil {
		return State{}, ItemMutationResult{}, err
	}
	source := p.Items
	if owner == "room" {
		source = room.Items
	}
	childID, err := selectContainedRoot(*source, containerID, itemName, occurrence, visible)
	if err != nil {
		return State{}, ItemMutationResult{}, err
	}
	child := source.Items[childID]
	if err := validateTakeCandidate(p, *source, *p.Items, childID, false); err != nil {
		return State{}, ItemMutationResult{}, err
	}

	next := s.clone()
	nextPlayer := next.Players[actorID]
	nextRoom := next.Rooms[p.Body.RoomID]
	if owner == "player" {
		container := nextPlayer.Items.Items[containerID]
		container.Contents, err = removeChildRoot(container.Contents, childID)
		if err != nil {
			return State{}, ItemMutationResult{}, err
		}
		nextPlayer.Items.Items[containerID] = container
		adjustContainerCount(nextPlayer.Items, containerID, -1)
		if err := insertInventoryRoot(nextPlayer.Items, childID); err != nil {
			return State{}, ItemMutationResult{}, err
		}
	} else {
		container := nextRoom.Items.Items[containerID]
		container.Contents, err = removeChildRoot(container.Contents, childID)
		if err != nil {
			return State{}, ItemMutationResult{}, err
		}
		nextRoom.Items.Items[containerID] = container
		adjustContainerCount(nextRoom.Items, containerID, -1)
		if err := moveCanonicalSubtree(nextRoom.Items, nextPlayer.Items, childID); err != nil {
			return State{}, ItemMutationResult{}, err
		}
		if err := insertInventoryRoot(nextPlayer.Items, childID); err != nil {
			return State{}, ItemMutationResult{}, err
		}
	}
	if weight, err := nextPlayer.Items.Weight(); err != nil || weight > maxPlayerWeight(nextPlayer.Body) {
		if err != nil {
			return State{}, ItemMutationResult{}, err
		}
		return State{}, ItemMutationResult{}, fmt.Errorf("더이상 가질 수 없습니다")
	}
	if capacity, err := nextPlayer.Items.CapacityCount(); err != nil || capacity > 150 {
		if err != nil {
			return State{}, ItemMutationResult{}, err
		}
		return State{}, ItemMutationResult{}, fmt.Errorf("더이상 가질 수 없습니다")
	}
	next.Players[actorID] = nextPlayer
	next.Rooms[p.Body.RoomID] = nextRoom
	if err := next.Validate(); err != nil {
		return State{}, ItemMutationResult{}, err
	}
	return next, ItemMutationResult{ItemName: child.Object.Name, Action: "take-contained"}, nil
}

// DropContainedItem ports drop <item> <container> for direct inventory roots.
// Item selection uses C val[1]; container selection uses str[2]/val[2] with
// the same inventory/room/worn occurrence spaces as get().
// Container destruction/devouring is deliberately fail-closed until its C
// side effects have a separate reducer.
func (s State) DropContainedItem(actorID, itemName, containerName string, occurrence, containerOccurrence int) (State, ItemMutationResult, error) {
	s, p, room, err := s.itemMutationContext(actorID)
	if err != nil {
		return State{}, ItemMutationResult{}, err
	}
	detect := flag(p.Body.Flags[:], playerDetectInvisibleFlag)
	visible := func(object LegacyObject) bool { return detect || !flag(object.Flags[:], objectInvisibleFlag) }
	itemID, err := selectInventoryRoot(*p.Items, itemName, occurrence, visible)
	if err != nil {
		return State{}, ItemMutationResult{}, err
	}
	protected, err := itemTreeContainsProtected(*p.Items, itemID)
	if err != nil {
		return State{}, ItemMutationResult{}, err
	}
	if protected && p.Body.Class < playerDMClass {
		return State{}, ItemMutationResult{}, fmt.Errorf("임무/이벤트 물건은 버릴 수 없습니다")
	}
	containerID, owner, err := resolveContainerRoot(*p.Items, *room.Items, containerName, containerOccurrence, visible)
	if err != nil {
		return State{}, ItemMutationResult{}, err
	}
	if containerID == itemID {
		return State{}, ItemMutationResult{}, fmt.Errorf("그것을 그것 자신에게는 넣을 수 없습니다")
	}
	if owner == "player" {
		if err := containerCapacityAvailable(*p.Items, containerID); err != nil {
			return State{}, ItemMutationResult{}, err
		}
	} else if err := containerCapacityAvailable(*room.Items, containerID); err != nil {
		return State{}, ItemMutationResult{}, err
	}

	next := s.clone()
	nextPlayer := next.Players[actorID]
	nextRoom := next.Rooms[p.Body.RoomID]
	if owner == "player" {
		nextPlayer.Items.Inventory, err = removeInventoryRoot(nextPlayer.Items.Inventory, itemID)
		if err != nil {
			return State{}, ItemMutationResult{}, err
		}
		container := nextPlayer.Items.Items[containerID]
		container.Contents = append(container.Contents, itemID)
		nextPlayer.Items.Items[containerID] = container
		adjustContainerCount(nextPlayer.Items, containerID, 1)
		if flag(container.Object.Flags[:], objectDevoursFlag) {
			return State{}, ItemMutationResult{}, fmt.Errorf("그릇이 물건을 삼키는 효과는 아직 구현되지 않았습니다")
		}
	} else {
		container := nextRoom.Items.Items[containerID]
		if flag(container.Object.Flags[:], objectDevoursFlag) {
			return State{}, ItemMutationResult{}, fmt.Errorf("그릇이 물건을 삼키는 효과는 아직 구현되지 않았습니다")
		}
		nextPlayer.Items.Inventory, err = removeInventoryRoot(nextPlayer.Items.Inventory, itemID)
		if err != nil {
			return State{}, ItemMutationResult{}, err
		}
		container.Contents = append(container.Contents, itemID)
		nextRoom.Items.Items[containerID] = container
		adjustContainerCount(nextRoom.Items, containerID, 1)
		if err := moveCanonicalSubtree(nextPlayer.Items, nextRoom.Items, itemID); err != nil {
			return State{}, ItemMutationResult{}, err
		}
	}
	next.Players[actorID] = nextPlayer
	next.Rooms[p.Body.RoomID] = nextRoom
	if err := next.Validate(); err != nil {
		return State{}, ItemMutationResult{}, err
	}
	return next, ItemMutationResult{ItemName: itemName, Action: "drop-contained"}, nil
}
