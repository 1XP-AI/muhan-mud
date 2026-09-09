package world

// This file is the deliberately small Go boundary for command9.c:use.  The
// legacy handler is a dispatcher, not a second item implementation: it finds
// one direct object, clears PHIDDN, and delegates to ready/wear/drink/etc.
// Keep that distinction here.  Only delegates which already have an atomic
// canonical reducer are admitted; a missing delegate is an explicit,
// typed fail-closed boundary.

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	useSharpType   = 0  // SHARP
	useThrustType  = 1  // THRUST
	useBluntType   = 2  // BLUNT
	usePoleType    = 3  // POLE
	useMissileType = 4  // MISSILE
	useArmorType   = 5  // ARMOR
	usePotionType  = 6  // POTION
	useScrollType  = 7  // SCROLL
	useWandType    = 8  // WAND
	useKeyType     = 11 // KEY
	useLightType   = 12 // LIGHTSOURCE
	useWarSpecial  = 3  // SP_WAR (object.special)
	useFloorFlag   = 24 // OUSEFL
)

// ErrUseUnsupported is returned when command9.c would dispatch to a reducer
// which is not yet represented by a canonical Go state transition.  It is a
// sentinel so callers can retain a stable error contract while the typed
// value below records the exact bounded edge.
var ErrUseUnsupported = errors.New("use target is unsupported")

// ErrUseFloor is a distinct fail-closed error for a floor object which is not
// marked OUSEFL.  In particular, knowing an object's display name never
// grants permission to use arbitrary room state.
var ErrUseFloor = errors.New("floor item is not usable")

// UseUnsupportedError identifies an unresolved use dispatcher edge.  It is
// intentionally not constructible from a client command; world planning
// creates it only after resolving a canonical object.
type UseUnsupportedError struct {
	ItemID   string
	ItemName string
	Type     byte
	Reason   string
}

func (e *UseUnsupportedError) Error() string {
	if e == nil {
		return ErrUseUnsupported.Error()
	}
	if e.Reason == "" {
		return fmt.Sprintf("%s: %s", ErrUseUnsupported, e.ItemName)
	}
	return fmt.Sprintf("%s: %s (%s)", ErrUseUnsupported, e.ItemName, e.Reason)
}

func (e *UseUnsupportedError) Unwrap() error { return ErrUseUnsupported }

// UseFloorError records a failed OUSEFL authorization without exposing a
// client-selectable object identity as permission.  It also unwraps to
// ErrUseFloor for callers which only need the category.
type UseFloorError struct {
	ItemID   string
	ItemName string
}

func (e *UseFloorError) Error() string {
	if e == nil {
		return ErrUseFloor.Error()
	}
	return fmt.Sprintf("%s: %s", ErrUseFloor, e.ItemName)
}

func (e *UseFloorError) Unwrap() error { return ErrUseFloor }

// UseLocation identifies the direct root from which the dispatcher selected
// an object.  Nested descendants and already-equipped roots are not command
// selectors; existing reducers retain their own ready-slot rules.
type UseLocation string

const (
	UseInventoryRoot UseLocation = "inventory"
	UseFloorRoot     UseLocation = "floor"
)

// UseEvent is emitted only after the candidate state commits.  The actor's
// response is separate; ExcludeActorID lets transport fan-out preserve the
// source broadcast ordering without sending a second copy to the actor.
type UseEvent struct {
	RoomID         int16  `json:"room_id"`
	ActorID        string `json:"actor_id"`
	ActorName      string `json:"actor_name"`
	ItemID         string `json:"item_id"`
	ItemName       string `json:"item_name"`
	ExcludeActorID string `json:"exclude_actor_id"`
	Text           string `json:"text"`
}

// UseResult is the durable receipt payload for command9.c:use.  Delegated
// potion fields are flattened so an adapter need not know which reducer was
// selected to publish the response and event.
type UseResult struct {
	Action      string      `json:"action"`
	Response    string      `json:"response"`
	Broadcast   bool        `json:"broadcast"`
	Changed     bool        `json:"changed"`
	ItemID      string      `json:"item_id"`
	ItemName    string      `json:"item_name"`
	Occurrence  int         `json:"occurrence"`
	Location    UseLocation `json:"location"`
	ItemType    byte        `json:"item_type"`
	Slot        int         `json:"slot,omitempty"`
	Consumed    bool        `json:"consumed,omitempty"`
	MovedToRoom bool        `json:"moved_to_room,omitempty"`
	Special     bool        `json:"special,omitempty"`
	Effect      string      `json:"effect,omitempty"`
	HPDelta     int32       `json:"hp_delta,omitempty"`
	MPDelta     int32       `json:"mp_delta,omitempty"`
	Event       *UseEvent   `json:"event,omitempty"`
}

// UseOptions binds the host clock and the only currently admitted random
// source (potion effects). ApplyUse never invokes Roll.
type UseOptions struct {
	Now  int32
	Roll func(int, int) int
}

// UseProposal is an in-memory, snapshot-bound candidate. expectedState and
// nextState are private on purpose: no client can manufacture an identity or
// bypass the direct-root/OUSEFL checks by submitting a proposal over the
// wire.  The full candidate makes the floor transfer and delegated reducer a
// single atomic state replacement.
type UseProposal struct {
	Action     string
	ActorID    string
	RoomID     int16
	ItemID     string
	ItemName   string
	Occurrence int
	Location   UseLocation
	ItemType   byte
	Now        int32
	Mode       EquipmentMode
	Result     UseResult

	expectedState State
	nextState     State
}

func validUseName(name string) bool {
	if name == "" || !utf8.ValidString(name) || strings.TrimSpace(name) != name {
		return false
	}
	for _, r := range name {
		// Quotes are intentionally rejected here as well as by the terminal
		// parser.  This prevents a direct world caller from broadening the
		// one-token command into a quoted/multiword selector.
		if unicode.IsControl(r) || r == '\'' || r == '"' || r == '\r' || r == '\n' || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

func useActor(s State, actorID string) (PlayerState, RoomState, error) {
	if err := s.Validate(); err != nil {
		return PlayerState{}, RoomState{}, err
	}
	actor, ok := s.Players[actorID]
	if actorID == "" || !ok || !actor.Online || actor.Body.Type != 0 || actor.Body.Name == "" || actor.Items == nil {
		return PlayerState{}, RoomState{}, fmt.Errorf("online use actor with migrated inventory required")
	}
	if len(actor.Body.Inventory) != 0 {
		return PlayerState{}, RoomState{}, fmt.Errorf("duplicate legacy and canonical use inventory")
	}
	if err := actor.Items.Validate(); err != nil {
		return PlayerState{}, RoomState{}, err
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok {
		return PlayerState{}, RoomState{}, fmt.Errorf("use actor room absent")
	}
	if room.Items != nil {
		if err := room.Items.Validate(); err != nil {
			return PlayerState{}, RoomState{}, err
		}
	}
	return actor, room, nil
}

type useRoot struct {
	id         string
	item       Item
	location   UseLocation
	occurrence int
}

// selectUseRoot follows command9.c's inventory-first lookup.  The fallback
// occurrence is evaluated against the floor roots independently, exactly as
// the two find_obj calls do.  Matching is exact case-insensitive; no prefix,
// quote, nested descendant or client item ID is accepted.
func selectUseRoot(actor PlayerState, room RoomState, name string, occurrence int) (useRoot, error) {
	if occurrence < 1 || !validUseName(name) {
		return useRoot{}, fmt.Errorf("invalid use item selector")
	}
	found := 0
	for _, id := range actor.Items.Inventory {
		item, ok := actor.Items.Items[id]
		if !ok {
			return useRoot{}, fmt.Errorf("canonical use inventory root absent")
		}
		if !strings.EqualFold(item.Object.Name, name) {
			continue
		}
		found++
		if found == occurrence {
			return useRoot{id: id, item: item, location: UseInventoryRoot, occurrence: occurrence}, nil
		}
	}
	found = 0
	if room.Items == nil {
		return useRoot{}, fmt.Errorf("%w: %q", ErrUseUnsupported, name)
	}
	for _, id := range room.Items.Inventory {
		item, ok := room.Items.Items[id]
		if !ok {
			return useRoot{}, fmt.Errorf("canonical use floor root absent")
		}
		if !strings.EqualFold(item.Object.Name, name) {
			continue
		}
		found++
		if found == occurrence {
			if !flag(item.Object.Flags[:], useFloorFlag) {
				return useRoot{}, &UseFloorError{ItemID: id, ItemName: item.Object.Name}
			}
			return useRoot{id: id, item: item, location: UseFloorRoot, occurrence: occurrence}, nil
		}
	}
	return useRoot{}, fmt.Errorf("%w: %q", ErrUseUnsupported, name)
}

func useMode(item LegacyObject) (EquipmentMode, bool) {
	switch item.Type {
	case useSharpType, useThrustType, useBluntType, usePoleType, useMissileType:
		return EquipmentWield, true
	case useArmorType:
		return EquipmentWear, true
	case useLightType:
		return EquipmentHold, true
	default:
		return "", false
	}
}

func useUnsupported(root useRoot, reason string) error {
	return &UseUnsupportedError{ItemID: root.id, ItemName: root.item.Object.Name, Type: root.item.Object.Type, Reason: reason}
}

func transferUseFloor(s State, actorID string, root useRoot) (State, int, error) {
	if root.location != UseFloorRoot {
		return s, root.occurrence, nil
	}
	actor := s.Players[actorID]
	room := s.Rooms[actor.Body.RoomID]
	plan, err := TransferItemRoots(*room.Items, *actor.Items, []string{root.id})
	if err != nil {
		return State{}, 0, err
	}
	next := s.clone()
	nextRoom := next.Rooms[actor.Body.RoomID]
	nextRoom.Items = &plan.Source
	next.Rooms[actor.Body.RoomID] = nextRoom
	nextActor := next.Players[actorID]
	nextActor.Items = &plan.Destination
	next.Players[actorID] = nextActor
	// Existing reducers select by exact name+occurrence, not ID. Recompute the
	// destination occurrence so the floor-selected identity remains bound even
	// when duplicate floor roots were present.
	found := 0
	for _, id := range plan.Destination.Inventory {
		item := plan.Destination.Items[id]
		if strings.EqualFold(item.Object.Name, root.item.Object.Name) {
			found++
		}
		if id == root.id {
			if found == 0 {
				return State{}, 0, fmt.Errorf("use floor occurrence binding failed")
			}
			return next, found, nil
		}
	}
	return State{}, 0, fmt.Errorf("use floor root moved without destination identity")
}

func useEquipmentResponse(item LegacyObject, action string) string {
	verb := "사용했습니다"
	switch action {
	case string(EquipmentWield):
		verb = "전투태세를 취합니다"
	case string(EquipmentWear):
		verb = "입었습니다"
	case string(EquipmentHold):
		verb = "쥐었습니다"
	}
	response := fmt.Sprintf("당신은 %s을(를) %s.", item.Name, verb)
	if item.UseOutput != "" {
		response += "\r\n" + item.UseOutput
	}
	if !strings.HasSuffix(response, "\r\n") {
		response += "\r\n"
	}
	return response
}

func useEquipmentEvent(actor LegacyMonster, actorID string, item LegacyObject, action string, roomID int16, itemID string) *UseEvent {
	verb := "사용합니다"
	switch action {
	case string(EquipmentWield):
		verb = "무장합니다"
	case string(EquipmentWear):
		verb = "입습니다"
	case string(EquipmentHold):
		verb = "쥡니다"
	}
	return &UseEvent{
		RoomID: roomID, ActorID: actorID, ActorName: actor.Name,
		ItemID: itemID, ItemName: item.Name, ExcludeActorID: actorID,
		Text: fmt.Sprintf("\n%s이 %s.", actor.Name, fmt.Sprintf("%s을(를) %s", item.Name, verb)),
	}
}

func useResultFromDrink(result DrinkResult, root useRoot) UseResult {
	var event *UseEvent
	if result.Event != nil {
		event = &UseEvent{RoomID: result.Event.RoomID, ActorID: result.Event.ActorID, ActorName: result.Event.ActorName, ItemID: result.Event.ItemID, ItemName: result.Event.ItemName, ExcludeActorID: result.Event.ExcludeActorID, Text: result.Event.Text}
	}
	return UseResult{
		Action: "use", Response: result.Response, Broadcast: result.Broadcast,
		Changed: result.Changed, ItemID: root.id, ItemName: root.item.Object.Name,
		Occurrence: root.occurrence, Location: root.location, ItemType: root.item.Object.Type,
		Consumed: result.Consumed, MovedToRoom: result.MovedToRoom, Special: result.Special,
		Effect: result.Effect, HPDelta: result.HPDelta, MPDelta: result.MPDelta, Event: event,
	}
}

// PlanUse resolves one direct object and composes only the existing canonical
// equipment/drink reducers. Floor roots are transferred into the actor's
// inventory in the same candidate before delegation; the final ApplyUse
// replaces both owners atomically. key/scroll/wand/special/unknown branches
// fail before PHIDDN or item state is changed.
func (s State) PlanUse(actorID, itemName string, occurrence int, options UseOptions) (UseProposal, error) {
	if options.Now < 0 {
		return UseProposal{}, fmt.Errorf("use clock must be nonnegative")
	}
	actor, room, err := useActor(s, actorID)
	if err != nil {
		return UseProposal{}, err
	}
	root, err := selectUseRoot(actor, room, itemName, occurrence)
	if err != nil {
		return UseProposal{}, err
	}
	if root.item.Object.Special == useWarSpecial {
		return UseProposal{}, useUnsupported(root, "special object")
	}
	mode, equipment := useMode(root.item.Object)
	if !equipment && root.item.Object.Type != usePotionType {
		reason := "unresolved reducer"
		switch root.item.Object.Type {
		case useScrollType:
			reason = "scroll reducer"
		case useWandType:
			reason = "wand reducer"
		case useKeyType:
			reason = "unlock target is absent from one-token use"
		}
		return UseProposal{}, useUnsupported(root, reason)
	}

	working, reducerOccurrence, err := transferUseFloor(s, actorID, root)
	if err != nil {
		return UseProposal{}, err
	}
	proposal := UseProposal{
		Action: "use", ActorID: actorID, RoomID: actor.Body.RoomID,
		ItemID: root.id, ItemName: root.item.Object.Name, Occurrence: occurrence,
		Location: root.location, ItemType: root.item.Object.Type, Now: options.Now,
		Mode: mode, expectedState: s.clone(),
	}
	if equipment {
		next, mutation, err := working.ReadyItem(actorID, root.item.Object.Name, reducerOccurrence, mode)
		if err != nil {
			return UseProposal{}, err
		}
		// command9.c clears PHIDDN immediately after direct-object resolution;
		// keep it in the same candidate as the equipment move.
		nextActor := next.Players[actorID]
		setSettingFlag(&nextActor.Body, playerHiddenStateFlag, false)
		next.Players[actorID] = nextActor
		item := root.item.Object
		result := UseResult{
			Action: "use", Response: useEquipmentResponse(item, mutation.Action),
			Broadcast: true, Changed: true, ItemID: root.id, ItemName: item.Name,
			Occurrence: occurrence, Location: root.location, ItemType: item.Type,
			Slot:  mutation.Slot,
			Event: useEquipmentEvent(actor.Body, actorID, item, mutation.Action, actor.Body.RoomID, root.id),
		}
		proposal.Result = result
		proposal.nextState = next
		return proposal, nil
	}

	drinkProposal, err := working.PlanDrink(actorID, root.item.Object.Name, reducerOccurrence, DrinkOptions{Now: options.Now, Roll: options.Roll})
	if err != nil {
		return UseProposal{}, err
	}
	next, drinkResult, err := working.ApplyDrink(drinkProposal)
	if err != nil {
		return UseProposal{}, err
	}
	// Drink already commits any consume/move atomically within working. PHIDDN
	// is a dispatcher concern, so clear it in the same final candidate.
	nextActor := next.Players[actorID]
	setSettingFlag(&nextActor.Body, playerHiddenStateFlag, false)
	next.Players[actorID] = nextActor
	proposal.Result = useResultFromDrink(drinkResult, root)
	proposal.nextState = next
	return proposal, nil
}

// ApplyUse accepts only the exact snapshot candidate produced by PlanUse.
// No lookup, random draw, or reducer is rerun. This is the receipt/replay
// boundary: the session executor persists the returned replacement once and
// serves the stored response on command-ID replay.
func (s State) ApplyUse(proposal UseProposal) (State, UseResult, error) {
	if proposal.Action != "use" || proposal.ActorID == "" || proposal.ItemID == "" || proposal.ItemName == "" || proposal.Occurrence < 1 || proposal.nextState.Version == 0 {
		return State{}, UseResult{}, fmt.Errorf("invalid use proposal")
	}
	if !reflect.DeepEqual(s, proposal.expectedState) {
		return State{}, UseResult{}, fmt.Errorf("stale use proposal")
	}
	if err := proposal.nextState.Validate(); err != nil {
		return State{}, UseResult{}, err
	}
	if proposal.Result.ItemID != proposal.ItemID || proposal.Result.ItemName != proposal.ItemName || proposal.Result.Occurrence != proposal.Occurrence || proposal.Result.Location != proposal.Location || proposal.Result.ItemType != proposal.ItemType || proposal.Result.Response == "" {
		return State{}, UseResult{}, fmt.Errorf("invalid use result")
	}
	return proposal.nextState.clone(), proposal.Result, nil
}

// UseItemByName is the convenience entry point used by small in-process
// callers. Durable session commands should call PlanUse then ApplyUse so the
// proposal is retained across the storage transaction.
func (s State) UseItemByName(actorID, itemName string, occurrence int, options UseOptions) (State, UseResult, error) {
	p, err := s.PlanUse(actorID, itemName, occurrence, options)
	if err != nil {
		return State{}, UseResult{}, err
	}
	return s.ApplyUse(p)
}
