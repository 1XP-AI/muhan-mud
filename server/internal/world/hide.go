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

var (
	// ErrHideObjectUnsupported is retained for callers that still distinguish
	// the old pre-migration boundary. Object hiding is now admitted only for a
	// canonical same-room floor root.
	ErrHideObjectUnsupported = errors.New("object hiding is not implemented")
	ErrHideObjectCannot      = errors.New("object cannot be hidden")
)

// HideProposal is the deterministic candidate for command5.c:hide. Succeeded
// is the one random outcome that must travel with a receipt: replay must not
// call the random source again. ObjectID is authoritative only after it has
// been resolved from the committed canonical room snapshot.
type HideProposal struct {
	ActorID string
	RoomID  int16
	// ObjectID/Name/Occurrence identify a canonical floor root when the
	// object branch is selected.  They are resolved from the committed room
	// snapshot during planning; clients never supply an item ID.
	ObjectID         string
	ObjectName       string
	ObjectOccurrence int
	ObjectBranch     bool
	ObjectWasHidden  bool
	Now              int32
	Chance           int
	Interval         int32
	WaitSeconds      int32
	Cooldown         bool
	Succeeded        bool
	Broadcast        bool
	Response         string
}

// HideResult is the durable actor response plus the outcome needed to derive
// the post-commit room event.  Succeeded is false for both a failed attempt
// and a cooldown no-op; Broadcast distinguishes those cases.
type HideResult struct {
	Response         string `json:"response"`
	Broadcast        bool   `json:"broadcast"`
	Succeeded        bool   `json:"succeeded"`
	ObjectID         string `json:"object_id,omitempty"`
	ObjectName       string `json:"object_name,omitempty"`
	ObjectOccurrence int    `json:"object_occurrence,omitempty"`
	ObjectBranch     bool   `json:"object_branch,omitempty"`
	RoomText         string `json:"room_text,omitempty"`
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
	ObjectID       string
	ObjectName     string
	Succeeded      bool
	Text           string
}

const (
	hideObjectAttemptResponse = "당신은 그것을 숨겨보려고 합니다."
	hideObjectSuccessResponse = "\r\n당신은 성공적으로 숨겼습니다."
)

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

// HideObjectChance ports the object branch of command5.c:hide.  Its chance
// table is intentionally separate from HideChance: the legacy player branch
// uses different coefficients, while the object branch uses 10/5/5 for
// thief/assassin, 5/9/3 for ranger, and 5/3/3 for all other classes.
// Unlike the player branch, the source object path has no blindness cap.
func HideObjectChance(player LegacyMonster) (int, error) {
	if player.Class > 12 {
		return 0, fmt.Errorf("hide object actor class outside legacy table")
	}
	if player.Stats[1] > 63 {
		return 0, fmt.Errorf("hide object actor dexterity outside legacy bonus table")
	}

	levelBand := (int(player.Level) + 3) / 4
	dexBonus := legacyStatBonus[player.Stats[1]]
	var chance int
	switch {
	case player.Class == hideThiefClass || player.Class == hideAssassinClass:
		chance = 10 + 5*levelBand + 5*dexBonus
		if chance > 90 {
			chance = 90
		}
	case player.Class == hideRangerClass:
		chance = 5 + 9*levelBand + 3*dexBonus
	default:
		chance = 5 + 3*levelBand + 3*dexBonus
		if chance > 90 {
			chance = 90
		}
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

// selectHideObjectRoot resolves an exact canonical room-floor root in stored
// order. Nested contents are not room roots, while a container root itself is
// a valid floor object. Prefix/key lookup is deliberately not inferred here;
// the command boundary admits a display name and a positive occurrence only.
func selectHideObjectRoot(room RoomState, name string, occurrence int) (string, Item, error) {
	if occurrence < 1 {
		return "", Item{}, fmt.Errorf("invalid hide object occurrence")
	}
	if !validHideActorName(name) {
		return "", Item{}, fmt.Errorf("invalid hide object name")
	}
	if room.Items == nil {
		if len(room.Resource.Objects) != 0 {
			return "", Item{}, fmt.Errorf("%w: canonical hide room objects unavailable", ErrHideObjectUnsupported)
		}
		return "", Item{}, fmt.Errorf("%w: canonical hide room items required", ErrHideObjectUnsupported)
	}
	if err := room.Items.Validate(); err != nil {
		return "", Item{}, fmt.Errorf("canonical hide room items unavailable: %w", err)
	}
	found := 0
	for _, id := range room.Items.Inventory {
		item, ok := room.Items.Items[id]
		if !ok || id == "" {
			return "", Item{}, fmt.Errorf("canonical hide room root absent")
		}
		if item.Object.Name != name {
			continue
		}
		found++
		if found == occurrence {
			return id, item, nil
		}
	}
	return "", Item{}, fmt.Errorf("hide object not found: %q", name)
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

// PlanHideObject plans command5.c:hide's canonical floor-object branch with
// its default first occurrence. The player cooldown is checked before target
// resolution or RNG, and a valid target consumes exactly one 1..100 roll.
func (s State) PlanHideObject(actorID, objectName string, now int32, roll func(int, int) int) (HideProposal, error) {
	return s.PlanHideObjectWithOccurrence(actorID, objectName, 1, now, roll)
}

// PlanHideObjectWithOccurrence is the explicit object branch. The selector is
// resolved against direct canonical floor roots only, in room inventory order.
func (s State) PlanHideObjectWithOccurrence(actorID, objectName string, occurrence int, now int32, roll func(int, int) int) (HideProposal, error) {
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
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok {
		return HideProposal{}, fmt.Errorf("hide actor room absent")
	}
	chance, err := HideObjectChance(actor.Body)
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

	objectID, object, err := selectHideObjectRoot(room, objectName, occurrence)
	if err != nil {
		return HideProposal{}, err
	}
	if flag(object.Object.Flags[:], objectNotTakeFlag) {
		return HideProposal{}, fmt.Errorf("%w: %q", ErrHideObjectCannot, objectName)
	}
	success, err := hideRoll(roll, chance)
	if err != nil {
		return HideProposal{}, err
	}
	proposal.ObjectID = objectID
	proposal.ObjectName = object.Object.Name
	proposal.ObjectOccurrence = occurrence
	proposal.ObjectBranch = true
	proposal.ObjectWasHidden = flag(object.Object.Flags[:], objectHiddenFlag)
	proposal.Succeeded = success
	proposal.Broadcast = true
	proposal.Response = hideObjectAttemptResponse
	if success {
		proposal.Response += hideObjectSuccessResponse
	}
	return proposal, nil
}

// PlanHideTarget is a command-boundary convenience for callers that parse an
// optional hide argument. An empty argument is the bare player branch; a
// non-empty argument is a canonical same-room floor root.
func (s State) PlanHideTarget(actorID, target string, now int32, roll func(int, int) int) (HideProposal, error) {
	return s.PlanHideTargetWithOccurrence(actorID, target, 1, now, roll)
}

// PlanHideTargetWithOccurrence keeps the client input at display-name scope;
// the authoritative object identity is resolved only from the current State.
func (s State) PlanHideTargetWithOccurrence(actorID, target string, occurrence int, now int32, roll func(int, int) int) (HideProposal, error) {
	if strings.TrimSpace(target) != "" {
		return s.PlanHideObjectWithOccurrence(actorID, target, occurrence, now, roll)
	}
	if occurrence != 1 {
		return HideProposal{}, fmt.Errorf("bare hide occurrence must be one")
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
	chance := 0
	var err error
	if proposal.ObjectBranch {
		chance, err = HideObjectChance(actor.Body)
	} else {
		chance, err = HideChance(actor.Body)
	}
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
	if proposal.ObjectBranch {
		room, roomOK := s.Rooms[proposal.RoomID]
		if !roomOK || room.Items == nil || proposal.ObjectID == "" || proposal.ObjectName == "" || proposal.ObjectOccurrence < 1 || proposal.ObjectOccurrence > len(room.Items.Inventory) {
			return State{}, HideResult{}, fmt.Errorf("invalid hide object proposal")
		}
		objectID, object, err := selectHideObjectRoot(room, proposal.ObjectName, proposal.ObjectOccurrence)
		if err != nil || objectID != proposal.ObjectID {
			if err != nil {
				return State{}, HideResult{}, fmt.Errorf("stale hide object proposal: %w", err)
			}
			return State{}, HideResult{}, fmt.Errorf("stale hide object identity")
		}
		if proposal.ObjectWasHidden != flag(object.Object.Flags[:], objectHiddenFlag) {
			return State{}, HideResult{}, fmt.Errorf("stale hide object flag")
		}
		if flag(object.Object.Flags[:], objectNotTakeFlag) {
			return State{}, HideResult{}, fmt.Errorf("%w: %q", ErrHideObjectCannot, proposal.ObjectName)
		}
		if proposal.Response != hideObjectAttemptResponse && proposal.Response != hideObjectAttemptResponse+hideObjectSuccessResponse {
			return State{}, HideResult{}, fmt.Errorf("hide object response mismatch")
		}
		next := s.clone()
		nextActor := next.Players[proposal.ActorID]
		nextActor.Body.Timers[hideTimerIndex].LastTime = proposal.Now
		nextActor.Body.Timers[hideTimerIndex].Interval = interval
		next.Players[proposal.ActorID] = nextActor
		nextRoom := next.Rooms[proposal.RoomID]
		nextObject := nextRoom.Items.Items[proposal.ObjectID]
		setObjectFlag(&nextObject.Object.Flags, objectHiddenFlag, proposal.Succeeded)
		nextRoom.Items.Items[proposal.ObjectID] = nextObject
		next.Rooms[proposal.RoomID] = nextRoom
		if err := next.Validate(); err != nil {
			return State{}, HideResult{}, err
		}
		roomText := fmt.Sprintf("\n%s님이 %s 숨겨보려고 합니다.\r\n", actor.Body.Name, proposal.ObjectName)
		if proposal.Succeeded {
			roomText = fmt.Sprintf("\n%s님이 %s 어딘가 숨깁니다.\r\n", actor.Body.Name, proposal.ObjectName)
		}
		return next, HideResult{
			Response:         proposal.Response,
			Broadcast:        true,
			Succeeded:        proposal.Succeeded,
			ObjectID:         proposal.ObjectID,
			ObjectName:       proposal.ObjectName,
			ObjectOccurrence: proposal.ObjectOccurrence,
			ObjectBranch:     true,
			RoomText:         roomText,
		}, nil
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

// RoomHideObjectEvent derives the object-branch room projection from the
// committed canonical root. The explicit outcome from a durable receipt is
// preferred; when omitted, the committed OHIDDN bit is used. This keeps a
// replay from rerolling or accidentally rebroadcasting a different object.
func (s State) RoomHideObjectEvent(actorID, objectID string, succeeded ...bool) (HideEvent, bool, error) {
	if len(succeeded) > 1 {
		return HideEvent{}, false, fmt.Errorf("hide object event accepts at most one outcome")
	}
	if err := s.Validate(); err != nil {
		return HideEvent{}, false, err
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Body.Type != 0 || !validHideActorName(actor.Body.Name) {
		return HideEvent{}, false, fmt.Errorf("online hide actor absent")
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok || room.Items == nil {
		return HideEvent{}, false, fmt.Errorf("canonical hide room items required")
	}
	object, ok := room.Items.Items[objectID]
	if !ok || objectID == "" {
		return HideEvent{}, false, fmt.Errorf("hide object absent")
	}
	// The event must address a direct room root, never a nested child.
	root := false
	for _, id := range room.Items.Inventory {
		if id == objectID {
			root = true
			break
		}
	}
	if !root || !validHideActorName(object.Object.Name) {
		return HideEvent{}, false, fmt.Errorf("hide object is not a canonical room root")
	}
	outcome := flag(object.Object.Flags[:], objectHiddenFlag)
	if len(succeeded) == 1 {
		outcome = succeeded[0]
	}
	text := fmt.Sprintf("\n%s님이 %s 숨겨보려고 합니다.\r\n", actor.Body.Name, object.Object.Name)
	if outcome {
		text = fmt.Sprintf("\n%s님이 %s 어딘가 숨깁니다.\r\n", actor.Body.Name, object.Object.Name)
	}
	return HideEvent{
		RoomID:         actor.Body.RoomID,
		ActorID:        actorID,
		ActorName:      actor.Body.Name,
		ExcludeActorID: actorID,
		ObjectID:       objectID,
		ObjectName:     object.Object.Name,
		Succeeded:      outcome,
		Text:           text,
	}, true, nil
}

// RoomHideObjectEventFor is a named form for receipt consumers that always
// have a persisted object outcome.
func (s State) RoomHideObjectEventFor(actorID, objectID string, succeeded bool) (HideEvent, bool, error) {
	return s.RoomHideObjectEvent(actorID, objectID, succeeded)
}
