package world

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ItemRenameNameMaxBytes is the client-visible byte limit for a newly
// assigned object name.  The legacy object field is an 80-byte fixed field;
// the canonical model stores the validated UTF-8 string rather than copying
// into a fixed C buffer.
const ItemRenameNameMaxBytes = 80

// MaxItemRenameNameBytes is the descriptive spelling used by command
// adapters and tests.
const MaxItemRenameNameBytes = ItemRenameNameMaxBytes

// OCNAME and ONAMED are the original object flag bits.  They are kept here,
// at the canonical item-rename boundary, instead of exposing a generic flag
// mutation API to callers.
const (
	ItemRenameChangeNameFlag uint = 43 // OCNAME
	ItemRenameNamedFlag      uint = 47 // ONAMED
)

// Compatibility spellings for source-oriented callers.  The values are the
// same canonical flag bits and are deliberately not a second flag domain.
const (
	ObjectChangeNameFlag = ItemRenameChangeNameFlag
	ObjectNamedFlag      = ItemRenameNamedFlag
	objectChangeNameFlag = ItemRenameChangeNameFlag
	objectNamedFlag      = ItemRenameNamedFlag
)

var (
	ErrInvalidItemRenameName              = errors.New("invalid item rename name")
	ErrItemRenameNameTooLong              = errors.New("item rename name exceeds the byte limit")
	ErrItemRenameActorAbsent              = errors.New("online item rename actor absent")
	ErrItemRenameCanonicalInventoryNeeded = errors.New("canonical player inventory required")
	ErrItemRenameInvalidOccurrence        = errors.New("invalid item rename occurrence")
	ErrItemRenameItemNameRequired         = errors.New("item rename item name required")
	ErrItemRenameItemMissing              = errors.New("item rename target not found")
	ErrItemRenameForbidden                = errors.New("item cannot be renamed")
	ErrItemRenameStaleProposal            = errors.New("stale or invalid item rename proposal")

	// Short aliases retained for callers that use the source noun rather than
	// the command's full item-rename spelling.
	ErrItemRenameNotAllowed      = ErrItemRenameForbidden
	ErrItemRenameMissingItem     = ErrItemRenameItemMissing
	ErrItemRenameInventoryNeeded = ErrItemRenameCanonicalInventoryNeeded
)

// ItemRenameProposal is a state-bound candidate for command8.c:chg_name.
// Item IDs are resolved from the canonical direct inventory during planning;
// clients must never be allowed to submit one as authority.  The unexported
// fields make a proposal produced from one snapshot impossible to reuse after
// the target player/item has changed.
type ItemRenameProposal struct {
	ActorID    string
	RoomID     int16
	ItemID     string
	ItemName   string
	OldName    string
	NewName    string
	Occurrence int
	Response   string
	Broadcast  bool

	expectedBody      LegacyMonster
	expectedOnline    bool
	expectedInventory []string
	expectedItems     ItemCollection
	expectedItem      Item
}

// ItemRenameResult is the deterministic actor response persisted in the
// command receipt.  Event is a post-commit room projection; the session layer
// stores it with the response but does not publish it during reduction.
type ItemRenameResult struct {
	Action     string           `json:"action"`
	ActorID    string           `json:"actor_id"`
	RoomID     int16            `json:"room_id"`
	ItemID     string           `json:"item_id"`
	ItemName   string           `json:"item_name"`
	OldName    string           `json:"old_name"`
	NewName    string           `json:"new_name"`
	Occurrence int              `json:"occurrence"`
	Response   string           `json:"response"`
	Broadcast  bool             `json:"broadcast"`
	Event      *ItemRenameEvent `json:"event,omitempty"`
}

// ItemRenameEvent contains fixed post-commit room fan-out data.  It does not
// include the new item name, because the original C broadcast only announces
// that the actor named an item and the name itself is client-controlled text.
type ItemRenameEvent struct {
	RoomID         int16  `json:"room_id"`
	ActorID        string `json:"actor_id"`
	ActorName      string `json:"actor_name"`
	ExcludeActorID string `json:"exclude_actor_id"`
	Text           string `json:"text"`
}

// ValidateItemRenameName enforces the safe subset of the original fixed
// object-name field.  Internal ASCII spaces are retained for source
// compatibility (chg_name copied the complete free-form suffix), while
// leading/trailing and non-ASCII whitespace are rejected to avoid ambiguous
// selectors and terminal line injection.
func ValidateItemRenameName(name string) error {
	if name == "" || !utf8.ValidString(name) {
		return ErrInvalidItemRenameName
	}
	if len(name) > ItemRenameNameMaxBytes {
		return ErrItemRenameNameTooLong
	}
	if strings.TrimSpace(name) != name {
		return ErrInvalidItemRenameName
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return ErrInvalidItemRenameName
		}
		if unicode.IsSpace(r) && r != ' ' {
			return ErrInvalidItemRenameName
		}
	}
	return nil
}

// ValidateItemRenameText is a compatibility alias for callers that name the
// command payload rather than the resulting object field.
func ValidateItemRenameText(name string) error { return ValidateItemRenameName(name) }

func itemRenameActor(s State, actorID string) (PlayerState, error) {
	if err := s.Validate(); err != nil {
		return PlayerState{}, err
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Body.Type != 0 || actor.Body.Name == "" {
		return PlayerState{}, ErrItemRenameActorAbsent
	}
	// A non-nil ItemCollection is the explicit migration marker.  Never
	// combine it with the old linked-list body inventory or guess an identity
	// from that legacy shape.
	if actor.Items == nil || len(actor.Body.Inventory) != 0 {
		return PlayerState{}, ErrItemRenameCanonicalInventoryNeeded
	}
	if err := actor.Items.Validate(); err != nil {
		return PlayerState{}, err
	}
	if !validItemRenameActorName(actor.Body.Name) {
		return PlayerState{}, fmt.Errorf("item rename actor name is unsafe")
	}
	return actor, nil
}

func validItemRenameActorName(name string) bool {
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

func validateItemRenameSelector(name string) error {
	if name == "" || !utf8.ValidString(name) || strings.TrimSpace(name) != name {
		return ErrItemRenameItemNameRequired
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' || (unicode.IsSpace(r) && r != ' ') {
			return ErrInvalidItemRenameName
		}
	}
	return nil
}

func selectItemRenameRoot(c ItemCollection, name string, occurrence int) (string, Item, error) {
	if occurrence < 1 {
		return "", Item{}, ErrItemRenameInvalidOccurrence
	}
	if err := validateItemRenameSelector(name); err != nil {
		return "", Item{}, err
	}
	if err := c.Validate(); err != nil {
		return "", Item{}, err
	}
	found := 0
	for _, id := range c.Inventory {
		item, ok := c.Items[id]
		if !ok || id == "" || !strings.EqualFold(item.Object.Name, name) {
			continue
		}
		found++
		if found != occurrence {
			continue
		}
		if !flag(item.Object.Flags[:], ItemRenameChangeNameFlag) {
			return "", Item{}, ErrItemRenameForbidden
		}
		return id, item, nil
	}
	return "", Item{}, ErrItemRenameItemMissing
}

func itemRenameResponse() string { return "\n이름 명명 되었습니다.\r\n" }

// PlanItemRename resolves one exact, case-insensitive direct inventory root
// and captures the preconditions required for an atomic ApplyItemRename.
// Nested children, equipped roots, legacy linked-list objects, and an
// un-migrated player are never searched or guessed.
func (s State) PlanItemRename(actorID, itemName string, occurrence int, newName string) (ItemRenameProposal, error) {
	actor, err := itemRenameActor(s, actorID)
	if err != nil {
		return ItemRenameProposal{}, err
	}
	if err := ValidateItemRenameName(newName); err != nil {
		return ItemRenameProposal{}, err
	}
	itemID, item, err := selectItemRenameRoot(*actor.Items, itemName, occurrence)
	if err != nil {
		return ItemRenameProposal{}, err
	}
	return ItemRenameProposal{
		ActorID:           actorID,
		RoomID:            actor.Body.RoomID,
		ItemID:            itemID,
		ItemName:          item.Object.Name,
		OldName:           item.Object.Name,
		NewName:           newName,
		Occurrence:        occurrence,
		Response:          itemRenameResponse(),
		Broadcast:         true,
		expectedBody:      actor.Body,
		expectedOnline:    actor.Online,
		expectedInventory: append([]string(nil), actor.Items.Inventory...),
		expectedItems:     actor.Items.clone(),
		expectedItem:      item,
	}, nil
}

// PlanRenameItem is the concise verb-first alias used by command adapters.
func (s State) PlanRenameItem(actorID, itemName string, occurrence int, newName string) (ItemRenameProposal, error) {
	return s.PlanItemRename(actorID, itemName, occurrence, newName)
}

// ApplyItemRename atomically applies a proposal to a cloned canonical state.
// It rechecks actor, inventory order, target identity, old display name, and
// OCNAME so a proposal planned against an earlier snapshot cannot overwrite a
// later mutation.
func (s State) ApplyItemRename(proposal ItemRenameProposal) (State, ItemRenameResult, error) {
	actor, err := itemRenameActor(s, proposal.ActorID)
	if err != nil {
		return State{}, ItemRenameResult{}, err
	}
	if proposal.ActorID == "" || proposal.ItemID == "" || proposal.RoomID != actor.Body.RoomID ||
		proposal.Occurrence < 1 || proposal.ItemName == "" || proposal.OldName == "" ||
		proposal.OldName != proposal.ItemName || proposal.Response != itemRenameResponse() || !proposal.Broadcast ||
		actor.Online != proposal.expectedOnline || !reflect.DeepEqual(actor.Body, proposal.expectedBody) ||
		!reflect.DeepEqual(actor.Items.Inventory, proposal.expectedInventory) ||
		!reflect.DeepEqual(*actor.Items, proposal.expectedItems) {
		return State{}, ItemRenameResult{}, ErrItemRenameStaleProposal
	}
	if err := ValidateItemRenameName(proposal.NewName); err != nil {
		return State{}, ItemRenameResult{}, err
	}
	current, ok := actor.Items.Items[proposal.ItemID]
	if !ok || !reflect.DeepEqual(current, proposal.expectedItem) || current.Object.Name != proposal.OldName ||
		!flag(current.Object.Flags[:], ItemRenameChangeNameFlag) {
		return State{}, ItemRenameResult{}, ErrItemRenameStaleProposal
	}

	next := s.clone()
	nextActor := next.Players[proposal.ActorID]
	item := nextActor.Items.Items[proposal.ItemID]
	item.Object.Name = proposal.NewName
	setObjectFlag(&item.Object.Flags, ItemRenameChangeNameFlag, false)
	setObjectFlag(&item.Object.Flags, ItemRenameNamedFlag, true)
	nextActor.Items.Items[proposal.ItemID] = item
	next.Players[proposal.ActorID] = nextActor
	if err := next.Validate(); err != nil {
		return State{}, ItemRenameResult{}, err
	}

	event := &ItemRenameEvent{
		RoomID: proposal.RoomID, ActorID: proposal.ActorID,
		ActorName: actor.Body.Name, ExcludeActorID: proposal.ActorID,
		Text: fmt.Sprintf("\n%s이 자신의 물건에 이름을 명명합니다.\r\n", actor.Body.Name),
	}
	result := ItemRenameResult{
		Action: "item_rename", ActorID: proposal.ActorID, RoomID: proposal.RoomID,
		ItemID: proposal.ItemID, ItemName: proposal.NewName, OldName: proposal.OldName,
		NewName: proposal.NewName, Occurrence: proposal.Occurrence,
		Response: proposal.Response, Broadcast: true, Event: event,
	}
	return next, result, nil
}

// ApplyRenameItem is the concise verb-first alias used by command adapters.
func (s State) ApplyRenameItem(proposal ItemRenameProposal) (State, ItemRenameResult, error) {
	return s.ApplyItemRename(proposal)
}

// RenameItemByName is the one-call state boundary for non-session callers.
// Session commands should use PlanItemRename/ApplyItemRename explicitly so
// ExecuteGame retains the candidate boundary in its receipt reducer.
func (s State) RenameItemByName(actorID, itemName string, occurrence int, newName string) (State, ItemRenameResult, error) {
	proposal, err := s.PlanItemRename(actorID, itemName, occurrence, newName)
	if err != nil {
		return State{}, ItemRenameResult{}, err
	}
	return s.ApplyItemRename(proposal)
}

// RenameItem is the short API spelling used by gameplay callers.
func (s State) RenameItem(actorID, itemName string, occurrence int, newName string) (State, ItemRenameResult, error) {
	return s.RenameItemByName(actorID, itemName, occurrence, newName)
}
