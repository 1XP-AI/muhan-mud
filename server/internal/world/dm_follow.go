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
	DMFollowUsageResponse     = "사용법: <괴물> *따르기\n"
	DMFollowMissingResponse   = "그런 괴물이 없습니다.\n"
	DMFollowPermanentResponse = "고정된 괴물입니다.\n"
)

type DMFollowAction string

const (
	DMFollowUnknown   DMFollowAction = "unknown"
	DMFollowPrompt    DMFollowAction = "prompt"
	DMFollowUsage     DMFollowAction = "usage"
	DMFollowMissing   DMFollowAction = "missing"
	DMFollowPermanent DMFollowAction = "permanent"
	DMFollowAttach    DMFollowAction = "attach"
	DMFollowDetach    DMFollowAction = "detach"
)

var (
	ErrDMFollowActorAbsent       = errors.New("online canonical DM follow actor absent")
	ErrDMFollowNPCUnresolved     = errors.New("canonical NPC state required")
	ErrDMFollowInvalidVerb       = errors.New("invalid DM follow verb")
	ErrDMFollowInvalidName       = errors.New("invalid DM follow monster name")
	ErrDMFollowInvalidOccurrence = errors.New("invalid DM follow occurrence")
	ErrDMFollowNotReciprocal     = errors.New("DM follow edge is not reciprocal")
	ErrDMFollowStaleProposal     = errors.New("stale DM follow proposal")
	ErrDMFollowInvalidProposal   = errors.New("invalid DM follow proposal")
)

// DMFollowResult is the durable receipt projection for dm6.c:dm_follow.
type DMFollowResult struct {
	Action     DMFollowAction `json:"action"`
	ActorID    string         `json:"actor_id"`
	NPCID      string         `json:"npc_id,omitempty"`
	NPCName    string         `json:"npc_name,omitempty"`
	Verb       string         `json:"verb,omitempty"`
	Occurrence int            `json:"occurrence,omitempty"`
	Response   string         `json:"response"`
	Changed    bool           `json:"changed"`
}

// DMFollowProposal binds one *따르기 / *cfollow transition to one snapshot.
type DMFollowProposal struct {
	Action     DMFollowAction
	ActorID    string
	NPCID      string
	NPCName    string
	Verb       string
	Occurrence int
	Response   string
	Changed    bool

	expectedActor PlayerState
	expectedNPC   NPCState
}

func DMFollowUnknownResponse(verb string) string {
	return fmt.Sprintf("\"%s\": 이런 명령어는 없네요.", verb)
}

func DMFollowAttachResponse(name string) string {
	return fmt.Sprintf("%s이 당신을 따릅니다.\n", name)
}

func DMFollowDetachResponse(name string) string {
	return fmt.Sprintf("%s이 당신을 그만 따릅니다.\n", name)
}

func dmFollowVerbOK(verb string) bool {
	return verb == "*따르기" || verb == "*cfollow"
}

func dmFollowActor(s State, actorID string) (PlayerState, error) {
	if err := s.Validate(); err != nil {
		return PlayerState{}, err
	}
	actor, ok := s.Players[actorID]
	if actorID == "" || !ok || !actor.Online || actor.Body.Type != 0 || actor.Body.Name == "" {
		return PlayerState{}, ErrDMFollowActorAbsent
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok || room.Resource.ID != actor.Body.RoomID || !containsString(room.PlayerIDs, actorID) {
		return PlayerState{}, ErrDMFollowActorAbsent
	}
	return actor, nil
}

func validDMFollowName(name string) error {
	if name == "" || !utf8.ValidString(name) || strings.TrimSpace(name) != name {
		return ErrDMFollowInvalidName
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' || (unicode.IsSpace(r) && r != ' ') {
			return ErrDMFollowInvalidName
		}
	}
	return nil
}

func (s State) selectDMFollowNPC(actor PlayerState, name string, occurrence int) (string, NPCState, bool, error) {
	if occurrence < 1 {
		return "", NPCState{}, false, ErrDMFollowInvalidOccurrence
	}
	if s.NPCs == nil {
		return "", NPCState{}, false, ErrDMFollowNPCUnresolved
	}
	found := 0
	for _, id := range s.Rooms[actor.Body.RoomID].NPCIDs {
		npc, ok := s.NPCs[id]
		if !ok || id == "" || npc.Body.Type != 1 || npc.Body.RoomID != actor.Body.RoomID {
			return "", NPCState{}, false, ErrDMFollowNPCUnresolved
		}
		if !strings.EqualFold(npc.Body.Name, name) {
			continue
		}
		found++
		if found == occurrence {
			return id, npc, true, nil
		}
	}
	return "", NPCState{}, false, nil
}

func dmFollowUnchanged(action DMFollowAction, actorID, verb, npcName string, occurrence int, response string, actor PlayerState) DMFollowProposal {
	return DMFollowProposal{
		Action: action, ActorID: actorID, NPCName: npcName, Verb: verb, Occurrence: occurrence,
		Response: response, Changed: false, expectedActor: actor,
	}
}

func dmFollowResult(p DMFollowProposal) DMFollowResult {
	return DMFollowResult{
		Action: p.Action, ActorID: p.ActorID, NPCID: p.NPCID, NPCName: p.NPCName,
		Verb: p.Verb, Occurrence: p.Occurrence, Response: p.Response, Changed: p.Changed,
	}
}

func setNPCFlag(body LegacyMonster, bit uint, on bool) LegacyMonster {
	idx := bit / 8
	if idx >= uint(len(body.Flags)) {
		return body
	}
	if on {
		body.Flags[idx] |= 1 << (bit % 8)
	} else {
		body.Flags[idx] &^= 1 << (bit % 8)
	}
	return body
}

// PlanDMFollow is dm6.c:dm_follow. process_cmd hides the alias below
// CARETAKER; the handler itself returns PROMPT below DM. Monster lookup is
// exact same-room identity; prefix matching stays fail-closed.
func (s State) PlanDMFollow(actorID, verb, npcName string, occurrence int) (DMFollowProposal, error) {
	actor, err := dmFollowActor(s, actorID)
	if err != nil {
		return DMFollowProposal{}, err
	}
	if !dmFollowVerbOK(verb) {
		return DMFollowProposal{}, ErrDMFollowInvalidVerb
	}
	if actor.Body.Class < playerCaretakerClass {
		return dmFollowUnchanged(DMFollowUnknown, actorID, verb, npcName, occurrence, DMFollowUnknownResponse(verb), actor), nil
	}
	if actor.Body.Class < playerDMClass {
		return dmFollowUnchanged(DMFollowPrompt, actorID, verb, npcName, occurrence, "", actor), nil
	}
	if npcName == "" {
		return dmFollowUnchanged(DMFollowUsage, actorID, verb, npcName, occurrence, DMFollowUsageResponse, actor), nil
	}
	if err := validDMFollowName(npcName); err != nil {
		return DMFollowProposal{}, err
	}
	if occurrence < 1 {
		return DMFollowProposal{}, ErrDMFollowInvalidOccurrence
	}
	npcID, npc, found, err := s.selectDMFollowNPC(actor, npcName, occurrence)
	if err != nil {
		return DMFollowProposal{}, err
	}
	if !found {
		return dmFollowUnchanged(DMFollowMissing, actorID, verb, npcName, occurrence, DMFollowMissingResponse, actor), nil
	}
	if flag(npc.Body.Flags[:], npcPermanentFlag) {
		return dmFollowUnchanged(DMFollowPermanent, actorID, verb, npc.Body.Name, occurrence, DMFollowPermanentResponse, actor), nil
	}
	if flag(npc.Body.Flags[:], npcDMFollowFlag) {
		if npc.FollowingPlayerID != actorID || !containsString(actor.NPCFollowerIDs, npcID) {
			return DMFollowProposal{}, ErrDMFollowNotReciprocal
		}
		return DMFollowProposal{
			Action: DMFollowDetach, ActorID: actorID, NPCID: npcID, NPCName: npc.Body.Name,
			Verb: verb, Occurrence: occurrence, Response: DMFollowDetachResponse(npc.Body.Name), Changed: true,
			expectedActor: actor, expectedNPC: npc,
		}, nil
	}
	return DMFollowProposal{
		Action: DMFollowAttach, ActorID: actorID, NPCID: npcID, NPCName: npc.Body.Name,
		Verb: verb, Occurrence: occurrence, Response: DMFollowAttachResponse(npc.Body.Name), Changed: true,
		expectedActor: actor, expectedNPC: npc,
	}, nil
}

// ApplyDMFollow toggles MDMFOL and the reciprocal first_fol edge atomically.
func (s State) ApplyDMFollow(proposal DMFollowProposal) (State, DMFollowResult, error) {
	actor, err := dmFollowActor(s, proposal.ActorID)
	if err != nil {
		return State{}, DMFollowResult{}, err
	}
	if !proposal.Changed || (proposal.Action != DMFollowAttach && proposal.Action != DMFollowDetach) {
		return State{}, DMFollowResult{}, ErrDMFollowInvalidProposal
	}
	if !dmFollowVerbOK(proposal.Verb) || proposal.NPCID == "" || proposal.NPCName == "" || proposal.Occurrence < 1 {
		return State{}, DMFollowResult{}, ErrDMFollowInvalidProposal
	}
	npc, ok := s.NPCs[proposal.NPCID]
	if !ok {
		return State{}, DMFollowResult{}, ErrDMFollowStaleProposal
	}
	if !reflect.DeepEqual(actor, proposal.expectedActor) || !reflect.DeepEqual(npc, proposal.expectedNPC) ||
		npc.Body.Name != proposal.NPCName || npc.Body.RoomID != actor.Body.RoomID {
		return State{}, DMFollowResult{}, ErrDMFollowStaleProposal
	}
	switch proposal.Action {
	case DMFollowAttach:
		if flag(npc.Body.Flags[:], npcDMFollowFlag) || flag(npc.Body.Flags[:], npcPermanentFlag) ||
			proposal.Response != DMFollowAttachResponse(npc.Body.Name) {
			return State{}, DMFollowResult{}, ErrDMFollowStaleProposal
		}
		next, err := s.FollowNPCToPlayer(proposal.NPCID, proposal.ActorID)
		if err != nil {
			return State{}, DMFollowResult{}, err
		}
		followed := next.NPCs[proposal.NPCID]
		followed.Body = setNPCFlag(followed.Body, npcDMFollowFlag, true)
		next.NPCs[proposal.NPCID] = followed
		if err := next.Validate(); err != nil {
			return State{}, DMFollowResult{}, err
		}
		return next, dmFollowResult(proposal), nil
	case DMFollowDetach:
		if !flag(npc.Body.Flags[:], npcDMFollowFlag) || npc.FollowingPlayerID != proposal.ActorID ||
			proposal.Response != DMFollowDetachResponse(npc.Body.Name) {
			return State{}, DMFollowResult{}, ErrDMFollowStaleProposal
		}
		next, err := s.UnfollowNPCFromPlayer(proposal.NPCID)
		if err != nil {
			return State{}, DMFollowResult{}, err
		}
		cleared := next.NPCs[proposal.NPCID]
		cleared.Body = setNPCFlag(cleared.Body, npcDMFollowFlag, false)
		next.NPCs[proposal.NPCID] = cleared
		if err := next.Validate(); err != nil {
			return State{}, DMFollowResult{}, err
		}
		return next, dmFollowResult(proposal), nil
	default:
		return State{}, DMFollowResult{}, ErrDMFollowInvalidProposal
	}
}
