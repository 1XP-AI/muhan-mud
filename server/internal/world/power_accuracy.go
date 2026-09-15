package world

import (
	"fmt"
	"math"
	"reflect"
)

// The values below are the source-backed slots from src/mtype.h.  They are
// intentionally kept as legacy state indices: a transport-local cooldown is
// not a substitute for the durable player flag/timer used by command9.c.
const (
	powerFlag          = 52 // PPOWER
	powerTimerIndex    = 37 // LT_POWER
	accurateFlag       = 53 // PSLAYE
	accurateTimerIndex = 38 // LT_SLAYE
	wieldSlotIndex     = 19 // WIELD-1 / MAXWEAR's twentieth slot

	powerClass        = 4 // FIGHTER
	accurateAssassin  = 1 // ASSASSIN
	accurateThief     = 8 // THIEF
	abilityInvincible = 9 // INVINCIBLE
	abilityMaxClass   = 12

	powerCooldownSeconds    = int64(600)
	accurateCooldownSeconds = int64(700)
	failureLastOffset       = int64(590)
)

// Export source slots and class constants for migration fixtures and callers
// that need to inspect the canonical contract without copying magic numbers.
const (
	PowerFlag               = powerFlag
	PowerTimerIndex         = powerTimerIndex
	AccurateFlag            = accurateFlag
	AccurateTimerIndex      = accurateTimerIndex
	WieldSlotIndex          = wieldSlotIndex
	WieldSlot               = wieldSlotIndex
	AccurateWieldSlot       = wieldSlotIndex
	PowerClass              = powerClass
	FighterClass            = powerClass
	AccurateAssassinClass   = accurateAssassin
	AssassinClass           = accurateAssassin
	AccurateThiefClass      = accurateThief
	ThiefClass              = accurateThief
	PowerInvincibleClass    = abilityInvincible
	PowerCooldownSeconds    = powerCooldownSeconds
	AccurateCooldownSeconds = accurateCooldownSeconds
)

// PowerAccuracyKind identifies one of command9.c's two self-only abilities.
// The string values are stable receipt fields, not user-facing aliases.
type PowerAccuracyKind string

const (
	PowerAbility    PowerAccuracyKind = "power"
	AccurateAbility PowerAccuracyKind = "accurate"
	Power           PowerAccuracyKind = PowerAbility
	Accurate        PowerAccuracyKind = AccurateAbility
	// Descriptive aliases make the closed operation contract discoverable to
	// callers without introducing additional command spellings.
	PowerKind    PowerAccuracyKind = PowerAbility
	AccurateKind PowerAccuracyKind = AccurateAbility
)

func validPowerAccuracyKind(kind PowerAccuracyKind) bool {
	return kind == PowerAbility || kind == AccurateAbility
}

func powerAccuracyFlag(kind PowerAccuracyKind) (int, error) {
	switch kind {
	case PowerAbility:
		return powerFlag, nil
	case AccurateAbility:
		return accurateFlag, nil
	default:
		return 0, fmt.Errorf("unknown power/accurate ability %q", kind)
	}
}

func powerAccuracyTimer(kind PowerAccuracyKind) (int, error) {
	switch kind {
	case PowerAbility:
		return powerTimerIndex, nil
	case AccurateAbility:
		return accurateTimerIndex, nil
	default:
		return 0, fmt.Errorf("unknown power/accurate ability %q", kind)
	}
}

func powerAccuracyCooldown(kind PowerAccuracyKind) (int64, error) {
	switch kind {
	case PowerAbility:
		return powerCooldownSeconds, nil
	case AccurateAbility:
		return accurateCooldownSeconds, nil
	default:
		return 0, fmt.Errorf("unknown power/accurate ability %q", kind)
	}
}

func powerAccuracyAuthorized(kind PowerAccuracyKind, class byte) (bool, error) {
	if class > abilityMaxClass {
		return false, fmt.Errorf("power/accurate actor class outside legacy table")
	}
	switch kind {
	case PowerAbility:
		return class == powerClass || class >= abilityInvincible, nil
	case AccurateAbility:
		return class == accurateAssassin || class == accurateThief || class >= abilityInvincible, nil
	default:
		return false, fmt.Errorf("unknown power/accurate ability %q", kind)
	}
}

func powerAccuracyChance(body LegacyMonster) (int, error) {
	if body.Stats[1] > 63 {
		return 0, fmt.Errorf("power/accurate dexterity outside legacy bonus table")
	}
	chance := ((int(body.Level) + 3) / 4) * 20
	chance += legacyStatBonus[body.Stats[1]]
	if chance > 85 {
		chance = 85
	}
	return chance, nil
}

// PowerChance and AccurateChance expose the exact command9.c chance formula.
// Both abilities use DEX and the same capped level-band calculation.
func PowerChance(body LegacyMonster) (int, error) {
	return powerAccuracyChance(body)
}

func AccurateChance(body LegacyMonster) (int, error) {
	return powerAccuracyChance(body)
}

func powerAccuracyInterval(kind PowerAccuracyKind, body LegacyMonster) (int32, error) {
	if body.Class > abilityMaxClass {
		return 0, fmt.Errorf("power/accurate actor class outside legacy table")
	}
	levelBand := (int(body.Level) + 3) / 4
	switch kind {
	case PowerAbility:
		return int32(120 + 60*(levelBand/5)), nil
	case AccurateAbility:
		return int32(150 + 60*(levelBand/5)), nil
	default:
		return 0, fmt.Errorf("unknown power/accurate ability %q", kind)
	}
}

func powerAccuracyCooldownResponse(wait int64) (string, error) {
	if wait < 1 || wait > int64(math.MaxInt32) {
		return "", fmt.Errorf("power/accurate cooldown overflow")
	}
	return fmt.Sprintf("%d분 %02d초 기다리세요.\n", wait/60, wait%60), nil
}

func powerAccuracyRoll(roll func(int, int) int) (value int, err error) {
	if roll == nil {
		return 0, fmt.Errorf("missing power/accurate random source")
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			value = 0
			err = fmt.Errorf("power/accurate random source panicked: %v", recovered)
		}
	}()
	value = roll(1, 100)
	if value < 1 || value > 100 {
		return 0, fmt.Errorf("power/accurate random value outside 1..100")
	}
	return value, nil
}

func powerAccuracyUnauthorizedResponse(kind PowerAccuracyKind) string {
	if kind == PowerAbility {
		return "검사만 사용할 수 있는 기술입니다.\n"
	}
	return "자객 도둑만 사용할 수 있는 기술입니다.\n"
}

func powerAccuracyActiveResponse(kind PowerAccuracyKind) string {
	if kind == PowerAbility {
		return "당신은 지금 기공집결을 사용중입니다.\n"
	}
	return "당신은 지금 살기충전을 사용중입니다.\n"
}

func powerAccuracyNoWeaponResponse() string {
	return "장비하고 있는 무기가 없습니다!\n"
}

func powerAccuracySuccessResponse(kind PowerAccuracyKind) string {
	if kind == PowerAbility {
		return "당신은 가부좌를 틀고 기를 모으기 시작합니다.\n온몸으로 기가 퍼져나가는것을 느낍니다.\n"
	}
	return "당신은 당신의 무기에 피를 먹입니다.\n무기에 살기가 감도는 것을 느낍니다.\n"
}

func powerAccuracyFailureResponse(kind PowerAccuracyKind) string {
	if kind == PowerAbility {
		return "기공집결이 실패하였습니다.\n"
	}
	return "살기충전이 실패하였습니다.\n"
}

func powerAccuracyEventText(kind PowerAccuracyKind, succeeded bool, actorName string) string {
	if kind == PowerAbility {
		if succeeded {
			return fmt.Sprintf("\n%s님이 가부좌를 틀고 앉아 기를 모읍니다.\r\n", actorName)
		}
		return fmt.Sprintf("\n%s님이 기공집결을 시도합니다.\r\n", actorName)
	}
	if succeeded {
		return fmt.Sprintf("\n%s님이 그의 무기에 피를 먹입니다.\r\n", actorName)
	}
	return fmt.Sprintf("\n%s님이 그의 무기에 살기충전을 시도합니다.\r\n", actorName)
}

// PowerAccuracyProposal is a snapshot-bound candidate.  The private actor
// copy is deliberately not client-controlled: ApplyPowerAccuracy rejects a
// stale candidate before writing any flag, timer, stat or THACO field.
type PowerAccuracyProposal struct {
	Kind    PowerAccuracyKind
	ActorID string
	RoomID  int16
	Now     int32

	Authorized    bool
	Active        bool
	AlreadyActive bool
	WeaponReady   bool
	Cooldown      bool
	Attempted     bool
	Succeeded     bool
	Activate      bool
	Changed       bool
	TimerWrite    bool
	Broadcast     bool

	Chance           int
	Roll             int
	WaitSeconds      int32
	PreviousInterval int32
	Interval         int32
	FailureLastTime  int32
	StatDelta        int8
	ThacoDelta       int8
	Armor            byte
	Response         string

	expectedActor PlayerState
	expectedTimer LegacyTimer
	weaponID      string
}

// Operation-specific aliases keep the source function names easy to discover
// while sharing one atomic implementation.
type PowerProposal = PowerAccuracyProposal
type AccurateProposal = PowerAccuracyProposal

// PowerAccuracyResult is the durable actor response and committed room-event
// projection. It contains the random outcome so a receipt replay never needs
// to call the injected random source again.
type PowerAccuracyResult struct {
	Kind          PowerAccuracyKind   `json:"kind"`
	Action        string              `json:"action"`
	Response      string              `json:"response"`
	Broadcast     bool                `json:"broadcast"`
	Changed       bool                `json:"changed"`
	Authorized    bool                `json:"authorized"`
	Active        bool                `json:"active"`
	AlreadyActive bool                `json:"already_active,omitempty"`
	WeaponReady   bool                `json:"weapon_ready,omitempty"`
	Cooldown      bool                `json:"cooldown,omitempty"`
	Attempted     bool                `json:"attempted"`
	TimerWrite    bool                `json:"timer_write"`
	Succeeded     bool                `json:"succeeded"`
	Chance        int                 `json:"chance,omitempty"`
	Roll          int                 `json:"roll,omitempty"`
	WaitSeconds   int32               `json:"wait_seconds,omitempty"`
	StatDelta     int8                `json:"stat_delta,omitempty"`
	ThacoDelta    int8                `json:"thaco_delta,omitempty"`
	PreviousStat  byte                `json:"previous_stat,omitempty"`
	NewStat       byte                `json:"new_stat,omitempty"`
	PreviousThaco byte                `json:"previous_thaco,omitempty"`
	NewThaco      byte                `json:"new_thaco,omitempty"`
	RoomID        int16               `json:"room_id,omitempty"`
	ActorID       string              `json:"actor_id,omitempty"`
	ActorName     string              `json:"actor_name,omitempty"`
	EventText     string              `json:"event_text,omitempty"`
	Event         *PowerAccuracyEvent `json:"event,omitempty"`
}

type PowerResult = PowerAccuracyResult
type AccurateResult = PowerAccuracyResult

// PowerAccuracyEvent is the committed room projection. The actor already
// receives Result.Response, so ExcludeActorID avoids a duplicate fan-out.
type PowerAccuracyEvent struct {
	Kind           PowerAccuracyKind `json:"kind"`
	RoomID         int16             `json:"room_id"`
	ActorID        string            `json:"actor_id"`
	ActorName      string            `json:"actor_name"`
	ExcludeActorID string            `json:"exclude_actor_id"`
	Succeeded      bool              `json:"succeeded"`
	Text           string            `json:"text"`
}

type PowerEvent = PowerAccuracyEvent
type AccurateEvent = PowerAccuracyEvent

func powerAccuracyActor(s State, actorID string) (PlayerState, error) {
	if actorID == "" {
		return PlayerState{}, fmt.Errorf("power/accurate actor absent")
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Body.Type != 0 || !validHideActorName(actor.Body.Name) {
		return PlayerState{}, fmt.Errorf("online canonical power/accurate actor absent")
	}
	if _, ok := s.Rooms[actor.Body.RoomID]; !ok {
		return PlayerState{}, fmt.Errorf("power/accurate actor room absent")
	}
	return actor, nil
}

func powerAccuracyHasWeapon(actor PlayerState) (bool, string, error) {
	if actor.Items == nil {
		return false, "", nil
	}
	if err := actor.Items.Validate(); err != nil {
		return false, "", err
	}
	weaponID := actor.Items.Ready[wieldSlotIndex]
	if weaponID == "" {
		return false, "", nil
	}
	if _, ok := actor.Items.Items[weaponID]; !ok {
		return false, "", fmt.Errorf("canonical wield item absent")
	}
	return true, weaponID, nil
}

func powerAccuracyTimerValid(timer LegacyTimer) error {
	if timer.LastTime < 0 || timer.Interval < 0 {
		return fmt.Errorf("power/accurate timer outside non-negative range")
	}
	return nil
}

func powerAccuracySignedByte(value byte, delta int) (byte, error) {
	next := int(int8(value)) + delta
	if next < math.MinInt8 || next > math.MaxInt8 {
		return 0, fmt.Errorf("power/accurate signed value overflow")
	}
	return byte(int8(next)), nil
}

func powerAccuracyStatAfter(body LegacyMonster) (byte, error) {
	// The source's bonus table is defined for 0..63.  Rejecting a power
	// success that would leave that table is safer than persisting an invalid
	// state that the next source-backed reducer could index out of bounds.
	if body.Stats[0] > 63 || int(body.Stats[0])+3 > 63 {
		return 0, fmt.Errorf("power success leaves strength outside legacy table")
	}
	return body.Stats[0] + 3, nil
}

func powerAccuracyResult(proposal PowerAccuracyProposal, actor PlayerState, nextBody LegacyMonster) PowerAccuracyResult {
	action := string(proposal.Kind)
	result := PowerAccuracyResult{
		Kind: proposal.Kind, Action: action, Response: proposal.Response,
		Broadcast: proposal.Broadcast, Changed: proposal.Changed,
		Authorized: proposal.Authorized, Active: proposal.Active || proposal.Succeeded,
		AlreadyActive: proposal.AlreadyActive, WeaponReady: proposal.WeaponReady,
		Cooldown: proposal.Cooldown, Attempted: proposal.Attempted,
		TimerWrite: proposal.TimerWrite, Succeeded: proposal.Succeeded,
		Chance: proposal.Chance, Roll: proposal.Roll, WaitSeconds: proposal.WaitSeconds,
		StatDelta: proposal.StatDelta, ThacoDelta: proposal.ThacoDelta,
		RoomID: actor.Body.RoomID, ActorID: proposal.ActorID, ActorName: actor.Body.Name,
	}
	if proposal.Kind == PowerAbility {
		result.PreviousStat = actor.Body.Stats[0]
		result.NewStat = nextBody.Stats[0]
	} else {
		result.PreviousThaco = actor.Body.Thaco
		result.NewThaco = nextBody.Thaco
	}
	if proposal.Broadcast {
		event := PowerAccuracyEvent{
			Kind: proposal.Kind, RoomID: actor.Body.RoomID, ActorID: proposal.ActorID,
			ActorName: actor.Body.Name, ExcludeActorID: proposal.ActorID,
			Succeeded: proposal.Succeeded,
			Text:      powerAccuracyEventText(proposal.Kind, proposal.Succeeded, actor.Body.Name),
		}
		result.Event = &event
		result.EventText = event.Text
	}
	return result
}

// planPowerAccuracy is the common pure planner. It follows command9.c's
// source order: permission, active state, accurate's weapon gate, cooldown,
// chance, then one random draw.
func (s State) planPowerAccuracy(kind PowerAccuracyKind, actorID string, now int32, roll func(int, int) int) (PowerAccuracyProposal, error) {
	if err := s.Validate(); err != nil {
		return PowerAccuracyProposal{}, err
	}
	if !validPowerAccuracyKind(kind) || now < 0 {
		return PowerAccuracyProposal{}, fmt.Errorf("invalid power/accurate ability or clock")
	}
	actor, err := powerAccuracyActor(s, actorID)
	if err != nil {
		return PowerAccuracyProposal{}, err
	}
	authorized, err := powerAccuracyAuthorized(kind, actor.Body.Class)
	if err != nil {
		return PowerAccuracyProposal{}, err
	}
	proposal := PowerAccuracyProposal{
		Kind: kind, ActorID: actorID, RoomID: actor.Body.RoomID, Now: now,
		Authorized: authorized, expectedActor: clonePrepareUpDmgPlayer(actor),
	}
	if !authorized {
		proposal.Response = powerAccuracyUnauthorizedResponse(kind)
		return proposal, nil
	}
	flagIndex, err := powerAccuracyFlag(kind)
	if err != nil {
		return PowerAccuracyProposal{}, err
	}
	if flag(actor.Body.Flags[:], uint(flagIndex)) {
		proposal.Active, proposal.AlreadyActive = true, true
		proposal.Response = powerAccuracyActiveResponse(kind)
		return proposal, nil
	}
	if kind == AccurateAbility {
		proposal.WeaponReady, proposal.weaponID, err = powerAccuracyHasWeapon(actor)
		if err != nil {
			return PowerAccuracyProposal{}, err
		}
		if !proposal.WeaponReady {
			proposal.Response = powerAccuracyNoWeaponResponse()
			return proposal, nil
		}
	}
	timerIndex, err := powerAccuracyTimer(kind)
	if err != nil {
		return PowerAccuracyProposal{}, err
	}
	timer := actor.Body.Timers[timerIndex]
	if err := powerAccuracyTimerValid(timer); err != nil {
		return PowerAccuracyProposal{}, err
	}
	proposal.expectedTimer = timer
	proposal.PreviousInterval = timer.Interval
	cooldown, err := powerAccuracyCooldown(kind)
	if err != nil {
		return PowerAccuracyProposal{}, err
	}
	elapsed := int64(now) - int64(timer.LastTime)
	if elapsed < cooldown {
		wait := cooldown - elapsed
		proposal.Cooldown = true
		proposal.WaitSeconds = int32(wait)
		proposal.Response, err = powerAccuracyCooldownResponse(wait)
		if err != nil {
			return PowerAccuracyProposal{}, err
		}
		return proposal, nil
	}
	proposal.Chance, err = powerAccuracyChance(actor.Body)
	if err != nil {
		return PowerAccuracyProposal{}, err
	}
	proposal.Roll, err = powerAccuracyRoll(roll)
	if err != nil {
		return PowerAccuracyProposal{}, err
	}
	proposal.Attempted, proposal.Changed, proposal.TimerWrite, proposal.Broadcast = true, true, true, true
	proposal.Succeeded = proposal.Roll <= proposal.Chance
	proposal.Activate = proposal.Succeeded
	if proposal.Succeeded {
		proposal.Interval, err = powerAccuracyInterval(kind, actor.Body)
		if err != nil {
			return PowerAccuracyProposal{}, err
		}
		if kind == PowerAbility {
			if _, err := powerAccuracyStatAfter(actor.Body); err != nil {
				return PowerAccuracyProposal{}, err
			}
			proposal.StatDelta = 3
		} else {
			if _, err := powerAccuracySignedByte(actor.Body.Thaco, -3); err != nil {
				return PowerAccuracyProposal{}, err
			}
			proposal.ThacoDelta = -3
		}
		proposal.Response = powerAccuracySuccessResponse(kind)
	} else {
		failureLast := int64(now) - failureLastOffset
		if failureLast < math.MinInt32 || failureLast > math.MaxInt32 {
			return PowerAccuracyProposal{}, fmt.Errorf("power/accurate failure timer overflow")
		}
		proposal.FailureLastTime = int32(failureLast)
		proposal.Interval = timer.Interval
		proposal.Response = powerAccuracyFailureResponse(kind)
	}
	proposal.Armor, err = upDmgArmor(actor)
	if err != nil {
		return PowerAccuracyProposal{}, err
	}
	return proposal, nil
}

// PlanPower computes a deterministic command9.c power attempt.
func (s State) PlanPower(actorID string, now int32, roll func(int, int) int) (PowerProposal, error) {
	return s.planPowerAccuracy(PowerAbility, actorID, now, roll)
}

// PlanAccurate computes a deterministic command9.c accurate attempt.
func (s State) PlanAccurate(actorID string, now int32, roll func(int, int) int) (AccurateProposal, error) {
	return s.planPowerAccuracy(AccurateAbility, actorID, now, roll)
}

// Power and Accurate are concise operation-shaped aliases.
func (s State) Power(actorID string, now int32, roll func(int, int) int) (PowerProposal, error) {
	return s.PlanPower(actorID, now, roll)
}

func (s State) Accurate(actorID string, now int32, roll func(int, int) int) (AccurateProposal, error) {
	return s.PlanAccurate(actorID, now, roll)
}

// ApplyPowerAccuracy revalidates all gates and commits one complete candidate.
// It never calls the random source: Roll is part of the proposal/receipt.
func (s State) ApplyPowerAccuracy(proposal PowerAccuracyProposal) (State, PowerAccuracyResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, PowerAccuracyResult{}, err
	}
	if !validPowerAccuracyKind(proposal.Kind) || proposal.ActorID == "" || proposal.Now < 0 || proposal.Response == "" {
		return State{}, PowerAccuracyResult{}, fmt.Errorf("invalid power/accurate proposal")
	}
	actor, err := powerAccuracyActor(s, proposal.ActorID)
	if err != nil || actor.Body.RoomID != proposal.RoomID {
		return State{}, PowerAccuracyResult{}, fmt.Errorf("power/accurate actor changed")
	}
	if !reflect.DeepEqual(actor, proposal.expectedActor) {
		return State{}, PowerAccuracyResult{}, fmt.Errorf("stale power/accurate actor")
	}
	authorized, err := powerAccuracyAuthorized(proposal.Kind, actor.Body.Class)
	if err != nil {
		return State{}, PowerAccuracyResult{}, err
	}
	if proposal.Authorized != authorized {
		return State{}, PowerAccuracyResult{}, fmt.Errorf("power/accurate authorization changed")
	}
	if !authorized {
		if proposal.Response != powerAccuracyUnauthorizedResponse(proposal.Kind) || proposal.Active || proposal.AlreadyActive || proposal.WeaponReady || proposal.Cooldown || proposal.Attempted || proposal.Succeeded || proposal.Activate || proposal.Changed || proposal.TimerWrite || proposal.Broadcast || proposal.Chance != 0 || proposal.Roll != 0 || proposal.WaitSeconds != 0 || proposal.Interval != 0 || proposal.PreviousInterval != 0 || proposal.StatDelta != 0 || proposal.ThacoDelta != 0 || proposal.Armor != 0 {
			return State{}, PowerAccuracyResult{}, fmt.Errorf("stale unauthorized power/accurate proposal")
		}
		return s, powerAccuracyResult(proposal, actor, actor.Body), nil
	}
	flagIndex, err := powerAccuracyFlag(proposal.Kind)
	if err != nil {
		return State{}, PowerAccuracyResult{}, err
	}
	active := flag(actor.Body.Flags[:], uint(flagIndex))
	if proposal.AlreadyActive {
		if !proposal.Active || !active || proposal.WeaponReady || proposal.Cooldown || proposal.Attempted || proposal.Succeeded || proposal.Activate || proposal.Changed || proposal.TimerWrite || proposal.Broadcast || proposal.Roll != 0 || proposal.Chance != 0 || proposal.WaitSeconds != 0 || proposal.Interval != 0 || proposal.PreviousInterval != 0 || proposal.StatDelta != 0 || proposal.ThacoDelta != 0 || proposal.Armor != 0 || proposal.Response != powerAccuracyActiveResponse(proposal.Kind) {
			return State{}, PowerAccuracyResult{}, fmt.Errorf("stale active power/accurate proposal")
		}
		return s, powerAccuracyResult(proposal, actor, actor.Body), nil
	}
	if active || proposal.Active {
		return State{}, PowerAccuracyResult{}, fmt.Errorf("power/accurate ability became active")
	}
	if proposal.Kind == AccurateAbility {
		weaponReady, weaponID, err := powerAccuracyHasWeapon(actor)
		if err != nil {
			return State{}, PowerAccuracyResult{}, err
		}
		if proposal.WeaponReady != weaponReady || proposal.weaponID != weaponID {
			return State{}, PowerAccuracyResult{}, fmt.Errorf("stale accurate weapon proposal")
		}
		if !weaponReady {
			if proposal.Response != powerAccuracyNoWeaponResponse() || proposal.Cooldown || proposal.Attempted || proposal.Succeeded || proposal.Activate || proposal.Changed || proposal.TimerWrite || proposal.Broadcast || proposal.Roll != 0 || proposal.Chance != 0 || proposal.WaitSeconds != 0 || proposal.Interval != 0 || proposal.PreviousInterval != 0 || proposal.StatDelta != 0 || proposal.ThacoDelta != 0 || proposal.Armor != 0 {
				return State{}, PowerAccuracyResult{}, fmt.Errorf("stale no-weapon accurate proposal")
			}
			return s, powerAccuracyResult(proposal, actor, actor.Body), nil
		}
	} else if proposal.WeaponReady || proposal.weaponID != "" {
		return State{}, PowerAccuracyResult{}, fmt.Errorf("stale power weapon fields")
	}
	timerIndex, err := powerAccuracyTimer(proposal.Kind)
	if err != nil {
		return State{}, PowerAccuracyResult{}, err
	}
	timer := actor.Body.Timers[timerIndex]
	if err := powerAccuracyTimerValid(timer); err != nil {
		return State{}, PowerAccuracyResult{}, err
	}
	if timer != proposal.expectedTimer || proposal.PreviousInterval != timer.Interval {
		return State{}, PowerAccuracyResult{}, fmt.Errorf("stale power/accurate timer proposal")
	}
	cooldown, err := powerAccuracyCooldown(proposal.Kind)
	if err != nil {
		return State{}, PowerAccuracyResult{}, err
	}
	elapsed := int64(proposal.Now) - int64(timer.LastTime)
	if proposal.Cooldown {
		wait := cooldown - elapsed
		response, waitErr := powerAccuracyCooldownResponse(wait)
		if waitErr != nil || elapsed >= cooldown || proposal.WaitSeconds != int32(wait) || proposal.Response != response || proposal.Active || proposal.Attempted || proposal.Succeeded || proposal.Activate || proposal.Changed || proposal.TimerWrite || proposal.Broadcast || proposal.Roll != 0 || proposal.Chance != 0 || proposal.Interval != 0 || proposal.StatDelta != 0 || proposal.ThacoDelta != 0 || proposal.Armor != 0 {
			return State{}, PowerAccuracyResult{}, fmt.Errorf("stale power/accurate cooldown proposal")
		}
		return s, powerAccuracyResult(proposal, actor, actor.Body), nil
	}
	chance, err := powerAccuracyChance(actor.Body)
	if err != nil {
		return State{}, PowerAccuracyResult{}, err
	}
	if elapsed < cooldown || proposal.WaitSeconds != 0 || proposal.Chance != chance || proposal.Roll < 1 || proposal.Roll > 100 || !proposal.Attempted || !proposal.Changed || !proposal.TimerWrite || !proposal.Broadcast || proposal.Activate != proposal.Succeeded {
		return State{}, PowerAccuracyResult{}, fmt.Errorf("power/accurate cooldown or roll changed")
	}
	if proposal.Succeeded != (proposal.Roll <= proposal.Chance) {
		return State{}, PowerAccuracyResult{}, fmt.Errorf("power/accurate roll mismatch")
	}
	if proposal.Succeeded {
		interval, intervalErr := powerAccuracyInterval(proposal.Kind, actor.Body)
		if intervalErr != nil || proposal.Interval != interval || proposal.Response != powerAccuracySuccessResponse(proposal.Kind) {
			return State{}, PowerAccuracyResult{}, fmt.Errorf("invalid power/accurate success proposal")
		}
		if proposal.Kind == PowerAbility {
			if proposal.StatDelta != 3 || proposal.ThacoDelta != 0 {
				return State{}, PowerAccuracyResult{}, fmt.Errorf("invalid power stat proposal")
			}
		} else if proposal.ThacoDelta != -3 || proposal.StatDelta != 0 {
			return State{}, PowerAccuracyResult{}, fmt.Errorf("invalid accurate thaco proposal")
		}
	} else {
		failureLast := int64(proposal.Now) - failureLastOffset
		if failureLast < math.MinInt32 || failureLast > math.MaxInt32 || proposal.FailureLastTime != int32(failureLast) || proposal.Interval != timer.Interval || proposal.Response != powerAccuracyFailureResponse(proposal.Kind) || proposal.StatDelta != 0 || proposal.ThacoDelta != 0 {
			return State{}, PowerAccuracyResult{}, fmt.Errorf("invalid power/accurate failure proposal")
		}
	}
	next := s.clone()
	nextActor := next.Players[proposal.ActorID]
	if proposal.Succeeded {
		if proposal.Kind == PowerAbility {
			newStat, statErr := powerAccuracyStatAfter(nextActor.Body)
			if statErr != nil {
				return State{}, PowerAccuracyResult{}, statErr
			}
			nextActor.Body.Stats[0] = newStat
		} else {
			newThaco, thacoErr := powerAccuracySignedByte(nextActor.Body.Thaco, -3)
			if thacoErr != nil {
				return State{}, PowerAccuracyResult{}, thacoErr
			}
			nextActor.Body.Thaco = newThaco
		}
		nextActor.Body.Flags[flagIndex/8] |= 1 << (flagIndex % 8)
		nextActor.Body.Timers[timerIndex] = LegacyTimer{LastTime: proposal.Now, Interval: proposal.Interval}
	} else {
		nextActor.Body.Timers[timerIndex].LastTime = proposal.FailureLastTime
		// C intentionally retains the previous interval after failure.
	}
	armor, err := upDmgArmor(nextActor)
	if err != nil {
		return State{}, PowerAccuracyResult{}, err
	}
	if proposal.Armor != armor {
		return State{}, PowerAccuracyResult{}, fmt.Errorf("stale power/accurate armor proposal")
	}
	nextActor.Body.Armor = armor
	next.Players[proposal.ActorID] = nextActor
	if err := next.Validate(); err != nil {
		return State{}, PowerAccuracyResult{}, err
	}
	result := powerAccuracyResult(proposal, actor, nextActor.Body)
	result.Active = flag(nextActor.Body.Flags[:], uint(flagIndex))
	return next, result, nil
}

func (s State) ApplyPower(proposal PowerProposal) (State, PowerResult, error) {
	if proposal.Kind == "" {
		proposal.Kind = PowerAbility
	}
	if proposal.Kind != PowerAbility {
		return State{}, PowerResult{}, fmt.Errorf("not a power proposal")
	}
	return s.ApplyPowerAccuracy(proposal)
}

func (s State) ApplyAccurate(proposal AccurateProposal) (State, AccurateResult, error) {
	if proposal.Kind == "" {
		proposal.Kind = AccurateAbility
	}
	if proposal.Kind != AccurateAbility {
		return State{}, AccurateResult{}, fmt.Errorf("not an accurate proposal")
	}
	return s.ApplyPowerAccuracy(proposal)
}

// PlanPowerAccuracy and ApplyPowerAccuracy are the kind-oriented aliases for
// callers that dispatch a closed operation enum instead of a named method.
func (s State) PlanPowerAccuracy(kind PowerAccuracyKind, actorID string, now int32, roll func(int, int) int) (PowerAccuracyProposal, error) {
	return s.planPowerAccuracy(kind, actorID, now, roll)
}

func powerAccuracyEvent(s State, kind PowerAccuracyKind, actorID string, succeeded bool) (PowerAccuracyEvent, bool, error) {
	if !validPowerAccuracyKind(kind) {
		return PowerAccuracyEvent{}, false, fmt.Errorf("unknown power/accurate ability %q", kind)
	}
	if err := s.Validate(); err != nil {
		return PowerAccuracyEvent{}, false, err
	}
	actor, err := powerAccuracyActor(s, actorID)
	if err != nil {
		return PowerAccuracyEvent{}, false, err
	}
	return PowerAccuracyEvent{Kind: kind, RoomID: actor.Body.RoomID, ActorID: actorID, ActorName: actor.Body.Name, ExcludeActorID: actorID, Succeeded: succeeded, Text: powerAccuracyEventText(kind, succeeded, actor.Body.Name)}, true, nil
}

// RoomPowerEvent and RoomAccurateEvent derive only the committed room
// projection. A caller should prefer the event embedded in the first receipt
// and never re-derive it on replay.
func (s State) RoomPowerEvent(actorID string, succeeded ...bool) (PowerEvent, bool, error) {
	if len(succeeded) > 1 {
		return PowerEvent{}, false, fmt.Errorf("power event accepts at most one outcome")
	}
	outcome := false
	if len(succeeded) == 1 {
		outcome = succeeded[0]
	} else {
		actor, err := powerAccuracyActor(s, actorID)
		if err != nil {
			return PowerEvent{}, false, err
		}
		outcome = flag(actor.Body.Flags[:], powerFlag)
	}
	return powerAccuracyEvent(s, PowerAbility, actorID, outcome)
}

func (s State) RoomAccurateEvent(actorID string, succeeded ...bool) (AccurateEvent, bool, error) {
	if len(succeeded) > 1 {
		return AccurateEvent{}, false, fmt.Errorf("accurate event accepts at most one outcome")
	}
	outcome := false
	if len(succeeded) == 1 {
		outcome = succeeded[0]
	} else {
		actor, err := powerAccuracyActor(s, actorID)
		if err != nil {
			return AccurateEvent{}, false, err
		}
		outcome = flag(actor.Body.Flags[:], accurateFlag)
	}
	return powerAccuracyEvent(s, AccurateAbility, actorID, outcome)
}

func (s State) RoomPowerAccuracyEvent(kind PowerAccuracyKind, actorID string, succeeded bool) (PowerAccuracyEvent, bool, error) {
	return powerAccuracyEvent(s, kind, actorID, succeeded)
}
