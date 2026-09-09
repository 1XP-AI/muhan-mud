package world

import (
	"fmt"
	"reflect"
	"strings"
)

// Repair uses the original command8.c object and room bits.  The canonical
// graph admits only direct inventory roots for this bounded slice; equipped
// and nested occurrence resolution remains a separate command boundary.
const (
	repairNoFixFlag     = 15 // ONOFIX
	repairEnchantedFlag = 14 // OENCHA
	repairMissileType   = 4  // MISSILE
	repairArmorType     = 5  // ARMOR
	repairBodyWear      = 1  // BODY
)

// RepairRoomFlag is the repair-shop room bit (RREPAI). It is kept local to
// this command boundary so repair remains independently buildable.
const RepairRoomFlag = 7

// RepairProposal is a snapshot-bound candidate.  Random outcomes are stored
// in the proposal so ApplyRepair never calls an injected RNG.  The unexported
// fields bind the proposal to the exact object/gold/room observation used to
// plan it; callers cannot turn a stale proposal into a partial mutation.
type RepairProposal struct {
	ActorID    string
	RoomID     int16
	ItemID     string
	ItemName   string
	Occurrence int
	Cost       int64

	BreakRoll         int
	AdjustmentRoll    int
	ShotsRoll         int
	Broke             bool
	Refunded          bool
	Removed           bool
	AdjustmentCleared bool
	Response          string

	expectedItem      Item
	expectedGold      int32
	expectedRoomFlags [8]byte
	expectedPiety     byte
}

// RepairResult is the durable actor response and audit projection.  A break
// refunds the cost and removes the complete item subtree.  A successful
// repair charges once, optionally clears enchantment adjustment, and restores
// shots from the source 5..9 roll.
type RepairResult struct {
	Response          string `json:"response"`
	Action            string `json:"action"`
	RoomID            int16  `json:"room_id"`
	ItemID            string `json:"item_id"`
	ItemName          string `json:"item_name"`
	Occurrence        int    `json:"occurrence"`
	Cost              int64  `json:"cost"`
	GoldBefore        int64  `json:"gold_before"`
	GoldAfter         int64  `json:"gold_after"`
	BreakRoll         int    `json:"break_roll"`
	AdjustmentRoll    int    `json:"adjustment_roll,omitempty"`
	ShotsRoll         int    `json:"shots_roll,omitempty"`
	Broke             bool   `json:"broke,omitempty"`
	Refunded          bool   `json:"refunded,omitempty"`
	Removed           bool   `json:"removed,omitempty"`
	AdjustmentCleared bool   `json:"adjustment_cleared,omitempty"`
	ShotsCurrent      int16  `json:"shots_current,omitempty"`
}

func repairRoll(roll func(int, int) int, low, high int) (value int, err error) {
	if roll == nil {
		return 0, fmt.Errorf("missing repair random source")
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			value = 0
			err = fmt.Errorf("repair random source panicked: %v", recovered)
		}
	}()
	value = roll(low, high)
	if value < low || value > high {
		return 0, fmt.Errorf("repair random value outside %d..%d", low, high)
	}
	return value, nil
}

func repairActor(s State, actorID string) (PlayerState, RoomState, error) {
	if err := s.Validate(); err != nil {
		return PlayerState{}, RoomState{}, err
	}
	if actorID == "" {
		return PlayerState{}, RoomState{}, fmt.Errorf("repair actor required")
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Body.Type != 0 || actor.Items == nil {
		return PlayerState{}, RoomState{}, fmt.Errorf("online player with migrated items required")
	}
	if actor.Body.Stats[4] > 63 {
		return PlayerState{}, RoomState{}, fmt.Errorf("repair actor piety outside legacy bonus table")
	}
	if err := actor.Items.Validate(); err != nil {
		return PlayerState{}, RoomState{}, err
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok {
		return PlayerState{}, RoomState{}, fmt.Errorf("repair room absent")
	}
	if !flag(room.Resource.Flags[:], RepairRoomFlag) {
		return PlayerState{}, RoomState{}, fmt.Errorf("여기서는 수리할 수 없습니다")
	}
	return actor, room, nil
}

func repairItem(actor PlayerState, name string, occurrence int) (string, Item, error) {
	if occurrence < 1 {
		return "", Item{}, fmt.Errorf("invalid item occurrence")
	}
	if name == "" || strings.TrimSpace(name) != name {
		return "", Item{}, fmt.Errorf("item name required")
	}
	id, err := selectInventoryRoot(*actor.Items, name, occurrence, nil)
	if err != nil {
		return "", Item{}, err
	}
	item, ok := actor.Items.Items[id]
	if !ok {
		return "", Item{}, fmt.Errorf("repair item absent")
	}
	return id, item, nil
}

func validateRepairItem(item Item, gold int32) (int64, error) {
	object := item.Object
	if flag(object.Flags[:], repairNoFixFlag) {
		return 0, fmt.Errorf("그것은 수리할수 없는 물건입니다")
	}
	if object.Type > repairMissileType && object.Type != repairArmorType {
		return 0, fmt.Errorf("수리할 수 있는 것은 무기나 방호구뿐입니다")
	}
	if object.ShotsMax < 0 || object.ShotsCurrent < 0 {
		return 0, fmt.Errorf("invalid repair condition")
	}
	threshold := int16(3)
	if candidate := object.ShotsMax / 10; candidate > threshold {
		threshold = candidate
	}
	if object.ShotsCurrent > threshold {
		return 0, fmt.Errorf("그건 아직 멀쩡한데")
	}
	if object.Value < 0 {
		return 0, fmt.Errorf("invalid repair value")
	}
	cost := int64(object.Value) / 4
	if gold < 0 || int64(gold) < cost {
		return 0, fmt.Errorf("수리비가 부족합니다")
	}
	return cost, nil
}

func repairObjectParticle(name string) string {
	runes := []rune(name)
	if len(runes) == 0 {
		return "를"
	}
	last := runes[len(runes)-1]
	if last >= 0xAC00 && last <= 0xD7A3 && (last-0xAC00)%28 != 0 {
		return "을"
	}
	return "를"
}

func repairResponse(itemName string, broke, adjustmentCleared bool) string {
	if broke {
		return "수리점 주인이 \"이런~~! 수리를 하다 부러뜨렸네. 미안하네\"라고 말합니다.\r\n수리점주인이 당신에게 돈을 돌려주었습니다.\r\n"
	}
	response := ""
	if adjustmentCleared {
		response = "수리점 주인이 \"수리가 다되었네.\"라고 말합니다.\r\n"
	}
	return response + fmt.Sprintf("수리점 주인이 당신에게 %s%s 되돌려 줍니다.\r\n그것은 거의 새것처럼 보입니다.\r\n", itemName, repairObjectParticle(itemName))
}

func validRepairResponse(proposal RepairProposal) bool {
	return proposal.Response == repairResponse(proposal.ItemName, proposal.Broke, proposal.AdjustmentCleared)
}

// PlanRepair ports command8.c:repair's bounded canonical inventory path.  It
// follows the C check order and consumes one break roll, an optional
// adjustment roll, and one shots roll only after all admission checks pass.
func (s State) PlanRepair(actorID, name string, occurrence int, roll func(int, int) int) (RepairProposal, error) {
	actor, room, err := repairActor(s, actorID)
	if err != nil {
		return RepairProposal{}, err
	}
	id, item, err := repairItem(actor, name, occurrence)
	if err != nil {
		return RepairProposal{}, err
	}
	cost, err := validateRepairItem(item, actor.Body.Gold)
	if err != nil {
		return RepairProposal{}, err
	}
	p := RepairProposal{
		ActorID:           actorID,
		RoomID:            actor.Body.RoomID,
		ItemID:            id,
		ItemName:          item.Object.Name,
		Occurrence:        occurrence,
		Cost:              cost,
		expectedItem:      item,
		expectedGold:      actor.Body.Gold,
		expectedRoomFlags: room.Resource.Flags,
		expectedPiety:     actor.Body.Stats[4],
	}
	p.BreakRoll, err = repairRoll(roll, 1, 100)
	if err != nil {
		return RepairProposal{}, err
	}
	breakScore := p.BreakRoll + legacyStatBonus[actor.Body.Stats[4]]
	p.Broke = (breakScore <= 15 && item.Object.ShotsCurrent < 1) || (breakScore <= 5 && item.Object.ShotsCurrent > 0)
	if p.Broke {
		p.Refunded = true
		p.Removed = true
		p.Response = repairResponse(p.ItemName, true, false)
		return p, nil
	}
	if flag(item.Object.Flags[:], repairEnchantedFlag) || item.Object.Adjustment != 0 {
		p.AdjustmentRoll, err = repairRoll(roll, 1, 50)
		if err != nil {
			return RepairProposal{}, err
		}
		p.AdjustmentCleared = p.AdjustmentRoll > int(actor.Body.Stats[4])
	}
	p.ShotsRoll, err = repairRoll(roll, 5, 9)
	if err != nil {
		return RepairProposal{}, err
	}
	p.Response = repairResponse(p.ItemName, false, p.AdjustmentCleared)
	return p, nil
}

func removeRepairRoot(items *ItemCollection, root string) error {
	if items == nil || root == "" {
		return fmt.Errorf("repair item collection required")
	}
	remaining, err := removeInventoryRoot(items.Inventory, root)
	if err != nil {
		return err
	}
	stack := []string{root}
	seen := map[string]bool{}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[id] {
			return fmt.Errorf("cyclic repair item subtree")
		}
		seen[id] = true
		item, ok := items.Items[id]
		if !ok {
			return fmt.Errorf("repair item subtree absent")
		}
		stack = append(stack, item.Contents...)
		delete(items.Items, id)
	}
	items.Inventory = remaining
	return nil
}

func repairAdjustedObject(object *LegacyObject) error {
	if object == nil {
		return fmt.Errorf("repair object required")
	}
	adjustment := int(int8(object.Adjustment))
	if object.Type == repairArmorType {
		amount := adjustment
		if object.Wear == repairBodyWear {
			amount *= 2
		}
		armor := int(int8(object.Armor)) - amount
		if armor < 0 {
			armor = 0
		}
		if armor > 127 {
			return fmt.Errorf("repaired armor outside legacy range")
		}
		object.Armor = byte(armor)
	} else if object.Type <= repairMissileType {
		shotsMax := int(object.ShotsMax) - adjustment*10
		dicePlus := int(object.DicePlus) - adjustment
		if shotsMax < -32768 || shotsMax > 32767 || dicePlus < -32768 || dicePlus > 32767 {
			return fmt.Errorf("repaired weapon outside legacy range")
		}
		object.ShotsMax = int16(shotsMax)
		if dicePlus < 0 {
			dicePlus = 0
		}
		object.DicePlus = int16(dicePlus)
	}
	object.Adjustment = 0
	setObjectFlag(&object.Flags, repairEnchantedFlag, false)
	return nil
}

// ApplyRepair rechecks the source-bound item, room, gold, and piety before
// applying the already decided candidate. It never calls the random source.
func (s State) ApplyRepair(proposal RepairProposal) (State, RepairResult, error) {
	actor, room, err := repairActor(s, proposal.ActorID)
	if err != nil {
		return State{}, RepairResult{}, err
	}
	if proposal.ActorID == "" || proposal.ItemID == "" || proposal.ItemName == "" || proposal.Occurrence < 1 || proposal.RoomID != actor.Body.RoomID || proposal.Cost < 0 || !validRepairResponse(proposal) {
		return State{}, RepairResult{}, fmt.Errorf("invalid repair proposal")
	}
	if room.Resource.Flags != proposal.expectedRoomFlags || actor.Body.Gold != proposal.expectedGold || actor.Body.Stats[4] != proposal.expectedPiety {
		return State{}, RepairResult{}, fmt.Errorf("stale repair actor or room proposal")
	}
	id, item, err := repairItem(actor, proposal.ItemName, proposal.Occurrence)
	if err != nil || id != proposal.ItemID || !reflect.DeepEqual(item, proposal.expectedItem) {
		return State{}, RepairResult{}, fmt.Errorf("stale repair item proposal")
	}
	cost, err := validateRepairItem(item, actor.Body.Gold)
	if err != nil || cost != proposal.Cost {
		if err != nil {
			return State{}, RepairResult{}, err
		}
		return State{}, RepairResult{}, fmt.Errorf("stale repair cost proposal")
	}
	if proposal.BreakRoll < 1 || proposal.BreakRoll > 100 {
		return State{}, RepairResult{}, fmt.Errorf("invalid repair break outcome")
	}
	breakScore := proposal.BreakRoll + legacyStatBonus[actor.Body.Stats[4]]
	wantBreak := (breakScore <= 15 && item.Object.ShotsCurrent < 1) || (breakScore <= 5 && item.Object.ShotsCurrent > 0)
	if proposal.Broke != wantBreak {
		return State{}, RepairResult{}, fmt.Errorf("repair break outcome mismatch")
	}
	if proposal.Broke {
		if !proposal.Refunded || !proposal.Removed || proposal.AdjustmentRoll != 0 || proposal.ShotsRoll != 0 || proposal.AdjustmentCleared {
			return State{}, RepairResult{}, fmt.Errorf("invalid repair break proposal")
		}
	} else {
		if proposal.Refunded || proposal.Removed || proposal.ShotsRoll < 5 || proposal.ShotsRoll > 9 {
			return State{}, RepairResult{}, fmt.Errorf("invalid repair success proposal")
		}
		if flag(item.Object.Flags[:], repairEnchantedFlag) || item.Object.Adjustment != 0 {
			if proposal.AdjustmentRoll < 1 || proposal.AdjustmentRoll > 50 || proposal.AdjustmentCleared != (proposal.AdjustmentRoll > int(actor.Body.Stats[4])) {
				return State{}, RepairResult{}, fmt.Errorf("invalid repair adjustment outcome")
			}
		} else if proposal.AdjustmentRoll != 0 || proposal.AdjustmentCleared {
			return State{}, RepairResult{}, fmt.Errorf("unexpected repair adjustment outcome")
		}
	}

	next := s.clone()
	nextActor := next.Players[proposal.ActorID]
	goldBefore := int64(nextActor.Body.Gold)
	result := RepairResult{
		Action:            "repair",
		RoomID:            proposal.RoomID,
		ItemID:            proposal.ItemID,
		ItemName:          proposal.ItemName,
		Occurrence:        proposal.Occurrence,
		Cost:              proposal.Cost,
		GoldBefore:        goldBefore,
		BreakRoll:         proposal.BreakRoll,
		Response:          proposal.Response,
		Broke:             proposal.Broke,
		Refunded:          proposal.Refunded,
		Removed:           proposal.Removed,
		AdjustmentCleared: proposal.AdjustmentCleared,
		AdjustmentRoll:    proposal.AdjustmentRoll,
		ShotsRoll:         proposal.ShotsRoll,
	}
	// C clears PHIDDN immediately after resolving the direct object. The
	// canonical candidate applies the same visible-state transition atomically
	// with charge, repair, or break removal.
	nextActor.Body.Flags[playerHiddenStateFlag/8] &^= 1 << (playerHiddenStateFlag % 8)
	if proposal.Broke {
		if err := removeRepairRoot(nextActor.Items, proposal.ItemID); err != nil {
			return State{}, RepairResult{}, err
		}
		// The failed repair refunds the charge, so gold remains unchanged.
		nextActor.Body.Gold = actor.Body.Gold
	} else {
		if proposal.Cost > int64(nextActor.Body.Gold) {
			return State{}, RepairResult{}, fmt.Errorf("repair gold changed")
		}
		nextActor.Body.Gold = int32(int64(nextActor.Body.Gold) - proposal.Cost)
		updated := nextActor.Items.Items[proposal.ItemID]
		if proposal.AdjustmentCleared {
			if err := repairAdjustedObject(&updated.Object); err != nil {
				return State{}, RepairResult{}, err
			}
		}
		shots := int64(updated.Object.ShotsMax) * int64(proposal.ShotsRoll) / 10
		if shots < -32768 || shots > 32767 {
			return State{}, RepairResult{}, fmt.Errorf("repaired shots outside legacy range")
		}
		updated.Object.ShotsCurrent = int16(shots)
		nextActor.Items.Items[proposal.ItemID] = updated
		result.ShotsCurrent = updated.Object.ShotsCurrent
	}
	next.Players[proposal.ActorID] = nextActor
	result.GoldAfter = int64(nextActor.Body.Gold)
	if err := next.Validate(); err != nil {
		return State{}, RepairResult{}, err
	}
	return next, result, nil
}
