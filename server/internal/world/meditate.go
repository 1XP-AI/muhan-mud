package world

import (
	"fmt"
	"math"
	"reflect"
)

// These are the source indices from src/mtype.h.  They remain legacy state
// indices so expiration/tick code and the command reducer share one authority.
const (
	meditateFlag       = 54 // PMEDIT
	meditateTimerIndex = 39 // LT_MEDIT
	meditateCooldown   = int64(700)
	meditateFailureLag = int64(590)
	meditateStatIndex  = 3 // intelligence
	meditateChanceStat = 4 // piety
)

const (
	MeditateFlag       = meditateFlag
	MeditateTimerIndex = meditateTimerIndex
)

// MeditateProposal is a snapshot-bound candidate for command9.c:meditate.
//
// PlanMeditate is the only place that may consume the injected random draw.
// ApplyMeditate rechecks the complete canonical actor snapshot and all source
// gates before committing, so a proposal cannot be used to write a stale flag,
// stat, or timer.  The random result is carried in the proposal/result rather
// than regenerated during receipt replay.
type MeditateProposal struct {
	ActorID     string
	RoomID      int16
	Now         int32
	Chance      int
	Interval    int32
	WaitSeconds int32

	Authorized      bool
	Active          bool
	AlreadyActive   bool
	Cooldown        bool
	Attempted       bool
	Succeeded       bool
	Activate        bool
	Changed         bool
	TimerWrite      bool
	Broadcast       bool
	Roll            int
	FailureLastTime int32

	Response string

	// The complete player snapshot is private by design.  A caller can carry a
	// proposal between plan and apply, but cannot manufacture authority or a
	// partial actor snapshot by filling a public field.
	expectedActor PlayerState
	expectedTimer LegacyTimer
}

// MeditationProposal is a spelling alias for callers that prefer the noun.
type MeditationProposal = MeditateProposal

// MeditateResult is the durable actor response and committed room projection
// input.  Transport may publish Event after the receipt commits; this package
// never writes to a socket.
type MeditateResult struct {
	Action        string         `json:"action"`
	Response      string         `json:"response"`
	Broadcast     bool           `json:"broadcast"`
	Changed       bool           `json:"changed"`
	Authorized    bool           `json:"authorized"`
	Active        bool           `json:"active"`
	AlreadyActive bool           `json:"already_active,omitempty"`
	Cooldown      bool           `json:"cooldown,omitempty"`
	Attempted     bool           `json:"attempted"`
	Succeeded     bool           `json:"succeeded"`
	TimerWrite    bool           `json:"timer_write"`
	Chance        int            `json:"chance,omitempty"`
	Roll          int            `json:"roll,omitempty"`
	WaitSeconds   int32          `json:"wait_seconds,omitempty"`
	RoomID        int16          `json:"room_id,omitempty"`
	ActorID       string         `json:"actor_id,omitempty"`
	ActorName     string         `json:"actor_name,omitempty"`
	EventText     string         `json:"event_text,omitempty"`
	Event         *MeditateEvent `json:"event,omitempty"`
}

// MeditationResult is a spelling alias for callers that prefer the noun.
type MeditationResult = MeditateResult

// MeditateEvent is the committed room broadcast projection.  The actor is
// excluded because Result.Response is already sent to that connection.
type MeditateEvent struct {
	RoomID         int16  `json:"room_id"`
	ActorID        string `json:"actor_id"`
	ActorName      string `json:"actor_name"`
	ExcludeActorID string `json:"exclude_actor_id"`
	Succeeded      bool   `json:"succeeded"`
	Text           string `json:"text"`
}

// MeditationEvent is a spelling alias for callers that prefer the noun.
type MeditationEvent = MeditateEvent

func meditateAuthorized(class byte) (bool, error) {
	if class > maxLegacyClass {
		return false, fmt.Errorf("meditate actor class outside legacy table")
	}
	return class == clericClass || class == paladinClass || class >= invincibleClass, nil
}

// MeditateChance exposes command9.c's exact chance calculation.  Piety is an
// index into the legacy bonus table; undefined signed/array values fail closed
// instead of being clamped or redirected to another stat.
func MeditateChance(body LegacyMonster) (int, error) {
	if body.Class > maxLegacyClass {
		return 0, fmt.Errorf("meditate actor class outside legacy table")
	}
	if body.Stats[meditateChanceStat] > 63 {
		return 0, fmt.Errorf("meditate actor piety outside legacy bonus table")
	}
	levelBand := (int(body.Level) + 3) / 4
	chance := levelBand*20 + legacyStatBonus[body.Stats[meditateChanceStat]]
	if chance > 85 {
		chance = 85
	}
	return chance, nil
}

// MeditationChance is a spelling alias for MeditateChance.
func MeditationChance(body LegacyMonster) (int, error) { return MeditateChance(body) }

func meditateInterval(body LegacyMonster) (int32, error) {
	if body.Class > maxLegacyClass {
		return 0, fmt.Errorf("meditate actor class outside legacy table")
	}
	levelBand := (int(body.Level) + 3) / 4
	return int32(150 + 60*(levelBand/5)), nil
}

func meditateWaitResponse(wait int64) (string, error) {
	if wait < 1 || wait > math.MaxInt32 {
		return "", fmt.Errorf("meditate cooldown overflow")
	}
	return fmt.Sprintf("%d분 %02d초 기다리세요.\n", wait/60, wait%60), nil
}

func meditateRoll(roll func(int, int) int, chance int) (success bool, value int, err error) {
	if roll == nil {
		return false, 0, fmt.Errorf("missing meditate random source")
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			success = false
			value = 0
			err = fmt.Errorf("meditate random source panicked: %v", recovered)
		}
	}()
	value = roll(1, 100)
	if value < 1 || value > 100 {
		return false, 0, fmt.Errorf("meditate random value outside 1..100")
	}
	return value <= chance, value, nil
}

func meditateActor(s State, actorID string) (PlayerState, error) {
	if actorID == "" {
		return PlayerState{}, fmt.Errorf("empty meditate actor")
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Body.Type != 0 || actor.Body.Name == "" {
		return PlayerState{}, fmt.Errorf("online canonical meditate actor absent")
	}
	// State.Validate establishes room membership.  Deliberately do not inspect
	// room.Resource.Monsters: the online PlayerState map is the sole authority.
	return actor, nil
}

func cloneMeditateActor(actor PlayerState) PlayerState {
	actor.Body.Inventory = cloneObjects(actor.Body.Inventory)
	if actor.Items != nil {
		items := actor.Items.clone()
		actor.Items = &items
	}
	if actor.PlayerEnemies != nil {
		actor.PlayerEnemies = append([]string{}, actor.PlayerEnemies...)
	}
	if actor.FollowerIDs != nil {
		actor.FollowerIDs = append([]string{}, actor.FollowerIDs...)
	}
	if actor.NPCFollowerIDs != nil {
		actor.NPCFollowerIDs = append([]string{}, actor.NPCFollowerIDs...)
	}
	if actor.FollowerRefs != nil {
		actor.FollowerRefs = append([]EntityRef{}, actor.FollowerRefs...)
	}
	return actor
}

func meditateUnauthorizedResponse() string {
	return "무사 불제자만 사용할 수 있는 기술입니다.\n"
}

func meditateActiveResponse() string {
	return "당신은 벌써 참선을 했습니다.\n"
}

func meditateSuccessResponse() string {
	return "당신은 자리에 앉아 참선에 들어갑니다.\n새롭게 사물을 바라보는 눈이 뜨였습니다.\n"
}

func meditateFailureResponse() string {
	return "참선도중 주화입마에 빠졌습니다.\n"
}

func meditateEventText(succeeded bool, actorName string) string {
	if succeeded {
		return fmt.Sprintf("\n%s님이 자리에 앉아 참선을 행합니다.\r\n", actorName)
	}
	return fmt.Sprintf("\n%s님이 참선을 하다가 주화입마에 빠졌습니다.\r\n", actorName)
}

func meditateResult(proposal MeditateProposal, actor PlayerState) MeditateResult {
	result := MeditateResult{
		Action:        "meditate",
		Response:      proposal.Response,
		Broadcast:     proposal.Broadcast,
		Changed:       proposal.Changed,
		Authorized:    proposal.Authorized,
		Active:        proposal.Active || proposal.Succeeded,
		AlreadyActive: proposal.AlreadyActive,
		Cooldown:      proposal.Cooldown,
		Attempted:     proposal.Attempted,
		Succeeded:     proposal.Succeeded,
		TimerWrite:    proposal.TimerWrite,
		Chance:        proposal.Chance,
		Roll:          proposal.Roll,
		WaitSeconds:   proposal.WaitSeconds,
		RoomID:        actor.Body.RoomID,
		ActorID:       proposal.ActorID,
		ActorName:     actor.Body.Name,
	}
	if proposal.Broadcast {
		event := MeditateEvent{
			RoomID:         actor.Body.RoomID,
			ActorID:        proposal.ActorID,
			ActorName:      actor.Body.Name,
			ExcludeActorID: proposal.ActorID,
			Succeeded:      proposal.Succeeded,
			Text:           meditateEventText(proposal.Succeeded, actor.Body.Name),
		}
		result.Event = &event
		result.EventText = event.Text
	}
	return result
}

// PlanMeditate ports command9.c:meditate.  Authorization, active state, and
// the 700-second source cooldown all precede the single injected 1..100 roll.
// No failure or no-op path consumes randomness.
func (s State) PlanMeditate(actorID string, now int32, roll func(int, int) int) (MeditateProposal, error) {
	if err := s.Validate(); err != nil {
		return MeditateProposal{}, err
	}
	if now < 0 {
		return MeditateProposal{}, fmt.Errorf("invalid meditate clock")
	}
	actor, err := meditateActor(s, actorID)
	if err != nil {
		return MeditateProposal{}, err
	}
	proposal := MeditateProposal{
		ActorID:       actorID,
		RoomID:        actor.Body.RoomID,
		Now:           now,
		expectedActor: cloneMeditateActor(actor),
	}
	authorized, err := meditateAuthorized(actor.Body.Class)
	if err != nil {
		return MeditateProposal{}, err
	}
	proposal.Authorized = authorized
	if !authorized {
		proposal.Response = meditateUnauthorizedResponse()
		return proposal, nil
	}

	if flag(actor.Body.Flags[:], meditateFlag) {
		proposal.Active = true
		proposal.AlreadyActive = true
		proposal.Response = meditateActiveResponse()
		return proposal, nil
	}
	timer := actor.Body.Timers[meditateTimerIndex]
	if timer.LastTime < 0 || timer.Interval < 0 {
		return MeditateProposal{}, fmt.Errorf("meditate timer outside legacy range")
	}
	proposal.expectedTimer = timer
	diff := int64(now) - int64(timer.LastTime)
	if diff < meditateCooldown {
		wait := meditateCooldown - diff
		proposal.Response, err = meditateWaitResponse(wait)
		if err != nil {
			return MeditateProposal{}, err
		}
		proposal.Cooldown = true
		proposal.WaitSeconds = int32(wait)
		return proposal, nil
	}
	proposal.Chance, err = MeditateChance(actor.Body)
	if err != nil {
		return MeditateProposal{}, err
	}
	proposal.Interval, err = meditateInterval(actor.Body)
	if err != nil {
		return MeditateProposal{}, err
	}
	proposal.Succeeded, proposal.Roll, err = meditateRoll(roll, proposal.Chance)
	if err != nil {
		return MeditateProposal{}, err
	}
	proposal.Attempted = true
	proposal.Changed = true
	proposal.TimerWrite = true
	proposal.Broadcast = true
	proposal.Activate = proposal.Succeeded
	if proposal.Succeeded {
		// Legacy creature.stats are signed chars.  Reject negative/malformed
		// values and +3 overflow before a candidate can wrap the byte field.
		intelligence := int(int8(actor.Body.Stats[meditateStatIndex]))
		if intelligence < 0 || intelligence > math.MaxInt8-3 {
			return MeditateProposal{}, fmt.Errorf("meditate intelligence overflow")
		}
		proposal.Response = meditateSuccessResponse()
	} else {
		failureLast := int64(now) - meditateFailureLag
		if failureLast < math.MinInt32 || failureLast > math.MaxInt32 {
			return MeditateProposal{}, fmt.Errorf("meditate failure timer overflow")
		}
		proposal.FailureLastTime = int32(failureLast)
		proposal.Interval = timer.Interval
		proposal.Response = meditateFailureResponse()
	}
	return proposal, nil
}

// Meditate is a concise source-named alias for PlanMeditate.
func (s State) Meditate(actorID string, now int32, roll func(int, int) int) (MeditateProposal, error) {
	return s.PlanMeditate(actorID, now, roll)
}

// PlanMeditation is a spelling alias for PlanMeditate.
func (s State) PlanMeditation(actorID string, now int32, roll func(int, int) int) (MeditationProposal, error) {
	return s.PlanMeditate(actorID, now, roll)
}

// ApplyMeditate revalidates the complete canonical actor snapshot and commits
// either the success effect (PMEDIT, intelligence +3, fresh effect timer) or
// the source-compatible failure cooldown (now-590, old interval only).
func (s State) ApplyMeditate(proposal MeditateProposal) (State, MeditateResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, MeditateResult{}, err
	}
	if proposal.ActorID == "" || proposal.Now < 0 || proposal.Response == "" {
		return State{}, MeditateResult{}, fmt.Errorf("invalid meditate proposal")
	}
	actor, err := meditateActor(s, proposal.ActorID)
	if err != nil || actor.Body.RoomID != proposal.RoomID {
		return State{}, MeditateResult{}, fmt.Errorf("meditate actor changed")
	}
	if !reflect.DeepEqual(actor, proposal.expectedActor) {
		return State{}, MeditateResult{}, fmt.Errorf("stale meditate actor")
	}
	authorized, err := meditateAuthorized(actor.Body.Class)
	if err != nil {
		return State{}, MeditateResult{}, err
	}
	if proposal.Authorized != authorized {
		return State{}, MeditateResult{}, fmt.Errorf("meditate authorization changed")
	}
	if !authorized {
		if proposal.Response != meditateUnauthorizedResponse() || proposal.Active || proposal.AlreadyActive || proposal.Cooldown || proposal.Attempted || proposal.Succeeded || proposal.Activate || proposal.Changed || proposal.TimerWrite || proposal.Broadcast || proposal.Chance != 0 || proposal.Roll != 0 || proposal.Interval != 0 || proposal.WaitSeconds != 0 || proposal.FailureLastTime != 0 {
			return State{}, MeditateResult{}, fmt.Errorf("stale unauthorized meditate proposal")
		}
		return s, meditateResult(proposal, actor), nil
	}

	active := flag(actor.Body.Flags[:], meditateFlag)
	if proposal.AlreadyActive {
		if !proposal.Active || !active || proposal.Cooldown || proposal.Attempted || proposal.Succeeded || proposal.Activate || proposal.Changed || proposal.TimerWrite || proposal.Broadcast || proposal.Chance != 0 || proposal.Roll != 0 || proposal.Interval != 0 || proposal.WaitSeconds != 0 || proposal.FailureLastTime != 0 || proposal.Response != meditateActiveResponse() {
			return State{}, MeditateResult{}, fmt.Errorf("stale active meditate proposal")
		}
		return s, meditateResult(proposal, actor), nil
	}
	if active {
		return State{}, MeditateResult{}, fmt.Errorf("meditate active state changed")
	}

	timer := actor.Body.Timers[meditateTimerIndex]
	if timer.LastTime < 0 || timer.Interval < 0 {
		return State{}, MeditateResult{}, fmt.Errorf("meditate timer outside legacy range")
	}
	if timer != proposal.expectedTimer {
		return State{}, MeditateResult{}, fmt.Errorf("stale meditate timer")
	}
	diff := int64(proposal.Now) - int64(timer.LastTime)
	if proposal.Cooldown {
		wait := meditateCooldown - diff
		response, waitErr := meditateWaitResponse(wait)
		if waitErr != nil || diff >= meditateCooldown || proposal.Active || proposal.Attempted || proposal.Succeeded || proposal.Activate || proposal.Changed || proposal.TimerWrite || proposal.Broadcast || proposal.Chance != 0 || proposal.Roll != 0 || proposal.Interval != 0 || proposal.WaitSeconds != int32(wait) || proposal.FailureLastTime != 0 || proposal.Response != response {
			return State{}, MeditateResult{}, fmt.Errorf("stale meditate cooldown proposal")
		}
		return s, meditateResult(proposal, actor), nil
	}
	chance, err := MeditateChance(actor.Body)
	if err != nil {
		return State{}, MeditateResult{}, err
	}
	interval, err := meditateInterval(actor.Body)
	if err != nil {
		return State{}, MeditateResult{}, err
	}
	if proposal.Chance != chance {
		return State{}, MeditateResult{}, fmt.Errorf("stale meditate chance")
	}
	if diff < meditateCooldown || proposal.Cooldown || !proposal.Attempted || !proposal.Changed || !proposal.TimerWrite || !proposal.Broadcast || proposal.WaitSeconds != 0 || proposal.Roll < 1 || proposal.Roll > 100 || proposal.Succeeded != (proposal.Roll <= chance) || proposal.Activate != proposal.Succeeded {
		return State{}, MeditateResult{}, fmt.Errorf("stale meditate roll proposal")
	}
	if proposal.Succeeded {
		if proposal.Interval != interval || proposal.FailureLastTime != 0 || proposal.Response != meditateSuccessResponse() {
			return State{}, MeditateResult{}, fmt.Errorf("stale meditate success proposal")
		}
		intelligence := int(int8(actor.Body.Stats[meditateStatIndex]))
		if intelligence < 0 || intelligence > math.MaxInt8-3 {
			return State{}, MeditateResult{}, fmt.Errorf("meditate intelligence overflow")
		}
	} else {
		failureLast := int64(proposal.Now) - meditateFailureLag
		if failureLast < math.MinInt32 || failureLast > math.MaxInt32 || proposal.FailureLastTime != int32(failureLast) || proposal.Interval != timer.Interval || proposal.Response != meditateFailureResponse() {
			return State{}, MeditateResult{}, fmt.Errorf("stale meditate failure proposal")
		}
	}

	next := s.clone()
	nextActor := next.Players[proposal.ActorID]
	if proposal.Succeeded {
		nextActor.Body.Flags[meditateFlag/8] |= 1 << (meditateFlag % 8)
		intelligence := int(int8(nextActor.Body.Stats[meditateStatIndex])) + 3
		nextActor.Body.Stats[meditateStatIndex] = byte(intelligence)
		nextActor.Body.Timers[meditateTimerIndex] = LegacyTimer{LastTime: proposal.Now, Interval: interval}
	} else {
		nextActor.Body.Timers[meditateTimerIndex].LastTime = proposal.FailureLastTime
		// C deliberately preserves LT_MEDIT.interval on failure.
	}
	next.Players[proposal.ActorID] = nextActor
	if err := next.Validate(); err != nil {
		return State{}, MeditateResult{}, err
	}
	return next, meditateResult(proposal, nextActor), nil
}

// ApplyMeditation is a spelling alias for ApplyMeditate.
func (s State) ApplyMeditation(proposal MeditationProposal) (State, MeditationResult, error) {
	return s.ApplyMeditate(proposal)
}

// RoomMeditateEvent derives the room projection from a committed snapshot.
// Passing the receipt outcome explicitly prevents a later state change from
// changing a persisted success message into a failure message.
func (s State) RoomMeditateEvent(actorID string, succeeded ...bool) (MeditateEvent, bool, error) {
	if len(succeeded) > 1 {
		return MeditateEvent{}, false, fmt.Errorf("meditate event accepts at most one outcome")
	}
	if err := s.Validate(); err != nil {
		return MeditateEvent{}, false, err
	}
	actor, err := meditateActor(s, actorID)
	if err != nil {
		return MeditateEvent{}, false, err
	}
	outcome := flag(actor.Body.Flags[:], meditateFlag)
	if len(succeeded) == 1 {
		outcome = succeeded[0]
	}
	return MeditateEvent{
		RoomID:         actor.Body.RoomID,
		ActorID:        actorID,
		ActorName:      actor.Body.Name,
		ExcludeActorID: actorID,
		Succeeded:      outcome,
		Text:           meditateEventText(outcome, actor.Body.Name),
	}, true, nil
}

// RoomMeditationEvent is a spelling alias for RoomMeditateEvent.
func (s State) RoomMeditationEvent(actorID string, succeeded ...bool) (MeditationEvent, bool, error) {
	return s.RoomMeditateEvent(actorID, succeeded...)
}
