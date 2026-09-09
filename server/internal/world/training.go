package world

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The values below are the source-backed train contract from mtype.h and
// command7.c.  RTRAIN itself is required for every class; bits 4..6 encode
// the three low bits of class-1 in reverse order.
const (
	trainingRoomFlag      = 3  // RTRAIN
	trainingFamilyFlag    = 55 // PFAMIL
	trainingMaxArrayLevel = 128
	trainingInvincible    = 9  // INVINCIBLE
	trainingCaretaker     = 10 // CARETAKER
	trainingMaxClass      = 12 // DM
	trainingUpDmgFlag     = 59 // PUPDMG
)

const (
	TrainingRoomFlag   = trainingRoomFlag
	TrainingFamilyFlag = trainingFamilyFlag
	TrainingInvincible = trainingInvincible
	TrainingCaretaker  = trainingCaretaker
	TrainingUpDmgFlag  = trainingUpDmgFlag
)

var (
	ErrTrainingActorAbsent      = errors.New("training actor absent")
	ErrTrainingRoomAbsent       = errors.New("training room absent")
	ErrTrainingRoom             = errors.New("training is unavailable in this room")
	ErrTrainingClass            = errors.New("training class does not match this room")
	ErrTrainingBlind            = errors.New("training is unavailable while blind")
	ErrTrainingCaretaker        = errors.New("caretaker cannot train")
	ErrTrainingExperience       = errors.New("training experience is insufficient")
	ErrTrainingGold             = errors.New("training gold is insufficient")
	ErrTrainingFamilyPending    = errors.New("training family membership update pending")
	ErrTrainingBroadcastPending = errors.New("training global broadcast projection pending")
	ErrTrainingNumeric          = errors.New("training numeric state outside canonical range")
	ErrTrainingStaleProposal    = errors.New("stale or invalid training proposal")
	ErrTrainingUnsupportedClass = errors.New("training class outside canonical range")
	ErrTrainingUnsupportedLevel = errors.New("training level outside canonical range")
)

// Source-oriented aliases make the fail-closed boundaries discoverable to
// callers using either train or training terminology.
var (
	ErrTrainActorAbsent      = ErrTrainingActorAbsent
	ErrTrainRoomAbsent       = ErrTrainingRoomAbsent
	ErrTrainRoom             = ErrTrainingRoom
	ErrTrainClass            = ErrTrainingClass
	ErrTrainBlind            = ErrTrainingBlind
	ErrTrainCaretaker        = ErrTrainingCaretaker
	ErrTrainExperience       = ErrTrainingExperience
	ErrTrainGold             = ErrTrainingGold
	ErrTrainFamilyPending    = ErrTrainingFamilyPending
	ErrTrainBroadcastPending = ErrTrainingBroadcastPending
	ErrTrainStaleProposal    = ErrTrainingStaleProposal
)

// TrainingProposal is a snapshot-bound candidate for command7.c:train.  The
// private actor/room copies prevent a caller from applying a level-up after
// another command changed the same player or moved the training room.
//
// Source train calls edit_member for family members at the two class
// transitions and broadcast_all after every success.  Neither the family
// roster nor a canonical global recipient set is present in State, so family
// transitions fail closed and successful state changes carry an explicit
// BroadcastPending marker with no unsafe Event projection.
type TrainingProposal struct {
	Action    string
	ActorID   string
	ActorName string
	RoomID    int16

	BeforeLevel      byte
	Level            byte
	BeforeClass      byte
	Class            byte
	BeforeExperience int32
	Experience       int32
	BeforeGold       int32
	Gold             int32

	ExperienceNeeded     int64
	GoldNeeded           int64
	NextExperienceNeeded int64
	NextGoldNeeded       int64
	GoldSpent            int64
	LevelsGained         int
	UpDmgReleased        bool
	Invincible           bool
	Caretaker            bool
	Changed              bool
	Broadcast            bool
	BroadcastPending     bool
	Response             string
	// Event remains nil until the global broadcast recipient projection is
	// canonical.  Keeping the field on the proposal makes that boundary
	// explicit to callers without accepting a caller-supplied event.
	Event *TrainingEvent

	expectedActor     PlayerState
	expectedRoomFlags [8]byte
}

// TrainingResult is the durable receipt projection. Broadcast is deliberately
// false because broadcast_all's global recipient/fan-out contract is not
// canonical yet; BroadcastPending records that the source requested output
// without fabricating an event that a transport could misdeliver.
type TrainingResult struct {
	Action               string `json:"action"`
	Response             string `json:"response"`
	Broadcast            bool   `json:"broadcast"`
	BroadcastPending     bool   `json:"broadcast_pending,omitempty"`
	Changed              bool   `json:"changed"`
	RoomID               int16  `json:"room_id"`
	ActorID              string `json:"actor_id"`
	ActorName            string `json:"actor_name"`
	BeforeLevel          byte   `json:"before_level"`
	Level                byte   `json:"level"`
	BeforeClass          byte   `json:"before_class"`
	Class                byte   `json:"class"`
	BeforeExperience     int32  `json:"before_experience"`
	Experience           int32  `json:"experience"`
	BeforeGold           int32  `json:"before_gold"`
	Gold                 int32  `json:"gold"`
	ExperienceNeeded     int64  `json:"experience_needed"`
	GoldNeeded           int64  `json:"gold_needed"`
	NextExperienceNeeded int64  `json:"next_experience_needed,omitempty"`
	NextGoldNeeded       int64  `json:"next_gold_needed,omitempty"`
	GoldSpent            int64  `json:"gold_spent"`
	LevelsGained         int    `json:"levels_gained"`
	UpDmgReleased        bool   `json:"up_dmg_released,omitempty"`
	Invincible           bool   `json:"invincible,omitempty"`
	Caretaker            bool   `json:"caretaker,omitempty"`
	// Event is intentionally nil in this bounded slice.  The source's
	// broadcast_all fan-out has no canonical recipient set in State.
	Event *TrainingEvent `json:"event,omitempty"`
}

// TrainingEvent is intentionally not produced in this bounded slice.  The
// type documents the missing projection boundary for future global fan-out;
// a nil Event is represented by TrainingResult.BroadcastPending instead of a
// guessed recipient list.
type TrainingEvent struct {
	Scope string `json:"scope"`
	Text  string `json:"text"`
}

type trainingSimulation struct {
	body                 LegacyMonster
	beforeLevel          byte
	beforeClass          byte
	beforeExperience     int32
	beforeGold           int32
	experienceNeeded     int64
	goldNeeded           int64
	nextExperienceNeeded int64
	nextGoldNeeded       int64
	levelsGained         int
	upDmgReleased        bool
	invincible           bool
	caretaker            bool
}

func trainingExperienceNeeded(level byte) (int64, error) {
	if level == 0 {
		return 0, ErrTrainingUnsupportedLevel
	}
	if int(level) <= trainingMaxArrayLevel {
		return int64(neededExperience[int(level)-1]), nil
	}
	// command7.c intentionally uses MAXALVL-2 plus the high-level five-million
	// progression, even though exp_to_lev has a separate historical boundary.
	return int64(neededExperience[trainingMaxArrayLevel-2]) + (int64(level)-trainingMaxArrayLevel+1)*5000000, nil
}

func trainingGoldNeeded(level byte, experienceNeeded int64) (int64, error) {
	if level == 0 || experienceNeeded < 0 {
		return 0, ErrTrainingUnsupportedLevel
	}
	if level < trainingMaxArrayLevel {
		return (experienceNeeded / 10) / 2, nil
	}
	return (int64(neededExperience[trainingMaxArrayLevel-2]) / 10) / 2, nil
}

// TrainingExperienceNeeded exposes command7.c's level threshold without
// exposing the mutable source table.
func TrainingExperienceNeeded(level byte) (int64, error) {
	return trainingExperienceNeeded(level)
}

// TrainingGoldNeeded exposes the source gold calculation for migration
// fixtures and callers that render a preview. With no optional threshold it
// derives the source threshold from level; a supplied threshold keeps the
// integer division order explicit for a command snapshot.
func TrainingGoldNeeded(level byte, experience ...int64) (int64, error) {
	if len(experience) > 1 {
		return 0, ErrTrainingUnsupportedLevel
	}
	var experienceNeeded int64
	var err error
	if len(experience) == 1 {
		experienceNeeded = experience[0]
	} else {
		experienceNeeded, err = trainingExperienceNeeded(level)
		if err != nil {
			return 0, err
		}
	}
	return trainingGoldNeeded(level, experienceNeeded)
}

func validTrainingName(name string) bool {
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

func trainingActor(s State, actorID string) (PlayerState, RoomState, error) {
	if err := s.Validate(); err != nil {
		return PlayerState{}, RoomState{}, err
	}
	actor, ok := s.Players[actorID]
	if actorID == "" || !ok || !actor.Online || actor.Body.Type != 0 || !validTrainingName(actor.Body.Name) {
		return PlayerState{}, RoomState{}, ErrTrainingActorAbsent
	}
	if actor.Body.Class == 0 || actor.Body.Class > trainingMaxClass {
		return PlayerState{}, RoomState{}, ErrTrainingUnsupportedClass
	}
	if actor.Body.Level == 0 {
		return PlayerState{}, RoomState{}, ErrTrainingUnsupportedLevel
	}
	if actor.Body.Experience < 0 || actor.Body.Gold < 0 {
		return PlayerState{}, RoomState{}, ErrTrainingNumeric
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok {
		return PlayerState{}, RoomState{}, ErrTrainingRoomAbsent
	}
	if !containsString(room.PlayerIDs, actorID) {
		return PlayerState{}, RoomState{}, fmt.Errorf("%w: actor is not present in room", ErrTrainingRoomAbsent)
	}
	return actor, room, nil
}

func trainingClassAllowed(body LegacyMonster, room LegacyRoom) bool {
	if body.Class > 8 {
		return true
	}
	classBits := body.Class - 1 // command7.c decrements class before reading bits.
	for i := 0; i < 3; i++ {
		want := classBits&(1<<i) != 0
		got := flag(room.Flags[:], uint(trainingRoomFlag+3-i))
		if want != got {
			return false
		}
	}
	return true
}

func trainingReleaseUpDmg(body *LegacyMonster) error {
	if !flag(body.Flags[:], trainingUpDmgFlag) {
		return nil
	}
	hpMax := int(body.HPMax) - 100
	mpMax := int(body.MPMax) - 100
	dicePlus := int(body.DicePlus) - 5
	if hpMax < math.MinInt16 || hpMax > math.MaxInt16 || mpMax < math.MinInt16 || mpMax > math.MaxInt16 || dicePlus < math.MinInt16 || dicePlus > math.MaxInt16 {
		return ErrTrainingNumeric
	}
	body.Flags[trainingUpDmgFlag/8] &^= 1 << (trainingUpDmgFlag % 8)
	body.HPMax, body.MPMax, body.DicePlus = int16(hpMax), int16(mpMax), int16(dicePlus)
	body.HPCurrent, body.MPCurrent = body.HPMax, body.MPMax
	return nil
}

func trainingSimulationFor(actor LegacyMonster, room LegacyRoom) (trainingSimulation, error) {
	if actor.Class == trainingCaretaker {
		return trainingSimulation{}, ErrTrainingCaretaker
	}
	if flag(actor.Flags[:], playerBlindFlag) {
		return trainingSimulation{}, ErrTrainingBlind
	}
	if !flag(room.Flags[:], trainingRoomFlag) {
		return trainingSimulation{}, ErrTrainingRoom
	}
	if !trainingClassAllowed(actor, room) {
		return trainingSimulation{}, ErrTrainingClass
	}
	if actor.Experience < 0 || actor.Gold < 0 {
		return trainingSimulation{}, ErrTrainingNumeric
	}
	experienceNeeded, err := trainingExperienceNeeded(actor.Level)
	if err != nil {
		return trainingSimulation{}, err
	}
	goldNeeded, err := trainingGoldNeeded(actor.Level, experienceNeeded)
	if err != nil {
		return trainingSimulation{}, err
	}
	if int64(actor.Experience) < experienceNeeded {
		return trainingSimulation{}, fmt.Errorf("%w: need %d more experience", ErrTrainingExperience, experienceNeeded-int64(actor.Experience))
	}
	if int64(actor.Gold) < goldNeeded {
		return trainingSimulation{}, fmt.Errorf("%w: need %d more gold", ErrTrainingGold, goldNeeded-int64(actor.Gold))
	}

	sim := trainingSimulation{
		body:                 actor,
		beforeLevel:          actor.Level,
		beforeClass:          actor.Class,
		beforeExperience:     actor.Experience,
		beforeGold:           actor.Gold,
		experienceNeeded:     experienceNeeded,
		goldNeeded:           goldNeeded,
		nextExperienceNeeded: experienceNeeded,
		nextGoldNeeded:       goldNeeded,
	}
	if flag(sim.body.Flags[:], trainingUpDmgFlag) {
		if err := trainingReleaseUpDmg(&sim.body); err != nil {
			return trainingSimulation{}, err
		}
		sim.upDmgReleased = true
	}

	// C's family edit_member is an external side effect. Refuse the transition
	// before changing the candidate when that unresolved side effect is needed.
	if sim.body.Level == 100 && sim.body.Class < trainingInvincible {
		if flag(sim.body.Flags[:], trainingFamilyFlag) {
			return trainingSimulation{}, ErrTrainingFamilyPending
		}
		sim.body.Class = trainingInvincible
		sim.body.Level = 1
		sim.body.Experience = 0
		sim.invincible = true
	}

	if sim.body.Level >= 127 && sim.body.Class == trainingInvincible {
		if flag(sim.body.Flags[:], trainingFamilyFlag) {
			return trainingSimulation{}, ErrTrainingFamilyPending
		}
		if int64(sim.body.Gold) < goldNeeded {
			return trainingSimulation{}, fmt.Errorf("%w: need %d more gold", ErrTrainingGold, goldNeeded-int64(sim.body.Gold))
		}
		sim.body.Class = trainingCaretaker
		sim.body.Level = 127
		sim.body.Gold = int32(int64(sim.body.Gold) - goldNeeded)
		sim.body.HPMax, sim.body.MPMax = 800, 600
		sim.body.DiceCount, sim.body.DiceSides, sim.body.DicePlus = 4, 4, 4
		sim.caretaker = true
		return sim, nil
	}

	for {
		// This guard is observable only for a malformed pre-transition state;
		// level 100 mortals were reset to INVINCIBLE above, matching source order.
		if sim.body.Level == 100 && sim.body.Class < trainingInvincible {
			break
		}
		if sim.body.Level == math.MaxUint8 {
			return trainingSimulation{}, ErrTrainingUnsupportedLevel
		}
		if int64(sim.body.Gold) < goldNeeded {
			break
		}
		sim.body.Gold = int32(int64(sim.body.Gold) - goldNeeded)
		levelled, err := RaisePlayerLevel(sim.body)
		if err != nil {
			return trainingSimulation{}, fmt.Errorf("%w: %v", ErrTrainingNumeric, err)
		}
		sim.body = levelled
		sim.levelsGained++
		experienceNeeded, err = trainingExperienceNeeded(sim.body.Level)
		if err != nil {
			return trainingSimulation{}, err
		}
		goldNeeded, err = trainingGoldNeeded(sim.body.Level, experienceNeeded)
		if err != nil {
			return trainingSimulation{}, err
		}
		if int64(sim.body.Experience) < experienceNeeded || int64(sim.body.Gold) < goldNeeded {
			break
		}
	}
	if sim.levelsGained == 0 {
		return trainingSimulation{}, ErrTrainingNumeric
	}
	sim.nextExperienceNeeded, sim.nextGoldNeeded = experienceNeeded, goldNeeded
	return sim, nil
}

func cloneTrainingActor(actor PlayerState) PlayerState {
	actor.Body.Inventory = cloneObjects(actor.Body.Inventory)
	actor.Aliases = clonePlayerAliases(actor.Aliases)
	actor.PlayerEnemies = append([]string(nil), actor.PlayerEnemies...)
	actor.FollowerIDs = append([]string(nil), actor.FollowerIDs...)
	if actor.NPCFollowerIDs != nil {
		actor.NPCFollowerIDs = append([]string(nil), actor.NPCFollowerIDs...)
	}
	if actor.FollowerRefs != nil {
		actor.FollowerRefs = append([]EntityRef(nil), actor.FollowerRefs...)
	}
	if actor.Items != nil {
		items := actor.Items.clone()
		actor.Items = &items
	}
	return actor
}

func trainingResponse(sim trainingSimulation) string {
	if sim.caretaker {
		return "\n축하합니다! 당신은 초인이 되었습니다!!"
	}
	return "\n축하합니다! 당신의 레벨이 올랐습니다!"
}

func trainingProposalFrom(actor PlayerState, room RoomState, sim trainingSimulation, actorID string) TrainingProposal {
	return TrainingProposal{
		Action: "train", ActorID: actorID, ActorName: actor.Body.Name, RoomID: room.Resource.ID,
		BeforeLevel: sim.beforeLevel, Level: sim.body.Level,
		BeforeClass: sim.beforeClass, Class: sim.body.Class,
		BeforeExperience: sim.beforeExperience, Experience: sim.body.Experience,
		BeforeGold: sim.beforeGold, Gold: sim.body.Gold,
		ExperienceNeeded: sim.experienceNeeded, GoldNeeded: sim.goldNeeded,
		NextExperienceNeeded: sim.nextExperienceNeeded, NextGoldNeeded: sim.nextGoldNeeded,
		GoldSpent:    int64(sim.beforeGold) - int64(sim.body.Gold),
		LevelsGained: sim.levelsGained, UpDmgReleased: sim.upDmgReleased,
		Invincible: sim.invincible, Caretaker: sim.caretaker,
		Changed: true, Broadcast: false, BroadcastPending: true, Event: nil,
		Response: trainingResponse(sim), expectedActor: cloneTrainingActor(actor),
		expectedRoomFlags: room.Resource.Flags,
	}
}

// PlanTraining validates all source-ordered gates and computes the complete
// multi-level candidate without mutating State or invoking external effects.
func (s State) PlanTraining(actorID string) (TrainingProposal, error) {
	actor, room, err := trainingActor(s, actorID)
	if err != nil {
		return TrainingProposal{}, err
	}
	sim, err := trainingSimulationFor(actor.Body, room.Resource)
	if err != nil {
		return TrainingProposal{}, err
	}
	return trainingProposalFrom(actor, room, sim, actorID), nil
}

// PlanTrain is a spelling alias for command-oriented adapters.
func (s State) PlanTrain(actorID string) (TrainingProposal, error) {
	return s.PlanTraining(actorID)
}

func trainingProposalMatches(p TrainingProposal, actor PlayerState, room RoomState, sim trainingSimulation) bool {
	return p.Action == "train" && p.ActorID != "" && p.ActorName == actor.Body.Name && p.RoomID == room.Resource.ID &&
		p.BeforeLevel == sim.beforeLevel && p.Level == sim.body.Level &&
		p.BeforeClass == sim.beforeClass && p.Class == sim.body.Class &&
		p.BeforeExperience == sim.beforeExperience && p.Experience == sim.body.Experience &&
		p.BeforeGold == sim.beforeGold && p.Gold == sim.body.Gold &&
		p.ExperienceNeeded == sim.experienceNeeded && p.GoldNeeded == sim.goldNeeded &&
		p.NextExperienceNeeded == sim.nextExperienceNeeded && p.NextGoldNeeded == sim.nextGoldNeeded &&
		p.GoldSpent == int64(sim.beforeGold)-int64(sim.body.Gold) &&
		p.LevelsGained == sim.levelsGained && p.UpDmgReleased == sim.upDmgReleased &&
		p.Invincible == sim.invincible && p.Caretaker == sim.caretaker && p.Changed &&
		!p.Broadcast && p.BroadcastPending && p.Event == nil && p.Response == trainingResponse(sim)
}

// ApplyTraining atomically applies a candidate made by PlanTraining. Any
// changed actor/room state or late numeric/progression discrepancy returns a
// zero State, so PUPDMG release, gold debit, and class/level conversion cannot
// be partially committed.
func (s State) ApplyTraining(proposal TrainingProposal) (State, TrainingResult, error) {
	actor, room, err := trainingActor(s, proposal.ActorID)
	if err != nil {
		return State{}, TrainingResult{}, err
	}
	if !reflect.DeepEqual(actor, proposal.expectedActor) || room.Resource.Flags != proposal.expectedRoomFlags {
		return State{}, TrainingResult{}, ErrTrainingStaleProposal
	}
	sim, err := trainingSimulationFor(actor.Body, room.Resource)
	if err != nil {
		return State{}, TrainingResult{}, err
	}
	if !trainingProposalMatches(proposal, actor, room, sim) {
		return State{}, TrainingResult{}, ErrTrainingStaleProposal
	}

	next := s.clone()
	nextActor := next.Players[proposal.ActorID]
	nextActor.Body = sim.body
	next.Players[proposal.ActorID] = nextActor
	if err := next.Validate(); err != nil {
		return State{}, TrainingResult{}, err
	}
	return next, TrainingResult{
		Action: "train", Response: proposal.Response, Broadcast: false, BroadcastPending: true,
		Changed: true, RoomID: proposal.RoomID, ActorID: proposal.ActorID, ActorName: actor.Body.Name,
		BeforeLevel: proposal.BeforeLevel, Level: proposal.Level,
		BeforeClass: proposal.BeforeClass, Class: proposal.Class,
		BeforeExperience: proposal.BeforeExperience, Experience: proposal.Experience,
		BeforeGold: proposal.BeforeGold, Gold: proposal.Gold,
		ExperienceNeeded: proposal.ExperienceNeeded, GoldNeeded: proposal.GoldNeeded,
		NextExperienceNeeded: proposal.NextExperienceNeeded, NextGoldNeeded: proposal.NextGoldNeeded,
		GoldSpent:    proposal.GoldSpent,
		LevelsGained: proposal.LevelsGained, UpDmgReleased: proposal.UpDmgReleased,
		Invincible: proposal.Invincible, Caretaker: proposal.Caretaker,
		Event: nil,
	}, nil
}

// Train is the reducer-shaped convenience API.
func (s State) Train(actorID string) (State, TrainingResult, error) {
	proposal, err := s.PlanTraining(actorID)
	if err != nil {
		return State{}, TrainingResult{}, err
	}
	return s.ApplyTraining(proposal)
}

// Training is a semantic alias retained for callers that use the noun.
func (s State) Training(actorID string) (State, TrainingResult, error) {
	return s.Train(actorID)
}
