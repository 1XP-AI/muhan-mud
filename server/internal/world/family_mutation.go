package world

import (
	"errors"
	"fmt"
	"reflect"
	"sort"

	"github.com/1XP-Inc/muhan-mud/server/internal/identity"
)

// FamilyMutationAction is the bounded subset of command11.c membership
// mutations admitted by the current canonical State.  Apply is the confirmed
// application branch of family()/add_family(); Withdraw is the no-fee
// cancellation branch of out_family()/exit_family().  Approval and active
// membership withdrawal remain explicit fail-closed actions because the
// family fee and family_member_<n> ledger are not in State yet.
type FamilyMutationAction string

const (
	FamilyMutationApply    FamilyMutationAction = "apply"
	FamilyMutationWithdraw FamilyMutationAction = "withdraw"
	FamilyMutationApprove  FamilyMutationAction = "approve"
)

// Compatibility spellings keep adapters aligned with the legacy command
// vocabulary without introducing a second action value in receipts.
const (
	FamilyMutationJoin   = FamilyMutationApply
	FamilyMutationLeave  = FamilyMutationWithdraw
	FamilyMutationSignup = FamilyMutationApply
	FamilyMutationCancel = FamilyMutationWithdraw
	FamilyMutationPermit = FamilyMutationApprove
	FamilyJoin           = FamilyMutationApply
	FamilyLeave          = FamilyMutationWithdraw
	FamilyApprove        = FamilyMutationApprove
)

const familyMutationDeactivatedBoss = "*해체*"

var (
	ErrFamilyMutationInvalidAction       = errors.New("invalid family mutation action")
	ErrFamilyMutationActorAbsent         = errors.New("online canonical family actor absent")
	ErrFamilyMutationIdentityUnresolved  = errors.New("family canonical identity unresolved")
	ErrFamilyMutationStateInvalid        = errors.New("invalid family membership state")
	ErrFamilyMutationFamilyRequired      = errors.New("family id is required")
	ErrFamilyMutationFamilyUnavailable   = errors.New("family is unavailable")
	ErrFamilyMutationBossUnavailable     = errors.New("family boss is not online")
	ErrFamilyMutationBossAmbiguous       = errors.New("family boss identity is ambiguous")
	ErrFamilyMutationAlreadyMember       = errors.New("actor is already a family member")
	ErrFamilyMutationAlreadyPending      = errors.New("actor already has a family application")
	ErrFamilyMutationNotPending          = errors.New("actor has no family application")
	ErrFamilyMutationBossCannotWithdraw  = errors.New("family boss cannot withdraw")
	ErrFamilyMutationFeeUnavailable      = errors.New("family fee ledger is unavailable")
	ErrFamilyMutationApprovalUnsupported = errors.New("family approval requires canonical fee and member ledgers")
	ErrFamilyMutationStaleProposal       = errors.New("stale family mutation proposal")
	ErrFamilyMutationInvalidProposal     = errors.New("invalid family mutation proposal")
)

// Source-oriented aliases make the intentionally unsupported boundaries
// discoverable to command adapters and tests without coupling them to one
// spelling of the legacy handler.
var (
	ErrFamilyJoinActorAbsent             = ErrFamilyMutationActorAbsent
	ErrFamilyApplicationActorAbsent      = ErrFamilyMutationActorAbsent
	ErrFamilyJoinIdentityUnresolved      = ErrFamilyMutationIdentityUnresolved
	ErrFamilyApplicationUnresolved       = ErrFamilyMutationIdentityUnresolved
	ErrFamilyJoinAlreadyMember           = ErrFamilyMutationAlreadyMember
	ErrFamilyAlreadyMember               = ErrFamilyMutationAlreadyMember
	ErrFamilyJoinAlreadyPending          = ErrFamilyMutationAlreadyPending
	ErrFamilyAlreadyPending              = ErrFamilyMutationAlreadyPending
	ErrFamilyLeaveNotPending             = ErrFamilyMutationNotPending
	ErrFamilyWithdrawalNotPending        = ErrFamilyMutationNotPending
	ErrFamilyBossUnavailable             = ErrFamilyMutationBossUnavailable
	ErrFamilyBossAmbiguous               = ErrFamilyMutationBossAmbiguous
	ErrFamilyWithdrawalFeeUnavailable    = ErrFamilyMutationFeeUnavailable
	ErrFamilyActiveWithdrawalUnsupported = ErrFamilyMutationFeeUnavailable
	ErrFamilyApprovalRequiresLedger      = ErrFamilyMutationApprovalUnsupported
	ErrFamilyApprovalUnsupported         = ErrFamilyMutationApprovalUnsupported
	ErrFamilyMutationStale               = ErrFamilyMutationStaleProposal
)

const (
	// These responses are the post-confirmation portions of add_family and
	// exit_family.  Interactive prompts are connection-local and are not
	// represented by a world proposal.
	FamilyApplicationResponse = "\r\n가입 신청을 하였습니다. \r\n패거리 두목의 허가를 기다리십시요."
	FamilyWithdrawalResponse  = "패거리 가입신청을 취소합니다."
)

// FamilyMutationProposal binds one membership transition to one complete
// world snapshot.  The private snapshot and canonical catalog copy prevent a
// client from manufacturing an authority proof or applying a stale family
// decision after another command changed the actor or family owner.
//
// Apply proposals contain the canonical online boss identity because the C
// application path notifies that boss.  The current world layer records this
// as BossNotificationPending; a transport may derive a post-commit event but
// must suppress it for a replayed receipt.
type FamilyMutationProposal struct {
	Action     FamilyMutationAction
	ActorID    string
	ActorName  string
	TargetID   string
	TargetName string
	BossID     string
	BossName   string
	FamilyID   int16
	FamilyName string

	BeforeFamilyID          int16
	AfterFamilyID           int16
	BeforeFlags             [8]byte
	AfterFlags              [8]byte
	Changed                 bool
	BossNotificationPending bool
	Response                string

	before          State
	expectedActor   PlayerState
	expectedBoss    PlayerState
	expectedCatalog FamilyCatalog
}

// FamilyMembershipProposal is a descriptive alias for callers that do not
// use the legacy command term.
type FamilyMembershipProposal = FamilyMutationProposal
type FamilyApplicationProposal = FamilyMutationProposal

// FamilyMutationResult is the durable receipt projection for the admitted
// application and pending-cancellation transitions.  No family file edit,
// gold transfer, broadcast, or approval is fabricated here.
type FamilyMutationResult struct {
	Action                  FamilyMutationAction `json:"action"`
	ActorID                 string               `json:"actor_id"`
	ActorName               string               `json:"actor_name"`
	TargetID                string               `json:"target_id,omitempty"`
	TargetName              string               `json:"target_name,omitempty"`
	BossID                  string               `json:"boss_id,omitempty"`
	BossName                string               `json:"boss_name,omitempty"`
	FamilyID                int16                `json:"family_id"`
	FamilyName              string               `json:"family_name"`
	BeforeFamilyID          int16                `json:"before_family_id"`
	AfterFamilyID           int16                `json:"after_family_id"`
	BeforeFlags             [8]byte              `json:"before_flags"`
	AfterFlags              [8]byte              `json:"after_flags"`
	Changed                 bool                 `json:"changed"`
	BossNotificationPending bool                 `json:"boss_notification_pending,omitempty"`
	Response                string               `json:"response"`
}

type FamilyMembershipResult = FamilyMutationResult
type FamilyApplicationResult = FamilyMutationResult

func cloneFamilyCatalog(catalog FamilyCatalog) FamilyCatalog {
	if catalog.Families == nil {
		return FamilyCatalog{}
	}
	copy := make(map[int16]FamilyDefinition, len(catalog.Families))
	for id, family := range catalog.Families {
		copy[id] = family
	}
	return FamilyCatalog{Families: copy}
}

func familyMutationCanonicalPlayer(s State, id string) (PlayerState, error) {
	player, ok := s.Players[id]
	if id == "" || !ok || !player.Online || player.Body.Type != 0 {
		return PlayerState{}, ErrFamilyMutationActorAbsent
	}
	if player.Body.Name == "" {
		return PlayerState{}, ErrFamilyMutationIdentityUnresolved
	}
	canonical, err := identity.CanonicalName(player.Body.Name)
	if err != nil || canonical != player.Body.Name || !validFamilyText(player.Body.Name) {
		return PlayerState{}, ErrFamilyMutationIdentityUnresolved
	}
	room, ok := s.Rooms[player.Body.RoomID]
	if !ok || room.Resource.ID != player.Body.RoomID || !containsString(room.PlayerIDs, id) {
		return PlayerState{}, fmt.Errorf("%w: canonical room membership absent", ErrFamilyMutationActorAbsent)
	}
	return player, nil
}

func familyMutationMembership(player PlayerState) (active, pending bool, id int16, err error) {
	active = familyActive(player.Body)
	pending = familyPending(player.Body)
	id = familyID(player.Body)
	if active && pending {
		return false, false, 0, ErrFamilyMutationStateInvalid
	}
	if active || pending {
		if id < 1 || id > FamilyMaxID {
			return false, false, 0, fmt.Errorf("%w: family id %d", ErrFamilyMutationStateInvalid, id)
		}
	} else if id != 0 {
		return false, false, 0, fmt.Errorf("%w: unowned actor has family id %d", ErrFamilyMutationStateInvalid, id)
	}
	if flag(player.Body.Flags[:], FamilyBossFlag) && (!active || id == 0) {
		return false, false, 0, fmt.Errorf("%w: family boss is not an active member", ErrFamilyMutationStateInvalid)
	}
	return active, pending, id, nil
}

func familyMutationBoss(s State, family FamilyDefinition) (string, PlayerState, error) {
	if family.Boss == familyMutationDeactivatedBoss {
		return "", PlayerState{}, ErrFamilyMutationFamilyUnavailable
	}
	// family_list stores the canonical display name and C find_who uses an
	// exact strcmp.  Do not use prefixes or map order to select an owner.
	matches := make([]string, 0, 1)
	for id, player := range s.Players {
		if id == "" || !player.Online || player.Body.Type != 0 || player.Body.Name != family.Boss {
			continue
		}
		canonical, err := identity.CanonicalName(player.Body.Name)
		if err != nil || canonical != player.Body.Name || !validFamilyText(player.Body.Name) {
			return "", PlayerState{}, ErrFamilyMutationIdentityUnresolved
		}
		matches = append(matches, id)
	}
	if len(matches) == 0 {
		return "", PlayerState{}, ErrFamilyMutationBossUnavailable
	}
	if len(matches) != 1 {
		sort.Strings(matches)
		return "", PlayerState{}, ErrFamilyMutationBossAmbiguous
	}
	bossID := matches[0]
	boss := s.Players[bossID]
	active, pending, familyIDValue, err := familyMutationMembership(boss)
	if err != nil {
		return "", PlayerState{}, err
	}
	if !active || pending || familyIDValue != family.ID || !flag(boss.Body.Flags[:], FamilyBossFlag) {
		// The catalog is the owner-name source, but State must still prove the
		// owner is a current PFAMIL/PFMBOS member before an application can be
		// accepted.  Otherwise a stale catalog could grant membership to a
		// family with no canonical owner.
		return "", PlayerState{}, ErrFamilyMutationIdentityUnresolved
	}
	return bossID, boss, nil
}

func familyMutationCatalog(catalog FamilyCatalog, familyID int16) (FamilyDefinition, error) {
	if err := catalog.Validate(); err != nil {
		return FamilyDefinition{}, err
	}
	if familyID < 1 || familyID > FamilyMaxID {
		return FamilyDefinition{}, fmt.Errorf("%w: %d", ErrFamilyMutationFamilyRequired, familyID)
	}
	family, err := catalog.lookup(familyID)
	if err != nil {
		return FamilyDefinition{}, err
	}
	if family.Name == familyMutationDeactivatedBoss || family.Boss == familyMutationDeactivatedBoss {
		return FamilyDefinition{}, ErrFamilyMutationFamilyUnavailable
	}
	return family, nil
}

func familyMutationJoinPlan(s State, actorID string, familyID int16, catalog FamilyCatalog) (FamilyMutationProposal, error) {
	if err := s.Validate(); err != nil {
		return FamilyMutationProposal{}, err
	}
	actor, err := familyMutationCanonicalPlayer(s, actorID)
	if err != nil {
		if errors.Is(err, ErrFamilyMutationActorAbsent) || errors.Is(err, ErrFamilyMutationIdentityUnresolved) {
			return FamilyMutationProposal{}, err
		}
		return FamilyMutationProposal{}, err
	}
	active, pending, currentFamilyID, err := familyMutationMembership(actor)
	if err != nil {
		return FamilyMutationProposal{}, err
	}
	if active {
		return FamilyMutationProposal{}, ErrFamilyMutationAlreadyMember
	}
	if pending {
		return FamilyMutationProposal{}, ErrFamilyMutationAlreadyPending
	}
	family, err := familyMutationCatalog(catalog, familyID)
	if err != nil {
		return FamilyMutationProposal{}, err
	}
	bossID, boss, err := familyMutationBoss(s, family)
	if err != nil {
		return FamilyMutationProposal{}, err
	}
	beforeFlags := actor.Body.Flags
	afterFlags := beforeFlags
	afterFlags[FamilyPendingFlag/8] |= 1 << (FamilyPendingFlag % 8)
	// A valid unowned actor must already have a zero family ID.  Keep this
	// guard explicit so a future State.Validate relaxation cannot silently
	// carry a stale numeric reference into a new application.
	if currentFamilyID != 0 {
		return FamilyMutationProposal{}, ErrFamilyMutationStateInvalid
	}
	snapshot := s.clone()
	return FamilyMutationProposal{
		Action:  FamilyMutationApply,
		ActorID: actorID, ActorName: actor.Body.Name,
		BossID: bossID, BossName: boss.Body.Name,
		FamilyID: family.ID, FamilyName: family.Name,
		BeforeFamilyID: 0, AfterFamilyID: family.ID,
		BeforeFlags: beforeFlags, AfterFlags: afterFlags,
		Changed: true, BossNotificationPending: true,
		Response: FamilyApplicationResponse,
		before:   snapshot, expectedActor: snapshot.Players[actorID], expectedBoss: snapshot.Players[bossID], expectedCatalog: cloneFamilyCatalog(catalog),
	}, nil
}

func familyMutationWithdrawPlan(s State, actorID string, catalog FamilyCatalog) (FamilyMutationProposal, error) {
	if err := s.Validate(); err != nil {
		return FamilyMutationProposal{}, err
	}
	actor, err := familyMutationCanonicalPlayer(s, actorID)
	if err != nil {
		return FamilyMutationProposal{}, err
	}
	active, pending, familyID, err := familyMutationMembership(actor)
	if err != nil {
		return FamilyMutationProposal{}, err
	}
	if flag(actor.Body.Flags[:], FamilyBossFlag) {
		return FamilyMutationProposal{}, ErrFamilyMutationBossCannotWithdraw
	}
	if !pending {
		if active {
			// C requires family_gold[id]*20000 and rewrites
			// family_member_<n>. Neither authority exists in State.
			return FamilyMutationProposal{}, ErrFamilyMutationFeeUnavailable
		}
		return FamilyMutationProposal{}, ErrFamilyMutationNotPending
	}
	family, err := familyMutationCatalog(catalog, familyID)
	if err != nil {
		return FamilyMutationProposal{}, err
	}
	beforeFlags := actor.Body.Flags
	afterFlags := beforeFlags
	afterFlags[FamilyPendingFlag/8] &^= 1 << (FamilyPendingFlag % 8)
	snapshot := s.clone()
	return FamilyMutationProposal{
		Action:  FamilyMutationWithdraw,
		ActorID: actorID, ActorName: actor.Body.Name,
		FamilyID: family.ID, FamilyName: family.Name,
		BeforeFamilyID: familyID, AfterFamilyID: 0,
		BeforeFlags: beforeFlags, AfterFlags: afterFlags,
		Changed: true, Response: FamilyWithdrawalResponse,
		before: snapshot, expectedActor: snapshot.Players[actorID], expectedCatalog: cloneFamilyCatalog(catalog),
	}, nil
}

// PlanFamilyMutation plans an admitted membership transition.  familyID is
// required for apply and must be zero for withdraw; targetName is reserved
// for approval and must be empty for the admitted actions.
func (s State) PlanFamilyMutation(actorID string, action FamilyMutationAction, familyID int16, targetName string, catalog FamilyCatalog) (FamilyMutationProposal, error) {
	switch action {
	case FamilyMutationApply:
		if targetName != "" {
			return FamilyMutationProposal{}, ErrFamilyMutationInvalidProposal
		}
		return familyMutationJoinPlan(s, actorID, familyID, catalog)
	case FamilyMutationWithdraw:
		if familyID != 0 || targetName != "" {
			return FamilyMutationProposal{}, ErrFamilyMutationInvalidProposal
		}
		return familyMutationWithdrawPlan(s, actorID, catalog)
	case FamilyMutationApprove:
		return FamilyMutationProposal{}, ErrFamilyMutationApprovalUnsupported
	default:
		return FamilyMutationProposal{}, ErrFamilyMutationInvalidAction
	}
}

// PlanFamilyJoin is the source-oriented family()/add_family() API.  It plans
// the confirmed application step; terminal confirmation remains connection
// local and must not be persisted as world state.
func (s State) PlanFamilyJoin(actorID string, familyID int16, catalog FamilyCatalog) (FamilyMutationProposal, error) {
	return s.PlanFamilyMutation(actorID, FamilyMutationApply, familyID, "", catalog)
}

func (s State) PlanFamilyApplication(actorID string, familyID int16, catalog FamilyCatalog) (FamilyMutationProposal, error) {
	return s.PlanFamilyJoin(actorID, familyID, catalog)
}

func (s State) PlanFamilySignup(actorID string, familyID int16, catalog FamilyCatalog) (FamilyMutationProposal, error) {
	return s.PlanFamilyJoin(actorID, familyID, catalog)
}

// PlanFamilyJoinByName retains the C selection surface while resolving to the
// numeric FamilyCatalog ID before the proposal is created.  Duplicate names
// are rejected by FamilyCatalog.Validate; no client-supplied name is stored as
// the membership key.
func (s State) PlanFamilyJoinByName(actorID, familyName string, catalog FamilyCatalog) (FamilyMutationProposal, error) {
	if err := catalog.Validate(); err != nil {
		return FamilyMutationProposal{}, err
	}
	if familyName == "" || !validFamilyText(familyName) {
		return FamilyMutationProposal{}, ErrFamilyMutationFamilyRequired
	}
	var familyID int16
	for id, family := range catalog.Families {
		if family.Name == familyName {
			if familyID != 0 {
				return FamilyMutationProposal{}, ErrFamilyMutationFamilyUnavailable
			}
			familyID = id
		}
	}
	if familyID == 0 {
		return FamilyMutationProposal{}, ErrFamilyMutationFamilyUnavailable
	}
	return s.PlanFamilyJoin(actorID, familyID, catalog)
}

// PlanFamilyWithdrawal plans only the source branch that cancels a pending
// application.  Active leave is rejected until fee and family-member ledger
// authority are admitted.
func (s State) PlanFamilyWithdrawal(actorID string, catalog FamilyCatalog) (FamilyMutationProposal, error) {
	return s.PlanFamilyMutation(actorID, FamilyMutationWithdraw, 0, "", catalog)
}

func (s State) PlanFamilyLeave(actorID string, catalog FamilyCatalog) (FamilyMutationProposal, error) {
	return s.PlanFamilyWithdrawal(actorID, catalog)
}

// PlanFamilyApproval names the intentionally unsupported command11.c
// boss_family boundary.  It does not resolve a target or inspect a name: the
// target would otherwise become an existence/visibility oracle before the
// fee and family-member transaction can be proven.
func (s State) PlanFamilyApproval(actorID, targetName string, catalog FamilyCatalog) (FamilyMutationProposal, error) {
	return FamilyMutationProposal{}, ErrFamilyMutationApprovalUnsupported
}

func (s State) PlanFamilyApprove(actorID, targetName string, catalog FamilyCatalog) (FamilyMutationProposal, error) {
	return s.PlanFamilyApproval(actorID, targetName, catalog)
}

func familyMutationProposalMatches(actual, expected FamilyMutationProposal) bool {
	return actual.Action == expected.Action && actual.ActorID == expected.ActorID && actual.ActorName == expected.ActorName &&
		actual.TargetID == expected.TargetID && actual.TargetName == expected.TargetName && actual.BossID == expected.BossID && actual.BossName == expected.BossName &&
		actual.FamilyID == expected.FamilyID && actual.FamilyName == expected.FamilyName && actual.BeforeFamilyID == expected.BeforeFamilyID && actual.AfterFamilyID == expected.AfterFamilyID &&
		actual.BeforeFlags == expected.BeforeFlags && actual.AfterFlags == expected.AfterFlags && actual.Changed == expected.Changed && actual.BossNotificationPending == expected.BossNotificationPending && actual.Response == expected.Response
}

func familyMutationResult(p FamilyMutationProposal) FamilyMutationResult {
	return FamilyMutationResult{
		Action: p.Action, ActorID: p.ActorID, ActorName: p.ActorName,
		TargetID: p.TargetID, TargetName: p.TargetName, BossID: p.BossID, BossName: p.BossName,
		FamilyID: p.FamilyID, FamilyName: p.FamilyName,
		BeforeFamilyID: p.BeforeFamilyID, AfterFamilyID: p.AfterFamilyID,
		BeforeFlags: p.BeforeFlags, AfterFlags: p.AfterFlags,
		Changed: p.Changed, BossNotificationPending: p.BossNotificationPending, Response: p.Response,
	}
}

// ApplyFamilyMutation atomically applies a proposal to a cloned State.  It
// first requires the complete planning snapshot to match, then re-plans from
// the copied FamilyCatalog and compares every public proposal field.  A stale
// or tampered proposal returns a zero State and never mutates the receiver.
func (s State) ApplyFamilyMutation(proposal FamilyMutationProposal) (State, FamilyMutationResult, error) {
	if proposal.ActorID == "" || proposal.before.Version == 0 || !reflect.DeepEqual(s, proposal.before) {
		return State{}, FamilyMutationResult{}, ErrFamilyMutationStaleProposal
	}
	if proposal.expectedCatalog.Families == nil {
		return State{}, FamilyMutationResult{}, ErrFamilyMutationInvalidProposal
	}
	var fresh FamilyMutationProposal
	var err error
	switch proposal.Action {
	case FamilyMutationApply:
		fresh, err = familyMutationJoinPlan(s, proposal.ActorID, proposal.FamilyID, proposal.expectedCatalog)
	case FamilyMutationWithdraw:
		fresh, err = familyMutationWithdrawPlan(s, proposal.ActorID, proposal.expectedCatalog)
	default:
		return State{}, FamilyMutationResult{}, ErrFamilyMutationInvalidProposal
	}
	if err != nil {
		return State{}, FamilyMutationResult{}, ErrFamilyMutationStaleProposal
	}
	if !familyMutationProposalMatches(proposal, fresh) || !reflect.DeepEqual(proposal.expectedActor, fresh.expectedActor) || !reflect.DeepEqual(proposal.expectedBoss, fresh.expectedBoss) || !reflect.DeepEqual(proposal.expectedCatalog, fresh.expectedCatalog) {
		return State{}, FamilyMutationResult{}, ErrFamilyMutationStaleProposal
	}
	if !proposal.Changed || proposal.Response == "" || proposal.BeforeFlags == proposal.AfterFlags {
		return State{}, FamilyMutationResult{}, ErrFamilyMutationInvalidProposal
	}

	next := s.clone()
	actor := next.Players[proposal.ActorID]
	if proposal.Action == FamilyMutationApply {
		actor.Body.Flags = proposal.AfterFlags
		actor.Body.Daily[FamilyDailySlot].Max = byte(proposal.AfterFamilyID)
	} else {
		actor.Body.Flags = proposal.AfterFlags
		actor.Body.Daily[FamilyDailySlot].Max = 0
	}
	next.Players[proposal.ActorID] = actor
	if err := next.Validate(); err != nil {
		return State{}, FamilyMutationResult{}, err
	}
	if _, _, _, err := familyMutationMembership(next.Players[proposal.ActorID]); err != nil {
		return State{}, FamilyMutationResult{}, err
	}
	return next, familyMutationResult(proposal), nil
}

func (s State) ApplyFamilyJoin(proposal FamilyMutationProposal) (State, FamilyMutationResult, error) {
	return s.ApplyFamilyMutation(proposal)
}

func (s State) ApplyFamilyApplication(proposal FamilyMutationProposal) (State, FamilyMutationResult, error) {
	return s.ApplyFamilyMutation(proposal)
}

func (s State) ApplyFamilyWithdrawal(proposal FamilyMutationProposal) (State, FamilyMutationResult, error) {
	return s.ApplyFamilyMutation(proposal)
}

func (s State) ApplyFamilyLeave(proposal FamilyMutationProposal) (State, FamilyMutationResult, error) {
	return s.ApplyFamilyMutation(proposal)
}

func (s State) FamilyJoin(actorID string, familyID int16, catalog FamilyCatalog) (State, FamilyMutationResult, error) {
	proposal, err := s.PlanFamilyJoin(actorID, familyID, catalog)
	if err != nil {
		return State{}, FamilyMutationResult{}, err
	}
	return s.ApplyFamilyMutation(proposal)
}

func (s State) FamilyWithdrawal(actorID string, catalog FamilyCatalog) (State, FamilyMutationResult, error) {
	proposal, err := s.PlanFamilyWithdrawal(actorID, catalog)
	if err != nil {
		return State{}, FamilyMutationResult{}, err
	}
	return s.ApplyFamilyMutation(proposal)
}

// Package-level wrappers mirror other world reducers for callers that keep a
// State value as an explicit first argument.
func PlanFamilyMutation(s State, actorID string, action FamilyMutationAction, familyID int16, targetName string, catalog FamilyCatalog) (FamilyMutationProposal, error) {
	return s.PlanFamilyMutation(actorID, action, familyID, targetName, catalog)
}

func ApplyFamilyMutation(s State, proposal FamilyMutationProposal) (State, FamilyMutationResult, error) {
	return s.ApplyFamilyMutation(proposal)
}
