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

// buy_states is the bounded Go port of command11.c:buy_states (cmdlist-149
// `향상`). The command keeps the C first-gate order, while its random apply
// path is admitted only when a server-owned roll source is supplied.
const (
	buyStatesCaretakerClass      = 10 // CARETAKER; class ==, not >=
	buyStatesExperienceFloor     = int64(100000000)
	buyStatesGoldUnit            = int64(1000000)
	buyStatesBasicExperienceCost = int64(2000000)
	buyStatesBasicGoldCost       = int64(1000000)
	buyStatesVitalityMax         = int64(2000)
	buyStatesVitalityCapLevel    = byte(128)
	buyStatesPowerDamageFlag     = 59 // PUPDMG

	BuyStatesCaretakerClass      = buyStatesCaretakerClass
	BuyStatesExperienceFloor     = buyStatesExperienceFloor
	BuyStatesGoldUnit            = buyStatesGoldUnit
	BuyStatesBasicExperienceCost = buyStatesBasicExperienceCost
	BuyStatesBasicGoldCost       = buyStatesBasicGoldCost
	BuyStatesVitalityMax         = buyStatesVitalityMax
	BuyStatesVitalityCapLevel    = buyStatesVitalityCapLevel
)

const (
	BuyStatesNotCaretakerResponse = "초인만이 가능합니다."
	BuyStatesPromptResponse       = "\"체력\" 과 \"도력\" 중 어느 것을 올리시려고요?"
	BuyStatesExperienceResponse   = "당신의 경험치로는 능력치 향상을 할 수 없습니다."
	BuyStatesGoldResponse         = "당신이 가진 돈으로는 능력치 향상을 할 수 없습니다."
	BuyStatesUnknownStatResponse  = "어떤 능력치를 올리시려고요?"
	BuyStatesHealthCapResponse    = "더이상은 체력을 올릴수 없습니다."
	BuyStatesManaCapResponse      = "더이상은 도력을 올릴수 없습니다."
	BuyStatesStatCapResponse      = "더이상 올릴수 없는 능력치입니다."
	BuyStatesSuccessResponse      = "\n축하합니다! 당신의 능력치가 올랐습니다!"
)

type BuyStatesAction string

const (
	BuyStatesNotCaretaker BuyStatesAction = "not_caretaker"
	BuyStatesPrompt       BuyStatesAction = "prompt"
	BuyStatesExperience   BuyStatesAction = "experience"
	BuyStatesGold         BuyStatesAction = "gold"
	BuyStatesUnknownStat  BuyStatesAction = "unknown_stat"
	BuyStatesApplied      BuyStatesAction = "applied"
	BuyStatesVitalityCap  BuyStatesAction = "vitality_cap"
	BuyStatesStatCap      BuyStatesAction = "stat_cap"

	// Descriptive aliases keep callers from having to infer which cap branch
	// was selected from the response string alone.
	BuyStatesApply     BuyStatesAction = BuyStatesApplied
	BuyStatesHealthCap BuyStatesAction = BuyStatesVitalityCap
	BuyStatesManaCap   BuyStatesAction = BuyStatesVitalityCap
	BuyStatesBasicCap  BuyStatesAction = BuyStatesStatCap
)

var (
	ErrBuyStatesActorAbsent     = errors.New("online canonical buy-states actor absent")
	ErrBuyStatesGoldUnresolved  = errors.New("canonical gold required")
	ErrBuyStatesStatsUnresolved = errors.New("canonical stats required")
	ErrBuyStatesApplyPending    = errors.New("buy_states apply is not admitted")
	ErrBuyStatesRandom          = errors.New("buy_states random source unavailable")
	ErrBuyStatesNumeric         = errors.New("buy_states numeric state outside canonical range")
	ErrBuyStatesInvalidStat     = errors.New("invalid buy-states stat selector")
	ErrBuyStatesStaleProposal   = errors.New("stale or invalid buy-states proposal")
	ErrBuyStatesInvalidProposal = errors.New("invalid buy-states proposal")
	ErrBuyStatesNameInvalid     = errors.New("buy-states actor name is unsafe")

	// ErrBuyStatesOverflow is a descriptive spelling for callers that classify
	// malformed numeric snapshots separately from other reducer errors.
	ErrBuyStatesOverflow = ErrBuyStatesNumeric
)

// BuyStatesOptions supplies only server-owned inputs. A nil Roll intentionally
// keeps the old fail-closed first-gate behavior for random apply branches.
type BuyStatesOptions struct {
	Roll func(int, int) int
}

// BuyStatesResult is the durable actor response and the complete result
// evidence for a successful apply. Rolls are retained so a receipt is enough
// to explain and replay the transition without consulting a random source.
type BuyStatesResult struct {
	Action   BuyStatesAction `json:"action"`
	ActorID  string          `json:"actor_id"`
	Stat     string          `json:"stat,omitempty"`
	Response string          `json:"response"`
	Changed  bool            `json:"changed"`
	Amount   int64           `json:"amount,omitempty"`
	Cost     int64           `json:"cost,omitempty"`
	Gain     int64           `json:"gain,omitempty"`

	Rolls       []int `json:"rolls,omitempty"`
	GrowthRolls []int `json:"growth_rolls,omitempty"`
	StatRoll    int   `json:"stat_roll,omitempty"`

	ExperienceBefore int32   `json:"experience_before,omitempty"`
	ExperienceAfter  int32   `json:"experience_after,omitempty"`
	GoldBefore       int32   `json:"gold_before,omitempty"`
	GoldAfter        int32   `json:"gold_after,omitempty"`
	HPMaxBefore      int16   `json:"hp_max_before,omitempty"`
	HPMaxAfter       int16   `json:"hp_max_after,omitempty"`
	HPCurrentBefore  int16   `json:"hp_current_before,omitempty"`
	HPCurrentAfter   int16   `json:"hp_current_after,omitempty"`
	MPMaxBefore      int16   `json:"mp_max_before,omitempty"`
	MPMaxAfter       int16   `json:"mp_max_after,omitempty"`
	MPCurrentBefore  int16   `json:"mp_current_before,omitempty"`
	MPCurrentAfter   int16   `json:"mp_current_after,omitempty"`
	StatsBefore      [5]byte `json:"stats_before,omitempty"`
	StatsAfter       [5]byte `json:"stats_after,omitempty"`
	DiceCountBefore  int16   `json:"dice_count_before,omitempty"`
	DiceCountAfter   int16   `json:"dice_count_after,omitempty"`
	DiceSidesBefore  int16   `json:"dice_sides_before,omitempty"`
	DiceSidesAfter   int16   `json:"dice_sides_after,omitempty"`
	DicePlusBefore   int16   `json:"dice_plus_before,omitempty"`
	DicePlusAfter    int16   `json:"dice_plus_after,omitempty"`
}

// BuyStatesProposal is a snapshot-bound candidate. expectedActor is private
// so callers cannot manufacture authorization or ownership from a terminal
// payload; the public fields are checked against it during Apply.
type BuyStatesProposal struct {
	Action   BuyStatesAction
	ActorID  string
	Stat     string
	Response string
	Changed  bool
	Amount   int64
	Cost     int64
	Gain     int64

	Rolls       []int
	GrowthRolls []int
	StatRoll    int

	ExperienceBefore int32
	ExperienceAfter  int32
	GoldBefore       int32
	GoldAfter        int32
	HPMaxBefore      int16
	HPMaxAfter       int16
	HPCurrentBefore  int16
	HPCurrentAfter   int16
	MPMaxBefore      int16
	MPMaxAfter       int16
	MPCurrentBefore  int16
	MPCurrentAfter   int16
	StatsBefore      [5]byte
	StatsAfter       [5]byte
	DiceCountBefore  int16
	DiceCountAfter   int16
	DiceSidesBefore  int16
	DiceSidesAfter   int16
	DicePlusBefore   int16
	DicePlusAfter    int16

	expectedActor PlayerState
}

type buyStatesAdmission struct {
	actor          PlayerState
	action         BuyStatesAction
	actorID        string
	stat           string
	response       string
	amount         int64
	cost           int64 // actual gold cost for this outcome
	experienceCost int64
	goldCost       int64
	apply          bool
}

func buyStatesApplyPending(stat string) bool {
	switch stat {
	case "체력", "도력", "힘", "민첩", "맷집", "지식", "신앙심":
		return true
	default:
		return false
	}
}

func buyStatesStatIndex(stat string) (int, bool) {
	switch stat {
	case "힘":
		return 0, true
	case "민첩":
		return 1, true
	case "맷집":
		return 2, true
	case "지식":
		return 3, true
	case "신앙심":
		return 4, true
	default:
		return 0, false
	}
}

func validBuyStatesActorName(name string) bool {
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

func validBuyStatesStat(stat string) error {
	if stat == "" {
		return nil
	}
	if !utf8.ValidString(stat) || strings.TrimSpace(stat) != stat {
		return ErrBuyStatesInvalidStat
	}
	for _, r := range stat {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' || unicode.IsSpace(r) {
			return ErrBuyStatesInvalidStat
		}
	}
	return nil
}

func buyStatesActor(s State, actorID string) (PlayerState, error) {
	if err := s.Validate(); err != nil {
		return PlayerState{}, err
	}
	actor, ok := s.Players[actorID]
	if actorID == "" || !ok || !actor.Online || actor.Body.Type != 0 || actor.Body.Name == "" {
		return PlayerState{}, ErrBuyStatesActorAbsent
	}
	if !validBuyStatesActorName(actor.Body.Name) {
		return PlayerState{}, ErrBuyStatesNameInvalid
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok || room.Resource.ID != actor.Body.RoomID || !containsString(room.PlayerIDs, actorID) {
		return PlayerState{}, ErrBuyStatesActorAbsent
	}
	return actor, nil
}

func buyStatesCanonicalGoldAndStats(actor PlayerState) error {
	// Legacy nested objects are not a canonical gold/stat source. An explicit
	// canonical collection is validated when present; an empty legacy inventory
	// remains compatible with the existing first-gate fixtures.
	if len(actor.Body.Inventory) != 0 {
		return errors.Join(ErrBuyStatesGoldUnresolved, ErrBuyStatesStatsUnresolved)
	}
	if actor.Items != nil {
		if err := actor.Items.Validate(); err != nil {
			return errors.Join(ErrBuyStatesStatsUnresolved, err)
		}
	}
	return nil
}

func buyStatesAmount(experience int32) int64 {
	return (int64(experience) - buyStatesExperienceFloor) / buyStatesGoldUnit
}

func buyStatesGoldCost(amt int64) (int64, error) {
	if amt < 1 {
		return 0, nil
	}
	if amt > math.MaxInt64/buyStatesGoldUnit {
		return 0, fmt.Errorf("%w: gold cost overflow", ErrBuyStatesNumeric)
	}
	return amt * buyStatesGoldUnit, nil
}

func buyStatesCheckedAdd(a, b int64) (int64, error) {
	if b > 0 && a > math.MaxInt64-b {
		return 0, fmt.Errorf("%w: addition overflow", ErrBuyStatesNumeric)
	}
	if b < 0 && a < math.MinInt64-b {
		return 0, fmt.Errorf("%w: addition underflow", ErrBuyStatesNumeric)
	}
	return a + b, nil
}

func buyStatesCheckedSub(a, b int64) (int64, error) {
	if b > 0 && a < math.MinInt64+b {
		return 0, fmt.Errorf("%w: subtraction underflow", ErrBuyStatesNumeric)
	}
	if b < 0 && a > math.MaxInt64+b {
		return 0, fmt.Errorf("%w: subtraction overflow", ErrBuyStatesNumeric)
	}
	return a - b, nil
}

func buyStatesInt16(value int64) (int16, error) {
	if value < math.MinInt16 || value > math.MaxInt16 {
		return 0, fmt.Errorf("%w: int16 result %d", ErrBuyStatesNumeric, value)
	}
	return int16(value), nil
}

func buyStatesInt32(value int64) (int32, error) {
	if value < math.MinInt32 || value > math.MaxInt32 {
		return 0, fmt.Errorf("%w: int32 result %d", ErrBuyStatesNumeric, value)
	}
	return int32(value), nil
}

func buyStatesResourceAfter(body LegacyMonster, experienceCost, goldCost int64) (int32, int32, error) {
	if body.Experience < 0 || body.Gold < 0 || experienceCost < 0 || goldCost < 0 {
		return 0, 0, fmt.Errorf("%w: negative resource", ErrBuyStatesNumeric)
	}
	experience, err := buyStatesCheckedSub(int64(body.Experience), experienceCost)
	if err != nil {
		return 0, 0, err
	}
	gold, err := buyStatesCheckedSub(int64(body.Gold), goldCost)
	if err != nil {
		return 0, 0, err
	}
	experienceAfter, err := buyStatesInt32(experience)
	if err != nil {
		return 0, 0, err
	}
	goldAfter, err := buyStatesInt32(gold)
	if err != nil {
		return 0, 0, err
	}
	return experienceAfter, goldAfter, nil
}

func buyStatesNumericBody(body LegacyMonster) error {
	if body.Experience < 0 || body.Gold < 0 || body.HPMax < 0 || body.HPCurrent < 0 || body.HPCurrent > body.HPMax || body.MPMax < 0 || body.MPCurrent < 0 || body.MPCurrent > body.MPMax {
		return fmt.Errorf("%w: invalid player vitals or resources", ErrBuyStatesNumeric)
	}
	return nil
}

func buyStatesUnchanged(action BuyStatesAction, actorID, stat, response string, amount, cost int64, actor PlayerState) BuyStatesProposal {
	return BuyStatesProposal{
		Action: action, ActorID: actorID, Stat: stat, Response: response,
		Amount: amount, Cost: cost, expectedActor: actor,
	}
}

func buyStatesAdmissionFor(s State, actorID, stat string) (buyStatesAdmission, error) {
	actor, err := buyStatesActor(s, actorID)
	if err != nil {
		return buyStatesAdmission{}, err
	}
	if err := validBuyStatesStat(stat); err != nil {
		return buyStatesAdmission{}, err
	}
	a := buyStatesAdmission{actor: actor, actorID: actorID, stat: stat}
	if actor.Body.Class != buyStatesCaretakerClass {
		a.action, a.response = BuyStatesNotCaretaker, BuyStatesNotCaretakerResponse
		return a, nil
	}
	if stat == "" {
		a.action, a.response = BuyStatesPrompt, BuyStatesPromptResponse
		return a, nil
	}
	if err := buyStatesCanonicalGoldAndStats(actor); err != nil {
		return buyStatesAdmission{}, err
	}
	a.amount = buyStatesAmount(actor.Body.Experience)
	if a.amount < 1 {
		a.action, a.response = BuyStatesExperience, BuyStatesExperienceResponse
		return a, nil
	}
	gateCost, err := buyStatesGoldCost(a.amount)
	if err != nil {
		return buyStatesAdmission{}, err
	}
	if int64(actor.Body.Gold) < gateCost {
		a.action, a.response, a.cost = BuyStatesGold, BuyStatesGoldResponse, gateCost
		return a, nil
	}
	if !buyStatesApplyPending(stat) {
		a.action, a.response, a.cost = BuyStatesUnknownStat, BuyStatesUnknownStatResponse, gateCost
		return a, nil
	}
	if stat == "체력" {
		if int64(actor.Body.HPMax) >= buyStatesVitalityMax && actor.Body.Level < buyStatesVitalityCapLevel {
			a.action, a.response = BuyStatesVitalityCap, BuyStatesHealthCapResponse
			return a, nil
		}
	} else if stat == "도력" {
		if int64(actor.Body.MPMax) >= buyStatesVitalityMax && actor.Body.Level < buyStatesVitalityCapLevel {
			a.action, a.response = BuyStatesVitalityCap, BuyStatesManaCapResponse
			return a, nil
		}
	} else if index, ok := buyStatesStatIndex(stat); ok {
		// command11.c checks the basic-stat experience requirement after the
		// overall amount/gold gate and before the stat cap.
		if int64(actor.Body.Experience)-buyStatesExperienceFloor < buyStatesBasicExperienceCost {
			a.action, a.response = BuyStatesExperience, BuyStatesExperienceResponse
			return a, nil
		}
		if actor.Body.Stats[index] > 44 {
			a.action, a.response = BuyStatesStatCap, BuyStatesStatCapResponse
			return a, nil
		}
	}
	a.apply = true
	a.action = BuyStatesApplied
	a.response = BuyStatesSuccessResponse
	a.cost = gateCost
	a.experienceCost = gateCost
	a.goldCost = gateCost
	if _, ok := buyStatesStatIndex(stat); ok {
		a.experienceCost = buyStatesBasicExperienceCost
		a.goldCost = buyStatesBasicGoldCost
		a.cost = buyStatesBasicGoldCost
	}
	return a, nil
}

func buyStatesRoll(roll func(int, int) int, low, high int) (value int, err error) {
	if roll == nil || low > high {
		return 0, ErrBuyStatesRandom
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			value = 0
			err = fmt.Errorf("%w: random source panicked: %v", ErrBuyStatesRandom, recovered)
		}
	}()
	value = roll(low, high)
	if value < low || value > high {
		return 0, fmt.Errorf("%w: value %d outside %d..%d", ErrBuyStatesRandom, value, low, high)
	}
	return value, nil
}

func buyStatesProposalFromAdmission(a buyStatesAdmission) BuyStatesProposal {
	return buyStatesUnchanged(a.action, a.actorID, a.stat, a.response, a.amount, a.cost, a.actor)
}

func buyStatesBuildApplied(a buyStatesAdmission, rolls []int) (BuyStatesProposal, error) {
	if !a.apply || a.action != BuyStatesApplied || len(rolls) == 0 {
		return BuyStatesProposal{}, ErrBuyStatesInvalidProposal
	}
	if err := buyStatesNumericBody(a.actor.Body); err != nil {
		return BuyStatesProposal{}, err
	}
	experienceAfter, goldAfter, err := buyStatesResourceAfter(a.actor.Body, a.experienceCost, a.goldCost)
	if err != nil {
		return BuyStatesProposal{}, err
	}
	p := BuyStatesProposal{
		Action: a.action, ActorID: a.actorID, Stat: a.stat, Response: a.response,
		Changed: true, Amount: a.amount, Cost: a.cost,
		ExperienceBefore: a.actor.Body.Experience, ExperienceAfter: experienceAfter,
		GoldBefore: a.actor.Body.Gold, GoldAfter: goldAfter,
		HPMaxBefore: a.actor.Body.HPMax, HPCurrentBefore: a.actor.Body.HPCurrent,
		HPMaxAfter: a.actor.Body.HPMax, HPCurrentAfter: a.actor.Body.HPCurrent,
		MPMaxBefore: a.actor.Body.MPMax, MPCurrentBefore: a.actor.Body.MPCurrent,
		MPMaxAfter: a.actor.Body.MPMax, MPCurrentAfter: a.actor.Body.MPCurrent,
		StatsBefore: a.actor.Body.Stats, StatsAfter: a.actor.Body.Stats,
		DiceCountBefore: a.actor.Body.DiceCount, DiceCountAfter: 4,
		DiceSidesBefore: a.actor.Body.DiceSides, DiceSidesAfter: 4,
		DicePlusBefore: a.actor.Body.DicePlus, DicePlusAfter: a.actor.Body.DicePlus,
		Rolls: append([]int(nil), rolls...), expectedActor: a.actor,
	}
	if !flag(a.actor.Body.Flags[:], buyStatesPowerDamageFlag) {
		p.DicePlusAfter = 4
	}
	if a.stat == "체력" || a.stat == "도력" {
		if len(rolls) != int(a.amount) {
			return BuyStatesProposal{}, ErrBuyStatesInvalidProposal
		}
		p.GrowthRolls = append([]int(nil), rolls...)
		growth, err := buyStatesCheckedAdd(a.amount, a.amount)
		if err != nil {
			return BuyStatesProposal{}, err
		}
		for _, roll := range rolls {
			if roll < 0 || roll > 3 {
				return BuyStatesProposal{}, fmt.Errorf("%w: growth roll outside 0..3", ErrBuyStatesRandom)
			}
			growth, err = buyStatesCheckedAdd(growth, int64(roll))
			if err != nil {
				return BuyStatesProposal{}, err
			}
			growth, err = buyStatesCheckedAdd(growth, 1)
			if err != nil {
				return BuyStatesProposal{}, err
			}
		}
		growth, err = buyStatesCheckedSub(growth, 1)
		if err != nil {
			return BuyStatesProposal{}, err
		}
		p.Gain = growth
		if a.stat == "체력" {
			maxAfter, addErr := buyStatesCheckedAdd(int64(a.actor.Body.HPMax), growth)
			if addErr != nil {
				return BuyStatesProposal{}, addErr
			}
			p.HPMaxAfter, err = buyStatesInt16(maxAfter)
			if err != nil {
				return BuyStatesProposal{}, err
			}
			p.HPCurrentAfter = p.HPMaxAfter
		} else {
			maxAfter, addErr := buyStatesCheckedAdd(int64(a.actor.Body.MPMax), growth)
			if addErr != nil {
				return BuyStatesProposal{}, addErr
			}
			p.MPMaxAfter, err = buyStatesInt16(maxAfter)
			if err != nil {
				return BuyStatesProposal{}, err
			}
			p.MPCurrentAfter = p.MPMaxAfter
		}
		return p, nil
	}

	index, ok := buyStatesStatIndex(a.stat)
	if !ok || len(rolls) != 1 || rolls[0] < 0 || rolls[0] > 1 {
		return BuyStatesProposal{}, fmt.Errorf("%w: basic-stat roll outside 0..1", ErrBuyStatesRandom)
	}
	p.StatRoll = rolls[0]
	statAfter, addErr := buyStatesCheckedAdd(int64(p.StatsAfter[index]), 1)
	if addErr != nil || statAfter > math.MaxUint8 {
		if addErr != nil {
			return BuyStatesProposal{}, addErr
		}
		return BuyStatesProposal{}, fmt.Errorf("%w: stat result %d", ErrBuyStatesNumeric, statAfter)
	}
	p.StatsAfter[index] = byte(statAfter)
	if p.StatRoll != 0 {
		p.Gain = 4
		maxAfter, addErr := buyStatesCheckedAdd(int64(a.actor.Body.HPMax), 4)
		if addErr != nil {
			return BuyStatesProposal{}, addErr
		}
		p.HPMaxAfter, err = buyStatesInt16(maxAfter)
	} else {
		p.Gain = 3
		maxAfter, addErr := buyStatesCheckedAdd(int64(a.actor.Body.MPMax), 3)
		if addErr != nil {
			return BuyStatesProposal{}, addErr
		}
		p.MPMaxAfter, err = buyStatesInt16(maxAfter)
	}
	if err != nil {
		return BuyStatesProposal{}, err
	}
	return p, nil
}

func buyStatesPlanApplied(a buyStatesAdmission, roll func(int, int) int) (BuyStatesProposal, error) {
	if roll == nil {
		// Keep the pre-existing public PlanBuyStates contract fail-closed. The
		// deterministic cap/no-op branches never reach this helper.
		return BuyStatesProposal{}, ErrBuyStatesApplyPending
	}
	count := 1
	if a.stat == "체력" || a.stat == "도력" {
		count = int(a.amount)
	}
	rolls := make([]int, 0, count)
	for i := 0; i < count; i++ {
		low, high := 0, 1
		if a.stat == "체력" || a.stat == "도력" {
			high = 3
		}
		value, err := buyStatesRoll(roll, low, high)
		if err != nil {
			return BuyStatesProposal{}, err
		}
		rolls = append(rolls, value)
	}
	return buyStatesBuildApplied(a, rolls)
}

func buyStatesProposalMatches(actual, expected BuyStatesProposal) bool {
	return actual.Action == expected.Action && actual.ActorID == expected.ActorID &&
		actual.Stat == expected.Stat && actual.Response == expected.Response &&
		actual.Changed == expected.Changed && actual.Amount == expected.Amount &&
		actual.Cost == expected.Cost && actual.Gain == expected.Gain &&
		reflect.DeepEqual(actual.Rolls, expected.Rolls) &&
		reflect.DeepEqual(actual.GrowthRolls, expected.GrowthRolls) &&
		actual.StatRoll == expected.StatRoll &&
		actual.ExperienceBefore == expected.ExperienceBefore && actual.ExperienceAfter == expected.ExperienceAfter &&
		actual.GoldBefore == expected.GoldBefore && actual.GoldAfter == expected.GoldAfter &&
		actual.HPMaxBefore == expected.HPMaxBefore && actual.HPMaxAfter == expected.HPMaxAfter &&
		actual.HPCurrentBefore == expected.HPCurrentBefore && actual.HPCurrentAfter == expected.HPCurrentAfter &&
		actual.MPMaxBefore == expected.MPMaxBefore && actual.MPMaxAfter == expected.MPMaxAfter &&
		actual.MPCurrentBefore == expected.MPCurrentBefore && actual.MPCurrentAfter == expected.MPCurrentAfter &&
		actual.StatsBefore == expected.StatsBefore && actual.StatsAfter == expected.StatsAfter &&
		actual.DiceCountBefore == expected.DiceCountBefore && actual.DiceCountAfter == expected.DiceCountAfter &&
		actual.DiceSidesBefore == expected.DiceSidesBefore && actual.DiceSidesAfter == expected.DiceSidesAfter &&
		actual.DicePlusBefore == expected.DicePlusBefore && actual.DicePlusAfter == expected.DicePlusAfter &&
		reflect.DeepEqual(actual.expectedActor, expected.expectedActor)
}

// PlanBuyStates is command11.c:buy_states admission with no random source.
// Existing first-gate and prompt behavior remains unchanged; a random apply
// branch returns ErrBuyStatesApplyPending rather than inventing a roll.
func (s State) PlanBuyStates(actorID, stat string) (BuyStatesProposal, error) {
	return s.PlanBuyStatesWithOptions(actorID, stat, BuyStatesOptions{})
}

// PlanBuyStatesWithOptions admits the seven source apply branches only with a
// server-owned Roll. Every random draw is copied into the proposal before
// Apply, which makes the apply step pure and replayable.
func (s State) PlanBuyStatesWithOptions(actorID, stat string, options BuyStatesOptions) (BuyStatesProposal, error) {
	a, err := buyStatesAdmissionFor(s, actorID, stat)
	if err != nil {
		return BuyStatesProposal{}, err
	}
	if !a.apply {
		return buyStatesProposalFromAdmission(a), nil
	}
	return buyStatesPlanApplied(a, options.Roll)
}

// PlanBuyStatesWithRoll is a concise source-named alias for callers that only
// need the host random stream.
func (s State) PlanBuyStatesWithRoll(actorID, stat string, roll func(int, int) int) (BuyStatesProposal, error) {
	return s.PlanBuyStatesWithOptions(actorID, stat, BuyStatesOptions{Roll: roll})
}

func buyStatesResult(p BuyStatesProposal) BuyStatesResult {
	return BuyStatesResult{
		Action: p.Action, ActorID: p.ActorID, Stat: p.Stat, Response: p.Response,
		Changed: p.Changed, Amount: p.Amount, Cost: p.Cost, Gain: p.Gain,
		Rolls: append([]int(nil), p.Rolls...), GrowthRolls: append([]int(nil), p.GrowthRolls...), StatRoll: p.StatRoll,
		ExperienceBefore: p.ExperienceBefore, ExperienceAfter: p.ExperienceAfter,
		GoldBefore: p.GoldBefore, GoldAfter: p.GoldAfter,
		HPMaxBefore: p.HPMaxBefore, HPMaxAfter: p.HPMaxAfter,
		HPCurrentBefore: p.HPCurrentBefore, HPCurrentAfter: p.HPCurrentAfter,
		MPMaxBefore: p.MPMaxBefore, MPMaxAfter: p.MPMaxAfter,
		MPCurrentBefore: p.MPCurrentBefore, MPCurrentAfter: p.MPCurrentAfter,
		StatsBefore: p.StatsBefore, StatsAfter: p.StatsAfter,
		DiceCountBefore: p.DiceCountBefore, DiceCountAfter: p.DiceCountAfter,
		DiceSidesBefore: p.DiceSidesBefore, DiceSidesAfter: p.DiceSidesAfter,
		DicePlusBefore: p.DicePlusBefore, DicePlusAfter: p.DicePlusAfter,
	}
}

// ApplyBuyStates verifies the proposal against the current actor and applies
// the already-recorded C transition. It never calls Roll. Rejected proposals
// return no candidate state, so callers cannot accidentally commit a partial
// resource/stat mutation.
func (s State) ApplyBuyStates(p BuyStatesProposal) (State, BuyStatesResult, error) {
	if p.ActorID == "" {
		return State{}, BuyStatesResult{}, ErrBuyStatesInvalidProposal
	}
	a, err := buyStatesAdmissionFor(s, p.ActorID, p.Stat)
	if err != nil {
		return State{}, BuyStatesResult{}, err
	}
	if !a.apply {
		expected := buyStatesProposalFromAdmission(a)
		if !buyStatesProposalMatches(p, expected) {
			return State{}, BuyStatesResult{}, ErrBuyStatesStaleProposal
		}
		next := s.clone()
		if err := next.Validate(); err != nil {
			return State{}, BuyStatesResult{}, err
		}
		return next, buyStatesResult(p), nil
	}
	expected, err := buyStatesBuildApplied(a, p.Rolls)
	if err != nil {
		return State{}, BuyStatesResult{}, err
	}
	if !buyStatesProposalMatches(p, expected) {
		return State{}, BuyStatesResult{}, ErrBuyStatesStaleProposal
	}
	next := s.clone()
	nextActor := next.Players[p.ActorID]
	nextActor.Body.Experience = p.ExperienceAfter
	nextActor.Body.Gold = p.GoldAfter
	nextActor.Body.HPMax = p.HPMaxAfter
	nextActor.Body.HPCurrent = p.HPCurrentAfter
	nextActor.Body.MPMax = p.MPMaxAfter
	nextActor.Body.MPCurrent = p.MPCurrentAfter
	nextActor.Body.Stats = p.StatsAfter
	nextActor.Body.DiceCount = p.DiceCountAfter
	nextActor.Body.DiceSides = p.DiceSidesAfter
	nextActor.Body.DicePlus = p.DicePlusAfter
	next.Players[p.ActorID] = nextActor
	if err := next.Validate(); err != nil {
		return State{}, BuyStatesResult{}, err
	}
	return next, buyStatesResult(p), nil
}

// BuyStates is the reducer-shaped convenience API for the existing first
// gate. Use BuyStatesWithOptions to admit random apply branches.
func (s State) BuyStates(actorID, stat string) (State, BuyStatesResult, error) {
	return s.BuyStatesWithOptions(actorID, stat, BuyStatesOptions{})
}

func (s State) BuyStatesWithOptions(actorID, stat string, options BuyStatesOptions) (State, BuyStatesResult, error) {
	p, err := s.PlanBuyStatesWithOptions(actorID, stat, options)
	if err != nil {
		return State{}, BuyStatesResult{}, err
	}
	return s.ApplyBuyStates(p)
}

func (s State) BuyStatesWithRoll(actorID, stat string, roll func(int, int) int) (State, BuyStatesResult, error) {
	return s.BuyStatesWithOptions(actorID, stat, BuyStatesOptions{Roll: roll})
}
