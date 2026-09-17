package world

// This file is the bounded, canonical implementation of magic1.c:drink.
// It deliberately admits only self-targeting effects whose state transition is
// represented by LegacyMonster.  Targeted combat, teleport, summon and other
// effects remain fail-closed until their authoritative Go contracts exist.

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
)

const (
	drinkPotionType       = 6  // POTION
	drinkSpecialFlag      = 44 // OSPECI
	drinkGoodOnlyFlag     = 12 // OGOODO
	drinkEvilOnlyFlag     = 13 // OEVILO
	drinkClassSelectFlag  = 31 // OCLSEL
	drinkNoPotionRoomFlag = 31 // RNOPOT
	drinkSurvivalFlag     = 36 // RSUVIV
	drinkCaretakerClass   = 10 // CARETAKER
	drinkSubDMClass       = 11 // SUB_DM
	drinkMaxSpellPower    = 56
	drinkHoursTimer       = 28 // LT_HOURS
)

// Exported names keep the legacy numeric contract visible to adapters and
// differential tests without leaking a client-selected bit number.
const (
	DrinkPotionType      = drinkPotionType
	DrinkSpecialFlag     = drinkSpecialFlag
	DrinkGoodOnlyFlag    = drinkGoodOnlyFlag
	DrinkEvilOnlyFlag    = drinkEvilOnlyFlag
	DrinkClassSelectFlag = drinkClassSelectFlag
	DrinkNoPotionRoom    = drinkNoPotionRoomFlag
	DrinkSurvivalRoom    = drinkSurvivalFlag
)

var (
	ErrDrinkMissingItem      = errors.New("drink item not found")
	ErrDrinkNotPotion        = errors.New("drink target is not a potion")
	ErrDrinkEmpty            = errors.New("potion is empty")
	ErrDrinkNoPotionRoom     = errors.New("potions are not allowed here")
	ErrDrinkSurvivalRoom     = errors.New("potions are not allowed in survival room")
	ErrDrinkClass            = errors.New("drink rejects actor class")
	ErrDrinkSpellUnavailable = errors.New("drink spell catalog entry unavailable")
	ErrDrinkSpecial          = errors.New("special potion effect unavailable")
)

// DrinkLocation identifies the legacy direct inventory or ready-slot lookup.
// Nested container contents are intentionally not selectable as command roots.
type DrinkLocation string

const (
	DrinkInventoryRoot DrinkLocation = "inventory"
	DrinkReadySlot     DrinkLocation = "ready"
)

type DrinkOptions struct {
	Now  int32
	Roll func(int, int) int
}

type DrinkEvent struct {
	RoomID         int16  `json:"room_id"`
	ActorID        string `json:"actor_id"`
	ActorName      string `json:"actor_name"`
	ItemID         string `json:"item_id"`
	ItemName       string `json:"item_name"`
	SpellName      string `json:"spell_name,omitempty"`
	ExcludeActorID string `json:"exclude_actor_id"`
	Text           string `json:"text"`
}

type DrinkResult struct {
	Action      string        `json:"action"`
	Response    string        `json:"response"`
	Broadcast   bool          `json:"broadcast"`
	Changed     bool          `json:"changed"`
	Consumed    bool          `json:"consumed"`
	MovedToRoom bool          `json:"moved_to_room,omitempty"`
	Special     bool          `json:"special,omitempty"`
	SpecialKind int16         `json:"special_kind,omitempty"`
	ItemID      string        `json:"item_id,omitempty"`
	ItemName    string        `json:"item_name,omitempty"`
	Occurrence  int           `json:"occurrence,omitempty"`
	Location    DrinkLocation `json:"location,omitempty"`
	ReadySlot   int           `json:"ready_slot,omitempty"`
	MagicPower  byte          `json:"magic_power,omitempty"`
	SpellIndex  int           `json:"spell_index,omitempty"`
	SpellName   string        `json:"spell_name,omitempty"`
	Effect      string        `json:"effect,omitempty"`
	HPDelta     int32         `json:"hp_delta,omitempty"`
	MPDelta     int32         `json:"mp_delta,omitempty"`
	ShotsBefore int16         `json:"shots_before,omitempty"`
	ShotsAfter  int16         `json:"shots_after,omitempty"`
	RoomID      int16         `json:"room_id"`
	ActorID     string        `json:"actor_id"`
	ActorName   string        `json:"actor_name"`
	Event       *DrinkEvent   `json:"event,omitempty"`
}

// DrinkProposal is bound to the exact actor, target item and room flags read
// during planning. ApplyDrink never calls Roll; this is the receipt/replay
// boundary for random potion effects.
type DrinkProposal struct {
	Action      string
	ActorID     string
	RoomID      int16
	ItemID      string
	ItemName    string
	Occurrence  int
	Location    DrinkLocation
	ReadySlot   int
	Now         int32
	Special     bool
	SpecialKind int16
	MagicPower  byte
	SpellIndex  int
	SpellName   string
	Consumed    bool
	MovedToRoom bool
	ClearHidden bool
	Broadcast   bool
	Response    string
	RoomText    string
	Effect      string
	HPDelta     int32
	MPDelta     int32
	Rolls       []int
	ShotsBefore int16
	ShotsAfter  int16

	expectedActor          PlayerState
	afterBody              LegacyMonster
	expectedRoomFlags      [8]byte
	expectedRoomItems      ItemCollection
	expectedRoomSet        bool
	expectedItem           Item
	expectedDestination    int16
	expectedDestinationSet bool
}

func drinkActor(s State, actorID string) (PlayerState, RoomState, error) {
	if err := s.Validate(); err != nil {
		return PlayerState{}, RoomState{}, err
	}
	actor, ok := s.Players[actorID]
	if actorID == "" || !ok || !actor.Online || actor.Body.Type != 0 || actor.Body.Name == "" || actor.Items == nil {
		return PlayerState{}, RoomState{}, fmt.Errorf("online drink actor with migrated inventory required")
	}
	if len(actor.Body.Inventory) != 0 {
		return PlayerState{}, RoomState{}, fmt.Errorf("duplicate legacy and canonical drink inventory")
	}
	if err := actor.Items.Validate(); err != nil {
		return PlayerState{}, RoomState{}, err
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok {
		return PlayerState{}, RoomState{}, fmt.Errorf("drink actor room absent")
	}
	if room.Items != nil {
		if err := room.Items.Validate(); err != nil {
			return PlayerState{}, RoomState{}, err
		}
	}
	return actor, room, nil
}

func cloneDrinkActor(actor PlayerState) PlayerState {
	actor.Body.Inventory = cloneObjects(actor.Body.Inventory)
	if actor.Items != nil {
		items := actor.Items.clone()
		actor.Items = &items
	}
	actor.Aliases = clonePlayerAliases(actor.Aliases)
	actor.FollowerIDs = append([]string(nil), actor.FollowerIDs...)
	actor.PlayerEnemies = append([]string(nil), actor.PlayerEnemies...)
	if actor.NPCFollowerIDs != nil {
		actor.NPCFollowerIDs = append([]string(nil), actor.NPCFollowerIDs...)
	}
	if actor.FollowerRefs != nil {
		actor.FollowerRefs = append([]EntityRef(nil), actor.FollowerRefs...)
	}
	return actor
}

func validDrinkName(name string) bool {
	return name != "" && strings.TrimSpace(name) == name && !strings.ContainsAny(name, "\r\n\x00")
}

func selectDrinkRoot(actor PlayerState, name string, occurrence int) (string, Item, DrinkLocation, int, error) {
	if occurrence < 1 || !validDrinkName(name) || actor.Items == nil {
		return "", Item{}, "", -1, ErrDrinkMissingItem
	}
	found := 0
	for _, id := range actor.Items.Inventory {
		item, ok := actor.Items.Items[id]
		if !ok {
			return "", Item{}, "", -1, fmt.Errorf("canonical drink inventory root absent")
		}
		if equalInventorySelector(item.Object, name) {
			found++
			if found == occurrence {
				return id, item, DrinkInventoryRoot, -1, nil
			}
		}
	}
	// find_obj owns its own match counter. magic1.c therefore starts a fresh
	// positive occurrence count for the Ready fallback after Inventory fails.
	found = 0
	for slot, id := range actor.Items.Ready {
		if id == "" {
			continue
		}
		item, ok := actor.Items.Items[id]
		if !ok {
			return "", Item{}, "", -1, fmt.Errorf("canonical ready item absent")
		}
		if equalInventorySelector(item.Object, name) {
			found++
			if found == occurrence {
				return id, item, DrinkReadySlot, slot, nil
			}
		}
	}
	return "", Item{}, "", -1, fmt.Errorf("%w: %q", ErrDrinkMissingItem, name)
}

func drinkSpell(magicPower byte) (int, string, error) {
	if magicPower < 1 || int(magicPower) > drinkMaxSpellPower {
		return 0, "", fmt.Errorf("%w: magicpower=%d", ErrDrinkSpellUnavailable, magicPower)
	}
	index := int(magicPower) - 1
	if index >= len(legacyInfoSpellNames) || legacyInfoSpellNames[index] == "" {
		return 0, "", fmt.Errorf("%w: spell index=%d", ErrDrinkSpellUnavailable, index)
	}
	return index, legacyInfoSpellNames[index], nil
}

func drinkClassAllowed(actor LegacyMonster, object LegacyObject) bool {
	return !flag(object.Flags[:], drinkClassSelectFlag) || actor.Class >= drinkCaretakerClass || flag(object.Flags[:], uint(drinkClassSelectFlag)+uint(actor.Class))
}

func drinkAlignmentRejected(actor LegacyMonster, object LegacyObject) bool {
	return (flag(object.Flags[:], drinkGoodOnlyFlag) && actor.Alignment < -100) || (flag(object.Flags[:], drinkEvilOnlyFlag) && actor.Alignment > 100)
}

func drinkRoll(options DrinkOptions, low, high int, rolls *[]int) (int, error) {
	if options.Roll == nil {
		return 0, fmt.Errorf("%w: random effect requires injected RNG", ErrDrinkSpellUnavailable)
	}
	n := options.Roll(low, high)
	if n < low || n > high {
		return 0, fmt.Errorf("drink roll outside %d..%d", low, high)
	}
	*rolls = append(*rolls, n)
	return n, nil
}

func drinkReplayRoll(rolls []int, at *int, low, high int) (int, error) {
	if *at >= len(rolls) {
		return 0, fmt.Errorf("drink receipt missing random draw")
	}
	n := rolls[*at]
	*at++
	if n < low || n > high {
		return 0, fmt.Errorf("drink receipt roll outside %d..%d", low, high)
	}
	return n, nil
}

func drinkHP(body *LegacyMonster, delta int32) error {
	value := int64(body.HPCurrent) + int64(delta)
	if value < 0 || value > int64(body.HPMax) || value > math.MaxInt16 {
		return fmt.Errorf("drink hit point result outside legacy range")
	}
	body.HPCurrent = int16(value)
	return nil
}

func drinkHPClamped(body *LegacyMonster, delta int32) error {
	value := int64(body.HPCurrent) + int64(delta)
	if value < 0 || value > math.MaxInt16 || body.HPMax < 0 {
		return fmt.Errorf("drink hit point result outside legacy range")
	}
	if value > int64(body.HPMax) {
		value = int64(body.HPMax)
	}
	body.HPCurrent = int16(value)
	return nil
}

func drinkTimer(body *LegacyMonster, index int, now int32, interval int64) error {
	if index < 0 || index >= len(body.Timers) || now < 0 || interval < 0 || interval > math.MaxInt32 {
		return fmt.Errorf("drink timer outside legacy range")
	}
	body.Timers[index] = LegacyTimer{LastTime: now, Interval: int32(interval)}
	return nil
}

// drinkSpellSpec is intentionally a closed map. The C spell function table is
// much larger than the current canonical world model.
type drinkSpellSpec struct{ effect string }

var drinkSpells = map[int]drinkSpellSpec{
	0: {"vigor"}, 2: {"light"}, 3: {"cure-poison"}, 4: {"bless"}, 5: {"protect"},
	7: {"invisibility"}, 9: {"detect-invisible"}, 10: {"detect-magic"},
	12: {"confuse"}, 18: {"mend"}, 19: {"heal"}, 21: {"levitate"}, 22: {"fire-resist"},
	23: {"fly"}, 24: {"magic-resist"}, 43: {"cold-resist"}, 44: {"water-breathe"},
	45: {"shield"}, 48: {"cure-disease"}, 49: {"remove-blindness"}, 50: {"fear"},
	51: {"restore-mana"}, 53: {"blind"}, 54: {"silence"}, 55: {"charm"},
}

func applyDrinkSpell(body LegacyMonster, spec drinkSpellSpec, now int32, options DrinkOptions, replay []int) (LegacyMonster, string, int32, int32, []int, error) {
	after := body
	rolls := append([]int(nil), replay...)
	rollAt := 0
	draw := func(low, high int) (int, error) {
		if replay != nil {
			return drinkReplayRoll(replay, &rollAt, low, high)
		}
		return drinkRoll(options, low, high, &rolls)
	}
	hpBefore, mpBefore := after.HPCurrent, after.MPCurrent
	set := func(bit uint, timer int, interval int64) error {
		setSettingFlag(&after, bit, true)
		return drinkTimer(&after, timer, now, interval)
	}
	interval1200 := int64(1200)
	switch spec.effect {
	case "vigor":
		n, err := draw(1, 6)
		if err != nil {
			return LegacyMonster{}, "", 0, 0, nil, err
		}
		if err := drinkHPClamped(&after, int32(n)); err != nil {
			return LegacyMonster{}, "", 0, 0, nil, err
		}
	case "mend":
		for i := 0; i < 2; i++ {
			n, err := draw(1, 6)
			if err != nil {
				return LegacyMonster{}, "", 0, 0, nil, err
			}
			if err := drinkHPClamped(&after, int32(n)); err != nil {
				return LegacyMonster{}, "", 0, 0, nil, err
			}
		}
	case "light":
		if err := set(17, 13, 300+int64((int(after.Level)+3)/4)*300); err != nil {
			return LegacyMonster{}, "", 0, 0, nil, err
		}
	case "cure-poison":
		setSettingFlag(&after, 16, false)
	case "bless":
		if err := set(0, 2, interval1200); err != nil {
			return LegacyMonster{}, "", 0, 0, nil, err
		}
	case "protect":
		if err := set(8, 1, interval1200); err != nil {
			return LegacyMonster{}, "", 0, 0, nil, err
		}
	case "invisibility":
		if err := set(2, 0, interval1200); err != nil {
			return LegacyMonster{}, "", 0, 0, nil, err
		}
	case "restore-mana":
		for i := 0; i < 2; i++ {
			n, err := draw(1, 10)
			if err != nil {
				return LegacyMonster{}, "", 0, 0, nil, err
			}
			if err := drinkHPClamped(&after, int32(n)); err != nil {
				return LegacyMonster{}, "", 0, 0, nil, err
			}
		}
		// The original first restores 2d10 HP and then has a 60% chance to
		// fill MP. Keep all three draws explicit for deterministic receipts.
		if n, err := draw(1, 100); err != nil {
			return LegacyMonster{}, "", 0, 0, nil, err
		} else if n < 60 {
			after.MPCurrent = after.MPMax
		}
	case "detect-invisible":
		if err := set(21, 17, interval1200); err != nil {
			return LegacyMonster{}, "", 0, 0, nil, err
		}
	case "detect-magic":
		if err := set(20, 18, interval1200); err != nil {
			return LegacyMonster{}, "", 0, 0, nil, err
		}
	case "confuse":
		a, err := draw(1, 6)
		if err != nil {
			return LegacyMonster{}, "", 0, 0, nil, err
		}
		b, err := draw(1, 6)
		if err != nil {
			return LegacyMonster{}, "", 0, 0, nil, err
		}
		interval := int64(a + b)
		if interval < 6 {
			interval = 6
		}
		if err := drinkTimer(&after, 3, now, interval); err != nil {
			return LegacyMonster{}, "", 0, 0, nil, err
		}
	case "heal":
		delta := int32(after.HPMax - after.HPCurrent)
		if delta < 0 {
			return LegacyMonster{}, "", 0, 0, nil, fmt.Errorf("negative hit point deficit")
		}
		after.HPCurrent = after.HPMax
		return after, spec.effect, delta, int32(after.MPCurrent - mpBefore), rolls, nil
	case "levitate":
		if err := set(25, 21, interval1200); err != nil {
			return LegacyMonster{}, "", 0, 0, nil, err
		}
	case "fire-resist":
		if err := set(30, 23, interval1200); err != nil {
			return LegacyMonster{}, "", 0, 0, nil, err
		}
	case "fly":
		if err := set(31, 24, interval1200); err != nil {
			return LegacyMonster{}, "", 0, 0, nil, err
		}
	case "magic-resist":
		if err := set(32, 25, interval1200); err != nil {
			return LegacyMonster{}, "", 0, 0, nil, err
		}
	case "cold-resist":
		if err := set(36, 29, interval1200); err != nil {
			return LegacyMonster{}, "", 0, 0, nil, err
		}
	case "water-breathe":
		if err := set(37, 30, interval1200); err != nil {
			return LegacyMonster{}, "", 0, 0, nil, err
		}
	case "shield":
		if err := set(38, 31, interval1200); err != nil {
			return LegacyMonster{}, "", 0, 0, nil, err
		}
	case "cure-disease":
		setSettingFlag(&after, 41, false)
	case "remove-blindness":
		setSettingFlag(&after, 42, false)
	case "fear":
		n, err := draw(1, 30)
		if err != nil {
			return LegacyMonster{}, "", 0, 0, nil, err
		}
		interval := int64(600 + n*10)
		if flag(after.Flags[:], 32) {
			interval /= 2
		}
		if err := set(43, 33, interval); err != nil {
			return LegacyMonster{}, "", 0, 0, nil, err
		}
	case "blind":
		setSettingFlag(&after, 42, true)
	case "silence":
		if after.Class < drinkSubDMClass {
			return LegacyMonster{}, "", 0, 0, nil, ErrDrinkClass
		}
		n, err := draw(1, 15)
		if err != nil {
			return LegacyMonster{}, "", 0, 0, nil, err
		}
		interval := int64(300 + n*10)
		if flag(after.Flags[:], 32) {
			interval /= 2
		}
		if err := set(44, 34, interval); err != nil {
			return LegacyMonster{}, "", 0, 0, nil, err
		}
	case "charm":
		n, err := draw(1, 15)
		if err != nil {
			return LegacyMonster{}, "", 0, 0, nil, err
		}
		if err := set(45, 35, 100+int64(n*10)); err != nil {
			return LegacyMonster{}, "", 0, 0, nil, err
		}
	default:
		return LegacyMonster{}, "", 0, 0, nil, ErrDrinkSpellUnavailable
	}
	return after, spec.effect, int32(after.HPCurrent - hpBefore), int32(after.MPCurrent - mpBefore), rolls, nil
}

func drinkSpecialBody(body LegacyMonster, object LegacyObject, now int32) (LegacyMonster, string, error) {
	after := body
	switch object.Special {
	case 1:
		value := int64(after.Timers[drinkHoursTimer].Interval) - 86400
		if value < 0 {
			value = 0
		}
		if value > math.MaxInt32 {
			return LegacyMonster{}, "", ErrDrinkSpecial
		}
		after.Timers[drinkHoursTimer].Interval = int32(value)
		return after, "hours-minus", nil
	case 2:
		setSettingFlag(&after, 28, !flag(after.Flags[:], 28))
		return after, "chaos-toggle", nil
	case 3:
		value := int64(after.Timers[drinkHoursTimer].Interval) + 86400
		if value > math.MaxInt32 {
			return LegacyMonster{}, "", ErrDrinkSpecial
		}
		after.Timers[drinkHoursTimer].Interval = int32(value)
		return after, "hours-plus", nil
	case 4:
		total := 54 + int(after.Level)/4
		if after.Class > 8 {
			total += 25
		}
		if flag(after.Flags[:], 19) { // PHASTE
			total += 15
		}
		if flag(after.Flags[:], 52) { // PPOWER
			total += 3
		}
		if flag(after.Flags[:], 54) { // PMEDIT
			total += 3
		}
		if flag(after.Flags[:], 22) { // PPRAYD
			total += 3
		}
		sum := 0
		for _, stat := range after.Stats {
			sum += int(int8(stat))
		}
		if sum <= total+3 {
			for i := range after.Stats {
				if int8(after.Stats[i]) == 127 {
					return LegacyMonster{}, "", ErrDrinkSpecial
				}
				after.Stats[i]++
			}
		}
		return after, "stats-plus", nil
	case 6:
		setSettingFlag(&after, 16, true)
		return after, "poison", nil
	case 5:
		// Room admission and level bounds are checked by PlanDrink. The body
		// itself has no special mutation until the room membership commit.
		return after, "map-teleport", nil
	default:
		return LegacyMonster{}, "", ErrDrinkSpecial
	}
}

func drinkUseResponse(object LegacyObject, effect string) string {
	var out strings.Builder
	if object.UseOutput != "" {
		out.WriteString(object.UseOutput)
		out.WriteString("\r\n")
	}
	if effect != "" {
		fmt.Fprintf(&out, "\n%s\r\n", effect)
	}
	fmt.Fprintf(&out, "\n당신은 %s을(를) 먹었습니다.\r\n", object.Name)
	return out.String()
}

func drinkEvent(actor LegacyMonster, actorID, itemID, itemName, spell, text string) DrinkEvent {
	return DrinkEvent{RoomID: actor.RoomID, ActorID: actorID, ActorName: actor.Name, ItemID: itemID, ItemName: itemName, SpellName: spell, ExcludeActorID: actorID, Text: text}
}

func removeDrinkRoot(items *ItemCollection, location DrinkLocation, readySlot int, root string) error {
	if items == nil {
		return fmt.Errorf("drink inventory absent")
	}
	if err := items.Validate(); err != nil {
		return err
	}
	if location == DrinkInventoryRoot {
		remaining, err := removeInventoryRoot(items.Inventory, root)
		if err != nil {
			return err
		}
		items.Inventory = remaining
	} else if location == DrinkReadySlot && readySlot >= 0 && readySlot < len(items.Ready) && items.Ready[readySlot] == root {
		items.Ready[readySlot] = ""
	} else {
		return fmt.Errorf("drink root location changed")
	}
	stack, seen := []string{root}, map[string]bool{}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[id] {
			return fmt.Errorf("cyclic drink item subtree")
		}
		seen[id] = true
		item, ok := items.Items[id]
		if !ok {
			return fmt.Errorf("drink item subtree absent")
		}
		stack = append(stack, item.Contents...)
		delete(items.Items, id)
	}
	return items.Validate()
}

func moveDrinkRoot(source, destination *ItemCollection, location DrinkLocation, readySlot int, root string) error {
	if source == nil || destination == nil {
		return fmt.Errorf("drink canonical transfer unavailable")
	}
	if err := source.Validate(); err != nil {
		return err
	}
	if err := destination.Validate(); err != nil {
		return err
	}
	if location == DrinkInventoryRoot {
		plan, err := TransferItemRoots(*source, *destination, []string{root})
		if err != nil {
			return err
		}
		*source, *destination = plan.Source, plan.Destination
		return nil
	}
	if location != DrinkReadySlot || readySlot < 0 || readySlot >= len(source.Ready) || source.Ready[readySlot] != root {
		return fmt.Errorf("drink ready root changed")
	}
	moved := map[string]Item{}
	nextSource := source.clone()
	nextSource.Ready[readySlot] = ""
	stack, seen := []string{root}, map[string]bool{}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[id] {
			return fmt.Errorf("cyclic drink item subtree")
		}
		seen[id] = true
		item, ok := nextSource.Items[id]
		if !ok {
			return fmt.Errorf("drink item subtree absent")
		}
		moved[id] = item
		delete(nextSource.Items, id)
		stack = append(stack, item.Contents...)
	}
	nextDestination := destination.clone()
	for id, item := range moved {
		if _, exists := nextDestination.Items[id]; exists {
			return fmt.Errorf("overlapping drink item owners")
		}
		nextDestination.Items[id] = item
	}
	object := moved[root].Object
	at := len(nextDestination.Inventory)
	for i, id := range nextDestination.Inventory {
		other := nextDestination.Items[id].Object
		if other.Name > object.Name || (other.Name == object.Name && int8(other.Adjustment) > int8(object.Adjustment)) {
			at = i
			break
		}
	}
	nextDestination.Inventory = append(nextDestination.Inventory, "")
	copy(nextDestination.Inventory[at+1:], nextDestination.Inventory[at:])
	nextDestination.Inventory[at] = root
	if err := nextSource.Validate(); err != nil {
		return err
	}
	if err := nextDestination.Validate(); err != nil {
		return err
	}
	*source, *destination = nextSource, nextDestination
	return nil
}

func drinkResult(p DrinkProposal, actor PlayerState) DrinkResult {
	return DrinkResult{Action: "drink", Response: p.Response, Broadcast: p.Broadcast, Changed: p.Consumed || p.MovedToRoom || p.ClearHidden, Consumed: p.Consumed, MovedToRoom: p.MovedToRoom, Special: p.Special, SpecialKind: p.SpecialKind, ItemID: p.ItemID, ItemName: p.ItemName, Occurrence: p.Occurrence, Location: p.Location, ReadySlot: p.ReadySlot, MagicPower: p.MagicPower, SpellIndex: p.SpellIndex, SpellName: p.SpellName, Effect: p.Effect, HPDelta: p.HPDelta, MPDelta: p.MPDelta, ShotsBefore: p.ShotsBefore, ShotsAfter: p.ShotsAfter, RoomID: p.RoomID, ActorID: p.ActorID, ActorName: actor.Body.Name}
}

// PlanDrink resolves, validates and evaluates one potion without mutating s.
// Unsupported spell functions return an error before item consumption.
func (s State) PlanDrink(actorID, itemName string, occurrence int, options DrinkOptions) (DrinkProposal, error) {
	if options.Now < 0 {
		return DrinkProposal{}, fmt.Errorf("drink clock must be nonnegative")
	}
	actor, room, err := drinkActor(s, actorID)
	if err != nil {
		return DrinkProposal{}, err
	}
	itemID, item, location, readySlot, err := selectDrinkRoot(actor, itemName, occurrence)
	if err != nil {
		return DrinkProposal{}, err
	}
	if item.Object.Type != drinkPotionType {
		return DrinkProposal{}, fmt.Errorf("%w: %s", ErrDrinkNotPotion, item.Object.Name)
	}
	if item.Object.ShotsCurrent < 0 {
		return DrinkProposal{}, fmt.Errorf("negative potion charges")
	}
	p := DrinkProposal{Action: "drink", ActorID: actorID, RoomID: actor.Body.RoomID, ItemID: itemID, ItemName: item.Object.Name, Occurrence: occurrence, Location: location, ReadySlot: readySlot, MagicPower: item.Object.MagicPower, Now: options.Now, ShotsBefore: item.Object.ShotsCurrent, expectedActor: cloneDrinkActor(actor), expectedItem: item}
	p.expectedRoomFlags = room.Resource.Flags
	if room.Items != nil {
		p.expectedRoomItems = room.Items.clone()
		p.expectedRoomSet = true
	}
	if flag(item.Object.Flags[:], drinkSpecialFlag) {
		p.Special, p.SpecialKind = true, item.Object.Special
		after, effect, err := drinkSpecialBody(actor.Body, item.Object, options.Now)
		if err != nil {
			return DrinkProposal{}, err
		}
		p.Effect, p.Consumed, p.Broadcast = effect, true, true
		p.ShotsAfter = item.Object.ShotsCurrent
		if item.Object.Special != 5 {
			p.ShotsAfter = item.Object.ShotsCurrent - 1
			if p.ShotsAfter < 0 {
				p.ShotsAfter = 0
			}
		}
		if item.Object.Special == 5 {
			destination := int16(item.Object.Value)
			if int32(destination) != item.Object.Value {
				return DrinkProposal{}, ErrDrinkSpecial
			}
			dest, ok := s.Rooms[destination]
			if !ok || destination == actor.Body.RoomID || (dest.Resource.HighLevel != 0 && actor.Body.Level > dest.Resource.HighLevel) || (dest.Resource.LowLevel != 0 && actor.Body.Level < dest.Resource.LowLevel) {
				return DrinkProposal{}, ErrDrinkSpecial
			}
			p.MovedToRoom, p.expectedDestination, p.expectedDestinationSet = true, destination, true
			after.RoomID = destination
			p.ShotsAfter = item.Object.ShotsCurrent - 1
			if p.ShotsAfter < 0 {
				p.ShotsAfter = 0
			}
		}
		p.Response = drinkUseResponse(item.Object, effect)
		p.RoomText = fmt.Sprintf("\n%s이 %s을(를) 먹었습니다.\r\n", actor.Body.Name, item.Object.Name)
		// pdice 5 has no body effect beyond movement; all other special body
		// mutations are committed through the expected post-state below.
		p.afterBody = after
		return p, nil
	}
	if item.Object.ShotsCurrent < 1 {
		return DrinkProposal{}, ErrDrinkEmpty
	}
	if flag(room.Resource.Flags[:], drinkNoPotionRoomFlag) {
		return DrinkProposal{}, ErrDrinkNoPotionRoom
	}
	if flag(room.Resource.Flags[:], drinkSurvivalFlag) {
		return DrinkProposal{}, ErrDrinkSurvivalRoom
	}
	if drinkAlignmentRejected(actor.Body, item.Object) {
		p.MovedToRoom, p.Response = true, fmt.Sprintf("%s가 당신의 손안에서 타버립니다.\r\n", item.Object.Name)
		return p, nil
	}
	if !drinkClassAllowed(actor.Body, item.Object) {
		return DrinkProposal{}, ErrDrinkClass
	}
	index, spellName, err := drinkSpell(item.Object.MagicPower)
	if err != nil {
		return DrinkProposal{}, err
	}
	spec, ok := drinkSpells[index]
	if !ok {
		return DrinkProposal{}, fmt.Errorf("%w: %s", ErrDrinkSpellUnavailable, spellName)
	}
	after, effect, hpDelta, mpDelta, rolls, err := applyDrinkSpell(actor.Body, spec, options.Now, options, nil)
	if err != nil {
		return DrinkProposal{}, err
	}
	p.SpellIndex, p.SpellName, p.Effect, p.HPDelta, p.MPDelta, p.Rolls = index, spellName, effect, hpDelta, mpDelta, rolls
	p.Consumed, p.ClearHidden, p.Broadcast = true, true, true
	p.ShotsAfter = item.Object.ShotsCurrent - 1
	if p.ShotsAfter < 0 {
		p.ShotsAfter = 0
	}
	p.Response = drinkUseResponse(item.Object, effect)
	p.RoomText = fmt.Sprintf("\n%s이 %s을(를) 먹었습니다.\r\n", actor.Body.Name, item.Object.Name)
	_ = after
	return p, nil
}

// ApplyDrink commits a previously planned potion. It re-evaluates only from
// stored draws and rejects any changed actor, room, item or effect mapping.
func (s State) ApplyDrink(p DrinkProposal) (State, DrinkResult, error) {
	actor, room, err := drinkActor(s, p.ActorID)
	if err != nil {
		return State{}, DrinkResult{}, err
	}
	if p.Action != "drink" || p.RoomID != actor.Body.RoomID || p.ItemID == "" ||
		p.ItemName == "" || p.Occurrence < 1 || p.Response == "" {
		return State{}, DrinkResult{}, fmt.Errorf("invalid drink proposal")
	}
	if !reflect.DeepEqual(actor, p.expectedActor) {
		return State{}, DrinkResult{}, fmt.Errorf("stale drink actor")
	}
	if room.Resource.Flags != p.expectedRoomFlags {
		return State{}, DrinkResult{}, fmt.Errorf("stale drink room flags")
	}
	if p.expectedRoomSet && (room.Items == nil || !reflect.DeepEqual(*room.Items, p.expectedRoomItems)) {
		return State{}, DrinkResult{}, fmt.Errorf("stale drink room")
	}
	id, item, location, readySlot, err := selectDrinkRoot(actor, p.ItemName, p.Occurrence)
	if err != nil || id != p.ItemID || location != p.Location || readySlot != p.ReadySlot || !reflect.DeepEqual(item, p.expectedItem) {
		return State{}, DrinkResult{}, fmt.Errorf("stale drink item")
	}
	if p.MagicPower != item.Object.MagicPower || p.ShotsBefore != item.Object.ShotsCurrent {
		return State{}, DrinkResult{}, fmt.Errorf("stale drink item metadata")
	}
	if p.Special {
		if !p.Consumed || !p.Broadcast || p.SpecialKind < 1 || p.SpecialKind > 6 ||
			(p.SpecialKind == 5) != p.MovedToRoom {
			return State{}, DrinkResult{}, fmt.Errorf("invalid drink special proposal")
		}
	} else if p.MovedToRoom {
		want := fmt.Sprintf("%s가 당신의 손안에서 타버립니다.\r\n", item.Object.Name)
		if p.Consumed || p.Broadcast || p.ClearHidden || p.RoomText != "" ||
			p.Response != want || !drinkAlignmentRejected(actor.Body, item.Object) {
			return State{}, DrinkResult{}, fmt.Errorf("invalid drink alignment proposal")
		}
	} else if !p.Consumed || !p.ClearHidden || !p.Broadcast || p.SpellIndex < 0 || p.SpellName == "" {
		return State{}, DrinkResult{}, fmt.Errorf("invalid drink spell proposal")
	}
	if p.Special {
		if p.Response != drinkUseResponse(item.Object, p.Effect) ||
			p.RoomText != fmt.Sprintf("\n%s이 %s을(를) 먹었습니다.\r\n", actor.Body.Name, item.Object.Name) {
			return State{}, DrinkResult{}, fmt.Errorf("invalid drink special response")
		}
	} else if !p.MovedToRoom && p.Response == "" {
		return State{}, DrinkResult{}, fmt.Errorf("invalid drink response")
	}
	next := s.clone()
	nextActor := next.Players[p.ActorID]
	nextRoom := next.Rooms[p.RoomID]

	if p.MovedToRoom && !p.Special {
		if nextRoom.Items == nil {
			return State{}, DrinkResult{}, fmt.Errorf("drink room transfer unavailable")
		}
		if err := moveDrinkRoot(nextActor.Items, nextRoom.Items, p.Location, p.ReadySlot, p.ItemID); err != nil {
			return State{}, DrinkResult{}, err
		}
		next.Rooms[p.RoomID] = nextRoom
	} else if p.Special {
		after, effect, err := drinkSpecialBody(actor.Body, item.Object, p.Now)
		if err != nil || effect != p.Effect {
			return State{}, DrinkResult{}, fmt.Errorf("stale drink special effect")
		}
		if p.SpecialKind == 5 {
			if !p.expectedDestinationSet {
				return State{}, DrinkResult{}, ErrDrinkSpecial
			}
			dest, ok := next.Rooms[p.expectedDestination]
			if !ok {
				return State{}, DrinkResult{}, ErrDrinkSpecial
			}
			after.RoomID = p.expectedDestination
			if !reflect.DeepEqual(after, p.afterBody) {
				return State{}, DrinkResult{}, fmt.Errorf("stale drink special body")
			}
			nextActor.Body = after
			sourceRoom := next.Rooms[p.RoomID]
			removeRoomPlayer(&sourceRoom, p.ActorID)
			next.Rooms[p.RoomID] = sourceRoom
			addRoomPlayer(&dest, p.ActorID)
			next.Rooms[p.expectedDestination] = dest
		} else {
			if !reflect.DeepEqual(after, p.afterBody) {
				return State{}, DrinkResult{}, fmt.Errorf("stale drink special body")
			}
			nextActor.Body = after
		}
	}

	if !p.Special && !p.MovedToRoom {
		spec, ok := drinkSpells[p.SpellIndex]
		if !ok {
			return State{}, DrinkResult{}, ErrDrinkSpellUnavailable
		}
		after, effect, hp, mp, _, err := applyDrinkSpell(actor.Body, spec, p.Now, DrinkOptions{}, p.Rolls)
		if err != nil || effect != p.Effect || hp != p.HPDelta || mp != p.MPDelta {
			return State{}, DrinkResult{}, fmt.Errorf("stale drink spell effect")
		}
		if p.Response != drinkUseResponse(item.Object, effect) ||
			p.RoomText != fmt.Sprintf("\n%s이 %s을(를) 먹었습니다.\r\n", actor.Body.Name, item.Object.Name) {
			return State{}, DrinkResult{}, fmt.Errorf("stale drink response")
		}
		nextActor.Body = after
		setSettingFlag(&nextActor.Body, 1, false)
	}

	if p.Consumed {
		if p.SpecialKind == 5 || !p.Special {
			if p.ShotsAfter > 0 {
				kept, ok := nextActor.Items.Items[p.ItemID]
				if !ok {
					return State{}, DrinkResult{}, fmt.Errorf("drink potion disappeared")
				}
				kept.Object.ShotsCurrent = p.ShotsAfter
				nextActor.Items.Items[p.ItemID] = kept
			} else if err := removeDrinkRoot(nextActor.Items, p.Location, p.ReadySlot, p.ItemID); err != nil {
				return State{}, DrinkResult{}, err
			}
		} else if err := removeDrinkRoot(nextActor.Items, p.Location, p.ReadySlot, p.ItemID); err != nil {
			return State{}, DrinkResult{}, err
		}
	}
	next.Players[p.ActorID] = nextActor
	if err := next.Validate(); err != nil {
		return State{}, DrinkResult{}, err
	}
	result := drinkResult(p, nextActor)
	if p.Broadcast {
		event := drinkEvent(actor.Body, p.ActorID, p.ItemID, p.ItemName, p.SpellName, p.RoomText)
		result.Event = &event
	}
	return next, result, nil
}

func removeRoomPlayer(room *RoomState, actorID string) {
	ids := room.PlayerIDs[:0]
	for _, id := range room.PlayerIDs {
		if id != actorID {
			ids = append(ids, id)
		}
	}
	room.PlayerIDs = ids
}
func addRoomPlayer(room *RoomState, actorID string) {
	for _, id := range room.PlayerIDs {
		if id == actorID {
			return
		}
	}
	room.PlayerIDs = append(room.PlayerIDs, actorID)
}

func (s State) DrinkItemByName(actorID, itemName string, occurrence int, options DrinkOptions) (State, DrinkResult, error) {
	p, err := s.PlanDrink(actorID, itemName, occurrence, options)
	if err != nil {
		return State{}, DrinkResult{}, err
	}
	return s.ApplyDrink(p)
}
