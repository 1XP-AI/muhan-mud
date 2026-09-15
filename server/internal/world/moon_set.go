package world

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"
)

// moon_set writes the current room onto an unbound inventory object.
// C hardcodes square room 1001 and treats object value 1001 as unbound.
const (
	moonSetSquareRoom          int16 = 1001
	moonSetUnboundValue        int32 = 1001
	MoonSetDescriptionMaxBytes       = 80
	MoonSetKeyMaxBytes               = 20
	MoonSetDescriptionSuffix         = "의 광경이 어른거립니다."

	MoonSetUsageResponse   = "\n무엇에 이곳의 위치를 기억시키실껀가요? <물건> [#] <이름> 기억\n"
	MoonSetMissingResponse = "그런 물건은 없어요."
	MoonSetSquareResponse  = "광장에선 기억을 시킬 수 없습니다."
	MoonSetBoundResponse   = "그것은 이미 어떤 장소를 기억하고 있습니다."
)

type MoonSetAction string

const (
	MoonSetUsage   MoonSetAction = "usage"
	MoonSetMissing MoonSetAction = "missing"
	MoonSetSquare  MoonSetAction = "square"
	MoonSetBound   MoonSetAction = "already_bound"
	MoonSetBind    MoonSetAction = "bind"
)

var (
	ErrMoonSetActorAbsent              = errors.New("online moon-set actor absent")
	ErrMoonSetCanonicalInventoryNeeded = errors.New("canonical player inventory required")
	ErrMoonSetItemNameRequired         = errors.New("moon-set item name required")
	ErrMoonSetInvalidOccurrence        = errors.New("invalid moon-set occurrence")
	ErrMoonSetRoomAbsent               = errors.New("moon-set actor room absent")
	ErrMoonSetRoomNameInvalid          = errors.New("invalid moon-set room name")
	ErrMoonSetDescriptionTooLong       = errors.New("moon-set description exceeds the byte limit")
	ErrMoonSetKeyTooLong               = errors.New("moon-set room key exceeds the byte limit")
	ErrMoonSetStaleProposal            = errors.New("stale or invalid moon-set proposal")
	ErrMoonSetInvalidProposal          = errors.New("invalid moon-set proposal")
)

// MoonSetEvent is one post-commit broadcast_rom line. C emits two of them.
type MoonSetEvent struct {
	RoomID         int16  `json:"room_id"`
	ActorID        string `json:"actor_id"`
	ActorName      string `json:"actor_name"`
	ExcludeActorID string `json:"exclude_actor_id"`
	Text           string `json:"text"`
}

// MoonSetResult is the durable receipt projection for command8.c:moon_set.
type MoonSetResult struct {
	Action      MoonSetAction  `json:"action"`
	ActorID     string         `json:"actor_id"`
	RoomID      int16          `json:"room_id,omitempty"`
	RoomName    string         `json:"room_name,omitempty"`
	ItemID      string         `json:"item_id,omitempty"`
	ItemName    string         `json:"item_name,omitempty"`
	Occurrence  int            `json:"occurrence,omitempty"`
	Description string         `json:"description,omitempty"`
	Response    string         `json:"response"`
	Changed     bool           `json:"changed"`
	Events      []MoonSetEvent `json:"events,omitempty"`
}

// MoonSetProposal binds one moon_set transition to one snapshot.
type MoonSetProposal struct {
	Action      MoonSetAction
	ActorID     string
	RoomID      int16
	RoomName    string
	ItemID      string
	ItemName    string
	Occurrence  int
	Description string
	Response    string
	Changed     bool
	Events      []MoonSetEvent

	expectedBody      LegacyMonster
	expectedOnline    bool
	expectedInventory []string
	expectedItems     ItemCollection
	expectedItem      Item
}

func MoonSetBindResponse() string {
	return "\n당신은 초인의 돌에 이곳의 장소를 기억시킵니다.\n초인의 돌이 갑자기 밝은 빛을 내다 다시 투명해 집니다."
}

func MoonSetBindEvents(roomID int16, actorID, actorName string) []MoonSetEvent {
	return []MoonSetEvent{
		{
			RoomID: roomID, ActorID: actorID, ActorName: actorName, ExcludeActorID: actorID,
			Text: fmt.Sprintf("\n%s이 초인의 돌에 이곳의 장소를 기억시킵니다.", actorName),
		},
		{
			RoomID: roomID, ActorID: actorID, ActorName: actorName, ExcludeActorID: actorID,
			Text: fmt.Sprintf("\n%s초인의 돌이 갑자기 밝은 빛을 내다 다시 투명해 집니다.", actorName),
		},
	}
}

func moonSetDescription(roomName string) (string, error) {
	if roomName == "" || !utf8.ValidString(roomName) {
		return "", ErrMoonSetRoomNameInvalid
	}
	for _, r := range roomName {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return "", ErrMoonSetRoomNameInvalid
		}
	}
	if len(roomName) > MoonSetKeyMaxBytes {
		return "", ErrMoonSetKeyTooLong
	}
	text := roomName + MoonSetDescriptionSuffix
	if len(text) > MoonSetDescriptionMaxBytes {
		return "", ErrMoonSetDescriptionTooLong
	}
	return text, nil
}

func moonSetActor(s State, actorID string) (PlayerState, RoomState, error) {
	if err := s.Validate(); err != nil {
		return PlayerState{}, RoomState{}, err
	}
	actor, ok := s.Players[actorID]
	if actorID == "" || !ok || !actor.Online || actor.Body.Type != 0 || actor.Body.Name == "" {
		return PlayerState{}, RoomState{}, ErrMoonSetActorAbsent
	}
	if actor.Items == nil || len(actor.Body.Inventory) != 0 {
		return PlayerState{}, RoomState{}, ErrMoonSetCanonicalInventoryNeeded
	}
	if err := actor.Items.Validate(); err != nil {
		return PlayerState{}, RoomState{}, err
	}
	if !validMoonSetActorName(actor.Body.Name) {
		return PlayerState{}, RoomState{}, fmt.Errorf("moon-set actor name is unsafe")
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok || room.Resource.ID != actor.Body.RoomID || !containsString(room.PlayerIDs, actorID) {
		return PlayerState{}, RoomState{}, ErrMoonSetRoomAbsent
	}
	return actor, room, nil
}

func validMoonSetActorName(name string) bool {
	if name == "" || !utf8.ValidString(name) {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

func validateMoonSetSelector(name string) error {
	if name == "" || !utf8.ValidString(name) || strings.TrimSpace(name) != name {
		return ErrMoonSetItemNameRequired
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' || (unicode.IsSpace(r) && r != ' ') {
			return ErrMoonSetItemNameRequired
		}
	}
	return nil
}

func validMoonSetObjectText(value string, allowEmpty bool) bool {
	if value == "" {
		return allowEmpty
	}
	if !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

func validateMoonSetCollection(c ItemCollection) error {
	if err := c.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrMoonSetCanonicalInventoryNeeded, err)
	}
	for id, item := range c.Items {
		if id == "" || !validMoonSetObjectText(item.Object.Name, false) {
			return fmt.Errorf("%w: unresolved item identity %q", ErrMoonSetCanonicalInventoryNeeded, id)
		}
		for _, key := range item.Object.Keys {
			if !validMoonSetObjectText(key, true) {
				return fmt.Errorf("%w: unsafe item key for %q", ErrMoonSetCanonicalInventoryNeeded, id)
			}
		}
	}
	return nil
}

func moonSetObjectPrefixMatch(object LegacyObject, selector string) bool {
	if equalFoldPrefix(object.Name, selector) {
		return true
	}
	for _, key := range object.Keys {
		if key != "" && equalFoldPrefix(key, selector) {
			return true
		}
	}
	return false
}

func selectMoonSetRoot(c ItemCollection, name string, occurrence int) (string, Item, bool, error) {
	return selectMoonSetRootWithVisibility(c, name, occurrence, false)
}

func selectMoonSetRootWithVisibility(c ItemCollection, name string, occurrence int, detectInvisible bool) (string, Item, bool, error) {
	if occurrence < 1 {
		return "", Item{}, false, ErrMoonSetInvalidOccurrence
	}
	if err := validateMoonSetSelector(name); err != nil {
		return "", Item{}, false, err
	}
	if err := validateMoonSetCollection(c); err != nil {
		return "", Item{}, false, err
	}
	found := 0
	for _, id := range c.Inventory {
		item := c.Items[id]
		if flag(item.Object.Flags[:], objectInvisibleFlag) && !detectInvisible {
			continue
		}
		if !moonSetObjectPrefixMatch(item.Object, name) {
			continue
		}
		found++
		if found == occurrence {
			return id, item, true, nil
		}
	}
	return "", Item{}, false, nil
}

func moonSetResult(p MoonSetProposal) MoonSetResult {
	return MoonSetResult{
		Action: p.Action, ActorID: p.ActorID, RoomID: p.RoomID, RoomName: p.RoomName,
		ItemID: p.ItemID, ItemName: p.ItemName, Occurrence: p.Occurrence,
		Description: p.Description, Response: p.Response, Changed: p.Changed,
		Events: append([]MoonSetEvent(nil), p.Events...),
	}
}

// PlanMoonSet is command8.c:moon_set. itemName empty is the original
// cmnd->num<2 usage prompt. Matching follows EQUAL's case-insensitive Go
// prefix contract across the display name and all three keys, on visible
// direct inventory roots in authoritative order. Nested/ready lookup stays
// fail-closed.
func (s State) PlanMoonSet(actorID, itemName string, occurrence int) (MoonSetProposal, error) {
	actor, room, err := moonSetActor(s, actorID)
	if err != nil {
		return MoonSetProposal{}, err
	}
	if itemName == "" {
		return MoonSetProposal{
			Action: MoonSetUsage, ActorID: actorID, RoomID: actor.Body.RoomID, RoomName: room.Resource.Name,
			Response: MoonSetUsageResponse, Changed: false,
			expectedBody: actor.Body, expectedOnline: actor.Online,
			expectedInventory: append([]string(nil), actor.Items.Inventory...),
			expectedItems:     actor.Items.clone(),
		}, nil
	}
	if occurrence < 1 {
		return MoonSetProposal{}, ErrMoonSetInvalidOccurrence
	}
	itemID, item, found, err := selectMoonSetRootWithVisibility(*actor.Items, itemName, occurrence, flag(actor.Body.Flags[:], playerDetectInvisibleFlag))
	if err != nil {
		return MoonSetProposal{}, err
	}
	if !found {
		return MoonSetProposal{
			Action: MoonSetMissing, ActorID: actorID, RoomID: actor.Body.RoomID, RoomName: room.Resource.Name,
			ItemName: itemName, Occurrence: occurrence, Response: MoonSetMissingResponse, Changed: false,
			expectedBody: actor.Body, expectedOnline: actor.Online,
			expectedInventory: append([]string(nil), actor.Items.Inventory...),
			expectedItems:     actor.Items.clone(),
		}, nil
	}
	if actor.Body.RoomID == moonSetSquareRoom {
		return MoonSetProposal{
			Action: MoonSetSquare, ActorID: actorID, RoomID: actor.Body.RoomID, RoomName: room.Resource.Name,
			ItemID: itemID, ItemName: item.Object.Name, Occurrence: occurrence,
			Response: MoonSetSquareResponse, Changed: false,
			expectedBody: actor.Body, expectedOnline: actor.Online,
			expectedInventory: append([]string(nil), actor.Items.Inventory...),
			expectedItems:     actor.Items.clone(),
			expectedItem:      item,
		}, nil
	}
	if item.Object.Value != moonSetUnboundValue {
		return MoonSetProposal{
			Action: MoonSetBound, ActorID: actorID, RoomID: actor.Body.RoomID, RoomName: room.Resource.Name,
			ItemID: itemID, ItemName: item.Object.Name, Occurrence: occurrence,
			Response: MoonSetBoundResponse, Changed: false,
			expectedBody: actor.Body, expectedOnline: actor.Online,
			expectedInventory: append([]string(nil), actor.Items.Inventory...),
			expectedItems:     actor.Items.clone(),
			expectedItem:      item,
		}, nil
	}
	description, err := moonSetDescription(room.Resource.Name)
	if err != nil {
		return MoonSetProposal{}, err
	}
	events := MoonSetBindEvents(actor.Body.RoomID, actorID, actor.Body.Name)
	return MoonSetProposal{
		Action: MoonSetBind, ActorID: actorID, RoomID: actor.Body.RoomID, RoomName: room.Resource.Name,
		ItemID: itemID, ItemName: item.Object.Name, Occurrence: occurrence, Description: description,
		Response: MoonSetBindResponse(), Changed: true, Events: events,
		expectedBody: actor.Body, expectedOnline: actor.Online,
		expectedInventory: append([]string(nil), actor.Items.Inventory...),
		expectedItems:     actor.Items.clone(),
		expectedItem:      item,
	}, nil
}

// ApplyMoonSet atomically writes description, key[1], and value on the
// selected inventory root. Unchanged gates persist as receipts without Apply.
func (s State) ApplyMoonSet(proposal MoonSetProposal) (State, MoonSetResult, error) {
	actor, room, err := moonSetActor(s, proposal.ActorID)
	if err != nil {
		return State{}, MoonSetResult{}, err
	}
	if !proposal.Changed || proposal.Action != MoonSetBind || proposal.ItemID == "" ||
		proposal.RoomID != actor.Body.RoomID || proposal.RoomName != room.Resource.Name ||
		proposal.Occurrence < 1 || proposal.ItemName == "" || proposal.Response != MoonSetBindResponse() ||
		len(proposal.Events) != 2 || actor.Online != proposal.expectedOnline ||
		!reflect.DeepEqual(actor.Body, proposal.expectedBody) ||
		!reflect.DeepEqual(actor.Items.Inventory, proposal.expectedInventory) ||
		!reflect.DeepEqual(*actor.Items, proposal.expectedItems) {
		return State{}, MoonSetResult{}, ErrMoonSetStaleProposal
	}
	description, err := moonSetDescription(proposal.RoomName)
	if err != nil || description != proposal.Description {
		return State{}, MoonSetResult{}, ErrMoonSetStaleProposal
	}
	current, ok := actor.Items.Items[proposal.ItemID]
	if !ok || !reflect.DeepEqual(current, proposal.expectedItem) || current.Object.Name != proposal.ItemName ||
		current.Object.Value != moonSetUnboundValue || actor.Body.RoomID == moonSetSquareRoom {
		return State{}, MoonSetResult{}, ErrMoonSetStaleProposal
	}
	wantEvents := MoonSetBindEvents(proposal.RoomID, proposal.ActorID, actor.Body.Name)
	if !reflect.DeepEqual(proposal.Events, wantEvents) {
		return State{}, MoonSetResult{}, ErrMoonSetStaleProposal
	}

	next := s.clone()
	nextActor := next.Players[proposal.ActorID]
	item := nextActor.Items.Items[proposal.ItemID]
	item.Object.Description = proposal.Description
	item.Object.Keys[1] = proposal.RoomName
	item.Object.Value = int32(proposal.RoomID)
	nextActor.Items.Items[proposal.ItemID] = item
	next.Players[proposal.ActorID] = nextActor
	if err := next.Validate(); err != nil {
		return State{}, MoonSetResult{}, err
	}
	return next, moonSetResult(proposal), nil
}
