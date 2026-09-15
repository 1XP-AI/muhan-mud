package world

import (
	"errors"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"
)

// suicide is the bounded Go port of command5.c:ply_suicide / suicide
// (cmdlist-153 `목매달기`). This slice starts C param 1→2 (password prompt
// and RETURN wait-state) and admits param 2 mismatch: F_CLR(PREADI),
// typed reject, RETURN(command, 1). Param 2 match / param 3 `찐짜로`
// confirm, broadcast_all, disconnect, and player/alias/bank `mv` stay
// unadmitted.
const (
	suicideReadingFlag   uint = 29 // PREADI
	suicidePasswordPhase      = 2  // suicide RETURN param after case 1
	SuicideReadingFlag        = suicideReadingFlag
	SuicidePasswordPhase      = suicidePasswordPhase
)

type SuicideAction string

const (
	SuicidePrompt SuicideAction = "prompt"
	SuicideReject SuicideAction = "reject"
)

const (
	SuicidePromptResponse = "당신에 관한 데이터를 완전히 삭제합니다.\n당신의 현재 암호를 넣어주십시요 : "
	SuicideRejectResponse = "암호가 틀립니다.\n삭제되지 않았습니다."
)

var (
	ErrSuicideActorAbsent       = errors.New("online canonical suicide actor absent")
	ErrSuicideStaleProposal     = errors.New("stale or invalid suicide proposal")
	ErrSuicideInvalidProposal   = errors.New("invalid suicide proposal")
	ErrSuicideNameInvalid       = errors.New("suicide actor name is unsafe")
	ErrSuicideNotReading        = errors.New("suicide password continuation is not active")
	ErrSuicideConfirmUnmigrated = errors.New("suicide confirm/archive is unmigrated")
)

type SuicideResult struct {
	Action       SuicideAction `json:"action"`
	ActorID      string        `json:"actor_id"`
	RoomID       int16         `json:"room_id"`
	Response     string        `json:"response"`
	Changed      bool          `json:"changed"`
	Continuation int           `json:"continuation,omitempty"`
	Reading      bool          `json:"reading,omitempty"`
}

type SuicideProposal struct {
	Action          SuicideAction
	ActorID         string
	RoomID          int16
	Response        string
	Changed         bool
	Continuation    int
	BeforeFlags     [8]byte
	AfterFlags      [8]byte
	PasswordMatched bool

	expectedActor PlayerState
}

func suicideFlag(flags [8]byte, bit uint) bool {
	return flag(flags[:], bit)
}

func suicideWithFlag(flags [8]byte, bit uint, on bool) [8]byte {
	idx := bit / 8
	if idx >= uint(len(flags)) {
		return flags
	}
	if on {
		flags[idx] |= 1 << (bit % 8)
	} else {
		flags[idx] &^= 1 << (bit % 8)
	}
	return flags
}

func validSuicideActorName(name string) bool {
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

func suicideActor(s State, actorID string) (PlayerState, error) {
	if err := s.Validate(); err != nil {
		if actor, ok := s.Players[actorID]; actorID != "" && ok {
			if _, roomOK := s.Rooms[actor.Body.RoomID]; !roomOK {
				return PlayerState{}, ErrSuicideActorAbsent
			}
		}
		return PlayerState{}, err
	}
	actor, ok := s.Players[actorID]
	if actorID == "" || !ok || !actor.Online || actor.Body.Type != 0 {
		return PlayerState{}, ErrSuicideActorAbsent
	}
	if !validSuicideActorName(actor.Body.Name) {
		return PlayerState{}, ErrSuicideNameInvalid
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok || room.Resource.ID != actor.Body.RoomID || !containsString(room.PlayerIDs, actorID) {
		return PlayerState{}, ErrSuicideActorAbsent
	}
	return actor, nil
}

func suicideResult(p SuicideProposal) SuicideResult {
	return SuicideResult{
		Action: p.Action, ActorID: p.ActorID, RoomID: p.RoomID, Response: p.Response,
		Changed: p.Changed, Continuation: p.Continuation,
		Reading: suicideFlag(p.AfterFlags, suicideReadingFlag),
	}
}

func suicideProposalMatches(actual, expected SuicideProposal) bool {
	return actual.Action == expected.Action && actual.ActorID == expected.ActorID &&
		actual.RoomID == expected.RoomID && actual.Response == expected.Response &&
		actual.Changed == expected.Changed && actual.Continuation == expected.Continuation &&
		actual.PasswordMatched == expected.PasswordMatched &&
		actual.BeforeFlags == expected.BeforeFlags && actual.AfterFlags == expected.AfterFlags &&
		reflect.DeepEqual(actual.expectedActor, expected.expectedActor)
}

func cloneSuicideActor(actor PlayerState) PlayerState {
	actor.Aliases = clonePlayerAliases(actor.Aliases)
	actor.PlayerEnemies = append([]string(nil), actor.PlayerEnemies...)
	actor.FollowerIDs = append([]string(nil), actor.FollowerIDs...)
	if actor.NPCFollowerIDs != nil {
		actor.NPCFollowerIDs = append([]string(nil), actor.NPCFollowerIDs...)
	}
	if actor.FollowerRefs != nil {
		actor.FollowerRefs = append([]EntityRef(nil), actor.FollowerRefs...)
	}
	actor.Body.Inventory = cloneObjects(actor.Body.Inventory)
	if actor.Items != nil {
		items := actor.Items.clone()
		actor.Items = &items
	}
	return actor
}

// PlanSuicide is command5.c:suicide case 1. The original prints the
// delete warning and password prompt, F_SET(PREADI), and RETURN(suicide, 2).
// Param 2 mismatch is PlanSuicidePassword; match/confirm/archive stay
// fail-closed.
func (s State) PlanSuicide(actorID string) (SuicideProposal, error) {
	actor, err := suicideActor(s, actorID)
	if err != nil {
		return SuicideProposal{}, err
	}
	after := suicideWithFlag(actor.Body.Flags, suicideReadingFlag, true)
	return SuicideProposal{
		Action: SuicidePrompt, ActorID: actorID, RoomID: actor.Body.RoomID,
		Response: SuicidePromptResponse, Changed: true, Continuation: suicidePasswordPhase,
		BeforeFlags: actor.Body.Flags, AfterFlags: after,
		expectedActor: cloneSuicideActor(actor),
	}, nil
}

// ApplySuicide atomically writes PREADI for the password prompt. This
// slice never removes the actor, archives files, or consumes a password.
func (s State) ApplySuicide(p SuicideProposal) (State, SuicideResult, error) {
	if p.ActorID == "" {
		return State{}, SuicideResult{}, ErrSuicideInvalidProposal
	}
	expected, err := s.PlanSuicide(p.ActorID)
	if err != nil {
		return State{}, SuicideResult{}, err
	}
	if !suicideProposalMatches(p, expected) {
		return State{}, SuicideResult{}, ErrSuicideStaleProposal
	}
	if !p.Changed || p.Action != SuicidePrompt || p.Continuation != suicidePasswordPhase || p.Response != SuicidePromptResponse {
		return State{}, SuicideResult{}, ErrSuicideInvalidProposal
	}
	next := s.clone()
	actor := next.Players[p.ActorID]
	actor.Body.Flags = p.AfterFlags
	next.Players[p.ActorID] = actor
	if err := next.Validate(); err != nil {
		return State{}, SuicideResult{}, err
	}
	return next, suicideResult(p), nil
}

// Suicide is the reducer-shaped convenience API for the 목매달기 start.
func (s State) Suicide(actorID string) (State, SuicideResult, error) {
	p, err := s.PlanSuicide(actorID)
	if err != nil {
		return State{}, SuicideResult{}, err
	}
	return s.ApplySuicide(p)
}

// PlanSuicidePassword is command5.c:suicide case 2. C F_CLR(PREADI) then
// strcmp. A mismatch prints the typed reject and RETURN(command, 1) without
// deleting files. A match would RETURN(suicide, 3); that confirm/archive
// path stays fail-closed.
func (s State) PlanSuicidePassword(actorID string, matched bool) (SuicideProposal, error) {
	actor, err := suicideActor(s, actorID)
	if err != nil {
		return SuicideProposal{}, err
	}
	if !suicideFlag(actor.Body.Flags, suicideReadingFlag) {
		return SuicideProposal{}, ErrSuicideNotReading
	}
	if matched {
		return SuicideProposal{}, ErrSuicideConfirmUnmigrated
	}
	after := suicideWithFlag(actor.Body.Flags, suicideReadingFlag, false)
	return SuicideProposal{
		Action: SuicideReject, ActorID: actorID, RoomID: actor.Body.RoomID,
		Response: SuicideRejectResponse, Changed: true,
		BeforeFlags: actor.Body.Flags, AfterFlags: after,
		expectedActor: cloneSuicideActor(actor),
	}, nil
}

// ApplySuicidePassword atomically clears PREADI for a typed password
// mismatch. It never removes the actor, archives files, or admits param 3.
func (s State) ApplySuicidePassword(p SuicideProposal) (State, SuicideResult, error) {
	if p.ActorID == "" {
		return State{}, SuicideResult{}, ErrSuicideInvalidProposal
	}
	expected, err := s.PlanSuicidePassword(p.ActorID, p.PasswordMatched)
	if err != nil {
		return State{}, SuicideResult{}, err
	}
	if !suicideProposalMatches(p, expected) {
		return State{}, SuicideResult{}, ErrSuicideStaleProposal
	}
	if !p.Changed || p.Action != SuicideReject || p.Continuation != 0 || p.PasswordMatched || p.Response != SuicideRejectResponse {
		return State{}, SuicideResult{}, ErrSuicideInvalidProposal
	}
	next := s.clone()
	actor := next.Players[p.ActorID]
	actor.Body.Flags = p.AfterFlags
	next.Players[p.ActorID] = actor
	if err := next.Validate(); err != nil {
		return State{}, SuicideResult{}, err
	}
	return next, suicideResult(p), nil
}

// SuicidePassword is the reducer-shaped convenience API for suicide case 2.
func (s State) SuicidePassword(actorID string, matched bool) (State, SuicideResult, error) {
	p, err := s.PlanSuicidePassword(actorID, matched)
	if err != nil {
		return State{}, SuicideResult{}, err
	}
	return s.ApplySuicidePassword(p)
}
