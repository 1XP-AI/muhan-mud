package world

import (
	"fmt"
)

// These values are the player flag and last-time slots used by
// src/mtype.h/command9.c.  They are kept next to the reducer so a caller
// cannot accidentally use a transport-local lockout in place of the legacy
// state.
const (
	hasteFlag       = 19 // PHASTE
	hasteTimerIndex = 16 // LT_HASTE
	prayFlag        = 22 // PPRAYD
	prayTimerIndex  = 19 // LT_PRAYD

	rangerClass     = 7 // RANGER
	clericClass     = 3 // CLERIC
	paladinClass    = 6 // PALADIN
	invincibleClass = 9 // INVINCIBLE
	maxLegacyClass  = 12
)

// Export the source slots for expiry/tick code and contract tests.  The
// lower-case constants above remain the implementation names used by this
// reducer.
const (
	HasteFlag       = hasteFlag
	HasteTimerIndex = hasteTimerIndex
	PrayFlag        = prayFlag
	PrayTimerIndex  = prayTimerIndex
	RangerClass     = rangerClass
	ClericClass     = clericClass
	PaladinClass    = paladinClass
	InvincibleClass = invincibleClass
)

// RangerPrayKind identifies one of command9.c's two self-only abilities.
// The string values are also persisted in the typed command result.
type RangerPrayKind string

const (
	RangerHaste RangerPrayKind = "haste"
	RangerPray  RangerPrayKind = "pray"
	// Descriptive aliases make the contract clear to callers that prefer an
	// operation-shaped name.
	HasteKind RangerPrayKind = RangerHaste
	PrayKind  RangerPrayKind = RangerPray
)

// RangerPrayProposal is a snapshot-bound candidate for haste or prayer.
// Proposals are never accepted from a client.  In particular, Attempted and
// Succeeded are the result of the injected 1..100 draw and are carried into
// ApplyRangerPray so a replay never calls the random source again.
type RangerPrayProposal struct {
	Kind        RangerPrayKind
	ActorID     string
	RoomID      int16
	Now         int32
	Chance      int
	Interval    int32
	WaitSeconds int32

	Authorized    bool
	Active        bool
	AlreadyActive bool
	Cooldown      bool
	Attempted     bool
	Succeeded     bool
	Activate      bool
	Changed       bool
	Roll          int
	TimerWrite    bool
	Broadcast     bool
	Response      string

	expectedTimer LegacyTimer
}

// Operation-specific aliases keep the two command APIs easy to discover while
// sharing one atomic implementation and one receipt envelope.
type HasteProposal = RangerPrayProposal
type PrayProposal = RangerPrayProposal

// RangerPrayResult is the typed actor response persisted in ExecuteGame's
// receipt.  Broadcast is only a committed result projection; this reducer
// never sends to a socket.  RoomID/ActorName/EventText let a transport derive
// the room event from the receipt without rerolling or re-evaluating state.
type RangerPrayResult struct {
	Kind          RangerPrayKind   `json:"kind"`
	Response      string           `json:"response"`
	Broadcast     bool             `json:"broadcast"`
	Succeeded     bool             `json:"succeeded"`
	Attempted     bool             `json:"attempted"`
	Authorized    bool             `json:"authorized"`
	Active        bool             `json:"active"`
	AlreadyActive bool             `json:"already_active,omitempty"`
	Cooldown      bool             `json:"cooldown"`
	Changed       bool             `json:"changed"`
	Chance        int              `json:"chance,omitempty"`
	Roll          int              `json:"roll,omitempty"`
	TimerWrite    bool             `json:"timer_write"`
	WaitSeconds   int32            `json:"wait_seconds,omitempty"`
	RoomID        int16            `json:"room_id,omitempty"`
	ActorID       string           `json:"actor_id,omitempty"`
	ActorName     string           `json:"actor_name,omitempty"`
	EventText     string           `json:"event_text,omitempty"`
	Event         *RangerPrayEvent `json:"event,omitempty"`
}

type HasteResult = RangerPrayResult
type PrayResult = RangerPrayResult

// RangerPrayEvent is the committed room projection.  The actor already gets
// Result.Response; ExcludeActorID prevents a second copy at transport time.
type RangerPrayEvent struct {
	Kind           RangerPrayKind `json:"kind"`
	RoomID         int16          `json:"room_id"`
	ActorID        string         `json:"actor_id"`
	ActorName      string         `json:"actor_name"`
	ExcludeActorID string         `json:"exclude_actor_id"`
	Succeeded      bool           `json:"succeeded"`
	Text           string         `json:"text"`
}

type HasteEvent = RangerPrayEvent
type PrayEvent = RangerPrayEvent

func validRangerPrayKind(kind RangerPrayKind) bool {
	return kind == RangerHaste || kind == RangerPray
}

func rangerPrayFlag(kind RangerPrayKind) (int, error) {
	switch kind {
	case RangerHaste:
		return hasteFlag, nil
	case RangerPray:
		return prayFlag, nil
	default:
		return 0, fmt.Errorf("unknown ranger/pray ability %q", kind)
	}
}

func rangerPrayTimerIndex(kind RangerPrayKind) (int, error) {
	switch kind {
	case RangerHaste:
		return hasteTimerIndex, nil
	case RangerPray:
		return prayTimerIndex, nil
	default:
		return 0, fmt.Errorf("unknown ranger/pray ability %q", kind)
	}
}

func rangerPrayStatIndex(kind RangerPrayKind) (int, error) {
	switch kind {
	case RangerHaste:
		return 1, nil // DEX
	case RangerPray:
		return 4, nil // PTY
	default:
		return 0, fmt.Errorf("unknown ranger/pray ability %q", kind)
	}
}

func rangerPrayAuthorized(kind RangerPrayKind, class byte) (bool, error) {
	if class > maxLegacyClass {
		return false, fmt.Errorf("ranger/pray actor class outside legacy table")
	}
	switch kind {
	case RangerHaste:
		return class == rangerClass || class >= invincibleClass, nil
	case RangerPray:
		return class == clericClass || class == paladinClass || class >= invincibleClass, nil
	default:
		return false, fmt.Errorf("unknown ranger/pray ability %q", kind)
	}
}

func rangerPrayChance(kind RangerPrayKind, body LegacyMonster) (int, error) {
	if body.Class > maxLegacyClass {
		return 0, fmt.Errorf("ranger/pray actor class outside legacy table")
	}
	statIndex, err := rangerPrayStatIndex(kind)
	if err != nil {
		return 0, err
	}
	if body.Stats[statIndex] > 63 {
		return 0, fmt.Errorf("ranger/pray stat outside legacy bonus table")
	}
	levelBand := (int(body.Level) + 3) / 4
	chance := levelBand*20 + legacyStatBonus[body.Stats[statIndex]]
	if chance > 85 {
		chance = 85
	}
	return chance, nil
}

// HasteChance and PrayChance expose the exact command9.c chance calculation
// while retaining the same fail-closed legacy bounds as the reducer.
func HasteChance(body LegacyMonster) (int, error) {
	return rangerPrayChance(RangerHaste, body)
}

func PrayChance(body LegacyMonster) (int, error) {
	return rangerPrayChance(RangerPray, body)
}

func rangerPrayInterval(kind RangerPrayKind, body LegacyMonster) (int32, error) {
	if body.Class > maxLegacyClass {
		return 0, fmt.Errorf("ranger/pray actor class outside legacy table")
	}
	switch kind {
	case RangerHaste:
		levelBand := (int(body.Level) + 3) / 4
		return int32(120 + 60*(levelBand/5)), nil
	case RangerPray:
		return 300, nil
	default:
		return 0, fmt.Errorf("unknown ranger/pray ability %q", kind)
	}
}

func rangerPrayWaitResponse(wait int64) (string, error) {
	if wait < 1 || wait > int64(^uint32(0)>>1) {
		return "", fmt.Errorf("ranger/pray cooldown overflow")
	}
	return fmt.Sprintf("%d분 %02d초 기다리세요.\n", wait/60, wait%60), nil
}

func rangerPrayRoll(roll func(int, int) int, chance int) (bool, int, error) {
	if roll == nil {
		return false, 0, fmt.Errorf("missing ranger/pray random source")
	}
	value, err := func() (value int, err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				err = fmt.Errorf("ranger/pray random source panicked: %v", recovered)
			}
		}()
		return roll(1, 100), nil
	}()
	if err != nil {
		return false, 0, err
	}
	if value < 1 || value > 100 {
		return false, 0, fmt.Errorf("ranger/pray random value outside 1..100")
	}
	return value <= chance, value, nil
}

func rangerPrayUnauthorizedResponse(kind RangerPrayKind) string {
	if kind == RangerHaste {
		return "포졸만 사용할 수 있는 기술입니다.\n"
	}
	return "불제자와 무사만이 신께 기원할 수 있습니다.\n"
}

func rangerPrayActiveResponse(kind RangerPrayKind) string {
	if kind == RangerHaste {
		return "당신은 지금 활보법을 사용중입니다.\n"
	}
	return "당신은 이미 신에게 빌었습니다.\n"
}

func rangerPraySuccessResponse(kind RangerPrayKind) string {
	if kind == RangerHaste {
		return "당신의 동작이 좀더 민첩해진것 같습니다.\n"
	}
	return "당신은 매우 신앙심이 깊어지는 것을 느낄 수 있습니다.\n"
}

func rangerPrayFailureResponse(kind RangerPrayKind) string {
	if kind == RangerHaste {
		return "활보법이 실패하였습니다.\n"
	}
	return "당신의 기원에 대한 신의 응답이 없습니다.\n"
}

func rangerPrayActor(s State, actorID string) (PlayerState, error) {
	if actorID == "" {
		return PlayerState{}, fmt.Errorf("ranger/pray actor absent")
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Body.Type != 0 || actor.Body.Name == "" {
		return PlayerState{}, fmt.Errorf("online canonical ranger/pray actor absent")
	}
	// State.Validate has already checked room membership, so checking the
	// canonical map/online/type here never falls back to room.Resource.Monsters.
	return actor, nil
}

// PlanRangerPray computes a bounded command9.c haste/pray attempt.  The
// random source is called exactly once only after authorization, active-state,
// and source cooldown gates pass.  Unauthorized, active, and cooldown
// outcomes are durable no-op proposals; malformed legacy values are errors.
func (s State) PlanRangerPray(kind RangerPrayKind, actorID string, now int32, roll func(int, int) int) (RangerPrayProposal, error) {
	if err := s.Validate(); err != nil {
		return RangerPrayProposal{}, err
	}
	if !validRangerPrayKind(kind) || now < 0 {
		return RangerPrayProposal{}, fmt.Errorf("invalid ranger/pray ability or clock")
	}
	actor, err := rangerPrayActor(s, actorID)
	if err != nil {
		return RangerPrayProposal{}, err
	}
	authorized, err := rangerPrayAuthorized(kind, actor.Body.Class)
	if err != nil {
		return RangerPrayProposal{}, err
	}
	proposal := RangerPrayProposal{Kind: kind, ActorID: actorID, RoomID: actor.Body.RoomID, Now: now, Authorized: authorized}
	if !authorized {
		proposal.Response = rangerPrayUnauthorizedResponse(kind)
		return proposal, nil
	}
	chance, err := rangerPrayChance(kind, actor.Body)
	if err != nil {
		return RangerPrayProposal{}, err
	}
	interval, err := rangerPrayInterval(kind, actor.Body)
	if err != nil {
		return RangerPrayProposal{}, err
	}
	timerIndex, err := rangerPrayTimerIndex(kind)
	if err != nil {
		return RangerPrayProposal{}, err
	}
	timer := actor.Body.Timers[timerIndex]
	if timer.LastTime < 0 || timer.Interval < 0 {
		return RangerPrayProposal{}, fmt.Errorf("ranger/pray timer outside legacy range")
	}
	proposal.Chance = chance
	proposal.Interval = interval
	proposal.expectedTimer = timer
	if flag(actor.Body.Flags[:], uint(mustRangerPrayFlag(kind))) {
		proposal.Active = true
		proposal.AlreadyActive = true
		proposal.Response = rangerPrayActiveResponse(kind)
		return proposal, nil
	}
	// command9.c's cooldown uses LT(...).ltime + 600, not the timer interval.
	// Widen before subtracting so malformed int32 combinations cannot wrap.
	diff := int64(now) - int64(timer.LastTime)
	if diff < 600 {
		wait := int64(600) - diff
		proposal.Cooldown = true
		proposal.WaitSeconds = int32(wait)
		proposal.Response, err = rangerPrayWaitResponse(wait)
		if err != nil {
			return RangerPrayProposal{}, err
		}
		return proposal, nil
	}
	succeeded, rollValue, err := rangerPrayRoll(roll, chance)
	if err != nil {
		return RangerPrayProposal{}, err
	}
	proposal.Attempted = true
	proposal.Changed = true
	proposal.Roll = rollValue
	proposal.TimerWrite = true
	proposal.Broadcast = true
	proposal.Succeeded = succeeded
	proposal.Activate = succeeded
	if succeeded {
		proposal.Response = rangerPraySuccessResponse(kind)
	} else {
		proposal.Response = rangerPrayFailureResponse(kind)
	}
	return proposal, nil
}

// PlanHaste and PlanPray are operation-specific entry points used by session
// reducers and tests.
func (s State) PlanHaste(actorID string, now int32, roll func(int, int) int) (HasteProposal, error) {
	return s.PlanRangerPray(RangerHaste, actorID, now, roll)
}

func (s State) PlanPray(actorID string, now int32, roll func(int, int) int) (PrayProposal, error) {
	return s.PlanRangerPray(RangerPray, actorID, now, roll)
}

// PlanPrayer is a spelling alias for callers that use the noun form.
func (s State) PlanPrayer(actorID string, now int32, roll func(int, int) int) (PrayProposal, error) {
	return s.PlanPray(actorID, now, roll)
}

func mustRangerPrayFlag(kind RangerPrayKind) int {
	flagIndex, _ := rangerPrayFlag(kind)
	return flagIndex
}

func rangerPrayResult(proposal RangerPrayProposal, actor PlayerState) RangerPrayResult {
	result := RangerPrayResult{
		Kind:          proposal.Kind,
		Response:      proposal.Response,
		Broadcast:     proposal.Broadcast,
		Succeeded:     proposal.Succeeded,
		Attempted:     proposal.Attempted,
		Authorized:    proposal.Authorized,
		Active:        proposal.Active || proposal.Succeeded,
		AlreadyActive: proposal.Active,
		Cooldown:      proposal.Cooldown,
		Changed:       proposal.Changed,
		Chance:        proposal.Chance,
		Roll:          proposal.Roll,
		TimerWrite:    proposal.TimerWrite,
		WaitSeconds:   proposal.WaitSeconds,
		RoomID:        actor.Body.RoomID,
		ActorID:       proposal.ActorID,
		ActorName:     actor.Body.Name,
	}
	if proposal.Broadcast {
		event := RangerPrayEvent{
			Kind:           proposal.Kind,
			RoomID:         actor.Body.RoomID,
			ActorID:        proposal.ActorID,
			ActorName:      actor.Body.Name,
			ExcludeActorID: proposal.ActorID,
			Succeeded:      proposal.Succeeded,
			Text:           rangerPrayEventText(proposal.Kind, proposal.Succeeded, actor.Body.Name),
		}
		result.Event = &event
		result.EventText = event.Text
	}
	return result
}

// ApplyRangerPray revalidates the proposal against the current canonical
// snapshot, then commits all source fields (flag, stat, timer and interval)
// on a cloned candidate.  Failure commits only the source's shortened
// cooldown timestamp and leaves flags/stats/interval unchanged.
func (s State) ApplyRangerPray(proposal RangerPrayProposal) (State, RangerPrayResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, RangerPrayResult{}, err
	}
	if !validRangerPrayKind(proposal.Kind) || proposal.ActorID == "" || proposal.Now < 0 || proposal.Response == "" {
		return State{}, RangerPrayResult{}, fmt.Errorf("invalid ranger/pray proposal")
	}
	actor, err := rangerPrayActor(s, proposal.ActorID)
	if err != nil || actor.Body.RoomID != proposal.RoomID {
		return State{}, RangerPrayResult{}, fmt.Errorf("ranger/pray actor changed")
	}
	authorized, err := rangerPrayAuthorized(proposal.Kind, actor.Body.Class)
	if err != nil {
		return State{}, RangerPrayResult{}, err
	}
	if proposal.Authorized != authorized {
		return State{}, RangerPrayResult{}, fmt.Errorf("ranger/pray authorization changed")
	}
	if !authorized {
		if proposal.Response != rangerPrayUnauthorizedResponse(proposal.Kind) || proposal.Active || proposal.AlreadyActive || proposal.Cooldown || proposal.Attempted || proposal.Succeeded || proposal.Activate || proposal.Changed || proposal.Roll != 0 || proposal.TimerWrite || proposal.Broadcast || proposal.Chance != 0 || proposal.Interval != 0 || proposal.WaitSeconds != 0 {
			return State{}, RangerPrayResult{}, fmt.Errorf("stale unauthorized ranger/pray proposal")
		}
		return s, rangerPrayResult(proposal, actor), nil
	}

	chance, err := rangerPrayChance(proposal.Kind, actor.Body)
	if err != nil {
		return State{}, RangerPrayResult{}, err
	}
	interval, err := rangerPrayInterval(proposal.Kind, actor.Body)
	if err != nil {
		return State{}, RangerPrayResult{}, err
	}
	timerIndex, err := rangerPrayTimerIndex(proposal.Kind)
	if err != nil {
		return State{}, RangerPrayResult{}, err
	}
	flagIndex := mustRangerPrayFlag(proposal.Kind)
	timer := actor.Body.Timers[timerIndex]
	if timer.LastTime < 0 || timer.Interval < 0 {
		return State{}, RangerPrayResult{}, fmt.Errorf("ranger/pray timer outside legacy range")
	}
	if timer != proposal.expectedTimer || proposal.Chance != chance || proposal.Interval != interval {
		return State{}, RangerPrayResult{}, fmt.Errorf("stale ranger/pray proposal")
	}

	active := flag(actor.Body.Flags[:], uint(flagIndex))
	if proposal.Active || proposal.AlreadyActive {
		if !proposal.Active || !proposal.AlreadyActive || !active || proposal.Cooldown || proposal.Attempted || proposal.Succeeded || proposal.Activate || proposal.Changed || proposal.Roll != 0 || proposal.TimerWrite || proposal.Broadcast || proposal.WaitSeconds != 0 || proposal.Response != rangerPrayActiveResponse(proposal.Kind) {
			return State{}, RangerPrayResult{}, fmt.Errorf("stale active ranger/pray proposal")
		}
		return s, rangerPrayResult(proposal, actor), nil
	}
	if active {
		return State{}, RangerPrayResult{}, fmt.Errorf("ranger/pray ability became active")
	}

	diff := int64(proposal.Now) - int64(timer.LastTime)
	if proposal.Cooldown {
		wait := int64(600) - diff
		response, waitErr := rangerPrayWaitResponse(wait)
		if waitErr != nil || diff >= 600 || proposal.WaitSeconds != int32(wait) || proposal.Response != response || proposal.Attempted || proposal.Succeeded || proposal.Activate || proposal.Changed || proposal.Roll != 0 || proposal.TimerWrite || proposal.Broadcast {
			return State{}, RangerPrayResult{}, fmt.Errorf("stale ranger/pray cooldown proposal")
		}
		return s, rangerPrayResult(proposal, actor), nil
	}
	if diff < 600 || proposal.WaitSeconds != 0 || !proposal.Attempted || !proposal.Changed || proposal.Activate != proposal.Succeeded || proposal.Roll < 1 || proposal.Roll > 100 || !proposal.TimerWrite || !proposal.Broadcast {
		return State{}, RangerPrayResult{}, fmt.Errorf("ranger/pray cooldown changed")
	}
	if proposal.Succeeded != (proposal.Roll <= proposal.Chance) {
		return State{}, RangerPrayResult{}, fmt.Errorf("ranger/pray roll mismatch")
	}
	expectedResponse := rangerPrayFailureResponse(proposal.Kind)
	if proposal.Succeeded {
		expectedResponse = rangerPraySuccessResponse(proposal.Kind)
	}
	if proposal.Response != expectedResponse {
		return State{}, RangerPrayResult{}, fmt.Errorf("ranger/pray response mismatch")
	}
	statIndex, err := rangerPrayStatIndex(proposal.Kind)
	if err != nil {
		return State{}, RangerPrayResult{}, err
	}
	boost := byte(5)
	if proposal.Kind == RangerHaste {
		boost = 15
	}
	if proposal.Succeeded && int(actor.Body.Stats[statIndex])+int(boost) > 63 {
		return State{}, RangerPrayResult{}, fmt.Errorf("ranger/pray success leaves stat outside legacy bonus table")
	}

	next := s.clone()
	nextActor := next.Players[proposal.ActorID]
	nextTimer := nextActor.Body.Timers[timerIndex]
	if proposal.Succeeded {
		nextTimer.LastTime = proposal.Now
		nextTimer.Interval = interval
		nextActor.Body.Flags[flagIndex/8] |= 1 << (flagIndex % 8)
		nextActor.Body.Stats[statIndex] += boost
	} else {
		// command9.c deliberately sets only ltime on a failed roll.  Keep the
		// previous interval and all flags/stats byte-for-byte intact.
		nextTimer.LastTime = proposal.Now - 590
	}
	nextActor.Body.Timers[timerIndex] = nextTimer
	next.Players[proposal.ActorID] = nextActor
	if err := next.Validate(); err != nil {
		return State{}, RangerPrayResult{}, err
	}
	result := rangerPrayResult(proposal, nextActor)
	return next, result, nil
}

func (s State) ApplyHaste(proposal HasteProposal) (State, HasteResult, error) {
	if proposal.Kind == "" {
		proposal.Kind = RangerHaste
	}
	if proposal.Kind != RangerHaste {
		return State{}, HasteResult{}, fmt.Errorf("not a haste proposal")
	}
	return s.ApplyRangerPray(proposal)
}

func (s State) ApplyPray(proposal PrayProposal) (State, PrayResult, error) {
	if proposal.Kind == "" {
		proposal.Kind = RangerPray
	}
	if proposal.Kind != RangerPray {
		return State{}, PrayResult{}, fmt.Errorf("not a pray proposal")
	}
	return s.ApplyRangerPray(proposal)
}

func (s State) ApplyPrayer(proposal PrayProposal) (State, PrayResult, error) {
	return s.ApplyPray(proposal)
}

func rangerPrayEventText(kind RangerPrayKind, succeeded bool, actorName string) string {
	if kind == RangerPray {
		return fmt.Sprintf("\n%s님이 신에게 기원합니다.\r\n", actorName)
	}
	if succeeded {
		return fmt.Sprintf("\n%s님이 활보법을 사용하였습니다.\r\n", actorName)
	}
	return fmt.Sprintf("\n%s님이 활보법을 써봅니다.\r\n", actorName)
}

func (s State) roomRangerPrayEvent(kind RangerPrayKind, actorID string, succeeded bool) (RangerPrayEvent, bool, error) {
	if !validRangerPrayKind(kind) {
		return RangerPrayEvent{}, false, fmt.Errorf("unknown ranger/pray ability %q", kind)
	}
	if err := s.Validate(); err != nil {
		return RangerPrayEvent{}, false, err
	}
	actor, err := rangerPrayActor(s, actorID)
	if err != nil {
		return RangerPrayEvent{}, false, err
	}
	return RangerPrayEvent{Kind: kind, RoomID: actor.Body.RoomID, ActorID: actorID, ActorName: actor.Body.Name, ExcludeActorID: actorID, Succeeded: succeeded, Text: rangerPrayEventText(kind, succeeded, actor.Body.Name)}, true, nil
}

// RoomRangerPrayEvent derives only the broadcast projection.  It must be
// called from the first committed result, never from a replayed receipt.
func (s State) RoomRangerPrayEvent(kind RangerPrayKind, actorID string, succeeded bool) (RangerPrayEvent, bool, error) {
	return s.roomRangerPrayEvent(kind, actorID, succeeded)
}

func (s State) RoomHasteEvent(actorID string, succeeded ...bool) (HasteEvent, bool, error) {
	if len(succeeded) > 1 {
		return HasteEvent{}, false, fmt.Errorf("haste event accepts at most one outcome")
	}
	outcome := false
	if len(succeeded) == 1 {
		outcome = succeeded[0]
	} else {
		actor, err := rangerPrayActor(s, actorID)
		if err != nil {
			return HasteEvent{}, false, err
		}
		outcome = flag(actor.Body.Flags[:], uint(hasteFlag))
	}
	return s.roomRangerPrayEvent(RangerHaste, actorID, outcome)
}

func (s State) RoomPrayEvent(actorID string, succeeded ...bool) (PrayEvent, bool, error) {
	if len(succeeded) > 1 {
		return PrayEvent{}, false, fmt.Errorf("pray event accepts at most one outcome")
	}
	outcome := false
	if len(succeeded) == 1 {
		outcome = succeeded[0]
	} else {
		actor, err := rangerPrayActor(s, actorID)
		if err != nil {
			return PrayEvent{}, false, err
		}
		outcome = flag(actor.Body.Flags[:], uint(prayFlag))
	}
	return s.roomRangerPrayEvent(RangerPray, actorID, outcome)
}
