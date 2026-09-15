package world

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"
)

const DMActiveHeaderResponse = "현재 활동중인 괴물\n\n이름:\n"

var (
	ErrDMActiveActorAbsent        = errors.New("online canonical DM active actor absent")
	ErrDMActiveStateInvalid       = errors.New("DM active state invalid")
	ErrDMActiveNPCUnresolved      = errors.New("canonical active NPC order unresolved")
	ErrDMActiveIdentityUnresolved = errors.New("canonical active NPC identity unresolved")
	ErrDMActiveInvalidVerb        = errors.New("invalid DM active verb")
	ErrDMActiveStaleProposal      = errors.New("stale DM active proposal")
	ErrDMActiveInvalidProposal    = errors.New("invalid DM active proposal")
)

type DMActiveAction string

const (
	DMActiveUnknown DMActiveAction = "unknown"
	DMActivePrompt  DMActiveAction = "prompt"
	DMActiveList    DMActiveAction = "list"
)

// DMActiveProjection is the canonical read-only projection of update.c:list_act.
// NPC IDs and names retain State.ActiveNPCIDs order; neither room indexes nor
// map iteration are used to construct this view.
type DMActiveProjection struct {
	Action   DMActiveAction `json:"action"`
	ActorID  string         `json:"actor_id"`
	Verb     string         `json:"verb"`
	NPCIDs   []string       `json:"npc_ids"`
	NPCNames []string       `json:"npc_names"`
	Response string         `json:"response"`
	Changed  bool           `json:"changed"`
}

// DMActiveResult is the receipt-facing name for DMActiveProjection.
type DMActiveResult = DMActiveProjection

// ActiveNPCProjection and ActiveNPCResult keep the projection discoverable to
// callers that name the underlying world index rather than the C handler.
type ActiveNPCProjection = DMActiveProjection
type ActiveNPCResult = DMActiveProjection

// DMActiveProposal binds one list_act read to the snapshot from which it was
// rendered. ApplyDMActive rechecks the projection before returning the same
// state, making the reducer read-only while still providing a stale boundary.
type DMActiveProposal struct {
	Action   DMActiveAction
	ActorID  string
	Verb     string
	NPCIDs   []string
	NPCNames []string
	Response string
	Changed  bool
}

func DMActiveUnknownResponse(verb string) string {
	return fmt.Sprintf("\"%s\": 이런 명령어는 없네요.", verb)
}

func dmActiveVerb(verbs []string) (string, error) {
	if len(verbs) > 1 {
		return "", ErrDMActiveInvalidVerb
	}
	if len(verbs) == 0 {
		return "*active", nil
	}
	return verbs[0], nil
}

func dmActiveVerbOK(verb string) bool {
	return verb == "*active" || verb == "*활성"
}

func validDMActiveName(name string) bool {
	if name == "" || !utf8.ValidString(name) || strings.TrimSpace(name) != name {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' || (unicode.IsSpace(r) && r != ' ') {
			return false
		}
	}
	return true
}

func dmActiveActor(s State, actorID string) (PlayerState, error) {
	if err := s.Validate(); err != nil {
		return PlayerState{}, fmt.Errorf("%w: %v", ErrDMActiveStateInvalid, err)
	}
	actor, ok := s.Players[actorID]
	if actorID == "" || !ok || !actor.Online || actor.Body.Type != 0 || !validDMActiveName(actor.Body.Name) {
		return PlayerState{}, ErrDMActiveActorAbsent
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok || room.Resource.ID != actor.Body.RoomID || !containsString(room.PlayerIDs, actorID) {
		return PlayerState{}, ErrDMActiveActorAbsent
	}
	return actor, nil
}

func dmActiveProjection(s State, actorID, verb string) (DMActiveProjection, error) {
	if s.ActiveNPCIDs == nil || s.NPCs == nil {
		return DMActiveProjection{}, ErrDMActiveNPCUnresolved
	}
	ids := append([]string{}, s.ActiveNPCIDs...)
	names := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id == "" {
			return DMActiveProjection{}, ErrDMActiveIdentityUnresolved
		}
		if _, duplicate := seen[id]; duplicate {
			return DMActiveProjection{}, ErrDMActiveIdentityUnresolved
		}
		seen[id] = struct{}{}
		npc, ok := s.NPCs[id]
		if !ok || npc.Body.Type != 1 || !validDMActiveName(npc.Body.Name) {
			return DMActiveProjection{}, ErrDMActiveIdentityUnresolved
		}
		names = append(names, npc.Body.Name)
	}
	var out strings.Builder
	out.WriteString(DMActiveHeaderResponse)
	for _, name := range names {
		fmt.Fprintf(&out, "   %s.\n", name)
	}
	return DMActiveProjection{
		Action: DMActiveList, ActorID: actorID, Verb: verb, NPCIDs: ids, NPCNames: names,
		Response: out.String(), Changed: false,
	}, nil
}

func dmActiveProposalProjection(p DMActiveProposal) DMActiveProjection {
	return DMActiveProjection{
		Action: p.Action, ActorID: p.ActorID, Verb: p.Verb,
		NPCIDs: append([]string{}, p.NPCIDs...), NPCNames: append([]string{}, p.NPCNames...),
		Response: p.Response, Changed: p.Changed,
	}
}

func dmActiveProposalFromProjection(p DMActiveProjection, verb string) DMActiveProposal {
	return DMActiveProposal{
		Action: p.Action, ActorID: p.ActorID, Verb: verb,
		NPCIDs: append([]string{}, p.NPCIDs...), NPCNames: append([]string{}, p.NPCNames...),
		Response: p.Response, Changed: p.Changed,
	}
}

// PlanDMActive is update.c:list_act. Only DM-or-higher actors can obtain the
// active list. Mortal and caretaker/sub-DM callers receive deterministic,
// unchanged responses matching the existing DM command style.
func (s State) PlanDMActive(actorID string, verbs ...string) (DMActiveProposal, error) {
	verb, err := dmActiveVerb(verbs)
	if err != nil {
		return DMActiveProposal{}, err
	}
	actor, err := dmActiveActor(s, actorID)
	if err != nil {
		return DMActiveProposal{}, err
	}
	if !dmActiveVerbOK(verb) {
		return DMActiveProposal{}, ErrDMActiveInvalidVerb
	}
	if actor.Body.Class < playerDMClass {
		action := DMActiveUnknown
		response := DMActiveUnknownResponse(verb)
		if actor.Body.Class >= playerCaretakerClass {
			action = DMActivePrompt
			response = ""
		}
		return DMActiveProposal{
			Action: action, ActorID: actorID, Verb: verb,
			NPCIDs: []string{}, NPCNames: []string{}, Response: response, Changed: false,
		}, nil
	}
	projection, err := dmActiveProjection(s, actorID, verb)
	if err != nil {
		return DMActiveProposal{}, err
	}
	return dmActiveProposalFromProjection(projection, verb), nil
}

// ApplyDMActive validates and returns the unchanged snapshot for a list_act
// proposal. The fresh projection check prevents a response from being reused
// after the ordered active identity or actor authorization changed.
func (s State) ApplyDMActive(proposal DMActiveProposal) (State, DMActiveResult, error) {
	if proposal.ActorID == "" || proposal.Verb == "" || proposal.Changed ||
		!dmActiveVerbOK(proposal.Verb) {
		return State{}, DMActiveResult{}, ErrDMActiveInvalidProposal
	}
	fresh, err := s.PlanDMActive(proposal.ActorID, proposal.Verb)
	if err != nil {
		return State{}, DMActiveResult{}, err
	}
	if fresh.Action != proposal.Action || fresh.ActorID != proposal.ActorID || fresh.Verb != proposal.Verb ||
		!reflect.DeepEqual(fresh.NPCIDs, proposal.NPCIDs) ||
		!reflect.DeepEqual(fresh.NPCNames, proposal.NPCNames) || fresh.Response != proposal.Response ||
		fresh.Changed != proposal.Changed {
		return State{}, DMActiveResult{}, ErrDMActiveStaleProposal
	}
	return s, dmActiveProposalProjection(fresh), nil
}

// ProjectDMActive exposes the canonical read-only projection without requiring
// callers to know the proposal/apply vocabulary. The optional verb supports
// both exact aliases while retaining a convenient actor-only form.
func (s State) ProjectDMActive(actorID string, verbs ...string) (DMActiveProjection, error) {
	proposal, err := s.PlanDMActive(actorID, verbs...)
	if err != nil {
		return DMActiveProjection{}, err
	}
	return dmActiveProposalProjection(proposal), nil
}

// ActiveNPCList renders the source-compatible response-only projection.
func (s State) ActiveNPCList(actorID string, verbs ...string) (string, error) {
	projection, err := s.ProjectDMActive(actorID, verbs...)
	if err != nil {
		return "", err
	}
	return projection.Response, nil
}
