package world

// This file is the bounded Go port of magic1.c:zap / zap_obj. Admission
// follows C order. Only the already-canonical vigor wand effect is applied;
// offensive, special, dice, and other catalog entries stay fail-closed.

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
	zapWandType      = 8  // WAND
	zapGoodOnly      = 12 // OGOODO
	zapEvilOnly      = 13 // OEVILO
	zapOddDice       = 28 // ODDICE
	zapClassSelect   = 31 // OCLSEL
	zapSpecialFlag   = 44 // OSPECI
	zapNoMagicRoom   = 17 // RNOMAG
	zapBlindFlag     = 42 // PBLIND
	zapHiddenFlag    = 1  // PHIDDN
	zapSpellTimer    = 9  // LT_SPELL
	zapCaretaker     = 10 // CARETAKER
	zapMaxClass      = 12 // DM
	zapVigorIndex    = 0  // spllist[0] 회복
	zapSpellInterval = 3
)

const (
	ZapWandType    = zapWandType
	ZapGoodOnly    = zapGoodOnly
	ZapEvilOnly    = zapEvilOnly
	ZapClassSelect = zapClassSelect
	ZapNoMagicRoom = zapNoMagicRoom
	ZapBlindFlag   = zapBlindFlag
	ZapHiddenFlag  = zapHiddenFlag
	ZapSpellTimer  = zapSpellTimer
)

type ZapAction string

const (
	ZapUsage         ZapAction = "usage"
	ZapBlind         ZapAction = "blind"
	ZapMissing       ZapAction = "missing"
	ZapNotWand       ZapAction = "not_wand"
	ZapEmpty         ZapAction = "empty"
	ZapEvaporate     ZapAction = "evaporate"
	ZapClass         ZapAction = "class"
	ZapNoOp          ZapAction = "noop"
	ZapCooldown      ZapAction = "cooldown"
	ZapMissingTarget ZapAction = "missing_target"
	ZapSpellFail     ZapAction = "spell_fail"
	ZapVigorSelf     ZapAction = "vigor_self"
	ZapVigorTarget   ZapAction = "vigor_target"
)

const (
	ZapUsageResponse         = "\n무엇을 사용합니까?\n"
	ZapBlindResponse         = "아무 것도 보이지 않습니다!\n"
	ZapMissingResponse       = "\n그런것이 존재하지 않습니다.\n"
	ZapNotWandResponse       = "\n막대나 지팡이가 아닙니다.\n"
	ZapEmptyResponse         = "\n모두 써버렸습니다.\n"
	ZapClassResponse         = "\n당신직업세계에서 금하는 물건이기에 사용할 수 없습니다.\n"
	ZapNoOpResponse          = "\n아무런 일도 일어나지 않습니다.\n"
	ZapSpellFailResponse     = "당신의 주문이 실패했습니다."
	ZapMissingTargetResponse = "그런 사람은 존재하지 않습니다.\n"
	ZapVigorSelfResponse     = "당신의 체력이 향상되었습니다.\n"
)

type ZapLocation string

const (
	ZapInventoryRoot ZapLocation = "inventory"
	ZapReadySlot     ZapLocation = "ready"
)

type ZapTargetKind string

const (
	ZapTargetNone   ZapTargetKind = ""
	ZapTargetPlayer ZapTargetKind = "player"
	ZapTargetNPC    ZapTargetKind = "npc"
)

type ZapOptions struct {
	Now  int32
	Roll func(int, int) int
}

type ZapEvent struct {
	RoomID         int16  `json:"room_id"`
	ActorID        string `json:"actor_id"`
	ActorName      string `json:"actor_name"`
	TargetID       string `json:"target_id,omitempty"`
	ExcludeActorID string `json:"exclude_actor_id,omitempty"`
	ExcludeTarget  bool   `json:"exclude_target,omitempty"`
	Text           string `json:"text"`
}

type ZapResult struct {
	Action       ZapAction     `json:"action"`
	Response     string        `json:"response"`
	Changed      bool          `json:"changed"`
	ItemID       string        `json:"item_id,omitempty"`
	ItemName     string        `json:"item_name,omitempty"`
	Occurrence   int           `json:"occurrence,omitempty"`
	Location     ZapLocation   `json:"location,omitempty"`
	ReadySlot    int           `json:"ready_slot,omitempty"`
	TargetKind   ZapTargetKind `json:"target_kind,omitempty"`
	TargetID     string        `json:"target_id,omitempty"`
	TargetName   string        `json:"target_name,omitempty"`
	TargetOcc    int           `json:"target_occurrence,omitempty"`
	SpellIndex   int           `json:"spell_index,omitempty"`
	SpellName    string        `json:"spell_name,omitempty"`
	Chance       int           `json:"chance,omitempty"`
	Roll         int           `json:"roll,omitempty"`
	HealRoll     int           `json:"heal_roll,omitempty"`
	Rolls        []int         `json:"rolls,omitempty"`
	HPDelta      int32         `json:"hp_delta,omitempty"`
	ShotsBefore  int16         `json:"shots_before,omitempty"`
	ShotsAfter   int16         `json:"shots_after,omitempty"`
	WaitSeconds  int32         `json:"wait_seconds,omitempty"`
	RoomID       int16         `json:"room_id"`
	ActorID      string        `json:"actor_id"`
	ActorName    string        `json:"actor_name,omitempty"`
	Events       []ZapEvent    `json:"events,omitempty"`
	SpellFailed  bool          `json:"spell_failed,omitempty"`
	MovedToRoom  bool          `json:"moved_to_room,omitempty"`
	ClearHidden  bool          `json:"clear_hidden,omitempty"`
	ConsumedShot bool          `json:"consumed_shot,omitempty"`
}

type ZapProposal struct {
	Action       ZapAction
	ActorID      string
	RoomID       int16
	ItemID       string
	ItemName     string
	Occurrence   int
	Location     ZapLocation
	ReadySlot    int
	TargetKind   ZapTargetKind
	TargetID     string
	TargetName   string
	TargetOcc    int
	Now          int32
	SpellIndex   int
	SpellName    string
	Chance       int
	Roll         int
	HealRoll     int
	Rolls        []int
	HPDelta      int32
	ShotsBefore  int16
	ShotsAfter   int16
	WaitSeconds  int32
	Response     string
	Changed      bool
	SpellFailed  bool
	MovedToRoom  bool
	ClearHidden  bool
	ConsumedShot bool
	Events       []ZapEvent

	expectedActor        PlayerState
	expectedRoom         RoomState
	expectedItem         Item
	expectedTimer        LegacyTimer
	afterBody            LegacyMonster
	expectedTargetPlayer PlayerState
	expectedTargetNPC    NPCState
	afterTargetPlayer    LegacyMonster
	afterTargetNPC       LegacyMonster
}

var (
	ErrZapActorAbsent        = errors.New("online canonical zap actor with inventory required")
	ErrZapItemNameRequired   = errors.New("zap item name required")
	ErrZapInvalidOccurrence  = errors.New("invalid zap occurrence")
	ErrZapTargetNameRequired = errors.New("zap target name required")
	ErrZapNPCUnresolved      = errors.New("canonical NPC state required")
	ErrZapSpellUnavailable   = errors.New("zap spell catalog entry unavailable")
	ErrZapRandom             = errors.New("zap random source unavailable")
	ErrZapRoomItemsRequired  = errors.New("canonical room floor required")
	ErrZapStaleProposal      = errors.New("stale or invalid zap proposal")
	ErrZapInvalidProposal    = errors.New("invalid zap proposal")
)

func ZapEvaporateResponse(itemName string) string {
	return fmt.Sprintf("\n%s의 수명이 다한 듯 수증기처럼 증발해 버렸습니다.\n", itemName)
}

func ZapVigorTargetResponse(targetName string) string {
	return fmt.Sprintf("당신은 합장을 하고서 %s의 회복을 기원하는 주문을 외웁니다.\n빛의 정기가 그의 몸으로 스며들고 있습니다.\n", targetName)
}

func ZapVigorTargetEvents(roomID int16, actorID, actorName, targetID, targetName string, targetPlayer bool) []ZapEvent {
	events := []ZapEvent{}
	if targetPlayer {
		events = append(events, ZapEvent{
			RoomID: roomID, ActorID: actorID, ActorName: actorName, TargetID: targetID,
			Text: fmt.Sprintf("%s이 합장을 하고서 당신의 회복을 기원하는 주문을 외웁니다.\n빛의 정기가 당신의 몸에 스며들면서 체력이 향상되었습니다.\n", actorName),
		})
	}
	events = append(events, ZapEvent{
		RoomID: roomID, ActorID: actorID, ActorName: actorName, TargetID: targetID,
		ExcludeActorID: actorID, ExcludeTarget: true,
		Text: fmt.Sprintf("\n%s이 %s에게 합장을 하고서 회복을 기원하는 주문을 외웁니다.\n빛의 정기가 당신의 몸을 스치며 그의 몸으로 모이는\n것이 느껴집니다.\n", actorName, targetName),
	})
	return events
}

func zapWaitResponse(wait int64) (string, error) {
	if wait < 1 || wait > math.MaxInt32 {
		return "", fmt.Errorf("zap cooldown outside response range")
	}
	if wait == 1 {
		return "1초만 기다리세요.\n", nil
	}
	return fmt.Sprintf("%d초동안 기다리세요.\n", wait), nil
}

func validZapText(value string) bool {
	if value == "" || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' || (unicode.IsSpace(r) && r != ' ') {
			return false
		}
	}
	return true
}

func zapActor(s State, actorID string) (PlayerState, RoomState, error) {
	if err := s.Validate(); err != nil {
		return PlayerState{}, RoomState{}, err
	}
	actor, ok := s.Players[actorID]
	if actorID == "" || !ok || !actor.Online || actor.Body.Type != 0 || !validZapText(actor.Body.Name) || actor.Items == nil {
		return PlayerState{}, RoomState{}, ErrZapActorAbsent
	}
	if actor.Body.Class > zapMaxClass {
		return PlayerState{}, RoomState{}, fmt.Errorf("zap actor class outside legacy table")
	}
	if actor.Body.Stats[3] > 63 {
		return PlayerState{}, RoomState{}, fmt.Errorf("zap actor intelligence outside legacy table")
	}
	if len(actor.Body.Inventory) != 0 {
		return PlayerState{}, RoomState{}, ErrZapActorAbsent
	}
	if err := actor.Items.Validate(); err != nil {
		return PlayerState{}, RoomState{}, err
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok || room.Resource.ID != actor.Body.RoomID || !containsString(room.PlayerIDs, actorID) {
		return PlayerState{}, RoomState{}, ErrZapActorAbsent
	}
	if room.Items != nil {
		if err := room.Items.Validate(); err != nil {
			return PlayerState{}, RoomState{}, err
		}
	}
	return actor, room, nil
}

func selectZapRoot(actor PlayerState, name string, occurrence int) (string, Item, ZapLocation, int, error) {
	if occurrence < 1 {
		return "", Item{}, "", -1, ErrZapInvalidOccurrence
	}
	if !validZapText(name) {
		return "", Item{}, "", -1, ErrZapItemNameRequired
	}
	if actor.Items == nil {
		return "", Item{}, "", -1, ErrZapActorAbsent
	}
	found := 0
	for _, id := range actor.Items.Inventory {
		item, ok := actor.Items.Items[id]
		if !ok {
			return "", Item{}, "", -1, fmt.Errorf("canonical zap inventory root absent")
		}
		if !equalInventorySelector(item.Object, name) || !inventoryObjectVisible(item.Object, flag(actor.Body.Flags[:], playerDetectInvisibleFlag)) {
			continue
		}
		found++
		if found == occurrence {
			return id, item, ZapInventoryRoot, -1, nil
		}
	}
	// C find_obj owns its own match counter. magic1.c therefore starts the
	// Ready fallback at occurrence one after direct Inventory lookup fails;
	// Ready slots remain in ascending slot order and are root-only selectors.
	found = 0
	for slot, id := range actor.Items.Ready {
		if id == "" {
			continue
		}
		item, ok := actor.Items.Items[id]
		if !ok {
			return "", Item{}, "", -1, fmt.Errorf("canonical zap ready root absent")
		}
		if !equalInventorySelector(item.Object, name) || !inventoryObjectVisible(item.Object, flag(actor.Body.Flags[:], playerDetectInvisibleFlag)) {
			continue
		}
		found++
		if found == occurrence {
			return id, item, ZapReadySlot, slot, nil
		}
	}
	return "", Item{}, "", -1, nil
}

func zapAlignmentRejected(actor LegacyMonster, object LegacyObject) bool {
	return (flag(object.Flags[:], zapGoodOnly) && actor.Alignment < -100) ||
		(flag(object.Flags[:], zapEvilOnly) && actor.Alignment > 100)
}

func zapClassAllowed(actor LegacyMonster, object LegacyObject) bool {
	if !flag(object.Flags[:], zapClassSelect) || actor.Class >= zapCaretaker {
		return true
	}
	return flag(object.Flags[:], uint(zapClassSelect)+uint(actor.Class))
}

func zapRoll(options ZapOptions, low, high int, draws *[]int) (int, error) {
	if options.Roll == nil {
		return 0, ErrZapRandom
	}
	n := options.Roll(low, high)
	if n < low || n > high {
		return 0, fmt.Errorf("zap roll outside %d..%d", low, high)
	}
	*draws = append(*draws, n)
	return n, nil
}

func zapSetTimer(body *LegacyMonster, now int32) error {
	if now < 0 || zapSpellTimer < 0 || zapSpellTimer >= len(body.Timers) {
		return fmt.Errorf("zap timer outside legacy range")
	}
	body.Timers[zapSpellTimer] = LegacyTimer{LastTime: now, Interval: zapSpellInterval}
	return nil
}

func zapVigorSecondSpellFail(class byte) bool {
	// magic2.c:vigor — BARBARIAN||FIGHTER && how!=POTION, after zap()'s spell_fail.
	return class == 2 || class == 4
}

func zapVigorFailDraws(class byte) int {
	if zapVigorSecondSpellFail(class) {
		return 2
	}
	return 1
}

func zapVigorSecondSucceeded(rolls []int, chance int, class byte) bool {
	if !zapVigorSecondSpellFail(class) {
		return true
	}
	if len(rolls) < 2 {
		return false
	}
	n := rolls[1]
	return n >= 1 && n <= 100 && n <= chance
}

func zapVigorHeal(current, max int16, roll int) (int16, int32) {
	heal := roll
	if heal < 1 {
		heal = 1
	}
	next := int64(current) + int64(heal)
	if max > 0 && next > int64(max) {
		next = int64(max)
	}
	if next > math.MaxInt16 {
		next = math.MaxInt16
	}
	if next < math.MinInt16 {
		next = math.MinInt16
	}
	return int16(next), int32(int16(next) - current)
}

func zapSuccessResponse(object LegacyObject, body string) string {
	if object.UseOutput == "" {
		return body
	}
	out := object.UseOutput
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out + body
}

func (s State) selectZapTarget(actor PlayerState, name string, occurrence int) (ZapTargetKind, string, PlayerState, NPCState, bool, error) {
	if occurrence < 1 {
		return "", "", PlayerState{}, NPCState{}, false, ErrZapInvalidOccurrence
	}
	if !validZapText(name) {
		return "", "", PlayerState{}, NPCState{}, false, ErrZapTargetNameRequired
	}
	found := 0
	for _, id := range s.Rooms[actor.Body.RoomID].PlayerIDs {
		player, ok := s.Players[id]
		if !ok || !player.Online || player.Body.Type != 0 || player.Body.RoomID != actor.Body.RoomID || !validZapText(player.Body.Name) {
			continue
		}
		if !strings.EqualFold(player.Body.Name, name) {
			continue
		}
		found++
		if found == occurrence {
			return ZapTargetPlayer, id, player, NPCState{}, true, nil
		}
	}
	if s.NPCs == nil {
		return "", "", PlayerState{}, NPCState{}, false, ErrZapNPCUnresolved
	}
	found = 0
	for _, id := range s.Rooms[actor.Body.RoomID].NPCIDs {
		npc, ok := s.NPCs[id]
		if !ok || id == "" || npc.Body.Type != 1 || npc.Body.RoomID != actor.Body.RoomID {
			return "", "", PlayerState{}, NPCState{}, false, ErrZapNPCUnresolved
		}
		if !strings.EqualFold(npc.Body.Name, name) {
			continue
		}
		found++
		if found == occurrence {
			return ZapTargetNPC, id, PlayerState{}, npc, true, nil
		}
	}
	return "", "", PlayerState{}, NPCState{}, false, nil
}

func zapUnchanged(action ZapAction, actorID string, actor PlayerState, room RoomState, response string) ZapProposal {
	return ZapProposal{
		Action: action, ActorID: actorID, RoomID: actor.Body.RoomID, Response: response, Changed: false,
		expectedActor: cloneDrinkActor(actor), expectedRoom: cloneRoomState(room),
	}
}

func zapResult(p ZapProposal, actor PlayerState) ZapResult {
	return ZapResult{
		Action: p.Action, Response: p.Response, Changed: p.Changed, ItemID: p.ItemID, ItemName: p.ItemName,
		Occurrence: p.Occurrence, Location: p.Location, ReadySlot: p.ReadySlot, TargetKind: p.TargetKind,
		TargetID: p.TargetID, TargetName: p.TargetName, TargetOcc: p.TargetOcc, SpellIndex: p.SpellIndex,
		SpellName: p.SpellName, Chance: p.Chance, Roll: p.Roll, HealRoll: p.HealRoll,
		Rolls: append([]int(nil), p.Rolls...), HPDelta: p.HPDelta, ShotsBefore: p.ShotsBefore,
		ShotsAfter: p.ShotsAfter, WaitSeconds: p.WaitSeconds, RoomID: p.RoomID, ActorID: p.ActorID,
		ActorName: actor.Body.Name, Events: append([]ZapEvent(nil), p.Events...),
		SpellFailed: p.SpellFailed, MovedToRoom: p.MovedToRoom, ClearHidden: p.ClearHidden,
		ConsumedShot: p.ConsumedShot,
	}
}

func zapVigorMapped(object LegacyObject) (int, string, error) {
	if object.Special != 0 || flag(object.Flags[:], zapSpecialFlag) || flag(object.Flags[:], zapOddDice) {
		return 0, "", ErrZapSpellUnavailable
	}
	if object.MagicPower < 1 {
		return 0, "", ErrZapSpellUnavailable
	}
	index := int(object.MagicPower) - 1
	if index != zapVigorIndex || index >= len(legacyInfoSpellNames) || legacyInfoSpellNames[index] == "" {
		return 0, "", ErrZapSpellUnavailable
	}
	return index, legacyInfoSpellNames[index], nil
}

func (s State) PlanZap(actorID, itemName string, occurrence int, targetName string, targetOcc int, options ZapOptions) (ZapProposal, error) {
	if options.Now < 0 {
		return ZapProposal{}, fmt.Errorf("zap clock must be nonnegative")
	}
	actor, room, err := zapActor(s, actorID)
	if err != nil {
		return ZapProposal{}, err
	}
	if itemName == "" {
		if targetName != "" {
			return ZapProposal{}, ErrZapItemNameRequired
		}
		return zapUnchanged(ZapUsage, actorID, actor, room, ZapUsageResponse), nil
	}
	if occurrence < 1 {
		return ZapProposal{}, ErrZapInvalidOccurrence
	}
	if flag(actor.Body.Flags[:], zapBlindFlag) {
		return zapUnchanged(ZapBlind, actorID, actor, room, ZapBlindResponse), nil
	}
	itemID, item, location, readySlot, err := selectZapRoot(actor, itemName, occurrence)
	if err != nil {
		return ZapProposal{}, err
	}
	if itemID == "" {
		p := zapUnchanged(ZapMissing, actorID, actor, room, ZapMissingResponse)
		p.ItemName, p.Occurrence = itemName, occurrence
		return p, nil
	}
	base := ZapProposal{
		ActorID: actorID, RoomID: actor.Body.RoomID, ItemID: itemID, ItemName: item.Object.Name,
		Occurrence: occurrence, Location: location, ReadySlot: readySlot, Now: options.Now,
		ShotsBefore: item.Object.ShotsCurrent, ShotsAfter: item.Object.ShotsCurrent,
		expectedActor: cloneDrinkActor(actor), expectedRoom: cloneRoomState(room),
		expectedItem: item, expectedTimer: actor.Body.Timers[zapSpellTimer],
	}
	if item.Object.Type != zapWandType {
		base.Action, base.Response = ZapNotWand, ZapNotWandResponse
		return base, nil
	}
	if item.Object.ShotsCurrent < 1 {
		base.Action, base.Response = ZapEmpty, ZapEmptyResponse
		return base, nil
	}
	if zapAlignmentRejected(actor.Body, item.Object) {
		if room.Items == nil {
			return ZapProposal{}, ErrZapRoomItemsRequired
		}
		base.Action, base.Response = ZapEvaporate, ZapEvaporateResponse(item.Object.Name)
		base.Changed, base.MovedToRoom = true, true
		return base, nil
	}
	if !zapClassAllowed(actor.Body, item.Object) {
		base.Action, base.Response = ZapClass, ZapClassResponse
		return base, nil
	}
	if flag(room.Resource.Flags[:], zapNoMagicRoom) || item.Object.MagicPower < 1 {
		base.Action, base.Response = ZapNoOp, ZapNoOpResponse
		return base, nil
	}
	timer := actor.Body.Timers[zapSpellTimer]
	if timer.LastTime < 0 || timer.Interval < 0 {
		return ZapProposal{}, fmt.Errorf("zap timer outside legacy range")
	}
	deadline := int64(timer.LastTime) + int64(timer.Interval)
	if int64(options.Now) < deadline {
		wait := deadline - int64(options.Now)
		response, waitErr := zapWaitResponse(wait)
		if waitErr != nil {
			return ZapProposal{}, waitErr
		}
		base.Action, base.Response, base.WaitSeconds = ZapCooldown, response, int32(wait)
		return base, nil
	}
	index, spellName, err := zapVigorMapped(item.Object)
	if err != nil {
		return ZapProposal{}, err
	}
	chance, err := readScrollSpellChance(actor.Body)
	if err != nil {
		return ZapProposal{}, err
	}
	base.SpellIndex, base.SpellName, base.Chance = index, spellName, chance
	base.Roll, err = zapRoll(options, 1, 100, &base.Rolls)
	if err != nil {
		return ZapProposal{}, err
	}
	after := actor.Body
	if err := zapSetTimer(&after, options.Now); err != nil {
		return ZapProposal{}, err
	}
	setSettingFlag(&after, zapHiddenFlag, false)
	base.afterBody = after
	base.Changed, base.ClearHidden = true, true
	if base.Roll > chance {
		base.Action, base.Response = ZapSpellFail, ZapSpellFailResponse
		base.SpellFailed, base.ConsumedShot = true, true
		base.ShotsAfter = item.Object.ShotsCurrent - 1
		return base, nil
	}
	if zapVigorSecondSpellFail(actor.Body.Class) {
		second, secondErr := zapRoll(options, 1, 100, &base.Rolls)
		if secondErr != nil {
			return ZapProposal{}, secondErr
		}
		if second > chance {
			base.Action, base.Response = ZapSpellFail, ZapSpellFailResponse
			base.SpellFailed = true
			return base, nil
		}
	}
	if targetName != "" {
		if targetOcc < 1 {
			return ZapProposal{}, ErrZapInvalidOccurrence
		}
		kind, id, player, npc, found, targetErr := s.selectZapTarget(actor, targetName, targetOcc)
		if targetErr != nil {
			return ZapProposal{}, targetErr
		}
		base.TargetName, base.TargetOcc = targetName, targetOcc
		if !found {
			base.Action, base.Response = ZapMissingTarget, ZapMissingTargetResponse
			return base, nil
		}
		heal, err := zapRoll(options, 1, 6, &base.Rolls)
		if err != nil {
			return ZapProposal{}, err
		}
		base.HealRoll = heal
		base.Action = ZapVigorTarget
		base.ConsumedShot = true
		base.ShotsAfter = item.Object.ShotsCurrent - 1
		base.TargetKind, base.TargetID = kind, id
		if kind == ZapTargetPlayer {
			nextHP, delta := zapVigorHeal(player.Body.HPCurrent, player.Body.HPMax, heal)
			targetAfter := player.Body
			targetAfter.HPCurrent = nextHP
			base.HPDelta = delta
			base.TargetName = player.Body.Name
			base.expectedTargetPlayer = player
			base.afterTargetPlayer = targetAfter
			base.Response = zapSuccessResponse(item.Object, ZapVigorTargetResponse(player.Body.Name))
			base.Events = ZapVigorTargetEvents(actor.Body.RoomID, actorID, actor.Body.Name, id, player.Body.Name, true)
		} else {
			nextHP, delta := zapVigorHeal(npc.Body.HPCurrent, npc.Body.HPMax, heal)
			targetAfter := npc.Body
			targetAfter.HPCurrent = nextHP
			base.HPDelta = delta
			base.TargetName = npc.Body.Name
			base.expectedTargetNPC = npc
			base.afterTargetNPC = targetAfter
			base.Response = zapSuccessResponse(item.Object, ZapVigorTargetResponse(npc.Body.Name))
			base.Events = ZapVigorTargetEvents(actor.Body.RoomID, actorID, actor.Body.Name, id, npc.Body.Name, false)
		}
		return base, nil
	}
	heal, err := zapRoll(options, 1, 6, &base.Rolls)
	if err != nil {
		return ZapProposal{}, err
	}
	base.HealRoll = heal
	nextHP, delta := zapVigorHeal(after.HPCurrent, after.HPMax, heal)
	after.HPCurrent = nextHP
	base.afterBody = after
	base.HPDelta = delta
	base.Action = ZapVigorSelf
	base.ConsumedShot = true
	base.ShotsAfter = item.Object.ShotsCurrent - 1
	base.Response = zapSuccessResponse(item.Object, ZapVigorSelfResponse)
	return base, nil
}

func (s State) ApplyZap(p ZapProposal) (State, ZapResult, error) {
	actor, room, err := zapActor(s, p.ActorID)
	if err != nil {
		return State{}, ZapResult{}, err
	}
	if p.ActorID == "" || p.RoomID != actor.Body.RoomID || p.Response == "" || p.Now < 0 {
		return State{}, ZapResult{}, ErrZapInvalidProposal
	}
	if !reflect.DeepEqual(actor, p.expectedActor) || !reflect.DeepEqual(room, p.expectedRoom) {
		return State{}, ZapResult{}, ErrZapStaleProposal
	}
	if !p.Changed {
		switch p.Action {
		case ZapUsage, ZapBlind, ZapMissing, ZapNotWand, ZapEmpty, ZapClass, ZapNoOp, ZapCooldown:
		default:
			return State{}, ZapResult{}, ErrZapInvalidProposal
		}
		if p.MovedToRoom || p.ClearHidden || p.ConsumedShot || p.SpellFailed || len(p.Events) != 0 || len(p.Rolls) != 0 {
			return State{}, ZapResult{}, ErrZapInvalidProposal
		}
		if p.Action == ZapCooldown {
			deadline := int64(p.expectedTimer.LastTime) + int64(p.expectedTimer.Interval)
			if int64(p.Now) >= deadline {
				return State{}, ZapResult{}, ErrZapStaleProposal
			}
			wait := deadline - int64(p.Now)
			response, waitErr := zapWaitResponse(wait)
			if waitErr != nil || p.WaitSeconds != int32(wait) || p.Response != response {
				return State{}, ZapResult{}, ErrZapStaleProposal
			}
		}
		return s.clone(), zapResult(p, actor), nil
	}
	if p.ItemID == "" || p.ItemName == "" || p.Occurrence < 1 {
		return State{}, ZapResult{}, ErrZapInvalidProposal
	}
	id, item, location, readySlot, err := selectZapRoot(actor, p.ItemName, p.Occurrence)
	if err != nil || id != p.ItemID || location != p.Location || readySlot != p.ReadySlot || !reflect.DeepEqual(item, p.expectedItem) {
		return State{}, ZapResult{}, ErrZapStaleProposal
	}
	if actor.Body.Timers[zapSpellTimer] != p.expectedTimer {
		return State{}, ZapResult{}, ErrZapStaleProposal
	}
	next := s.clone()
	nextActor := next.Players[p.ActorID]
	nextRoom := next.Rooms[p.RoomID]
	switch p.Action {
	case ZapEvaporate:
		if !p.MovedToRoom || p.ConsumedShot || p.ClearHidden || p.SpellFailed || p.Response != ZapEvaporateResponse(item.Object.Name) || !zapAlignmentRejected(actor.Body, item.Object) {
			return State{}, ZapResult{}, ErrZapInvalidProposal
		}
		if nextRoom.Items == nil {
			return State{}, ZapResult{}, ErrZapRoomItemsRequired
		}
		if err := moveDrinkRoot(nextActor.Items, nextRoom.Items, DrinkLocation(p.Location), p.ReadySlot, p.ItemID); err != nil {
			return State{}, ZapResult{}, err
		}
		next.Players[p.ActorID] = nextActor
		next.Rooms[p.RoomID] = nextRoom
	case ZapSpellFail, ZapMissingTarget, ZapVigorSelf, ZapVigorTarget:
		if p.MovedToRoom || !p.ClearHidden || p.SpellIndex != zapVigorIndex || p.SpellName != "회복" || p.Roll < 1 || p.Roll > 100 || len(p.Rolls) < 1 || p.Rolls[0] != p.Roll {
			return State{}, ZapResult{}, ErrZapInvalidProposal
		}
		chance, chanceErr := readScrollSpellChance(actor.Body)
		if chanceErr != nil || p.Chance != chance {
			return State{}, ZapResult{}, ErrZapStaleProposal
		}
		expectedAfter := actor.Body
		if err := zapSetTimer(&expectedAfter, p.Now); err != nil {
			return State{}, ZapResult{}, err
		}
		setSettingFlag(&expectedAfter, zapHiddenFlag, false)
		switch p.Action {
		case ZapSpellFail:
			if !p.SpellFailed || p.Response != ZapSpellFailResponse || !reflect.DeepEqual(p.afterBody, expectedAfter) {
				return State{}, ZapResult{}, ErrZapInvalidProposal
			}
			if p.ConsumedShot {
				if p.Roll <= chance || p.ShotsAfter != item.Object.ShotsCurrent-1 || len(p.Rolls) != 1 {
					return State{}, ZapResult{}, ErrZapInvalidProposal
				}
			} else {
				if !zapVigorSecondSpellFail(actor.Body.Class) || p.Roll > chance || len(p.Rolls) != 2 || p.Rolls[1] < 1 || p.Rolls[1] > 100 || p.Rolls[1] <= chance || p.ShotsAfter != item.Object.ShotsCurrent || p.HealRoll != 0 || p.TargetID != "" || p.HPDelta != 0 || len(p.Events) != 0 {
					return State{}, ZapResult{}, ErrZapInvalidProposal
				}
			}
			nextActor.Body = expectedAfter
		case ZapMissingTarget:
			failDraws := zapVigorFailDraws(actor.Body.Class)
			if p.SpellFailed || p.ConsumedShot || p.Roll > chance || p.Response != ZapMissingTargetResponse || p.ShotsAfter != item.Object.ShotsCurrent || !reflect.DeepEqual(p.afterBody, expectedAfter) || p.TargetName == "" || p.TargetOcc < 1 || len(p.Rolls) != failDraws || !zapVigorSecondSucceeded(p.Rolls, chance, actor.Body.Class) {
				return State{}, ZapResult{}, ErrZapInvalidProposal
			}
			kind, _, _, _, found, targetErr := s.selectZapTarget(actor, p.TargetName, p.TargetOcc)
			if targetErr != nil {
				return State{}, ZapResult{}, targetErr
			}
			if found || kind != "" {
				return State{}, ZapResult{}, ErrZapStaleProposal
			}
			nextActor.Body = expectedAfter
		case ZapVigorSelf:
			failDraws := zapVigorFailDraws(actor.Body.Class)
			if p.SpellFailed || !p.ConsumedShot || p.Roll > chance || p.TargetID != "" || p.HealRoll < 1 || p.HealRoll > 6 || len(p.Rolls) != failDraws+1 || p.Rolls[failDraws] != p.HealRoll || !zapVigorSecondSucceeded(p.Rolls, chance, actor.Body.Class) {
				return State{}, ZapResult{}, ErrZapInvalidProposal
			}
			nextHP, delta := zapVigorHeal(expectedAfter.HPCurrent, expectedAfter.HPMax, p.HealRoll)
			expectedAfter.HPCurrent = nextHP
			if delta != p.HPDelta || !reflect.DeepEqual(p.afterBody, expectedAfter) || p.Response != zapSuccessResponse(item.Object, ZapVigorSelfResponse) || p.ShotsAfter != item.Object.ShotsCurrent-1 {
				return State{}, ZapResult{}, ErrZapInvalidProposal
			}
			nextActor.Body = expectedAfter
		case ZapVigorTarget:
			failDraws := zapVigorFailDraws(actor.Body.Class)
			if p.SpellFailed || !p.ConsumedShot || p.Roll > chance || p.TargetID == "" || p.HealRoll < 1 || p.HealRoll > 6 || len(p.Rolls) != failDraws+1 || p.Rolls[failDraws] != p.HealRoll || !reflect.DeepEqual(p.afterBody, expectedAfter) || !zapVigorSecondSucceeded(p.Rolls, chance, actor.Body.Class) {
				return State{}, ZapResult{}, ErrZapInvalidProposal
			}
			kind, id, player, npc, found, targetErr := s.selectZapTarget(actor, p.TargetName, p.TargetOcc)
			if targetErr != nil || !found || kind != p.TargetKind || id != p.TargetID {
				return State{}, ZapResult{}, ErrZapStaleProposal
			}
			nextActor.Body = expectedAfter
			if kind == ZapTargetPlayer {
				if !reflect.DeepEqual(player, p.expectedTargetPlayer) {
					return State{}, ZapResult{}, ErrZapStaleProposal
				}
				nextHP, delta := zapVigorHeal(player.Body.HPCurrent, player.Body.HPMax, p.HealRoll)
				want := player.Body
				want.HPCurrent = nextHP
				wantEvents := ZapVigorTargetEvents(p.RoomID, p.ActorID, actor.Body.Name, id, player.Body.Name, true)
				if delta != p.HPDelta || !reflect.DeepEqual(p.afterTargetPlayer, want) || p.Response != zapSuccessResponse(item.Object, ZapVigorTargetResponse(player.Body.Name)) || !reflect.DeepEqual(p.Events, wantEvents) {
					return State{}, ZapResult{}, ErrZapInvalidProposal
				}
				target := next.Players[id]
				target.Body.HPCurrent = nextHP
				next.Players[id] = target
			} else {
				if !reflect.DeepEqual(npc, p.expectedTargetNPC) {
					return State{}, ZapResult{}, ErrZapStaleProposal
				}
				nextHP, delta := zapVigorHeal(npc.Body.HPCurrent, npc.Body.HPMax, p.HealRoll)
				want := npc.Body
				want.HPCurrent = nextHP
				wantEvents := ZapVigorTargetEvents(p.RoomID, p.ActorID, actor.Body.Name, id, npc.Body.Name, false)
				if delta != p.HPDelta || !reflect.DeepEqual(p.afterTargetNPC, want) || p.Response != zapSuccessResponse(item.Object, ZapVigorTargetResponse(npc.Body.Name)) || !reflect.DeepEqual(p.Events, wantEvents) {
					return State{}, ZapResult{}, ErrZapInvalidProposal
				}
				target := next.NPCs[id]
				target.Body.HPCurrent = nextHP
				next.NPCs[id] = target
			}
		}
		if p.ConsumedShot {
			kept, ok := nextActor.Items.Items[p.ItemID]
			if !ok || p.ShotsAfter != item.Object.ShotsCurrent-1 {
				return State{}, ZapResult{}, ErrZapStaleProposal
			}
			kept.Object.ShotsCurrent = p.ShotsAfter
			nextActor.Items.Items[p.ItemID] = kept
		}
		next.Players[p.ActorID] = nextActor
	default:
		return State{}, ZapResult{}, ErrZapInvalidProposal
	}
	if err := next.Validate(); err != nil {
		return State{}, ZapResult{}, err
	}
	return next, zapResult(p, next.Players[p.ActorID]), nil
}

func (s State) Zap(actorID, itemName string, occurrence int, targetName string, targetOcc int, options ZapOptions) (State, ZapResult, error) {
	p, err := s.PlanZap(actorID, itemName, occurrence, targetName, targetOcc, options)
	if err != nil {
		return State{}, ZapResult{}, err
	}
	return s.ApplyZap(p)
}
