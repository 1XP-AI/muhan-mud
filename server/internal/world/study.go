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

// StudyProposal is a snapshot-bound candidate for magic1.c:study. The item
// ID is resolved from the canonical direct inventory during planning; it is
// never accepted from a client. No random source or ID allocator is involved.
type StudyProposal struct {
	Action      string
	ActorID     string
	RoomID      int16
	ItemID      string
	ItemName    string
	Occurrence  int
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

func selectStudyInventoryRoot(actor PlayerState, name string, occurrence int) (string, Item, error) {
	if occurrence < 1 {
		return "", Item{}, fmt.Errorf("invalid study occurrence")
	}
	if !validStudyName(name) {
		return "", Item{}, fmt.Errorf("invalid study item name")
	}
	if actor.Items == nil || len(actor.Body.Inventory) != 0 {
		return "", Item{}, fmt.Errorf("canonical study player inventory required")
	}
	if err := actor.Items.Validate(); err != nil {
		return "", Item{}, err
	}
	found := 0
	for _, id := range actor.Items.Inventory {
		item, ok := actor.Items.Items[id]
		if !ok || id == "" {
			return "", Item{}, fmt.Errorf("canonical study inventory root absent")
		}
		if !strings.EqualFold(item.Object.Name, name) {
			continue
		}
		found++
		if found == occurrence {
			return id, item, nil
		}
	}
	return "", Item{}, fmt.Errorf("%w: %q", ErrStudyMissingItem, name)
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
	if items == nil || root == "" {
		return fmt.Errorf("study item collection required")
	}
	if err := items.Validate(); err != nil {
		return err
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
	items.Inventory = remaining
	return items.Validate()
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
	itemID, item, err := selectStudyInventoryRoot(actor, itemName, occurrence)
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
		Occurrence:        occurrence,
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
	if proposal.Action != "study" || proposal.ActorID == "" || proposal.RoomID != actor.Body.RoomID || proposal.ItemID == "" || proposal.ItemName == "" || proposal.Occurrence < 1 || proposal.SpellIndex < 0 || proposal.SpellIndex >= studyMaxSpellPower || proposal.SpellName == "" || proposal.Response == "" {
		return State{}, StudyResult{}, fmt.Errorf("invalid study proposal")
	}
	if !reflect.DeepEqual(actor, proposal.expectedActor) || room.Items == nil || !reflect.DeepEqual(*room.Items, proposal.expectedRoomItems) {
		return State{}, StudyResult{}, fmt.Errorf("stale study actor or room proposal")
	}
	itemID, item, err := selectStudyInventoryRoot(actor, proposal.ItemName, proposal.Occurrence)
	if err != nil || itemID != proposal.ItemID || !reflect.DeepEqual(item, proposal.expectedItem) {
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
		plan, err := TransferItemRoots(*nextActor.Items, *nextRoom.Items, []string{proposal.ItemID})
		if err != nil {
			return State{}, StudyResult{}, err
		}
		nextActor.Items = &plan.Source
		nextRoom.Items = &plan.Destination
		next.Rooms[proposal.RoomID] = nextRoom
	} else {
		if err := removeStudyRoot(nextActor.Items, proposal.ItemID); err != nil {
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
