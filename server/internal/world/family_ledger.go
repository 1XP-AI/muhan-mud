package world

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/1XP-Inc/muhan-mud/server/internal/identity"
)

var (
	// ErrFamilyStateUnresolved means the legacy family_member_<n> files have
	// not been admitted.  A nil FamilyState and a nil Members map are
	// intentionally different from an imported, empty member ledger.
	ErrFamilyStateUnresolved = errors.New("family state unresolved")
	ErrFamilyStateInvalid    = errors.New("invalid family state")
	ErrFamilyMemberInvalid   = errors.New("invalid family member")
	ErrFamilyMemberAbsent    = errors.New("family member ledger entry absent")
	ErrFamilyMemberDuplicate = errors.New("duplicate family member")
)

// FamilyMember is the pointer-free canonical projection of one row in
// family_member_<n>.  ID is the server-owned character identity; Name and
// Class are retained as an integrity projection and are never used to select
// an account or authorize a command.
type FamilyMember struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Class byte   `json:"class"`
}

// FamilyState is the imported family-member ledger.  A non-nil FamilyState
// with non-nil Members is an explicit migration boundary: an empty slice is a
// known empty family, while a missing family key means that family's source
// ledger is unavailable and reducers must fail closed.
//
// Fee remains in FamilyCatalog because it is immutable family_list data; this
// aggregate owns only mutable membership rows.
type FamilyState struct {
	Members map[int16][]FamilyMember `json:"members"`
}

// FamilyMemberLedger is a descriptive alias for callers that use the source
// file terminology.
type FamilyMemberLedger = FamilyState

func validFamilyMemberID(id string) bool {
	if id == "" || !utf8.ValidString(id) || strings.TrimSpace(id) != id {
		return false
	}
	for _, r := range id {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

func validateFamilyMember(member FamilyMember) error {
	if !validFamilyMemberID(member.ID) || !validFamilyText(member.Name) {
		return ErrFamilyMemberInvalid
	}
	canonical, err := identity.CanonicalName(member.Name)
	if err != nil || canonical != member.Name {
		return fmt.Errorf("%w: non-canonical name", ErrFamilyMemberInvalid)
	}
	return nil
}

// Validate checks every imported row without repairing order or duplicate
// identities.  Order is source evidence (edit_member appends members), so it
// is preserved for deterministic receipts and exports.
func (f FamilyState) Validate() error {
	if f.Members == nil {
		return ErrFamilyStateUnresolved
	}
	seenIDs := make(map[string]int16)
	seenNames := make(map[string]int16)
	for familyID, members := range f.Members {
		if familyID < 1 || familyID > FamilyMaxID {
			return fmt.Errorf("%w: family id %d", ErrFamilyStateInvalid, familyID)
		}
		localIDs := make(map[string]struct{}, len(members))
		localNames := make(map[string]struct{}, len(members))
		for index, member := range members {
			if err := validateFamilyMember(member); err != nil {
				return fmt.Errorf("%w: family %d member %d: %v", ErrFamilyStateInvalid, familyID, index, err)
			}
			if _, ok := localIDs[member.ID]; ok {
				return fmt.Errorf("%w: family %d member %q", ErrFamilyMemberDuplicate, familyID, member.ID)
			}
			if _, ok := localNames[member.Name]; ok {
				return fmt.Errorf("%w: family %d name %q", ErrFamilyMemberDuplicate, familyID, member.Name)
			}
			if previous, ok := seenIDs[member.ID]; ok {
				return fmt.Errorf("%w: identity %q in families %d and %d", ErrFamilyMemberDuplicate, member.ID, previous, familyID)
			}
			if previous, ok := seenNames[member.Name]; ok {
				return fmt.Errorf("%w: name %q in families %d and %d", ErrFamilyMemberDuplicate, member.Name, previous, familyID)
			}
			localIDs[member.ID] = struct{}{}
			localNames[member.Name] = struct{}{}
			seenIDs[member.ID] = familyID
			seenNames[member.Name] = familyID
		}
	}
	return nil
}

// Clone returns an independent ledger while preserving the non-nil import
// marker. It deliberately does not validate; callers clone only after the
// containing State has passed validation.
func (f FamilyState) Clone() FamilyState {
	if f.Members == nil {
		return FamilyState{}
	}
	next := FamilyState{Members: make(map[int16][]FamilyMember, len(f.Members))}
	for familyID, members := range f.Members {
		next.Members[familyID] = append([]FamilyMember(nil), members...)
	}
	return next
}

func (f FamilyState) members(familyID int16) ([]FamilyMember, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	members, ok := f.Members[familyID]
	if !ok {
		return nil, fmt.Errorf("%w: family %d", ErrFamilyMemberAbsent, familyID)
	}
	return append([]FamilyMember(nil), members...), nil
}

func (f FamilyState) hasMember(familyID int16, id string) (FamilyMember, bool, error) {
	members, err := f.members(familyID)
	if err != nil {
		return FamilyMember{}, false, err
	}
	for _, member := range members {
		if member.ID == id {
			return member, true, nil
		}
	}
	return FamilyMember{}, false, nil
}

func (f FamilyState) addMember(familyID int16, member FamilyMember) (FamilyState, error) {
	if err := f.Validate(); err != nil {
		return FamilyState{}, err
	}
	if err := validateFamilyMember(member); err != nil {
		return FamilyState{}, err
	}
	if _, ok, err := f.hasMember(familyID, member.ID); err != nil {
		return FamilyState{}, err
	} else if ok {
		return FamilyState{}, fmt.Errorf("%w: identity %q", ErrFamilyMemberDuplicate, member.ID)
	}
	next := f.Clone()
	next.Members[familyID] = append(next.Members[familyID], member)
	return next, nil
}

func (f FamilyState) removeMember(familyID int16, id string) (FamilyState, FamilyMember, error) {
	if err := f.Validate(); err != nil {
		return FamilyState{}, FamilyMember{}, err
	}
	members, ok := f.Members[familyID]
	if !ok {
		return FamilyState{}, FamilyMember{}, fmt.Errorf("%w: family %d", ErrFamilyMemberAbsent, familyID)
	}
	next := f.Clone()
	for index, member := range members {
		if member.ID != id {
			continue
		}
		next.Members[familyID] = append(append([]FamilyMember(nil), members[:index]...), members[index+1:]...)
		return next, member, nil
	}
	return FamilyState{}, FamilyMember{}, fmt.Errorf("%w: identity %q", ErrFamilyMemberAbsent, id)
}

// WithFamilyState installs a complete family-member ledger into a valid world
// snapshot.  It is the only state-level migration seam; no legacy file read or
// name-based member discovery occurs here.
func (s State) WithFamilyState(family FamilyState) (State, error) {
	if err := s.Validate(); err != nil {
		return State{}, err
	}
	if err := family.Validate(); err != nil {
		return State{}, err
	}
	for _, members := range family.Members {
		for _, member := range members {
			player, ok := s.Players[member.ID]
			if !ok || player.Body.Name != member.Name || player.Body.Class != member.Class {
				return State{}, fmt.Errorf("%w: identity %q", ErrFamilyMemberInvalid, member.ID)
			}
		}
	}
	next := s.clone()
	copy := family.Clone()
	next.Family = &copy
	if err := next.Validate(); err != nil {
		return State{}, err
	}
	return next, nil
}

// WithFamilies is the concise alias used by migration adapters.
func (s State) WithFamilies(family FamilyState) (State, error) {
	return s.WithFamilyState(family)
}
