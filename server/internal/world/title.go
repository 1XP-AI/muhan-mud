package world

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxPlayerTitleBytes matches alias.c:set_title's fixed 78-byte acceptance
// boundary. The canonical field stores validated UTF-8 rather than copying
// into the legacy fixed buffer.
const MaxPlayerTitleBytes = 78

var (
	ErrTitleActorAbsent = errors.New("online title actor absent")
	ErrTitleRequired    = errors.New("title is required")
	ErrTitleTooLong     = errors.New("title exceeds the byte limit")
)

type TitleAction string

const (
	TitleView  TitleAction = "view"
	TitleSet   TitleAction = "set"
	TitleClear TitleAction = "clear"
)

type TitleEvent struct {
	ActorID string `json:"actor_id"`
	Text    string `json:"text"`
}

type TitleResult struct {
	Action        TitleAction `json:"action"`
	ActorID       string      `json:"actor_id"`
	ActorName     string      `json:"actor_name"`
	Title         string      `json:"title"`
	PreviousTitle string      `json:"previous_title,omitempty"`
	Changed       bool        `json:"changed"`
	Response      string      `json:"response"`
	Event         *TitleEvent `json:"event,omitempty"`
}

type TitleProposal struct {
	Action         TitleAction
	ActorID        string
	NewTitle       string
	expectedBody   LegacyMonster
	expectedTitle  string
	expectedOnline bool
}

func ValidatePlayerTitle(title string) error {
	if title == "" {
		return ErrTitleRequired
	}
	if !utf8.ValidString(title) {
		return ErrTitleRequired
	}
	if len(title) > MaxPlayerTitleBytes {
		return ErrTitleTooLong
	}
	if strings.TrimSpace(title) != title {
		return ErrTitleRequired
	}
	for _, r := range title {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' || r == '\r' || r == '\n' {
			return ErrTitleRequired
		}
	}
	return nil
}

func titleActor(s State, actorID string) (PlayerState, error) {
	if err := s.Validate(); err != nil {
		return PlayerState{}, err
	}
	player, ok := s.Players[actorID]
	if !ok || !player.Online || player.Body.Type != 0 || player.Body.Name == "" {
		return PlayerState{}, ErrTitleActorAbsent
	}
	return player, nil
}

func titleResponse(player PlayerState) string {
	if player.Title == "" {
		return fmt.Sprintf("%s님은 설정된 칭호가 없습니다.\r\n", player.Body.Name)
	}
	return fmt.Sprintf("%s님의 칭호는 %s입니다.\r\n", player.Body.Name, player.Title)
}

func (s State) ViewTitle(actorID string) (TitleResult, error) {
	player, err := titleActor(s, actorID)
	if err != nil {
		return TitleResult{}, err
	}
	return TitleResult{Action: TitleView, ActorID: actorID, ActorName: player.Body.Name, Title: player.Title, Response: titleResponse(player)}, nil
}

func (s State) PlanSetTitle(actorID, title string) (TitleProposal, error) {
	player, err := titleActor(s, actorID)
	if err != nil {
		return TitleProposal{}, err
	}
	if err := ValidatePlayerTitle(title); err != nil {
		return TitleProposal{}, err
	}
	return TitleProposal{Action: TitleSet, ActorID: actorID, NewTitle: title, expectedBody: player.Body, expectedTitle: player.Title, expectedOnline: player.Online}, nil
}

func (s State) PlanClearTitle(actorID string) (TitleProposal, error) {
	player, err := titleActor(s, actorID)
	if err != nil {
		return TitleProposal{}, err
	}
	return TitleProposal{Action: TitleClear, ActorID: actorID, expectedBody: player.Body, expectedTitle: player.Title, expectedOnline: player.Online}, nil
}

func (s State) ApplyTitle(proposal TitleProposal) (State, TitleResult, error) {
	player, err := titleActor(s, proposal.ActorID)
	if err != nil {
		return State{}, TitleResult{}, err
	}
	if proposal.Action != TitleSet && proposal.Action != TitleClear {
		return State{}, TitleResult{}, fmt.Errorf("unsupported title proposal")
	}
	if proposal.ActorID == "" || proposal.expectedOnline != player.Online || proposal.expectedTitle != player.Title || !reflect.DeepEqual(proposal.expectedBody, player.Body) {
		return State{}, TitleResult{}, fmt.Errorf("stale title proposal")
	}
	if proposal.Action == TitleSet {
		if err := ValidatePlayerTitle(proposal.NewTitle); err != nil {
			return State{}, TitleResult{}, err
		}
	} else if proposal.NewTitle != "" {
		return State{}, TitleResult{}, fmt.Errorf("clear title proposal contains title")
	}
	next := s.clone()
	nextPlayer := next.Players[proposal.ActorID]
	previous := nextPlayer.Title
	if proposal.Action == TitleSet {
		nextPlayer.Title = proposal.NewTitle
	} else {
		nextPlayer.Title = ""
	}
	next.Players[proposal.ActorID] = nextPlayer
	if err := next.Validate(); err != nil {
		return State{}, TitleResult{}, err
	}
	changed := previous != nextPlayer.Title
	result := TitleResult{Action: proposal.Action, ActorID: proposal.ActorID, ActorName: nextPlayer.Body.Name, Title: nextPlayer.Title, PreviousTitle: previous, Changed: changed}
	if proposal.Action == TitleSet {
		result.Response = titleResponse(nextPlayer)
	} else if changed {
		result.Response = fmt.Sprintf("%s님의 칭호를 삭제했습니다.\r\n", nextPlayer.Body.Name)
	} else {
		result.Response = fmt.Sprintf("%s님은 설정된 칭호가 없습니다.\r\n", nextPlayer.Body.Name)
	}
	return next, result, nil
}
