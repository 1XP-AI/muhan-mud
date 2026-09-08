package world

import (
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
)

// LegacyRoomAdmissionPolicy is the explicit compatibility boundary between
// the strict legacy decoder and a Go world catalog. DecodeLegacyRoom remains
// strict: callers must opt in to each known, bounded anomaly that the C
// loader historically tolerated.
//
// No policy option permits truncated records, invalid counts, excessive
// nesting, or bytes read outside a fixed-width field. Those are structural
// errors and always fail closed.
type LegacyRoomAdmissionPolicy struct {
	AllowInvalidEUCKR          bool
	AllowMissingTextTerminator bool
	AllowTrailingData          bool
	// Some historical room blobs contain a zeroed creature record in the
	// serialized monster list. C keeps the record in memory but it is neither
	// a player nor a valid runtime monster; admission may quarantine it while
	// retaining the raw bytes in Evidence.
	AllowEmptyMonsterPlaceholder bool
	// C load_rom uses the path argument as the runtime room identity and
	// overwrites the raw struct's room number after read_rom. This option is
	// therefore required for a path-scoped catalog to admit a stale header ID.
	AllowHeaderIDPathMismatch bool
}

// LegacyRoomCompatibilityPolicy admits only the bounded issue classes observed
// in the checked-in room corpus. It does not modify the source bytes or the
// strict decoder; the resulting evidence must travel with the catalog entry.
var LegacyRoomCompatibilityPolicy = LegacyRoomAdmissionPolicy{
	AllowInvalidEUCKR:            true,
	AllowMissingTextTerminator:   true,
	AllowTrailingData:            true,
	AllowEmptyMonsterPlaceholder: true,
	AllowHeaderIDPathMismatch:    true,
}

func (p LegacyRoomAdmissionPolicy) allows(issue LegacyIssue) bool {
	switch issue.Kind {
	case "invalid-euc-kr":
		return p.AllowInvalidEUCKR
	case "missing-text-terminator":
		return p.AllowMissingTextTerminator
	case "trailing-data":
		return p.AllowTrailingData
	case "empty-monster-placeholder":
		return p.AllowEmptyMonsterPlaceholder
	case "header-id-mismatch":
		return p.AllowHeaderIDPathMismatch
	default:
		return false
	}
}

// LegacyRoomAdmission is a decoded room plus the immutable source evidence
// required to review or re-import a noncanonical resource. Source is never
// sent to a client and is copied on both input and catalog lookup.
type LegacyRoomAdmission struct {
	Room     LegacyRoom
	Evidence LegacyInspection
}

// LegacyRoomAdmissionError identifies an issue rejected by the explicit
// compatibility policy. Structural decoder errors are returned unchanged so
// callers can distinguish policy review from malformed input.
type LegacyRoomAdmissionError struct {
	Issue LegacyIssue
}

func (e LegacyRoomAdmissionError) Error() string {
	return fmt.Sprintf("legacy room issue %q at byte %d is not admitted", e.Issue.Kind, e.Issue.Offset)
}

// AdmitLegacyRoom is the only API that may turn inspection output into a
// runtime catalog entry. It preserves original bytes and issue locations,
// while requiring a caller-owned policy for every tolerated anomaly.
func AdmitLegacyRoom(raw []byte, policy LegacyRoomAdmissionPolicy) (LegacyRoomAdmission, error) {
	report, err := InspectLegacyRoom(raw)
	if err != nil {
		return LegacyRoomAdmission{}, err
	}
	for _, issue := range report.Issues {
		if !policy.allows(issue) {
			return LegacyRoomAdmission{}, LegacyRoomAdmissionError{Issue: issue}
		}
	}
	for _, monster := range report.Room.Monsters {
		if !legacyEmptyMonsterPlaceholder(monster) {
			continue
		}
		issue := LegacyIssue{Kind: "empty-monster-placeholder", Offset: -1}
		report.Issues = append(report.Issues, issue)
		if !policy.allows(issue) {
			return LegacyRoomAdmission{}, LegacyRoomAdmissionError{Issue: issue}
		}
	}
	room := cloneRoom(report.Room)
	kept := room.Monsters[:0]
	for _, monster := range room.Monsters {
		if !legacyEmptyMonsterPlaceholder(monster) {
			kept = append(kept, monster)
			continue
		}
	}
	room.Monsters = kept
	return LegacyRoomAdmission{Room: room, Evidence: cloneLegacyInspection(report)}, nil
}

func legacyEmptyMonsterPlaceholder(monster LegacyMonster) bool {
	return monster.Name == "" && len(monster.Inventory) == 0
}

func cloneLegacyInspection(report LegacyInspection) LegacyInspection {
	out := report
	out.Room = cloneRoom(report.Room)
	out.Source = append([]byte(nil), report.Source...)
	out.Issues = append([]LegacyIssue(nil), report.Issues...)
	return out
}

// LegacyRoomIndexIssue records a file that was deliberately not loaded into
// the C-compatible room catalog. The original tree contains historical room
// artifacts such as rooms/r01/r00001 whose path does not match C's
// rooms/r%02d/r%05d lookup for room 1. They remain visible in the index rather
// than being silently treated as alternate definitions.
type LegacyRoomIndexIssue struct {
	Path string
	Kind string
}

// LegacyRoomCatalog is an immutable, C-path-indexed room resource catalog.
// Mutable room state belongs in world.State; this catalog only supplies the
// decoded resource and its source evidence.
type LegacyRoomCatalog struct {
	rooms   map[int16]LegacyRoomAdmission
	ignored []LegacyRoomIndexIssue
}

// LoadLegacyRoomCatalog walks an fs.FS rooted at the legacy rooms directory.
// Only a regular file at r%02d/r%05d, where the path-derived number matches
// the room header ID, is loadable. Noncanonical historical artifacts are
// recorded in Ignored instead of winning a duplicate-ID race.
func LoadLegacyRoomCatalog(fsys fs.FS, policy LegacyRoomAdmissionPolicy) (LegacyRoomCatalog, error) {
	if fsys == nil {
		return LegacyRoomCatalog{}, fmt.Errorf("missing room filesystem")
	}
	catalog := LegacyRoomCatalog{rooms: map[int16]LegacyRoomAdmission{}}
	err := fs.WalkDir(fsys, ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&fs.ModeType != 0 {
			catalog.ignored = append(catalog.ignored, LegacyRoomIndexIssue{Path: name, Kind: "non-regular-file"})
			return nil
		}
		id, canonical := canonicalRoomPath(name)
		if !canonical {
			catalog.ignored = append(catalog.ignored, LegacyRoomIndexIssue{Path: name, Kind: "noncanonical-path"})
			return nil
		}
		raw, err := fs.ReadFile(fsys, name)
		if err != nil {
			return fmt.Errorf("read room %s: %w", name, err)
		}
		admission, err := AdmitLegacyRoom(raw, policy)
		if err != nil {
			return fmt.Errorf("admit room %s: %w", name, err)
		}
		if admission.Room.ID != id {
			issue := LegacyIssue{Kind: "header-id-mismatch", Offset: 0}
			if !policy.allows(issue) {
				return fmt.Errorf("admit room %s: %w", name, LegacyRoomAdmissionError{Issue: issue})
			}
			admission.Evidence.Issues = append(admission.Evidence.Issues, issue)
			// Match load_rom: the canonical path, not the stale raw field, owns
			// the runtime room identity. The raw value remains in Evidence.Source.
			admission.Room.ID = id
		}
		if _, exists := catalog.rooms[id]; exists {
			return fmt.Errorf("duplicate canonical room ID %d", id)
		}
		catalog.rooms[id] = admission
		return nil
	})
	if err != nil {
		return LegacyRoomCatalog{}, err
	}
	sort.Slice(catalog.ignored, func(i, j int) bool { return catalog.ignored[i].Path < catalog.ignored[j].Path })
	return catalog, nil
}

// canonicalRoomPath applies the same directory rule as files2.c/load_rom.
func canonicalRoomPath(name string) (int16, bool) {
	clean := path.Clean(name)
	base := path.Base(clean)
	dir := path.Base(path.Dir(clean))
	if clean == "." || len(base) != 6 || base[0] != 'r' || len(dir) != 3 || dir[0] != 'r' {
		return 0, false
	}
	for _, b := range base[1:] {
		if b < '0' || b > '9' {
			return 0, false
		}
	}
	for _, b := range dir[1:] {
		if b < '0' || b > '9' {
			return 0, false
		}
	}
	id, err := strconv.Atoi(base[1:])
	if err != nil || id > 32767 {
		return 0, false
	}
	expectedDir := fmt.Sprintf("r%02d", id/1000)
	return int16(id), dir == expectedDir && base == fmt.Sprintf("r%05d", id)
}

func (c LegacyRoomCatalog) Len() int { return len(c.rooms) }

// Room returns an owned copy of a resource. Callers may mutate it while
// constructing a world snapshot without changing catalog evidence.
func (c LegacyRoomCatalog) Room(id int16) (LegacyRoom, bool) {
	entry, ok := c.rooms[id]
	if !ok {
		return LegacyRoom{}, false
	}
	return cloneRoom(entry.Room), true
}

// Evidence returns an owned copy of the source hash/issues and raw bytes for
// audit or migration tooling.
func (c LegacyRoomCatalog) Evidence(id int16) (LegacyInspection, bool) {
	entry, ok := c.rooms[id]
	if !ok {
		return LegacyInspection{}, false
	}
	return cloneLegacyInspection(entry.Evidence), true
}

func (c LegacyRoomCatalog) Ignored() []LegacyRoomIndexIssue {
	out := append([]LegacyRoomIndexIssue(nil), c.ignored...)
	return out
}

// NewState creates the initial, empty-player snapshot for a room catalog. It
// deliberately leaves legacy room objects and monsters in their migration
// representation; canonical item/NPC identity conversion is a separate,
// explicit transaction and cannot be inferred from a room file alone.
func (c LegacyRoomCatalog) NewState() (State, error) {
	if len(c.rooms) == 0 {
		return State{}, fmt.Errorf("empty legacy room catalog")
	}
	s := State{Version: 1, Rooms: make(map[int16]RoomState, len(c.rooms)), Players: map[string]PlayerState{}}
	for id, entry := range c.rooms {
		room := cloneRoom(entry.Room)
		if room.ID != id {
			return State{}, fmt.Errorf("catalog room %d has resource ID %d", id, room.ID)
		}
		s.Rooms[id] = RoomState{Resource: room}
	}
	if err := s.Validate(); err != nil {
		return State{}, fmt.Errorf("validate catalog state: %w", err)
	}
	return s, nil
}

// String is intentionally small and deterministic for operator diagnostics;
// it never includes raw resource bytes.
func (c LegacyRoomCatalog) String() string {
	return fmt.Sprintf("legacy room catalog rooms=%d ignored=%d", c.Len(), len(c.ignored))
}
