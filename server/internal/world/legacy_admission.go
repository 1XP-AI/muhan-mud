package world

import (
	"crypto/sha256"
	"encoding/binary"
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
// rooms/r%02d/r%05d lookup for room 1. Their byte hash, consumed offset, and
// inspection issues remain visible rather than silently treating them as
// alternate definitions.
type LegacyRoomIndexIssue struct {
	Path      string
	Kind      string
	Size      int
	Consumed  int
	SHA256    [32]byte
	Issues    []LegacyIssue
	DecodeErr string
}

// LegacyRoomManifestClass is the source-backed classification used by the
// reviewed room admission boundary. A noncanonical artifact is never a room
// definition, even when its bytes happen to decode successfully. BodyException
// is kept as a separate bit because the 63 strict body failures are an
// evidence class orthogonal to path canonicality (17 are historical artifacts).
type LegacyRoomManifestClass string

const (
	LegacyRoomCanonical            LegacyRoomManifestClass = "canonical"
	LegacyRoomNoncanonicalArtifact LegacyRoomManifestClass = "noncanonical-artifact"
)

// LegacyRoomManifestEntry is deterministic evidence for one regular source
// path. SHA256 covers the exact bytes read from fsys; no normalized or repaired
// bytes are hashed. Issues and DecodeErr retain what inspection observed for
// ignored artifacts and admitted canonical resources.
type LegacyRoomManifestEntry struct {
	Path          string
	Class         LegacyRoomManifestClass
	Admitted      bool
	BodyException bool
	Size          int
	Consumed      int
	SHA256        [32]byte
	Issues        []LegacyIssue
	DecodeErr     string
}

// LegacyRoomAdmissionManifest is the immutable, deterministic source
// inventory attached to a catalog. It is deliberately computed from the
// source filesystem and compared with the reviewed digest; it is not a broad
// runtime allow-list.
type LegacyRoomAdmissionManifest struct {
	version                string
	entries                []LegacyRoomManifestEntry
	totalFiles             int
	canonicalPaths         int
	admittedCanonicalPaths int
	noncanonicalArtifacts  int
	bodyExceptions         int
	digest                 [32]byte
}

// LegacyRoomManifestDriftError reports a mismatch between observed source
// evidence and an expected manifest. Callers should fail closed rather than
// selecting a best-effort subset of rooms.
type LegacyRoomManifestDriftError struct {
	Reason         string
	ExpectedDigest [32]byte
	ActualDigest   [32]byte
	ExpectedFiles  int
	ActualFiles    int
}

func (e LegacyRoomManifestDriftError) Error() string {
	return fmt.Sprintf("legacy room admission manifest drift: %s (expected files=%d digest=%x, actual files=%d digest=%x)", e.Reason, e.ExpectedFiles, e.ExpectedDigest, e.ActualFiles, e.ActualDigest)
}

const legacyRoomManifestVersion = "legacy-room-admission/v1"

// These are the reviewed source-corpus boundaries. The digest is intentionally
// populated from the checked-in corpus rather than calculated as an admission
// side effect; a changed source tree must therefore be re-reviewed.
const (
	reviewedLegacyRoomTotalFiles            = 3216
	reviewedLegacyRoomCanonicalPaths        = 2341
	reviewedLegacyRoomNoncanonicalArtifacts = 875
	reviewedLegacyRoomBodyExceptions        = 63
)

var reviewedLegacyRoomManifestDigest = [32]byte{
	0x5e, 0x90, 0x29, 0x0d, 0xc9, 0xe0, 0xf6, 0xd7,
	0x19, 0x0e, 0x4c, 0x80, 0xf1, 0x3a, 0x2e, 0xf8,
	0x2f, 0xcd, 0x9f, 0x1b, 0xe0, 0x28, 0x9c, 0x6f,
	0xc4, 0x3f, 0x67, 0x8a, 0x81, 0x15, 0xd8, 0x5a,
}

// LegacyRoomCatalog is an immutable, C-path-indexed room resource catalog.
// Mutable room state belongs in world.State; this catalog only supplies the
// decoded resource and its source evidence.
type LegacyRoomCatalog struct {
	rooms    map[int16]LegacyRoomAdmission
	ignored  []LegacyRoomIndexIssue
	manifest LegacyRoomAdmissionManifest
}

// AdmissionManifest returns an owned copy of the source manifest. The copy
// includes issue slices so callers cannot mutate the catalog's audit boundary.
func (c LegacyRoomCatalog) AdmissionManifest() LegacyRoomAdmissionManifest {
	return cloneLegacyRoomAdmissionManifest(c.manifest)
}

// ReviewedLegacyRoomAdmissionManifest returns the checked-in corpus contract.
// It contains the expected summary and digest, but not source bytes.
func ReviewedLegacyRoomAdmissionManifest() LegacyRoomAdmissionManifest {
	return LegacyRoomAdmissionManifest{
		version:                legacyRoomManifestVersion,
		totalFiles:             reviewedLegacyRoomTotalFiles,
		canonicalPaths:         reviewedLegacyRoomCanonicalPaths,
		admittedCanonicalPaths: reviewedLegacyRoomCanonicalPaths,
		noncanonicalArtifacts:  reviewedLegacyRoomNoncanonicalArtifacts,
		bodyExceptions:         reviewedLegacyRoomBodyExceptions,
		digest:                 reviewedLegacyRoomManifestDigest,
	}
}

func (m LegacyRoomAdmissionManifest) Version() string     { return m.version }
func (m LegacyRoomAdmissionManifest) TotalFiles() int     { return m.totalFiles }
func (m LegacyRoomAdmissionManifest) CanonicalPaths() int { return m.canonicalPaths }
func (m LegacyRoomAdmissionManifest) AdmittedCanonicalPaths() int {
	return m.admittedCanonicalPaths
}
func (m LegacyRoomAdmissionManifest) NoncanonicalArtifacts() int {
	return m.noncanonicalArtifacts
}
func (m LegacyRoomAdmissionManifest) BodyExceptions() int { return m.bodyExceptions }
func (m LegacyRoomAdmissionManifest) Digest() [32]byte    { return m.digest }

// IssueCounts reports immutable issue-kind counts without exposing internal
// slices. The map is newly allocated for each call so diagnostics cannot alter
// the reviewed catalog.
func (m LegacyRoomAdmissionManifest) IssueCounts() map[string]int {
	counts := make(map[string]int)
	for _, entry := range m.entries {
		for _, issue := range entry.Issues {
			counts[issue.Kind]++
		}
	}
	return counts
}

// Entries returns sorted, owned evidence for all regular source paths.
func (m LegacyRoomAdmissionManifest) Entries() []LegacyRoomManifestEntry {
	return cloneLegacyRoomManifestEntries(m.entries)
}

// Verify compares entries with this manifest's deterministic summary and
// digest. It is useful for callers that retain an independently inspected
// manifest before opening the runtime catalog.
func (m LegacyRoomAdmissionManifest) Verify(entries []LegacyRoomManifestEntry) error {
	actual := newLegacyRoomAdmissionManifest(entries)
	if actual.version != m.version {
		return LegacyRoomManifestDriftError{Reason: "version mismatch", ExpectedDigest: m.digest, ActualDigest: actual.digest, ExpectedFiles: m.totalFiles, ActualFiles: actual.totalFiles}
	}
	if actual.totalFiles != m.totalFiles || actual.canonicalPaths != m.canonicalPaths || actual.admittedCanonicalPaths != m.admittedCanonicalPaths || actual.noncanonicalArtifacts != m.noncanonicalArtifacts || actual.bodyExceptions != m.bodyExceptions {
		return LegacyRoomManifestDriftError{Reason: "summary mismatch", ExpectedDigest: m.digest, ActualDigest: actual.digest, ExpectedFiles: m.totalFiles, ActualFiles: actual.totalFiles}
	}
	if actual.digest != m.digest {
		return LegacyRoomManifestDriftError{Reason: "digest mismatch", ExpectedDigest: m.digest, ActualDigest: actual.digest, ExpectedFiles: m.totalFiles, ActualFiles: actual.totalFiles}
	}
	return nil
}

// VerifyReviewedAdmissionManifest fails closed unless this catalog exactly
// matches the reviewed source corpus. It does not mutate room state or source
// evidence.
func (c LegacyRoomCatalog) VerifyReviewedAdmissionManifest() error {
	expected := ReviewedLegacyRoomAdmissionManifest()
	return expected.Verify(c.manifest.entries)
}

// LoadReviewedLegacyRoomCatalog is the explicit provisioning/runtime entry
// point for the checked-in corpus. LoadLegacyRoomCatalog remains available for
// controlled synthetic fixtures, while this helper requires the reviewed
// summary and digest before returning a catalog.
func LoadReviewedLegacyRoomCatalog(fsys fs.FS, policy LegacyRoomAdmissionPolicy) (LegacyRoomCatalog, error) {
	catalog, err := LoadLegacyRoomCatalog(fsys, policy)
	if err != nil {
		return LegacyRoomCatalog{}, err
	}
	if err := catalog.VerifyReviewedAdmissionManifest(); err != nil {
		return LegacyRoomCatalog{}, err
	}
	return catalog, nil
}

// VerifyLegacyRoomAdmissionManifest performs an explicit source review check
// without exposing a catalog to the caller.
func VerifyLegacyRoomAdmissionManifest(fsys fs.FS, policy LegacyRoomAdmissionPolicy) error {
	_, err := LoadReviewedLegacyRoomCatalog(fsys, policy)
	return err
}

func cloneLegacyRoomManifestEntries(entries []LegacyRoomManifestEntry) []LegacyRoomManifestEntry {
	out := make([]LegacyRoomManifestEntry, len(entries))
	for i, entry := range entries {
		out[i] = entry
		out[i].Issues = append([]LegacyIssue(nil), entry.Issues...)
	}
	return out
}

func cloneLegacyRoomAdmissionManifest(manifest LegacyRoomAdmissionManifest) LegacyRoomAdmissionManifest {
	manifest.entries = cloneLegacyRoomManifestEntries(manifest.entries)
	return manifest
}

func newLegacyRoomAdmissionManifest(entries []LegacyRoomManifestEntry) LegacyRoomAdmissionManifest {
	owned := cloneLegacyRoomManifestEntries(entries)
	sort.Slice(owned, func(i, j int) bool { return owned[i].Path < owned[j].Path })
	manifest := LegacyRoomAdmissionManifest{
		version:    legacyRoomManifestVersion,
		entries:    owned,
		totalFiles: len(owned),
	}
	for _, entry := range owned {
		switch entry.Class {
		case LegacyRoomCanonical:
			manifest.canonicalPaths++
			if entry.Admitted {
				manifest.admittedCanonicalPaths++
			}
		case LegacyRoomNoncanonicalArtifact:
			manifest.noncanonicalArtifacts++
		}
		if entry.BodyException {
			manifest.bodyExceptions++
		}
	}
	manifest.digest = digestLegacyRoomManifest(owned)
	return manifest
}

func digestLegacyRoomManifest(entries []LegacyRoomManifestEntry) [32]byte {
	h := sha256.New()
	h.Write([]byte(legacyRoomManifestVersion))
	for _, entry := range entries {
		writeLegacyManifestString(h, entry.Path)
		writeLegacyManifestString(h, string(entry.Class))
		if entry.Admitted {
			h.Write([]byte{1})
		} else {
			h.Write([]byte{0})
		}
		if entry.BodyException {
			h.Write([]byte{1})
		} else {
			h.Write([]byte{0})
		}
		writeLegacyManifestInt(h, entry.Size)
		writeLegacyManifestInt(h, entry.Consumed)
		h.Write(entry.SHA256[:])
		writeLegacyManifestInt(h, len(entry.Issues))
		for _, issue := range entry.Issues {
			writeLegacyManifestString(h, issue.Kind)
			writeLegacyManifestInt(h, issue.Offset)
		}
		writeLegacyManifestString(h, entry.DecodeErr)
	}
	var digest [32]byte
	copy(digest[:], h.Sum(nil))
	return digest
}

func writeLegacyManifestString(h interface{ Write([]byte) (int, error) }, value string) {
	writeLegacyManifestInt(h, len(value))
	_, _ = h.Write([]byte(value))
}

func writeLegacyManifestInt(h interface{ Write([]byte) (int, error) }, value int) {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(int64(value)))
	_, _ = h.Write(b[:])
}

func cloneLegacyIssues(issues []LegacyIssue) []LegacyIssue {
	return append([]LegacyIssue(nil), issues...)
}

func hasLegacyBodyException(issues []LegacyIssue) bool {
	for _, issue := range issues {
		switch issue.Kind {
		case "invalid-euc-kr", "missing-text-terminator", "trailing-data":
			return true
		}
	}
	return false
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
	var entries []LegacyRoomManifestEntry
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
		raw, err := fs.ReadFile(fsys, name)
		if err != nil {
			return fmt.Errorf("read room %s: %w", name, err)
		}
		id, canonical := canonicalRoomPath(name)
		if !canonical {
			sha := sha256.Sum256(raw)
			artifact := LegacyRoomIndexIssue{Path: name, Kind: "noncanonical-path", Size: len(raw), SHA256: sha}
			manifestEntry := LegacyRoomManifestEntry{Path: name, Class: LegacyRoomNoncanonicalArtifact, Size: len(raw), SHA256: sha}
			if report, inspectErr := InspectLegacyRoom(raw); inspectErr != nil {
				artifact.DecodeErr = inspectErr.Error()
				manifestEntry.DecodeErr = inspectErr.Error()
			} else {
				artifact.Consumed = report.Consumed
				artifact.Issues = cloneLegacyIssues(report.Issues)
				manifestEntry.Consumed = report.Consumed
				manifestEntry.BodyException = hasLegacyBodyException(report.Issues)
				manifestEntry.Issues = cloneLegacyIssues(report.Issues)
			}
			catalog.ignored = append(catalog.ignored, artifact)
			entries = append(entries, manifestEntry)
			return nil
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
		entries = append(entries, LegacyRoomManifestEntry{
			Path:          name,
			Class:         LegacyRoomCanonical,
			Admitted:      true,
			BodyException: hasLegacyBodyException(admission.Evidence.Issues),
			Size:          len(admission.Evidence.Source),
			Consumed:      admission.Evidence.Consumed,
			SHA256:        admission.Evidence.SHA256,
			Issues:        cloneLegacyIssues(admission.Evidence.Issues),
		})
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
	catalog.manifest = newLegacyRoomAdmissionManifest(entries)
	return catalog, nil
}

// canonicalRoomPath applies the same directory rule as files2.c/load_rom.
func canonicalRoomPath(name string) (int16, bool) {
	clean := path.Clean(name)
	base := path.Base(clean)
	dir := path.Base(path.Dir(clean))
	if clean == "." || path.Dir(path.Dir(clean)) != "." || len(base) != 6 || base[0] != 'r' || len(dir) != 3 || dir[0] != 'r' {
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
	out := make([]LegacyRoomIndexIssue, len(c.ignored))
	for i, issue := range c.ignored {
		out[i] = issue
		out[i].Issues = cloneLegacyIssues(issue.Issues)
	}
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
