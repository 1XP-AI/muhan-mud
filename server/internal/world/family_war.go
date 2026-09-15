package world

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
)

// FamilyWarAction is the bounded subset of special1.c:call_war admitted by
// the current canonical State. Declare is the first CALLWAR1/CALLWAR2 write,
// Cancel clears a pending challenge by the original caller, and Accept sets
// AT_WAR from the challenged family's boss.
type FamilyWarAction string

const (
	FamilyWarDeclare FamilyWarAction = "declare"
	FamilyWarCancel  FamilyWarAction = "cancel"
	FamilyWarAccept  FamilyWarAction = "accept"

	FamilyWarNotBossResponse        = "당신은 선전 포고할 권리가 없습니다.\n"
	FamilyWarTargetPrompt           = "어느 패거리와 전쟁을 하시려고요?"
	FamilyWarUnknownResponse        = "그런 패거리는 없습니다."
	FamilyWarSelfResponse           = "자기 자신들과 싸우시려고요?"
	FamilyWarAlreadyResponse        = "벌써 전쟁중입니다.\n"
	FamilyWarBusyResponse           = "다른 패거리에서 먼저 전쟁을 준비중입니다."
	FamilyWarOtherChallengeResponse = "다른 패거리에서 전쟁을 신청해두고 있습니다."
)

var (
	ErrFamilyWarUnresolved         = errors.New("family war state unresolved")
	ErrFamilyWarActorAbsent        = errors.New("online canonical family-war actor absent")
	ErrFamilyWarIdentityUnresolved = errors.New("family-war canonical identity unresolved")
	ErrFamilyWarStateInvalid       = errors.New("invalid family-war membership state")
	ErrFamilyWarStaleProposal      = errors.New("stale family war proposal")
	ErrFamilyWarInvalidProposal    = errors.New("invalid family war proposal")
)

func FamilyWarBossOfflineResponse(boss string) string {
	return fmt.Sprintf("상대편의 두목인 %s님이 이용중이 아닙니다.", boss)
}

func FamilyWarDeclareText(actorFamily, targetFamily string) string {
	return fmt.Sprintf("\n### %s 패거리가 %s에게 선전포고를 합니다.\n\n", actorFamily, targetFamily)
}

func FamilyWarCancelText(actorFamily string) string {
	return fmt.Sprintf("\n### %s 패거리에서 선전포고를 취소합니다.\n", actorFamily)
}

func FamilyWarAcceptText(actorFamily string) string {
	return fmt.Sprintf("\n### %s 패거리에서 선전포고를 받아들였습니다.\n", actorFamily)
}

func FamilyWarBossDiedText(familyName string) string {
	return fmt.Sprintf("\n### [%s] 패거리의 문주가 죽었습니다.", familyName)
}

func FamilyWarDefeatedText(familyName string) string {
	return fmt.Sprintf("\n### [%s] 패거리는 전쟁에서 패했습니다.", familyName)
}

// FamilyDefeatBroadcasts is creature.c:die's two broadcast_all lines after a
// war-participating PFMBOS death. The catalog name is family_str[id]; a missing
// or invalid catalog fails closed instead of inventing a display name.
func FamilyDefeatBroadcasts(catalog FamilyCatalog, familyID byte) ([]FamilyWarEvent, error) {
	family, err := catalog.lookup(int16(familyID))
	if err != nil {
		return nil, err
	}
	return []FamilyWarEvent{
		{AllPlayers: true, Text: FamilyWarBossDiedText(family.Name)},
		{AllPlayers: true, Text: FamilyWarDefeatedText(family.Name)},
	}, nil
}

// FamilyWarEvent is a post-commit global announcement. AllPlayers is
// broadcast_all (no PNOBRD filter). HonorNoBroadcast is broadcast().
type FamilyWarEvent struct {
	AllPlayers bool   `json:"all_players,omitempty"`
	ActorID    string `json:"actor_id,omitempty"`
	Text       string `json:"text"`
}

// FamilyWarResult is the durable receipt projection for 선전포고.
type FamilyWarResult struct {
	Action     FamilyWarAction  `json:"action,omitempty"`
	ActorID    string           `json:"actor_id"`
	ActorName  string           `json:"actor_name,omitempty"`
	FamilyID   int16            `json:"family_id,omitempty"`
	FamilyName string           `json:"family_name,omitempty"`
	TargetID   int16            `json:"target_id,omitempty"`
	TargetName string           `json:"target_name,omitempty"`
	Before     FamilyWar        `json:"before"`
	After      FamilyWar        `json:"after"`
	Response   string           `json:"response"`
	Changed    bool             `json:"changed"`
	Events     []FamilyWarEvent `json:"events,omitempty"`
}

// FamilyWarProposal binds one call_war transition to one snapshot.
type FamilyWarProposal struct {
	Action     FamilyWarAction
	ActorID    string
	ActorName  string
	FamilyID   int16
	FamilyName string
	TargetID   int16
	TargetName string
	Before     FamilyWar
	After      FamilyWar
	Response   string
	Changed    bool
	Events     []FamilyWarEvent

	before          State
	expectedActor   PlayerState
	expectedWar     FamilyWar
	expectedCatalog FamilyCatalog
}

func familyWarImported(s State) (FamilyWar, error) {
	if s.War == nil {
		return FamilyWar{}, ErrFamilyWarUnresolved
	}
	return *s.War, nil
}

func familyWarActor(s State, actorID string, catalog FamilyCatalog) (PlayerState, FamilyDefinition, FamilyWar, FamilyWarResult, error) {
	if err := s.Validate(); err != nil {
		return PlayerState{}, FamilyDefinition{}, FamilyWar{}, FamilyWarResult{}, err
	}
	war, err := familyWarImported(s)
	if err != nil {
		return PlayerState{}, FamilyDefinition{}, FamilyWar{}, FamilyWarResult{}, err
	}
	actor, err := familyMutationCanonicalPlayer(s, actorID)
	if err != nil {
		if errors.Is(err, ErrFamilyMutationActorAbsent) {
			return PlayerState{}, FamilyDefinition{}, FamilyWar{}, FamilyWarResult{}, ErrFamilyWarActorAbsent
		}
		return PlayerState{}, FamilyDefinition{}, FamilyWar{}, FamilyWarResult{}, ErrFamilyWarIdentityUnresolved
	}
	result := FamilyWarResult{ActorID: actorID, ActorName: actor.Body.Name, Before: war, After: war}
	active, pending, familyID, err := familyMutationMembership(actor)
	if err != nil {
		return PlayerState{}, FamilyDefinition{}, FamilyWar{}, FamilyWarResult{}, fmt.Errorf("%w: %v", ErrFamilyWarStateInvalid, err)
	}
	if !active || pending || familyID == 0 || !flag(actor.Body.Flags[:], FamilyBossFlag) {
		result.Response = FamilyWarNotBossResponse
		return actor, FamilyDefinition{}, war, result, nil
	}
	family, err := familyMutationCatalog(catalog, familyID)
	if err != nil {
		return PlayerState{}, FamilyDefinition{}, FamilyWar{}, FamilyWarResult{}, err
	}
	if family.Boss != actor.Body.Name {
		result.Response = FamilyWarNotBossResponse
		return actor, family, war, result, nil
	}
	result.FamilyID, result.FamilyName = family.ID, family.Name
	return actor, family, war, result, nil
}

func familyWarLookupName(catalog FamilyCatalog, name string) (FamilyDefinition, bool, error) {
	if err := catalog.Validate(); err != nil {
		return FamilyDefinition{}, false, err
	}
	if name == "" || !validFamilyText(name) {
		return FamilyDefinition{}, false, nil
	}
	var found FamilyDefinition
	count := 0
	for _, family := range catalog.Families {
		if family.Name == name {
			found = family
			count++
		}
	}
	if count != 1 {
		return FamilyDefinition{}, false, nil
	}
	return found, true, nil
}

func familyWarOnlineBoss(s State, name string) (string, PlayerState, bool, error) {
	ids := make([]string, 0, 1)
	for id, player := range s.Players {
		if id == "" || !player.Online || player.Body.Type != 0 || player.Body.Name != name {
			continue
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return "", PlayerState{}, false, nil
	}
	if len(ids) != 1 {
		sort.Strings(ids)
		return "", PlayerState{}, false, ErrFamilyWarIdentityUnresolved
	}
	return ids[0], s.Players[ids[0]], true, nil
}

func familyWarPack(first, second int16) byte {
	return byte(first)*16 + byte(second)
}

func familyWarResult(p FamilyWarProposal) FamilyWarResult {
	return FamilyWarResult{
		Action: p.Action, ActorID: p.ActorID, ActorName: p.ActorName,
		FamilyID: p.FamilyID, FamilyName: p.FamilyName, TargetID: p.TargetID, TargetName: p.TargetName,
		Before: p.Before, After: p.After, Response: p.Response, Changed: p.Changed,
		Events: append([]FamilyWarEvent(nil), p.Events...),
	}
}

func familyWarGateProposal(actor PlayerState, family FamilyDefinition, war FamilyWar, result FamilyWarResult, catalog FamilyCatalog, snapshot State) FamilyWarProposal {
	return FamilyWarProposal{
		ActorID: result.ActorID, ActorName: actor.Body.Name,
		FamilyID: family.ID, FamilyName: family.Name,
		Before: war, After: war, Response: result.Response, Changed: false,
		before: snapshot, expectedActor: snapshot.Players[result.ActorID], expectedWar: war,
		expectedCatalog: cloneFamilyCatalog(catalog),
	}
}

// PlanFamilyWar is special1.c:call_war. targetName is the exact catalog
// family name; an empty name is the original missing-argument prompt.
func (s State) PlanFamilyWar(actorID, targetName string, catalog FamilyCatalog) (FamilyWarProposal, error) {
	actor, family, war, gated, err := familyWarActor(s, actorID, catalog)
	if err != nil {
		return FamilyWarProposal{}, err
	}
	snapshot := s.clone()
	if gated.Response == FamilyWarNotBossResponse {
		return familyWarGateProposal(actor, family, war, gated, catalog, snapshot), nil
	}
	if targetName == "" {
		gated.Response = FamilyWarTargetPrompt
		return familyWarGateProposal(actor, family, war, gated, catalog, snapshot), nil
	}
	target, ok, err := familyWarLookupName(catalog, targetName)
	if err != nil {
		return FamilyWarProposal{}, err
	}
	if !ok {
		gated.Response = FamilyWarUnknownResponse
		return familyWarGateProposal(actor, family, war, gated, catalog, snapshot), nil
	}
	_, _, online, err := familyWarOnlineBoss(s, target.Boss)
	if err != nil {
		return FamilyWarProposal{}, err
	}
	if !online {
		gated.Response = FamilyWarBossOfflineResponse(target.Boss)
		return familyWarGateProposal(actor, family, war, gated, catalog, snapshot), nil
	}
	if family.ID == target.ID {
		gated.Response = FamilyWarSelfResponse
		return familyWarGateProposal(actor, family, war, gated, catalog, snapshot), nil
	}
	if war.Active != 0 {
		gated.Response = FamilyWarAlreadyResponse
		return familyWarGateProposal(actor, family, war, gated, catalog, snapshot), nil
	}

	proposal := FamilyWarProposal{
		ActorID: actorID, ActorName: actor.Body.Name,
		FamilyID: family.ID, FamilyName: family.Name,
		TargetID: target.ID, TargetName: target.Name,
		Before: war, After: war,
		before: snapshot, expectedActor: snapshot.Players[actorID], expectedWar: war,
		expectedCatalog: cloneFamilyCatalog(catalog),
	}
	actorNum := byte(family.ID)
	targetNum := byte(target.ID)
	switch {
	case war.CalledAgainst == 0:
		proposal.Action = FamilyWarDeclare
		proposal.After = FamilyWar{CalledBy: actorNum, CalledAgainst: targetNum}
		proposal.Changed = true
		text := FamilyWarDeclareText(family.Name, target.Name)
		proposal.Response = text
		proposal.Events = []FamilyWarEvent{{AllPlayers: true, ActorID: actorID, Text: text}}
	case war.CalledBy == actorNum:
		proposal.Action = FamilyWarCancel
		proposal.After = FamilyWar{}
		proposal.Changed = true
		text := FamilyWarCancelText(family.Name)
		proposal.Response = text
		proposal.Events = []FamilyWarEvent{{ActorID: actorID, Text: text}}
	case war.CalledAgainst == actorNum:
		if war.CalledBy != targetNum {
			proposal.Response = FamilyWarOtherChallengeResponse
			return proposal, nil
		}
		proposal.Action = FamilyWarAccept
		proposal.After = FamilyWar{Active: familyWarPack(family.ID, target.ID), CalledBy: war.CalledBy, CalledAgainst: war.CalledAgainst}
		proposal.Changed = true
		text := FamilyWarAcceptText(family.Name)
		proposal.Response = text
		proposal.Events = []FamilyWarEvent{{ActorID: actorID, Text: text}}
	default:
		proposal.Response = FamilyWarBusyResponse
	}
	return proposal, nil
}

func familyWarProposalMatches(actual, expected FamilyWarProposal) bool {
	return actual.Action == expected.Action && actual.ActorID == expected.ActorID && actual.ActorName == expected.ActorName &&
		actual.FamilyID == expected.FamilyID && actual.FamilyName == expected.FamilyName &&
		actual.TargetID == expected.TargetID && actual.TargetName == expected.TargetName &&
		actual.Before == expected.Before && actual.After == expected.After &&
		actual.Response == expected.Response && actual.Changed == expected.Changed &&
		reflect.DeepEqual(actual.Events, expected.Events)
}

// ApplyFamilyWar atomically applies a snapshot-bound declare/cancel/accept.
// Unchanged gates persist as receipts without Apply.
func (s State) ApplyFamilyWar(proposal FamilyWarProposal) (State, FamilyWarResult, error) {
	if proposal.ActorID == "" || proposal.before.Version == 0 || !reflect.DeepEqual(s, proposal.before) {
		return State{}, FamilyWarResult{}, ErrFamilyWarStaleProposal
	}
	if proposal.expectedCatalog.Families == nil || !proposal.Changed {
		return State{}, FamilyWarResult{}, ErrFamilyWarInvalidProposal
	}
	fresh, err := s.PlanFamilyWar(proposal.ActorID, proposal.TargetName, proposal.expectedCatalog)
	if err != nil {
		return State{}, FamilyWarResult{}, ErrFamilyWarStaleProposal
	}
	if !familyWarProposalMatches(proposal, fresh) || !reflect.DeepEqual(proposal.expectedActor, fresh.expectedActor) ||
		proposal.expectedWar != fresh.expectedWar || !reflect.DeepEqual(proposal.expectedCatalog, fresh.expectedCatalog) {
		return State{}, FamilyWarResult{}, ErrFamilyWarStaleProposal
	}
	next := s.clone()
	war := proposal.After
	next.War = &war
	if err := next.Validate(); err != nil {
		return State{}, FamilyWarResult{}, err
	}
	return next, familyWarResult(proposal), nil
}
