package world

// This file is the deliberately bounded Go port of magic1.c:readscroll.  A
// scroll command is admitted only when its effect can be expressed by the
// already-canonical self-target potion reducer.  Offensive, targeted,
// special/map and otherwise unresolved spell functions remain fail-closed.

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	readScrollType       = 7  // SCROLL
	readScrollGoodOnly   = 12 // OGOODO
	readScrollEvilOnly   = 13 // OEVILO
	readScrollClass      = 31 // OCLSEL
	readScrollNoMagic    = 17 // RNOMAG
	readScrollTimerIndex = 12 // LT_READS
	readScrollBlindFlag  = 42 // PBLIND
	readScrollHiddenFlag = 1  // PHIDDN
	readScrollSpecial    = 44 // OSPECI
	readScrollCaretaker  = 10 // CARETAKER
	readScrollMaxClass   = 12 // DM
	readScrollMaxSpell   = 56 // spllist entries 1..56

	// Descriptive aliases used by package-local fixtures.
	readScrollGoodOnlyFlag    = readScrollGoodOnly
	readScrollEvilOnlyFlag    = readScrollEvilOnly
	readScrollClassSelectFlag = readScrollClass
	readScrollNoMagicRoomFlag = readScrollNoMagic
)

// Export source-backed values for command adapters and differential tests.
const (
	ReadScrollType       = readScrollType
	ReadScrollGoodOnly   = readScrollGoodOnly
	ReadScrollEvilOnly   = readScrollEvilOnly
	ReadScrollClassFlag  = readScrollClass
	ReadScrollNoMagic    = readScrollNoMagic
	ReadScrollTimerIndex = readScrollTimerIndex
	ReadScrollBlindFlag  = readScrollBlindFlag
	ReadScrollHiddenFlag = readScrollHiddenFlag
	ReadScrollSpecial    = readScrollSpecial
)

var (
	ErrReadScrollBlind            = errors.New("read scroll unavailable while blind")
	ErrReadScrollMissingItem      = errors.New("read scroll item not found")
	ErrReadScrollNotScroll        = errors.New("read target is not a scroll")
	ErrReadScrollEmpty            = errors.New("scroll has no remaining uses")
	ErrReadScrollLevel            = errors.New("scroll level is too high")
	ErrReadScrollAlignment        = errors.New("scroll rejects actor alignment")
	ErrReadScrollClass            = errors.New("scroll rejects actor class")
	ErrReadScrollNoMagicRoom      = errors.New("scroll magic is not allowed here")
	ErrReadScrollCooldown         = errors.New("scroll is on cooldown")
	ErrReadScrollUnsupported      = errors.New("read scroll effect unsupported")
	ErrReadScrollSpellUnavailable = ErrReadScrollUnsupported
	ErrReadScrollRandom           = errors.New("read scroll random source unavailable")
)

// ScrollLocation identifies a direct inventory root or an equipped root.  A
// nested item is never selected as an independent command target.
type ScrollLocation string

const (
	ScrollInventoryRoot ScrollLocation = "inventory"
	ScrollReadySlot     ScrollLocation = "ready"

	// Short aliases keep callers that use the legacy vocabulary readable.
	ScrollInventory ScrollLocation = ScrollInventoryRoot
	ScrollReady     ScrollLocation = ScrollReadySlot
)

// ScrollOptions contains only host-owned values.  Roll is required because
// C calls mrand for spell_fail even when a class has the default no-failure
// branch; ApplyReadScroll never calls it.
type ScrollOptions struct {
	Now  int32
	Roll func(int, int) int
}

type ScrollEvent struct {
	RoomID         int16  `json:"room_id"`
	ActorID        string `json:"actor_id"`
	ActorName      string `json:"actor_name"`
	ItemID         string `json:"item_id"`
	ItemName       string `json:"item_name"`
	SpellName      string `json:"spell_name"`
	ExcludeActorID string `json:"exclude_actor_id"`
	Text           string `json:"text"`
}

// ScrollResult is the deterministic actor response and committed room
// projection.  The random draws are retained in receipt order so a durable
// replay cannot ask the host RNG for a different spell outcome.
type ScrollResult struct {
	Action        string         `json:"action"`
	Response      string         `json:"response"`
	Broadcast     bool           `json:"broadcast"`
	Changed       bool           `json:"changed"`
	NoOp          bool           `json:"no_op,omitempty"`
	Cooldown      bool           `json:"cooldown,omitempty"`
	WaitSeconds   int32          `json:"wait_seconds,omitempty"`
	Attempted     bool           `json:"attempted,omitempty"`
	Succeeded     bool           `json:"succeeded,omitempty"`
	SpellFailed   bool           `json:"spell_failed,omitempty"`
	Consumed      bool           `json:"consumed,omitempty"`
	MovedToRoom   bool           `json:"moved_to_room,omitempty"`
	ClearHidden   bool           `json:"clear_hidden,omitempty"`
	ItemID        string         `json:"item_id,omitempty"`
	ItemName      string         `json:"item_name,omitempty"`
	Occurrence    int            `json:"occurrence,omitempty"`
	Location      ScrollLocation `json:"location,omitempty"`
	ReadySlot     int            `json:"ready_slot,omitempty"`
	MagicPower    byte           `json:"magic_power,omitempty"`
	SpellIndex    int            `json:"spell_index,omitempty"`
	SpellName     string         `json:"spell_name,omitempty"`
	Effect        string         `json:"effect,omitempty"`
	Chance        int            `json:"chance,omitempty"`
	Roll          int            `json:"roll,omitempty"`
	SpellFailRoll int            `json:"spell_fail_roll,omitempty"`
	Rolls         []int          `json:"rolls,omitempty"`
	HPDelta       int32          `json:"hp_delta,omitempty"`
	MPDelta       int32          `json:"mp_delta,omitempty"`
	ShotsBefore   int16          `json:"shots_before,omitempty"`
	ShotsAfter    int16          `json:"shots_after,omitempty"`
	RoomID        int16          `json:"room_id"`
	ActorID       string         `json:"actor_id"`
	ActorName     string         `json:"actor_name"`
	EventText     string         `json:"event_text,omitempty"`
	Event         *ScrollEvent   `json:"event,omitempty"`
}

// ScrollProposal is a snapshot-bound receipt candidate.  The private fields
// are intentionally not serializable authority: they make Apply reject a
// candidate that was planned against a different canonical actor, room or
// item graph.
type ScrollProposal struct {
	Action        string
	ActorID       string
	RoomID        int16
	ItemID        string
	ItemName      string
	Occurrence    int
	Location      ScrollLocation
	ReadySlot     int
	Now           int32
	MagicPower    byte
	SpellIndex    int
	SpellName     string
	Effect        string
	Chance        int
	Roll          int
	SpellFailRoll int
	Rolls         []int
	HPDelta       int32
	MPDelta       int32
	ShotsBefore   int16
	ShotsAfter    int16

	Cooldown    bool
	WaitSeconds int32
	NoOp        bool
	Attempted   bool
	Succeeded   bool
	SpellFailed bool
	Consumed    bool
	MovedToRoom bool
	ClearHidden bool
	Broadcast   bool
	Response    string
	RoomText    string

	expectedActor PlayerState
	expectedRoom  RoomState
	expectedItem  Item
	expectedTimer LegacyTimer
	afterBody     LegacyMonster
}

// ReadScrollProposal and ReadScrollResult are noun-prefixed aliases for
// adapters that prefer names parallel to PlanReadScroll.
type ReadScrollProposal = ScrollProposal
type ReadScrollResult = ScrollResult

func validReadScrollText(value string, allowNewline bool) bool {
	if value == "" || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r == '\x00' || (!allowNewline && (r == '\r' || r == '\n')) || unicode.IsControl(r) && r != '\r' && r != '\n' {
			return false
		}
	}
	return true
}

func readScrollCRLF(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if !validReadScrollText(value, true) {
		return "", fmt.Errorf("%w: invalid scroll output", ErrReadScrollUnsupported)
	}
	// Legacy resources occasionally contain a bare LF.  Normalize all line
	// endings at the world boundary so the receipt is always UTF-8/CRLF.
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return strings.ReplaceAll(value, "\n", "\r\n"), nil
}

func readScrollActor(s State, actorID string) (PlayerState, RoomState, error) {
	if err := s.Validate(); err != nil {
		return PlayerState{}, RoomState{}, err
	}
	actor, ok := s.Players[actorID]
	if actorID == "" || !ok || !actor.Online || actor.Body.Type != 0 || !validReadScrollText(actor.Body.Name, false) || actor.Items == nil {
		return PlayerState{}, RoomState{}, fmt.Errorf("online canonical read-scroll actor with inventory required")
	}
	if actor.Body.Class > readScrollMaxClass {
		return PlayerState{}, RoomState{}, fmt.Errorf("read-scroll actor class outside legacy table")
	}
	if actor.Body.Stats[3] > 63 {
		return PlayerState{}, RoomState{}, fmt.Errorf("read-scroll actor intelligence outside legacy table")
	}
	if len(actor.Body.Inventory) != 0 {
		return PlayerState{}, RoomState{}, fmt.Errorf("duplicate legacy and canonical read-scroll inventory")
	}
	if err := actor.Items.Validate(); err != nil {
		return PlayerState{}, RoomState{}, err
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok {
		return PlayerState{}, RoomState{}, fmt.Errorf("read-scroll actor room absent")
	}
	if !validReadScrollText(room.Resource.Name, false) && room.Resource.Name != "" {
		return PlayerState{}, RoomState{}, fmt.Errorf("read-scroll room name is not valid UTF-8")
	}
	if room.Items != nil {
		if err := room.Items.Validate(); err != nil {
			return PlayerState{}, RoomState{}, err
		}
	}
	return actor, room, nil
}

func selectReadScrollRoot(actor PlayerState, name string, occurrence int) (string, Item, ScrollLocation, int, error) {
	if occurrence < 1 || !validReadScrollText(name, false) || actor.Items == nil {
		return "", Item{}, "", -1, ErrReadScrollMissingItem
	}
	found := 0
	for _, id := range actor.Items.Inventory {
		item, ok := actor.Items.Items[id]
		if !ok {
			return "", Item{}, "", -1, fmt.Errorf("canonical read-scroll inventory root absent")
		}
		if !strings.EqualFold(item.Object.Name, name) {
			continue
		}
		found++
		if found == occurrence {
			return id, item, ScrollInventoryRoot, -1, nil
		}
	}
	// magic1.c's find_obj fallback searches ready slots only after the direct
	// inventory list, preserving one-based occurrence order across both roots.
	for slot, id := range actor.Items.Ready {
		if id == "" {
			continue
		}
		item, ok := actor.Items.Items[id]
		if !ok {
			return "", Item{}, "", -1, fmt.Errorf("canonical read-scroll ready root absent")
		}
		if !strings.EqualFold(item.Object.Name, name) {
			continue
		}
		found++
		if found == occurrence {
			return id, item, ScrollReadySlot, slot, nil
		}
	}
	return "", Item{}, "", -1, fmt.Errorf("%w: %q", ErrReadScrollMissingItem, name)
}

func readScrollAlignmentRejected(actor LegacyMonster, object LegacyObject) bool {
	return (flag(object.Flags[:], readScrollGoodOnly) && actor.Alignment < -100) ||
		(flag(object.Flags[:], readScrollEvilOnly) && actor.Alignment > 100)
}

func readScrollClassAllowed(actor LegacyMonster, object LegacyObject) bool {
	if !flag(object.Flags[:], readScrollClass) || actor.Class >= readScrollCaretaker {
		return true
	}
	return flag(object.Flags[:], uint(readScrollClass)+uint(actor.Class))
}

func readScrollSpell(object LegacyObject) (int, string, drinkSpellSpec, error) {
	if object.MagicPower < 1 || int(object.MagicPower) > readScrollMaxSpell || int(object.MagicPower)-1 >= len(legacyInfoSpellNames) {
		return 0, "", drinkSpellSpec{}, fmt.Errorf("%w: magicpower=%d", ErrReadScrollUnsupported, object.MagicPower)
	}
	index := int(object.MagicPower) - 1
	name := legacyInfoSpellNames[index]
	if name == "" || !validReadScrollText(name, false) {
		return 0, "", drinkSpellSpec{}, fmt.Errorf("%w: spell index=%d", ErrReadScrollUnsupported, index)
	}
	spec, ok := drinkSpells[index]
	if !ok || spec.effect == "" {
		return 0, "", drinkSpellSpec{}, fmt.Errorf("%w: spell=%s", ErrReadScrollUnsupported, name)
	}
	return index, name, spec, nil
}

func readScrollSpellChance(body LegacyMonster) (int, error) {
	if body.Class > readScrollMaxClass || body.Stats[3] > 63 {
		return 0, fmt.Errorf("read-scroll spell-failure table input outside legacy range")
	}
	levelBand := (int(body.Level) + 3) / 4
	bonus := legacyStatBonus[body.Stats[3]]
	switch body.Class {
	case 1: // ASSASSIN
		return (levelBand+bonus)*5 + 30, nil
	case 2: // BARBARIAN
		return (levelBand + bonus) * 5, nil
	case 3: // CLERIC
		return (levelBand+bonus)*5 + 65, nil
	case 4: // FIGHTER
		return (levelBand+bonus)*5 + 10, nil
	case 5: // MAGE
		return (levelBand+bonus)*5 + 75, nil
	case 6: // PALADIN
		return (levelBand+bonus)*5 + 50, nil
	case 7: // RANGER
		return (levelBand+bonus)*4 + 56, nil
	case 8: // THIEF
		return (levelBand+bonus)*6 + 22, nil
	default:
		// The legacy default branch returns success after consuming mrand.
		return math.MaxInt, nil
	}
}

func readScrollRoll(options ScrollOptions, low, high int, draws *[]int) (value int, err error) {
	if options.Roll == nil {
		return 0, ErrReadScrollRandom
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			value = 0
			err = fmt.Errorf("%w: random source panicked: %v", ErrReadScrollRandom, recovered)
		}
	}()
	value = options.Roll(low, high)
	if value < low || value > high {
		return 0, fmt.Errorf("read-scroll random value outside %d..%d", low, high)
	}
	*draws = append(*draws, value)
	return value, nil
}

func readScrollReplayDraw(draws []int, at *int, low, high int) (int, error) {
	if *at < 0 || *at >= len(draws) {
		return 0, fmt.Errorf("read-scroll receipt missing random draw")
	}
	value := draws[*at]
	*at++
	if value < low || value > high {
		return 0, fmt.Errorf("read-scroll receipt random value outside %d..%d", low, high)
	}
	return value, nil
}

func readScrollSetTimer(body *LegacyMonster, now int32) error {
	if now < 0 {
		return fmt.Errorf("read-scroll clock must be nonnegative")
	}
	timer := body.Timers[readScrollTimerIndex]
	timer.LastTime = now
	timer.Interval = 3
	body.Timers[readScrollTimerIndex] = timer
	return nil
}

func readScrollWaitResponse(wait int64) (string, error) {
	if wait < 1 || wait > math.MaxInt32 {
		return "", fmt.Errorf("read-scroll cooldown outside response range")
	}
	if wait == 1 {
		return "1초만 기다리세요.\r\n", nil
	}
	return fmt.Sprintf("%d초동안 기다리세요.\r\n", wait), nil
}

func readScrollBurnResponse(itemName string) string {
	return fmt.Sprintf("\n모든 것을 읽고 나자 %s의 형체가 먼지로 변하면서 바람과 함께 사라져 버렸습니다.\r\n", itemName)
}

func readScrollSuccessResponse(object LegacyObject, itemName string) (string, error) {
	output, err := readScrollCRLF(object.UseOutput)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	if output != "" {
		out.WriteString(output)
		if !strings.HasSuffix(output, "\r\n") {
			out.WriteString("\r\n")
		}
	}
	out.WriteString(readScrollBurnResponse(itemName))
	return out.String(), nil
}

func readScrollRoomText(actorName, itemName string) string {
	return fmt.Sprintf("\n%s님이 %s의 주문을 읽습니다.\r\n", actorName, itemName)
}

func readScrollResult(p ScrollProposal, actor PlayerState) ScrollResult {
	result := ScrollResult{
		Action:        p.Action,
		Response:      p.Response,
		Broadcast:     p.Broadcast,
		Changed:       p.Consumed || p.MovedToRoom || p.ClearHidden,
		NoOp:          p.NoOp,
		Cooldown:      p.Cooldown,
		WaitSeconds:   p.WaitSeconds,
		Attempted:     p.Attempted,
		Succeeded:     p.Succeeded,
		SpellFailed:   p.SpellFailed,
		Consumed:      p.Consumed,
		MovedToRoom:   p.MovedToRoom,
		ClearHidden:   p.ClearHidden,
		ItemID:        p.ItemID,
		ItemName:      p.ItemName,
		Occurrence:    p.Occurrence,
		Location:      p.Location,
		ReadySlot:     p.ReadySlot,
		MagicPower:    p.MagicPower,
		SpellIndex:    p.SpellIndex,
		SpellName:     p.SpellName,
		Effect:        p.Effect,
		Chance:        p.Chance,
		Roll:          p.Roll,
		SpellFailRoll: p.SpellFailRoll,
		Rolls:         append([]int(nil), p.Rolls...),
		HPDelta:       p.HPDelta,
		MPDelta:       p.MPDelta,
		ShotsBefore:   p.ShotsBefore,
		ShotsAfter:    p.ShotsAfter,
		RoomID:        p.RoomID,
		ActorID:       p.ActorID,
		ActorName:     actor.Body.Name,
		EventText:     p.RoomText,
	}
	if p.Broadcast {
		event := ScrollEvent{
			RoomID: p.RoomID, ActorID: p.ActorID, ActorName: actor.Body.Name,
			ItemID: p.ItemID, ItemName: p.ItemName, SpellName: p.SpellName,
			ExcludeActorID: p.ActorID, Text: p.RoomText,
		}
		result.Event = &event
	}
	return result
}

func readScrollEffectDrawCount(effect string) int {
	switch effect {
	case "vigor", "fear", "silence", "charm":
		return 1
	case "mend", "confuse":
		return 2
	case "restore-mana":
		return 3
	default:
		return 0
	}
}

// PlanReadScroll resolves and evaluates one canonical direct-root scroll.
// No source state is mutated.  Every Roll call is recorded in p.Rolls in C's
// order: spell_fail first, then any self-effect draws.
func (s State) PlanReadScroll(actorID, itemName string, occurrence int, options ScrollOptions) (ScrollProposal, error) {
	if options.Now < 0 {
		return ScrollProposal{}, fmt.Errorf("read-scroll clock must be nonnegative")
	}
	actor, room, err := readScrollActor(s, actorID)
	if err != nil {
		return ScrollProposal{}, err
	}
	if flag(actor.Body.Flags[:], readScrollBlindFlag) {
		return ScrollProposal{}, ErrReadScrollBlind
	}
	itemID, item, location, readySlot, err := selectReadScrollRoot(actor, itemName, occurrence)
	if err != nil {
		return ScrollProposal{}, err
	}
	if item.Object.Special != 0 || flag(item.Object.Flags[:], readScrollSpecial) {
		return ScrollProposal{}, fmt.Errorf("%w: special/map object", ErrReadScrollUnsupported)
	}
	if item.Object.Type != readScrollType {
		return ScrollProposal{}, fmt.Errorf("%w: %s", ErrReadScrollNotScroll, item.Object.Name)
	}
	if item.Object.ShotsCurrent < 1 {
		return ScrollProposal{}, fmt.Errorf("%w: %s", ErrReadScrollEmpty, item.Object.Name)
	}
	if item.Object.DiceCount < 0 || int(item.Object.DiceCount) > int(actor.Body.Level) {
		return ScrollProposal{}, fmt.Errorf("%w: %s", ErrReadScrollLevel, item.Object.Name)
	}
	index, spellName, spec, err := readScrollSpell(item.Object)
	if err != nil {
		return ScrollProposal{}, err
	}
	p := ScrollProposal{
		Action:        "read_scroll",
		ActorID:       actorID,
		RoomID:        actor.Body.RoomID,
		ItemID:        itemID,
		ItemName:      item.Object.Name,
		Occurrence:    occurrence,
		Location:      location,
		ReadySlot:     readySlot,
		Now:           options.Now,
		MagicPower:    item.Object.MagicPower,
		SpellIndex:    index,
		SpellName:     spellName,
		ShotsBefore:   item.Object.ShotsCurrent,
		ShotsAfter:    0,
		expectedActor: cloneDrinkActor(actor),
		expectedRoom:  cloneRoomState(room),
		expectedItem:  item,
		expectedTimer: actor.Body.Timers[readScrollTimerIndex],
	}
	if readScrollAlignmentRejected(actor.Body, item.Object) {
		if room.Items == nil {
			return ScrollProposal{}, fmt.Errorf("%w: canonical room floor required", ErrReadScrollUnsupported)
		}
		p.MovedToRoom = true
		p.Response = readScrollBurnResponse(item.Object.Name)
		return p, nil
	}
	if !readScrollClassAllowed(actor.Body, item.Object) {
		return ScrollProposal{}, fmt.Errorf("%w: %s", ErrReadScrollClass, item.Object.Name)
	}
	if flag(room.Resource.Flags[:], readScrollNoMagic) {
		return ScrollProposal{}, ErrReadScrollNoMagicRoom
	}
	timer := actor.Body.Timers[readScrollTimerIndex]
	if timer.LastTime < 0 || timer.Interval < 0 {
		return ScrollProposal{}, fmt.Errorf("read-scroll timer outside legacy range")
	}
	deadline := int64(timer.LastTime) + int64(timer.Interval)
	if int64(options.Now) < deadline {
		wait, err := readScrollWaitResponse(deadline - int64(options.Now))
		if err != nil {
			return ScrollProposal{}, err
		}
		p.Cooldown, p.NoOp, p.WaitSeconds, p.Response = true, true, int32(deadline-int64(options.Now)), wait
		return p, nil
	}

	chance, err := readScrollSpellChance(actor.Body)
	if err != nil {
		return ScrollProposal{}, err
	}
	p.Chance = chance
	p.Roll, err = readScrollRoll(options, 1, 100, &p.Rolls)
	if err != nil {
		return ScrollProposal{}, err
	}
	p.SpellFailRoll = p.Roll
	p.Attempted, p.Consumed, p.ClearHidden = true, true, true
	p.SpellFailed = p.Roll > chance
	p.Succeeded = !p.SpellFailed
	p.ShotsAfter = 0
	p.afterBody = actor.Body
	if err := readScrollSetTimer(&p.afterBody, options.Now); err != nil {
		return ScrollProposal{}, err
	}
	setSettingFlag(&p.afterBody, readScrollHiddenFlag, false)
	if p.SpellFailed {
		p.Response = readScrollBurnResponse(item.Object.Name)
		return p, nil
	}
	after, effect, hpDelta, mpDelta, effectRolls, err := applyDrinkSpell(actor.Body, spec, options.Now, DrinkOptions{Now: options.Now, Roll: options.Roll}, nil)
	if err != nil {
		return ScrollProposal{}, fmt.Errorf("%w: %v", ErrReadScrollUnsupported, err)
	}
	p.Rolls = append(p.Rolls, effectRolls...)
	p.Effect, p.HPDelta, p.MPDelta = effect, hpDelta, mpDelta
	if err := readScrollSetTimer(&after, options.Now); err != nil {
		return ScrollProposal{}, err
	}
	setSettingFlag(&after, readScrollHiddenFlag, false)
	p.afterBody = after
	p.Succeeded, p.Broadcast = true, true
	p.Response, err = readScrollSuccessResponse(item.Object, item.Object.Name)
	if err != nil {
		return ScrollProposal{}, err
	}
	p.RoomText = readScrollRoomText(actor.Body.Name, item.Object.Name)
	return p, nil
}

// ApplyReadScroll atomically commits an alignment move, a successful
// self-target effect, or C's spell-failure consumption.  It replays stored
// draws only; no random source is consulted here.
func (s State) ApplyReadScroll(p ScrollProposal) (State, ScrollResult, error) {
	actor, room, err := readScrollActor(s, p.ActorID)
	if err != nil {
		return State{}, ScrollResult{}, err
	}
	if p.Action != "read_scroll" || p.ActorID == "" || p.RoomID != actor.Body.RoomID || p.ItemID == "" || !validReadScrollText(p.ItemName, false) || p.Occurrence < 1 || p.Now < 0 || p.Response == "" {
		return State{}, ScrollResult{}, fmt.Errorf("invalid read-scroll proposal")
	}
	if !reflect.DeepEqual(actor, p.expectedActor) || !reflect.DeepEqual(room, p.expectedRoom) {
		return State{}, ScrollResult{}, fmt.Errorf("stale read-scroll actor or room")
	}
	if actor.Body.Timers[readScrollTimerIndex] != p.expectedTimer {
		return State{}, ScrollResult{}, fmt.Errorf("stale read-scroll timer")
	}
	id, item, location, readySlot, err := selectReadScrollRoot(actor, p.ItemName, p.Occurrence)
	if err != nil || id != p.ItemID || location != p.Location || readySlot != p.ReadySlot || !reflect.DeepEqual(item, p.expectedItem) {
		return State{}, ScrollResult{}, fmt.Errorf("stale read-scroll item")
	}
	if item.Object.Special != 0 || flag(item.Object.Flags[:], readScrollSpecial) {
		return State{}, ScrollResult{}, fmt.Errorf("%w: special/map object", ErrReadScrollUnsupported)
	}
	if item.Object.Type != readScrollType || item.Object.ShotsCurrent < 1 || item.Object.DiceCount < 0 || int(item.Object.DiceCount) > int(actor.Body.Level) {
		return State{}, ScrollResult{}, fmt.Errorf("stale read-scroll item admission")
	}
	if p.MagicPower != item.Object.MagicPower || p.ShotsBefore != item.Object.ShotsCurrent || p.ShotsAfter != 0 {
		return State{}, ScrollResult{}, fmt.Errorf("stale read-scroll item metadata")
	}
	index, spellName, spec, err := readScrollSpell(item.Object)
	if err != nil || p.SpellIndex != index || p.SpellName != spellName {
		return State{}, ScrollResult{}, fmt.Errorf("stale read-scroll spell mapping")
	}

	if p.Cooldown {
		if !p.NoOp || p.Attempted || p.Succeeded || p.SpellFailed || p.Consumed || p.MovedToRoom || p.ClearHidden || p.Broadcast || p.Roll != 0 || p.SpellFailRoll != 0 || len(p.Rolls) != 0 || p.Chance != 0 || p.Effect != "" || p.HPDelta != 0 || p.MPDelta != 0 || p.RoomText != "" {
			return State{}, ScrollResult{}, fmt.Errorf("invalid read-scroll cooldown proposal")
		}
		deadline := int64(p.expectedTimer.LastTime) + int64(p.expectedTimer.Interval)
		if int64(p.Now) >= deadline {
			return State{}, ScrollResult{}, fmt.Errorf("stale read-scroll cooldown")
		}
		wait := deadline - int64(p.Now)
		response, waitErr := readScrollWaitResponse(wait)
		if waitErr != nil || p.WaitSeconds != int32(wait) || p.Response != response {
			return State{}, ScrollResult{}, fmt.Errorf("stale read-scroll cooldown response")
		}
		return s.clone(), readScrollResult(p, actor), nil
	}
	if p.NoOp || p.WaitSeconds != 0 || p.MovedToRoom && (p.Attempted || p.Consumed || p.ClearHidden || p.Broadcast || p.Succeeded || p.SpellFailed || p.Roll != 0 || p.SpellFailRoll != 0 || len(p.Rolls) != 0 || p.Chance != 0 || p.Effect != "" || p.HPDelta != 0 || p.MPDelta != 0 || p.RoomText != "") {
		return State{}, ScrollResult{}, fmt.Errorf("invalid read-scroll outcome proposal")
	}
	if p.MovedToRoom {
		if p.Response != readScrollBurnResponse(item.Object.Name) || !readScrollAlignmentRejected(actor.Body, item.Object) || p.Location != ScrollInventoryRoot && p.Location != ScrollReadySlot {
			return State{}, ScrollResult{}, fmt.Errorf("invalid read-scroll alignment proposal")
		}
		if room.Items == nil {
			return State{}, ScrollResult{}, fmt.Errorf("read-scroll room transfer unavailable")
		}
		next := s.clone()
		nextActor := next.Players[p.ActorID]
		nextRoom := next.Rooms[p.RoomID]
		if err := moveDrinkRoot(nextActor.Items, nextRoom.Items, DrinkLocation(p.Location), p.ReadySlot, p.ItemID); err != nil {
			return State{}, ScrollResult{}, err
		}
		next.Players[p.ActorID] = nextActor
		next.Rooms[p.RoomID] = nextRoom
		if err := next.Validate(); err != nil {
			return State{}, ScrollResult{}, err
		}
		return next, readScrollResult(p, nextActor), nil
	}

	if !readScrollClassAllowed(actor.Body, item.Object) || flag(room.Resource.Flags[:], readScrollNoMagic) || readScrollAlignmentRejected(actor.Body, item.Object) || !p.Attempted || !p.Consumed || !p.ClearHidden || p.SpellIndex < 0 || p.SpellName == "" || p.Roll < 1 || p.Roll > 100 || p.SpellFailRoll != p.Roll || len(p.Rolls) < 1 || p.Rolls[0] != p.Roll || p.Broadcast != p.Succeeded || p.SpellFailed == p.Succeeded {
		return State{}, ScrollResult{}, fmt.Errorf("invalid read-scroll spell proposal")
	}
	chance, err := readScrollSpellChance(actor.Body)
	if err != nil || p.Chance != chance || p.Succeeded != (p.Roll <= chance) {
		return State{}, ScrollResult{}, fmt.Errorf("stale read-scroll spell-failure outcome")
	}
	if p.SpellFailed {
		expectedAfter := actor.Body
		if err := readScrollSetTimer(&expectedAfter, p.Now); err != nil {
			return State{}, ScrollResult{}, err
		}
		setSettingFlag(&expectedAfter, readScrollHiddenFlag, false)
		if len(p.Rolls) != 1 || p.Effect != "" || p.HPDelta != 0 || p.MPDelta != 0 || p.RoomText != "" || p.Response != readScrollBurnResponse(item.Object.Name) || !reflect.DeepEqual(p.afterBody, expectedAfter) {
			return State{}, ScrollResult{}, fmt.Errorf("invalid read-scroll spell-failure proposal")
		}
	} else {
		if p.Response == "" || !p.Broadcast || p.RoomText != readScrollRoomText(actor.Body.Name, item.Object.Name) || p.Effect == "" || len(p.Rolls) != 1+readScrollEffectDrawCount(spec.effect) {
			return State{}, ScrollResult{}, fmt.Errorf("invalid read-scroll success proposal")
		}
		after, effect, hpDelta, mpDelta, _, applyErr := applyDrinkSpell(actor.Body, spec, p.Now, DrinkOptions{}, p.Rolls[1:])
		if applyErr != nil || effect != p.Effect || hpDelta != p.HPDelta || mpDelta != p.MPDelta {
			return State{}, ScrollResult{}, fmt.Errorf("stale read-scroll spell effect")
		}
		if err := readScrollSetTimer(&after, p.Now); err != nil {
			return State{}, ScrollResult{}, err
		}
		setSettingFlag(&after, readScrollHiddenFlag, false)
		if !reflect.DeepEqual(after, p.afterBody) {
			return State{}, ScrollResult{}, fmt.Errorf("stale read-scroll body effect")
		}
		wantResponse, responseErr := readScrollSuccessResponse(item.Object, item.Object.Name)
		if responseErr != nil || p.Response != wantResponse {
			return State{}, ScrollResult{}, fmt.Errorf("stale read-scroll response")
		}
	}

	next := s.clone()
	nextActor := next.Players[p.ActorID]
	if p.Succeeded {
		after, _, _, _, _, applyErr := applyDrinkSpell(nextActor.Body, spec, p.Now, DrinkOptions{}, p.Rolls[1:])
		if applyErr != nil {
			return State{}, ScrollResult{}, fmt.Errorf("stale read-scroll commit effect")
		}
		if err := readScrollSetTimer(&after, p.Now); err != nil {
			return State{}, ScrollResult{}, err
		}
		setSettingFlag(&after, readScrollHiddenFlag, false)
		nextActor.Body = after
	} else {
		after := nextActor.Body
		if err := readScrollSetTimer(&after, p.Now); err != nil {
			return State{}, ScrollResult{}, err
		}
		setSettingFlag(&after, readScrollHiddenFlag, false)
		nextActor.Body = after
	}
	if err := removeDrinkRoot(nextActor.Items, DrinkLocation(p.Location), p.ReadySlot, p.ItemID); err != nil {
		return State{}, ScrollResult{}, err
	}
	next.Players[p.ActorID] = nextActor
	if err := next.Validate(); err != nil {
		return State{}, ScrollResult{}, err
	}
	return next, readScrollResult(p, nextActor), nil
}

// ReadScroll is the one-call pure reducer boundary used by command adapters.
func (s State) ReadScroll(actorID, itemName string, occurrence int, options ScrollOptions) (State, ScrollResult, error) {
	p, err := s.PlanReadScroll(actorID, itemName, occurrence, options)
	if err != nil {
		return State{}, ScrollResult{}, err
	}
	return s.ApplyReadScroll(p)
}
