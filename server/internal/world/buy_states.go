package world

import (
	"errors"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"
)

// buy_states is the bounded Go port of command11.c:buy_states (cmdlist-149
// `향상`). This slice admits the C first prompt/gate only: CARETAKER,
// missing-argument prompt, then experience/gold. 체력/도력 and the later
// stat applies stay fail-closed. Nested legacy inventory is not a gold or
// stats source.
const (
	buyStatesCaretakerClass  = 10 // CARETAKER; class ==, not >=
	buyStatesExperienceFloor = int64(100000000)
	buyStatesGoldUnit        = int64(1000000)
	BuyStatesCaretakerClass  = buyStatesCaretakerClass
	BuyStatesExperienceFloor = buyStatesExperienceFloor
	BuyStatesGoldUnit        = buyStatesGoldUnit
)

const (
	BuyStatesNotCaretakerResponse = "초인만이 가능합니다."
	BuyStatesPromptResponse       = "\"체력\" 과 \"도력\" 중 어느 것을 올리시려고요?"
	BuyStatesExperienceResponse   = "당신의 경험치로는 능력치 향상을 할 수 없습니다."
	BuyStatesGoldResponse         = "당신이 가진 돈으로는 능력치 향상을 할 수 없습니다."
	BuyStatesUnknownStatResponse  = "어떤 능력치를 올리시려고요?"
)

type BuyStatesAction string

const (
	BuyStatesNotCaretaker BuyStatesAction = "not_caretaker"
	BuyStatesPrompt       BuyStatesAction = "prompt"
	BuyStatesExperience   BuyStatesAction = "experience"
	BuyStatesGold         BuyStatesAction = "gold"
	BuyStatesUnknownStat  BuyStatesAction = "unknown_stat"
)

var (
	ErrBuyStatesActorAbsent     = errors.New("online canonical buy-states actor absent")
	ErrBuyStatesGoldUnresolved  = errors.New("canonical gold required")
	ErrBuyStatesStatsUnresolved = errors.New("canonical stats required")
	ErrBuyStatesApplyPending    = errors.New("buy_states apply is not admitted")
	ErrBuyStatesInvalidStat     = errors.New("invalid buy-states stat selector")
	ErrBuyStatesStaleProposal   = errors.New("stale or invalid buy-states proposal")
	ErrBuyStatesInvalidProposal = errors.New("invalid buy-states proposal")
	ErrBuyStatesNameInvalid     = errors.New("buy-states actor name is unsafe")
)

type BuyStatesResult struct {
	Action   BuyStatesAction `json:"action"`
	ActorID  string          `json:"actor_id"`
	Stat     string          `json:"stat,omitempty"`
	Response string          `json:"response"`
	Changed  bool            `json:"changed"`
	Amount   int64           `json:"amount,omitempty"`
	Cost     int64           `json:"cost,omitempty"`
}

type BuyStatesProposal struct {
	Action   BuyStatesAction
	ActorID  string
	Stat     string
	Response string
	Changed  bool
	Amount   int64
	Cost     int64

	expectedActor PlayerState
}

func buyStatesApplyPending(stat string) bool {
	switch stat {
	case "체력", "도력", "힘", "민첩", "맷집", "지식", "신앙심":
		return true
	default:
		return false
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
	cost := amt * buyStatesGoldUnit
	if cost/amt != buyStatesGoldUnit {
		return 0, ErrBuyStatesGoldUnresolved
	}
	return cost, nil
}

func buyStatesUnchanged(action BuyStatesAction, actorID, stat, response string, amount, cost int64, actor PlayerState) BuyStatesProposal {
	return BuyStatesProposal{
		Action: action, ActorID: actorID, Stat: stat, Response: response,
		Amount: amount, Cost: cost, expectedActor: actor,
	}
}

func buyStatesResult(p BuyStatesProposal) BuyStatesResult {
	return BuyStatesResult{
		Action: p.Action, ActorID: p.ActorID, Stat: p.Stat, Response: p.Response,
		Changed: p.Changed, Amount: p.Amount, Cost: p.Cost,
	}
}

func buyStatesProposalMatches(actual, expected BuyStatesProposal) bool {
	return actual.Action == expected.Action && actual.ActorID == expected.ActorID &&
		actual.Stat == expected.Stat && actual.Response == expected.Response &&
		actual.Changed == expected.Changed && actual.Amount == expected.Amount &&
		actual.Cost == expected.Cost &&
		reflect.DeepEqual(actual.expectedActor, expected.expectedActor)
}

// PlanBuyStates is command11.c:buy_states admission through the first
// prompt and experience/gold gates. A present 체력/도력/능력치 token that
// would enter the C apply branches is fail-closed instead of inventing
// hpmax/mpmax/RNG writes.
func (s State) PlanBuyStates(actorID, stat string) (BuyStatesProposal, error) {
	actor, err := buyStatesActor(s, actorID)
	if err != nil {
		return BuyStatesProposal{}, err
	}
	if err := validBuyStatesStat(stat); err != nil {
		return BuyStatesProposal{}, err
	}
	if actor.Body.Class != buyStatesCaretakerClass {
		return buyStatesUnchanged(BuyStatesNotCaretaker, actorID, stat, BuyStatesNotCaretakerResponse, 0, 0, actor), nil
	}
	if stat == "" {
		return buyStatesUnchanged(BuyStatesPrompt, actorID, stat, BuyStatesPromptResponse, 0, 0, actor), nil
	}
	if err := buyStatesCanonicalGoldAndStats(actor); err != nil {
		return BuyStatesProposal{}, err
	}
	amt := buyStatesAmount(actor.Body.Experience)
	if amt < 1 {
		return buyStatesUnchanged(BuyStatesExperience, actorID, stat, BuyStatesExperienceResponse, amt, 0, actor), nil
	}
	cost, err := buyStatesGoldCost(amt)
	if err != nil {
		return BuyStatesProposal{}, err
	}
	if int64(actor.Body.Gold) < cost {
		return buyStatesUnchanged(BuyStatesGold, actorID, stat, BuyStatesGoldResponse, amt, cost, actor), nil
	}
	if buyStatesApplyPending(stat) {
		return BuyStatesProposal{}, ErrBuyStatesApplyPending
	}
	return buyStatesUnchanged(BuyStatesUnknownStat, actorID, stat, BuyStatesUnknownStatResponse, amt, cost, actor), nil
}

// ApplyBuyStates records the first-gate no-op. This slice never mutates
// gold, experience, or vitals; a Changed candidate is invalid.
func (s State) ApplyBuyStates(p BuyStatesProposal) (State, BuyStatesResult, error) {
	if p.ActorID == "" {
		return State{}, BuyStatesResult{}, ErrBuyStatesInvalidProposal
	}
	expected, err := s.PlanBuyStates(p.ActorID, p.Stat)
	if err != nil {
		return State{}, BuyStatesResult{}, err
	}
	if !buyStatesProposalMatches(p, expected) {
		return State{}, BuyStatesResult{}, ErrBuyStatesStaleProposal
	}
	if p.Changed {
		return State{}, BuyStatesResult{}, ErrBuyStatesInvalidProposal
	}
	next := s.clone()
	if err := next.Validate(); err != nil {
		return State{}, BuyStatesResult{}, err
	}
	return next, buyStatesResult(p), nil
}

// BuyStates is the reducer-shaped convenience API for the 향상 first gate.
func (s State) BuyStates(actorID, stat string) (State, BuyStatesResult, error) {
	p, err := s.PlanBuyStates(actorID, stat)
	if err != nil {
		return State{}, BuyStatesResult{}, err
	}
	return s.ApplyBuyStates(p)
}
