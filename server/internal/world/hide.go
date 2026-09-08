package world

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// hideTimerIndex is LT_HIDES in src/mtype.h.  The timer is part of the
	// player's legacy 45-entry timer array, not a transport-local lockout.
	hideTimerIndex = 14
	HideTimerIndex = hideTimerIndex

	hideAssassinClass  = 1  // ASSASSIN
	hideRangerClass    = 7  // RANGER
	hideThiefClass     = 8  // THIEF
	hideCaretakerClass = 10 // CARETAKER; SUB_DM and DM are included by >=.
	hideBlindFlag      = 42 // PBLIND
)

var ErrHideObjectUnsupported = errors.New("object hiding is not implemented")

// HideProposal is the deterministic candidate for the bare player branch of
// command5.c:hide.  Succeeded is the one random outcome that must travel with
// a receipt: replay must not call the random source again, and both outcomes
// produce a room projection in the legacy command.
//
// The object branch is deliberately not represented by a target identity.
// Callers that have an object argument must reject it at the command boundary
// (PlanHideObject/PlanHideTarget return ErrHideObjectUnsupported).
type HideProposal struct {
	ActorID     string
	RoomID      int16
	Now         int32
	Chance      int
	Interval    int32
	WaitSeconds int32
	Cooldown    bool
	Succeeded   bool
	Broadcast   bool
	Response    string
}

// HideResult is the durable actor response plus the outcome needed to derive
// the post-commit room event.  Succeeded is false for both a failed attempt
// and a cooldown no-op; Broadcast distinguishes those cases.
type HideResult struct {
	Response  string
	Broadcast bool
	Succeeded bool
}

// HideEvent is the committed-state projection of the bare player hide branch.
// ExcludeActorID is explicit because the actor already receives Response.
// ActorName is retained alongside Text for consumers that need a structured
// projection; recipient-specific PINVIS/PDMINV redaction remains a transport
// concern and is not guessed here.
type HideEvent struct {
	RoomID         int16
	ActorID        string
	ActorName      string
	ExcludeActorID string
	Succeeded      bool
	Text           string
}

// HideChance ports command5.c's player/bare chance calculation.  C indexes
// bonus[] with dexterity and reads class constants from mtype.h; an admitted
// Go snapshot with an out-of-table class/stat is rejected instead of allowing
// an undefined C read to become a guessed result.
//
// The source ordering is significant:
//   - thief/assassin/caretaker use the 6-per-level formula capped at 90;
//   - ranger uses the 10-per-level formula with no source cap;
//   - all other classes use the 2-per-level formula capped at 90;
//   - blindness applies the final MIN(chance, 20), including ranger and
//     caretaker overrides.
func HideChance(player LegacyMonster) (int, error) {
	if player.Class > 12 {
		return 0, fmt.Errorf("hide actor class outside legacy table")
	}
	if player.Stats[1] > 63 {
		return 0, fmt.Errorf("hide actor dexterity outside legacy bonus table")
	}

	levelBand := (int(player.Level) + 3) / 4
	dexBonus := legacyStatBonus[player.Stats[1]]
	var chance int
	switch {
	case player.Class == hideThiefClass || player.Class == hideAssassinClass || player.Class >= hideCaretakerClass:
		chance = 5 + 6*levelBand + 3*dexBonus
		if chance > 90 {
			chance = 90
		}
	case player.Class == hideRangerClass:
		chance = 5 + 10*levelBand + 3*dexBonus
	default:
		chance = 5 + 2*levelBand + 3*dexBonus
		if chance > 90 {
			chance = 90
		}
	}
	if flag(player.Flags[:], hideBlindFlag) && chance > 20 {
		chance = 20
	}
	return chance, nil
}

func hideInterval(player LegacyMonster) (int32, error) {
	if player.Class > 12 {
		return 0, fmt.Errorf("hide actor class outside legacy table")
	}
	if player.Class == hideThiefClass || player.Class == hideAssassinClass || player.Class == hideRangerClass {
		return 5, nil
	}
	return 15, nil
}

func hideWaitResponse(seconds int32) string {
	if seconds == 1 {
		return "1초만 기다리세요.\r\n"
	}
	return fmt.Sprintf("%d초동안 기다리세요.\r\n", seconds)
}

func validHideActorName(name string) bool {
	if name == "" || !utf8.ValidString(name) || strings.TrimSpace(name) != name {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == '\r' || r == '\n' {
			return false
		}
	}
	return true
}

// hideRoll validates the random boundary and turns a panicking injected RNG
// into an ordinary planning error.  A malformed RNG must never produce a
// partially applicable proposal.
func hideRoll(roll func(int, int) int, chance int) (success bool, err error) {
	if roll == nil {
		return false, fmt.Errorf("missing hide random source")
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			success = false
			err = fmt.Errorf("hide random source panicked: %v", recovered)
		}
	}()
	value := roll(1, 100)
	if value < 1 || value > 100 {
		return false, fmt.Errorf("hide random value outside 1..100")
	}
	return value <= chance, nil
}

// PlanHide ports only command5.c:hide's bare player branch.  Cooldown is
// checked before the random source is touched, matching LT(ply, LT_HIDES)
// and please_wait.  Once eligible, the candidate consumes exactly one
// 1..100 roll; the timer write and PHIDDN outcome are applied by ApplyHide.
func (s State) PlanHide(actorID string, now int32, roll func(int, int) int) (HideProposal, error) {
	if err := s.Validate(); err != nil {
		return HideProposal{}, err
	}
	if actorID == "" || now < 0 {
		return HideProposal{}, fmt.Errorf("invalid hide actor or clock")
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Body.Type != 0 || !validHideActorName(actor.Body.Name) {
		return HideProposal{}, fmt.Errorf("online hide actor absent")
	}
	if _, ok := s.Rooms[actor.Body.RoomID]; !ok {
		return HideProposal{}, fmt.Errorf("hide actor room absent")
	}
	chance, err := HideChance(actor.Body)
	if err != nil {
		return HideProposal{}, err
	}
	interval, err := hideInterval(actor.Body)
	if err != nil {
		return HideProposal{}, err
	}
	timer := actor.Body.Timers[hideTimerIndex]
	if timer.LastTime < 0 || timer.Interval < 0 {
		return HideProposal{}, fmt.Errorf("hide timer outside legacy range")
	}
	deadline := int64(timer.LastTime) + int64(timer.Interval)
	proposal := HideProposal{
		ActorID:  actorID,
		RoomID:   actor.Body.RoomID,
		Now:      now,
		Chance:   chance,
		Interval: interval,
	}
	if int64(now) < deadline {
		wait := deadline - int64(now)
		if wait < 1 || wait > int64(^uint32(0)>>1) {
			return HideProposal{}, fmt.Errorf("hide cooldown overflow")
		}
		proposal.Cooldown = true
		proposal.WaitSeconds = int32(wait)
		proposal.Response = hideWaitResponse(proposal.WaitSeconds)
		return proposal, nil
	}

	success, err := hideRoll(roll, chance)
	if err != nil {
		return HideProposal{}, err
	}
	proposal.Succeeded = success
	proposal.Broadcast = true
	if success {
		proposal.Response = "당신은 애써 숨어보려고 합니다.\r\n당신은 성공적으로 숨었습니다."
	} else {
		// C prints no direct success/failure line on a failed roll; the
		// failure wording is only the room broadcast.
		proposal.Response = "당신은 애써 숨어보려고 합니다."
	}
	return proposal, nil
}

// PlanHideObject is the explicit fail-closed boundary for command5.c's
// object branch.  No object identity, ONOTAK check, or object flag mutation is
// admitted by this slice.
func (s State) PlanHideObject(actorID, objectName string, now int32, roll func(int, int) int) (HideProposal, error) {
	return HideProposal{}, fmt.Errorf("%w: %q", ErrHideObjectUnsupported, objectName)
}

// PlanHideTarget is a command-boundary convenience for callers that parse an
// optional hide argument.  An empty argument is the admitted bare path;
// non-empty arguments are object requests and fail closed.
func (s State) PlanHideTarget(actorID, target string, now int32, roll func(int, int) int) (HideProposal, error) {
	if strings.TrimSpace(target) != "" {
		return s.PlanHideObject(actorID, target, now, roll)
	}
	return s.PlanHide(actorID, now, roll)
}

// ApplyHide atomically applies a proposal created from the same state.  It
// rechecks all class/stat/timer-derived values and the cooldown branch so a
// stale proposal cannot silently set PHIDDN or only part of its timer.
func (s State) ApplyHide(proposal HideProposal) (State, HideResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, HideResult{}, err
	}
	actor, ok := s.Players[proposal.ActorID]
	if !ok || !actor.Online || actor.Body.Type != 0 || actor.Body.RoomID != proposal.RoomID || !validHideActorName(actor.Body.Name) {
		return State{}, HideResult{}, fmt.Errorf("hide actor changed")
	}
	if proposal.ActorID == "" || proposal.Now < 0 || proposal.Response == "" {
		return State{}, HideResult{}, fmt.Errorf("invalid hide proposal")
	}
	chance, err := HideChance(actor.Body)
	if err != nil {
		return State{}, HideResult{}, err
	}
	interval, err := hideInterval(actor.Body)
	if err != nil {
		return State{}, HideResult{}, err
	}
	if proposal.Chance != chance || proposal.Interval != interval {
		return State{}, HideResult{}, fmt.Errorf("stale hide chance or interval proposal")
	}
	timer := actor.Body.Timers[hideTimerIndex]
	if timer.LastTime < 0 || timer.Interval < 0 {
		return State{}, HideResult{}, fmt.Errorf("hide timer outside legacy range")
	}
	deadline := int64(timer.LastTime) + int64(timer.Interval)

	if proposal.Cooldown {
		wait := deadline - int64(proposal.Now)
		if wait < 1 || wait > int64(^uint32(0)>>1) || proposal.WaitSeconds != int32(wait) || proposal.Broadcast || proposal.Succeeded {
			return State{}, HideResult{}, fmt.Errorf("stale hide cooldown proposal")
		}
		if proposal.Response != hideWaitResponse(proposal.WaitSeconds) || int64(proposal.Now) >= deadline {
			return State{}, HideResult{}, fmt.Errorf("hide cooldown response mismatch")
		}
		return s, HideResult{Response: proposal.Response}, nil
	}
	if int64(proposal.Now) < deadline || proposal.WaitSeconds != 0 || !proposal.Broadcast {
		return State{}, HideResult{}, fmt.Errorf("hide cooldown changed")
	}
	if proposal.Succeeded {
		if proposal.Response != "당신은 애써 숨어보려고 합니다.\r\n당신은 성공적으로 숨었습니다." {
			return State{}, HideResult{}, fmt.Errorf("hide success response mismatch")
		}
	} else if proposal.Response != "당신은 애써 숨어보려고 합니다." {
		return State{}, HideResult{}, fmt.Errorf("hide failure response mismatch")
	}

	next := s.clone()
	actor = next.Players[proposal.ActorID]
	actor.Body.Timers[hideTimerIndex].LastTime = proposal.Now
	actor.Body.Timers[hideTimerIndex].Interval = interval
	actor.Body.Flags[playerHiddenStateFlag/8] &^= 1 << (playerHiddenStateFlag % 8)
	if proposal.Succeeded {
		actor.Body.Flags[playerHiddenStateFlag/8] |= 1 << (playerHiddenStateFlag % 8)
	}
	next.Players[proposal.ActorID] = actor
	if err := next.Validate(); err != nil {
		return State{}, HideResult{}, err
	}
	return next, HideResult{Response: proposal.Response, Broadcast: true, Succeeded: proposal.Succeeded}, nil
}

// RoomHideEvent derives the room projection from the committed snapshot. If
// succeeded is omitted, the committed PHIDDN bit is used. A caller that has a
// receipt should pass its persisted outcome explicitly so a later unrelated
// state change cannot turn a success message into a failure message.
func (s State) RoomHideEvent(actorID string, succeeded ...bool) (HideEvent, bool, error) {
	if len(succeeded) > 1 {
		return HideEvent{}, false, fmt.Errorf("hide event accepts at most one outcome")
	}
	if err := s.Validate(); err != nil {
		return HideEvent{}, false, err
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Body.Type != 0 || !validHideActorName(actor.Body.Name) {
		return HideEvent{}, false, fmt.Errorf("online hide actor absent")
	}
	outcome := flag(actor.Body.Flags[:], playerHiddenStateFlag)
	if len(succeeded) == 1 {
		outcome = succeeded[0]
	}
	text := "애써 숨어보려고 합니다."
	if outcome {
		text = "그림자 사이로 숨었습니다."
	}
	return HideEvent{
		RoomID:         actor.Body.RoomID,
		ActorID:        actorID,
		ActorName:      actor.Body.Name,
		ExcludeActorID: actorID,
		Succeeded:      outcome,
		Text:           fmt.Sprintf("\n%s님이 %s\r\n", actor.Body.Name, text),
	}, true, nil
}

// RoomHideEventFor is a named alternative for callers that prefer an
// explicit receipt outcome over the variadic convenience form.
func (s State) RoomHideEventFor(actorID string, succeeded bool) (HideEvent, bool, error) {
	return s.RoomHideEvent(actorID, succeeded)
}
