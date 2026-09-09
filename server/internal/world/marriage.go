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

// These values are the legacy command11.c/mtype.h indexes.  Marriage is
// intentionally kept on the existing player snapshot: command11.c stores
// the pending/active bits and spouse name in creature flags/key[2], while
// the old marriage files are commented out.  A normalized relationship table
// is a later migration boundary, not an input to this reducer.
const (
	marriageRoomFlag            uint = 41 // RMARRI
	marriageMaleFlag            uint = 12 // PMALES
	marriageInvisibleFlag       uint = 2  // PINVIS
	marriageDMInvisibleFlag     uint = 10 // PDMINV
	marriageDetectInvisibleFlag uint = 21 // PDINVI
	marriageBlindFlag           uint = 42 // PBLIND
	marriageActiveFlag          uint = 60 // PMARRI
	marriagePendingFlag         uint = 61 // PRDMAR
	marriageHoursTimerIndex          = 28 // LT_HOURS
	marriageSpouseKeyIndex           = 2  // creature key[2]
	marriageMinimumAge               = 25
	marriageSpouseKeyPrefix          = "m"
	marriageSpouseKeyMaxBytes        = 19 // key[2] is a 20-byte C buffer
)

// Exported source indexes are useful to migration fixtures and keep callers
// from copying numeric flag positions into a second, potentially divergent
// contract.
const (
	MarriageRoomFlag            = marriageRoomFlag
	MarriageMaleFlag            = marriageMaleFlag
	MarriageInvisibleFlag       = marriageInvisibleFlag
	MarriageDMInvisibleFlag     = marriageDMInvisibleFlag
	MarriageDetectInvisibleFlag = marriageDetectInvisibleFlag
	MarriageBlindFlag           = marriageBlindFlag
	MarriageActiveFlag          = marriageActiveFlag
	MarriagePendingFlag         = marriagePendingFlag
	MarriageHoursTimerIndex     = marriageHoursTimerIndex
	MarriageSpouseKeyIndex      = marriageSpouseKeyIndex
	MarriageMinimumAge          = marriageMinimumAge
	MarriageSpouseKeyPrefix     = marriageSpouseKeyPrefix
)

var (
	ErrMarriageActorAbsent            = errors.New("online canonical marriage actor absent")
	ErrMarriageNotWeddingHall         = errors.New("marriage requires a wedding hall")
	ErrMarriageActorTooYoung          = errors.New("marriage actor is too young")
	ErrMarriageAlreadyMarried         = errors.New("marriage actor is already married")
	ErrMarriageTargetRequired         = errors.New("marriage target is required")
	ErrMarriageTargetUnavailable      = errors.New("marriage target is unavailable")
	ErrMarriageTargetAmbiguous        = errors.New("marriage target is ambiguous")
	ErrMarriageTargetInvisible        = errors.New("marriage target is invisible")
	ErrMarriageSameSex                = errors.New("marriage target has the same sex")
	ErrMarriageTargetTooYoung         = errors.New("marriage target is too young")
	ErrMarriageTargetMarried          = errors.New("marriage target is already married")
	ErrMarriageTargetPendingDifferent = errors.New("marriage target has a different pending proposal")
	ErrMarriageStateInvalid           = errors.New("invalid marriage state")
	ErrMarriageStaleProposal          = errors.New("stale marriage proposal")
	ErrMarriageInvalidProposal        = errors.New("invalid marriage proposal")
)

// Source-oriented aliases make the deliberately bounded command easier to
// discover without creating separate error semantics.
var (
	ErrMarriageRoom          = ErrMarriageNotWeddingHall
	ErrMarriageAge           = ErrMarriageActorTooYoung
	ErrMarriageTargetAbsent  = ErrMarriageTargetUnavailable
	ErrMarriageTargetPending = ErrMarriageTargetPendingDifferent
	ErrMarriageStale         = ErrMarriageStaleProposal
)

// MarriageAction is the closed set of state transitions admitted by this
// slice.  The source bare command cancels an actor's pending proposal; with a
// target it either creates a proposal or accepts a matching one.
type MarriageAction string

const (
	MarriageCancel  MarriageAction = "cancel"
	MarriageRequest MarriageAction = "request"
	MarriageAccept  MarriageAction = "accept"
)

const (
	MarriageCancelResponse = "결혼 신청을 취소합니다.\r\n"
)

// MarriageEvent is a post-commit recipient projection.  The event carries an
// immutable player ID and canonical display name so transport cannot resolve
// a different online player after the receipt commits.
type MarriageEvent struct {
	RecipientID   string `json:"recipient_id"`
	RecipientName string `json:"recipient_name"`
	Text          string `json:"text"`
}

// MarriageProposal binds a transition to a complete source snapshot.  The
// private fields are the authority proof; ApplyMarriage re-plans and compares
// them before cloning or mutating any state.
type MarriageProposal struct {
	Action MarriageAction

	ActorID    string
	ActorName  string
	TargetID   string
	TargetName string

	BeforeActorFlags  [8]byte
	AfterActorFlags   [8]byte
	BeforeTargetFlags [8]byte
	AfterTargetFlags  [8]byte
	ActorSpouseKey    string
	TargetSpouseKey   string
	Changed           bool
	Broadcast         bool
	BroadcastText     string
	Response          string
	Events            []MarriageEvent

	before         State
	expectedActor  PlayerState
	expectedTarget PlayerState
}

// MarriageResult is the durable receipt projection.  Events are emitted only
// by the transport after the first successful commit; replay returns these
// bytes without re-fanning out any message.
type MarriageResult struct {
	Action        MarriageAction  `json:"action"`
	ActorID       string          `json:"actor_id"`
	ActorName     string          `json:"actor_name"`
	TargetID      string          `json:"target_id,omitempty"`
	TargetName    string          `json:"target_name,omitempty"`
	Changed       bool            `json:"changed"`
	Broadcast     bool            `json:"broadcast,omitempty"`
	BroadcastText string          `json:"broadcast_text,omitempty"`
	Response      string          `json:"response"`
	Events        []MarriageEvent `json:"events,omitempty"`
}

// Compatibility aliases mirror the names used by other social reducers.
type MarriageRelationshipProposal = MarriageProposal
type MarriageRelationshipResult = MarriageResult

func marriageValidName(name string) bool {
	if name == "" || !utf8.ValidString(name) || strings.TrimSpace(name) != name {
		return false
	}
	for _, r := range name {
		if unicode.IsSpace(r) || unicode.IsControl(r) || r == '/' || r == '\\' || r == ':' || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

func marriageCanonicalPlayer(s State, id string) (PlayerState, error) {
	player, ok := s.Players[id]
	if id == "" || !ok || !player.Online || player.Body.Type != 0 {
		return PlayerState{}, ErrMarriageActorAbsent
	}
	if !marriageValidName(player.Body.Name) {
		return PlayerState{}, ErrMarriageActorAbsent
	}
	canonical, err := identity.CanonicalName(player.Body.Name)
	if err != nil || canonical != player.Body.Name {
		return PlayerState{}, ErrMarriageActorAbsent
	}
	room, ok := s.Rooms[player.Body.RoomID]
	if !ok || room.Resource.ID != player.Body.RoomID || !containsString(room.PlayerIDs, id) {
		return PlayerState{}, ErrMarriageActorAbsent
	}
	return player, nil
}

func marriageAge(body LegacyMonster) int64 {
	return 18 + int64(body.Timers[marriageHoursTimerIndex].Interval)/86400
}

func marriageFlagPair(body LegacyMonster) (active, pending bool) {
	return flag(body.Flags[:], marriageActiveFlag), flag(body.Flags[:], marriagePendingFlag)
}

func marriageSpouseName(body LegacyMonster) (string, bool) {
	key := body.Keys[marriageSpouseKeyIndex]
	if len(key) < 2 || len(key) > marriageSpouseKeyMaxBytes || !utf8.ValidString(key) || !strings.HasPrefix(key, marriageSpouseKeyPrefix) {
		return "", false
	}
	name := key[len(marriageSpouseKeyPrefix):]
	if !marriageValidName(name) {
		return "", false
	}
	canonical, err := identity.CanonicalName(name)
	if err != nil || canonical != name {
		return "", false
	}
	return name, true
}

func marriageSpouseKey(name string) (string, bool) {
	if !marriageValidName(name) || len(marriageSpouseKeyPrefix)+len(name) > marriageSpouseKeyMaxBytes {
		return "", false
	}
	return marriageSpouseKeyPrefix + name, true
}

func marriageSetFlag(flags *[8]byte, bit uint, on bool) {
	if on {
		flags[bit/8] |= 1 << (bit % 8)
	} else {
		flags[bit/8] &^= 1 << (bit % 8)
	}
}

func marriageTargetID(s State, selector string) (string, PlayerState, error) {
	if selector == "" || !marriageValidName(selector) {
		return "", PlayerState{}, ErrMarriageTargetRequired
	}
	canonical, err := identity.CanonicalName(selector)
	if err != nil {
		return "", PlayerState{}, ErrMarriageTargetUnavailable
	}
	matches := make([]string, 0, 1)
	for id, player := range s.Players {
		if id == "" || !player.Online || player.Body.Type != 0 || player.Body.Name != canonical {
			continue
		}
		if !marriageValidName(player.Body.Name) {
			return "", PlayerState{}, ErrMarriageActorAbsent
		}
		matches = append(matches, id)
	}
	if len(matches) == 0 {
		return "", PlayerState{}, ErrMarriageTargetUnavailable
	}
	if len(matches) != 1 {
		return "", PlayerState{}, ErrMarriageTargetAmbiguous
	}
	id := matches[0]
	return id, s.Players[id], nil
}

func marriageFlagsAfter(before [8]byte, active, pending bool) [8]byte {
	after := before
	marriageSetFlag(&after, marriageActiveFlag, active)
	marriageSetFlag(&after, marriagePendingFlag, pending)
	return after
}

func marriageRequestResponse(target string) string {
	return fmt.Sprintf("당신은 %s님에게 결혼을 신청하였습니다.\r\n", target)
}

func marriageAcceptResponse(target string) string {
	return fmt.Sprintf("당신은 %s님의 결혼신청을 받아들입니다.\r\n", target)
}

func marriageProposalEvents(action MarriageAction, actor, target PlayerState, actorID, targetID string) ([]MarriageEvent, bool, string) {
	switch action {
	case MarriageRequest:
		return []MarriageEvent{{RecipientID: targetID, RecipientName: target.Body.Name, Text: fmt.Sprintf("\n%s님이 당신에게 결혼을 신청합니다.\r\n", actor.Body.Name)}}, false, ""
	case MarriageAccept:
		return []MarriageEvent{{RecipientID: targetID, RecipientName: target.Body.Name, Text: fmt.Sprintf("\n%s님이 당신의 결혼신청을 받아들였습니다.\r\n", actor.Body.Name)}}, true, fmt.Sprintf("\n### %s님과 %s님이 결혼을 하였습니다.\r\n", target.Body.Name, actor.Body.Name)
	default:
		return nil, false, ""
	}
}

func marriageProposalMatches(actual, expected MarriageProposal) bool {
	if actual.Action != expected.Action || actual.ActorID != expected.ActorID || actual.ActorName != expected.ActorName || actual.TargetID != expected.TargetID || actual.TargetName != expected.TargetName || actual.BeforeActorFlags != expected.BeforeActorFlags || actual.AfterActorFlags != expected.AfterActorFlags || actual.BeforeTargetFlags != expected.BeforeTargetFlags || actual.AfterTargetFlags != expected.AfterTargetFlags || actual.ActorSpouseKey != expected.ActorSpouseKey || actual.TargetSpouseKey != expected.TargetSpouseKey || actual.Changed != expected.Changed || actual.Broadcast != expected.Broadcast || actual.BroadcastText != expected.BroadcastText || actual.Response != expected.Response || !reflect.DeepEqual(actual.Events, expected.Events) {
		return false
	}
	return true
}

func marriageResult(p MarriageProposal) MarriageResult {
	return MarriageResult{Action: p.Action, ActorID: p.ActorID, ActorName: p.ActorName, TargetID: p.TargetID, TargetName: p.TargetName, Changed: p.Changed, Broadcast: p.Broadcast, BroadcastText: p.BroadcastText, Response: p.Response, Events: append([]MarriageEvent(nil), p.Events...)}
}

// PlanMarriage ports the source marriage handler's direct state branches:
// wedding hall and age gates come first, a pending actor cancels without
// target lookup, and a target's matching m<name> key accepts a proposal.
func (s State) PlanMarriage(actorID, targetSelector string) (MarriageProposal, error) {
	if err := s.Validate(); err != nil {
		return MarriageProposal{}, err
	}
	actor, err := marriageCanonicalPlayer(s, actorID)
	if err != nil {
		return MarriageProposal{}, err
	}
	room := s.Rooms[actor.Body.RoomID]
	if !flag(room.Resource.Flags[:], marriageRoomFlag) {
		return MarriageProposal{}, ErrMarriageNotWeddingHall
	}
	if marriageAge(actor.Body) < marriageMinimumAge {
		return MarriageProposal{}, ErrMarriageActorTooYoung
	}
	active, pending := marriageFlagPair(actor.Body)
	if active && pending {
		return MarriageProposal{}, ErrMarriageStateInvalid
	}
	if pending {
		// command11.c checks PRDMAR before looking at cmnd->str[1].  Keep the
		// same cancellation boundary; an old key is irrelevant to cancel.
		snapshot := s.clone()
		proposal := MarriageProposal{
			Action: MarriageCancel, ActorID: actorID, ActorName: actor.Body.Name,
			BeforeActorFlags: actor.Body.Flags, AfterActorFlags: marriageFlagsAfter(actor.Body.Flags, active, false),
			ActorSpouseKey: actor.Body.Keys[marriageSpouseKeyIndex], Changed: true,
			Response: MarriageCancelResponse, before: snapshot, expectedActor: snapshot.Players[actorID],
		}
		return proposal, nil
	}
	if active {
		return MarriageProposal{}, ErrMarriageAlreadyMarried
	}
	if targetSelector == "" {
		return MarriageProposal{}, ErrMarriageTargetRequired
	}
	targetID, target, err := marriageTargetID(s, targetSelector)
	if err != nil {
		return MarriageProposal{}, err
	}
	if targetID == actorID {
		// The source's same-body lookup reaches the same-sex branch. Keep
		// this deterministic rather than introducing a new self target rule.
		return MarriageProposal{}, ErrMarriageSameSex
	}
	if flag(actor.Body.Flags[:], marriageBlindFlag) || flag(target.Body.Flags[:], marriageDMInvisibleFlag) || (flag(target.Body.Flags[:], marriageInvisibleFlag) && !flag(actor.Body.Flags[:], marriageDetectInvisibleFlag)) {
		return MarriageProposal{}, ErrMarriageTargetInvisible
	}
	if flag(actor.Body.Flags[:], marriageMaleFlag) == flag(target.Body.Flags[:], marriageMaleFlag) {
		return MarriageProposal{}, ErrMarriageSameSex
	}
	if marriageAge(target.Body) < marriageMinimumAge {
		return MarriageProposal{}, ErrMarriageTargetTooYoung
	}
	targetActive, targetPending := marriageFlagPair(target.Body)
	if targetActive && targetPending {
		return MarriageProposal{}, ErrMarriageStateInvalid
	}
	if targetActive {
		return MarriageProposal{}, ErrMarriageTargetMarried
	}

	action := MarriageRequest
	response := marriageRequestResponse(target.Body.Name)
	actorKey, ok := marriageSpouseKey(target.Body.Name)
	if !ok {
		return MarriageProposal{}, ErrMarriageStateInvalid
	}
	targetKey := ""
	if targetPending {
		spouse, valid := marriageSpouseName(target.Body)
		if !valid || spouse != actor.Body.Name {
			return MarriageProposal{}, ErrMarriageTargetPendingDifferent
		}
		action = MarriageAccept
		response = marriageAcceptResponse(target.Body.Name)
		targetKey, ok = marriageSpouseKey(actor.Body.Name)
		if !ok {
			return MarriageProposal{}, ErrMarriageStateInvalid
		}
	}
	actorAfter := marriageFlagsAfter(actor.Body.Flags, action == MarriageAccept, action == MarriageRequest)
	targetAfter := target.Body.Flags
	if action == MarriageAccept {
		targetAfter = marriageFlagsAfter(target.Body.Flags, true, false)
	}
	events, broadcast, broadcastText := marriageProposalEvents(action, actor, target, actorID, targetID)
	snapshot := s.clone()
	return MarriageProposal{
		Action: action, ActorID: actorID, ActorName: actor.Body.Name, TargetID: targetID, TargetName: target.Body.Name,
		BeforeActorFlags: actor.Body.Flags, AfterActorFlags: actorAfter, BeforeTargetFlags: target.Body.Flags, AfterTargetFlags: targetAfter,
		ActorSpouseKey: actorKey, TargetSpouseKey: targetKey, Changed: true, Broadcast: broadcast, BroadcastText: broadcastText,
		Response: response, Events: events, before: snapshot, expectedActor: snapshot.Players[actorID], expectedTarget: snapshot.Players[targetID],
	}, nil
}

// ApplyMarriage atomically applies a plan to a cloned State after proving it
// still describes the exact source snapshot. No notification is sent here.
func (s State) ApplyMarriage(proposal MarriageProposal) (State, MarriageResult, error) {
	if proposal.ActorID == "" || proposal.before.Version == 0 || !reflect.DeepEqual(s, proposal.before) {
		return State{}, MarriageResult{}, ErrMarriageStaleProposal
	}
	if !proposal.Changed || proposal.Response == "" || proposal.Action == "" {
		return State{}, MarriageResult{}, ErrMarriageInvalidProposal
	}
	fresh, err := s.PlanMarriage(proposal.ActorID, proposal.TargetName)
	if err != nil || !marriageProposalMatches(proposal, fresh) || !reflect.DeepEqual(proposal.expectedActor, fresh.expectedActor) || !reflect.DeepEqual(proposal.expectedTarget, fresh.expectedTarget) {
		return State{}, MarriageResult{}, ErrMarriageStaleProposal
	}
	next := s.clone()
	actor := next.Players[proposal.ActorID]
	actor.Body.Flags = proposal.AfterActorFlags
	switch proposal.Action {
	case MarriageCancel:
		// C only clears PRDMAR here; the legacy key slot is overwritten by a
		// later request and is intentionally preserved during cancellation.
	case MarriageRequest:
		actor.Body.Keys[marriageSpouseKeyIndex] = proposal.ActorSpouseKey
	case MarriageAccept:
		actor.Body.Keys[marriageSpouseKeyIndex] = proposal.ActorSpouseKey
		target := next.Players[proposal.TargetID]
		target.Body.Flags = proposal.AfterTargetFlags
		target.Body.Keys[marriageSpouseKeyIndex] = proposal.TargetSpouseKey
		next.Players[proposal.TargetID] = target
	default:
		return State{}, MarriageResult{}, ErrMarriageInvalidProposal
	}
	next.Players[proposal.ActorID] = actor
	if err := next.Validate(); err != nil {
		return State{}, MarriageResult{}, err
	}
	return next, marriageResult(proposal), nil
}

// Package-level wrappers match the other world reducers' explicit-state API.
func PlanMarriage(s State, actorID, targetSelector string) (MarriageProposal, error) {
	return s.PlanMarriage(actorID, targetSelector)
}

func ApplyMarriage(s State, proposal MarriageProposal) (State, MarriageResult, error) {
	return s.ApplyMarriage(proposal)
}
