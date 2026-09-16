package world

// This file is the player-facing slice of magic1.c:cast. It admits the
// self-target healing, cleansing and timed utility spells whose state transition is
// represented by the canonical player body, plus the targeted SLOCAT / 천리안
// locate path, SSUMMO / 소환, and self-cast SRECAL / 귀환. Other spells remain
// visible in the spell catalog but fail closed at this boundary until their
// target, combat, room or item contracts are migrated.

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"time"
)

const (
	castSpellTimerIndex = 9  // LT_SPELL
	castNoMagicRoomFlag = 17 // RNOMAG
	castHiddenFlag      = 1  // PHIDDN
	castBlindFlag       = 42 // PBLIND
	castSilentFlag      = 44 // PSILNC
	castDailyHealIndex  = 2  // DL_FHEAL

	castVigorSpell           = 0  // SVIGOR / 회복
	castMendSpell            = 18 // SMENDW / 원기회복
	castHealSpell            = 19 // SFHEAL / 완치
	castLightSpell           = 2  // SLIGHT / 발광
	castBlessSpell           = 4  // SBLESS / 성현진
	castProtectionSpell      = 5  // SPROTE / 수호진
	castRemoveCurseSpell     = 42 // SREMOV / 저주해소
	castBlindSpell           = 53 // SBLIND / 실명
	castSilenceSpell         = 54 // SSILNC / 봉합구
	castFearSpell            = 50 // SFEARS / 공포
	castCurePoisonSpell      = 3  // SCUREP / 해독
	castInvisibilitySpell    = 7  // SINVIS / 은둔법
	castDetectInvisibleSpell = 9  // SDINVI / 은둔감지술
	castDetectMagicSpell     = 10 // SDMAGI / 주문감지술
	castLevitateSpell        = 21 // SLEVIT / 부양술
	castResistFireSpell      = 22 // SRFIRE / 방열진
	castFlySpell             = 23 // SFLYSP / 비상술
	castResistMagicSpell     = 24 // SRMAGI / 보마진
	castResistColdSpell      = 43 // SRCOLD / 방한진
	castWaterSpell           = 44 // SBRWAT / 수생술
	castEarthShieldSpell     = 45 // SSSHLD / 지방호
	castDiseaseSpell         = 48 // SRMDIS / 치료
	castRemoveBlindSpell     = 49 // SRMBLD / 개안술
	castKnowAlignmentSpell   = 41 // SKNOWA / 선악감지
	castLocateSpell          = 46 // SLOCAT / 천리안
	// castSummonSpell is defined in summon.go so the SSUMMO reducer can own
	// the source index next to its gates.
)

const (
	castInvisibilityFlag     = 2
	castInvisibilityTimer    = 0
	castDetectInvisibleFlag  = 21
	castDetectInvisibleTimer = 17
	castDetectMagicFlag      = 20
	castDetectMagicTimer     = 18
	castKnowAlignmentFlag    = 33
	castKnowAlignmentTimer   = 27
	castLevitateFlag         = 25
	castLevitateTimer        = 21
	castResistFireFlag       = 30
	castResistFireTimer      = 23
	castFlyFlag              = 31
	castFlyTimer             = 24
	castResistMagicFlag      = 32
	castResistMagicTimer     = 25
	castResistColdFlag       = 36
	castResistColdTimer      = 29
	castWaterFlag            = 37
	castWaterTimer           = 30
	castEarthShieldFlag      = 38
	castEarthShieldTimer     = 31
	castBlessFlag            = 0
	castBlessTimer           = 2
	castProtectionFlag       = 8
	castProtectionTimer      = 1
	castCurePoisonFlag       = 16
	castDiseaseFlag          = 41
	castRemoveBlindFlag      = 42
	castLightFlag            = 17
	castLightTimer           = 13
	castSilenceTimer         = 34 // LT_SILNC
	castFearFlag             = 43 // PFEARS
	castFearTimer            = 33 // LT_FEARS
)

const (
	castHealNone = iota
	castHealVigor
	castHealMend
	castHealFull
	castHealTimed
	castHealCleanse
	castHealFlag
	castHealLocate
	castHealSummon
	castHealRecall
)

var (
	ErrCastSpellUnavailable = errors.New("cast spell unavailable")
	ErrCastSpellAmbiguous   = errors.New("cast spell name is ambiguous")
	ErrCastRandom           = errors.New("cast random source unavailable")
	ErrCastActorAbsent      = errors.New("online cast actor absent")
	ErrCastStaleProposal    = errors.New("stale cast proposal")
	ErrCastInvalidProposal  = errors.New("invalid cast proposal")
)

// Export source-backed values for session adapters and differential tests.
const (
	CastSpellTimerIndex = castSpellTimerIndex
	CastNoMagicRoomFlag = castNoMagicRoomFlag
	CastBlindFlag       = castBlindFlag
	CastSilentFlag      = castSilentFlag
	CastDailyHealIndex  = castDailyHealIndex
)

type castSpellSpec struct {
	Name     string
	Index    int
	Cost     int16
	healKind int
	Flag     uint
	Timer    int
	// Timed spell intervals are derived from the caster's intelligence and
	// optional mage/room terms in the original spell routine.
	IntervalBase int32
	LevelStep    int32
	RoomExtend   int32
	MageInterval bool
	MinInterval  bool
	// Bless/protection add the cleric/paladin level-band term from their
	// source routines. They also refresh the derived canonical combat value
	// after setting their flag.
	ClassIntervalTerm  bool
	CombatStats        bool
	ClearCursedReady   bool
	RevealInvisibility bool
	FixedInterval      bool
	HalfIntervalFlag   uint
	RandomInterval     bool
	RandomIntervalDie  int
	RandomIntervalStep int32
	StatIntervalStep   int32
	CombatGate         bool
	// A zero mask means spell_fail is not needed for this spell.  Otherwise
	// only the listed class bits call spell_fail, matching magic2.c/magic5.c.
	failMask uint16
	// The source heal spell has no spell_fail call and is restricted to
	// cleric/paladin/invincible classes.
	classGate int
}

const (
	castAnyClass = iota
	castClericInvincible
	castClericPaladinInvincible
	castSubDMGate
)

const castAllSpellFailMask uint16 = 1<<castAssassinClass | 1<<castBarbarianClass | 1<<castClericClass | 1<<castFighterClass | 1<<castMageClass | 1<<castPaladinClass | 1<<7 | 1<<8

func castSpellSpecFor(name string) (castSpellSpec, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return castSpellSpec{}, ErrCastSpellUnavailable
	}
	// Keep the source prefix/unique-match behavior, but resolve against the
	// complete catalog first so a prefix that names an unported spell does not
	// accidentally select another admitted effect.
	entries, err := SpellCatalog()
	if err != nil {
		return castSpellSpec{}, err
	}
	index := -1
	matches := 0
	for _, entry := range entries {
		if entry.Name == name {
			index = entry.Index
			matches = 1
			break
		}
		if strings.HasPrefix(entry.Name, name) {
			index = entry.Index
			matches++
		}
	}
	if matches == 0 {
		return castSpellSpec{}, ErrCastSpellUnavailable
	}
	if matches > 1 {
		return castSpellSpec{}, ErrCastSpellAmbiguous
	}
	spec := castSpellSpec{Name: entries[index].Name, Index: index}
	switch index {
	case castVigorSpell:
		spec.Cost, spec.healKind = 2, castHealVigor
		spec.failMask = 1<<castBarbarianClass | 1<<castFighterClass
		spec.classGate = castClericPaladinInvincible
	case castMendSpell:
		spec.Cost, spec.healKind = 4, castHealMend
		spec.failMask = 1<<castAssassinClass | 1<<castBarbarianClass | 1<<castFighterClass
	case castHealSpell:
		spec.Cost, spec.healKind = 20, castHealFull
		spec.classGate = castClericPaladinInvincible
	case castInvisibilitySpell:
		spec.Cost, spec.healKind = 15, castHealTimed
		spec.Flag, spec.Timer = castInvisibilityFlag, castInvisibilityTimer
		spec.IntervalBase, spec.RoomExtend, spec.MageInterval, spec.CombatGate = 1200, 600, true, true
		spec.failMask = castAllSpellFailMask
	case castDetectInvisibleSpell:
		spec.Cost, spec.healKind = 10, castHealTimed
		spec.Flag, spec.Timer = castDetectInvisibleFlag, castDetectInvisibleTimer
		spec.IntervalBase, spec.RoomExtend, spec.MageInterval, spec.MinInterval = 1200, 600, true, true
		spec.failMask = castAllSpellFailMask
	case castDetectMagicSpell:
		spec.Cost, spec.healKind = 10, castHealTimed
		spec.Flag, spec.Timer = castDetectMagicFlag, castDetectMagicTimer
		spec.IntervalBase, spec.RoomExtend, spec.MageInterval, spec.MinInterval = 1200, 600, true, true
		spec.failMask = castAllSpellFailMask
	case castLevitateSpell:
		spec.Cost, spec.healKind = 10, castHealTimed
		spec.Flag, spec.Timer = castLevitateFlag, castLevitateTimer
		spec.IntervalBase, spec.RoomExtend = 2400, 800
		spec.failMask = castAllSpellFailMask
	case castResistFireSpell:
		spec.Cost, spec.healKind = 12, castHealTimed
		spec.Flag, spec.Timer = castResistFireFlag, castResistFireTimer
		spec.IntervalBase, spec.RoomExtend, spec.MinInterval = 1200, 800, true
		spec.failMask = castAllSpellFailMask
	case castFlySpell:
		spec.Cost, spec.healKind = 15, castHealTimed
		spec.Flag, spec.Timer = castFlyFlag, castFlyTimer
		spec.IntervalBase, spec.RoomExtend, spec.MinInterval = 1200, 600, true
		spec.failMask = castAllSpellFailMask
	case castResistMagicSpell:
		spec.Cost, spec.healKind = 12, castHealTimed
		spec.Flag, spec.Timer = castResistMagicFlag, castResistMagicTimer
		spec.IntervalBase, spec.RoomExtend, spec.MinInterval = 1200, 800, true
		spec.failMask = castAllSpellFailMask
	case castResistColdSpell:
		spec.Cost, spec.healKind = 12, castHealTimed
		spec.Flag, spec.Timer = castResistColdFlag, castResistColdTimer
		spec.IntervalBase, spec.RoomExtend, spec.MinInterval = 1200, 800, true
		spec.failMask = castAllSpellFailMask
	case castWaterSpell:
		spec.Cost, spec.healKind = 12, castHealTimed
		spec.Flag, spec.Timer = castWaterFlag, castWaterTimer
		spec.IntervalBase, spec.RoomExtend, spec.MinInterval = 1200, 800, true
		spec.failMask = castAllSpellFailMask
	case castEarthShieldSpell:
		spec.Cost, spec.healKind = 12, castHealTimed
		spec.Flag, spec.Timer = castEarthShieldFlag, castEarthShieldTimer
		spec.IntervalBase, spec.RoomExtend, spec.MinInterval = 1200, 800, true
		spec.failMask = castAllSpellFailMask
	case castCurePoisonSpell:
		spec.Cost, spec.healKind = 6, castHealCleanse
		spec.Flag, spec.Timer = castCurePoisonFlag, -1
		spec.failMask = castAllSpellFailMask
	case castDiseaseSpell:
		spec.Cost, spec.healKind = 12, castHealCleanse
		spec.Flag, spec.Timer = castDiseaseFlag, -1
		spec.failMask = castAllSpellFailMask
		spec.classGate = castClericInvincible
	case castRemoveBlindSpell:
		spec.Cost, spec.healKind = 12, castHealCleanse
		spec.Flag, spec.Timer = castRemoveBlindFlag, -1
		spec.failMask = castAllSpellFailMask
		spec.classGate = castClericPaladinInvincible
	case castLightSpell:
		spec.Cost, spec.healKind = 5, castHealTimed
		spec.Flag, spec.Timer = castLightFlag, castLightTimer
		spec.IntervalBase, spec.LevelStep, spec.RoomExtend = 300, 300, 600
		spec.failMask = castAllSpellFailMask
	case castBlessSpell:
		spec.Cost, spec.healKind = 10, castHealTimed
		spec.Flag, spec.Timer = castBlessFlag, castBlessTimer
		spec.RoomExtend, spec.MinInterval, spec.ClassIntervalTerm, spec.CombatStats = 800, true, true, true
		spec.failMask = castAllSpellFailMask
	case castProtectionSpell:
		spec.Cost, spec.healKind = 10, castHealTimed
		spec.Flag, spec.Timer = castProtectionFlag, castProtectionTimer
		spec.RoomExtend, spec.MinInterval, spec.ClassIntervalTerm, spec.CombatStats = 800, true, true, true
		spec.failMask = castAllSpellFailMask
	case castRemoveCurseSpell:
		spec.Cost, spec.healKind = 18, castHealCleanse
		spec.Flag, spec.Timer, spec.ClearCursedReady = fleeFearFlag, -1, true
		spec.failMask = castAllSpellFailMask
	case castBlindSpell:
		spec.Cost, spec.healKind = 15, castHealFlag
		spec.Flag, spec.Timer, spec.RevealInvisibility = castBlindFlag, -1, true
		spec.failMask = castAllSpellFailMask
		spec.classGate = castSubDMGate
	case castSilenceSpell:
		spec.Cost, spec.healKind = 12, castHealTimed
		spec.Flag, spec.Timer = castSilentFlag, castSilenceTimer
		spec.IntervalBase, spec.FixedInterval, spec.HalfIntervalFlag, spec.RevealInvisibility = 3600, true, castResistMagicFlag, true
		spec.failMask = castAllSpellFailMask
		spec.classGate = castSubDMGate
	case castFearSpell:
		spec.Cost, spec.healKind = 15, castHealTimed
		spec.Flag, spec.Timer = castFearFlag, castFearTimer
		spec.IntervalBase, spec.RandomInterval, spec.RandomIntervalDie, spec.RandomIntervalStep, spec.StatIntervalStep, spec.HalfIntervalFlag, spec.RevealInvisibility = 600, true, 30, 10, 150, castResistMagicFlag, true
		spec.failMask = castAllSpellFailMask
	case castKnowAlignmentSpell:
		spec.Cost, spec.healKind = 6, castHealTimed
		spec.Flag, spec.Timer = castKnowAlignmentFlag, castKnowAlignmentTimer
		spec.IntervalBase, spec.RoomExtend, spec.MinInterval = 1200, 800, true
	case castLocateSpell:
		spec.Cost, spec.healKind = locatePlayerCost, castHealLocate
	case castSummonSpell:
		spec.Cost, spec.healKind = summonNormalCost, castHealSummon
	case castRecallSpell:
		spec.Cost, spec.healKind, spec.classGate = recallCost, castHealRecall, castClericInvincible
	default:
		return castSpellSpec{}, ErrCastSpellUnavailable
	}
	return spec, nil
}

const (
	castAssassinClass  = 1
	castBarbarianClass = 2
	castClericClass    = 3
	castFighterClass   = 4
	castMageClass      = 5
	castPaladinClass   = 6
	castInvincible     = 9
	castSubDMClass     = 11
)

type CastOptions struct {
	Now    int32
	Hour   int
	Roll   func(int, int) int
	Target string
	// Occurrence is the one-based player occurrence from the legacy
	// command's val[2]. Zero means the existing targeted API omitted the
	// suffix and is normalized to the first occurrence by recall.
	Occurrence int
}

type CastEvent struct {
	RoomID          int16  `json:"room_id"`
	ActorID         string `json:"actor_id"`
	ActorName       string `json:"actor_name"`
	SpellName       string `json:"spell_name"`
	ExcludeActorID  string `json:"exclude_actor_id"`
	ExcludeTargetID string `json:"exclude_target_id,omitempty"`
	Text            string `json:"text"`
}

type CastResult struct {
	Action             string     `json:"action"`
	Response           string     `json:"response"`
	Broadcast          bool       `json:"broadcast"`
	Changed            bool       `json:"changed"`
	Attempted          bool       `json:"attempted,omitempty"`
	Succeeded          bool       `json:"succeeded,omitempty"`
	SpellFailed        bool       `json:"spell_failed,omitempty"`
	DailyUsed          bool       `json:"daily_used,omitempty"`
	HiddenCleared      bool       `json:"hidden_cleared,omitempty"`
	SpellName          string     `json:"spell_name,omitempty"`
	SpellIndex         int        `json:"spell_index,omitempty"`
	Cost               int16      `json:"cost,omitempty"`
	Chance             int        `json:"chance,omitempty"`
	Roll               int        `json:"roll,omitempty"`
	EffectRolls        []int      `json:"effect_rolls,omitempty"`
	HPDelta            int32      `json:"hp_delta,omitempty"`
	MPDelta            int32      `json:"mp_delta,omitempty"`
	Now                int32      `json:"now,omitempty"`
	Hour               int        `json:"hour,omitempty"`
	SpellInterval      int32      `json:"spell_interval,omitempty"`
	TimedFlag          uint       `json:"timed_flag,omitempty"`
	TimedInterval      int32      `json:"timed_interval,omitempty"`
	CursedItemsCleared int        `json:"cursed_items_cleared,omitempty"`
	TargetID           string     `json:"target_id,omitempty"`
	TargetName         string     `json:"target_name,omitempty"`
	LocateLinked       bool       `json:"locate_linked,omitempty"`
	TargetText         string     `json:"target_text,omitempty"`
	Event              *CastEvent `json:"event,omitempty"`
}

// CastEvent.ExcludeTargetID is set for targeted casts so a post-apply room
// publish can omit the summoned/scryed player the way C broadcast_rom2 does.

// CastProposal is the snapshot-bound receipt candidate.  ApplyCast never
// calls Roll: all spell-fail/effect draws are copied into EffectRolls during
// planning and validated from those values during apply/replay.
type CastProposal struct {
	Action             string
	ActorID            string
	RoomID             int16
	SpellName          string
	SpellIndex         int
	Cost               int16
	HealKind           int
	Now                int32
	Chance             int
	Roll               int
	EffectRolls        []int
	HPDelta            int32
	MPDelta            int32
	SpellInterval      int32
	Attempted          bool
	Succeeded          bool
	SpellFailed        bool
	DailyUsed          bool
	HiddenCleared      bool
	Changed            bool
	Broadcast          bool
	Response           string
	RoomText           string
	TimedFlag          uint
	TimedInterval      int32
	CursedItemsCleared int
	TargetID           string
	TargetName         string
	TargetOccurrence   int
	LocateLinked       bool
	TargetText         string
	Hour               int

	expectedActor     PlayerState
	expectedRoomFlags [8]byte
	// knowAlignmentTargeted binds the explicit player-target form to its
	// reducer. It must survive validation even if the exported target fields
	// are edited after planning, so a targeted receipt cannot fall through to
	// the generic self-cast path.
	knowAlignmentTargeted         bool
	knowAlignmentTargetID         string
	knowAlignmentTargetName       string
	knowAlignmentTargetOccurrence int
	knowAlignmentExpectedTarget   PlayerState
	expectedTarget                PlayerState
	expectedTargetSet             bool
	expectedTargetRoomFlags       [8]byte
	targetRoomID                  int16
	sourceRoomID                  int16
	afterSourceIDs                []string
	afterDestIDs                  []string
	expectedSourceIDs             []string
	expectedDestIDs               []string
	afterDestBeenHere             int32
	deactivateSource              bool
	afterActiveNPCIDs             []string
	afterActiveSet                bool
	afterBody                     LegacyMonster
	afterTarget                   PlayerState
	afterItems                    ItemCollection
	afterItemsSet                 bool
}

func castActor(s State, actorID string) (PlayerState, RoomState, error) {
	if err := s.Validate(); err != nil {
		return PlayerState{}, RoomState{}, err
	}
	actor, ok := s.Players[actorID]
	if actorID == "" || !ok || !actor.Online || actor.Body.Type != 0 || actor.Body.Name == "" {
		return PlayerState{}, RoomState{}, ErrCastActorAbsent
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok {
		return PlayerState{}, RoomState{}, ErrCastActorAbsent
	}
	if actor.Body.Stats[3] > 63 || actor.Body.Stats[4] > 63 || actor.Body.Class > maxLegacyClass {
		return PlayerState{}, RoomState{}, fmt.Errorf("cast actor stats/class outside legacy range")
	}
	return actor, room, nil
}

func castSpellFailRequired(spec castSpellSpec, class byte) bool {
	return spec.failMask != 0 && class < 16 && spec.failMask&(1<<class) != 0
}

func castRoll(options CastOptions, low, high int, draws *[]int) (value int, err error) {
	if options.Roll == nil {
		return 0, ErrCastRandom
	}
	if low < 1 || high < low {
		return 0, fmt.Errorf("cast random bounds outside legacy range")
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			value = 0
			err = fmt.Errorf("%w: random source panicked: %v", ErrCastRandom, recovered)
		}
	}()
	value = options.Roll(low, high)
	if value < low || value > high {
		return 0, fmt.Errorf("cast random value outside %d..%d", low, high)
	}
	if draws != nil {
		*draws = append(*draws, value)
	}
	return value, nil
}

func castReplayRoll(draws []int, at *int, low, high int) (int, error) {
	if at == nil || *at < 0 || *at >= len(draws) {
		return 0, fmt.Errorf("cast receipt missing random draw")
	}
	value := draws[*at]
	*at++
	if value < low || value > high {
		return 0, fmt.Errorf("cast receipt random value outside %d..%d", low, high)
	}
	return value, nil
}

func castSpellChance(body LegacyMonster) (int, error) {
	return readScrollSpellChance(body)
}

func castCooldownResponse(wait int64) string {
	if wait == 1 {
		return "1초만 기다리세요.\r\n"
	}
	return fmt.Sprintf("%d초동안 기다리세요.\r\n", wait)
}

func castClassAllowed(body LegacyMonster, spec castSpellSpec) bool {
	switch spec.classGate {
	case castAnyClass:
		return true
	case castClericInvincible:
		return body.Class == castClericClass || body.Class >= castInvincible
	case castClericPaladinInvincible:
		return body.Class == castClericClass || body.Class == castPaladinClass || body.Class >= castInvincible
	case castSubDMGate:
		return body.Class >= castSubDMClass
	default:
		return false
	}
}

func castSpellInterval(class byte) int32 {
	switch class {
	case castClericClass, castMageClass, 10: // CARETAKER
		return 3
	case 11, 12: // SUB_DM/DM
		return 0
	default:
		return 5
	}
}

func castTimedInterval(body LegacyMonster, room RoomState, spec castSpellSpec) (int32, error) {
	return castTimedIntervalWithRoll(body, room, spec, 0)
}

func castTimedIntervalWithRoll(body LegacyMonster, room RoomState, spec castSpellSpec, randomRoll int) (int32, error) {
	if body.Stats[3] > 63 {
		return 0, fmt.Errorf("cast timed spell intelligence outside legacy table")
	}
	if spec.RandomInterval {
		if spec.RandomIntervalDie < 1 || randomRoll < 1 || randomRoll > spec.RandomIntervalDie {
			return 0, fmt.Errorf("cast timed spell random interval outside source range")
		}
		interval := int64(spec.IntervalBase) + int64(randomRoll)*int64(spec.RandomIntervalStep) + int64(legacyStatBonus[body.Stats[3]])*int64(spec.StatIntervalStep)
		if spec.HalfIntervalFlag != 0 && flag(body.Flags[:], spec.HalfIntervalFlag) {
			interval /= 2
		}
		if interval < 0 || interval > math.MaxInt32 {
			return 0, fmt.Errorf("cast timed spell interval outside int32")
		}
		return int32(interval), nil
	}
	if spec.FixedInterval {
		interval := int64(spec.IntervalBase)
		if interval < 0 {
			return 0, fmt.Errorf("cast timed spell interval outside int32")
		}
		if spec.HalfIntervalFlag != 0 && flag(body.Flags[:], spec.HalfIntervalFlag) {
			interval /= 2
		}
		if interval > math.MaxInt32 {
			return 0, fmt.Errorf("cast timed spell interval outside int32")
		}
		return int32(interval), nil
	}
	base := spec.IntervalBase
	if base == 0 {
		base = 1200
	}
	interval := int64(base)
	if spec.LevelStep != 0 {
		interval += int64((int(body.Level)+3)/4) * int64(spec.LevelStep)
	} else {
		interval += int64(legacyStatBonus[body.Stats[3]]) * 600
	}
	if spec.MinInterval && interval < 300 {
		interval = 300
	}
	if spec.ClassIntervalTerm && (body.Class == castClericClass || body.Class == castPaladinClass) {
		interval += 60 * int64((int(body.Level)+3)/4)
	}
	if spec.MageInterval && body.Class == castMageClass {
		interval += int64(60 * ((int(body.Level) + 3) / 4))
	}
	if flag(room.Resource.Flags[:], npcTalkRoomMagicExtend) {
		extend := spec.RoomExtend
		if extend == 0 {
			extend = 800
		}
		interval += int64(extend)
	}
	if interval < 0 || interval > math.MaxInt32 {
		return 0, fmt.Errorf("cast timed spell interval outside int32")
	}
	return int32(interval), nil
}

func castTimedRollsValid(spec castSpellSpec, rolls []int) bool {
	if !spec.RandomInterval {
		return len(rolls) == 0
	}
	return len(rolls) == 1 && spec.RandomIntervalDie >= 1 && rolls[0] >= 1 && rolls[0] <= spec.RandomIntervalDie
}

// castApplyTimedEffect installs a source timer/flag and, for the two timed
// combat buffs, refreshes the legacy-derived value from the canonical item
// graph. The item graph is an explicit admission boundary: a migrated player
// has an ID collection (possibly empty), while a legacy-only player is kept
// fail-closed instead of mixing nested legacy objects into a receipt.
func castApplyTimedEffect(body *LegacyMonster, items *ItemCollection, spec castSpellSpec, now, interval int32) error {
	if body == nil || spec.Flag >= uint(len(body.Flags)*8) || spec.Timer < 0 || spec.Timer >= len(body.Timers) || now < 0 || interval < 0 {
		return ErrCastSpellUnavailable
	}
	setSettingFlag(body, spec.Flag, true)
	body.Timers[spec.Timer] = LegacyTimer{LastTime: now, Interval: interval}
	if !spec.CombatStats {
		return nil
	}
	if items == nil || len(body.Inventory) != 0 {
		return fmt.Errorf("%w: canonical equipment required", ErrCastSpellUnavailable)
	}
	if err := items.Validate(); err != nil {
		return fmt.Errorf("%w: canonical equipment invalid: %v", ErrCastSpellUnavailable, err)
	}
	stats, err := items.CombatStats(*body)
	if err != nil {
		return fmt.Errorf("%w: canonical combat stats unavailable: %v", ErrCastSpellUnavailable, err)
	}
	switch spec.Flag {
	case castBlessFlag:
		// compute_thaco stores before applying PBLESS's local -3 adjustment in
		// the original C routine; ComputeThaco preserves that observable bug.
		body.Thaco = byte(stats.Thaco)
	case castProtectionFlag:
		body.Armor = byte(stats.Armor)
	default:
		return ErrCastSpellUnavailable
	}
	return nil
}

// castClearCursedReady mirrors remove_curse's self-target branch: only
// equipped roots are un-cursed, while inventory/container objects remain
// untouched. The returned collection is a receipt projection, never a
// mutation of the planning snapshot.
func castClearCursedReady(items *ItemCollection) (ItemCollection, int, error) {
	if items == nil {
		return ItemCollection{}, 0, fmt.Errorf("%w: canonical equipment required", ErrCastSpellUnavailable)
	}
	if err := items.Validate(); err != nil {
		return ItemCollection{}, 0, fmt.Errorf("%w: canonical equipment invalid: %v", ErrCastSpellUnavailable, err)
	}
	next := items.clone()
	cleared := 0
	for _, id := range next.Ready {
		if id == "" {
			continue
		}
		item := next.Items[id]
		if flag(item.Object.Flags[:], objectCursedFlag) {
			setObjectFlag(&item.Object.Flags, objectCursedFlag, false)
			next.Items[id] = item
			cleared++
		}
	}
	return next, cleared, nil
}

func castCombatActive(s State, actorID string, actor PlayerState) bool {
	if len(actor.PlayerEnemies) != 0 {
		return true
	}
	for _, npc := range s.NPCs {
		if npc.Body.RoomID != actor.Body.RoomID || npc.Enemies == nil {
			continue
		}
		for _, enemy := range npc.Enemies {
			if enemy.Target.Kind == "player" && enemy.Target.ID == actorID {
				return true
			}
		}
	}
	return false
}

func castEffectBounds(body LegacyMonster, room RoomState, kind int) ([][2]int, error) {
	if body.Stats[3] > 63 || body.Stats[4] > 63 {
		return nil, fmt.Errorf("cast healing stat outside legacy table")
	}
	levelBand := (int(body.Level) + 3) / 4
	bounds := make([][2]int, 0, 5)
	appendRoll := func(low, high int) { bounds = append(bounds, [2]int{low, high}) }
	switch kind {
	case castHealVigor:
		if body.Class == castClericClass {
			appendRoll(1, 1+levelBand/2)
		}
		if body.Class == castPaladinClass {
			appendRoll(1, 1+levelBand/4)
		}
		appendRoll(1, 6)
		if flag(room.Resource.Flags[:], npcTalkRoomMagicExtend) {
			appendRoll(1, 3)
		}
	case castHealMend:
		// mend() grants the cleric term to invincible and higher classes too.
		if body.Class == castClericClass || body.Class >= castInvincible {
			appendRoll(1, 1+levelBand/2)
		}
		if body.Class == castPaladinClass {
			appendRoll(1, 1+levelBand/3)
		}
		appendRoll(1, 6)
		appendRoll(1, 6)
		if flag(room.Resource.Flags[:], npcTalkRoomMagicExtend) {
			appendRoll(1, 6)
		}
	default:
		return nil, fmt.Errorf("cast healing effect unavailable")
	}
	return bounds, nil
}

func castHealingAmount(body LegacyMonster, room RoomState, kind int, rolls []int) (int32, error) {
	bounds, err := castEffectBounds(body, room, kind)
	if err != nil {
		return 0, err
	}
	if len(rolls) != len(bounds) {
		return 0, fmt.Errorf("cast healing roll count changed")
	}
	cursor := 0
	nextRoll := func() (int64, error) {
		if cursor >= len(bounds) {
			return 0, fmt.Errorf("cast healing roll order changed")
		}
		bound := bounds[cursor]
		value := rolls[cursor]
		cursor++
		if value < bound[0] || value > bound[1] {
			return 0, fmt.Errorf("cast healing roll outside %d..%d", bound[0], bound[1])
		}
		return int64(value), nil
	}
	heal := int64(legacyStatBonus[body.Stats[3]])
	if piety := int64(legacyStatBonus[body.Stats[4]]); piety > heal {
		heal = piety
	}
	levelBand := (int(body.Level) + 3) / 4
	switch kind {
	case castHealVigor:
		if body.Class == castClericClass {
			heal += int64(levelBand)
			value, err := nextRoll()
			if err != nil {
				return 0, err
			}
			heal += value
		}
		if body.Class == castPaladinClass {
			heal += int64(levelBand / 2)
			value, err := nextRoll()
			if err != nil {
				return 0, err
			}
			heal += value
		}
		value, err := nextRoll()
		if err != nil {
			return 0, err
		}
		heal += value
		if flag(room.Resource.Flags[:], npcTalkRoomMagicExtend) {
			value, err = nextRoll()
			if err != nil {
				return 0, err
			}
			heal += value
		}
	case castHealMend:
		if body.Class == castClericClass || body.Class >= castInvincible {
			heal += int64(levelBand * 2)
			value, err := nextRoll()
			if err != nil {
				return 0, err
			}
			heal += value
		}
		if body.Class == castPaladinClass {
			heal += int64(levelBand)
			value, err := nextRoll()
			if err != nil {
				return 0, err
			}
			heal += value
		}
		for i := 0; i < 2; i++ {
			value, err := nextRoll()
			if err != nil {
				return 0, err
			}
			heal += value
		}
		if flag(room.Resource.Flags[:], npcTalkRoomMagicExtend) {
			value, err := nextRoll()
			if err != nil {
				return 0, err
			}
			heal += value + 1
		}
	default:
		return 0, fmt.Errorf("cast healing effect unavailable")
	}
	if cursor != len(bounds) || heal < 1 || heal > math.MaxInt32 {
		return 0, fmt.Errorf("cast healing amount outside int32")
	}
	return int32(heal), nil
}

func castDailyUse(before LegacyDaily, now int32) (LegacyDaily, bool, error) {
	if now < 0 {
		return LegacyDaily{}, false, fmt.Errorf("cast clock must be nonnegative")
	}
	out := before
	current := time.Unix(int64(now), 0).In(time.Local)
	last := time.Unix(int64(before.LastTime), 0).In(time.Local)
	// dec_daily compares tm_yday in the original implementation. Preserve that
	// observable contract instead of introducing a new calendar authority.
	if current.YearDay() != last.YearDay() {
		out.Current = out.Max
		out.LastTime = now
	}
	if out.Current == 0 {
		return out, false, nil
	}
	out.Current--
	return out, true, nil
}

func castBodyAfter(body LegacyMonster, room RoomState, spec castSpellSpec, options CastOptions, effectRolls []int, dailyUsed bool, failed bool) (LegacyMonster, int32, error) {
	after := body
	if failed {
		if body.MPCurrent < spec.Cost {
			return LegacyMonster{}, 0, fmt.Errorf("cast failed spell has insufficient mana")
		}
		after.MPCurrent -= spec.Cost
		setSettingFlag(&after, castHiddenFlag, false)
		if spec.RevealInvisibility {
			setSettingFlag(&after, castInvisibilityFlag, false)
		}
		return after, int32(after.MPCurrent) - int32(body.MPCurrent), nil
	}
	if body.MPCurrent < spec.Cost {
		return LegacyMonster{}, 0, fmt.Errorf("cast success has insufficient mana")
	}
	after.MPCurrent -= spec.Cost
	setSettingFlag(&after, castHiddenFlag, false)
	if spec.RevealInvisibility {
		setSettingFlag(&after, castInvisibilityFlag, false)
	}
	switch spec.healKind {
	case castHealVigor, castHealMend:
		heal, err := castHealingAmount(body, room, spec.healKind, effectRolls)
		if err != nil {
			return LegacyMonster{}, 0, err
		}
		before := after.HPCurrent
		value := int64(after.HPCurrent) + int64(heal)
		if value > int64(after.HPMax) {
			value = int64(after.HPMax)
		}
		if value < -32768 || value > 32767 {
			return LegacyMonster{}, 0, fmt.Errorf("cast healing HP outside int16")
		}
		after.HPCurrent = int16(value)
		return after, int32(after.HPCurrent) - int32(before), nil
	case castHealFull:
		before := after.HPCurrent
		after.HPCurrent = after.HPMax
		return after, int32(after.HPCurrent) - int32(before), nil
	case castHealTimed:
		// Timed utility spells only consume mana here. PlanCast/ApplyCast write
		// the spell-specific flag and timer after this pure body transition.
		return after, 0, nil
	case castHealCleanse:
		if spec.Flag >= uint(len(after.Flags)*8) {
			return LegacyMonster{}, 0, ErrCastSpellUnavailable
		}
		setSettingFlag(&after, spec.Flag, false)
		return after, 0, nil
	case castHealFlag:
		if spec.Flag >= uint(len(after.Flags)*8) || spec.Timer != -1 {
			return LegacyMonster{}, 0, ErrCastSpellUnavailable
		}
		setSettingFlag(&after, spec.Flag, true)
		return after, 0, nil
	case castHealLocate, castHealSummon, castHealRecall:
		return after, 0, nil
	default:
		return LegacyMonster{}, 0, ErrCastSpellUnavailable
	}
}

func castResponse(spec castSpellSpec, failed bool, noOp string) string {
	if noOp != "" {
		return noOp
	}
	if failed {
		return "당신의 주문이 실패했습니다.\r\n"
	}
	switch spec.healKind {
	case castHealVigor:
		return "당신은 합장을 하고서 회복 주문을 외웁니다.\r\n빛의 정기가 온몸에 스며들면서 체력이 향상되었습니다.\r\n"
	case castHealMend:
		return "당신은 기공팔식의 자세를 취하며 원기회복의 주문을 외웁니다.\r\n지기의 뜨거운 기운이 당신의 몸에 가득차 체력을 향상시킵니다.\r\n"
	case castHealFull:
		return "당신은 천부공을 끌어올리며 완치 주문을 외웁니다.\r\n천상의 기운들이 당신의 몸으로 모이면서 체력을 최상으로 올려 줍니다.\r\n"
	case castHealTimed:
		switch spec.Index {
		case castBlessSpell:
			return "당신은 조용히 눈을 감으며 성현진을 외웁니다.\r\n성현진을 외우자 머리에서 삼매광이 뿜어져 나와 성스런 기운이 몸을 휘감습니다.\r\n"
		case castProtectionSpell:
			return "두손으로 인을 맺은 뒤 수호진의 주문을 외웁니다.\r\n빛의 수호령들이 당신 주위를 둘러싸며 방어의 진을 형성했습니다.\r\n"
		case castInvisibilitySpell:
			return "당신은 소명부를 삼키면서 은둔법의 주문을 외웁니다.\r\n몸이 눈부실 정도로 강렬한 빛을 내다가 갑자기 사라졌습니다.\r\n"
		case castDetectInvisibleSpell:
			return "당신은 버들잎을 두눈에 비비며 은둔감지술의 주문을 외웁니다.\r\n두눈에 푸른광안이 떠오르며 숨어있는 자들을 볼수 있게되었습니다.\r\n"
		case castDetectMagicSpell:
			return "당신은 됴화잎을 눈에 비비며 주문감지술을 외웁니다.\r\n당신의 눈에서 은빛광안이 떠오르며 주술에 관한 안목이 넓어졌습니다.\r\n"
		case castKnowAlignmentSpell:
			return "당신은 선악감지 주문을 외웁니다.\r\n당신은 선악을 감지할 수 있는 식별력이 높아졌습니다.\r\n"
		case castLightSpell:
			return "당신의 왼손에 발광 주문을 걸었습니다.\r\n왼손에서 황금빛이 뿜어져 나와 주위를 밝혀 줍니다.\r\n"
		case castFearSpell:
			return "당신은 실수로 지옥구슬을 떨어뜨렸습니다.\r\n갑자기 공포의 기운이 당신을 둘러싸며 공포에 떨기 시작합니다.\r\n"
		case castSilenceSpell:
			return "당신은 실수로 봉합구 주문을 자신에게 걸었습니다.\r\n입을 벌려 말을 하려 하지만 목소리가 사라졌습니다.\r\n"
		default:
			return fmt.Sprintf("당신은 %s 주문을 외웁니다.\r\n", spec.Name)
		}
	case castHealCleanse:
		switch spec.Index {
		case castRemoveCurseSpell:
			return "당신은 오른손에 성스러운 기운을 모으자 저주해소 주문을 외웁니다.\r\n붉은 빛이 퍼져나가며 당신의 몸에 걸렸던 저주가 풀리기 시작합니다.\r\n"
		case castCurePoisonSpell:
			return "당신은 오른손으로 혈도를 짚으면서 해독 주문을 외웁니다.\r\n당신 몸에 남아 있는 독이 모두 빠져나갔습니다.\r\n"
		case castDiseaseSpell:
			return "당신은 생사과를 먹으며 치료 주문을 외웁니다.\r\n병마에 시달리던 당신의 몸이 활기를 띄기 시작합니다.\r\n"
		case castRemoveBlindSpell:
			return "당신의 이마에 개안부를 붙히며 개안술 주문을 외웁니다.\r\n감겼던 눈이 움찔거리다가 갑자기 눈앞이 밝아집니다.\r\n"
		default:
			return fmt.Sprintf("당신은 %s 주문을 외웁니다.\r\n", spec.Name)
		}
	case castHealFlag:
		if spec.Index == castBlindSpell {
			return "당신은 두손가락을 독수리 발톱모양으로 하고서 실명 주문을 걸었습니다.\r\n검은 안개가 눈을 찔러 앞이 보이지 않습니다.\r\n"
		}
		return fmt.Sprintf("당신은 %s 주문을 외웁니다.\r\n", spec.Name)
	default:
		return ""
	}
}

func castRoomText(actorName string, spec castSpellSpec) string {
	switch spec.healKind {
	case castHealVigor:
		return fmt.Sprintf("\n%s이 합장을 하고서 주문을 외웁니다.\r\n빛의 정기가 그의 몸으로 모이는 것이 보입니다.\r\n", actorName)
	case castHealMend:
		return fmt.Sprintf("\n%s이 기공팔식의 자세를 취하며 원기회복의 주문을 외웁니다.\r\n지기의 뜨거운 기운이 그에게 흘러가는 것이 느껴집니다.\r\n", actorName)
	case castHealFull:
		return fmt.Sprintf("\n%s이 천부공 자세를 취하면서 완치주문을 외웠습니다.\r\n천상의 기운들이 그에게로 모이는 것이 느껴집니다.\r\n", actorName)
	case castHealTimed, castHealCleanse, castHealFlag:
		return fmt.Sprintf("\n%s이 %s 주문을 외웁니다.\r\n", actorName, spec.Name)
	default:
		return ""
	}
}

// PlanCast evaluates `주문 <spell>` and the targeted `주문 천리안 <name>` /
// `주문 소환 <name>` paths, plus self-cast `주문 귀환`. Prompt, gate and cooldown
// responses are represented as no-op receipts so the terminal sees deterministic
// output without mutating canonical state.
func (s State) PlanCast(actorID, spellName string, options CastOptions) (CastProposal, error) {
	if options.Now < 0 {
		return CastProposal{}, fmt.Errorf("cast clock must be nonnegative")
	}
	if options.Hour < 0 {
		return CastProposal{}, fmt.Errorf("cast hour must be nonnegative")
	}
	actor, room, err := castActor(s, actorID)
	if err != nil {
		return CastProposal{}, err
	}
	p := CastProposal{Action: "cast", ActorID: actorID, RoomID: actor.Body.RoomID, Now: options.Now, Hour: options.Hour, expectedActor: cloneDrinkActor(actor), expectedRoomFlags: room.Resource.Flags}
	if spellName == "" {
		p.Response = "어떤 주술을 펼치실겁니까?\r\n"
		return p, nil
	}
	if flag(actor.Body.Flags[:], castBlindFlag) {
		p.Response = "아무것도 보이지 않습니다!\r\n"
		return p, nil
	}
	if flag(actor.Body.Flags[:], castSilentFlag) {
		p.Response = "한마디도 할수 없습니다!\r\n"
		return p, nil
	}
	spec, err := castSpellSpecFor(spellName)
	if errors.Is(err, ErrCastSpellAmbiguous) {
		p.Response = "펼치실 주문의 이름이 이상하군요.\r\n"
		return p, nil
	}
	if errors.Is(err, ErrCastSpellUnavailable) {
		p.Response = "그런 주문은 존재하지 않습니다.\r\n"
		return p, nil
	}
	if err != nil {
		return CastProposal{}, err
	}
	p.SpellName, p.SpellIndex, p.Cost, p.HealKind = spec.Name, spec.Index, spec.Cost, spec.healKind
	if flag(room.Resource.Flags[:], castNoMagicRoomFlag) {
		p.Response = "주술을 출수 하는데 실패 하셨습니다.\r\n"
		return p, nil
	}
	timer := actor.Body.Timers[castSpellTimerIndex]
	if timer.LastTime < 0 || timer.Interval < 0 {
		return CastProposal{}, fmt.Errorf("cast spell timer outside legacy range")
	}
	deadline := int64(timer.LastTime) + int64(timer.Interval)
	if int64(options.Now) < deadline {
		p.Response = castCooldownResponse(deadline - int64(options.Now))
		return p, nil
	}
	// magic1.c clears PHIDDN immediately before invoking the selected spell
	// function. Thus an otherwise rejected spell (insufficient mana, an
	// unlearned bit, or a class/daily gate) still reveals a hidden caster.
	p.HiddenCleared = flag(actor.Body.Flags[:], castHiddenFlag)
	if p.HiddenCleared {
		p.afterBody = actor.Body
		setSettingFlag(&p.afterBody, castHiddenFlag, false)
		p.Changed = true
	}
	if spec.healKind == castHealSummon {
		return s.planSummonCast(p, actor, room, spec, options)
	}
	if spec.healKind == castHealLocate {
		return s.planLocateCast(p, actor, room, spec, options)
	}
	if spec.healKind == castHealRecall {
		return s.planRecallCast(p, actor, room, spec, options)
	}
	if spec.Index == castKnowAlignmentSpell && options.Target != "" {
		return s.planKnowAlignmentCast(p, actor, room, spec, options)
	}
	if actor.Body.MPCurrent < spec.Cost {
		p.Response = "당신의 도력이 부족합니다.\r\n"
		return p, nil
	}
	if !castClassAllowed(actor.Body, spec) {
		p.Response = "불제자와 무사만이 이 주술을 사용할 수 있습니다.\r\n"
		return p, nil
	}
	if !legacyInfoFlag(actor.Body.Spells[:], uint(spec.Index)) {
		p.Response = "당신은 아직 그 주술을 터득하지 못했습니다.\r\n"
		return p, nil
	}
	if spec.CombatGate && castCombatActive(s, actorID, actor) {
		// The legacy routine checks combat after spell_fail. The canonical
		// receipt intentionally checks first so a blocked cast is a true no-op:
		// no RNG draw, mana charge, or timer write can be replayed ambiguously.
		p.Response = "지금 싸우고 있잖아요..!!.\r\n"
		return p, nil
	}
	var clearedItems ItemCollection
	var clearedCount int
	if spec.ClearCursedReady {
		var clearErr error
		clearedItems, clearedCount, clearErr = castClearCursedReady(actor.Items)
		if clearErr != nil {
			return CastProposal{}, clearErr
		}
	}
	if spec.CombatStats {
		// Validate the canonical equipment projection before spell_fail so a
		// legacy-only body cannot consume an unreplayable random draw.
		probe := actor.Body
		if err := castApplyTimedEffect(&probe, actor.Items, spec, 0, 0); err != nil {
			return CastProposal{}, err
		}
	}
	dailyUsed := false
	dailyAfter := actor.Body.Daily[castDailyHealIndex]
	if spec.healKind == castHealFull {
		updated, allowed, err := castDailyUse(actor.Body.Daily[castDailyHealIndex], options.Now)
		if err != nil {
			return CastProposal{}, err
		}
		dailyUsed = allowed
		if !allowed && actor.Body.Class < 10 {
			p.Response = "당신의 몸이 너무 피곤해 이 주술을 더 이상 펼칠 수 없습니다.\r\n"
			return p, nil
		}
		p.DailyUsed = allowed
		dailyAfter = updated
	}
	spellFailRequired := castSpellFailRequired(spec, actor.Body.Class)
	chance, roll := 100, 0
	var timedRolls []int
	if spec.RandomInterval {
		if _, err := castRoll(options, 1, spec.RandomIntervalDie, &timedRolls); err != nil {
			return CastProposal{}, err
		}
	}
	if spellFailRequired {
		chance, err = castSpellChance(actor.Body)
		if err != nil {
			return CastProposal{}, err
		}
		roll, err = castRoll(options, 1, 100, nil)
		if err != nil {
			return CastProposal{}, err
		}
	}
	p.Attempted, p.Chance, p.Roll, p.EffectRolls = true, chance, roll, timedRolls
	p.Changed = true
	p.afterBody = actor.Body
	if spellFailRequired && roll > chance {
		p.SpellFailed = true
		p.Response = castResponse(spec, true, "")
		p.afterBody, p.MPDelta, err = castBodyAfter(actor.Body, room, spec, CastOptions{}, nil, false, true)
		if err != nil {
			return CastProposal{}, err
		}
		p.expectedActor = cloneDrinkActor(actor)
		p.RoomText = ""
		return p, nil
	}
	if spec.healKind == castHealVigor || spec.healKind == castHealMend {
		bounds, err := castEffectBounds(actor.Body, room, spec.healKind)
		if err != nil {
			return CastProposal{}, err
		}
		p.EffectRolls = make([]int, 0, len(bounds))
		for _, bound := range bounds {
			value, err := castRoll(options, bound[0], bound[1], &p.EffectRolls)
			if err != nil {
				return CastProposal{}, err
			}
			_ = value
		}
	}
	p.Succeeded = true
	p.SpellInterval = castSpellInterval(actor.Body.Class)
	if spec.healKind == castHealTimed {
		p.TimedFlag = spec.Flag
		randomRoll := 0
		if len(timedRolls) == 1 {
			randomRoll = timedRolls[0]
		}
		p.TimedInterval, err = castTimedIntervalWithRoll(actor.Body, room, spec, randomRoll)
		if err != nil {
			return CastProposal{}, err
		}
	}
	p.afterBody, p.HPDelta, err = castBodyAfter(actor.Body, room, spec, options, p.EffectRolls, dailyUsed, false)
	if err != nil {
		return CastProposal{}, err
	}
	p.afterBody.Timers[castSpellTimerIndex] = LegacyTimer{LastTime: options.Now, Interval: p.SpellInterval}
	if spec.healKind == castHealTimed {
		if err := castApplyTimedEffect(&p.afterBody, actor.Items, spec, options.Now, p.TimedInterval); err != nil {
			return CastProposal{}, err
		}
	}
	if spec.ClearCursedReady {
		p.afterItems = clearedItems
		p.afterItemsSet = true
		p.CursedItemsCleared = clearedCount
	}
	if spec.healKind == castHealFull {
		p.afterBody.Daily[castDailyHealIndex] = dailyAfter
	}
	p.MPDelta = int32(p.afterBody.MPCurrent) - int32(actor.Body.MPCurrent)
	p.DailyUsed = spec.healKind == castHealFull && p.DailyUsed
	p.Broadcast = true
	p.RoomText = castRoomText(actor.Body.Name, spec)
	p.Response = castResponse(spec, false, "")
	return p, nil
}

func castResult(p CastProposal, actor PlayerState) CastResult {
	result := CastResult{Action: p.Action, Response: p.Response, Broadcast: p.Broadcast, Changed: p.Changed, Attempted: p.Attempted, Succeeded: p.Succeeded, SpellFailed: p.SpellFailed, DailyUsed: p.DailyUsed, HiddenCleared: p.HiddenCleared, SpellName: p.SpellName, SpellIndex: p.SpellIndex, Cost: p.Cost, Chance: p.Chance, Roll: p.Roll, EffectRolls: append([]int(nil), p.EffectRolls...), HPDelta: p.HPDelta, MPDelta: p.MPDelta, Now: p.Now, Hour: p.Hour, SpellInterval: p.SpellInterval, TimedFlag: p.TimedFlag, TimedInterval: p.TimedInterval, CursedItemsCleared: p.CursedItemsCleared, TargetID: p.TargetID, LocateLinked: p.LocateLinked, TargetText: p.TargetText}
	if p.expectedTargetSet {
		result.TargetName = p.expectedTarget.Body.Name
	}
	if p.Broadcast {
		result.Event = &CastEvent{RoomID: p.RoomID, ActorID: p.ActorID, ActorName: actor.Body.Name, SpellName: p.SpellName, ExcludeActorID: p.ActorID, ExcludeTargetID: p.TargetID, Text: p.RoomText}
	}
	return result
}

// ApplyCast verifies every state and effect field from the proposal, then
// applies the already-recorded transition without consulting the host RNG.
func (s State) ApplyCast(p CastProposal) (State, CastResult, error) {
	actor, room, err := castActor(s, p.ActorID)
	if err != nil {
		return State{}, CastResult{}, err
	}
	if p.Action != "cast" || p.RoomID != actor.Body.RoomID || p.Response == "" || !reflect.DeepEqual(actor, p.expectedActor) || room.Resource.Flags != p.expectedRoomFlags {
		return State{}, CastResult{}, ErrCastStaleProposal
	}
	if p.SpellName == "" {
		if p.Changed || p.HiddenCleared || p.Broadcast || p.Attempted || p.Succeeded || p.SpellFailed || p.SpellIndex != 0 || p.Cost != 0 || p.TimedFlag != 0 || p.TimedInterval != 0 || p.CursedItemsCleared != 0 || p.afterItemsSet || p.RoomText != "" {
			return State{}, CastResult{}, ErrCastInvalidProposal
		}
		return s.clone(), castResult(p, actor), nil
	}
	spec, err := castSpellSpecFor(p.SpellName)
	if err != nil || spec.Name != p.SpellName || spec.Index != p.SpellIndex || spec.healKind != p.HealKind {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}
	if spec.healKind == castHealSummon {
		return s.applySummonCast(p, actor, room, spec)
	}
	if spec.healKind == castHealRecall {
		return s.applyRecallCast(p, actor, room, spec)
	}
	if spec.Index == castKnowAlignmentSpell && knowAlignmentTargetProposalPresent(p) {
		return s.applyKnowAlignmentCast(p, actor, room, spec)
	}
	if spec.Cost != p.Cost {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}
	if spec.healKind == castHealLocate {
		return s.applyLocateCast(p, actor, room, spec)
	}
	if !p.Attempted {
		if p.Broadcast || p.Succeeded || p.SpellFailed || p.EffectRolls != nil || p.HPDelta != 0 || p.MPDelta != 0 || p.SpellInterval != 0 || p.TimedFlag != 0 || p.TimedInterval != 0 || p.CursedItemsCleared != 0 || p.afterItemsSet {
			return State{}, CastResult{}, ErrCastInvalidProposal
		}
		if p.HiddenCleared {
			if !p.Changed {
				return State{}, CastResult{}, ErrCastInvalidProposal
			}
			after := actor.Body
			setSettingFlag(&after, castHiddenFlag, false)
			if !reflect.DeepEqual(after, p.afterBody) {
				return State{}, CastResult{}, ErrCastInvalidProposal
			}
			next := s.clone()
			nextActor := next.Players[p.ActorID]
			nextActor.Body = after
			next.Players[p.ActorID] = nextActor
			if err := next.Validate(); err != nil {
				return State{}, CastResult{}, err
			}
			return next, castResult(p, nextActor), nil
		}
		if p.Changed || !reflect.DeepEqual(p.afterBody, LegacyMonster{}) {
			return State{}, CastResult{}, ErrCastInvalidProposal
		}
		return s.clone(), castResult(p, actor), nil
	}
	if !p.Changed || p.Broadcast != p.Succeeded || p.Succeeded == p.SpellFailed || p.Chance < 1 || p.Chance > math.MaxInt32 || p.Roll < 0 || p.Roll > 100 || p.CursedItemsCleared < 0 {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}
	if p.HiddenCleared != flag(actor.Body.Flags[:], castHiddenFlag) {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}
	if castSpellFailRequired(spec, actor.Body.Class) {
		chance, err := castSpellChance(actor.Body)
		if err != nil || chance != p.Chance || p.Roll < 1 {
			return State{}, CastResult{}, ErrCastInvalidProposal
		}
	} else if p.Chance != 100 || p.Roll != 0 || p.SpellFailed {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}
	if p.SpellFailed {
		if !castTimedRollsValid(spec, p.EffectRolls) || p.DailyUsed || p.SpellInterval != 0 || p.TimedFlag != 0 || p.TimedInterval != 0 || p.CursedItemsCleared != 0 || p.afterItemsSet || p.Broadcast || p.RoomText != "" || p.HPDelta != 0 {
			return State{}, CastResult{}, ErrCastInvalidProposal
		}
		after, mpDelta, err := castBodyAfter(actor.Body, room, spec, CastOptions{}, nil, false, true)
		if err != nil || mpDelta != p.MPDelta || !reflect.DeepEqual(after, p.afterBody) || p.Response != castResponse(spec, true, "") {
			return State{}, CastResult{}, ErrCastInvalidProposal
		}
		next := s.clone()
		nextActor := next.Players[p.ActorID]
		nextActor.Body = after
		next.Players[p.ActorID] = nextActor
		if err := next.Validate(); err != nil {
			return State{}, CastResult{}, err
		}
		return next, castResult(p, nextActor), nil
	}
	if p.SpellInterval != castSpellInterval(actor.Body.Class) || p.SpellInterval < 0 || p.RoomText != castRoomText(actor.Body.Name, spec) || p.Response != castResponse(spec, false, "") {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}
	if spec.healKind == castHealTimed {
		if p.DailyUsed || !castTimedRollsValid(spec, p.EffectRolls) || p.HPDelta != 0 || p.CursedItemsCleared != 0 || p.afterItemsSet || p.TimedFlag != spec.Flag || p.TimedInterval < 0 {
			return State{}, CastResult{}, ErrCastInvalidProposal
		}
		randomRoll := 0
		if spec.RandomInterval {
			randomRoll = p.EffectRolls[0]
		}
		expectedInterval, err := castTimedIntervalWithRoll(actor.Body, room, spec, randomRoll)
		if err != nil || p.TimedInterval != expectedInterval || spec.Flag >= uint(len(actor.Body.Flags)*8) || spec.Timer < 0 || spec.Timer >= len(actor.Body.Timers) {
			return State{}, CastResult{}, ErrCastInvalidProposal
		}
	} else if spec.healKind == castHealCleanse {
		if p.DailyUsed || len(p.EffectRolls) != 0 || p.HPDelta != 0 || p.TimedFlag != 0 || p.TimedInterval != 0 || spec.Flag >= uint(len(actor.Body.Flags)*8) || spec.Timer != -1 {
			return State{}, CastResult{}, ErrCastInvalidProposal
		}
		if spec.ClearCursedReady {
			if !p.afterItemsSet {
				return State{}, CastResult{}, ErrCastInvalidProposal
			}
		} else if p.afterItemsSet || p.CursedItemsCleared != 0 {
			return State{}, CastResult{}, ErrCastInvalidProposal
		}
	} else if spec.healKind == castHealFlag {
		if p.DailyUsed || len(p.EffectRolls) != 0 || p.HPDelta != 0 || p.TimedFlag != 0 || p.TimedInterval != 0 || p.CursedItemsCleared != 0 || p.afterItemsSet || spec.Flag >= uint(len(actor.Body.Flags)*8) || spec.Timer != -1 || spec.ClearCursedReady {
			return State{}, CastResult{}, ErrCastInvalidProposal
		}
	} else if spec.healKind == castHealFull {
		if p.TimedFlag != 0 || p.TimedInterval != 0 {
			return State{}, CastResult{}, ErrCastInvalidProposal
		}
		_, allowed, err := castDailyUse(actor.Body.Daily[castDailyHealIndex], p.Now)
		if err != nil || (actor.Body.Class < 10 && !allowed) || allowed != p.DailyUsed {
			return State{}, CastResult{}, ErrCastInvalidProposal
		}
	} else if p.DailyUsed || len(p.EffectRolls) == 0 && spec.healKind != castHealFull {
		// Vigor/mend always have at least one effect draw.  Full heal was
		// handled above and has no effect draws. Timed spells were handled in
		// the preceding branch and also have no effect draws.
		if p.DailyUsed {
			return State{}, CastResult{}, ErrCastInvalidProposal
		}
	}
	after, hpDelta, err := castBodyAfter(actor.Body, room, spec, CastOptions{}, p.EffectRolls, p.DailyUsed || spec.healKind != castHealFull, false)
	if err != nil || hpDelta != p.HPDelta {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}
	after.Timers[castSpellTimerIndex] = LegacyTimer{LastTime: p.Now, Interval: p.SpellInterval}
	if spec.healKind == castHealTimed {
		if err := castApplyTimedEffect(&after, actor.Items, spec, p.Now, p.TimedInterval); err != nil {
			return State{}, CastResult{}, ErrCastInvalidProposal
		}
	} else if spec.healKind == castHealCleanse {
		setSettingFlag(&after, spec.Flag, false)
	}
	if spec.healKind == castHealFull {
		daily, allowed, err := castDailyUse(actor.Body.Daily[castDailyHealIndex], p.Now)
		if err != nil || (actor.Body.Class < 10 && !allowed) || allowed != p.DailyUsed {
			return State{}, CastResult{}, ErrCastInvalidProposal
		}
		after.Daily[castDailyHealIndex] = daily
	}
	var afterItems *ItemCollection
	if spec.ClearCursedReady {
		projected, cleared, err := castClearCursedReady(actor.Items)
		if err != nil || !p.afterItemsSet || cleared != p.CursedItemsCleared || !reflect.DeepEqual(projected, p.afterItems) {
			return State{}, CastResult{}, ErrCastInvalidProposal
		}
		items := projected.clone()
		afterItems = &items
	}
	if int32(after.MPCurrent)-int32(actor.Body.MPCurrent) != p.MPDelta || !reflect.DeepEqual(after, p.afterBody) {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}
	next := s.clone()
	nextActor := next.Players[p.ActorID]
	nextActor.Body = after
	if afterItems != nil {
		nextActor.Items = afterItems
	}
	next.Players[p.ActorID] = nextActor
	if err := next.Validate(); err != nil {
		return State{}, CastResult{}, err
	}
	return next, castResult(p, nextActor), nil
}

// Cast is the one-call reducer-shaped API for direct self-target spells.
func (s State) Cast(actorID, spellName string, options CastOptions) (State, CastResult, error) {
	p, err := s.PlanCast(actorID, spellName, options)
	if err != nil {
		return State{}, CastResult{}, err
	}
	return s.ApplyCast(p)
}
