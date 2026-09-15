package world

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	DMCharmPromptResponse = "누구의 최면자를 봅니까?\n"
	DMCharmHeaderFormat   = "%s의 피최면자:\n"
)

var (
	ErrDMCharmActorAbsent         = errors.New("online canonical DM charm actor absent")
	ErrDMCharmStateInvalid        = errors.New("DM charm state invalid")
	ErrDMCharmRelationsUnresolved = errors.New("canonical charm relations unresolved")
	ErrDMCharmIdentityUnresolved  = errors.New("canonical charm identity unresolved")
	ErrDMCharmTargetRequired      = errors.New("DM charm target required")
	ErrDMCharmTargetAbsent        = errors.New("canonical global player target absent")
	ErrDMCharmTargetAmbiguous     = errors.New("canonical global player target ambiguous")
	ErrDMCharmTargetNameInvalid   = errors.New("invalid DM charm target name")
	ErrDMCharmInvalidVerb         = errors.New("invalid DM charm verb")
	ErrDMCharmStaleProposal       = errors.New("stale DM charm proposal")
	ErrDMCharmInvalidProposal     = errors.New("invalid DM charm proposal")
)

type DMCharmAction string

const (
	DMCharmUnknown DMCharmAction = "unknown"
	DMCharmList    DMCharmAction = "list"
)

// DMCharmProjection is the canonical read-only projection of
// dm6.c:list_charm. The target is resolved globally from ordered room player
// membership, while CharmRefs and their names retain the relation's stored
// order. Map iteration, legacy descriptor pointers, and room NPC lookup are
// never used to construct this view.
type DMCharmProjection struct {
	Action       DMCharmAction `json:"action"`
	ActorID      string        `json:"actor_id"`
	Verb         string        `json:"verb"`
	TargetID     string        `json:"target_id,omitempty"`
	TargetName   string        `json:"target_name,omitempty"`
	CharmerIDs   []string      `json:"charmer_ids"`
	CharmerNames []string      `json:"charmer_names"`
	Response     string        `json:"response"`
	Changed      bool          `json:"changed"`
}

type DMCharmResult = DMCharmProjection

// DMCharmProposal binds one list_charm projection to one source snapshot.
// ApplyDMCharm rechecks the projection and returns the same snapshot.
type DMCharmProposal struct {
	Action       DMCharmAction
	ActorID      string
	Verb         string
	TargetID     string
	TargetName   string
	CharmerIDs   []string
	CharmerNames []string
	Response     string
	Changed      bool
}

func DMCharmUnknownResponse(verb string) string {
	return fmt.Sprintf("\"%s\": 이런 명령어는 없네요.", verb)
}

func dmCharmVerbOK(verb string) bool {
	return verb == "*charm" || verb == "*최면"
}

func validDMCharmName(name string) bool {
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

func validDMCharmTargetName(name string) bool {
	if !validDMCharmName(name) {
		return false
	}
	return strings.IndexFunc(name, unicode.IsSpace) < 0
}

// validateCharmRelations admits only immutable canonical identities. A
// charmer may belong to at most one owner's ordered list; this reverse
// uniqueness check is the reciprocal edge guard for the one-owner C list.
func (s State) validateCharmRelations() error {
	owners := make(map[EntityRef]string)
	for ownerID, owner := range s.Players {
		if owner.CharmRefs == nil {
			continue
		}
		seen := make(map[EntityRef]struct{}, len(owner.CharmRefs))
		for _, ref := range owner.CharmRefs {
			if ref.ID == "" {
				return fmt.Errorf("invalid charm identity")
			}
			if _, duplicate := seen[ref]; duplicate {
				return fmt.Errorf("duplicate charm identity")
			}
			seen[ref] = struct{}{}
			if previousOwner, duplicate := owners[ref]; duplicate && previousOwner != ownerID {
				return fmt.Errorf("charm identity has multiple owners")
			}
			owners[ref] = ownerID
			switch ref.Kind {
			case "player":
				charmer, ok := s.Players[ref.ID]
				if !ok || charmer.Body.Type != 0 || !validDMCharmName(charmer.Body.Name) {
					return fmt.Errorf("invalid charm player identity")
				}
			case "npc":
				if s.NPCs == nil {
					return fmt.Errorf("NPC charm identity without canonical NPC state")
				}
				charmer, ok := s.NPCs[ref.ID]
				if !ok || charmer.Body.Type != 1 || !validDMCharmName(charmer.Body.Name) {
					return fmt.Errorf("invalid charm NPC identity")
				}
			default:
				return fmt.Errorf("unknown charm identity kind")
			}
		}
	}
	return nil
}

func dmCharmActor(s State, actorID string) (PlayerState, error) {
	if err := s.Validate(); err != nil {
		return PlayerState{}, fmt.Errorf("%w: %v", ErrDMCharmStateInvalid, err)
	}
	actor, ok := s.Players[actorID]
	if actorID == "" || !ok || !actor.Online || actor.Body.Type != 0 || !validDMCharmName(actor.Body.Name) {
		return PlayerState{}, ErrDMCharmActorAbsent
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok || room.Resource.ID != actor.Body.RoomID || !containsString(room.PlayerIDs, actorID) {
		return PlayerState{}, ErrDMCharmActorAbsent
	}
	return actor, nil
}

// findDMCharmPlayer mirrors C find_who against canonical online players. Room
// IDs are sorted only to make a duplicate-name failure deterministic; the
// relation projection itself never depends on this traversal order.
func (s State) findDMCharmPlayer(name string) (string, PlayerState, error) {
	roomIDs := make([]int, 0, len(s.Rooms))
	for roomID := range s.Rooms {
		roomIDs = append(roomIDs, int(roomID))
	}
	sort.Ints(roomIDs)
	var foundID string
	var found PlayerState
	for _, roomKey := range roomIDs {
		roomID := int16(roomKey)
		room := s.Rooms[roomID]
		for _, playerID := range room.PlayerIDs {
			player, ok := s.Players[playerID]
			if !ok || playerID == "" || !player.Online || player.Body.Type != 0 || player.Body.RoomID != roomID || !validDMCharmName(player.Body.Name) {
				return "", PlayerState{}, ErrDMCharmIdentityUnresolved
			}
			if !strings.EqualFold(player.Body.Name, name) {
				continue
			}
			if foundID != "" {
				return "", PlayerState{}, ErrDMCharmTargetAmbiguous
			}
			foundID, found = playerID, player
		}
	}
	if foundID == "" {
		return "", PlayerState{}, ErrDMCharmTargetAbsent
	}
	return foundID, found, nil
}

func (s State) dmCharmNames(refs []EntityRef) ([]string, []string, error) {
	if refs == nil {
		return nil, nil, ErrDMCharmRelationsUnresolved
	}
	ids := make([]string, 0, len(refs))
	names := make([]string, 0, len(refs))
	seen := make(map[EntityRef]struct{}, len(refs))
	for _, ref := range refs {
		if ref.ID == "" {
			return nil, nil, ErrDMCharmIdentityUnresolved
		}
		if _, duplicate := seen[ref]; duplicate {
			return nil, nil, ErrDMCharmIdentityUnresolved
		}
		seen[ref] = struct{}{}
		var name string
		switch ref.Kind {
		case "player":
			player, ok := s.Players[ref.ID]
			if !ok || !player.Online || player.Body.Type != 0 {
				return nil, nil, ErrDMCharmIdentityUnresolved
			}
			name = player.Body.Name
		case "npc":
			if s.NPCs == nil {
				return nil, nil, ErrDMCharmIdentityUnresolved
			}
			npc, ok := s.NPCs[ref.ID]
			if !ok || npc.Body.Type != 1 {
				return nil, nil, ErrDMCharmIdentityUnresolved
			}
			name = npc.Body.Name
		default:
			return nil, nil, ErrDMCharmIdentityUnresolved
		}
		if !validDMCharmName(name) {
			return nil, nil, ErrDMCharmIdentityUnresolved
		}
		ids = append(ids, ref.ID)
		names = append(names, name)
	}
	return ids, names, nil
}

func dmCharmResponse(targetName string, charmerNames []string) string {
	var out strings.Builder
	fmt.Fprintf(&out, DMCharmHeaderFormat, targetName)
	if len(charmerNames) == 0 {
		out.WriteString("없음.\n")
		return out.String()
	}
	for _, name := range charmerNames {
		fmt.Fprintf(&out, "%s.\n", name)
	}
	return out.String()
}

// PlanDMCharm is dm6.c:list_charm. Only SUB_DM-or-higher actors may inspect;
// all other callers receive an opaque unchanged result without target lookup.
func (s State) PlanDMCharm(actorID, verb, targetName string) (DMCharmProposal, error) {
	if !dmCharmVerbOK(verb) {
		return DMCharmProposal{}, ErrDMCharmInvalidVerb
	}
	actor, err := dmCharmActor(s, actorID)
	if err != nil {
		return DMCharmProposal{}, err
	}
	if actor.Body.Class < playerSubDMClass {
		return DMCharmProposal{
			Action: DMCharmUnknown, ActorID: actorID, Verb: verb,
			CharmerIDs: []string{}, CharmerNames: []string{}, Response: DMCharmUnknownResponse(verb), Changed: false,
		}, nil
	}
	if targetName == "" {
		return DMCharmProposal{}, ErrDMCharmTargetRequired
	}
	if !validDMCharmTargetName(targetName) {
		return DMCharmProposal{}, ErrDMCharmTargetNameInvalid
	}
	targetID, target, err := s.findDMCharmPlayer(targetName)
	if err != nil {
		return DMCharmProposal{}, err
	}
	charmerIDs, charmerNames, err := s.dmCharmNames(target.CharmRefs)
	if err != nil {
		return DMCharmProposal{}, err
	}
	return DMCharmProposal{
		Action: DMCharmList, ActorID: actorID, Verb: verb,
		TargetID: targetID, TargetName: target.Body.Name,
		CharmerIDs: charmerIDs, CharmerNames: charmerNames,
		Response: dmCharmResponse(target.Body.Name, charmerNames), Changed: false,
	}, nil
}

func dmCharmProjection(p DMCharmProposal) DMCharmProjection {
	return DMCharmProjection{
		Action: p.Action, ActorID: p.ActorID, Verb: p.Verb,
		TargetID: p.TargetID, TargetName: p.TargetName,
		CharmerIDs: append([]string{}, p.CharmerIDs...), CharmerNames: append([]string{}, p.CharmerNames...),
		Response: p.Response, Changed: p.Changed,
	}
}

// ApplyDMCharm validates a snapshot-bound list_charm projection and returns
// the exact input state. No relation, player, or NPC state is mutated.
func (s State) ApplyDMCharm(proposal DMCharmProposal) (State, DMCharmResult, error) {
	if proposal.ActorID == "" || proposal.Verb == "" || proposal.Changed || !dmCharmVerbOK(proposal.Verb) {
		return State{}, DMCharmResult{}, ErrDMCharmInvalidProposal
	}
	if proposal.Action == DMCharmUnknown {
		if proposal.TargetID != "" || proposal.TargetName != "" || len(proposal.CharmerIDs) != 0 || len(proposal.CharmerNames) != 0 {
			return State{}, DMCharmResult{}, ErrDMCharmInvalidProposal
		}
	} else if proposal.Action == DMCharmList {
		if proposal.TargetID == "" || proposal.TargetName == "" {
			return State{}, DMCharmResult{}, ErrDMCharmInvalidProposal
		}
	} else {
		return State{}, DMCharmResult{}, ErrDMCharmInvalidProposal
	}
	requestedTarget := proposal.TargetName
	if proposal.Action == DMCharmUnknown {
		requestedTarget = "requested-target-redacted"
	}
	fresh, err := s.PlanDMCharm(proposal.ActorID, proposal.Verb, requestedTarget)
	if err != nil {
		return State{}, DMCharmResult{}, err
	}
	if fresh.Action != proposal.Action || fresh.ActorID != proposal.ActorID || fresh.Verb != proposal.Verb ||
		fresh.TargetID != proposal.TargetID || fresh.TargetName != proposal.TargetName ||
		!reflect.DeepEqual(fresh.CharmerIDs, proposal.CharmerIDs) || !reflect.DeepEqual(fresh.CharmerNames, proposal.CharmerNames) ||
		fresh.Response != proposal.Response || fresh.Changed != proposal.Changed {
		return State{}, DMCharmResult{}, ErrDMCharmStaleProposal
	}
	return s, dmCharmProjection(fresh), nil
}

func (s State) ProjectDMCharm(actorID, verb, targetName string) (DMCharmProjection, error) {
	proposal, err := s.PlanDMCharm(actorID, verb, targetName)
	if err != nil {
		return DMCharmProjection{}, err
	}
	return dmCharmProjection(proposal), nil
}

func (s State) CharmList(actorID, verb, targetName string) (string, error) {
	projection, err := s.ProjectDMCharm(actorID, verb, targetName)
	if err != nil {
		return "", err
	}
	return projection.Response, nil
}
