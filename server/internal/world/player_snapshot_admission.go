package world

import (
	"errors"
	"fmt"

	"github.com/1XP-Inc/muhan-mud/server/internal/identity"
)

var (
	// ErrPlayerSnapshotAdmission is returned when an explicit player-ID
	// mapping cannot be proven against the current canonical world.
	ErrPlayerSnapshotAdmission        = errors.New("player snapshot admission rejected")
	ErrPlayerSnapshotIdentityConflict = errors.New("player snapshot identity conflict")
)

// AdmitPlayerSnapshot inserts one verified, offline player into a cloned world
// snapshot. The caller must supply the exact world player ID obtained from an
// operator/import manifest; this method never derives identity from a name.
// Item IDs are allocated by the caller so retries can reuse a durable mapping.
// No database, credential, or transport state is changed here.
func (s State) AdmitPlayerSnapshot(playerID string, snapshot PlayerSnapshotV1, allocate func() (string, error)) (State, error) {
	if err := s.Validate(); err != nil {
		return State{}, fmt.Errorf("%w: current world invalid: %v", ErrPlayerSnapshotAdmission, err)
	}
	if playerID == "" || len(playerID) > 128 {
		return State{}, fmt.Errorf("%w: explicit player ID required", ErrPlayerSnapshotAdmission)
	}
	if _, exists := s.Players[playerID]; exists {
		return State{}, fmt.Errorf("%w: player ID already exists", ErrPlayerSnapshotIdentityConflict)
	}
	player, err := snapshot.ToPlayerState(allocate)
	if err != nil {
		return State{}, err
	}
	canonical, err := identity.CanonicalName(player.Body.Name)
	if err != nil {
		return State{}, fmt.Errorf("%w: snapshot name is invalid", ErrPlayerSnapshotIdentityConflict)
	}
	// The legacy file may carry the pre-login spelling; use the same stable
	// lowercize(name, 1) normalization as terminal registration. The raw name
	// remains in the caller's immutable CDTO evidence.
	player.Body.Name = canonical
	if _, exists := s.Rooms[player.Body.RoomID]; !exists {
		return State{}, fmt.Errorf("%w: snapshot room is absent", ErrPlayerSnapshotAdmission)
	}
	for existingID, existing := range s.Players {
		if existing.Body.Name == player.Body.Name {
			return State{}, fmt.Errorf("%w: name already maps to %q", ErrPlayerSnapshotIdentityConflict, existingID)
		}
	}
	next := s.clone()
	next.Players[playerID] = player
	if err := next.Validate(); err != nil {
		return State{}, fmt.Errorf("%w: resulting world invalid: %v", ErrPlayerSnapshotAdmission, err)
	}
	return next, nil
}
