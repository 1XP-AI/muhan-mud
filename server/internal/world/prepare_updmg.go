package world

import (
	"fmt"
	"math"
	"reflect"
)

// These are the source indices from src/mtype.h.  They intentionally remain
// the legacy indices rather than introducing a second Go-only flag/timer
// numbering scheme.
const (
	prepareTimerIndex = 20 // LT_PREPA
	prepareFlag       = 24 // PPREPA
	upDmgTimerIndex   = 42 // LT_UPDMG
	upDmgFlag         = 59 // PUPDMG
	dmClass           = 12 // DM
)

// Exported names are useful to migration fixtures without exposing any
// mutable state.  The lower-case aliases above keep this package's source
// backed tests concise and match the other world reducers.
const (
	PrepareTimerIndex = prepareTimerIndex
	PrepareFlag       = prepareFlag
	UpDmgTimerIndex   = upDmgTimerIndex
	UpDmgFlag         = upDmgFlag
)

const (
	prepareCooldownSeconds = int32(15)
	upDmgCooldownSeconds   = int64(1200)
	upDmgEffectSeconds     = int32(120)
	upDmgFailureOffset     = int64(960)
)

// PrepareProposal is the snapshot-bound candidate for command9.c:prepare.
// ApplyPrepare accepts only a proposal produced by PlanPrepare; the private
// actor copy prevents a stale command from setting only a flag or timer after
// another command changed the same player.
type PrepareProposal struct {
	ActorID       string
	RoomID        int16
	Now           int32
	Interval      int32
	WaitSeconds   int32
	Cooldown      bool
	Active        bool
	AlreadyActive bool
	Activate      bool
	TimerWrite    bool
	Blind         bool
	Broadcast     bool
	Response      string

	expectedActor PlayerState
}

// PrepareResult is the durable actor response and the committed room-event
// input.  Event is populated only for the accepted prepare attempt, including
// the source-compatible PBLIND case where the flag is set and immediately
// cleared.  Transports may fan this event out after commit; this package never
// writes to a connection.
type PrepareResult struct {
	Action    string        `json:"action"`
	Response  string        `json:"response"`
	Broadcast bool          `json:"broadcast"`
	Changed   bool          `json:"changed"`
	Active    bool          `json:"active"`
	Cooldown  bool          `json:"cooldown,omitempty"`
	Succeeded bool          `json:"succeeded,omitempty"`
	RoomID    int16         `json:"room_id,omitempty"`
	ActorID   string        `json:"actor_id,omitempty"`
	ActorName string        `json:"actor_name,omitempty"`
	EventText string        `json:"event_text,omitempty"`
	Event     *PrepareEvent `json:"event,omitempty"`
}

// PrepareEvent is the room projection of a committed prepare attempt.  The
// actor is excluded because its response is already in the durable receipt.
type PrepareEvent struct {
	RoomID         int16  `json:"room_id"`
	ActorID        string `json:"actor_id"`
	ActorName      string `json:"actor_name"`
	ExcludeActorID string `json:"exclude_actor_id"`
	Text           string `json:"text"`
}

// UpDmgProposal is the deterministic candidate for command9.c:up_dmg.  The
// source has three no-mutation gates (authorization, active effect, and
// cooldown), then consumes exactly one 1..100 roll.  Roll is persisted in the
// proposal/result so an application/replay path never needs to call the RNG a
// second time.
type UpDmgProposal struct {
	ActorID          string
	RoomID           int16
	Now              int32
	Chance           int
	Roll             int
	WaitSeconds      int32
	Cooldown         bool
	Authorized       bool
	Active           bool
	AlreadyActive    bool
	Attempted        bool
	Succeeded        bool
	Broadcast        bool
	Changed          bool
	TimerWrite       bool
	PreviousInterval int32
	Interval         int32
	FailureLastTime  int32
	Armor            byte
	Response         string

	expectedActor PlayerState
}

// UpDmgResult is the durable actor response and committed room-event input.
// Failure is still a broadcast/changed result: C writes LT_UPDMG to now-960
// on a failed roll, leaving the previous interval untouched.
type UpDmgResult struct {
	Action        string      `json:"action"`
	Response      string      `json:"response"`
	Broadcast     bool        `json:"broadcast"`
	Changed       bool        `json:"changed"`
	Active        bool        `json:"active"`
	AlreadyActive bool        `json:"already_active,omitempty"`
	Cooldown      bool        `json:"cooldown,omitempty"`
	Authorized    bool        `json:"authorized"`
	Attempted     bool        `json:"attempted"`
	TimerWrite    bool        `json:"timer_write"`
	Succeeded     bool        `json:"succeeded"`
	Chance        int         `json:"chance,omitempty"`
	Roll          int         `json:"roll,omitempty"`
	RoomID        int16       `json:"room_id,omitempty"`
	ActorID       string      `json:"actor_id,omitempty"`
	ActorName     string      `json:"actor_name,omitempty"`
	EventText     string      `json:"event_text,omitempty"`
	Event         *UpDmgEvent `json:"event,omitempty"`
}

// UpDmgEvent is the room projection of a committed success or failed attempt.
// The actor receives Response directly, so fan-out excludes ActorID.
type UpDmgEvent struct {
	RoomID         int16  `json:"room_id"`
	ActorID        string `json:"actor_id"`
	ActorName      string `json:"actor_name"`
	ExcludeActorID string `json:"exclude_actor_id"`
	Succeeded      bool   `json:"succeeded"`
	Text           string `json:"text"`
}

// Compatibility aliases use the spelling found in prose and let callers
// migrate from up_dmg to up-damage without creating a second reducer.
type UpDamageProposal = UpDmgProposal
type UpDamageResult = UpDmgResult
type UpDamageEvent = UpDmgEvent

func clonePrepareUpDmgPlayer(player PlayerState) PlayerState {
	player.Body.Inventory = cloneObjects(player.Body.Inventory)
	if player.Items != nil {
		items := player.Items.clone()
		player.Items = &items
	}
	player.PlayerEnemies = append([]string(nil), player.PlayerEnemies...)
	player.FollowerIDs = append([]string(nil), player.FollowerIDs...)
	if player.NPCFollowerIDs != nil {
		player.NPCFollowerIDs = append([]string(nil), player.NPCFollowerIDs...)
	}
	if player.FollowerRefs != nil {
		player.FollowerRefs = append([]EntityRef(nil), player.FollowerRefs...)
	}
	return player
}

func prepareUpDmgActor(s State, actorID string) (PlayerState, error) {
	if actorID == "" {
		return PlayerState{}, fmt.Errorf("empty prepare/up_dmg actor")
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Body.Type != 0 || !validHideActorName(actor.Body.Name) {
		return PlayerState{}, fmt.Errorf("online canonical player absent")
	}
	if _, ok := s.Rooms[actor.Body.RoomID]; !ok {
		return PlayerState{}, fmt.Errorf("actor room absent")
	}
	return actor, nil
}

func legacyTimerDeadline(timer LegacyTimer) (int64, error) {
	if timer.LastTime < 0 || timer.Interval < 0 {
		return 0, fmt.Errorf("legacy timer outside non-negative range")
	}
	return int64(timer.LastTime) + int64(timer.Interval), nil
}

func prepareWaitResponse(seconds int32) string {
	if seconds == 1 {
		return "1초만 기다리세요.\n"
	}
	return fmt.Sprintf("%d초동안 기다리세요.\n", seconds)
}

func upDmgCooldownResponse(seconds int32) string {
	return fmt.Sprintf("%d분 %02d초 기다리세요.\n", seconds/60, seconds%60)
}

func prepareRoomEvent(actorID string, actor PlayerState, roomID int16) *PrepareEvent {
	return &PrepareEvent{
		RoomID: roomID, ActorID: actorID, ActorName: actor.Body.Name,
		ExcludeActorID: actorID,
		Text:           fmt.Sprintf("\n%s님이 함정을 조심하며 갑니다.\r\n", actor.Body.Name),
	}
}

func upDmgRoomEvent(actorID string, actor PlayerState, roomID int16, succeeded bool) *UpDmgEvent {
	text := fmt.Sprintf("\n%s님이 잠력격발을 시도합니다.\r\n", actor.Body.Name)
	if succeeded {
		text = fmt.Sprintf("\n%s님이 자신의 혈도를 짚으며 힘을 끌어들입니다.\r\n", actor.Body.Name)
	}
	return &UpDmgEvent{
		RoomID: roomID, ActorID: actorID, ActorName: actor.Body.Name,
		ExcludeActorID: actorID, Succeeded: succeeded, Text: text,
	}
}

// RoomPrepareEvent and RoomUpDmgEvent are read-only projections for callers
// that need to derive a room event outside Apply*.  ExecuteGame should prefer
// the event embedded in the committed result so a replay cannot re-evaluate
// current state or consume randomness.
func (s State) RoomPrepareEvent(actorID string) (PrepareEvent, bool, error) {
	if err := s.Validate(); err != nil {
		return PrepareEvent{}, false, err
	}
	actor, err := prepareUpDmgActor(s, actorID)
	if err != nil {
		return PrepareEvent{}, false, err
	}
	event := prepareRoomEvent(actorID, actor, actor.Body.RoomID)
	return *event, true, nil
}

func (s State) RoomUpDmgEvent(actorID string, succeeded bool) (UpDmgEvent, bool, error) {
	if err := s.Validate(); err != nil {
		return UpDmgEvent{}, false, err
	}
	actor, err := prepareUpDmgActor(s, actorID)
	if err != nil {
		return UpDmgEvent{}, false, err
	}
	event := upDmgRoomEvent(actorID, actor, actor.Body.RoomID, succeeded)
	return *event, true, nil
}

func (s State) RoomUpDamageEvent(actorID string, succeeded bool) (UpDamageEvent, bool, error) {
	return s.RoomUpDmgEvent(actorID, succeeded)
}

// PlanPrepare ports command9.c:prepare's bare command.  Duplicate active
// state wins over cooldown, as in C.  A blind player still consumes the
// prepare timer and emits the room event; PPREPA is set then immediately
// cleared after the source broadcast.
func (s State) PlanPrepare(actorID string, now int32) (PrepareProposal, error) {
	if err := s.Validate(); err != nil {
		return PrepareProposal{}, err
	}
	if now < 0 {
		return PrepareProposal{}, fmt.Errorf("invalid prepare clock")
	}
	actor, err := prepareUpDmgActor(s, actorID)
	if err != nil {
		return PrepareProposal{}, err
	}
	proposal := PrepareProposal{
		ActorID: actorID, RoomID: actor.Body.RoomID, Now: now,
		expectedActor: clonePrepareUpDmgPlayer(actor),
	}
	if flag(actor.Body.Flags[:], prepareFlag) {
		proposal.Active = true
		proposal.AlreadyActive = true
		proposal.Response = "당신은 이미 함정들을 주의하고 있습니다."
		return proposal, nil
	}
	timer := actor.Body.Timers[prepareTimerIndex]
	deadline, err := legacyTimerDeadline(timer)
	if err != nil {
		return PrepareProposal{}, err
	}
	if int64(now) < deadline {
		wait := deadline - int64(now)
		if wait < 1 || wait > int64(math.MaxInt32) {
			return PrepareProposal{}, fmt.Errorf("prepare cooldown overflow")
		}
		proposal.Cooldown = true
		proposal.WaitSeconds = int32(wait)
		proposal.Response = prepareWaitResponse(proposal.WaitSeconds)
		return proposal, nil
	}
	proposal.Activate = true
	proposal.Broadcast = true
	proposal.Interval = prepareCooldownSeconds
	if actor.Body.Class == dmClass {
		proposal.Interval = 0
	}
	proposal.Blind = flag(actor.Body.Flags[:], playerBlindFlag)
	proposal.TimerWrite = true
	proposal.Response = "당신은 이제부터 함정이 있나 살펴보며 갑니다."
	return proposal, nil
}

// ApplyPrepare atomically applies a source-ordered prepare proposal.  It
// rechecks all gates and the exact actor snapshot, so a stale proposal cannot
// partially write LT_PREPA/PPREPA.
func (s State) ApplyPrepare(proposal PrepareProposal) (State, PrepareResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, PrepareResult{}, err
	}
	if proposal.ActorID == "" || proposal.Now < 0 || proposal.Response == "" {
		return State{}, PrepareResult{}, fmt.Errorf("invalid prepare proposal")
	}
	actor, err := prepareUpDmgActor(s, proposal.ActorID)
	if err != nil || actor.Body.RoomID != proposal.RoomID {
		return State{}, PrepareResult{}, fmt.Errorf("prepare actor changed")
	}
	if !reflect.DeepEqual(actor, proposal.expectedActor) {
		return State{}, PrepareResult{}, fmt.Errorf("stale prepare actor")
	}
	active := flag(actor.Body.Flags[:], prepareFlag)
	if proposal.AlreadyActive {
		if !proposal.Active || !active || proposal.Activate || proposal.Cooldown || proposal.TimerWrite || proposal.Broadcast || proposal.Interval != 0 || proposal.WaitSeconds != 0 || proposal.Blind || proposal.Response != "당신은 이미 함정들을 주의하고 있습니다." {
			return State{}, PrepareResult{}, fmt.Errorf("stale active prepare proposal")
		}
		return s, PrepareResult{Action: "prepare", Response: proposal.Response, Active: true, RoomID: proposal.RoomID, ActorID: proposal.ActorID, ActorName: actor.Body.Name}, nil
	}
	if active {
		return State{}, PrepareResult{}, fmt.Errorf("prepare active state changed")
	}
	timer := actor.Body.Timers[prepareTimerIndex]
	deadline, err := legacyTimerDeadline(timer)
	if err != nil {
		return State{}, PrepareResult{}, err
	}
	if proposal.Cooldown {
		wait := deadline - int64(proposal.Now)
		if wait < 1 || wait > int64(math.MaxInt32) || proposal.Active || proposal.Activate || proposal.TimerWrite || proposal.Broadcast || proposal.Blind || proposal.Interval != 0 || proposal.WaitSeconds != int32(wait) || int64(proposal.Now) >= deadline || proposal.Response != prepareWaitResponse(proposal.WaitSeconds) {
			return State{}, PrepareResult{}, fmt.Errorf("stale prepare cooldown proposal")
		}
		return s, PrepareResult{Action: "prepare", Response: proposal.Response, Cooldown: true, RoomID: proposal.RoomID, ActorID: proposal.ActorID, ActorName: actor.Body.Name}, nil
	}
	interval := prepareCooldownSeconds
	if actor.Body.Class == dmClass {
		interval = 0
	}
	if !proposal.Activate || !proposal.TimerWrite || !proposal.Broadcast || proposal.Active || proposal.AlreadyActive || proposal.WaitSeconds != 0 || proposal.Interval != interval || int64(proposal.Now) < deadline || proposal.Response != "당신은 이제부터 함정이 있나 살펴보며 갑니다." || proposal.Blind != flag(actor.Body.Flags[:], playerBlindFlag) {
		return State{}, PrepareResult{}, fmt.Errorf("stale prepare activation proposal")
	}
	next := s.clone()
	nextActor := next.Players[proposal.ActorID]
	nextActor.Body.Timers[prepareTimerIndex] = LegacyTimer{LastTime: proposal.Now, Interval: interval}
	setSettingFlag(&nextActor.Body, prepareFlag, true)
	if proposal.Blind {
		setSettingFlag(&nextActor.Body, prepareFlag, false)
	}
	next.Players[proposal.ActorID] = nextActor
	if err := next.Validate(); err != nil {
		return State{}, PrepareResult{}, err
	}
	event := prepareRoomEvent(proposal.ActorID, nextActor, proposal.RoomID)
	return next, PrepareResult{
		Action: "prepare", Response: proposal.Response, Broadcast: true,
		Changed: true, Active: flag(nextActor.Body.Flags[:], prepareFlag),
		Succeeded: true, RoomID: proposal.RoomID, ActorID: proposal.ActorID, ActorName: nextActor.Body.Name,
		EventText: event.Text, Event: event,
	}, nil
}

// Prepare is a concise alias for callers that do not use the Plan prefix.
func (s State) Prepare(actorID string, now int32) (PrepareProposal, error) {
	return s.PlanPrepare(actorID, now)
}

func upDmgChance(body LegacyMonster) (int, error) {
	if body.Stats[1] > 63 {
		return 0, fmt.Errorf("up_dmg dexterity outside legacy bonus table")
	}
	chance := ((int(body.Level) + 3) / 4) * 20
	chance += legacyStatBonus[body.Stats[1]]
	if chance > 85 {
		chance = 85
	}
	return chance, nil
}

// UpDmgChance exposes the source chance calculation for migration fixtures
// without exposing the mutable reducer proposal.
func UpDmgChance(body LegacyMonster) (int, error) {
	return upDmgChance(body)
}

func upDmgRoll(roll func(int, int) int) (value int, err error) {
	if roll == nil {
		return 0, fmt.Errorf("missing up_dmg random source")
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			value = 0
			err = fmt.Errorf("up_dmg random source panicked: %v", recovered)
		}
	}()
	value = roll(1, 100)
	if value < 1 || value > 100 {
		return 0, fmt.Errorf("up_dmg random value outside 1..100")
	}
	return value, nil
}

func upDmgArmor(actor PlayerState) (byte, error) {
	var ready [20]*LegacyObject
	if actor.Items != nil {
		if err := actor.Items.Validate(); err != nil {
			return 0, err
		}
		for slot, id := range actor.Items.Ready {
			if id == "" {
				continue
			}
			item, ok := actor.Items.Items[id]
			if !ok {
				return 0, fmt.Errorf("up_dmg ready item absent")
			}
			object := item.Object
			ready[slot] = &object
		}
	}
	armor, err := ComputeArmorClass(actor.Body.Stats[1], ready, flag(actor.Body.Flags[:], 8))
	if err != nil {
		return 0, err
	}
	return byte(armor), nil
}

func upDmgNumericSuccessValid(body LegacyMonster) bool {
	return int64(body.DicePlus)+5 >= math.MinInt16 && int64(body.DicePlus)+5 <= math.MaxInt16 &&
		int64(body.HPMax)+100 >= math.MinInt16 && int64(body.HPMax)+100 <= math.MaxInt16 &&
		int64(body.MPMax)+100 >= math.MinInt16 && int64(body.MPMax)+100 <= math.MaxInt16
}

// PlanUpDmg ports command9.c:up_dmg.  Authorization and active/cooldown
// failures intentionally return no-op proposals so the source response is
// durably replayable.  Only an eligible attempt consumes a random value.
func (s State) PlanUpDmg(actorID string, now int32, roll func(int, int) int) (UpDmgProposal, error) {
	if err := s.Validate(); err != nil {
		return UpDmgProposal{}, err
	}
	if now < 0 {
		return UpDmgProposal{}, fmt.Errorf("invalid up_dmg clock")
	}
	actor, err := prepareUpDmgActor(s, actorID)
	if err != nil {
		return UpDmgProposal{}, err
	}
	proposal := UpDmgProposal{
		ActorID: actorID, RoomID: actor.Body.RoomID, Now: now,
		expectedActor: clonePrepareUpDmgPlayer(actor),
	}
	if actor.Body.Class < InvincibleClass {
		proposal.Response = "무적만 사용할 수 있는 기술입니다.\n"
		return proposal, nil
	}
	proposal.Authorized = true
	if flag(actor.Body.Flags[:], upDmgFlag) {
		proposal.Active = true
		proposal.AlreadyActive = true
		proposal.Response = "당신은 지금 잠력격발을 사용중입니다.\n"
		return proposal, nil
	}
	timer := actor.Body.Timers[upDmgTimerIndex]
	if timer.LastTime < 0 || timer.Interval < 0 {
		return UpDmgProposal{}, fmt.Errorf("up_dmg timer outside non-negative range")
	}
	elapsed := int64(now) - int64(timer.LastTime)
	if elapsed < upDmgCooldownSeconds {
		wait := upDmgCooldownSeconds - elapsed
		if wait < 1 || wait > int64(math.MaxInt32) {
			return UpDmgProposal{}, fmt.Errorf("up_dmg cooldown overflow")
		}
		proposal.Cooldown = true
		proposal.WaitSeconds = int32(wait)
		proposal.PreviousInterval = timer.Interval
		proposal.Interval = timer.Interval
		proposal.Response = upDmgCooldownResponse(proposal.WaitSeconds)
		return proposal, nil
	}
	chance, err := upDmgChance(actor.Body)
	if err != nil {
		return UpDmgProposal{}, err
	}
	proposal.Chance = chance
	proposal.Roll, err = upDmgRoll(roll)
	if err != nil {
		return UpDmgProposal{}, err
	}
	proposal.Succeeded = proposal.Roll <= chance
	proposal.Attempted = true
	proposal.Broadcast = true
	proposal.Changed = true
	proposal.TimerWrite = true
	proposal.PreviousInterval = timer.Interval
	if proposal.Succeeded {
		if !upDmgNumericSuccessValid(actor.Body) {
			return UpDmgProposal{}, fmt.Errorf("up_dmg numeric overflow")
		}
		proposal.Interval = upDmgEffectSeconds
		proposal.Response = "당신은 자신의 혈도를 짚으며 몸의 잠력을 격발시킵니다.\n온몸으로 기가 퍼져나가는것을 느낍니다.\n"
		proposal.Armor, err = upDmgArmor(actor)
		if err != nil {
			return UpDmgProposal{}, err
		}
		return proposal, nil
	}
	failureLast := int64(now) - upDmgFailureOffset
	if failureLast < math.MinInt32 || failureLast > math.MaxInt32 {
		return UpDmgProposal{}, fmt.Errorf("up_dmg failure timer overflow")
	}
	proposal.FailureLastTime = int32(failureLast)
	proposal.Interval = timer.Interval
	proposal.Response = "힘을 격발시키는데 실패했습니다.\n"
	proposal.Armor, err = upDmgArmor(actor)
	if err != nil {
		return UpDmgProposal{}, err
	}
	return proposal, nil
}

// ApplyUpDmg atomically applies a planned success or failure, retaining the
// source's failure cooldown (now-960 with the old interval) and success effect
// timer (+5 dice, +100 max/current HP/MP, interval 120).
func (s State) ApplyUpDmg(proposal UpDmgProposal) (State, UpDmgResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, UpDmgResult{}, err
	}
	if proposal.ActorID == "" || proposal.Now < 0 || proposal.Response == "" {
		return State{}, UpDmgResult{}, fmt.Errorf("invalid up_dmg proposal")
	}
	actor, err := prepareUpDmgActor(s, proposal.ActorID)
	if err != nil || actor.Body.RoomID != proposal.RoomID {
		return State{}, UpDmgResult{}, fmt.Errorf("up_dmg actor changed")
	}
	if !reflect.DeepEqual(actor, proposal.expectedActor) {
		return State{}, UpDmgResult{}, fmt.Errorf("stale up_dmg actor")
	}
	if proposal.Now < 0 {
		return State{}, UpDmgResult{}, fmt.Errorf("invalid up_dmg clock")
	}
	active := flag(actor.Body.Flags[:], upDmgFlag)
	if !proposal.Authorized {
		if actor.Body.Class >= InvincibleClass || proposal.Active || proposal.AlreadyActive || proposal.Cooldown || proposal.Attempted || proposal.Succeeded || proposal.Changed || proposal.TimerWrite || proposal.Broadcast || proposal.Chance != 0 || proposal.Roll != 0 || proposal.WaitSeconds != 0 || proposal.Interval != 0 || proposal.Response != "무적만 사용할 수 있는 기술입니다.\n" {
			return State{}, UpDmgResult{}, fmt.Errorf("stale up_dmg authorization proposal")
		}
		return s, UpDmgResult{Action: "up_dmg", Response: proposal.Response, RoomID: proposal.RoomID, ActorID: proposal.ActorID, ActorName: actor.Body.Name}, nil
	}
	if proposal.AlreadyActive {
		if !proposal.Active || !active || proposal.Cooldown || proposal.Attempted || proposal.Succeeded || proposal.Changed || proposal.TimerWrite || proposal.Broadcast || proposal.Roll != 0 || proposal.Chance != 0 || proposal.Response != "당신은 지금 잠력격발을 사용중입니다.\n" {
			return State{}, UpDmgResult{}, fmt.Errorf("stale up_dmg active proposal")
		}
		return s, UpDmgResult{Action: "up_dmg", Response: proposal.Response, Authorized: true, Active: true, AlreadyActive: true, RoomID: proposal.RoomID, ActorID: proposal.ActorID, ActorName: actor.Body.Name}, nil
	}
	if actor.Body.Class < InvincibleClass || active {
		return State{}, UpDmgResult{}, fmt.Errorf("up_dmg gate changed")
	}
	timer := actor.Body.Timers[upDmgTimerIndex]
	if timer.LastTime < 0 || timer.Interval < 0 {
		return State{}, UpDmgResult{}, fmt.Errorf("up_dmg timer outside non-negative range")
	}
	elapsed := int64(proposal.Now) - int64(timer.LastTime)
	if proposal.Cooldown {
		wait := upDmgCooldownSeconds - elapsed
		if wait < 1 || wait > int64(math.MaxInt32) || proposal.Active || proposal.WaitSeconds != int32(wait) || proposal.Attempted || proposal.Changed || proposal.TimerWrite || proposal.Broadcast || proposal.Roll != 0 || proposal.Chance != 0 || proposal.PreviousInterval != timer.Interval || proposal.Interval != timer.Interval || proposal.Response != upDmgCooldownResponse(proposal.WaitSeconds) {
			return State{}, UpDmgResult{}, fmt.Errorf("stale up_dmg cooldown proposal")
		}
		return s, UpDmgResult{Action: "up_dmg", Response: proposal.Response, Authorized: true, Cooldown: true, RoomID: proposal.RoomID, ActorID: proposal.ActorID, ActorName: actor.Body.Name}, nil
	}
	chance, err := upDmgChance(actor.Body)
	if err != nil {
		return State{}, UpDmgResult{}, err
	}
	if proposal.Chance != chance || proposal.Roll < 1 || proposal.Roll > 100 || !proposal.Attempted || !proposal.Changed || !proposal.TimerWrite || !proposal.Broadcast || proposal.WaitSeconds != 0 || proposal.PreviousInterval != timer.Interval || proposal.Succeeded != (proposal.Roll <= chance) {
		return State{}, UpDmgResult{}, fmt.Errorf("stale up_dmg roll proposal")
	}
	if proposal.Succeeded {
		if !upDmgNumericSuccessValid(actor.Body) || proposal.Interval != upDmgEffectSeconds || proposal.Response != "당신은 자신의 혈도를 짚으며 몸의 잠력을 격발시킵니다.\n온몸으로 기가 퍼져나가는것을 느낍니다.\n" {
			return State{}, UpDmgResult{}, fmt.Errorf("invalid up_dmg success proposal")
		}
	} else {
		failureLast := int64(proposal.Now) - upDmgFailureOffset
		if failureLast < math.MinInt32 || failureLast > math.MaxInt32 || proposal.FailureLastTime != int32(failureLast) || proposal.Interval != timer.Interval || proposal.Response != "힘을 격발시키는데 실패했습니다.\n" {
			return State{}, UpDmgResult{}, fmt.Errorf("invalid up_dmg failure proposal")
		}
	}
	next := s.clone()
	nextActor := next.Players[proposal.ActorID]
	if proposal.Succeeded {
		nextActor.Body.DicePlus = int16(int64(nextActor.Body.DicePlus) + 5)
		nextActor.Body.HPMax = int16(int64(nextActor.Body.HPMax) + 100)
		nextActor.Body.MPMax = int16(int64(nextActor.Body.MPMax) + 100)
		nextActor.Body.HPCurrent = nextActor.Body.HPMax
		nextActor.Body.MPCurrent = nextActor.Body.MPMax
		nextActor.Body.Timers[upDmgTimerIndex] = LegacyTimer{LastTime: proposal.Now, Interval: upDmgEffectSeconds}
		setSettingFlag(&nextActor.Body, upDmgFlag, true)
		armor, err := upDmgArmor(nextActor)
		if err != nil {
			return State{}, UpDmgResult{}, err
		}
		nextActor.Body.Armor = armor
	} else {
		nextActor.Body.Timers[upDmgTimerIndex].LastTime = proposal.FailureLastTime
		// C intentionally leaves interval unchanged after failure.
	}
	armor, err := upDmgArmor(nextActor)
	if err != nil {
		return State{}, UpDmgResult{}, err
	}
	if proposal.Armor != armor {
		return State{}, UpDmgResult{}, fmt.Errorf("stale up_dmg armor proposal")
	}
	nextActor.Body.Armor = armor
	next.Players[proposal.ActorID] = nextActor
	if err := next.Validate(); err != nil {
		return State{}, UpDmgResult{}, err
	}
	event := upDmgRoomEvent(proposal.ActorID, nextActor, proposal.RoomID, proposal.Succeeded)
	result := UpDmgResult{
		Action: "up_dmg", Response: proposal.Response, Broadcast: true,
		Changed: true, Authorized: true, Attempted: true, TimerWrite: true, Succeeded: proposal.Succeeded,
		Chance: proposal.Chance, Roll: proposal.Roll,
		RoomID: proposal.RoomID, ActorID: proposal.ActorID, ActorName: nextActor.Body.Name,
		Active:    flag(nextActor.Body.Flags[:], upDmgFlag),
		EventText: event.Text, Event: event,
	}
	return next, result, nil
}

// UpDmg is a concise alias for callers that do not use the Plan prefix.
func (s State) UpDmg(actorID string, now int32, roll func(int, int) int) (UpDmgProposal, error) {
	return s.PlanUpDmg(actorID, now, roll)
}

// PlanUpDamage and ApplyUpDamage are spelling aliases only; both use the
// source-named up_dmg reducer and therefore have identical receipt semantics.
func (s State) PlanUpDamage(actorID string, now int32, roll func(int, int) int) (UpDamageProposal, error) {
	return s.PlanUpDmg(actorID, now, roll)
}

func (s State) ApplyUpDamage(proposal UpDamageProposal) (State, UpDamageResult, error) {
	return s.ApplyUpDmg(proposal)
}
