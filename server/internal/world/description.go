package world

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// command12.c:description measures cmnd.fullstr with strlen before it
	// removes the final command token.  Keep that source byte boundary explicit
	// at the canonical state edge instead of relying on a Go rune count.
	MaxDescriptionLineBytes   = 31
	MaxDescriptionBytes       = MaxDescriptionLineBytes
	MaxPlayerDescriptionBytes = MaxDescriptionLineBytes
)

var (
	ErrInvalidDescription = errors.New("invalid player description")
	ErrDescriptionTooLong = errors.New("player description exceeds the byte limit")
)

// DescriptionResult is the durable response for command12.c:description.
// Description is the canonical PlayerState.Body.Description value and ends in
// one ASCII space when non-empty, matching the source formatter's strcat.
type DescriptionResult struct {
	Action      string `json:"action"`
	ActorID     string `json:"actor_id"`
	Description string `json:"description"`
	Cleared     bool   `json:"cleared"`
	Response    string `json:"response"`
}

// PlayerDescriptionResult is a descriptive compatibility alias for callers
// that name the affected field rather than the legacy command.
type PlayerDescriptionResult = DescriptionResult

// DescriptionProposal is a state-bound candidate. ApplyDescription checks the
// captured body before changing the cloned snapshot, so planning against one
// player cannot overwrite a later durable update.
type DescriptionProposal struct {
	ActorID     string
	RoomID      int16
	Description string
	Cleared     bool
	Response    string

	expectedBody   LegacyMonster
	expectedOnline bool
}

// PlayerDescriptionProposal is a descriptive compatibility alias.
type PlayerDescriptionProposal = DescriptionProposal

// ValidateDescriptionText validates the canonical payload before it reaches a
// receipt or a player body. The command parser supplies text without the
// suffix; this helper accepts either that form or an already canonical value
// with one trailing space and rejects control/invalid UTF-8 bytes.
func ValidateDescriptionText(text string) error {
	if !utf8.ValidString(text) {
		return ErrInvalidDescription
	}
	if len(text) > MaxDescriptionBytes {
		return ErrDescriptionTooLong
	}
	for _, r := range text {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return ErrInvalidDescription
		}
	}
	return nil
}

func canonicalPlayerDescription(text string) (string, error) {
	if err := ValidateDescriptionText(text); err != nil {
		return "", err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", nil
	}
	canonical := text + " "
	if len(canonical) > MaxDescriptionBytes {
		return "", ErrDescriptionTooLong
	}
	return canonical, nil
}

func descriptionActor(s State, actorID string) (PlayerState, error) {
	if err := s.Validate(); err != nil {
		return PlayerState{}, err
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Body.Type != 0 || actor.Body.Name == "" {
		return PlayerState{}, fmt.Errorf("online description actor absent")
	}
	return actor, nil
}

// PlanDescription validates and prepares a player description candidate. The
// command token is deliberately not accepted here: callers pass only the
// text before the final 묘사 token, while an empty text clears the field.
func (s State) PlanDescription(actorID, text string) (DescriptionProposal, error) {
	actor, err := descriptionActor(s, actorID)
	if err != nil {
		return DescriptionProposal{}, err
	}
	canonical, err := canonicalPlayerDescription(text)
	if err != nil {
		return DescriptionProposal{}, err
	}
	proposal := DescriptionProposal{
		ActorID:        actorID,
		RoomID:         actor.Body.RoomID,
		Description:    canonical,
		Cleared:        canonical == "",
		expectedBody:   actor.Body,
		expectedOnline: actor.Online,
	}
	if proposal.Cleared {
		proposal.Response = "당신은 서 있습니다.\r\n"
	} else {
		proposal.Response = fmt.Sprintf("당신은 이제부터 %s서 있습니다.\r\n", canonical)
	}
	return proposal, nil
}

// PlanPlayerDescription is the explicit player-domain alias.
func (s State) PlanPlayerDescription(actorID, text string) (DescriptionProposal, error) {
	return s.PlanDescription(actorID, text)
}

// ApplyDescription applies a previously planned candidate atomically to a
// cloned world snapshot. A stale actor body or room identity returns an error
// and never mutates the receiver.
func (s State) ApplyDescription(proposal DescriptionProposal) (State, DescriptionResult, error) {
	actor, err := descriptionActor(s, proposal.ActorID)
	if err != nil {
		return State{}, DescriptionResult{}, err
	}
	canonical, err := canonicalPlayerDescription(proposal.Description)
	if err != nil || canonical != proposal.Description || (proposal.Cleared != (canonical == "")) {
		if err != nil {
			return State{}, DescriptionResult{}, err
		}
		return State{}, DescriptionResult{}, fmt.Errorf("invalid description proposal")
	}
	if proposal.ActorID == "" || proposal.RoomID != actor.Body.RoomID || proposal.Response == "" ||
		actor.Online != proposal.expectedOnline || !reflect.DeepEqual(actor.Body, proposal.expectedBody) {
		return State{}, DescriptionResult{}, fmt.Errorf("stale or invalid description proposal")
	}

	next := s.clone()
	nextActor := next.Players[proposal.ActorID]
	nextActor.Body.Description = proposal.Description
	next.Players[proposal.ActorID] = nextActor
	if err := next.Validate(); err != nil {
		return State{}, DescriptionResult{}, err
	}
	result := DescriptionResult{
		Action:      "description",
		ActorID:     proposal.ActorID,
		Description: proposal.Description,
		Cleared:     proposal.Cleared,
		Response:    proposal.Response,
	}
	return next, result, nil
}

// ApplyPlayerDescription is the explicit player-domain alias.
func (s State) ApplyPlayerDescription(proposal DescriptionProposal) (State, DescriptionResult, error) {
	return s.ApplyDescription(proposal)
}

// SetDescription is the one-call candidate/apply boundary for non-session
// callers. Session commands should use PlanDescription/ApplyDescription so
// the receipt reducer retains the proposal boundary explicitly.
func (s State) SetDescription(actorID, text string) (State, DescriptionResult, error) {
	proposal, err := s.PlanDescription(actorID, text)
	if err != nil {
		return State{}, DescriptionResult{}, err
	}
	return s.ApplyDescription(proposal)
}
