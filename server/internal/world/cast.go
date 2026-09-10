package world

// This file is the first player-facing slice of magic1.c:cast.  It admits
// only the three self-target healing spells whose state transition is already
// represented by the canonical player body.  Other spells remain visible in
// the spell catalog but fail closed at this boundary until their target,
// combat, room or item contracts are migrated.

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

	castVigorSpell = 0  // SVIGOR / 회복
	castMendSpell  = 18 // SMENDW / 원기회복
	castHealSpell  = 19 // SFHEAL / 완치
)

const (
	castHealNone = iota
	castHealVigor
	castHealMend
	castHealFull
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
	// A zero mask means spell_fail is not needed for this spell.  Otherwise
	// only the listed class bits call spell_fail, matching magic2.c/magic5.c.
	failMask uint16
	// The source heal spell has no spell_fail call and is restricted to
	// cleric/paladin/invincible classes.
	classGate int
}

const (
	castAnyClass = iota
	castClericPaladinInvincible
)

func castSpellSpecFor(name string) (castSpellSpec, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return castSpellSpec{}, ErrCastSpellUnavailable
	}
	// Keep the source prefix/unique-match behavior, but resolve against the
	// complete catalog first so a prefix that names an unported spell does not
	// accidentally select one of the three admitted effects.
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
)

type CastOptions struct {
	Now  int32
	Roll func(int, int) int
}

type CastEvent struct {
	RoomID         int16  `json:"room_id"`
	ActorID        string `json:"actor_id"`
	ActorName      string `json:"actor_name"`
	SpellName      string `json:"spell_name"`
	ExcludeActorID string `json:"exclude_actor_id"`
	Text           string `json:"text"`
}

type CastResult struct {
	Action        string     `json:"action"`
	Response      string     `json:"response"`
	Broadcast     bool       `json:"broadcast"`
	Changed       bool       `json:"changed"`
	Attempted     bool       `json:"attempted,omitempty"`
	Succeeded     bool       `json:"succeeded,omitempty"`
	SpellFailed   bool       `json:"spell_failed,omitempty"`
	DailyUsed     bool       `json:"daily_used,omitempty"`
	HiddenCleared bool       `json:"hidden_cleared,omitempty"`
	SpellName     string     `json:"spell_name,omitempty"`
	SpellIndex    int        `json:"spell_index,omitempty"`
	Cost          int16      `json:"cost,omitempty"`
	Chance        int        `json:"chance,omitempty"`
	Roll          int        `json:"roll,omitempty"`
	EffectRolls   []int      `json:"effect_rolls,omitempty"`
	HPDelta       int32      `json:"hp_delta,omitempty"`
	MPDelta       int32      `json:"mp_delta,omitempty"`
	Now           int32      `json:"now,omitempty"`
	SpellInterval int32      `json:"spell_interval,omitempty"`
	Event         *CastEvent `json:"event,omitempty"`
}

// CastProposal is the snapshot-bound receipt candidate.  ApplyCast never
// calls Roll: all spell-fail/effect draws are copied into EffectRolls during
// planning and validated from those values during apply/replay.
type CastProposal struct {
	Action        string
	ActorID       string
	RoomID        int16
	SpellName     string
	SpellIndex    int
	Cost          int16
	HealKind      int
	Now           int32
	Chance        int
	Roll          int
	EffectRolls   []int
	HPDelta       int32
	MPDelta       int32
	SpellInterval int32
	Attempted     bool
	Succeeded     bool
	SpellFailed   bool
	DailyUsed     bool
	HiddenCleared bool
	Changed       bool
	Broadcast     bool
	Response      string
	RoomText      string

	expectedActor     PlayerState
	expectedRoomFlags [8]byte
	afterBody         LegacyMonster
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
	if spec.classGate == castAnyClass {
		return true
	}
	return body.Class == castClericClass || body.Class == castPaladinClass || body.Class >= castInvincible
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
		return after, int32(after.MPCurrent) - int32(body.MPCurrent), nil
	}
	if body.MPCurrent < spec.Cost {
		return LegacyMonster{}, 0, fmt.Errorf("cast success has insufficient mana")
	}
	after.MPCurrent -= spec.Cost
	setSettingFlag(&after, castHiddenFlag, false)
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
	default:
		return ""
	}
}

// PlanCast evaluates `주문 <spell>` with no target, matching cast()'s
// self-target branch. Prompt, gate and cooldown responses are represented as
// no-op receipts so the terminal sees deterministic output without mutating
// canonical state.
func (s State) PlanCast(actorID, spellName string, options CastOptions) (CastProposal, error) {
	if options.Now < 0 {
		return CastProposal{}, fmt.Errorf("cast clock must be nonnegative")
	}
	actor, room, err := castActor(s, actorID)
	if err != nil {
		return CastProposal{}, err
	}
	p := CastProposal{Action: "cast", ActorID: actorID, RoomID: actor.Body.RoomID, Now: options.Now, expectedActor: cloneDrinkActor(actor), expectedRoomFlags: room.Resource.Flags}
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
	p.Attempted, p.Chance, p.Roll = true, chance, roll
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
	p.afterBody, p.HPDelta, err = castBodyAfter(actor.Body, room, spec, options, p.EffectRolls, dailyUsed, false)
	if err != nil {
		return CastProposal{}, err
	}
	p.afterBody.Timers[castSpellTimerIndex] = LegacyTimer{LastTime: options.Now, Interval: p.SpellInterval}
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
	result := CastResult{Action: p.Action, Response: p.Response, Broadcast: p.Broadcast, Changed: p.Changed, Attempted: p.Attempted, Succeeded: p.Succeeded, SpellFailed: p.SpellFailed, DailyUsed: p.DailyUsed, HiddenCleared: p.HiddenCleared, SpellName: p.SpellName, SpellIndex: p.SpellIndex, Cost: p.Cost, Chance: p.Chance, Roll: p.Roll, EffectRolls: append([]int(nil), p.EffectRolls...), HPDelta: p.HPDelta, MPDelta: p.MPDelta, Now: p.Now, SpellInterval: p.SpellInterval}
	if p.Broadcast {
		result.Event = &CastEvent{RoomID: p.RoomID, ActorID: p.ActorID, ActorName: actor.Body.Name, SpellName: p.SpellName, ExcludeActorID: p.ActorID, Text: p.RoomText}
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
		if p.Changed || p.HiddenCleared || p.Broadcast || p.Attempted || p.Succeeded || p.SpellFailed || p.SpellIndex != 0 || p.Cost != 0 || p.RoomText != "" {
			return State{}, CastResult{}, ErrCastInvalidProposal
		}
		return s.clone(), castResult(p, actor), nil
	}
	spec, err := castSpellSpecFor(p.SpellName)
	if err != nil || spec.Name != p.SpellName || spec.Index != p.SpellIndex || spec.Cost != p.Cost || spec.healKind != p.HealKind {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}
	if !p.Attempted {
		if p.Broadcast || p.Succeeded || p.SpellFailed || p.EffectRolls != nil || p.HPDelta != 0 || p.MPDelta != 0 || p.SpellInterval != 0 {
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
	if !p.Changed || p.Broadcast != p.Succeeded || p.Succeeded == p.SpellFailed || p.Chance < 1 || p.Chance > math.MaxInt32 || p.Roll < 0 || p.Roll > 100 {
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
		if len(p.EffectRolls) != 0 || p.DailyUsed || p.SpellInterval != 0 || p.Broadcast || p.RoomText != "" || p.HPDelta != 0 {
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
	if spec.healKind == castHealFull {
		_, allowed, err := castDailyUse(actor.Body.Daily[castDailyHealIndex], p.Now)
		if err != nil || (actor.Body.Class < 10 && !allowed) || allowed != p.DailyUsed {
			return State{}, CastResult{}, ErrCastInvalidProposal
		}
	} else if p.DailyUsed || len(p.EffectRolls) == 0 && spec.healKind != castHealFull {
		// Vigor/mend always have at least one effect draw.  Full heal was
		// handled above and has no effect draws.
		if p.DailyUsed {
			return State{}, CastResult{}, ErrCastInvalidProposal
		}
	}
	after, hpDelta, err := castBodyAfter(actor.Body, room, spec, CastOptions{}, p.EffectRolls, p.DailyUsed || spec.healKind != castHealFull, false)
	if err != nil || hpDelta != p.HPDelta {
		return State{}, CastResult{}, ErrCastInvalidProposal
	}
	after.Timers[castSpellTimerIndex] = LegacyTimer{LastTime: p.Now, Interval: p.SpellInterval}
	if spec.healKind == castHealFull {
		daily, allowed, err := castDailyUse(actor.Body.Daily[castDailyHealIndex], p.Now)
		if err != nil || (actor.Body.Class < 10 && !allowed) || allowed != p.DailyUsed {
			return State{}, CastResult{}, ErrCastInvalidProposal
		}
		after.Daily[castDailyHealIndex] = daily
	}
	if int32(after.MPCurrent)-int32(actor.Body.MPCurrent) != p.MPDelta || !reflect.DeepEqual(after, p.afterBody) {
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

// Cast is the one-call reducer-shaped API for direct self-target spells.
func (s State) Cast(actorID, spellName string, options CastOptions) (State, CastResult, error) {
	p, err := s.PlanCast(actorID, spellName, options)
	if err != nil {
		return State{}, CastResult{}, err
	}
	return s.ApplyCast(p)
}
