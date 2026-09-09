package world

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/1XP-Inc/muhan-mud/server/internal/identity"
)

// command11.c keeps PRDDIV next to the marriage bits in mtype.h.  It is
// deliberately not added to marriage.go's MarriageProposal/MarriageResult:
// those types are the already-admitted 결혼 command contract, while this file
// is the follow-up divorce/사랑말 boundary.
const marriageDivorcePendingFlag uint = 40 // PRDDIV (src/mtype.h)

// MarriageDivorcePendingFlag is exported for migration fixtures and callers
// that need to set the source bit without copying its numeric value.
const MarriageDivorcePendingFlag = marriageDivorcePendingFlag

// MarriageDivorceNoBroadcastFlag is PNOBRD from command11.c's broadcast()
// path.  Divorce acceptance uses the ordinary global broadcast projection,
// so transport must honor this per-player opt-out exactly as the legacy loop
// does rather than sending to every connected descriptor unconditionally.
const MarriageDivorceNoBroadcastFlag = broadcastNoChatFlag

var (
	ErrDivorceActorAbsent            = errors.New("online canonical divorce actor absent")
	ErrDivorceStateInvalid           = errors.New("invalid canonical divorce state")
	ErrDivorceSpouseKeyInvalid       = errors.New("invalid canonical spouse key")
	ErrDivorceSpouseUnavailable      = errors.New("canonical spouse is unavailable")
	ErrDivorceSpouseAmbiguous        = errors.New("canonical spouse identity is ambiguous")
	ErrDivorcePlayerStoreUnavailable = errors.New("legacy player-store load is unavailable")
	ErrDivorceStaleProposal          = errors.New("stale divorce proposal")
	ErrDivorceInvalidProposal        = errors.New("invalid divorce proposal")
	ErrMarriageSendActorAbsent       = errors.New("online canonical marriage-send actor absent")
	ErrMarriageSendNotMarried        = errors.New("marriage-send actor is not married")
	ErrMarriageSendStateInvalid      = errors.New("invalid canonical marriage-send state")
	ErrMarriageSendSpouseUnavailable = errors.New("marriage-send spouse is unavailable")
	ErrMarriageSendSpouseAmbiguous   = errors.New("marriage-send spouse identity is ambiguous")
	ErrMarriageSendMessageEmpty      = errors.New("marriage-send message is empty")
	ErrMarriageSendMessageInvalid    = errors.New("invalid marriage-send message")
	ErrMarriageSendPLECHOUnsupported = errors.New("marriage-send PLECHO exact echo is unavailable")
	ErrMarriageSendDescriptorFormat  = errors.New("marriage-send descriptor formatting is unavailable")
	ErrMarriageSendStaleProposal     = errors.New("stale marriage-send proposal")
	ErrMarriageSendInvalidProposal   = errors.New("invalid marriage-send proposal")
)

// Source-oriented aliases keep adapters from having to know whether an
// unavailable spouse came from the online lookup or the unported load_ply
// branch.  No alias changes the error semantics.
var (
	ErrDivorceSpouseOffline      = ErrDivorceSpouseUnavailable
	ErrDivorceSpouseAbsent       = ErrDivorceSpouseUnavailable
	ErrDivorceLegacyPlayerStore  = ErrDivorcePlayerStoreUnavailable
	ErrDivorcePlayerStoreLoad    = ErrDivorcePlayerStoreUnavailable
	ErrDivorceStale              = ErrDivorceStaleProposal
	ErrDivorceInvalid            = ErrDivorceInvalidProposal
	ErrMarriageSendSpouseOffline = ErrMarriageSendSpouseUnavailable
	ErrMarriageSendSpouseAbsent  = ErrMarriageSendSpouseUnavailable
	ErrMarriageSendDescriptor    = ErrMarriageSendDescriptorFormat
	ErrMarriageSendPLECHO        = ErrMarriageSendPLECHOUnsupported
	ErrMarriageSendStale         = ErrMarriageSendStaleProposal
	ErrMarriageSendInvalid       = ErrMarriageSendInvalidProposal
)

// DivorceAction is the closed action set of command11.c:divorce.  The first
// three actions are no-target branches and intentionally do not resolve the
// spouse.  Request and Accept are the only branches that can touch a second
// canonical player.
type DivorceAction string

const (
	DivorceCancelMarriage DivorceAction = "cancel-marriage"
	DivorceUnmarried      DivorceAction = "unmarried"
	DivorceCancelRequest  DivorceAction = "cancel-divorce"
	DivorceRequest        DivorceAction = "request"
	DivorceAccept         DivorceAction = "accept"
)

// Compatibility spellings make the source branch names discoverable without
// introducing a second action enum or colliding with MarriageAction.
const (
	MarriageDivorceCancelMarriage = DivorceCancelMarriage
	MarriageDivorceUnmarried      = DivorceUnmarried
	MarriageDivorceCancelRequest  = DivorceCancelRequest
	MarriageDivorceRequest        = DivorceRequest
	MarriageDivorceAccept         = DivorceAccept
	DivorceCancelPendingMarriage  = DivorceCancelMarriage
	DivorceCancelPendingRequest   = DivorceCancelRequest
)

const (
	divorceCancelMarriageResponse = MarriageCancelResponse
	divorceUnmarriedResponse      = "당신은 결혼하지 않았습니다.\r\n"
	divorceCancelResponse         = "이혼 신청을 취소합니다.\r\n"
)

// DivorceEvent is a post-commit projection.  RecipientID/RecipientName are
// fixed at plan time; a global event has Broadcast=true and an empty
// RecipientID.  The reducer never sends this event to a connection.
type DivorceEvent struct {
	Kind          string `json:"kind"`
	Broadcast     bool   `json:"broadcast,omitempty"`
	RecipientID   string `json:"recipient_id,omitempty"`
	RecipientName string `json:"recipient_name,omitempty"`
	ActorID       string `json:"actor_id"`
	ActorName     string `json:"actor_name"`
	TargetID      string `json:"target_id,omitempty"`
	TargetName    string `json:"target_name,omitempty"`
	Text          string `json:"text"`
}

// DivorceProposal is an immutable candidate for one command11.c:divorce
// branch.  The public fields are receipt diagnostics; private copies bind
// ApplyDivorce to the complete State and exact player snapshots observed by
// PlanDivorce.  In particular, an actor/target name is never re-resolved by
// map order during apply.
type DivorceProposal struct {
	Action     DivorceAction
	ActorID    string
	ActorName  string
	TargetID   string
	TargetName string

	BeforeActorFlags  [8]byte
	AfterActorFlags   [8]byte
	BeforeTargetFlags [8]byte
	AfterTargetFlags  [8]byte
	BeforeActorKey    string
	AfterActorKey     string
	BeforeTargetKey   string
	AfterTargetKey    string

	Changed       bool
	Broadcast     bool
	BroadcastText string
	Response      string
	Events        []DivorceEvent

	before         State
	expectedActor  PlayerState
	expectedTarget PlayerState
}

// MarriageDivorceProposal is a descriptive alias for callers that keep
// divorce under the marriage domain.
type MarriageDivorceProposal = DivorceProposal

// DivorceResult is the durable receipt projection. Events are data only: the
// command owner may publish them after a first successful commit, but replay
// must return the saved bytes without re-resolving connections.
type DivorceResult struct {
	Action     DivorceAction `json:"action"`
	ActorID    string        `json:"actor_id"`
	ActorName  string        `json:"actor_name"`
	TargetID   string        `json:"target_id,omitempty"`
	TargetName string        `json:"target_name,omitempty"`

	BeforeActorFlags  [8]byte `json:"before_actor_flags"`
	AfterActorFlags   [8]byte `json:"after_actor_flags"`
	BeforeTargetFlags [8]byte `json:"before_target_flags"`
	AfterTargetFlags  [8]byte `json:"after_target_flags"`
	BeforeActorKey    string  `json:"before_actor_key,omitempty"`
	AfterActorKey     string  `json:"after_actor_key,omitempty"`
	BeforeTargetKey   string  `json:"before_target_key,omitempty"`
	AfterTargetKey    string  `json:"after_target_key,omitempty"`

	Changed       bool           `json:"changed"`
	Broadcast     bool           `json:"broadcast,omitempty"`
	BroadcastText string         `json:"broadcast_text,omitempty"`
	Response      string         `json:"response"`
	Events        []DivorceEvent `json:"events,omitempty"`
}

// MarriageDivorceResult is a descriptive alias for the durable receipt.
type MarriageDivorceResult = DivorceResult

func divorceResult(p DivorceProposal) DivorceResult {
	return DivorceResult{
		Action: p.Action, ActorID: p.ActorID, ActorName: p.ActorName,
		TargetID: p.TargetID, TargetName: p.TargetName,
		BeforeActorFlags: p.BeforeActorFlags, AfterActorFlags: p.AfterActorFlags,
		BeforeTargetFlags: p.BeforeTargetFlags, AfterTargetFlags: p.AfterTargetFlags,
		BeforeActorKey: p.BeforeActorKey, AfterActorKey: p.AfterActorKey,
		BeforeTargetKey: p.BeforeTargetKey, AfterTargetKey: p.AfterTargetKey,
		Changed: p.Changed, Broadcast: p.Broadcast, BroadcastText: p.BroadcastText,
		Response: p.Response, Events: append([]DivorceEvent(nil), p.Events...),
	}
}

func divorceFlagPair(body LegacyMonster) (active, pendingMarriage, pendingDivorce bool) {
	active, pendingMarriage = marriageFlagPair(body)
	return active, pendingMarriage, flag(body.Flags[:], marriageDivorcePendingFlag)
}

func divorceFlagsAfter(before [8]byte, active, pendingMarriage, pendingDivorce bool) [8]byte {
	after := marriageFlagsAfter(before, active, pendingMarriage)
	marriageSetFlag(&after, marriageDivorcePendingFlag, pendingDivorce)
	return after
}

func validDivorceName(name string) bool {
	if !marriageValidName(name) {
		return false
	}
	canonical, err := identity.CanonicalName(name)
	return err == nil && canonical == name
}

// divorceActor is stricter than a map lookup: source find_who operates on a
// live PLAYER descriptor, so State must prove online canonical room
// membership before a candidate can be applied.
func divorceActor(s State, actorID string) (PlayerState, error) {
	actor, err := marriageCanonicalPlayer(s, actorID)
	if err != nil {
		return PlayerState{}, ErrDivorceActorAbsent
	}
	if !validDivorceName(actor.Body.Name) {
		return PlayerState{}, ErrDivorceActorAbsent
	}
	return actor, nil
}

// divorceSpouseName reads the exact m<name> key[2] relation written by
// marriage.  A missing or malformed key is not treated as an unmarried
// character: active PMARRI without a canonical spouse is unsafe to mutate.
func divorceSpouseName(body LegacyMonster) (string, error) {
	name, ok := marriageSpouseName(body)
	if !ok || !validDivorceName(name) {
		return "", ErrDivorceSpouseKeyInvalid
	}
	return name, nil
}

// divorceOnlineSpouse resolves the source find_who branch against canonical
// State. It deliberately does not call a player-store reader. When no online
// identity is present, the legacy load_ply distinction (not found vs read
// error vs offline record) is unavailable and callers must fail closed.
func divorceOnlineSpouse(s State, actorID, spouseName string) (string, PlayerState, error) {
	matches := make([]string, 0, 1)
	for id, player := range s.Players {
		if id == "" || id == actorID || !player.Online || player.Body.Type != 0 || player.Body.Name != spouseName {
			continue
		}
		if !validDivorceName(player.Body.Name) {
			return "", PlayerState{}, ErrDivorceStateInvalid
		}
		matches = append(matches, id)
	}
	if len(matches) == 0 {
		return "", PlayerState{}, fmt.Errorf("%w: %w", ErrDivorceSpouseUnavailable, ErrDivorcePlayerStoreUnavailable)
	}
	if len(matches) != 1 {
		return "", PlayerState{}, ErrDivorceSpouseAmbiguous
	}
	id := matches[0]
	return id, s.Players[id], nil
}

func divorceProposalMatches(actual, expected DivorceProposal) bool {
	return actual.Action == expected.Action &&
		actual.ActorID == expected.ActorID && actual.ActorName == expected.ActorName &&
		actual.TargetID == expected.TargetID && actual.TargetName == expected.TargetName &&
		actual.BeforeActorFlags == expected.BeforeActorFlags && actual.AfterActorFlags == expected.AfterActorFlags &&
		actual.BeforeTargetFlags == expected.BeforeTargetFlags && actual.AfterTargetFlags == expected.AfterTargetFlags &&
		actual.BeforeActorKey == expected.BeforeActorKey && actual.AfterActorKey == expected.AfterActorKey &&
		actual.BeforeTargetKey == expected.BeforeTargetKey && actual.AfterTargetKey == expected.AfterTargetKey &&
		actual.Changed == expected.Changed && actual.Broadcast == expected.Broadcast &&
		actual.BroadcastText == expected.BroadcastText && actual.Response == expected.Response &&
		reflect.DeepEqual(actual.Events, expected.Events)
}

func divorceRequestEvent(actorID, actorName, targetID, targetName string) DivorceEvent {
	return DivorceEvent{
		Kind: "spouse", RecipientID: targetID, RecipientName: targetName,
		ActorID: actorID, ActorName: actorName, TargetID: targetID, TargetName: targetName,
		Text: fmt.Sprintf("\n%s님이 당신에게 이혼신청을 하였습니다.\r\n", actorName),
	}
}

func divorceAcceptEvents(actorID, actorName, targetID, targetName string) ([]DivorceEvent, string) {
	broadcast := fmt.Sprintf("\n### %s님과 %s님이 이혼을 하였습니다.\r\n", actorName, targetName)
	return []DivorceEvent{
		{
			Kind: "spouse", RecipientID: targetID, RecipientName: targetName,
			ActorID: actorID, ActorName: actorName, TargetID: targetID, TargetName: targetName,
			Text: fmt.Sprintf("\n%s님이 당신의 이혼신청을 받아들였습니다.\r\n", actorName),
		},
		{
			Kind: "broadcast", Broadcast: true,
			ActorID: actorID, ActorName: actorName, TargetID: targetID, TargetName: targetName,
			Text: broadcast,
		},
	}, broadcast
}

// PlanDivorce ports the source ordering in command11.c:1291-1360. Pending
// marriage cancellation happens before spouse lookup; unmarried and pending
// divorce cancellation are durable no-op/toggle candidates. The source's
// load_ply branch is intentionally absent from State, so an offline/missing
// spouse is an explicit fail-closed error rather than a guessed mutation.
func (s State) PlanDivorce(actorID string) (DivorceProposal, error) {
	if err := s.Validate(); err != nil {
		return DivorceProposal{}, err
	}
	actor, err := divorceActor(s, actorID)
	if err != nil {
		return DivorceProposal{}, err
	}
	active, pendingMarriage, pendingDivorce := divorceFlagPair(actor.Body)
	if active && pendingMarriage {
		return DivorceProposal{}, ErrDivorceStateInvalid
	}

	snapshot := s.clone()
	base := DivorceProposal{
		ActorID: actorID, ActorName: actor.Body.Name,
		BeforeActorFlags: actor.Body.Flags, BeforeActorKey: actor.Body.Keys[marriageSpouseKeyIndex],
		AfterActorKey: actor.Body.Keys[marriageSpouseKeyIndex], before: snapshot,
		expectedActor: snapshot.Players[actorID],
	}
	if pendingMarriage {
		base.Action = DivorceCancelMarriage
		base.AfterActorFlags = divorceFlagsAfter(actor.Body.Flags, active, false, pendingDivorce)
		base.Changed = true
		base.Response = divorceCancelMarriageResponse
		return base, nil
	}
	if !active {
		base.Action = DivorceUnmarried
		base.AfterActorFlags = actor.Body.Flags
		base.Changed = false
		base.Response = divorceUnmarriedResponse
		return base, nil
	}
	if pendingDivorce {
		base.Action = DivorceCancelRequest
		base.AfterActorFlags = divorceFlagsAfter(actor.Body.Flags, true, false, false)
		base.Changed = true
		base.Response = divorceCancelResponse
		return base, nil
	}

	spouseName, err := divorceSpouseName(actor.Body)
	if err != nil {
		return DivorceProposal{}, err
	}
	targetID, target, err := divorceOnlineSpouse(s, actorID, spouseName)
	if err != nil {
		return DivorceProposal{}, err
	}
	targetActive, targetPendingMarriage, targetPendingDivorce := divorceFlagPair(target.Body)
	if !targetActive || targetPendingMarriage {
		return DivorceProposal{}, ErrDivorceStateInvalid
	}
	targetSpouseName, err := divorceSpouseName(target.Body)
	if err != nil || targetSpouseName != actor.Body.Name {
		return DivorceProposal{}, ErrDivorceStateInvalid
	}
	if target.Body.Keys[marriageSpouseKeyIndex] == "" {
		return DivorceProposal{}, ErrDivorceStateInvalid
	}

	base.TargetID, base.TargetName = targetID, target.Body.Name
	base.BeforeTargetFlags, base.BeforeTargetKey = target.Body.Flags, target.Body.Keys[marriageSpouseKeyIndex]
	base.AfterTargetFlags = target.Body.Flags
	base.AfterTargetKey = target.Body.Keys[marriageSpouseKeyIndex]
	base.expectedTarget = snapshot.Players[targetID]
	if !targetPendingDivorce {
		base.Action = DivorceRequest
		base.AfterActorFlags = divorceFlagsAfter(actor.Body.Flags, true, false, true)
		base.AfterTargetFlags = target.Body.Flags
		base.Changed = true
		base.Response = fmt.Sprintf("당신은 당신의 배우자에게 이혼신청을 하였습니다.\r\n")
		base.Events = []DivorceEvent{divorceRequestEvent(actorID, actor.Body.Name, targetID, target.Body.Name)}
		return base, nil
	}

	base.Action = DivorceAccept
	base.AfterActorFlags = divorceFlagsAfter(actor.Body.Flags, false, false, false)
	base.AfterTargetFlags = divorceFlagsAfter(target.Body.Flags, false, false, false)
	base.Changed = true
	base.Response = fmt.Sprintf("당신은 %s님의 이혼신청을 받아들입니다.\r\n", target.Body.Name)
	base.Events, base.BroadcastText = divorceAcceptEvents(actorID, actor.Body.Name, targetID, target.Body.Name)
	base.Broadcast = true
	return base, nil
}

// ApplyDivorce re-plans against the exact before snapshot and then mutates a
// clone. It never sends events; the result is the sole notification source.
func (s State) ApplyDivorce(proposal DivorceProposal) (State, DivorceResult, error) {
	if proposal.ActorID == "" || proposal.before.Version == 0 || !reflect.DeepEqual(s, proposal.before) {
		return State{}, DivorceResult{}, ErrDivorceStaleProposal
	}
	if proposal.Action == "" || proposal.Response == "" {
		return State{}, DivorceResult{}, ErrDivorceInvalidProposal
	}
	fresh, err := s.PlanDivorce(proposal.ActorID)
	if err != nil {
		return State{}, DivorceResult{}, err
	}
	if !divorceProposalMatches(proposal, fresh) || !reflect.DeepEqual(proposal.expectedActor, fresh.expectedActor) || !reflect.DeepEqual(proposal.expectedTarget, fresh.expectedTarget) {
		return State{}, DivorceResult{}, ErrDivorceStaleProposal
	}
	next := s.clone()
	actor := next.Players[proposal.ActorID]
	actor.Body.Flags = proposal.AfterActorFlags
	next.Players[proposal.ActorID] = actor
	if proposal.TargetID != "" {
		target, ok := next.Players[proposal.TargetID]
		if !ok || target.Body.Name != proposal.TargetName {
			return State{}, DivorceResult{}, ErrDivorceStaleProposal
		}
		target.Body.Flags = proposal.AfterTargetFlags
		next.Players[proposal.TargetID] = target
	}
	if err := next.Validate(); err != nil {
		return State{}, DivorceResult{}, err
	}
	return next, divorceResult(proposal), nil
}

// Package-level wrappers match the other world reducers' explicit-state API.
func PlanDivorce(s State, actorID string) (DivorceProposal, error) {
	return s.PlanDivorce(actorID)
}

func ApplyDivorce(s State, proposal DivorceProposal) (State, DivorceResult, error) {
	return s.ApplyDivorce(proposal)
}

// MarriageSendAction is kept separate from MailSend and the existing
// MarriageAction. The source command is an instantaneous spouse message and
// has no canonical durable body or mailbox mutation.
type MarriageSendAction string

const MarriageSend MarriageSendAction = "send"

// MarriageSendEvent is intentionally semantic. The current Go State carries
// canonical IDs/names but not the C descriptor/title/particle formatter, so a
// future transport may render it only after that contract is admitted.
type MarriageSendEvent struct {
	RecipientID   string `json:"recipient_id"`
	RecipientName string `json:"recipient_name"`
	ActorID       string `json:"actor_id"`
	ActorName     string `json:"actor_name"`
	Message       string `json:"message"`
	Text          string `json:"text,omitempty"`
}

// MarriageSendProposal binds the proof portion of m_send to one snapshot.
// PlanMarriageSend currently fails closed after these checks because its
// successful actor echo and recipient output require unported descriptor
// formatting. Keeping the proposal type lets a future renderer admit the
// same identity/snapshot contract without changing the receipt shape.
type MarriageSendProposal struct {
	Action           MarriageSendAction
	ActorID          string
	ActorName        string
	TargetID         string
	TargetName       string
	Message          string
	Response         string
	Delivered        bool
	BeforeActorFlags [8]byte
	BeforeActorKey   string

	before         State
	expectedActor  PlayerState
	expectedTarget PlayerState
}

type SpouseMessageProposal = MarriageSendProposal

// MarriageSendResult is the future receipt projection. No network send is
// performed by this package; the current admitted outcomes are no-op errors
// until descriptor/title and PLECHO exact echo contracts are present.
type MarriageSendResult struct {
	Action     MarriageSendAction `json:"action"`
	ActorID    string             `json:"actor_id"`
	ActorName  string             `json:"actor_name"`
	TargetID   string             `json:"target_id"`
	TargetName string             `json:"target_name"`
	Message    string             `json:"message"`
	Response   string             `json:"response"`
	Delivered  bool               `json:"delivered"`
	Event      *MarriageSendEvent `json:"event,omitempty"`
}

type SpouseMessageResult = MarriageSendResult

const maxMarriageSendMessageBytes = 255 // command11.c fullstr[256], NUL reserved

// MaxMarriageSendMessageBytes exposes the source buffer boundary to terminal
// adapters without allowing them to bypass validMarriageSendMessage.
const MaxMarriageSendMessageBytes = maxMarriageSendMessageBytes

func validMarriageSendMessage(message string) error {
	if strings.TrimSpace(message) == "" {
		return ErrMarriageSendMessageEmpty
	}
	if !utf8.ValidString(message) {
		return ErrMarriageSendMessageInvalid
	}
	if len(message) > maxMarriageSendMessageBytes {
		return ErrMarriageSendMessageInvalid
	}
	for _, r := range message {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return ErrMarriageSendMessageInvalid
		}
	}
	return nil
}

func marriageSendActor(s State, actorID string) (PlayerState, error) {
	actor, err := divorceActor(s, actorID)
	if err != nil {
		return PlayerState{}, ErrMarriageSendActorAbsent
	}
	return actor, nil
}

func marriageSendOnlineSpouse(s State, actorID, spouseName string) (string, PlayerState, error) {
	id, target, err := divorceOnlineSpouse(s, actorID, spouseName)
	if err != nil {
		if errors.Is(err, ErrDivorceSpouseAmbiguous) {
			return "", PlayerState{}, ErrMarriageSendSpouseAmbiguous
		}
		return "", PlayerState{}, ErrMarriageSendSpouseUnavailable
	}
	return id, target, nil
}

// PlanMarriageSend ports the canonical proof gates of command11.c:m_send
// (1247-1285): PMARRI, m<spouse> key[2], online spouse and non-empty bounded
// text. The C actor echo uses %M and the recipient projection uses %C/%M/%j;
// neither descriptor formatter exists in State. PLECHO is therefore rejected
// explicitly, and even the non-PLECHO success path remains fail-closed until
// a renderer can prove both outputs. No proposal or notification is emitted.
func (s State) PlanMarriageSend(actorID, message string) (MarriageSendProposal, error) {
	if err := s.Validate(); err != nil {
		return MarriageSendProposal{}, err
	}
	actor, err := marriageSendActor(s, actorID)
	if err != nil {
		return MarriageSendProposal{}, err
	}
	if !flag(actor.Body.Flags[:], marriageActiveFlag) {
		return MarriageSendProposal{}, ErrMarriageSendNotMarried
	}
	spouseName, err := divorceSpouseName(actor.Body)
	if err != nil {
		return MarriageSendProposal{}, ErrMarriageSendStateInvalid
	}
	targetID, target, err := marriageSendOnlineSpouse(s, actorID, spouseName)
	if err != nil {
		return MarriageSendProposal{}, err
	}
	if !flag(target.Body.Flags[:], marriageActiveFlag) {
		return MarriageSendProposal{}, ErrMarriageSendStateInvalid
	}
	targetSpouse, err := divorceSpouseName(target.Body)
	if err != nil || targetSpouse != actor.Body.Name {
		return MarriageSendProposal{}, ErrMarriageSendStateInvalid
	}
	if err := validMarriageSendMessage(message); err != nil {
		return MarriageSendProposal{}, err
	}
	if flag(actor.Body.Flags[:], playerLocalEchoFlag) {
		return MarriageSendProposal{}, ErrMarriageSendPLECHOUnsupported
	}
	// Even with PLECHO disabled, m_send's recipient line contains C
	// descriptor directives (%C/%M/%j/%D). Returning a plain approximation
	// would make replay bytes dependent on an unproven renderer.
	_ = targetID
	_ = target
	return MarriageSendProposal{}, ErrMarriageSendDescriptorFormat
}

// ApplyMarriageSend is provided for the eventual renderer-backed admission.
// Hand-built proposals are rejected unless all private proof fields are
// present; the current planner never returns one because formatting is not
// canonical yet.
func (s State) ApplyMarriageSend(proposal MarriageSendProposal) (State, MarriageSendResult, error) {
	if proposal.Action != MarriageSend || proposal.ActorID == "" || proposal.TargetID == "" || proposal.Message == "" || proposal.before.Version == 0 || !reflect.DeepEqual(s, proposal.before) {
		return State{}, MarriageSendResult{}, ErrMarriageSendInvalidProposal
	}
	fresh, err := s.PlanMarriageSend(proposal.ActorID, proposal.Message)
	if err != nil {
		return State{}, MarriageSendResult{}, err
	}
	if !reflect.DeepEqual(proposal, fresh) || !reflect.DeepEqual(proposal.expectedActor, fresh.expectedActor) || !reflect.DeepEqual(proposal.expectedTarget, fresh.expectedTarget) {
		return State{}, MarriageSendResult{}, ErrMarriageSendStaleProposal
	}
	return s, MarriageSendResult{
		Action: MarriageSend, ActorID: proposal.ActorID, ActorName: proposal.ActorName,
		TargetID: proposal.TargetID, TargetName: proposal.TargetName, Message: proposal.Message,
		Response: proposal.Response, Delivered: proposal.Delivered,
	}, nil
}

func PlanMarriageSend(s State, actorID, message string) (MarriageSendProposal, error) {
	return s.PlanMarriageSend(actorID, message)
}

func ApplyMarriageSend(s State, proposal MarriageSendProposal) (State, MarriageSendResult, error) {
	return s.ApplyMarriageSend(proposal)
}
