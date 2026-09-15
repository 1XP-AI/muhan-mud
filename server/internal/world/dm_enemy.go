package world

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"
)

const DMEnemyHeaderFormat = "%s의 적들:\n"

var (
	ErrDMEnemyActorAbsent         = errors.New("online canonical DM enemy actor absent")
	ErrDMEnemyStateInvalid        = errors.New("DM enemy state invalid")
	ErrDMEnemyNPCUnresolved       = errors.New("canonical same-room NPC order unresolved")
	ErrDMEnemyIdentityUnresolved  = errors.New("canonical enemy identity unresolved")
	ErrDMEnemyRelationsUnresolved = errors.New("canonical enemy relations unresolved")
	ErrDMEnemyTargetRequired      = errors.New("DM enemy target required")
	ErrDMEnemyTargetAbsent        = errors.New("canonical same-room NPC target absent")
	ErrDMEnemyTargetNameInvalid   = errors.New("invalid DM enemy target name")
	ErrDMEnemyInvalidOccurrence   = errors.New("invalid DM enemy occurrence")
	ErrDMEnemyInvalidVerb         = errors.New("invalid DM enemy verb")
	ErrDMEnemyStaleProposal       = errors.New("stale DM enemy proposal")
	ErrDMEnemyInvalidProposal     = errors.New("invalid DM enemy proposal")
)

type DMEnemyAction string

const (
	DMEnemyUnknown DMEnemyAction = "unknown"
	DMEnemyList    DMEnemyAction = "list"
)

// DMEnemyProjection is the canonical read-only projection of dm6.c:list_enm.
// The selected NPC is resolved from the actor's room.NPCIDs order. Enemy IDs
// and names retain the stored NPC.Enemies order; map iteration and legacy room
// monsters are never used.
type DMEnemyProjection struct {
	Action     DMEnemyAction `json:"action"`
	ActorID    string        `json:"actor_id"`
	Verb       string        `json:"verb"`
	Occurrence int           `json:"occurrence"`
	TargetID   string        `json:"target_id,omitempty"`
	TargetName string        `json:"target_name,omitempty"`
	EnemyIDs   []string      `json:"enemy_ids"`
	EnemyNames []string      `json:"enemy_names"`
	Response   string        `json:"response"`
	Changed    bool          `json:"changed"`
}

type DMEnemyResult = DMEnemyProjection

// DMEnemyProposal binds a list_enm projection to its source snapshot. Apply
// rechecks the same projection and returns the input state unchanged.
type DMEnemyProposal struct {
	Action     DMEnemyAction
	ActorID    string
	Verb       string
	Occurrence int
	TargetID   string
	TargetName string
	EnemyIDs   []string
	EnemyNames []string
	Response   string
	Changed    bool
}

func DMEnemyUnknownResponse(verb string) string {
	return fmt.Sprintf("\"%s\": 이런 명령어는 없네요.", verb)
}

func dmEnemyVerbOK(verb string) bool {
	return verb == "*enemy" || verb == "*적"
}

func validDMEnemyName(name string) bool {
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

// dmEnemyActor intentionally validates only the authenticated actor boundary.
// This lets an unauthorized caller receive the deterministic unknown result
// without inspecting target identities or unresolved enemy relations.
func dmEnemyActor(s State, actorID string) (PlayerState, error) {
	if s.Version != 1 || s.Rooms == nil || s.Players == nil {
		return PlayerState{}, ErrDMEnemyActorAbsent
	}
	actor, ok := s.Players[actorID]
	if actorID == "" || !ok || !actor.Online || actor.Body.Type != 0 || !validDMEnemyName(actor.Body.Name) {
		return PlayerState{}, ErrDMEnemyActorAbsent
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok || room.Resource.ID != actor.Body.RoomID || !containsString(room.PlayerIDs, actorID) {
		return PlayerState{}, ErrDMEnemyActorAbsent
	}
	return actor, nil
}

func dmEnemyProposalProjection(p DMEnemyProposal) DMEnemyProjection {
	return DMEnemyProjection{
		Action: p.Action, ActorID: p.ActorID, Verb: p.Verb, Occurrence: p.Occurrence,
		TargetID: p.TargetID, TargetName: p.TargetName,
		EnemyIDs: append([]string{}, p.EnemyIDs...), EnemyNames: append([]string{}, p.EnemyNames...),
		Response: p.Response, Changed: p.Changed,
	}
}

func dmEnemyProposalFromProjection(p DMEnemyProjection) DMEnemyProposal {
	return DMEnemyProposal{
		Action: p.Action, ActorID: p.ActorID, Verb: p.Verb, Occurrence: p.Occurrence,
		TargetID: p.TargetID, TargetName: p.TargetName,
		EnemyIDs: append([]string{}, p.EnemyIDs...), EnemyNames: append([]string{}, p.EnemyNames...),
		Response: p.Response, Changed: p.Changed,
	}
}

func (s State) selectDMEnemyNPC(actor PlayerState, name string, occurrence int) (string, NPCState, error) {
	if s.NPCs == nil {
		return "", NPCState{}, ErrDMEnemyNPCUnresolved
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok || room.NPCIDs == nil {
		return "", NPCState{}, ErrDMEnemyNPCUnresolved
	}
	found := 0
	for _, id := range room.NPCIDs {
		npc, exists := s.NPCs[id]
		if id == "" || !exists || npc.Body.Type != 1 || npc.Body.RoomID != actor.Body.RoomID || !validDMEnemyName(npc.Body.Name) {
			return "", NPCState{}, ErrDMEnemyIdentityUnresolved
		}
		if !strings.EqualFold(npc.Body.Name, name) {
			continue
		}
		found++
		if found == occurrence {
			return id, npc, nil
		}
	}
	return "", NPCState{}, ErrDMEnemyTargetAbsent
}

func (s State) enemyNames(target NPCState) ([]string, []string, error) {
	if target.Enemies == nil {
		return nil, nil, ErrDMEnemyRelationsUnresolved
	}
	ids := make([]string, 0, len(target.Enemies))
	names := make([]string, 0, len(target.Enemies))
	seen := make(map[EntityRef]struct{}, len(target.Enemies))
	for _, relation := range target.Enemies {
		ref := relation.Target
		if ref.ID == "" {
			return nil, nil, ErrDMEnemyIdentityUnresolved
		}
		if _, duplicate := seen[ref]; duplicate {
			return nil, nil, ErrDMEnemyIdentityUnresolved
		}
		seen[ref] = struct{}{}
		var name string
		switch ref.Kind {
		case "player":
			player, ok := s.Players[ref.ID]
			if !ok || player.Body.Type != 0 {
				return nil, nil, ErrDMEnemyIdentityUnresolved
			}
			name = player.Body.Name
		case "npc":
			npc, ok := s.NPCs[ref.ID]
			if !ok || npc.Body.Type != 1 {
				return nil, nil, ErrDMEnemyIdentityUnresolved
			}
			name = npc.Body.Name
		default:
			return nil, nil, ErrDMEnemyIdentityUnresolved
		}
		if !validDMEnemyName(name) {
			return nil, nil, ErrDMEnemyIdentityUnresolved
		}
		ids = append(ids, ref.ID)
		names = append(names, name)
	}
	return ids, names, nil
}

func dmEnemyResponse(targetName string, enemyNames []string) string {
	var out strings.Builder
	fmt.Fprintf(&out, DMEnemyHeaderFormat, targetName)
	if len(enemyNames) == 0 {
		out.WriteString("없음.\n")
		return out.String()
	}
	for _, name := range enemyNames {
		fmt.Fprintf(&out, "%s.\n", name)
	}
	return out.String()
}

// PlanDMEnemy is dm6.c:list_enm. The source gate is SUB_DM-or-higher; all
// lower classes receive an unchanged unknown result without target lookup.
func (s State) PlanDMEnemy(actorID, verb, targetName string, occurrence int) (DMEnemyProposal, error) {
	if !dmEnemyVerbOK(verb) {
		return DMEnemyProposal{}, ErrDMEnemyInvalidVerb
	}
	actor, err := dmEnemyActor(s, actorID)
	if err != nil {
		return DMEnemyProposal{}, err
	}
	if actor.Body.Class < playerSubDMClass {
		return DMEnemyProposal{
			Action: DMEnemyUnknown, ActorID: actorID, Verb: verb, Occurrence: occurrence,
			EnemyIDs: []string{}, EnemyNames: []string{}, Response: DMEnemyUnknownResponse(verb), Changed: false,
		}, nil
	}
	if targetName == "" {
		return DMEnemyProposal{}, ErrDMEnemyTargetRequired
	}
	if !validDMEnemyName(targetName) || strings.IndexFunc(targetName, unicode.IsSpace) >= 0 {
		return DMEnemyProposal{}, ErrDMEnemyTargetNameInvalid
	}
	if occurrence < 1 {
		return DMEnemyProposal{}, ErrDMEnemyInvalidOccurrence
	}
	if s.NPCs == nil {
		return DMEnemyProposal{}, ErrDMEnemyNPCUnresolved
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok || room.NPCIDs == nil {
		return DMEnemyProposal{}, ErrDMEnemyNPCUnresolved
	}
	if err := s.Validate(); err != nil {
		return DMEnemyProposal{}, fmt.Errorf("%w: %v", ErrDMEnemyStateInvalid, err)
	}
	npcID, npc, err := s.selectDMEnemyNPC(actor, targetName, occurrence)
	if err != nil {
		return DMEnemyProposal{}, err
	}
	enemyIDs, enemyNames, err := s.enemyNames(npc)
	if err != nil {
		return DMEnemyProposal{}, err
	}
	projection := DMEnemyProjection{
		Action: DMEnemyList, ActorID: actorID, Verb: verb, Occurrence: occurrence,
		TargetID: npcID, TargetName: npc.Body.Name, EnemyIDs: enemyIDs, EnemyNames: enemyNames,
		Response: dmEnemyResponse(npc.Body.Name, enemyNames), Changed: false,
	}
	return dmEnemyProposalFromProjection(projection), nil
}

// ApplyDMEnemy validates the snapshot-bound projection and returns the exact
// input state. No enemy list or world state is mutated by list_enm.
func (s State) ApplyDMEnemy(proposal DMEnemyProposal) (State, DMEnemyResult, error) {
	if proposal.ActorID == "" || proposal.Verb == "" || proposal.Occurrence < 1 || proposal.Changed ||
		!dmEnemyVerbOK(proposal.Verb) || proposal.Action == DMEnemyAction("") {
		return State{}, DMEnemyResult{}, ErrDMEnemyInvalidProposal
	}
	if proposal.Action == DMEnemyList && (proposal.TargetID == "" || proposal.TargetName == "") {
		return State{}, DMEnemyResult{}, ErrDMEnemyInvalidProposal
	}
	if proposal.Action == DMEnemyUnknown && (proposal.TargetID != "" || proposal.TargetName != "" || len(proposal.EnemyIDs) != 0 || len(proposal.EnemyNames) != 0) {
		return State{}, DMEnemyResult{}, ErrDMEnemyInvalidProposal
	}
	if proposal.Action != DMEnemyList && proposal.Action != DMEnemyUnknown {
		return State{}, DMEnemyResult{}, ErrDMEnemyInvalidProposal
	}
	fresh, err := s.PlanDMEnemy(proposal.ActorID, proposal.Verb, proposal.TargetName, proposal.Occurrence)
	if proposal.Action == DMEnemyUnknown {
		// Unauthorized proposals deliberately do not carry a target name. The
		// fresh check uses the same no-op authorization path without resolving it.
		fresh, err = s.PlanDMEnemy(proposal.ActorID, proposal.Verb, "requested-target-redacted", proposal.Occurrence)
	}
	if err != nil {
		return State{}, DMEnemyResult{}, err
	}
	if fresh.Action != proposal.Action || fresh.ActorID != proposal.ActorID || fresh.Verb != proposal.Verb ||
		fresh.Occurrence != proposal.Occurrence || fresh.TargetID != proposal.TargetID || fresh.TargetName != proposal.TargetName ||
		!reflect.DeepEqual(fresh.EnemyIDs, proposal.EnemyIDs) || !reflect.DeepEqual(fresh.EnemyNames, proposal.EnemyNames) ||
		fresh.Response != proposal.Response || fresh.Changed != proposal.Changed {
		return State{}, DMEnemyResult{}, ErrDMEnemyStaleProposal
	}
	return s, dmEnemyProposalProjection(fresh), nil
}

func (s State) ProjectDMEnemy(actorID, verb, targetName string, occurrence int) (DMEnemyProjection, error) {
	proposal, err := s.PlanDMEnemy(actorID, verb, targetName, occurrence)
	if err != nil {
		return DMEnemyProjection{}, err
	}
	return dmEnemyProposalProjection(proposal), nil
}

func (s State) EnemyList(actorID, verb, targetName string, occurrence int) (string, error) {
	projection, err := s.ProjectDMEnemy(actorID, verb, targetName, occurrence)
	if err != nil {
		return "", err
	}
	return projection.Response, nil
}
