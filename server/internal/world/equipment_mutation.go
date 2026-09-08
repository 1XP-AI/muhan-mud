package world

import (
	"fmt"
	"strings"
)

// EquipmentMode names the three legacy command3 paths that move an inventory
// root into a ready slot. The mode is part of the command payload so replay
// cannot reinterpret a line using a different slot policy.
type EquipmentMode string

const (
	EquipmentWear  EquipmentMode = "wear"
	EquipmentHold  EquipmentMode = "hold"
	EquipmentWield EquipmentMode = "wield"
)

const (
	equipmentMaxWear = 20
	equipmentBody    = 1
	equipmentArms    = 2
	equipmentLegs    = 3
	equipmentNeck1   = 4
	equipmentNeck2   = 5
	equipmentHands   = 6
	equipmentHead    = 7
	equipmentFeet    = 8
	equipmentFinger1 = 9
	equipmentFinger8 = 16
	equipmentHeld    = 17
	equipmentShield  = 18
	equipmentFace    = 19
	equipmentWield   = 20
)

// These are the original object flags. Keep their numeric values local to the
// canonical item mutation boundary rather than exposing the legacy bitset to
// clients or storage callers.
const (
	objectCursedFlag         = 22
	objectWearsFlag          = 23
	objectGoodOnlyFlag       = 12
	objectEvilOnlyFlag       = 13
	objectClassSelectFlag    = 31
	objectSmallFlag          = 19
	objectMediumFlag         = 20
	objectNoMagicFlag        = 10
	objectMaleOnlyFlag       = 27
	objectFemaleOnlyFlag     = 26
	objectMarriedOnlyFlag    = 45
	objectEventFlag          = 46
	objectHeldFlag           = 49
	objectOneWevFlag         = 50
	objectNeverShattersFlag  = 41
	objectAlwaysCriticalFlag = 42
)

type EquipmentMutationResult struct {
	ItemName string
	Action   string
	// Slot is the one-based C MAXWEAR slot (BODY=1 ... WIELD=20).
	Slot  int
	Count int
}

func equipmentMutationPlayer(s State, actorID string) (PlayerState, error) {
	if err := s.Validate(); err != nil {
		return PlayerState{}, err
	}
	p, ok := s.Players[actorID]
	if !ok || !p.Online || p.Items == nil {
		return PlayerState{}, fmt.Errorf("online player with migrated items required")
	}
	if err := p.Items.Validate(); err != nil {
		return PlayerState{}, err
	}
	return p, nil
}

func selectReadyRoot(c ItemCollection, name string, occurrence int, visible func(LegacyObject) bool) (int, string, error) {
	if occurrence < 1 {
		return 0, "", fmt.Errorf("invalid item occurrence")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, "", fmt.Errorf("item name required")
	}
	found := 0
	for slot, id := range c.Ready {
		if id == "" {
			continue
		}
		item, ok := c.Items[id]
		if !ok || !strings.EqualFold(item.Object.Name, name) || (visible != nil && !visible(item.Object)) {
			continue
		}
		found++
		if found == occurrence {
			return slot, id, nil
		}
	}
	return 0, "", fmt.Errorf("item not found")
}

func setObjectFlag(flags *[8]byte, bit uint, enabled bool) {
	if bit >= uint(len(flags))*8 {
		return
	}
	mask := byte(1 << (bit % 8))
	if enabled {
		flags[bit/8] |= mask
	} else {
		flags[bit/8] &^= mask
	}
}

func removeInventoryRoot(ids []string, want string) ([]string, error) {
	for i, id := range ids {
		if id != want {
			continue
		}
		out := append([]string(nil), ids[:i]...)
		out = append(out, ids[i+1:]...)
		return out, nil
	}
	return nil, fmt.Errorf("inventory root not found")
}

func insertInventoryRoot(c *ItemCollection, root string) error {
	item, ok := c.Items[root]
	if !ok {
		return fmt.Errorf("item root absent")
	}
	at := len(c.Inventory)
	for i, id := range c.Inventory {
		other, ok := c.Items[id]
		if !ok {
			return fmt.Errorf("inventory root absent")
		}
		if other.Object.Name > item.Object.Name || (other.Object.Name == item.Object.Name && int8(other.Object.Adjustment) > int8(item.Object.Adjustment)) {
			at = i
			break
		}
	}
	c.Inventory = append(c.Inventory, "")
	copy(c.Inventory[at+1:], c.Inventory[at:])
	c.Inventory[at] = root
	return nil
}

func equipmentSlots(item LegacyObject, mode EquipmentMode) ([]int, error) {
	switch mode {
	case EquipmentHold:
		if item.Wear != equipmentHeld && item.Wear != equipmentWield {
			return nil, fmt.Errorf("당신은 그것을 쥘 수 없습니다")
		}
		return []int{equipmentHeld - 1}, nil
	case EquipmentWield:
		if item.Wear != equipmentWield {
			return nil, fmt.Errorf("당신은 그것을 무장할 수 없습니다")
		}
		return []int{equipmentWield - 1}, nil
	case EquipmentWear:
		if item.Wear < equipmentBody || item.Wear > equipmentFace || item.Wear == equipmentHeld || item.Wear == equipmentWield {
			return nil, fmt.Errorf("입는 물건이 아닙니다")
		}
		switch item.Wear {
		case equipmentNeck1:
			return []int{equipmentNeck1 - 1, equipmentNeck2 - 1}, nil
		case equipmentFinger1:
			out := make([]int, 0, equipmentFinger8-equipmentFinger1+1)
			for slot := equipmentFinger1 - 1; slot <= equipmentFinger8-1; slot++ {
				out = append(out, slot)
			}
			return out, nil
		case equipmentBody, equipmentArms, equipmentLegs, equipmentHands, equipmentHead, equipmentFeet, equipmentShield, equipmentFace:
			return []int{int(item.Wear) - 1}, nil
		default:
			return nil, fmt.Errorf("invalid legacy wear position")
		}
	default:
		return nil, fmt.Errorf("unsupported equipment mode")
	}
}

func equipmentClassAllowed(player LegacyMonster, item LegacyObject) bool {
	if player.Class >= 9 || !flag(item.Flags[:], objectClassSelectFlag) {
		return true
	}
	return flag(item.Flags[:], uint(objectClassSelectFlag)+uint(player.Class))
}

func equipmentSizeAllowed(player LegacyMonster, item LegacyObject) bool {
	size := 0
	if flag(item.Flags[:], objectSmallFlag) {
		size += 2
	}
	if flag(item.Flags[:], objectMediumFlag) {
		size++
	}
	switch size {
	case 0:
		return true
	case 1:
		return player.Race == 8 || player.Race == 4 || player.Race == 1 // GNOME/HOBBIT/DWARF
	case 2:
		return player.Race == 5 || player.Race == 2 || player.Race == 3 || player.Race == 6 // HUMAN/ELF/HALFELF/ORC
	case 3:
		return player.Race == 7 // HALFGIANT
	default:
		return false
	}
}

func equipmentAlignmentAllowed(player LegacyMonster, item LegacyObject) bool {
	if flag(item.Flags[:], objectGoodOnlyFlag) && player.Alignment < -50 {
		return false
	}
	if flag(item.Flags[:], objectEvilOnlyFlag) && player.Alignment > 50 {
		return false
	}
	return true
}

func equipmentUseAllowed(player LegacyMonster, item LegacyObject, mode EquipmentMode) error {
	if item.ShotsCurrent < 1 {
		return fmt.Errorf("부서져서 사용할 수 없습니다")
	}
	if !equipmentClassAllowed(player, item) {
		return fmt.Errorf("직업에 맞지 않습니다")
	}
	if !equipmentAlignmentAllowed(player, item) {
		return fmt.Errorf("성향에 맞지 않습니다")
	}
	if mode != EquipmentHold && player.Class < 9 && !equipmentSizeAllowed(player, item) {
		return fmt.Errorf("몸 크기와 맞지 않습니다")
	}
	if mode == EquipmentWear && item.Type == 5 {
		if flag(item.Flags[:], objectNoMagicFlag) && (player.Class == 5 || player.Class == 3) {
			return fmt.Errorf("도술사, 불제자는 사용할 수 없습니다")
		}
		if flag(item.Flags[:], objectMaleOnlyFlag) && !flag(player.Flags[:], 12) {
			return fmt.Errorf("남자들만 입을 수 있습니다")
		}
		if flag(item.Flags[:], objectFemaleOnlyFlag) && flag(player.Flags[:], 12) {
			return fmt.Errorf("여자들만 입을 수 있습니다")
		}
		if flag(item.Flags[:], objectMarriedOnlyFlag) && !flag(player.Flags[:], 60) {
			return fmt.Errorf("결혼한 사람들만 입을 수 있습니다")
		}
		if item.Quest == 0 && (item.ShotsMax > 1001 || item.ShotsCurrent > 1001) {
			return fmt.Errorf("사용할 수 없는 물건입니다")
		}
		checkAC := int(int8(item.Armor)) * 5
		if item.Wear == equipmentBody {
			checkAC = int(int8(item.Armor)) * 2
		}
		if checkAC > 151 && item.Quest == 0 {
			return fmt.Errorf("사용할 수 없는 물건입니다")
		}
		if checkAC < 30 {
			checkAC = 0
		}
		if item.Quest == 0 && player.Class < 9 && !flag(item.Flags[:], objectOneWevFlag) && int(player.Level) < checkAC {
			return fmt.Errorf("당신의 능력으로는 사용할 수 없는 물건입니다")
		}
	}
	if mode == EquipmentWield {
		if item.Quest == 0 && !flag(item.Flags[:], objectOneWevFlag) && int(item.DiceCount)*int(item.DiceSides)+int(item.DicePlus) > 39 {
			return fmt.Errorf("사용할 수 없는 무기입니다")
		}
		if !flag(item.Flags[:], objectOneWevFlag) && (item.ShotsMax > 600 || item.ShotsCurrent > 600) {
			return fmt.Errorf("사용할 수 없는 무기입니다")
		}
		if flag(item.Flags[:], objectNeverShattersFlag) && flag(item.Flags[:], objectAlwaysCriticalFlag) {
			return fmt.Errorf("사용할 수 없는 무기입니다")
		}
		if player.Class < 9 && item.Quest == 0 && !flag(item.Flags[:], objectOneWevFlag) {
			damage := int(item.DiceCount)*int(item.DiceSides) + int(item.DicePlus)
			switch player.Class {
			case 4:
				damage -= 7
			case 1, 8:
				damage -= 3
			case 6, 7:
				damage -= 2
			}
			if damage > 15 && int(player.Level) < damage*3 {
				return fmt.Errorf("당신의 능력으로는 사용할 수 없는 무기입니다")
			}
		}
		if flag(item.Flags[:], objectOneWevFlag) && item.Keys[2] != "" && !strings.EqualFold(item.Keys[2], player.Name) {
			return fmt.Errorf("다른 사람의 물건은 사용할 수 없습니다")
		}
	}
	if mode == EquipmentHold {
		if flag(item.Flags[:], objectEventFlag) || flag(item.Flags[:], objectOneWevFlag) || item.Quest > 0 {
			return fmt.Errorf("당신은 그것을 쥘 수 없습니다")
		}
		if int(item.DiceCount)*int(item.DiceSides)+int(item.DicePlus) > 100 {
			return fmt.Errorf("당신은 그것을 쥘 수 없습니다")
		}
	}
	return nil
}

func refreshEquipmentStats(player *PlayerState) error {
	stats, err := player.Items.CombatStats(player.Body)
	if err != nil {
		return err
	}
	player.Body.Armor, player.Body.Thaco = byte(stats.Armor), byte(stats.Thaco)
	return nil
}

// ReadyItem ports one item at a time from the canonical inventory roots into a
// C-compatible ready slot. It returns an all-zero state on every error so the
// caller cannot accidentally persist a partial move.
func (s State) ReadyItem(actorID, name string, occurrence int, mode EquipmentMode) (State, EquipmentMutationResult, error) {
	p, err := equipmentMutationPlayer(s, actorID)
	if err != nil {
		return State{}, EquipmentMutationResult{}, err
	}
	detect := flag(p.Body.Flags[:], playerDetectInvisibleFlag)
	id, err := selectInventoryRoot(*p.Items, name, occurrence, func(object LegacyObject) bool {
		return detect || !flag(object.Flags[:], objectInvisibleFlag)
	})
	if err != nil {
		return State{}, EquipmentMutationResult{}, err
	}
	item := p.Items.Items[id]
	slots, err := equipmentSlots(item.Object, mode)
	if err != nil {
		return State{}, EquipmentMutationResult{}, err
	}
	if err := equipmentUseAllowed(p.Body, item.Object, mode); err != nil {
		return State{}, EquipmentMutationResult{}, err
	}
	slot := -1
	for _, candidate := range slots {
		if candidate >= 0 && candidate < equipmentMaxWear && p.Items.Ready[candidate] == "" {
			slot = candidate
			break
		}
	}
	if slot < 0 {
		return State{}, EquipmentMutationResult{}, fmt.Errorf("장비 슬롯이 가득 찼습니다")
	}

	next := s.clone()
	player := next.Players[actorID]
	items := player.Items
	items.Inventory, err = removeInventoryRoot(items.Inventory, id)
	if err != nil {
		return State{}, EquipmentMutationResult{}, err
	}
	object := items.Items[id]
	setObjectFlag(&object.Object.Flags, objectWearsFlag, true)
	setObjectFlag(&object.Object.Flags, objectHeldFlag, mode == EquipmentHold && object.Object.Type < 5)
	items.Items[id] = object
	items.Ready[slot] = id
	if err := refreshEquipmentStats(&player); err != nil {
		return State{}, EquipmentMutationResult{}, err
	}
	next.Players[actorID] = player
	if err := next.Validate(); err != nil {
		return State{}, EquipmentMutationResult{}, err
	}
	return next, EquipmentMutationResult{ItemName: object.Object.Name, Action: string(mode), Slot: slot + 1}, nil
}

// WearAllItems ports the useful part of command3's wear_all loop. Items that
// are not currently eligible or whose slot is full are skipped just like the
// legacy routine; successful moves are committed as one durable candidate.
func (s State) WearAllItems(actorID string) (State, EquipmentMutationResult, error) {
	_, err := equipmentMutationPlayer(s, actorID)
	if err != nil {
		return State{}, EquipmentMutationResult{}, err
	}
	next := s.clone()
	player := next.Players[actorID]
	items := player.Items
	ids := append([]string(nil), items.Inventory...)
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		item, ok := items.Items[id]
		if !ok {
			return State{}, EquipmentMutationResult{}, fmt.Errorf("inventory item absent")
		}
		slots, slotErr := equipmentSlots(item.Object, EquipmentWear)
		if slotErr != nil || equipmentUseAllowed(player.Body, item.Object, EquipmentWear) != nil {
			continue
		}
		slot := -1
		for _, candidate := range slots {
			if items.Ready[candidate] == "" {
				slot = candidate
				break
			}
		}
		if slot < 0 {
			continue
		}
		var removeErr error
		items.Inventory, removeErr = removeInventoryRoot(items.Inventory, id)
		if removeErr != nil {
			return State{}, EquipmentMutationResult{}, removeErr
		}
		object := items.Items[id]
		setObjectFlag(&object.Object.Flags, objectWearsFlag, true)
		setObjectFlag(&object.Object.Flags, objectHeldFlag, false)
		items.Items[id] = object
		items.Ready[slot] = id
		names = append(names, object.Object.Name)
	}
	if len(names) == 0 {
		return next, EquipmentMutationResult{Action: "wear-all"}, nil
	}
	if err := refreshEquipmentStats(&player); err != nil {
		return State{}, EquipmentMutationResult{}, err
	}
	next.Players[actorID] = player
	if err := next.Validate(); err != nil {
		return State{}, EquipmentMutationResult{}, err
	}
	return next, EquipmentMutationResult{ItemName: strings.Join(names, ", "), Action: "wear-all", Count: len(names)}, nil
}

// RemoveItem returns one ready root to sorted inventory order. Cursed items
// remain in place exactly as command3's remove_obj does.
func (s State) RemoveItem(actorID, name string, occurrence int) (State, EquipmentMutationResult, error) {
	p, err := equipmentMutationPlayer(s, actorID)
	if err != nil {
		return State{}, EquipmentMutationResult{}, err
	}
	detect := flag(p.Body.Flags[:], playerDetectInvisibleFlag)
	slot, id, err := selectReadyRoot(*p.Items, name, occurrence, func(object LegacyObject) bool {
		return detect || !flag(object.Flags[:], objectInvisibleFlag)
	})
	if err != nil {
		return State{}, EquipmentMutationResult{}, err
	}
	selected := p.Items.Items[id]
	if flag(selected.Object.Flags[:], objectCursedFlag) {
		return State{}, EquipmentMutationResult{}, fmt.Errorf("저주받은 물건은 벗을 수 없습니다")
	}

	next := s.clone()
	player := next.Players[actorID]
	items := player.Items
	object := items.Items[id]
	items.Ready[slot] = ""
	setObjectFlag(&object.Object.Flags, objectWearsFlag, false)
	setObjectFlag(&object.Object.Flags, objectHeldFlag, false)
	items.Items[id] = object
	if err := insertInventoryRoot(items, id); err != nil {
		return State{}, EquipmentMutationResult{}, err
	}
	if err := refreshEquipmentStats(&player); err != nil {
		return State{}, EquipmentMutationResult{}, err
	}
	next.Players[actorID] = player
	if err := next.Validate(); err != nil {
		return State{}, EquipmentMutationResult{}, err
	}
	return next, EquipmentMutationResult{ItemName: object.Object.Name, Action: "remove", Slot: slot + 1}, nil
}

// RemoveAllItems mirrors remove_all: every non-cursed ready root is returned
// to inventory in canonical order, while cursed roots remain equipped.
func (s State) RemoveAllItems(actorID string) (State, EquipmentMutationResult, error) {
	_, err := equipmentMutationPlayer(s, actorID)
	if err != nil {
		return State{}, EquipmentMutationResult{}, err
	}
	next := s.clone()
	player := next.Players[actorID]
	items := player.Items
	names := make([]string, 0, equipmentMaxWear)
	for slot, id := range items.Ready {
		if id == "" {
			continue
		}
		object := items.Items[id]
		if flag(object.Object.Flags[:], objectCursedFlag) {
			continue
		}
		items.Ready[slot] = ""
		setObjectFlag(&object.Object.Flags, objectWearsFlag, false)
		setObjectFlag(&object.Object.Flags, objectHeldFlag, false)
		items.Items[id] = object
		if err := insertInventoryRoot(items, id); err != nil {
			return State{}, EquipmentMutationResult{}, err
		}
		names = append(names, object.Object.Name)
	}
	if len(names) == 0 {
		return next, EquipmentMutationResult{Action: "remove-all"}, nil
	}
	if err := refreshEquipmentStats(&player); err != nil {
		return State{}, EquipmentMutationResult{}, err
	}
	next.Players[actorID] = player
	if err := next.Validate(); err != nil {
		return State{}, EquipmentMutationResult{}, err
	}
	return next, EquipmentMutationResult{ItemName: strings.Join(names, ", "), Action: "remove-all", Count: len(names)}, nil
}
