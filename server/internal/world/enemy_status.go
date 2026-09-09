package world

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// enemy_status calls display_status, whose semantic bar is intended to be
	// fifteen cells wide.  ANSI styling is deliberately not part of this
	// projection: the source derives it from descriptor-local PANSIC/PBRIGH,
	// which is not canonical world state and belongs to the transport layer.
	EnemyStatusBarWidth = 15

	enemyStatusNPCType                  = 1
	enemyStatusBlindFlag           uint = 42 // PBLIND
	enemyStatusInvisibleFlag       uint = 2  // MINVIS
	enemyStatusDetectInvisibleFlag uint = 21 // PDINVI
	enemyStatusDMInvisibleFlag     uint = 10 // PDMINV
	enemyStatusCaretakerClass      byte = 10 // CARETAKER
)

var (
	// Enemy status can only read the canonical NPC identity graph.  A legacy
	// room.Monsters list has no stable target identity and is never consulted.
	ErrEnemyStatusCanonicalOnly  = errors.New("enemy status requires canonical NPC state")
	ErrEnemyStatusTargetRequired = errors.New("enemy status target required")
	ErrEnemyStatusTargetAbsent   = errors.New("enemy status NPC target absent")
	// HPMax is the denominator used by display_status.  A non-positive value
	// is not a meaningful canonical ratio, so the reducer refuses to guess.
	ErrEnemyStatusVitalsUnavailable = errors.New("enemy status NPC vitals unavailable")
)

const EnemyStatusBlindResponse = "아무것도 보이지 않습니다!\r\n"

// EnemyStatusProjection is the deterministic, semantic portion of
// command8.c:enemy_status.  It intentionally contains only fields proven by
// the canonical NPC body and the derived fixed-width bar.  ANSI escape
// sequences, descriptor state and display_status's file descriptor are not
// projected here.
type EnemyStatusProjection struct {
	TargetID   string `json:"target_id,omitempty"`
	TargetName string `json:"target_name,omitempty"`
	HPCurrent  int16  `json:"hp_current,omitempty"`
	HPMax      int16  `json:"hp_max,omitempty"`
	Filled     int    `json:"filled,omitempty"`
	Width      int    `json:"width,omitempty"`
	Bar        string `json:"bar"`
	Response   string `json:"response"`
	Blind      bool   `json:"blind,omitempty"`
}

// EnemyStatusResult is a descriptive compatibility name for callers that
// treat the output as a command result rather than a display projection.
type EnemyStatusResult = EnemyStatusProjection

// NPCEnemyStatusProjection is retained as an explicit NPC-scoped alias.  It
// does not widen the command to player targets.
type NPCEnemyStatusProjection = EnemyStatusProjection

func validEnemyStatusName(name string) bool {
	if name == "" || !utf8.ValidString(name) || strings.TrimSpace(name) != name {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

// EnemyStatusBar renders the source display_status health ratio without the
// source's descriptor-dependent ANSI styling.  Positive ratios round up to
// the same filled-cell count as C's integer loop (for example, 50/100 yields
// eight filled cells); the result is clamped to a deterministic 15-cell bar.
// A non-positive maximum is rejected instead of turning division by zero or
// a negative denominator into guessed output.
func EnemyStatusBar(hpCurrent, hpMax int16) (string, error) {
	if hpMax <= 0 {
		return "", ErrEnemyStatusVitalsUnavailable
	}
	filled := 0
	if hpCurrent > 0 {
		// Convert before multiplying so the calculation remains explicit if the
		// migration type widens beyond int16 in a future snapshot version.
		filled = (int(hpCurrent)*EnemyStatusBarWidth + int(hpMax) - 1) / int(hpMax)
		if filled > EnemyStatusBarWidth {
			filled = EnemyStatusBarWidth
		}
	}
	empty := EnemyStatusBarWidth - filled
	return "[" + strings.Repeat("=", filled) + strings.Repeat(" ", empty) + "]", nil
}

func enemyStatusNPCVisible(actor, npc LegacyMonster) bool {
	// Preserve find_crt's two proven visibility predicates.  Caretaker-class
	// DM invisibility is always skipped; ordinary MINVIS requires PDINVI.
	if npc.Class >= enemyStatusCaretakerClass && flag(npc.Flags[:], enemyStatusDMInvisibleFlag) {
		return false
	}
	return !flag(npc.Flags[:], enemyStatusInvisibleFlag) || flag(actor.Flags[:], enemyStatusDetectInvisibleFlag)
}

// ProjectEnemyStatus resolves one exact, same-room canonical NPC and returns
// its read-only status projection.  The room.NPCIDs slice is the authoritative
// C first_mon order; map iteration and legacy room.Monsters are never used.
// The source allows name/key prefixes and an occurrence number, but this
// bounded slice admits only an exact display name.  A duplicate exact name is
// resolved by canonical room order, equivalent to the source's default first
// occurrence.
func (s State) ProjectEnemyStatus(actorID, targetName string) (EnemyStatusProjection, error) {
	if err := s.Validate(); err != nil {
		return EnemyStatusProjection{}, err
	}
	if actorID == "" {
		return EnemyStatusProjection{}, fmt.Errorf("enemy status actor required")
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Body.Type != 0 {
		return EnemyStatusProjection{}, fmt.Errorf("online enemy status actor absent")
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok {
		return EnemyStatusProjection{}, fmt.Errorf("enemy status actor room absent")
	}
	// command8.c checks blindness before checking the argument or calling
	// find_crt.  Preserve that ordering so a blind actor never learns whether
	// a canonical target exists.
	if flag(actor.Body.Flags[:], enemyStatusBlindFlag) {
		return EnemyStatusProjection{Blind: true, Bar: "", Response: EnemyStatusBlindResponse}, nil
	}
	if s.NPCs == nil {
		return EnemyStatusProjection{}, ErrEnemyStatusCanonicalOnly
	}
	if !validEnemyStatusName(targetName) || strings.ContainsAny(targetName, " \t\r\n") {
		if targetName == "" {
			return EnemyStatusProjection{}, ErrEnemyStatusTargetRequired
		}
		return EnemyStatusProjection{}, fmt.Errorf("invalid enemy status target")
	}

	for _, id := range room.NPCIDs {
		npc, exists := s.NPCs[id]
		// State.Validate already checks this relation, but keep the identity
		// check local to the projection so future snapshot versions cannot
		// accidentally fall back to an unowned legacy body.
		if !exists || id == "" || npc.Body.Type != enemyStatusNPCType || npc.Body.RoomID != room.Resource.ID || !validEnemyStatusName(npc.Body.Name) {
			return EnemyStatusProjection{}, fmt.Errorf("%w: unresolved NPC identity", ErrEnemyStatusCanonicalOnly)
		}
		if !strings.EqualFold(npc.Body.Name, targetName) || !enemyStatusNPCVisible(actor.Body, npc.Body) {
			continue
		}
		bar, err := EnemyStatusBar(npc.Body.HPCurrent, npc.Body.HPMax)
		if err != nil {
			return EnemyStatusProjection{}, err
		}
		filled := strings.Count(bar, "=")
		return EnemyStatusProjection{
			TargetID:   id,
			TargetName: npc.Body.Name,
			HPCurrent:  npc.Body.HPCurrent,
			HPMax:      npc.Body.HPMax,
			Filled:     filled,
			Width:      EnemyStatusBarWidth,
			Bar:        bar,
			Response:   fmt.Sprintf("%s : %s\r\n", npc.Body.Name, bar),
		}, nil
	}
	return EnemyStatusProjection{}, ErrEnemyStatusTargetAbsent
}

// EnemyStatusProjection is the method spelling used by command adapters that
// prefer a noun-style projection name.  It delegates to the same reducer and
// therefore has identical canonical and visibility boundaries.
func (s State) EnemyStatusProjection(actorID, targetName string) (EnemyStatusProjection, error) {
	return s.ProjectEnemyStatus(actorID, targetName)
}

// EnemyStatus renders only the semantic response string used by the terminal
// command.  It is read-only and never changes the source snapshot.
func (s State) EnemyStatus(actorID, targetName string) (string, error) {
	projection, err := s.ProjectEnemyStatus(actorID, targetName)
	if err != nil {
		return "", err
	}
	return projection.Response, nil
}

// RenderEnemyStatus is a descriptive alias for EnemyStatus.
func (s State) RenderEnemyStatus(actorID, targetName string) (string, error) {
	return s.EnemyStatus(actorID, targetName)
}
