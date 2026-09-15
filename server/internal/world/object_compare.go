package world

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// CompareKind identifies the two object families handled by command12.c's
// obj_compare.  Other legacy object types are deliberately not coerced into a
// weapon or armor comparison.
type CompareKind string

const (
	CompareWeapon      CompareKind = "weapon"
	CompareArmor       CompareKind = "armor"
	CompareUnsupported CompareKind = "unsupported"
)

// CompareErrorCode is a source-level outcome, not a Go execution error.  A
// missing object and an object of an unsupported type are ordinary responses
// from obj_compare, so they are retained in a read-only receipt instead of
// becoming an unrecorded transport failure.
type CompareErrorCode string

const (
	CompareMissingItem CompareErrorCode = "missing-item"
	// CompareItemNotFound is the concise error-code alias used by callers
	// that mirror the legacy find_obj terminology.
	CompareItemNotFound    CompareErrorCode = CompareMissingItem
	CompareUnsupportedItem CompareErrorCode = "unsupported-item"
)

// CompareClassLevels is the INVINCIBLE+ weapon table printed by obj_compare.
// The labels intentionally describe the source's grouped classes rather than
// inventing a new class taxonomy.
type CompareClassLevels struct {
	Fighter       int `json:"fighter"`
	AssassinThief int `json:"assassin_thief"`
	PaladinRanger int `json:"paladin_ranger"`
	Other         int `json:"other"`
}

// CompareResult is the deterministic, read-only projection of command12.c.
// The ItemID and occurrence bind the display to one canonical root for
// receipt/replay auditing; clients should render Response rather than use the
// ID as authority for a later mutation.
type CompareResult struct {
	Action        string             `json:"action"`
	ActorID       string             `json:"actor_id"`
	RoomID        int16              `json:"room_id"`
	ItemID        string             `json:"item_id"`
	ItemName      string             `json:"item_name"`
	DisplayName   string             `json:"display_name"`
	Occurrence    int                `json:"occurrence"`
	Kind          CompareKind        `json:"kind"`
	Success       bool               `json:"success"`
	ErrorCode     CompareErrorCode   `json:"error_code"`
	CheckAC       int                `json:"check_ac"`
	CheckDamage   int                `json:"check_damage"`
	RequiredLevel int                `json:"required_level"`
	Universal     bool               `json:"universal"`
	ClassLevels   CompareClassLevels `json:"class_levels"`
	Response      string             `json:"response"`
}

var (
	// These errors identify failures at the canonical state boundary.  They
	// are distinct from CompareResult.ErrorCode, which represents a valid
	// source response and is durably recorded by ExecuteCompareLine.
	ErrCompareActorAbsent                = errors.New("online compare actor absent")
	ErrCompareCanonicalInventoryRequired = errors.New("canonical player inventory required")
	ErrCompareInvalidOccurrence          = errors.New("invalid compare occurrence")
	ErrCompareItemNameRequired           = errors.New("compare item name required")
	ErrCompareItemNotFound               = errors.New("당신은 그런 것을 갖고 있지 않습니다.")
)

const (
	compareArmorType         = 5  // ARMOR
	compareBodyWear          = 1  // BODY
	compareInvincibleClass   = 9  // INVINCIBLE
	compareAssassinClass     = 1  // ASSASSIN
	compareFighterClass      = 4  // FIGHTER
	comparePaladinClass      = 6  // PALADIN
	compareRangerClass       = 7  // RANGER
	compareThiefClass        = 8  // THIEF
	comparePlayerDetectMagic = 20 // PDMAGI
)

// ComparePrompt is the source response for a bare 비교 command.  It keeps
// actor/session validation at the same canonical boundary as a targeted
// comparison while preserving command12.c's no-argument branch.
func (s State) ComparePrompt(actorID string) (CompareResult, error) {
	actor, err := compareActor(s, actorID)
	if err != nil {
		return CompareResult{}, err
	}
	return CompareResult{
		Action:     "compare",
		ActorID:    actorID,
		RoomID:     actor.Body.RoomID,
		Occurrence: 0,
		Success:    false,
		ErrorCode:  CompareMissingItem,
		Response:   "무엇을 비교하시려고요?\r\n",
	}, nil
}

// CompareItemByName ports obj_compare's player-inventory path.  Selection is
// intentionally narrower than legacy find_obj: it accepts an exact,
// case-insensitive display name and a positive one-based occurrence, and it
// searches only direct canonical inventory roots in stored order.  Nested
// roots, equipped items, legacy linked-list objects, and un-migrated players
// are never guessed or used as a fallback.
func (s State) CompareItemByName(actorID, name string, occurrence int) (CompareResult, error) {
	actor, err := compareActor(s, actorID)
	if err != nil {
		return CompareResult{}, err
	}
	if occurrence < 1 {
		return CompareResult{}, ErrCompareInvalidOccurrence
	}
	if err := validCompareItemName(name); err != nil {
		return CompareResult{}, err
	}

	itemID, item, err := selectCompareInventoryRoot(*actor.Items, name, occurrence, flag(actor.Body.Flags[:], playerDetectInvisibleFlag))
	if errors.Is(err, ErrCompareItemNotFound) {
		return CompareResult{
			Action:     "compare",
			ActorID:    actorID,
			RoomID:     actor.Body.RoomID,
			ItemName:   name,
			Occurrence: occurrence,
			Success:    false,
			ErrorCode:  CompareMissingItem,
			Response:   "당신은 그런 것을 갖고 있지 않습니다.\r\n",
		}, nil
	}
	if err != nil {
		return CompareResult{}, err
	}

	displayName := compareDisplayName(actor.Body, item.Object)
	result := CompareResult{
		Action:      "compare",
		ActorID:     actorID,
		RoomID:      actor.Body.RoomID,
		ItemID:      itemID,
		ItemName:    item.Object.Name,
		DisplayName: displayName,
		Occurrence:  occurrence,
		Success:     true,
		Response:    "",
	}

	switch {
	case item.Object.Type == compareArmorType:
		result.Kind = CompareArmor
		result.CheckAC = int(int8(item.Object.Armor)) * 5
		if item.Object.Wear == compareBodyWear {
			result.CheckAC = int(int8(item.Object.Armor)) * 2
		}
		if result.CheckAC > 29 {
			result.RequiredLevel = result.CheckAC
			result.Response = fmt.Sprintf("%s%s %d 레벨부터 입을 수 있습니다.\r\n", displayName, compareTopicParticle(displayName), result.CheckAC)
		} else {
			result.Universal = true
			result.Response = fmt.Sprintf("%s%s 누구나 입을 수 있습니다.\r\n", displayName, compareTopicParticle(displayName))
		}
		return result, nil

	case item.Object.Type < compareArmorType:
		result.Kind = CompareWeapon
		result.CheckDamage = int(item.Object.DiceCount)*int(item.Object.DiceSides) + int(item.Object.DicePlus)
		switch actor.Body.Class {
		case compareFighterClass:
			result.CheckDamage -= 7
		case compareAssassinClass, compareThiefClass:
			result.CheckDamage -= 3
		case comparePaladinClass, compareRangerClass:
			result.CheckDamage -= 2
		}

		if actor.Body.Class >= compareInvincibleClass {
			if result.CheckDamage > 15 {
				result.ClassLevels = CompareClassLevels{
					Fighter:       (result.CheckDamage - 7) * 3,
					AssassinThief: (result.CheckDamage - 3) * 3,
					PaladinRanger: (result.CheckDamage - 2) * 3,
					Other:         result.CheckDamage * 3,
				}
				result.Response = fmt.Sprintf("%s%s 검사는 %d레벨, 자객 도둑은 %d레벨, 무사 포졸은 %d레벨,\r\n권법가 불제자 도술사는 %d레벨부터 사용할 수 있습니다.\r\n", displayName, compareTopicParticle(displayName), result.ClassLevels.Fighter, result.ClassLevels.AssassinThief, result.ClassLevels.PaladinRanger, result.ClassLevels.Other)
			} else {
				result.Universal = true
				result.Response = fmt.Sprintf("%s%s 누구나 무장할 수 있습니다.\r\n", displayName, compareTopicParticle(displayName))
			}
			return result, nil
		}

		if result.CheckDamage > 15 {
			result.RequiredLevel = result.CheckDamage * 3
			result.Response = fmt.Sprintf("%s%s %d 레벨부터 무장할 수 있습니다.\r\n", displayName, compareTopicParticle(displayName), result.RequiredLevel)
		} else {
			result.Universal = true
			result.Response = fmt.Sprintf("%s%s 누구나 무장할 수 있습니다.\r\n", displayName, compareTopicParticle(displayName))
		}
		return result, nil

	default:
		result.Kind = CompareUnsupported
		result.Success = false
		result.ErrorCode = CompareUnsupportedItem
		result.Response = "무기나 방어구만 가능합니다.\r\n"
		return result, nil
	}
}

// Compare is a concise alias for non-session callers.
func (s State) Compare(actorID, name string, occurrence int) (CompareResult, error) {
	return s.CompareItemByName(actorID, name, occurrence)
}

// CompareObjectByName is a descriptive compatibility alias for callers that
// use the legacy object terminology.
func (s State) CompareObjectByName(actorID, name string, occurrence int) (CompareResult, error) {
	return s.CompareItemByName(actorID, name, occurrence)
}

func compareActor(s State, actorID string) (PlayerState, error) {
	if err := s.Validate(); err != nil {
		return PlayerState{}, err
	}
	if actorID == "" {
		return PlayerState{}, ErrCompareActorAbsent
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Body.Type != 0 || actor.Body.Name == "" || actor.Body.Class > 12 {
		return PlayerState{}, ErrCompareActorAbsent
	}
	// A non-nil collection is the explicit migration marker.  Never fall back
	// to Body.Inventory, even when it happens to be empty.
	if actor.Items == nil || len(actor.Body.Inventory) != 0 {
		return PlayerState{}, ErrCompareCanonicalInventoryRequired
	}
	if err := actor.Items.Validate(); err != nil {
		return PlayerState{}, fmt.Errorf("%w: %v", ErrCompareCanonicalInventoryRequired, err)
	}
	return actor, nil
}

func validCompareItemName(name string) error {
	if name == "" || strings.TrimSpace(name) != name || !utf8.ValidString(name) {
		return ErrCompareItemNameRequired
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == '\r' || r == '\n' || r == '\x00' {
			return ErrCompareItemNameRequired
		}
	}
	return nil
}

func selectCompareInventoryRoot(collection ItemCollection, name string, occurrence int, detectInvisible bool) (string, Item, error) {
	if err := collection.Validate(); err != nil {
		return "", Item{}, fmt.Errorf("%w: %v", ErrCompareCanonicalInventoryRequired, err)
	}
	found := 0
	for _, id := range collection.Inventory {
		item, ok := collection.Items[id]
		if !ok || id == "" {
			return "", Item{}, fmt.Errorf("%w: missing root %q", ErrCompareCanonicalInventoryRequired, id)
		}
		if !strings.EqualFold(item.Object.Name, name) {
			continue
		}
		if flag(item.Object.Flags[:], objectInvisibleFlag) && !detectInvisible {
			continue
		}
		// A root with Contents is a nested graph boundary.  Do not compare the
		// container as if it were a leaf, and never search its descendants.
		if len(item.Contents) != 0 {
			continue
		}
		found++
		if found == occurrence {
			return id, item, nil
		}
	}
	return "", Item{}, ErrCompareItemNotFound
}

func compareDisplayName(actor LegacyMonster, object LegacyObject) string {
	name := strings.TrimRight(object.Name, " ")
	if flag(actor.Flags[:], comparePlayerDetectMagic) {
		if object.Adjustment != 0 {
			return fmt.Sprintf("%s(%+d)", name, int(int8(object.Adjustment)))
		}
		if object.MagicPower != 0 {
			return name + "(주문)"
		}
	}
	return name
}

// compareTopicParticle ports print's %j with selector 0 (은/는).  As in the
// source formatter, a detected magic/adjustment suffix is ignored when
// determining the final Hangul syllable.
func compareTopicParticle(name string) string {
	if strings.HasSuffix(name, ")") {
		if at := strings.LastIndexByte(name, '('); at >= 0 {
			name = name[:at]
		}
	}
	runes := []rune(name)
	if len(runes) == 0 {
		return "는"
	}
	last := runes[len(runes)-1]
	if last >= 0xAC00 && last <= 0xD7A3 && (last-0xAC00)%28 != 0 {
		return "은"
	}
	return "는"
}
