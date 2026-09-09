package world

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Change class is the bounded command7.c:change_class/chg_class_main port
// (global.c cmdno 86).  The old command first validates the actor and room,
// asks for a confirmation, and only then subtracts 100000 experience and
// repeatedly calls down_level after changing class.  Family file updates are
// deliberately not guessed here: State has no canonical family roster.
const (
	changeClassRoomFlag       = trainingRoomFlag // RTRAIN, bit 3
	changeClassBlindFlag      = playerBlindFlag  // PBLIND, bit 42
	changeClassFamilyFlag     = trainingFamilyFlag
	changeClassMaxClass       = 8
	changeClassExperienceCost = int32(100000)
)

const (
	ChangeClassRoomFlag       = changeClassRoomFlag
	ChangeClassBlindFlag      = changeClassBlindFlag
	ChangeClassFamilyFlag     = changeClassFamilyFlag
	ChangeClassMaxClass       = changeClassMaxClass
	ChangeClassExperienceCost = changeClassExperienceCost
)

var (
	ErrChangeClassActorAbsent      = errors.New("change class actor absent")
	ErrChangeClassRoomAbsent       = errors.New("change class room absent")
	ErrChangeClassRoom             = errors.New("change class is unavailable in this room")
	ErrChangeClassBlind            = errors.New("change class is unavailable while blind")
	ErrChangeClassUnsupportedClass = errors.New("change class actor has an unsupported class")
	ErrChangeClassSameClass        = errors.New("change class destination is the current class")
	ErrChangeClassExperience       = errors.New("change class experience is insufficient")
	ErrChangeClassFamilyPending    = errors.New("change class family membership update pending")
	ErrChangeClassStaleProposal    = errors.New("stale or invalid change class proposal")
	ErrChangeClassNumeric          = errors.New("change class numeric state outside canonical range")
	ErrChangeClassConfirmation     = errors.New("change class confirmation is pending")
)

// Descriptive aliases keep command adapters independent of the exact C
// function spelling while retaining one error identity for errors.Is.
var (
	ErrChangeClassTrainingRoom      = ErrChangeClassRoom
	ErrChangeClassNotTrainingRoom   = ErrChangeClassRoom
	ErrChangeClassNotEnoughExp      = ErrChangeClassExperience
	ErrChangeClassInsufficientExp   = ErrChangeClassExperience
	ErrChangeClassCurrentClass      = ErrChangeClassSameClass
	ErrChangeClassSame              = ErrChangeClassSameClass
	ErrChangeClassFamilyEditPending = ErrChangeClassFamilyPending
	ErrChangeClassStale             = ErrChangeClassStaleProposal
)

const (
	// ChangeClassPromptResponse is command7.c:change_class's confirmation
	// continuation text.  It is returned only by the bare terminal form.
	ChangeClassPromptResponse = "직업전환을 하려면 경험치 10만이 필요합니다.\n정말로 직업전환을 하시겠습니까?(예/아니오): "
	// ChangeClassSuccessResponse is command7.c:chg_class_main's own-player
	// response.  The source has no trailing newline on this print.
	ChangeClassSuccessResponse = "\n당신의 직업이 전환되었습니다."
)

// ChangeClassProposal is a snapshot-bound candidate for one confirmation
// step of command7.c.  Confirmed=false is the explicitly bounded bare-command
// prompt: it is a typed no-op and never mutates canonical state.
//
// Before/after stat, vital, and timer projections are included in the
// candidate and receipt so a level-down cannot silently discard those fields.
// Timers are copied unchanged because source down_level does not touch them.
type ChangeClassProposal struct {
	Action    string
	ActorID   string
	ActorName string
	RoomID    int16
	Confirmed bool

	PendingConfirmation bool
	NoOp                bool
	Changed             bool
	Broadcast           bool
	BroadcastPending    bool
	PendingReason       string

	BeforeClass      byte
	Class            byte
	BeforeLevel      byte
	Level            byte
	TargetLevel      int
	LevelsDown       int
	BeforeExperience int32
	Experience       int32
	ExperienceSpent  int32

	BeforeStats  [5]byte
	Stats        [5]byte
	BeforeHPMax  int16
	HPMax        int16
	BeforeHP     int16
	HP           int16
	BeforeMPMax  int16
	MPMax        int16
	BeforeMP     int16
	MP           int16
	BeforeTimers [45]LegacyTimer
	Timers       [45]LegacyTimer

	Response string

	// before is the complete world snapshot on which the candidate was made.
	// It is intentionally private so a client cannot supply a state proposal.
	before            State
	expectedActor     PlayerState
	expectedRoomFlags [8]byte
}

// ChangeClassResult is the durable receipt projection.  No broadcast/event
// is fabricated: command7.c does not call broadcast_all for this operation,
// and family edit_member is rejected before a candidate when PFAMIL is set.
type ChangeClassResult struct {
	Action    string `json:"action"`
	ActorID   string `json:"actor_id"`
	ActorName string `json:"actor_name"`
	RoomID    int16  `json:"room_id"`
	Confirmed bool   `json:"confirmed"`

	PendingConfirmation bool   `json:"pending_confirmation,omitempty"`
	NoOp                bool   `json:"no_op,omitempty"`
	Changed             bool   `json:"changed"`
	Broadcast           bool   `json:"broadcast"`
	BroadcastPending    bool   `json:"broadcast_pending,omitempty"`
	PendingReason       string `json:"pending_reason,omitempty"`

	BeforeClass      byte  `json:"before_class"`
	Class            byte  `json:"class"`
	BeforeLevel      byte  `json:"before_level"`
	Level            byte  `json:"level"`
	TargetLevel      int   `json:"target_level"`
	LevelsDown       int   `json:"levels_down"`
	BeforeExperience int32 `json:"before_experience"`
	Experience       int32 `json:"experience"`
	ExperienceSpent  int32 `json:"experience_spent"`

	BeforeStats  [5]byte         `json:"before_stats"`
	Stats        [5]byte         `json:"stats"`
	BeforeHPMax  int16           `json:"before_hp_max"`
	HPMax        int16           `json:"hp_max"`
	BeforeHP     int16           `json:"before_hp"`
	HP           int16           `json:"hp"`
	BeforeMPMax  int16           `json:"before_mp_max"`
	MPMax        int16           `json:"mp_max"`
	BeforeMP     int16           `json:"before_mp"`
	MP           int16           `json:"mp"`
	BeforeTimers [45]LegacyTimer `json:"before_timers"`
	Timers       [45]LegacyTimer `json:"timers"`

	Response string `json:"response"`
}

// ChangeClassCandidate and ChangeClassOutcome are descriptive compatibility
// aliases used by command adapters that call all reducer values candidates or
// outcomes.
type ChangeClassCandidate = ChangeClassProposal
type ChangeClassOutcome = ChangeClassResult

func validChangeClassName(name string) bool {
	if name == "" || !utf8.ValidString(name) || strings.TrimSpace(name) != name {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

// changeClassDestination reproduces chg_class_main's RTRAIN bit fold. The
// source reads RTRAIN+1 through +3 in increasing order, shifting the
// accumulated value before each bit; the final +1 maps 000 to class one and
// 111 to class eight.
func changeClassDestination(room LegacyRoom) byte {
	var class byte
	for i := 0; i < 3; i++ {
		class *= 2
		if flag(room.Flags[:], uint(changeClassRoomFlag+i+1)) {
			class |= 1
		}
	}
	return class + 1
}

func changeClassActor(s State, actorID string) (PlayerState, RoomState, error) {
	if err := s.Validate(); err != nil {
		return PlayerState{}, RoomState{}, err
	}
	if actorID == "" {
		return PlayerState{}, RoomState{}, ErrChangeClassActorAbsent
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Body.Type != 0 || !validChangeClassName(actor.Body.Name) {
		return PlayerState{}, RoomState{}, ErrChangeClassActorAbsent
	}
	if actor.Body.Level == 0 || actor.Body.Experience < 0 {
		return PlayerState{}, RoomState{}, ErrChangeClassNumeric
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok || room.Resource.ID != actor.Body.RoomID || !containsString(room.PlayerIDs, actorID) {
		return PlayerState{}, RoomState{}, ErrChangeClassRoomAbsent
	}
	return actor, room, nil
}

func changeClassCounts(before, after LegacyMonster) (int, error) {
	if after.Level > before.Level {
		return 0, ErrChangeClassNumeric
	}
	return int(before.Level - after.Level), nil
}

// changeClassCandidate computes the complete state-independent body result.
// It follows source gate order before confirmation and then uses the existing
// source-backed ExperienceLevel and LowerPlayerLevel implementations.
func changeClassCandidate(s State, actorID string, confirmed bool) (ChangeClassProposal, error) {
	actor, room, err := changeClassActor(s, actorID)
	if err != nil {
		return ChangeClassProposal{}, err
	}
	body := actor.Body
	// command7.c checks PBLIND before the room gate.
	if flag(body.Flags[:], uint(changeClassBlindFlag)) {
		return ChangeClassProposal{}, ErrChangeClassBlind
	}
	if !flag(room.Resource.Flags[:], uint(changeClassRoomFlag)) {
		return ChangeClassProposal{}, ErrChangeClassRoom
	}
	destination := changeClassDestination(room.Resource)
	// Source rejects classes above 8. Class zero is also rejected here as a
	// malformed player rather than allowing an undefined legacy table lookup.
	if body.Class == 0 || body.Class > changeClassMaxClass {
		return ChangeClassProposal{}, ErrChangeClassUnsupportedClass
	}
	if destination == body.Class {
		return ChangeClassProposal{}, ErrChangeClassSameClass
	}
	if body.Experience < changeClassExperienceCost {
		return ChangeClassProposal{}, ErrChangeClassExperience
	}
	// chg_class_main calls edit_member(..., 3) for PFAMIL. State has no family
	// file/member projection, so never commit a class that would diverge it.
	if flag(body.Flags[:], uint(changeClassFamilyFlag)) {
		return ChangeClassProposal{}, ErrChangeClassFamilyPending
	}

	beforeActor := cloneChangeClassActor(actor)
	p := ChangeClassProposal{
		Action: "change_class", ActorID: actorID, ActorName: body.Name,
		RoomID: room.Resource.ID, Confirmed: confirmed,
		BeforeClass: body.Class, Class: body.Class,
		BeforeLevel: body.Level, Level: body.Level, TargetLevel: int(body.Level),
		BeforeExperience: body.Experience, Experience: body.Experience,
		BeforeStats: body.Stats, Stats: body.Stats,
		BeforeHPMax: body.HPMax, HPMax: body.HPMax, BeforeHP: body.HPCurrent, HP: body.HPCurrent,
		BeforeMPMax: body.MPMax, MPMax: body.MPMax, BeforeMP: body.MPCurrent, MP: body.MPCurrent,
		BeforeTimers: body.Timers, Timers: body.Timers,
		Response: ChangeClassPromptResponse, before: s.clone(), expectedRoomFlags: room.Resource.Flags,
	}
	if !confirmed {
		p.PendingConfirmation, p.NoOp = true, true
		p.PendingReason = "interactive confirmation continuation is not canonical"
		p.expectedActor = beforeActor
		return p, nil
	}

	changed := body
	changed.Experience -= changeClassExperienceCost
	changed.Class = destination
	target := ExperienceLevel(changed.Experience)
	if target < 1 {
		return ChangeClassProposal{}, ErrChangeClassNumeric
	}
	if int(changed.Level) > target {
		var lowerErr error
		changed, lowerErr = LowerPlayerLevel(changed, target)
		if lowerErr != nil {
			return ChangeClassProposal{}, fmt.Errorf("%w: %v", ErrChangeClassNumeric, lowerErr)
		}
	}
	levelsDown, err := changeClassCounts(body, changed)
	if err != nil {
		return ChangeClassProposal{}, err
	}
	p.Class, p.Level, p.TargetLevel = changed.Class, changed.Level, target
	p.Experience, p.ExperienceSpent = changed.Experience, changeClassExperienceCost
	p.Stats = changed.Stats
	p.HPMax, p.HP = changed.HPMax, changed.HPCurrent
	p.MPMax, p.MP = changed.MPMax, changed.MPCurrent
	p.Timers = changed.Timers
	p.LevelsDown = levelsDown
	p.Changed = true
	p.Response = ChangeClassSuccessResponse
	p.expectedActor = beforeActor
	return p, nil
}

func cloneChangeClassActor(actor PlayerState) PlayerState {
	actor.Aliases = clonePlayerAliases(actor.Aliases)
	actor.PlayerEnemies = append([]string(nil), actor.PlayerEnemies...)
	actor.FollowerIDs = append([]string(nil), actor.FollowerIDs...)
	if actor.NPCFollowerIDs != nil {
		actor.NPCFollowerIDs = append([]string(nil), actor.NPCFollowerIDs...)
	}
	if actor.FollowerRefs != nil {
		actor.FollowerRefs = append([]EntityRef(nil), actor.FollowerRefs...)
	}
	actor.Body.Inventory = cloneObjects(actor.Body.Inventory)
	if actor.Items != nil {
		items := actor.Items.clone()
		actor.Items = &items
	}
	return actor
}

// PlanChangeClass validates the source gates and computes a candidate. With
// no optional confirmation argument it plans the confirmed reducer path; a
// false argument is used by the session adapter for bare `직업전환` and yields
// a typed prompt/no-op candidate. The variadic shape keeps direct callers
// source-oriented without exposing a second reducer.
func (s State) PlanChangeClass(actorID string, confirmation ...bool) (ChangeClassProposal, error) {
	if len(confirmation) > 1 {
		return ChangeClassProposal{}, ErrChangeClassConfirmation
	}
	confirmed := true
	if len(confirmation) == 1 {
		confirmed = confirmation[0]
	}
	return changeClassCandidate(s, actorID, confirmed)
}

// PlanClassChange is a descriptive spelling alias.
func (s State) PlanClassChange(actorID string, confirmation ...bool) (ChangeClassProposal, error) {
	return s.PlanChangeClass(actorID, confirmation...)
}

func changeClassProposalMatches(p, expected ChangeClassProposal) bool {
	// Compare all public fields explicitly. This prevents a caller from
	// tampering with a receipt projection while retaining the same snapshot.
	return p.Action == expected.Action && p.ActorID == expected.ActorID && p.ActorName == expected.ActorName && p.RoomID == expected.RoomID && p.Confirmed == expected.Confirmed &&
		p.PendingConfirmation == expected.PendingConfirmation && p.NoOp == expected.NoOp && p.Changed == expected.Changed && p.Broadcast == expected.Broadcast && p.BroadcastPending == expected.BroadcastPending && p.PendingReason == expected.PendingReason &&
		p.BeforeClass == expected.BeforeClass && p.Class == expected.Class && p.BeforeLevel == expected.BeforeLevel && p.Level == expected.Level && p.TargetLevel == expected.TargetLevel && p.LevelsDown == expected.LevelsDown &&
		p.BeforeExperience == expected.BeforeExperience && p.Experience == expected.Experience && p.ExperienceSpent == expected.ExperienceSpent &&
		p.BeforeStats == expected.BeforeStats && p.Stats == expected.Stats && p.BeforeHPMax == expected.BeforeHPMax && p.HPMax == expected.HPMax && p.BeforeHP == expected.BeforeHP && p.HP == expected.HP &&
		p.BeforeMPMax == expected.BeforeMPMax && p.MPMax == expected.MPMax && p.BeforeMP == expected.BeforeMP && p.MP == expected.MP && p.BeforeTimers == expected.BeforeTimers && p.Timers == expected.Timers && p.Response == expected.Response && p.expectedRoomFlags == expected.expectedRoomFlags
}

func changeClassResult(p ChangeClassProposal) ChangeClassResult {
	return ChangeClassResult{
		Action: p.Action, ActorID: p.ActorID, ActorName: p.ActorName, RoomID: p.RoomID, Confirmed: p.Confirmed,
		PendingConfirmation: p.PendingConfirmation, NoOp: p.NoOp, Changed: p.Changed,
		Broadcast: p.Broadcast, BroadcastPending: p.BroadcastPending, PendingReason: p.PendingReason,
		BeforeClass: p.BeforeClass, Class: p.Class, BeforeLevel: p.BeforeLevel, Level: p.Level,
		TargetLevel: p.TargetLevel, LevelsDown: p.LevelsDown, BeforeExperience: p.BeforeExperience,
		Experience: p.Experience, ExperienceSpent: p.ExperienceSpent,
		BeforeStats: p.BeforeStats, Stats: p.Stats, BeforeHPMax: p.BeforeHPMax, HPMax: p.HPMax,
		BeforeHP: p.BeforeHP, HP: p.HP, BeforeMPMax: p.BeforeMPMax, MPMax: p.MPMax,
		BeforeMP: p.BeforeMP, MP: p.MP, BeforeTimers: p.BeforeTimers, Timers: p.Timers,
		Response: p.Response,
	}
}

// ApplyChangeClass atomically applies a PlanChangeClass candidate. Any
// changed world field, including stats/vitals/timers unrelated to class, makes
// the candidate stale. The result is a cloned state and no partial mutation.
func (s State) ApplyChangeClass(p ChangeClassProposal) (State, ChangeClassResult, error) {
	if p.ActorID == "" || p.before.Version == 0 || !reflect.DeepEqual(s, p.before) {
		return State{}, ChangeClassResult{}, ErrChangeClassStaleProposal
	}
	expected, err := changeClassCandidate(s, p.ActorID, p.Confirmed)
	if err != nil {
		return State{}, ChangeClassResult{}, err
	}
	if !changeClassProposalMatches(p, expected) || !reflect.DeepEqual(p.expectedActor, expected.expectedActor) {
		return State{}, ChangeClassResult{}, ErrChangeClassStaleProposal
	}
	next := s.clone()
	if p.Changed {
		actor := next.Players[p.ActorID]
		body := actor.Body
		body.Experience = p.Experience
		body.Class = p.Class
		body.Level = p.Level
		body.Stats = p.Stats
		body.HPMax, body.HPCurrent = p.HPMax, p.HP
		body.MPMax, body.MPCurrent = p.MPMax, p.MP
		body.Timers = p.Timers
		actor.Body = body
		next.Players[p.ActorID] = actor
	}
	if err := next.Validate(); err != nil {
		return State{}, ChangeClassResult{}, err
	}
	return next, changeClassResult(p), nil
}

// ChangeClass is the reducer-shaped convenience API. It defaults to the
// confirmed one-line path; pass false to obtain the typed bare-command prompt.
func (s State) ChangeClass(actorID string, confirmation ...bool) (State, ChangeClassResult, error) {
	p, err := s.PlanChangeClass(actorID, confirmation...)
	if err != nil {
		return State{}, ChangeClassResult{}, err
	}
	return s.ApplyChangeClass(p)
}

func (s State) ClassChange(actorID string, confirmation ...bool) (State, ChangeClassResult, error) {
	return s.ChangeClass(actorID, confirmation...)
}
