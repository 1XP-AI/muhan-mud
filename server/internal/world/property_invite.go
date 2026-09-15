package world

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The invite command owns the legacy marriage-house file invite_N.  These
// values are deliberately kept local to this bounded slice: they are source
// indexes, not a second permission system.
const (
	propertyInviteDailyIndex        = 8  // DL_MARRI
	propertyInviteRoomFlag          = 40 // RONMAR
	maxPropertyInvitations          = 10
	propertyInviteNameMaxBytes      = 14 // PLAYER_NAME_MAX_BYTES
	propertyInviteNameMaxCodepoints = 12
	propertyInviteInvisibleFlag     = 2  // PINVIS
	propertyInviteDMInvisibleFlag   = 10 // PDMINV
	propertyInviteDetectFlag        = 21 // PDINVI
	propertyInviteSubDMClass        = 11 // SUB_DM
	propertyInviteDMClass           = 12 // DM
)

// Exported aliases make the source boundaries available to adapters and
// tests without making callers depend on the private legacy names.
const (
	PropertyInviteDailyIndex        = propertyInviteDailyIndex
	PropertyInviteRoomFlag          = propertyInviteRoomFlag
	PropertyInviteMaxInvitations    = maxPropertyInvitations
	MaxPropertyInvitations          = maxPropertyInvitations
	PropertyInviteNameMaxBytes      = propertyInviteNameMaxBytes
	MaxPropertyInviteNameBytes      = propertyInviteNameMaxBytes
	PropertyInviteNameMaxCodePoints = propertyInviteNameMaxCodepoints
	MaxPropertyInviteNameCodePoints = propertyInviteNameMaxCodepoints
)

var (
	// ErrPropertyInvitationsUnmigrated distinguishes a nil invitation map from
	// a confirmed empty map.  No command may invent an empty map at runtime.
	ErrPropertyInvitationsUnmigrated = errors.New("property invitations are not migrated")
	ErrPropertyInviteActorAbsent     = errors.New("online property-invite actor absent")
	ErrPropertyInviteNotHome         = errors.New("property invite requires a marriage-house room")
	ErrPropertyInviteNoProperty      = errors.New("property invite permission is absent")
	ErrPropertyInviteNameRequired    = errors.New("property invite target name is required")
	ErrPropertyInviteNameInvalid     = errors.New("property invite target name is invalid")
	ErrPropertyInviteNameTooLong     = errors.New("property invite target name exceeds the byte limit")
	ErrPropertyInviteTargetMissing   = errors.New("property invite target is unavailable")
	ErrPropertyInviteTargetAmbiguous = errors.New("property invite target is ambiguous")
	ErrPropertyInviteTargetInvisible = errors.New("property invite target is invisible")
	ErrPropertyInviteSelf            = errors.New("property invite cannot target the actor")
	ErrPropertyInviteLimit           = errors.New("property invitation limit reached")
	ErrPropertyInviteInvalidProposal = errors.New("invalid property invite proposal")
	ErrPropertyInviteStaleProposal   = errors.New("stale property invite proposal")
)

// Compatibility spellings keep source-oriented callers from having to know
// whether the migration state or command boundary supplied an error.
var (
	ErrPropertyInviteStateUnmigrated = ErrPropertyInvitationsUnmigrated
	ErrPropertyInviteTargetAbsent    = ErrPropertyInviteTargetMissing
	ErrPropertyInviteCapacity        = ErrPropertyInviteLimit
	ErrPropertyInviteStale           = ErrPropertyInviteStaleProposal
)

// PropertyInviteAction is the closed set of effects admitted by invite_N.
// Listing is read-only and is exposed separately from the toggle proposal.
type PropertyInviteAction string

const (
	PropertyInviteAdd    PropertyInviteAction = "add"
	PropertyInviteRemove PropertyInviteAction = "remove"
	PropertyInviteList   PropertyInviteAction = "list"
)

// PropertyInviteResult is the deterministic receipt projection. Target and
// Property are canonical audit values (ID and property number); the explicit
// TargetName/PropertyID fields make the display projection unambiguous. The
// ordered ID slice is never sorted or reconstructed from map iteration.
type PropertyInviteResult struct {
	Action           PropertyInviteAction `json:"action"`
	ActorID          string               `json:"actor_id"`
	Target           string               `json:"target"`
	TargetID         string               `json:"target_id,omitempty"`
	TargetName       string               `json:"target_name,omitempty"`
	Property         int16                `json:"property"`
	PropertyID       int16                `json:"property_id,omitempty"`
	OrderedInviteIDs []string             `json:"ordered_invite_ids"`
	InviteIDs        []string             `json:"-"`
	InviteNames      []string             `json:"invite_names,omitempty"`
	Changed          bool                 `json:"changed"`
	Response         string               `json:"response"`
}

// PropertyInviteProposal binds the toggle to one exact canonical snapshot.
// The expected fields are private so a client cannot manufacture an
// authority proof; ApplyPropertyInvite re-plans and compares the whole
// proposal before cloning or mutating the state.
type PropertyInviteProposal struct {
	Action           PropertyInviteAction
	ActorID          string
	Target           string
	TargetID         string
	TargetName       string
	Property         int16
	PropertyID       int16
	RoomID           int16
	OrderedInviteIDs []string
	Changed          bool
	Response         string

	expectedActor       PlayerState
	expectedTarget      PlayerState
	expectedRoom        RoomState
	expectedInvitations map[int16][]string
}

// ValidatePropertyInviteName preserves player_name_is_valid's relevant
// boundary for a terminal name while rejecting whitespace that cannot be a
// single command token. The canonical name is UTF-8, at most 14 bytes and 12
// code points, and cannot carry path/control characters into a legacy-shaped
// identifier.
func ValidatePropertyInviteName(name string) error {
	if name == "" {
		return ErrPropertyInviteNameRequired
	}
	if !utf8.ValidString(name) {
		return ErrPropertyInviteNameInvalid
	}
	if len(name) > propertyInviteNameMaxBytes || utf8.RuneCountInString(name) > propertyInviteNameMaxCodepoints {
		return ErrPropertyInviteNameTooLong
	}
	if name == "." || name == ".." || strings.TrimSpace(name) != name {
		return ErrPropertyInviteNameInvalid
	}
	for _, r := range name {
		if unicode.IsControl(r) || unicode.IsSpace(r) || r == '/' || r == '\\' || r == ':' || r == '\u2028' || r == '\u2029' {
			return ErrPropertyInviteNameInvalid
		}
	}
	return nil
}

// ValidateInviteName is a concise compatibility spelling for adapters.
func ValidateInviteName(name string) error { return ValidatePropertyInviteName(name) }

func clonePropertyInviteIDs(ids []string) []string {
	result := make([]string, len(ids))
	copy(result, ids)
	return result
}

func clonePropertyInviteMap(source map[int16][]string) map[int16][]string {
	if source == nil {
		return nil
	}
	result := make(map[int16][]string, len(source))
	for property, ids := range source {
		if ids == nil {
			result[property] = nil
			continue
		}
		result[property] = clonePropertyInviteIDs(ids)
	}
	return result
}

func propertyInviteRoomFlagSet(room LegacyRoom) bool {
	return flag(room.Flags[:], propertyInviteRoomFlag)
}

func propertyInviteActor(s State, actorID string) (PlayerState, RoomState, int16, error) {
	actor, ok := s.Players[actorID]
	if actorID == "" || !ok || !actor.Online || actor.Body.Type != 0 || ValidatePropertyInviteName(actor.Body.Name) != nil {
		return PlayerState{}, RoomState{}, 0, ErrPropertyInviteActorAbsent
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok || room.Resource.ID != actor.Body.RoomID || !containsString(room.PlayerIDs, actorID) {
		return PlayerState{}, RoomState{}, 0, fmt.Errorf("%w: canonical room membership absent", ErrPropertyInviteActorAbsent)
	}
	if !propertyInviteRoomFlagSet(room.Resource) {
		return PlayerState{}, RoomState{}, 0, ErrPropertyInviteNotHome
	}
	property := int16(actor.Body.Daily[propertyInviteDailyIndex].Max)
	if property <= 0 {
		return PlayerState{}, RoomState{}, 0, ErrPropertyInviteNoProperty
	}
	return actor, room, property, nil
}

// propertyInviteTarget resolves the source find_who boundary: online,
// canonical players only, exact case-sensitive name matching, and no
// descriptor/map-order tie breaking. Visibility is checked after all exact
// candidates are counted so a hidden duplicate cannot become an oracle.
func (s State) propertyInviteTarget(actorID, targetName string, actor PlayerState) (string, PlayerState, error) {
	if err := ValidatePropertyInviteName(targetName); err != nil {
		return "", PlayerState{}, err
	}
	matches := make([]string, 0, 1)
	for id, target := range s.Players {
		if id == "" || !target.Online || target.Body.Type != 0 {
			continue
		}
		if ValidatePropertyInviteName(target.Body.Name) != nil || target.Body.Name != targetName {
			continue
		}
		matches = append(matches, id)
	}
	if len(matches) == 0 {
		return "", PlayerState{}, ErrPropertyInviteTargetMissing
	}
	if len(matches) != 1 {
		return "", PlayerState{}, ErrPropertyInviteTargetAmbiguous
	}
	targetID := matches[0]
	target := s.Players[targetID]
	if targetID == actorID {
		return "", PlayerState{}, ErrPropertyInviteSelf
	}
	// social_status.go is the shared source of truth for PINVIS/PDMINV and
	// PDINVI/SUB_DM exceptions. In particular, DM-invisible DM identities
	// remain hidden even from another DM.
	if !visibleToWho(actor, target, actorID, targetID) {
		return "", PlayerState{}, ErrPropertyInviteTargetInvisible
	}
	return targetID, target, nil
}

func propertyInviteIDs(s State, property int16) []string {
	ids := s.Invitations[property]
	return clonePropertyInviteIDs(ids)
}

func propertyInviteNames(s State, ids []string) ([]string, error) {
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		player, ok := s.Players[id]
		if !ok || id == "" || player.Body.Type != 0 || ValidatePropertyInviteName(player.Body.Name) != nil {
			return nil, fmt.Errorf("invalid canonical invited player %q", id)
		}
		names = append(names, player.Body.Name)
	}
	return names, nil
}

func propertyInviteListResponse(names []string) string {
	if len(names) == 0 {
		return "초대한 사람이 없습니다.\r\n"
	}
	var response strings.Builder
	response.WriteString("당신이 초대한 사람들 : \r\n")
	for _, name := range names {
		response.WriteString(name)
		response.WriteString("\r\n")
	}
	return response.String()
}

func propertyInviteToggleResponse(action PropertyInviteAction) string {
	if action == PropertyInviteRemove {
		return "초대 대상에서 삭제하였습니다.\r\n"
	}
	return "초대 대상에 추가하였습니다.\r\n"
}

func propertyInviteResult(action PropertyInviteAction, actorID, targetID, targetName string, property int16, ids []string, names []string, changed bool, response string) PropertyInviteResult {
	ordered := clonePropertyInviteIDs(ids)
	return PropertyInviteResult{
		Action:           action,
		ActorID:          actorID,
		Target:           targetID,
		TargetID:         targetID,
		TargetName:       targetName,
		Property:         property,
		PropertyID:       property,
		OrderedInviteIDs: ordered,
		InviteIDs:        clonePropertyInviteIDs(ordered),
		InviteNames:      clonePropertyInviteIDs(names),
		Changed:          changed,
		Response:         response,
	}
}

// PlanPropertyInvite validates house/property authority, resolves one exact
// online target, and computes the source toggle without changing State. A
// missing property key is an empty, writable list; a nil Invitations map is
// an import gap and is rejected.
func (s State) PlanPropertyInvite(actorID, targetName string) (PropertyInviteProposal, error) {
	if err := s.Validate(); err != nil {
		return PropertyInviteProposal{}, err
	}
	if s.Invitations == nil {
		return PropertyInviteProposal{}, ErrPropertyInvitationsUnmigrated
	}
	actor, room, property, err := propertyInviteActor(s, actorID)
	if err != nil {
		return PropertyInviteProposal{}, err
	}
	targetID, _, err := s.propertyInviteTarget(actorID, targetName, actor)
	if err != nil {
		return PropertyInviteProposal{}, err
	}
	beforeIDs := propertyInviteIDs(s, property)
	action := PropertyInviteAdd
	afterIDs := clonePropertyInviteIDs(beforeIDs)
	for i, id := range beforeIDs {
		if id != targetID {
			continue
		}
		action = PropertyInviteRemove
		afterIDs = append(afterIDs[:i:i], afterIDs[i+1:]...)
		break
	}
	if action == PropertyInviteAdd {
		if len(beforeIDs) >= maxPropertyInvitations {
			return PropertyInviteProposal{}, ErrPropertyInviteLimit
		}
		afterIDs = append(afterIDs, targetID)
	}
	response := propertyInviteToggleResponse(action)
	snapshot := s.clone()
	return PropertyInviteProposal{
		Action:              action,
		ActorID:             actorID,
		Target:              targetID,
		TargetID:            targetID,
		TargetName:          targetName,
		Property:            property,
		PropertyID:          property,
		RoomID:              actor.Body.RoomID,
		OrderedInviteIDs:    afterIDs,
		Changed:             true,
		Response:            response,
		expectedActor:       snapshot.Players[actorID],
		expectedTarget:      snapshot.Players[targetID],
		expectedRoom:        snapshot.Rooms[room.Resource.ID],
		expectedInvitations: snapshot.Invitations,
	}, nil
}

// ApplyPropertyInvite revalidates the proposal against a fresh plan, then
// mutates a cloned snapshot. Last-removal deletes the property key to mirror
// command12.c's unlink(file); the nonnil map still records confirmed import
// state and Validate remains happy. Any stale actor/room/target/invitation or
// tampered proposal returns a zero State and leaves the receiver untouched.
func (s State) ApplyPropertyInvite(proposal PropertyInviteProposal) (State, PropertyInviteResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, PropertyInviteResult{}, err
	}
	if s.Invitations == nil {
		return State{}, PropertyInviteResult{}, ErrPropertyInvitationsUnmigrated
	}
	fresh, err := s.PlanPropertyInvite(proposal.ActorID, proposal.TargetName)
	if err != nil {
		return State{}, PropertyInviteResult{}, fmt.Errorf("%w: %v", ErrPropertyInviteStaleProposal, err)
	}
	if !reflect.DeepEqual(fresh, proposal) {
		return State{}, PropertyInviteResult{}, ErrPropertyInviteStaleProposal
	}
	if fresh.Action != PropertyInviteAdd && fresh.Action != PropertyInviteRemove || fresh.ActorID == "" || fresh.TargetID == "" || fresh.TargetName == "" || fresh.PropertyID <= 0 || fresh.RoomID == 0 || !fresh.Changed || fresh.Response != propertyInviteToggleResponse(fresh.Action) {
		return State{}, PropertyInviteResult{}, ErrPropertyInviteInvalidProposal
	}

	next := s.clone()
	ids := propertyInviteIDs(next, fresh.PropertyID)
	switch fresh.Action {
	case PropertyInviteAdd:
		if len(ids) >= maxPropertyInvitations {
			return State{}, PropertyInviteResult{}, ErrPropertyInviteStaleProposal
		}
		ids = append(ids, fresh.TargetID)
		next.Invitations[fresh.PropertyID] = ids
	case PropertyInviteRemove:
		index := -1
		for i, id := range ids {
			if id == fresh.TargetID {
				index = i
				break
			}
		}
		if index < 0 {
			return State{}, PropertyInviteResult{}, ErrPropertyInviteStaleProposal
		}
		ids = append(ids[:index:index], ids[index+1:]...)
		if len(ids) == 0 {
			delete(next.Invitations, fresh.PropertyID)
		} else {
			next.Invitations[fresh.PropertyID] = ids
		}
	default:
		return State{}, PropertyInviteResult{}, ErrPropertyInviteInvalidProposal
	}
	if err := next.Validate(); err != nil {
		return State{}, PropertyInviteResult{}, err
	}
	names, err := propertyInviteNames(next, ids)
	if err != nil {
		return State{}, PropertyInviteResult{}, err
	}
	result := propertyInviteResult(fresh.Action, fresh.ActorID, fresh.TargetID, fresh.TargetName, fresh.PropertyID, ids, names, true, fresh.Response)
	return next, result, nil
}

// ListPropertyInvitations is the no-target, read-only command branch. It
// returns canonical IDs for audit/replay and renders their current canonical
// names in the stored order; offline invitees remain listable.
func (s State) ListPropertyInvitations(actorID string) (PropertyInviteResult, error) {
	if err := s.Validate(); err != nil {
		return PropertyInviteResult{}, err
	}
	if s.Invitations == nil {
		return PropertyInviteResult{}, ErrPropertyInvitationsUnmigrated
	}
	_, _, property, err := propertyInviteActor(s, actorID)
	if err != nil {
		return PropertyInviteResult{}, err
	}
	ids := propertyInviteIDs(s, property)
	names, err := propertyInviteNames(s, ids)
	if err != nil {
		return PropertyInviteResult{}, err
	}
	return propertyInviteResult(PropertyInviteList, actorID, "", "", property, ids, names, false, propertyInviteListResponse(names)), nil
}

// ListInvitations is a descriptive compatibility alias.
func (s State) ListInvitations(actorID string) (PropertyInviteResult, error) {
	return s.ListPropertyInvitations(actorID)
}

// Package-level wrappers are useful to small reducers that keep State as an
// explicit first argument, while the method forms remain the canonical API.
func PlanPropertyInvite(s State, actorID, targetName string) (PropertyInviteProposal, error) {
	return s.PlanPropertyInvite(actorID, targetName)
}

func ApplyPropertyInvite(s State, proposal PropertyInviteProposal) (State, PropertyInviteResult, error) {
	return s.ApplyPropertyInvite(proposal)
}

func ListPropertyInvitations(s State, actorID string) (PropertyInviteResult, error) {
	return s.ListPropertyInvitations(actorID)
}
