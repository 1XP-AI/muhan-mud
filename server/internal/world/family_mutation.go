package world

import (
	"errors"
	"fmt"
	"reflect"
	"sort"

	"github.com/1XP-Inc/muhan-mud/server/internal/identity"
)

// FamilyMutationAction is the bounded subset of command11.c membership
// mutations admitted by the current canonical State. Apply is the confirmed
// application branch of family()/add_family(); Withdraw covers pending
// cancellation and the ledger-backed active leave branch; Approve is the
// ledger-backed boss_family branch. Bare interactive signup and fm_out remain
// outside this reducer.
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
	ErrFamilyMutationNotBoss             = errors.New("family actor is not the canonical boss")
	ErrFamilyMutationTargetUnavailable   = errors.New("family target is not available")
	ErrFamilyMutationTargetAmbiguous     = errors.New("family target identity is ambiguous")
	ErrFamilyMutationTargetNotPending    = errors.New("family target has no matching application")
	ErrFamilyMutationTargetAlreadyMember = errors.New("family target is already a member")
	ErrFamilyMutationMemberLedgerMissing = errors.New("family member ledger is unavailable")
	ErrFamilyMutationInsufficientGold    = errors.New("family actor gold is insufficient")
	ErrFamilyMutationGoldOverflow        = errors.New("family gold transfer overflows")
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
	ErrFamilyNotBoss                     = ErrFamilyMutationNotBoss
	ErrFamilyTargetUnavailableMutation   = ErrFamilyMutationTargetUnavailable
	ErrFamilyTargetAmbiguousMutation     = ErrFamilyMutationTargetAmbiguous
	ErrFamilyTargetNotPending            = ErrFamilyMutationTargetNotPending
	ErrFamilyTargetAlreadyMember         = ErrFamilyMutationTargetAlreadyMember
	ErrFamilyMemberLedgerUnavailable     = ErrFamilyMutationMemberLedgerMissing
	ErrFamilyInsufficientGold            = ErrFamilyMutationInsufficientGold
	ErrFamilyGoldOverflow                = ErrFamilyMutationGoldOverflow
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
	TargetBeforeFlags       [8]byte
	TargetAfterFlags        [8]byte
	BeforeGold              int32
	AfterGold               int32
	TargetBeforeGold        int32
	TargetAfterGold         int32
	BossBeforeGold          int32
	BossAfterGold           int32
	Fee                     int64
	GoldTransferred         int64
	Changed                 bool
	BossNotificationPending bool
	Response                string

	before          State
	expectedActor   PlayerState
	expectedBoss    PlayerState
	expectedTarget  PlayerState
	expectedFamily  FamilyState
	afterFamily     FamilyState
	expectedCatalog FamilyCatalog
}

// FamilyMembershipProposal is a descriptive alias for callers that do not
// use the legacy command term.
type FamilyMembershipProposal = FamilyMutationProposal
type FamilyApplicationProposal = FamilyMutationProposal

// FamilyMutationResult is the durable receipt projection for admitted family
// transitions. Approval and active leave include the exact gold and member
// ledger deltas; unsupported broadcast/fm_out side effects are not fabricated.
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
	TargetBeforeFlags       [8]byte              `json:"target_before_flags,omitempty"`
	TargetAfterFlags        [8]byte              `json:"target_after_flags,omitempty"`
	BeforeGold              int32                `json:"before_gold,omitempty"`
	AfterGold               int32                `json:"after_gold,omitempty"`
	TargetBeforeGold        int32                `json:"target_before_gold,omitempty"`
	TargetAfterGold         int32                `json:"target_after_gold,omitempty"`
	BossBeforeGold          int32                `json:"boss_before_gold,omitempty"`
	BossAfterGold           int32                `json:"boss_after_gold,omitempty"`
	Fee                     int64                `json:"fee,omitempty"`
	GoldTransferred         int64                `json:"gold_transferred,omitempty"`
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

const (
	familyJoinGoldMultiplier  = int64(10000)
	familyLeaveGoldMultiplier = int64(20000)
	familyMaxPlayerGold       = int64(1<<31 - 1)
)

func familyMutationFeeAmount(fee, multiplier int64) (int64, error) {
	if fee < 0 || multiplier <= 0 || fee > int64(^uint64(0)>>1)/multiplier {
		return 0, ErrFamilyMutationGoldOverflow
	}
	amount := fee * multiplier
	// Gold is a legacy int32 field. A transfer larger than its representable
	// range can never be paid by a player, even when the int64 multiplication
	// itself is safe.
	if amount > familyMaxPlayerGold {
		return 0, ErrFamilyMutationGoldOverflow
	}
	return amount, nil
}

func familyMutationCanonicalTarget(s State, actor PlayerState, targetName string) (string, PlayerState, error) {
	if targetName == "" || !validFamilyText(targetName) {
		return "", PlayerState{}, ErrFamilyMutationIdentityUnresolved
	}
	canonical, err := identity.CanonicalName(targetName)
	if err != nil || canonical != targetName {
		return "", PlayerState{}, ErrFamilyMutationIdentityUnresolved
	}
	ids := make([]string, 0, len(s.Players))
	for id, player := range s.Players {
		if id == "" || !player.Online || player.Body.Type != 0 || player.Body.Name != targetName {
			continue
		}
		bodyCanonical, bodyErr := identity.CanonicalName(player.Body.Name)
		if bodyErr != nil || bodyCanonical != player.Body.Name || !validFamilyText(player.Body.Name) {
			return "", PlayerState{}, ErrFamilyMutationIdentityUnresolved
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return "", PlayerState{}, ErrFamilyMutationTargetUnavailable
	}
	if len(ids) != 1 {
		sort.Strings(ids)
		return "", PlayerState{}, ErrFamilyMutationTargetAmbiguous
	}
	targetID := ids[0]
	target := s.Players[targetID]
	if flag(target.Body.Flags[:], playerDMInvisibleFlag) || flag(actor.Body.Flags[:], FamilyBlindFlag) ||
		(flag(target.Body.Flags[:], playerInvisibleFlag) && !flag(actor.Body.Flags[:], playerDetectFlag)) {
		return "", PlayerState{}, ErrFamilyMutationTargetUnavailable
	}
	return targetID, target, nil
}

func familyMutationApprovalActor(s State, actorID string, catalog FamilyCatalog) (PlayerState, FamilyDefinition, error) {
	actor, err := familyMutationCanonicalPlayer(s, actorID)
	if err != nil {
		return PlayerState{}, FamilyDefinition{}, err
	}
	active, pending, familyID, err := familyMutationMembership(actor)
	if err != nil {
		return PlayerState{}, FamilyDefinition{}, err
	}
	if !active || pending || !flag(actor.Body.Flags[:], FamilyBossFlag) {
		return PlayerState{}, FamilyDefinition{}, ErrFamilyMutationNotBoss
	}
	family, err := familyMutationCatalog(catalog, familyID)
	if err != nil {
		return PlayerState{}, FamilyDefinition{}, err
	}
	// PFMBOS is necessary but not sufficient: the immutable family catalog
	// must identify this exact canonical display name as the owner.
	if family.Boss != actor.Body.Name {
		return PlayerState{}, FamilyDefinition{}, ErrFamilyMutationNotBoss
	}
	canonicalBossID, _, bossErr := familyMutationBoss(s, family)
	if bossErr != nil {
		return PlayerState{}, FamilyDefinition{}, bossErr
	}
	if canonicalBossID != actorID {
		return PlayerState{}, FamilyDefinition{}, ErrFamilyMutationNotBoss
	}
	return actor, family, nil
}

func familyMutationMemberLedger(s State, familyID int16) (FamilyState, []FamilyMember, error) {
	if s.Family == nil {
		return FamilyState{}, nil, ErrFamilyMutationMemberLedgerMissing
	}
	if err := s.Family.Validate(); err != nil {
		return FamilyState{}, nil, fmt.Errorf("%w: %v", ErrFamilyMutationMemberLedgerMissing, err)
	}
	members, err := s.Family.members(familyID)
	if err != nil {
		return FamilyState{}, nil, fmt.Errorf("%w: %v", ErrFamilyMutationMemberLedgerMissing, err)
	}
	return s.Family.Clone(), members, nil
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
	if !pending && !active {
		return FamilyMutationProposal{}, ErrFamilyMutationNotPending
	}
	family, err := familyMutationCatalog(catalog, familyID)
	if err != nil {
		return FamilyMutationProposal{}, err
	}
	if family.Boss == actor.Body.Name {
		return FamilyMutationProposal{}, ErrFamilyMutationBossCannotWithdraw
	}
	beforeFlags := actor.Body.Flags
	afterFlags := beforeFlags
	snapshot := s.clone()
	if active {
		if s.Family == nil {
			// Preserve the historical fail-closed sentinel for snapshots that
			// have not crossed the family-member migration boundary.
			return FamilyMutationProposal{}, ErrFamilyMutationFeeUnavailable
		}
		amount, feeErr := familyMutationFeeAmount(family.Fee, familyLeaveGoldMultiplier)
		if feeErr != nil {
			return FamilyMutationProposal{}, feeErr
		}
		familyState, _, ledgerErr := familyMutationMemberLedger(s, familyID)
		if ledgerErr != nil {
			return FamilyMutationProposal{}, ledgerErr
		}
		member, found, memberErr := familyState.hasMember(familyID, actorID)
		if memberErr != nil {
			return FamilyMutationProposal{}, fmt.Errorf("%w: %v", ErrFamilyMutationMemberLedgerMissing, memberErr)
		}
		if !found || member.Name != actor.Body.Name || member.Class != actor.Body.Class {
			return FamilyMutationProposal{}, ErrFamilyMutationIdentityUnresolved
		}
		if actor.Body.Gold < 0 || int64(actor.Body.Gold) < amount {
			return FamilyMutationProposal{}, ErrFamilyMutationInsufficientGold
		}
		afterFlags[FamilyMemberFlag/8] &^= 1 << (FamilyMemberFlag % 8)
		return FamilyMutationProposal{
			Action: FamilyMutationWithdraw, ActorID: actorID, ActorName: actor.Body.Name,
			FamilyID: family.ID, FamilyName: family.Name,
			BeforeFamilyID: familyID, AfterFamilyID: 0,
			BeforeFlags: beforeFlags, AfterFlags: afterFlags,
			BeforeGold: actor.Body.Gold, AfterGold: int32(int64(actor.Body.Gold) - amount),
			Fee: family.Fee, GoldTransferred: amount,
			Changed: true, Response: fmt.Sprintf("당신은 패거리에서 탈퇴를 하였습니다.\r\n\n당신은 이제 %d냥을 갖고 있습니다.", int64(actor.Body.Gold)-amount),
			before: snapshot, expectedActor: snapshot.Players[actorID], expectedFamily: familyState,
			afterFamily:     func() FamilyState { next, _, _ := familyState.removeMember(family.ID, actorID); return next }(),
			expectedCatalog: cloneFamilyCatalog(catalog),
		}, nil
	}
	afterFlags[FamilyPendingFlag/8] &^= 1 << (FamilyPendingFlag % 8)
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

func familyMutationApprovalPlan(s State, actorID, targetName string, catalog FamilyCatalog) (FamilyMutationProposal, error) {
	if err := s.Validate(); err != nil {
		return FamilyMutationProposal{}, err
	}
	if s.Family == nil {
		return FamilyMutationProposal{}, ErrFamilyMutationApprovalUnsupported
	}
	actor, family, err := familyMutationApprovalActor(s, actorID, catalog)
	if err != nil {
		return FamilyMutationProposal{}, err
	}
	targetID, target, err := familyMutationCanonicalTarget(s, actor, targetName)
	if err != nil {
		return FamilyMutationProposal{}, err
	}
	active, pending, targetFamilyID, err := familyMutationMembership(target)
	if err != nil {
		return FamilyMutationProposal{}, err
	}
	if active {
		return FamilyMutationProposal{}, ErrFamilyMutationTargetAlreadyMember
	}
	if !pending || targetFamilyID != family.ID {
		return FamilyMutationProposal{}, ErrFamilyMutationTargetNotPending
	}
	familyState, _, err := familyMutationMemberLedger(s, family.ID)
	if err != nil {
		return FamilyMutationProposal{}, err
	}
	if _, exists, memberErr := familyState.hasMember(family.ID, targetID); memberErr != nil {
		return FamilyMutationProposal{}, fmt.Errorf("%w: %v", ErrFamilyMutationMemberLedgerMissing, memberErr)
	} else if exists {
		return FamilyMutationProposal{}, ErrFamilyMutationTargetAlreadyMember
	}
	amount, err := familyMutationFeeAmount(family.Fee, familyJoinGoldMultiplier)
	if err != nil {
		return FamilyMutationProposal{}, err
	}
	if actor.Body.Gold < 0 || int64(actor.Body.Gold) < amount {
		return FamilyMutationProposal{}, ErrFamilyMutationInsufficientGold
	}
	if target.Body.Gold < 0 || int64(target.Body.Gold) > familyMaxPlayerGold-amount {
		return FamilyMutationProposal{}, ErrFamilyMutationGoldOverflow
	}
	beforeTargetFlags := target.Body.Flags
	afterTargetFlags := beforeTargetFlags
	afterTargetFlags[FamilyPendingFlag/8] &^= 1 << (FamilyPendingFlag % 8)
	afterTargetFlags[FamilyMemberFlag/8] |= 1 << (FamilyMemberFlag % 8)
	member := FamilyMember{ID: targetID, Name: target.Body.Name, Class: target.Body.Class}
	updatedFamily, err := familyState.addMember(family.ID, member)
	if err != nil {
		return FamilyMutationProposal{}, err
	}
	snapshot := s.clone()
	return FamilyMutationProposal{
		Action:  FamilyMutationApprove,
		ActorID: actorID, ActorName: actor.Body.Name,
		TargetID: targetID, TargetName: target.Body.Name,
		BossID: actorID, BossName: actor.Body.Name,
		FamilyID: family.ID, FamilyName: family.Name,
		BeforeFamilyID: targetFamilyID, AfterFamilyID: family.ID,
		BeforeFlags: actor.Body.Flags, AfterFlags: actor.Body.Flags,
		TargetBeforeFlags: beforeTargetFlags, TargetAfterFlags: afterTargetFlags,
		BeforeGold: actor.Body.Gold, AfterGold: int32(int64(actor.Body.Gold) - amount),
		TargetBeforeGold: target.Body.Gold, TargetAfterGold: int32(int64(target.Body.Gold) + amount),
		BossBeforeGold: actor.Body.Gold, BossAfterGold: int32(int64(actor.Body.Gold) - amount),
		Fee: family.Fee, GoldTransferred: amount,
		Changed: true, BossNotificationPending: false,
		Response: fmt.Sprintf("%s님의 패거리 가입을 허가하였습니다.\r\n%s님에게 가입축하금을 지급하였습니다.", target.Body.Name, target.Body.Name),
		before:   snapshot, expectedActor: snapshot.Players[actorID], expectedBoss: snapshot.Players[actorID], expectedTarget: snapshot.Players[targetID], expectedFamily: familyState, afterFamily: updatedFamily,
		expectedCatalog: cloneFamilyCatalog(catalog),
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
		if targetName == "" {
			return FamilyMutationProposal{}, ErrFamilyMutationInvalidProposal
		}
		if s.Family == nil {
			return FamilyMutationProposal{}, ErrFamilyMutationApprovalUnsupported
		}
		if familyID != 0 {
			return FamilyMutationProposal{}, ErrFamilyMutationInvalidProposal
		}
		return familyMutationApprovalPlan(s, actorID, targetName, catalog)
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

// PlanFamilyApproval plans the source boss_family approval branch once the
// canonical fee/member ledger has been imported. It resolves the target only
// after proving the actor's online PFMBOS/boss authority.
func (s State) PlanFamilyApproval(actorID, targetName string, catalog FamilyCatalog) (FamilyMutationProposal, error) {
	return s.PlanFamilyMutation(actorID, FamilyMutationApprove, 0, targetName, catalog)
}

func (s State) PlanFamilyApprove(actorID, targetName string, catalog FamilyCatalog) (FamilyMutationProposal, error) {
	return s.PlanFamilyApproval(actorID, targetName, catalog)
}

func familyMutationProposalMatches(actual, expected FamilyMutationProposal) bool {
	return actual.Action == expected.Action && actual.ActorID == expected.ActorID && actual.ActorName == expected.ActorName &&
		actual.TargetID == expected.TargetID && actual.TargetName == expected.TargetName && actual.BossID == expected.BossID && actual.BossName == expected.BossName &&
		actual.FamilyID == expected.FamilyID && actual.FamilyName == expected.FamilyName && actual.BeforeFamilyID == expected.BeforeFamilyID && actual.AfterFamilyID == expected.AfterFamilyID &&
		actual.BeforeFlags == expected.BeforeFlags && actual.AfterFlags == expected.AfterFlags && actual.TargetBeforeFlags == expected.TargetBeforeFlags && actual.TargetAfterFlags == expected.TargetAfterFlags &&
		actual.BeforeGold == expected.BeforeGold && actual.AfterGold == expected.AfterGold && actual.TargetBeforeGold == expected.TargetBeforeGold && actual.TargetAfterGold == expected.TargetAfterGold &&
		actual.BossBeforeGold == expected.BossBeforeGold && actual.BossAfterGold == expected.BossAfterGold && actual.Fee == expected.Fee && actual.GoldTransferred == expected.GoldTransferred &&
		actual.Changed == expected.Changed && actual.BossNotificationPending == expected.BossNotificationPending && actual.Response == expected.Response
}

func familyMutationResult(p FamilyMutationProposal) FamilyMutationResult {
	return FamilyMutationResult{
		Action: p.Action, ActorID: p.ActorID, ActorName: p.ActorName,
		TargetID: p.TargetID, TargetName: p.TargetName, BossID: p.BossID, BossName: p.BossName,
		FamilyID: p.FamilyID, FamilyName: p.FamilyName,
		BeforeFamilyID: p.BeforeFamilyID, AfterFamilyID: p.AfterFamilyID,
		BeforeFlags: p.BeforeFlags, AfterFlags: p.AfterFlags,
		TargetBeforeFlags: p.TargetBeforeFlags, TargetAfterFlags: p.TargetAfterFlags,
		BeforeGold: p.BeforeGold, AfterGold: p.AfterGold,
		TargetBeforeGold: p.TargetBeforeGold, TargetAfterGold: p.TargetAfterGold,
		BossBeforeGold: p.BossBeforeGold, BossAfterGold: p.BossAfterGold,
		Fee: p.Fee, GoldTransferred: p.GoldTransferred,
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
	case FamilyMutationApprove:
		fresh, err = familyMutationApprovalPlan(s, proposal.ActorID, proposal.TargetName, proposal.expectedCatalog)
	default:
		return State{}, FamilyMutationResult{}, ErrFamilyMutationInvalidProposal
	}
	if err != nil {
		return State{}, FamilyMutationResult{}, ErrFamilyMutationStaleProposal
	}
	if !familyMutationProposalMatches(proposal, fresh) || !reflect.DeepEqual(proposal.expectedActor, fresh.expectedActor) || !reflect.DeepEqual(proposal.expectedBoss, fresh.expectedBoss) || !reflect.DeepEqual(proposal.expectedTarget, fresh.expectedTarget) || !reflect.DeepEqual(proposal.expectedFamily, fresh.expectedFamily) || !reflect.DeepEqual(proposal.afterFamily, fresh.afterFamily) || !reflect.DeepEqual(proposal.expectedCatalog, fresh.expectedCatalog) {
		return State{}, FamilyMutationResult{}, ErrFamilyMutationStaleProposal
	}
	if !proposal.Changed || proposal.Response == "" {
		return State{}, FamilyMutationResult{}, ErrFamilyMutationInvalidProposal
	}
	if proposal.Action != FamilyMutationApprove && proposal.BeforeFlags == proposal.AfterFlags {
		return State{}, FamilyMutationResult{}, ErrFamilyMutationInvalidProposal
	}
	if proposal.Action == FamilyMutationApprove && proposal.TargetBeforeFlags == proposal.TargetAfterFlags {
		return State{}, FamilyMutationResult{}, ErrFamilyMutationInvalidProposal
	}

	next := s.clone()
	actor := next.Players[proposal.ActorID]
	switch proposal.Action {
	case FamilyMutationApply:
		actor.Body.Flags = proposal.AfterFlags
		actor.Body.Daily[FamilyDailySlot].Max = byte(proposal.AfterFamilyID)
		next.Players[proposal.ActorID] = actor
	case FamilyMutationWithdraw:
		actor.Body.Flags = proposal.AfterFlags
		actor.Body.Daily[FamilyDailySlot].Max = 0
		if proposal.BeforeFamilyID != 0 && proposal.BeforeFlags != proposal.AfterFlags && proposal.BeforeGold != proposal.AfterGold {
			actor.Body.Gold = proposal.AfterGold
		}
		next.Players[proposal.ActorID] = actor
		if proposal.BeforeFlags[FamilyMemberFlag/8]&(1<<(FamilyMemberFlag%8)) != 0 {
			family := proposal.afterFamily.Clone()
			next.Family = &family
		}
	case FamilyMutationApprove:
		actor.Body.Gold = proposal.BossAfterGold
		next.Players[proposal.ActorID] = actor
		target := next.Players[proposal.TargetID]
		target.Body.Flags = proposal.TargetAfterFlags
		target.Body.Daily[FamilyDailySlot].Max = byte(proposal.AfterFamilyID)
		target.Body.Gold = proposal.TargetAfterGold
		next.Players[proposal.TargetID] = target
		family := proposal.afterFamily.Clone()
		next.Family = &family
	default:
		return State{}, FamilyMutationResult{}, ErrFamilyMutationInvalidProposal
	}
	if err := next.Validate(); err != nil {
		return State{}, FamilyMutationResult{}, err
	}
	if _, _, _, err := familyMutationMembership(next.Players[proposal.ActorID]); err != nil {
		return State{}, FamilyMutationResult{}, err
	}
	if proposal.Action == FamilyMutationApprove {
		if _, _, _, err := familyMutationMembership(next.Players[proposal.TargetID]); err != nil {
			return State{}, FamilyMutationResult{}, err
		}
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
