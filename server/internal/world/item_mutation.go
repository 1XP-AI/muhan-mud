package world

import (
	"fmt"
	"strings"
)

const (
	objectHiddenFlag     = 1  // OHIDDN
	objectNotTakeFlag    = 17 // ONOTAK
	objectSceneryFlag    = 18 // OSCENE
	objectDevoursFlag    = 25 // OCNDES
	monsterGuardFlag     = 7  // MGUARD
	playerCaretakerClass = 10
)

type ItemMutationResult struct {
	ItemName string
	Action   string
	Count    int
}

func selectInventoryRoot(c ItemCollection, name string, occurrence int, visible func(LegacyObject) bool) (string, error) {
	if occurrence < 1 {
		return "", fmt.Errorf("invalid item occurrence")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("item name required")
	}
	found := 0
	for _, id := range c.Inventory {
		item := c.Items[id]
		if !strings.EqualFold(item.Object.Name, name) || (visible != nil && !visible(item.Object)) {
			continue
		}
		found++
		if found == occurrence {
			return id, nil
		}
	}
	return "", fmt.Errorf("item not found")
}

func (s State) itemMutationContext(actorID string) (State, PlayerState, RoomState, error) {
	if err := s.Validate(); err != nil {
		return State{}, PlayerState{}, RoomState{}, err
	}
	p, ok := s.Players[actorID]
	if !ok || !p.Online || p.Items == nil {
		return State{}, PlayerState{}, RoomState{}, fmt.Errorf("online player with migrated items required")
	}
	room, ok := s.Rooms[p.Body.RoomID]
	if !ok || room.Items == nil {
		return State{}, PlayerState{}, RoomState{}, fmt.Errorf("canonical room items required")
	}
	return s, p, room, nil
}

func maxPlayerWeight(body LegacyMonster) int {
	strength := int(int8(body.Stats[0]))
	max := 20 + strength*10
	if body.Class == 2 { // BARBARIAN
		max += ((int(body.Level) + 3) / 4) * 10
	}
	return max
}

func roomHasGuard(s State, room RoomState) bool {
	if s.NPCs == nil {
		for _, monster := range room.Resource.Monsters {
			if flag(monster.Flags[:], monsterGuardFlag) {
				return true
			}
		}
		return false
	}
	for _, id := range room.NPCIDs {
		npc, ok := s.NPCs[id]
		if ok && flag(npc.Body.Flags[:], monsterGuardFlag) {
			return true
		}
	}
	return false
}

func itemTreeContainsProtected(c ItemCollection, root string) (bool, error) {
	seen := map[string]bool{}
	var visit func(string) (bool, error)
	visit = func(id string) (bool, error) {
		if seen[id] {
			return false, fmt.Errorf("cyclic item tree")
		}
		item, ok := c.Items[id]
		if !ok {
			return false, fmt.Errorf("missing item")
		}
		seen[id] = true
		if item.Object.Quest != 0 || flag(item.Object.Flags[:], objectOneWevFlag) {
			return true, nil
		}
		for _, child := range item.Contents {
			protected, err := visit(child)
			if err != nil || protected {
				return protected, err
			}
		}
		return false, nil
	}
	return visit(root)
}

func validateTakeCandidate(p PlayerState, source, destination ItemCollection, id string, checkCapacity bool) error {
	item, ok := source.Items[id]
	if !ok {
		return fmt.Errorf("floor item absent")
	}
	if flag(item.Object.Flags[:], objectNotTakeFlag) || flag(item.Object.Flags[:], objectSceneryFlag) {
		return fmt.Errorf("주울 수 있는 물건이 아닙니다")
	}
	if item.Object.Quest != 0 {
		quest := int(item.Object.Quest) - 1
		if quest >= 0 && quest < len(p.Body.Quests)*8 && flag(p.Body.Quests[:], uint(quest)) {
			return fmt.Errorf("이미 완수한 임무 물건입니다")
		}
		return fmt.Errorf("임무 물건은 아직 quest reducer가 필요합니다")
	}
	if item.Object.Type == 10 {
		return fmt.Errorf("돈 물건은 gold reducer가 필요합니다")
	}
	if !checkCapacity {
		return nil
	}
	rootWeight, err := source.objectWeight(id)
	if err != nil {
		return err
	}
	carriedWeight, err := destination.Weight()
	if err != nil {
		return err
	}
	capacity, err := destination.CapacityCount()
	if err != nil {
		return err
	}
	if carriedWeight+rootWeight > maxPlayerWeight(p.Body) || capacity > 150 {
		return fmt.Errorf("더이상 가질 수 없습니다")
	}
	return nil
}

// TakeItem moves one visible floor root into the player's inventory. It keeps
// the complete nested subtree and both owners in one candidate state.
func (s State) TakeItem(actorID, name string, occurrence int) (State, ItemMutationResult, error) {
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
	id, err := selectInventoryRoot(*room.Items, name, occurrence, func(object LegacyObject) bool {
		return detect || (!flag(object.Flags[:], objectInvisibleFlag) && !flag(object.Flags[:], objectHiddenFlag))
	})
	if err != nil {
		return State{}, ItemMutationResult{}, err
	}
	if err := validateTakeCandidate(p, *room.Items, *p.Items, id, true); err != nil {
		return State{}, ItemMutationResult{}, err
	}
	plan, err := TransferItemRoots(*room.Items, *p.Items, []string{id})
	if err != nil {
		return State{}, ItemMutationResult{}, err
	}
	next := s.clone()
	nextRoom := next.Rooms[p.Body.RoomID]
	nextRoom.Items = &plan.Source
	next.Rooms[p.Body.RoomID] = nextRoom
	nextPlayer := next.Players[actorID]
	nextPlayer.Items = &plan.Destination
	next.Players[actorID] = nextPlayer
	if err := next.Validate(); err != nil {
		return State{}, ItemMutationResult{}, err
	}
	return next, ItemMutationResult{ItemName: plan.Destination.Items[id].Object.Name, Action: "take"}, nil
}

// TakeAllItems ports get_all_rom for canonical floor roots. Ineligible roots
// are skipped; all successful transfers are still one atomic candidate.
func (s State) TakeAllItems(actorID string) (State, ItemMutationResult, error) {
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
	source := room.Items.clone()
	destination := p.Items.clone()
	ids := append([]string(nil), source.Inventory...)
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		item, ok := source.Items[id]
		if !ok || (flag(item.Object.Flags[:], objectInvisibleFlag) && !detect) || (flag(item.Object.Flags[:], objectHiddenFlag) && !detect) {
			continue
		}
		if err := validateTakeCandidate(p, source, destination, id, true); err != nil {
			continue
		}
		plan, err := TransferItemRoots(source, destination, []string{id})
		if err != nil {
			return State{}, ItemMutationResult{}, err
		}
		source, destination = plan.Source, plan.Destination
		names = append(names, item.Object.Name)
	}
	if len(names) == 0 {
		return s.clone(), ItemMutationResult{Action: "take-all"}, nil
	}
	next := s.clone()
	nextRoom := next.Rooms[p.Body.RoomID]
	nextRoom.Items = &source
	next.Rooms[p.Body.RoomID] = nextRoom
	nextPlayer := next.Players[actorID]
	nextPlayer.Items = &destination
	next.Players[actorID] = nextPlayer
	if err := next.Validate(); err != nil {
		return State{}, ItemMutationResult{}, err
	}
	return next, ItemMutationResult{ItemName: strings.Join(names, ", "), Action: "take-all", Count: len(names)}, nil
}

// DropItem moves one inventory root to the current room. Equipped items are
// not inventory roots and therefore cannot be dropped until a remove command
// explicitly returns them to inventory.
func (s State) DropItem(actorID, name string, occurrence int) (State, ItemMutationResult, error) {
	s, p, room, err := s.itemMutationContext(actorID)
	if err != nil {
		return State{}, ItemMutationResult{}, err
	}
	id, err := selectInventoryRoot(*p.Items, name, occurrence, nil)
	if err != nil {
		return State{}, ItemMutationResult{}, err
	}
	protected, err := itemTreeContainsProtected(*p.Items, id)
	if err != nil {
		return State{}, ItemMutationResult{}, err
	}
	if protected && p.Body.Class < playerDMClass {
		return State{}, ItemMutationResult{}, fmt.Errorf("임무/이벤트 물건은 버릴 수 없습니다")
	}
	plan, err := TransferItemRoots(*p.Items, *room.Items, []string{id})
	if err != nil {
		return State{}, ItemMutationResult{}, err
	}
	next := s.clone()
	nextPlayer := next.Players[actorID]
	nextPlayer.Items = &plan.Source
	next.Players[actorID] = nextPlayer
	nextRoom := next.Rooms[p.Body.RoomID]
	nextRoom.Items = &plan.Destination
	next.Rooms[p.Body.RoomID] = nextRoom
	if err := next.Validate(); err != nil {
		return State{}, ItemMutationResult{}, err
	}
	return next, ItemMutationResult{ItemName: plan.Destination.Items[id].Object.Name, Action: "drop"}, nil
}

// DropAllItems ports drop_all_rom for canonical inventory roots. Quest/event
// roots remain with ordinary players; a DM-class player may drop them.
func (s State) DropAllItems(actorID string) (State, ItemMutationResult, error) {
	s, p, room, err := s.itemMutationContext(actorID)
	if err != nil {
		return State{}, ItemMutationResult{}, err
	}
	source := p.Items.clone()
	destination := room.Items.clone()
	ids := append([]string(nil), source.Inventory...)
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		protected, err := itemTreeContainsProtected(source, id)
		if err != nil {
			return State{}, ItemMutationResult{}, err
		}
		if protected && p.Body.Class < playerDMClass {
			continue
		}
		plan, err := TransferItemRoots(source, destination, []string{id})
		if err != nil {
			return State{}, ItemMutationResult{}, err
		}
		item := source.Items[id]
		source, destination = plan.Source, plan.Destination
		names = append(names, item.Object.Name)
	}
	if len(names) == 0 {
		return s.clone(), ItemMutationResult{Action: "drop-all"}, nil
	}
	next := s.clone()
	nextPlayer := next.Players[actorID]
	nextPlayer.Items = &source
	next.Players[actorID] = nextPlayer
	nextRoom := next.Rooms[p.Body.RoomID]
	nextRoom.Items = &destination
	next.Rooms[p.Body.RoomID] = nextRoom
	if err := next.Validate(); err != nil {
		return State{}, ItemMutationResult{}, err
	}
	return next, ItemMutationResult{ItemName: strings.Join(names, ", "), Action: "drop-all", Count: len(names)}, nil
}
