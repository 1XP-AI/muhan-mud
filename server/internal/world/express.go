package world

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ExpressAlias is the legacy command11.c emote command as registered by the
// global command table.  It is intentionally separate from the bounded
// action.c aliases in emote.go: 표현 accepts a player-supplied message.
const ExpressAlias = "표현"

// MaxExpressTextBytes follows the legacy cmd.fullstr byte budget.  The C
// command owns a 256-byte buffer and reserves the terminating byte, so the
// payload admitted by this Go boundary is at most 255 UTF-8 bytes.
const MaxExpressTextBytes = 255

// ExpressTextMaxBytes is a descriptive compatibility alias for callers that
// name command limits by the domain rather than the legacy field name.
const ExpressTextMaxBytes = MaxExpressTextBytes

var (
	ErrInvalidExpressText = errors.New("invalid 표현 text")
	ErrExpressTextTooLong = errors.New("표현 text exceeds the byte limit")
)

// ExpressResult is the durable part of an 표현 command.  It deliberately
// contains no event or arbitrary user text: the receipt stores only the actor
// response, while a room event is derived from the committed snapshot by
// RoomExpressEvent after the receipt succeeds.
type ExpressResult struct {
	Response  string
	Broadcast bool
}

// ExpressEvent is a post-commit room projection.  It is not part of State or
// the durable receipt.  ExcludeActorID keeps the transport boundary explicit:
// the actor already received the command response and must not receive this
// asynchronous room event a second time.
type ExpressEvent struct {
	RoomID         int16
	ActorID        string
	ActorName      string
	ExcludeActorID string
	Text           string
}

// IsExpressAlias reports whether alias is the exact global.c command alias.
// Prefixes and occurrence selectors are intentionally not inferred here.
func IsExpressAlias(alias string) bool { return strings.TrimSpace(alias) == ExpressAlias }

// ValidateExpressText validates the bounded, line-oriented payload.  The
// command is sent through a terminal line, so control characters are not
// meaningful message content and could otherwise inject line breaks or
// terminal formatting into the room fan-out.  We validate before any JSON
// marshaling because encoding/json replaces invalid UTF-8 rather than
// preserving the rejection boundary.
func ValidateExpressText(text string) error {
	if !utf8.ValidString(text) {
		return ErrInvalidExpressText
	}
	if len(text) > MaxExpressTextBytes {
		return ErrExpressTextTooLong
	}
	for _, r := range text {
		if unicode.IsControl(r) {
			return ErrInvalidExpressText
		}
	}
	return nil
}

// PlanExpress applies the stateful portion of command11.c:emote.  Empty and
// silenced expressions are successful no-op receipts.  In particular, a
// silenced player remains hidden: the legacy command clears PHIDDN only on a
// non-silent expression.  A successful non-silent command clears PHIDDN on a
// cloned candidate, leaving the input snapshot untouched.
func (s State) PlanExpress(actorID, text string) (State, ExpressResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, ExpressResult{}, err
	}
	if err := ValidateExpressText(text); err != nil {
		return State{}, ExpressResult{}, err
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online {
		return State{}, ExpressResult{}, fmt.Errorf("online expression actor absent")
	}
	if !validExpressActorName(actor.Body.Name) {
		return State{}, ExpressResult{}, fmt.Errorf("expression actor name is not safe")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return s, ExpressResult{Response: "무슨말을 표현하시려구요?\r\n"}, nil
	}
	if flag(actor.Body.Flags[:], playerSilentStateFlag) {
		return s, ExpressResult{Response: "당신은 지금당장 그것을 할 수 없습니다.\r\n"}, nil
	}

	next := s.clone()
	actor = next.Players[actorID]
	actor.Body.Flags[playerHiddenStateFlag/8] &^= 1 << (playerHiddenStateFlag % 8)
	next.Players[actorID] = actor
	response := "예. 좋습니다.\r\n"
	if flag(actor.Body.Flags[:], playerLocalEchoFlag) {
		response = fmt.Sprintf(":%s님이 %s.\r\n", actor.Body.Name, text)
	}
	return next, ExpressResult{Response: response, Broadcast: true}, nil
}

// RoomExpressEvent derives the fixed-format room message from a committed
// snapshot.  It must be called only after a non-replayed receipt.  The payload
// is validated again so an event publisher cannot turn a later untrusted
// string into an injection even if it bypasses PlanExpress.
func (s State) RoomExpressEvent(actorID, text string) (ExpressEvent, bool, error) {
	if err := s.Validate(); err != nil {
		return ExpressEvent{}, false, err
	}
	if err := ValidateExpressText(text); err != nil {
		return ExpressEvent{}, false, err
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online {
		return ExpressEvent{}, false, fmt.Errorf("online expression actor absent")
	}
	if !validExpressActorName(actor.Body.Name) {
		return ExpressEvent{}, false, fmt.Errorf("expression actor name is not safe")
	}
	text = strings.TrimSpace(text)
	if text == "" || flag(actor.Body.Flags[:], playerSilentStateFlag) {
		return ExpressEvent{}, false, nil
	}
	return ExpressEvent{
		RoomID:         actor.Body.RoomID,
		ActorID:        actorID,
		ActorName:      actor.Body.Name,
		ExcludeActorID: actorID,
		Text:           fmt.Sprintf("\n:%s님이 %s.\r\n", actor.Body.Name, text),
	}, true, nil
}

func validExpressActorName(name string) bool {
	if name == "" || !utf8.ValidString(name) {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
