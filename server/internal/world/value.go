package world

import (
	"errors"
	"fmt"
	"strings"
)

// RoomRepairFlag is RREPAI from src/mtype.h. Value uses the pawn branch when
// both service flags are present, matching command7.c's if/else ordering.
const RoomRepairFlag = 7

const valuePawnMax = int64(100000)

const (
	ValueQuotedAction     = "value"
	ValueNotServiceAction = "not-service"
	ValueAskWhatAction    = "ask-what"
	ValueNotHoldingAction = "not-holding"

	ValueNotServiceResponse = "전당포에 가셔서 물건의 가치를 알아보세요."
	ValueAskWhatResponse    = "어떤 물건의 가치를 알고 싶으세요?"
	ValueNotHoldingResponse = "당신은 그런 물건을 갖고 있지 않습니다."
)

var errValueItemAbsent = errors.New("당신은 그런 물건을 갖고 있지 않습니다")

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
// computes the exact pawn/repair quote without returning a candidate State.
// Command receipts, C leftover prints, and F_CLR PHIDDN are ValueByName.
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
	quote, err := computeValueQuote(mode, item.Object.Value)
	if err != nil {
		return ValueResult{}, err
	}
	response := renderValueResponse(mode, item.Object.Name, quote)
	return ValueResult{
		Action:          ValueQuotedAction,
		Mode:            mode,
		RoomID:          room.Resource.ID,
		ItemID:          itemID,
		ItemName:        item.Object.Name,
		Occurrence:      occurrence,
		Value:           int64(item.Object.Value),
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

// valueServiceRoom is the C RPAWNS/RREPAI gate. A room with neither flag is a
// successful "전당포에 가셔서 물건의 가치를 알아보세요." receipt — not a command
// error. Actor inventory is not required for this print; unmigrated items
// are checked only after a name is present.
func (s State) valueServiceRoom(actorID string) (PlayerState, RoomState, ValueResult, bool, error) {
	if err := s.Validate(); err != nil {
		return PlayerState{}, RoomState{}, ValueResult{}, false, err
	}
	if actorID == "" {
		return PlayerState{}, RoomState{}, ValueResult{}, false, fmt.Errorf("value actor required")
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Body.Type != 0 || actor.Body.Name == "" {
		return PlayerState{}, RoomState{}, ValueResult{}, false, fmt.Errorf("online value actor absent")
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok {
		return PlayerState{}, RoomState{}, ValueResult{}, false, fmt.Errorf("value room absent")
	}
	if !flag(room.Resource.Flags[:], RoomPawnFlag) && !flag(room.Resource.Flags[:], RoomRepairFlag) {
		return actor, room, ValueResult{
			Action:   ValueNotServiceAction,
			RoomID:   room.Resource.ID,
			Response: ValueNotServiceResponse,
		}, true, nil
	}
	return actor, room, ValueResult{}, false, nil
}

func valueQuoteMode(room RoomState) ValueMode {
	if flag(room.Resource.Flags[:], RoomPawnFlag) {
		return ValueModePawn
	}
	return ValueModeRepair
}

func computeValueQuote(mode ValueMode, itemValue int32) (int64, error) {
	if itemValue < 0 {
		return 0, fmt.Errorf("invalid value item price")
	}
	value := int64(itemValue)
	if mode == ValueModePawn {
		quote := value / 2
		if quote > valuePawnMax {
			return valuePawnMax, nil
		}
		return quote, nil
	}
	return value / 4, nil
}

// ValueByName is the terminal 가치/가격 reducer. C order is preserved:
// non-RPAWNS and non-RREPAI, then missing name, then F_CLR PHIDDN, then
// find_obj on player first_obj. Those three prints are receipts; named
// leftover still reveals the actor because command7.c:312 clears hide
// before find_obj. A successful quote keeps inventory and gold unchanged
// but still F_CLR PHIDDN. Prefix/key matching stays closed.
func (s State) ValueByName(actorID, name string, occurrence int) (State, ValueResult, error) {
	actor, room, printed, handled, err := s.valueServiceRoom(actorID)
	if err != nil {
		return State{}, ValueResult{}, err
	}
	if handled {
		return s.clone(), printed, nil
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return s.clone(), ValueResult{
			Action:   ValueAskWhatAction,
			RoomID:   room.Resource.ID,
			Response: ValueAskWhatResponse,
		}, nil
	}
	if occurrence < 1 {
		return State{}, ValueResult{}, fmt.Errorf("invalid value occurrence")
	}
	if actor.Items == nil || len(actor.Body.Inventory) != 0 {
		return State{}, ValueResult{}, fmt.Errorf("online player with migrated items required")
	}
	// command7.c:312 F_CLR(ply_ptr, PHIDDN) after cmnd->num >= 2 and before
	// find_obj, so a later missing object still reveals the actor.
	revealed := s.clone()
	revealedPlayer := revealed.Players[actorID]
	revealedPlayer.Body.Flags[playerHiddenStateFlag/8] &^= 1 << (playerHiddenStateFlag % 8)
	revealed.Players[actorID] = revealedPlayer
	detectInvisible := flag(actor.Body.Flags[:], playerDetectInvisibleFlag)
	itemID, item, err := selectValueInventoryRoot(*actor.Items, name, occurrence, detectInvisible)
	if err != nil {
		if errors.Is(err, errValueItemAbsent) {
			return revealed, ValueResult{
				Action:     ValueNotHoldingAction,
				RoomID:     room.Resource.ID,
				Occurrence: occurrence,
				Response:   ValueNotHoldingResponse,
			}, nil
		}
		return State{}, ValueResult{}, err
	}
	mode := valueQuoteMode(room)
	quote, err := computeValueQuote(mode, item.Object.Value)
	if err != nil {
		return State{}, ValueResult{}, err
	}
	return revealed, ValueResult{
		Action:          ValueQuotedAction,
		Mode:            mode,
		RoomID:          room.Resource.ID,
		ItemID:          itemID,
		ItemName:        item.Object.Name,
		Occurrence:      occurrence,
		Value:           int64(item.Object.Value),
		Quote:           quote,
		VisibleToPlayer: true,
		Response:        renderValueResponse(mode, item.Object.Name, quote),
	}, nil
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
	return "", Item{}, errValueItemAbsent
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
