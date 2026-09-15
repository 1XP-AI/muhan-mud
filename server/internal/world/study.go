package world

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	studyScrollType       = 7  // SCROLL
	studyGoodOnlyFlag     = 12 // OGOODO
	studyEvilOnlyFlag     = 13 // OEVILO
	studyClassSelectFlag  = 31 // OCLSEL
	studyCaretakerClass   = 10 // CARETAKER
	studyMaxSpellPower    = 56 // spell bits 0..55
	studyPlayerBlindFlag  = 42 // PBLIND
	studyPlayerHiddenFlag = 1  // PHIDDN
)

const (
	StudyScrollType      = studyScrollType
	StudyMaxSpellPower   = studyMaxSpellPower
	StudyGoodOnlyFlag    = studyGoodOnlyFlag
	StudyEvilOnlyFlag    = studyEvilOnlyFlag
	StudyClassSelectFlag = studyClassSelectFlag
)

var (
	ErrStudyBlind            = errors.New("study is unavailable while blind")
	ErrStudyMissingItem      = errors.New("study scroll not found")
	ErrStudyNotScroll        = errors.New("study target is not a scroll")
	ErrStudyLevel            = errors.New("study scroll level is too high")
	ErrStudyAlignment        = errors.New("study scroll rejects actor alignment")
	ErrStudyClass            = errors.New("study scroll rejects actor class")
	ErrStudySpellUnavailable = errors.New("study spell catalog entry unavailable")
)

// StudyLocation identifies the canonical direct root searched by study. A
// nested item is never an independent selector target; equipped roots are
// admitted only through the legacy Ready fallback after Inventory search.
type StudyLocation string

const (
	StudyInventoryRoot StudyLocation = "inventory"
	StudyReadySlot     StudyLocation = "ready"

	// Short aliases keep callers that use the legacy vocabulary readable.
	StudyInventory StudyLocation = StudyInventoryRoot
	StudyReady     StudyLocation = StudyReadySlot
)

// StudyProposal is a snapshot-bound candidate for magic1.c:study. The item
// ID is resolved from a canonical Inventory or Ready root during planning; it
// is never accepted from a client. No random source or ID allocator is involved.
type StudyProposal struct {
	Action      string
	ActorID     string
	RoomID      int16
	ItemID      string
	ItemName    string
	Selector    string
	Occurrence  int
	Location    StudyLocation
	ReadySlot   int
	SpellIndex  int
	SpellName   string
	Learned     bool
	MoveToRoom  bool
	ClearHidden bool
	Response    string
	RoomText    string

	expectedActor     PlayerState
	expectedRoomItems ItemCollection
	expectedItem      Item
}

// StudyResult is the deterministic receipt response. Event is included only
// for successful learning, when the source broadcasts the study action to the
// actor's room. Alignment rejection moves the scroll to the room but has no
// room broadcast in the original command.
type StudyResult struct {
	Action      string      `json:"action"`
	Response    string      `json:"response"`
	Broadcast   bool        `json:"broadcast"`
	Changed     bool        `json:"changed"`
	Learned     bool        `json:"learned"`
	MovedToRoom bool        `json:"moved_to_room"`
	RoomID      int16       `json:"room_id"`
	ActorID     string      `json:"actor_id"`
	ActorName   string      `json:"actor_name"`
	ItemID      string      `json:"item_id"`
	ItemName    string      `json:"item_name"`
	Occurrence  int         `json:"occurrence"`
	SpellIndex  int         `json:"spell_index"`
	SpellName   string      `json:"spell_name"`
	EventText   string      `json:"event_text,omitempty"`
	Event       *StudyEvent `json:"event,omitempty"`
}

// StudyEvent is the committed room projection for successful learning. The
// actor receives Result.Response directly, so the room fan-out excludes it.
type StudyEvent struct {
	RoomID         int16  `json:"room_id"`
	ActorID        string `json:"actor_id"`
	ActorName      string `json:"actor_name"`
	ExcludeActorID string `json:"exclude_actor_id"`
	ItemID         string `json:"item_id"`
	ItemName       string `json:"item_name"`
	SpellName      string `json:"spell_name"`
	Text           string `json:"text"`
}

func validStudyName(name string) bool {
	if name == "" || !utf8.ValidString(name) || strings.TrimSpace(name) != name {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == '\r' || r == '\n' || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

func studySpellName(magicPower byte) (string, int, error) {
	if magicPower < 1 || int(magicPower) > studyMaxSpellPower {
		return "", 0, fmt.Errorf("%w: magicpower=%d", ErrStudySpellUnavailable, magicPower)
	}
	index := int(magicPower) - 1
	// legacyInfoSpellNames is the single canonical projection of src/global.c
	// spllist. A missing/empty entry is an unresolved migration boundary, not
	// permission to set an arbitrary spell bit.
	if index >= len(legacyInfoSpellNames) || legacyInfoSpellNames[index] == "" || !utf8.ValidString(legacyInfoSpellNames[index]) {
		return "", 0, fmt.Errorf("%w: spell index=%d", ErrStudySpellUnavailable, index)
	}
	return legacyInfoSpellNames[index], index, nil
}

func studyObjectVisible(actor LegacyMonster, object LegacyObject) bool {
	return !flag(object.Flags[:], objectInvisibleFlag) || flag(actor.Flags[:], playerDetectInvisibleFlag)
}

// selectStudyRoot ports object.c:find_obj and magic1.c:study's Ready loop.
// Each canonical root counts once when its display name or any key matches
// the case-insensitive EQUAL-style prefix; hidden objects count only for a
// player with PDINVI. The two C loops own separate counters, so Ready fallback
// starts a fresh positive occurrence count after Inventory fails to resolve.
func selectStudyRoot(actor PlayerState, name string, occurrence int) (string, Item, StudyLocation, int, error) {
	if occurrence < 1 {
		return "", Item{}, "", -1, fmt.Errorf("invalid study occurrence")
	}
	if !validStudyName(name) {
		return "", Item{}, "", -1, fmt.Errorf("invalid study item name")
	}
	if actor.Items == nil || len(actor.Body.Inventory) != 0 {
		return "", Item{}, "", -1, fmt.Errorf("canonical study player inventory required")
	}
	if err := actor.Items.Validate(); err != nil {
		return "", Item{}, "", -1, err
	}
	found := 0
	for _, id := range actor.Items.Inventory {
		item, ok := actor.Items.Items[id]
		if !ok || id == "" {
			return "", Item{}, "", -1, fmt.Errorf("canonical study inventory root absent")
		}
		if !equalInventorySelector(item.Object, name) || !studyObjectVisible(actor.Body, item.Object) {
			continue
		}
		found++
		if found == occurrence {
			return id, item, StudyInventoryRoot, -1, nil
		}
	}
	// find_obj owns its own match counter. magic1.c therefore resets the
	// occurrence for this Ready fallback after the direct Inventory lookup did
	// not resolve the requested occurrence; Ready slots retain slot order.
	found = 0
	for slot, id := range actor.Items.Ready {
		if id == "" {
			continue
		}
		item, ok := actor.Items.Items[id]
		if !ok {
			return "", Item{}, "", -1, fmt.Errorf("canonical study ready root absent")
		}
		if !equalInventorySelector(item.Object, name) || !studyObjectVisible(actor.Body, item.Object) {
			continue
		}
		found++
		if found == occurrence {
			return id, item, StudyReadySlot, slot, nil
		}
	}
	return "", Item{}, "", -1, fmt.Errorf("%w: %q", ErrStudyMissingItem, name)
}

// selectStudyInventoryRoot preserves the pre-location helper for package-local
// callers. It intentionally remains Inventory-only; PlanStudy uses
// selectStudyRoot so the Ready fallback can carry its slot through Apply.
func selectStudyInventoryRoot(actor PlayerState, name string, occurrence int) (string, Item, error) {
	id, item, location, _, err := selectStudyRoot(actor, name, occurrence)
	if err == nil && location != StudyInventoryRoot {
		return "", Item{}, fmt.Errorf("%w: %q", ErrStudyMissingItem, name)
	}
	return id, item, err
}

func studyActor(s State, actorID string) (PlayerState, RoomState, error) {
	if err := s.Validate(); err != nil {
		return PlayerState{}, RoomState{}, err
	}
	actor, ok := s.Players[actorID]
	if actorID == "" || !ok || !actor.Online || actor.Body.Type != 0 || !validStudyName(actor.Body.Name) {
		return PlayerState{}, RoomState{}, fmt.Errorf("online study actor absent")
	}
	if actor.Body.Class > maxLegacyClass {
		return PlayerState{}, RoomState{}, fmt.Errorf("study actor class outside legacy table")
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok || room.Items == nil {
		return PlayerState{}, RoomState{}, fmt.Errorf("canonical study room inventory required")
	}
	if err := room.Items.Validate(); err != nil {
		return PlayerState{}, RoomState{}, err
	}
	if actor.Items == nil || len(actor.Body.Inventory) != 0 {
		return PlayerState{}, RoomState{}, fmt.Errorf("canonical study player inventory required")
	}
	if err := actor.Items.Validate(); err != nil {
		return PlayerState{}, RoomState{}, err
	}
	return actor, room, nil
}

func studyClassAllowed(actor LegacyMonster, item LegacyObject) bool {
	if !flag(item.Flags[:], studyClassSelectFlag) {
		return true
	}
	if actor.Class >= studyCaretakerClass {
		return true
	}
	return flag(item.Flags[:], uint(studyClassSelectFlag)+uint(actor.Class))
}

func studyAlignmentRejected(actor LegacyMonster, item LegacyObject) bool {
	return (flag(item.Flags[:], studyGoodOnlyFlag) && actor.Alignment < -100) ||
		(flag(item.Flags[:], studyEvilOnlyFlag) && actor.Alignment > 100)
}

func studyResponse(itemName, spellName string, learned, moved bool) string {
	if moved {
		return fmt.Sprintf("\n연마를 끝마치자 %s의 형체가 화염에 휩싸이며 어디론가 사라져\n 버렸습니다.\r\n", itemName)
	}
	if !learned {
		return ""
	}
	return fmt.Sprintf("당신은 %s의 내용을 알아내고 연마하기 시작합니다.\r\n연마를 해 나감에 따라 몸안에서 이상한 기운이 퍼져 나가는\r\n것이 느껴집니다.\r\n이야야~~~~~얍 그 기운이 안정되면서 완전히 당신의 것이\r\n되었습니다.\r\n\r\n연마를 마치자 %s의 형체에 화염이 휩싸이며 어디론가 사라져 버렸습니다.\r\n", spellName, itemName)
}

func studyEventText(actorName, itemName string) string {
	return fmt.Sprintf("\n%s님이 %s의 내용을 읽고 연마합니다.\r\n", actorName, itemName)
}

func cloneStudyActor(actor PlayerState) PlayerState {
	actor.Body.Inventory = cloneObjects(actor.Body.Inventory)
	actor.Aliases = clonePlayerAliases(actor.Aliases)
	if actor.Items != nil {
		items := actor.Items.clone()
		actor.Items = &items
	}
	actor.PlayerEnemies = append([]string(nil), actor.PlayerEnemies...)
	actor.FollowerIDs = append([]string(nil), actor.FollowerIDs...)
	if actor.NPCFollowerIDs != nil {
		actor.NPCFollowerIDs = append([]string(nil), actor.NPCFollowerIDs...)
	}
	if actor.FollowerRefs != nil {
		actor.FollowerRefs = append([]EntityRef(nil), actor.FollowerRefs...)
	}
	return actor
}

func removeStudyRoot(items *ItemCollection, root string) error {
	return removeStudyRootAt(items, StudyInventoryRoot, -1, root)
}

func removeStudyRootAt(items *ItemCollection, location StudyLocation, readySlot int, root string) error {
	if items == nil || root == "" {
		return fmt.Errorf("study item collection required")
	}
	if err := items.Validate(); err != nil {
		return err
	}
	switch location {
	case StudyInventoryRoot:
		remaining, err := removeInventoryRoot(items.Inventory, root)
		if err != nil {
			return err
		}
		items.Inventory = remaining
	case StudyReadySlot:
		if readySlot < 0 || readySlot >= len(items.Ready) || items.Ready[readySlot] != root {
			return fmt.Errorf("study ready root location changed")
		}
		items.Ready[readySlot] = ""
	default:
		return fmt.Errorf("study root location required")
	}
	stack := []string{root}
	seen := map[string]bool{}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[id] {
			return fmt.Errorf("cyclic study item subtree")
		}
		seen[id] = true
		item, ok := items.Items[id]
		if !ok {
			return fmt.Errorf("study item subtree absent")
		}
		stack = append(stack, item.Contents...)
		delete(items.Items, id)
	}
	return items.Validate()
}

// moveStudyRoot transfers one canonical root from Inventory or Ready to a
// room. TransferItemRoots handles Inventory roots; Ready roots need an
// explicit slot clear before the same complete subtree is attached to the
// room's ordered floor roots.
func moveStudyRoot(source, destination *ItemCollection, location StudyLocation, readySlot int, root string) error {
	if source == nil || destination == nil {
		return fmt.Errorf("study canonical transfer unavailable")
	}
	if location == StudyInventoryRoot {
		plan, err := TransferItemRoots(*source, *destination, []string{root})
		if err != nil {
			return err
		}
		*source, *destination = plan.Source, plan.Destination
		return nil
	}
	if location != StudyReadySlot || readySlot < 0 || readySlot >= len(source.Ready) || source.Ready[readySlot] != root {
		return fmt.Errorf("study ready root location changed")
	}
	if err := source.Validate(); err != nil {
		return err
	}
	if err := destination.Validate(); err != nil {
		return err
	}
	moved := map[string]Item{}
	nextSource := source.clone()
	nextSource.Ready[readySlot] = ""
	stack, seen := []string{root}, map[string]bool{}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[id] {
			return fmt.Errorf("cyclic study item subtree")
		}
		seen[id] = true
		item, ok := nextSource.Items[id]
		if !ok {
			return fmt.Errorf("study item subtree absent")
		}
		moved[id] = item
		delete(nextSource.Items, id)
		stack = append(stack, item.Contents...)
	}
	nextDestination := destination.clone()
	for id, item := range moved {
		if _, exists := nextDestination.Items[id]; exists {
			return fmt.Errorf("overlapping study item owners")
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

// PlanStudy ports the canonical direct-inventory path of magic1.c:study. It
// fails closed for every unresolved spell/item/catalog boundary and does not
// invoke randomness or allocate IDs.
func (s State) PlanStudy(actorID, itemName string, occurrence int) (StudyProposal, error) {
	actor, room, err := studyActor(s, actorID)
	if err != nil {
		return StudyProposal{}, err
	}
	if flag(actor.Body.Flags[:], studyPlayerBlindFlag) {
		return StudyProposal{}, ErrStudyBlind
	}
	itemID, item, location, readySlot, err := selectStudyRoot(actor, itemName, occurrence)
	if err != nil {
		return StudyProposal{}, err
	}
	if item.Object.Type != studyScrollType {
		return StudyProposal{}, fmt.Errorf("%w: %s", ErrStudyNotScroll, item.Object.Name)
	}
	if item.Object.DiceCount > int16(actor.Body.Level) {
		return StudyProposal{}, fmt.Errorf("%w: %s", ErrStudyLevel, item.Object.Name)
	}
	spellName, spellIndex, err := studySpellName(item.Object.MagicPower)
	if err != nil {
		return StudyProposal{}, err
	}
	proposal := StudyProposal{
		Action:            "study",
		ActorID:           actorID,
		RoomID:            actor.Body.RoomID,
		ItemID:            itemID,
		ItemName:          item.Object.Name,
		Selector:          itemName,
		Occurrence:        occurrence,
		Location:          location,
		ReadySlot:         readySlot,
		SpellIndex:        spellIndex,
		SpellName:         spellName,
		expectedActor:     cloneStudyActor(actor),
		expectedRoomItems: room.Items.clone(),
		expectedItem:      item,
	}
	if studyAlignmentRejected(actor.Body, item.Object) {
		proposal.MoveToRoom = true
		proposal.Response = studyResponse(item.Object.Name, spellName, false, true)
		return proposal, nil
	}
	if !studyClassAllowed(actor.Body, item.Object) {
		return StudyProposal{}, fmt.Errorf("%w: %s", ErrStudyClass, item.Object.Name)
	}
	proposal.Learned = true
	// The legacy command clears PHIDDN only after every admission check has
	// passed.  Alignment rejection moves the scroll but leaves hidden state
	// untouched, so ClearHidden is set only on the successful learn path.
	proposal.ClearHidden = true
	proposal.Response = studyResponse(item.Object.Name, spellName, true, false)
	proposal.RoomText = studyEventText(actor.Body.Name, item.Object.Name)
	return proposal, nil
}

// PlanStudyByName is a semantic alias used by command adapters.
func (s State) PlanStudyByName(actorID, itemName string, occurrence int) (StudyProposal, error) {
	return s.PlanStudy(actorID, itemName, occurrence)
}

// ApplyStudy commits a planned spell learn or the alignment rejection move.
// It rechecks the complete actor/item/room snapshots, so a stale proposal
// cannot delete a different item, set a different spell bit, or duplicate a
// room transfer.
func (s State) ApplyStudy(proposal StudyProposal) (State, StudyResult, error) {
	actor, room, err := studyActor(s, proposal.ActorID)
	if err != nil {
		return State{}, StudyResult{}, err
	}
	if proposal.Action != "study" || proposal.ActorID == "" || proposal.RoomID != actor.Body.RoomID || proposal.ItemID == "" || proposal.ItemName == "" || !validStudyName(proposal.Selector) || proposal.Occurrence < 1 || (proposal.Location != StudyInventoryRoot && proposal.Location != StudyReadySlot) || (proposal.Location == StudyInventoryRoot && proposal.ReadySlot != -1) || (proposal.Location == StudyReadySlot && (proposal.ReadySlot < 0 || proposal.ReadySlot >= len(actor.Items.Ready))) || proposal.SpellIndex < 0 || proposal.SpellIndex >= studyMaxSpellPower || proposal.SpellName == "" || proposal.Response == "" {
		return State{}, StudyResult{}, fmt.Errorf("invalid study proposal")
	}
	if !reflect.DeepEqual(actor, proposal.expectedActor) || room.Items == nil || !reflect.DeepEqual(*room.Items, proposal.expectedRoomItems) {
		return State{}, StudyResult{}, fmt.Errorf("stale study actor or room proposal")
	}
	itemID, item, location, readySlot, err := selectStudyRoot(actor, proposal.Selector, proposal.Occurrence)
	if err != nil || itemID != proposal.ItemID || location != proposal.Location || readySlot != proposal.ReadySlot || !reflect.DeepEqual(item, proposal.expectedItem) {
		return State{}, StudyResult{}, fmt.Errorf("stale study item proposal")
	}
	spellName, spellIndex, err := studySpellName(item.Object.MagicPower)
	if err != nil || spellName != proposal.SpellName || spellIndex != proposal.SpellIndex {
		return State{}, StudyResult{}, fmt.Errorf("stale study spell mapping")
	}
	if proposal.MoveToRoom {
		if proposal.Learned || proposal.ClearHidden || proposal.RoomText != "" || proposal.Response != studyResponse(proposal.ItemName, proposal.SpellName, false, true) || !studyAlignmentRejected(actor.Body, item.Object) {
			return State{}, StudyResult{}, fmt.Errorf("invalid study alignment proposal")
		}
	} else {
		if !proposal.Learned || !proposal.ClearHidden || proposal.Response != studyResponse(proposal.ItemName, proposal.SpellName, true, false) || proposal.RoomText != studyEventText(actor.Body.Name, proposal.ItemName) || studyAlignmentRejected(actor.Body, item.Object) || !studyClassAllowed(actor.Body, item.Object) {
			return State{}, StudyResult{}, fmt.Errorf("invalid study learn proposal")
		}
	}

	next := s.clone()
	nextActor := next.Players[proposal.ActorID]
	result := StudyResult{
		Action:      "study",
		Response:    proposal.Response,
		Changed:     true,
		Learned:     proposal.Learned,
		MovedToRoom: proposal.MoveToRoom,
		RoomID:      proposal.RoomID,
		ActorID:     proposal.ActorID,
		ActorName:   actor.Body.Name,
		ItemID:      proposal.ItemID,
		ItemName:    proposal.ItemName,
		Occurrence:  proposal.Occurrence,
		SpellIndex:  proposal.SpellIndex,
		SpellName:   proposal.SpellName,
	}
	if proposal.MoveToRoom {
		nextRoom := next.Rooms[proposal.RoomID]
		if nextActor.Items == nil || nextRoom.Items == nil {
			return State{}, StudyResult{}, fmt.Errorf("study canonical transfer unavailable")
		}
		if err := moveStudyRoot(nextActor.Items, nextRoom.Items, proposal.Location, proposal.ReadySlot, proposal.ItemID); err != nil {
			return State{}, StudyResult{}, err
		}
		next.Rooms[proposal.RoomID] = nextRoom
	} else {
		if err := removeStudyRootAt(nextActor.Items, proposal.Location, proposal.ReadySlot, proposal.ItemID); err != nil {
			return State{}, StudyResult{}, err
		}
		nextActor.Body.Flags[studyPlayerHiddenFlag/8] &^= 1 << (studyPlayerHiddenFlag % 8)
		nextActor.Body.Spells[proposal.SpellIndex/8] |= 1 << (proposal.SpellIndex % 8)
	}
	next.Players[proposal.ActorID] = nextActor
	if proposal.Learned {
		result.Broadcast = true
		event := StudyEvent{
			RoomID:         proposal.RoomID,
			ActorID:        proposal.ActorID,
			ActorName:      actor.Body.Name,
			ExcludeActorID: proposal.ActorID,
			ItemID:         proposal.ItemID,
			ItemName:       proposal.ItemName,
			SpellName:      proposal.SpellName,
			Text:           proposal.RoomText,
		}
		result.Event = &event
		result.EventText = event.Text
	}
	if err := next.Validate(); err != nil {
		return State{}, StudyResult{}, err
	}
	return next, result, nil
}

// Study is the reducer-shaped convenience API.
func (s State) Study(actorID, itemName string, occurrence int) (State, StudyResult, error) {
	proposal, err := s.PlanStudy(actorID, itemName, occurrence)
	if err != nil {
		return State{}, StudyResult{}, err
	}
	return s.ApplyStudy(proposal)
}
