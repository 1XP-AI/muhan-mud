package world

import (
	"fmt"
	"strings"
)

// RoomRepairFlag is RREPAI from src/mtype.h. Value uses the pawn branch when
// both service flags are present, matching command7.c's if/else ordering.
const RoomRepairFlag = 7

const valuePawnMax = int64(100000)

// ValueMode identifies the service room that supplied a valuation quote.
type ValueMode string

const (
	ValueModePawn   ValueMode = "pawn"
	ValueModeRepair ValueMode = "repair"
)

// ValueResult is the deterministic, read-only result of command7.c:value.
// ItemID is retained for audit/replay, while clients should render Response
// and must not use the ID as an authority for a later mutation.
type ValueResult struct {
	Action          string    `json:"action"`
	Mode            ValueMode `json:"mode"`
	RoomID          int16     `json:"room_id"`
	ItemID          string    `json:"item_id"`
	ItemName        string    `json:"item_name"`
	Occurrence      int       `json:"occurrence"`
	Value           int64     `json:"value"`
	Quote           int64     `json:"quote"`
	VisibleToPlayer bool      `json:"visible_to_player"`
	Response        string    `json:"response"`
}

// ValueQuote is kept as a semantic alias for callers that treat the result
// as an observation rather than a state-changing command result.
type ValueQuote = ValueResult

// QuoteValueByName validates the canonical player inventory boundary and
// computes the exact pawn/repair quote. It intentionally never returns a
// candidate State: command7.c:value is observational, and the Go command
// boundary must not clear PHIDDN or otherwise mutate the snapshot.
//
// The selector is deliberately narrower than legacy find_obj: it accepts an
// exact (case-insensitive) display name and a positive one-based occurrence,
// and searches only direct canonical Inventory roots in stored order. Prefix,
// key, equipped, legacy, and nested-object selection are not guessed.
func (s State) QuoteValueByName(actorID, name string, occurrence int) (ValueResult, error) {
	if err := s.Validate(); err != nil {
		return ValueResult{}, err
	}
	if actorID == "" {
		return ValueResult{}, fmt.Errorf("value actor required")
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Body.Type != 0 || actor.Body.Name == "" {
		return ValueResult{}, fmt.Errorf("online value actor absent")
	}
	// A non-nil ItemCollection is the explicit migration marker. Do not fall
	// back to Body.Inventory, even when that legacy slice happens to be empty.
	if actor.Items == nil || len(actor.Body.Inventory) != 0 {
		return ValueResult{}, fmt.Errorf("online player with migrated items required")
	}
	if occurrence < 1 {
		return ValueResult{}, fmt.Errorf("invalid value occurrence")
	}
	if name == "" || name != strings.TrimSpace(name) {
		return ValueResult{}, fmt.Errorf("value item name required")
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok {
		return ValueResult{}, fmt.Errorf("value room absent")
	}
	mode := ValueMode("")
	switch {
	case flag(room.Resource.Flags[:], RoomPawnFlag):
		mode = ValueModePawn
	case flag(room.Resource.Flags[:], RoomRepairFlag):
		mode = ValueModeRepair
	default:
		return ValueResult{}, fmt.Errorf("가치 평가를 할 수 없는 방입니다")
	}

	detectInvisible := flag(actor.Body.Flags[:], playerDetectInvisibleFlag)
	itemID, item, err := selectValueInventoryRoot(*actor.Items, name, occurrence, detectInvisible)
	if err != nil {
		return ValueResult{}, err
	}
	if item.Object.Value < 0 {
		return ValueResult{}, fmt.Errorf("invalid value item price")
	}
	value := int64(item.Object.Value)
	quote := value / 4
	if mode == ValueModePawn {
		quote = value / 2
		if quote > valuePawnMax {
			quote = valuePawnMax
		}
	}
	response := renderValueResponse(mode, item.Object.Name, quote)
	return ValueResult{
		Action:          "value",
		Mode:            mode,
		RoomID:          room.Resource.ID,
		ItemID:          itemID,
		ItemName:        item.Object.Name,
		Occurrence:      occurrence,
		Value:           value,
		Quote:           quote,
		VisibleToPlayer: true,
		Response:        response,
	}, nil
}

// QuoteValue is the short-form world API for callers that do not need the
// source-oriented ByName suffix. It intentionally delegates to the same
// implementation so selection and validation cannot drift.
func (s State) QuoteValue(actorID, name string, occurrence int) (ValueResult, error) {
	return s.QuoteValueByName(actorID, name, occurrence)
}

func selectValueInventoryRoot(c ItemCollection, name string, occurrence int, detectInvisible bool) (string, Item, error) {
	if err := c.Validate(); err != nil {
		return "", Item{}, err
	}
	found := 0
	for _, id := range c.Inventory {
		item, ok := c.Items[id]
		if !ok || id == "" || !strings.EqualFold(item.Object.Name, name) {
			continue
		}
		if flag(item.Object.Flags[:], objectInvisibleFlag) && !detectInvisible {
			continue
		}
		// ItemCollection keeps nested ownership in Contents IDs. A direct root
		// that owns a subtree is still a nested graph and is rejected rather
		// than valuing only its parent or silently traversing its children.
		if len(item.Contents) != 0 {
			continue
		}
		found++
		if found == occurrence {
			return id, item, nil
		}
	}
	return "", Item{}, fmt.Errorf("당신은 그런 물건을 갖고 있지 않습니다")
}

func renderValueResponse(mode ValueMode, itemName string, quote int64) string {
	if mode == ValueModePawn {
		return fmt.Sprintf("상점주인이 \"%s이라면 %d냥  정도는 드릴 수  있어요.\"라고 말합니다.\r\n", itemName, quote)
	}
	return fmt.Sprintf("상점주인이 \"%s%s 수리하는데 %d냥이 듭니다.\"라고 말합니다.\r\n", itemName, valueObjectParticle(itemName), quote)
}

// valueObjectParticle implements the only source formatting dependency used
// by value(): %j with selector 3 chooses 을/를 from the final Hangul syllable.
// Non-Hangul names follow the legacy under_han fallback and use 를.
func valueObjectParticle(name string) string {
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
