package world

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// LegacyVotePathPrefix is the exact path that vote_cmnd constructs from
	// PLAYERPATH, the literal vote directory, and the player's display name.
	// Vote import inputs are repository-rooted; callers using a player-rooted
	// fs.FS can use ReadLegacyVoteFilesFromPlayerRoot instead.
	LegacyVotePathPrefix = "player/vote/"
	legacyVoteNameLimit  = 79
)

var (
	// Import errors are deliberately separate from the runtime vote errors.
	// They identify a source-admission failure and let a migration tool report
	// malformed legacy bytes without treating them as a command denial.
	ErrVoteImportSourceUnavailable = errors.New("legacy vote source is unavailable")
	ErrVoteImportSourceInvalid     = errors.New("legacy vote source is invalid")
	ErrVoteImportPath              = errors.New("legacy vote path is not canonical")
	ErrVoteImportMalformed         = errors.New("legacy vote file is malformed")
	ErrVoteImportTruncated         = errors.New("legacy vote file is truncated")
	ErrVoteImportDuplicate         = errors.New("legacy vote source contains a duplicate")
	ErrVoteImportUnknownActor      = errors.New("legacy vote actor is unknown")
	ErrVoteImportResolver          = errors.New("legacy vote actor resolver is missing")
	ErrVoteImportChoiceCount       = errors.New("legacy vote choice count is invalid")
	ErrVoteImportChoice            = errors.New("legacy vote choice is invalid")
	ErrVoteImportIssueDigest       = errors.New("legacy vote ISSUE catalog digest mismatch")
	ErrVoteImportAlreadyResolved   = errors.New("canonical vote state is already imported")
)

// Descriptive aliases keep source-adapter terminology stable without adding
// another set of error values that callers would need to compare.
var (
	ErrLegacyVoteSourceUnavailable = ErrVoteImportSourceUnavailable
	ErrLegacyVoteSourceInvalid     = ErrVoteImportSourceInvalid
	ErrLegacyVotePath              = ErrVoteImportPath
	ErrLegacyVoteMalformed         = ErrVoteImportMalformed
	ErrLegacyVoteTruncated         = ErrVoteImportTruncated
	ErrLegacyVoteDuplicate         = ErrVoteImportDuplicate
	ErrLegacyVoteUnknownActor      = ErrVoteImportUnknownActor
	ErrLegacyVoteResolver          = ErrVoteImportResolver
	ErrLegacyVoteChoiceCount       = ErrVoteImportChoiceCount
	ErrLegacyVoteChoice            = ErrVoteImportChoice
	ErrLegacyVoteIssueDigest       = ErrVoteImportIssueDigest
	ErrLegacyVoteAlreadyImported   = ErrVoteImportAlreadyResolved
	ErrVoteImportMissingSource     = ErrVoteImportSourceUnavailable
	ErrVoteImportInvalid           = ErrVoteImportSourceInvalid
	ErrVoteImportDigestMismatch    = ErrVoteImportIssueDigest
)

// LegacyVoteFile is one exact source file. Path is not a display-name hint:
// it must be exactly player/vote/<name>_v. Bytes are copied before they are
// validated, so importing cannot mutate a caller-owned source buffer.
//
// Data and Source are compatibility spellings for adapters that call the
// byte payload by a different name. At most one of Bytes, Data, and Source
// may be supplied for a record.
type LegacyVoteFile struct {
	Path   string
	Bytes  []byte
	Data   []byte
	Source []byte
}

// LegacyVotePlayerIDResolver is the only callback form accepted for mapping
// a legacy path name to a canonical player ID. The importer never scans
// State.Players by display name and never performs case folding.
type LegacyVotePlayerIDResolver func(legacyName string) (string, error)

// VotePlayerIDResolver is the shorter source-adapter spelling.
type VotePlayerIDResolver = LegacyVotePlayerIDResolver

// LegacyVoteImportInput is a complete, explicit migration source. Exactly
// one of Records or Files must be non-nil; a nonnil empty collection means
// that the legacy vote directory was inspected and contained no files.
//
// Catalog is the server-owned ISSUE snapshot. Its digest is computed from
// the validated snapshot. Any non-empty digest field is an independently
// reviewed expected digest and must match that computed value. Resolver or
// one of the explicit name maps is required when files are present; no map
// is needed for an explicitly empty source.
type LegacyVoteImportInput struct {
	Records []LegacyVoteFile
	Files   map[string][]byte

	Catalog VoteCatalog

	// CatalogDigest is the primary expected-digest field.
	CatalogDigest string
	// ExpectedCatalogDigest and IssueDigest are descriptive aliases. If more
	// than one is supplied they must all carry the same value.
	ExpectedCatalogDigest string
	IssueDigest           string

	Resolver        LegacyVotePlayerIDResolver
	ResolvePlayerID LegacyVotePlayerIDResolver
	NameResolver    LegacyVotePlayerIDResolver

	// NameToID is the explicit legacy-name -> canonical-player-ID map. The
	// additional fields are aliases for callers whose schema uses a longer
	// name; conflicting nonnil maps are rejected rather than guessed.
	NameToID       map[string]string
	PlayerIDs      map[string]string
	NameToPlayerID map[string]string
}

// LegacyVoteFileEvidence is the immutable source evidence generated while a
// plan is built. It retains exact bytes, path, and SHA-256 without putting
// legacy file data into State.Votes. Evidence is returned only by planning
// APIs and is never sent to a game client.
type LegacyVoteFileEvidence struct {
	Path       string
	LegacyName string
	ActorID    string
	Size       int
	SHA256     [32]byte
	Bytes      []byte
}

// LegacyVoteImportPlan is a fully validated, deterministic candidate for
// installing State.Votes. History is intentionally not fabricated: the
// legacy active files contain only current choices and no operation log.
type LegacyVoteImportPlan struct {
	CatalogDigest string
	Ballots       map[string]VoteBallot
	Evidence      []LegacyVoteFileEvidence
}

// voteImportRecords copies and canonicalizes either accepted source shape.
// Map keys are sorted before records are created, which fixes resolver call
// order and makes evidence deterministic across Go map iteration orders.
func (in LegacyVoteImportInput) voteImportRecords() ([]LegacyVoteFile, error) {
	if in.Records != nil && in.Files != nil {
		return nil, ErrVoteImportSourceInvalid
	}
	if in.Records == nil && in.Files == nil {
		return nil, ErrVoteImportSourceUnavailable
	}
	if in.Records != nil {
		records := make([]LegacyVoteFile, len(in.Records))
		for i, record := range in.Records {
			bytes, err := legacyVoteRecordBytes(record)
			if err != nil {
				return nil, fmt.Errorf("record %d: %w", i, err)
			}
			records[i] = LegacyVoteFile{Path: record.Path, Bytes: bytes}
		}
		return records, nil
	}

	paths := make([]string, 0, len(in.Files))
	for sourcePath := range in.Files {
		paths = append(paths, sourcePath)
	}
	sort.Strings(paths)
	records := make([]LegacyVoteFile, 0, len(paths))
	for _, sourcePath := range paths {
		records = append(records, LegacyVoteFile{
			Path:  sourcePath,
			Bytes: append([]byte(nil), in.Files[sourcePath]...),
		})
	}
	return records, nil
}

func legacyVoteRecordBytes(record LegacyVoteFile) ([]byte, error) {
	provided := 0
	if record.Bytes != nil {
		provided++
	}
	if record.Data != nil {
		provided++
	}
	if record.Source != nil {
		provided++
	}
	if provided > 1 {
		return nil, ErrVoteImportSourceInvalid
	}
	switch {
	case record.Bytes != nil:
		return append([]byte(nil), record.Bytes...), nil
	case record.Data != nil:
		return append([]byte(nil), record.Data...), nil
	case record.Source != nil:
		return append([]byte(nil), record.Source...), nil
	default:
		// A nil payload is retained as nil. The exact-count check reports it as
		// truncated for every valid ISSUE, rather than silently treating a
		// missing record as an empty vote.
		return nil, nil
	}
}

func (in LegacyVoteImportInput) expectedCatalogDigest() (string, error) {
	values := []string{}
	for _, value := range []string{in.CatalogDigest, in.ExpectedCatalogDigest, in.IssueDigest} {
		if value != "" {
			values = append(values, value)
		}
	}
	if len(values) > 1 {
		for _, value := range values[1:] {
			if value != values[0] {
				return "", ErrVoteImportIssueDigest
			}
		}
	}
	return func() (string, error) {
		computed, err := in.Catalog.Digest()
		if err != nil {
			return "", err
		}
		if len(values) == 0 {
			return computed, nil
		}
		if !validVoteCatalogDigest(values[0]) || values[0] != computed {
			return "", ErrVoteImportIssueDigest
		}
		return computed, nil
	}()
}

func (in LegacyVoteImportInput) resolvePlayerID() (LegacyVotePlayerIDResolver, error) {
	callbacks := []LegacyVotePlayerIDResolver{}
	for _, callback := range []LegacyVotePlayerIDResolver{in.Resolver, in.ResolvePlayerID, in.NameResolver} {
		if callback != nil {
			callbacks = append(callbacks, callback)
		}
	}
	if len(callbacks) > 1 {
		return nil, ErrVoteImportSourceInvalid
	}
	maps := []map[string]string{}
	for _, mapping := range []map[string]string{in.NameToID, in.PlayerIDs, in.NameToPlayerID} {
		if mapping != nil {
			maps = append(maps, mapping)
		}
	}
	if len(maps) > 1 {
		return nil, ErrVoteImportSourceInvalid
	}
	if len(callbacks) == 1 && len(maps) == 1 {
		return nil, ErrVoteImportSourceInvalid
	}
	if len(callbacks) == 1 {
		return callbacks[0], nil
	}
	if len(maps) == 1 {
		mapping := maps[0]
		return func(name string) (string, error) {
			id, ok := mapping[name]
			if !ok {
				return "", ErrVoteImportUnknownActor
			}
			return id, nil
		}, nil
	}
	return nil, nil
}

func legacyVoteName(sourcePath string) (string, error) {
	if !fs.ValidPath(sourcePath) || path.Clean(sourcePath) != sourcePath || !strings.HasPrefix(sourcePath, LegacyVotePathPrefix) {
		return "", ErrVoteImportPath
	}
	filename := strings.TrimPrefix(sourcePath, LegacyVotePathPrefix)
	if filename == "" || strings.ContainsAny(filename, "/\\") || !strings.HasSuffix(filename, "_v") {
		return "", ErrVoteImportPath
	}
	name := strings.TrimSuffix(filename, "_v")
	if name == "" || name == "." || name == ".." || len(name) > legacyVoteNameLimit || !utf8.ValidString(name) {
		return "", ErrVoteImportPath
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return "", ErrVoteImportPath
		}
	}
	return name, nil
}

func cloneLegacyVoteEvidence(evidence LegacyVoteFileEvidence) LegacyVoteFileEvidence {
	evidence.Bytes = append([]byte(nil), evidence.Bytes...)
	return evidence
}

// PlanLegacyVoteImport validates the complete legacy source without changing
// State. Records are sorted by exact path, so resolver invocation, duplicate
// detection, ballots, and returned evidence are deterministic. A nil
// State.Votes is required: an existing nonnil aggregate is already an
// explicit migration marker and must not be overwritten.
func (s State) PlanLegacyVoteImport(input LegacyVoteImportInput) (LegacyVoteImportPlan, error) {
	if err := s.Validate(); err != nil {
		return LegacyVoteImportPlan{}, err
	}
	if s.Votes != nil {
		return LegacyVoteImportPlan{}, ErrVoteImportAlreadyResolved
	}
	digest, err := input.expectedCatalogDigest()
	if err != nil {
		if errors.Is(err, ErrVoteImportIssueDigest) {
			return LegacyVoteImportPlan{}, err
		}
		return LegacyVoteImportPlan{}, fmt.Errorf("%w: %w", ErrVoteImportMalformed, err)
	}
	records, err := input.voteImportRecords()
	if err != nil {
		return LegacyVoteImportPlan{}, err
	}
	resolver, err := input.resolvePlayerID()
	if err != nil {
		return LegacyVoteImportPlan{}, err
	}
	if len(records) > 0 && resolver == nil {
		return LegacyVoteImportPlan{}, fmt.Errorf("%w: %w", ErrVoteImportUnknownActor, ErrVoteImportResolver)
	}

	// A slice source may repeat a path, while a map source cannot. Sorting a
	// copied slice makes both forms use one duplicate/error ordering.
	sort.SliceStable(records, func(i, j int) bool { return records[i].Path < records[j].Path })
	ballots := make(map[string]VoteBallot, len(records))
	evidence := make([]LegacyVoteFileEvidence, 0, len(records))
	seenPaths := make(map[string]struct{}, len(records))
	seenNames := make(map[string]struct{}, len(records))
	for index, record := range records {
		name, err := legacyVoteName(record.Path)
		if err != nil {
			return LegacyVoteImportPlan{}, fmt.Errorf("record %d: %w", index, err)
		}
		if _, exists := seenPaths[record.Path]; exists {
			return LegacyVoteImportPlan{}, fmt.Errorf("%w: path %q", ErrVoteImportDuplicate, record.Path)
		}
		seenPaths[record.Path] = struct{}{}
		if _, exists := seenNames[name]; exists {
			return LegacyVoteImportPlan{}, fmt.Errorf("%w: legacy name %q", ErrVoteImportDuplicate, name)
		}
		seenNames[name] = struct{}{}

		if len(record.Bytes) < input.Catalog.Issue.Number {
			return LegacyVoteImportPlan{}, fmt.Errorf("%w: path %q has %d bytes, expected %d", ErrVoteImportTruncated, record.Path, len(record.Bytes), input.Catalog.Issue.Number)
		}
		if len(record.Bytes) > input.Catalog.Issue.Number {
			return LegacyVoteImportPlan{}, fmt.Errorf("%w: path %q has %d bytes, expected %d", ErrVoteImportChoiceCount, record.Path, len(record.Bytes), input.Catalog.Issue.Number)
		}
		for choiceIndex, choice := range record.Bytes {
			if choice < 'A' || choice > 'G' {
				return LegacyVoteImportPlan{}, fmt.Errorf("%w: path %q byte %d", ErrVoteImportChoice, record.Path, choiceIndex)
			}
		}

		actorID, resolveErr := resolver(name)
		if resolveErr != nil {
			return LegacyVoteImportPlan{}, fmt.Errorf("%w: name %q: %w", ErrVoteImportUnknownActor, name, resolveErr)
		}
		if actorID == "" {
			return LegacyVoteImportPlan{}, fmt.Errorf("%w: name %q resolved to an empty ID", ErrVoteImportUnknownActor, name)
		}
		if _, exists := s.Players[actorID]; !exists {
			return LegacyVoteImportPlan{}, fmt.Errorf("%w: name %q -> %q", ErrVoteImportUnknownActor, name, actorID)
		}
		if _, exists := ballots[actorID]; exists {
			return LegacyVoteImportPlan{}, fmt.Errorf("%w: canonical actor %q", ErrVoteImportDuplicate, actorID)
		}
		choices := append([]byte(nil), record.Bytes...)
		ballots[actorID] = VoteBallot{CatalogDigest: digest, Choices: choices}
		evidence = append(evidence, LegacyVoteFileEvidence{
			Path:       record.Path,
			LegacyName: name,
			ActorID:    actorID,
			Size:       len(record.Bytes),
			SHA256:     sha256.Sum256(record.Bytes),
			Bytes:      append([]byte(nil), record.Bytes...),
		})
	}
	return LegacyVoteImportPlan{CatalogDigest: digest, Ballots: ballots, Evidence: evidence}, nil
}

// ImportLegacyVotes installs one complete canonical VoteState. The source
// files only contain active choices, so History is initialized as a nonnil
// empty slice rather than inventing Write entries with unknown timestamps.
func (s State) ImportLegacyVotes(input LegacyVoteImportInput) (State, error) {
	plan, err := s.PlanLegacyVoteImport(input)
	if err != nil {
		return State{}, err
	}
	votes := VoteState{
		Ballots: make(map[string]VoteBallot, len(plan.Ballots)),
		History: make([]VoteHistoryEntry, 0),
	}
	for actorID, ballot := range plan.Ballots {
		ballot.Choices = append([]byte(nil), ballot.Choices...)
		votes.Ballots[actorID] = ballot
	}
	next := s.clone()
	next.Votes = &votes
	if err := next.Validate(); err != nil {
		return State{}, fmt.Errorf("%w: %w", ErrVoteImportMalformed, err)
	}
	return next, nil
}

// ImportLegacyVoteState is the state-oriented alias used by migration
// callers that name the destination aggregate explicitly.
func (s State) ImportLegacyVoteState(input LegacyVoteImportInput) (State, error) {
	return s.ImportLegacyVotes(input)
}

// ImportLegacyVotes installs a complete source map through a package-level
// spelling for adapters that do not keep the State as a method receiver.
func ImportLegacyVotes(s State, input LegacyVoteImportInput) (State, error) {
	return s.ImportLegacyVotes(input)
}

// ReadLegacyVoteFiles reads the exact repository-rooted player/vote directory.
// A missing directory is unavailable (unresolved), while an existing empty
// directory returns a nonnil empty slice that can be imported explicitly.
func ReadLegacyVoteFiles(fsys fs.FS) ([]LegacyVoteFile, error) {
	return readLegacyVoteDirectory(fsys, "player/vote", LegacyVotePathPrefix)
}

// ReadLegacyVoteFilesFromPlayerRoot is the equivalent loader for an fs.FS
// rooted at the legacy player directory. Evidence paths are normalized to
// the original repository-relative player/vote/<name>_v form.
func ReadLegacyVoteFilesFromPlayerRoot(fsys fs.FS) ([]LegacyVoteFile, error) {
	return readLegacyVoteDirectory(fsys, "vote", LegacyVotePathPrefix)
}

// ReadLegacyVoteDirectory is a descriptive alias for the repository-rooted
// loader.
func ReadLegacyVoteDirectory(fsys fs.FS) ([]LegacyVoteFile, error) {
	return ReadLegacyVoteFiles(fsys)
}

func readLegacyVoteDirectory(fsys fs.FS, directory, evidencePrefix string) ([]LegacyVoteFile, error) {
	if fsys == nil {
		return nil, ErrVoteImportSourceUnavailable
	}
	info, err := fs.Stat(fsys, directory)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrVoteImportSourceUnavailable, directory, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%w: %s is not a directory", ErrVoteImportSourceInvalid, directory)
	}
	entries, err := fs.ReadDir(fsys, directory)
	if err != nil {
		return nil, fmt.Errorf("%w: read %s: %v", ErrVoteImportSourceUnavailable, directory, err)
	}
	files := make([]LegacyVoteFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&fs.ModeType != 0 {
			return nil, fmt.Errorf("%w: %s/%s", ErrVoteImportPath, directory, entry.Name())
		}
		relativePath := path.Join(evidencePrefix, entry.Name())
		if _, err := legacyVoteName(relativePath); err != nil {
			return nil, fmt.Errorf("%w: %s", ErrVoteImportPath, relativePath)
		}
		raw, err := fs.ReadFile(fsys, path.Join(directory, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("%w: read %s: %v", ErrVoteImportSourceUnavailable, relativePath, err)
		}
		files = append(files, LegacyVoteFile{Path: relativePath, Bytes: append([]byte(nil), raw...)})
	}
	return files, nil
}

// ImportLegacyVotesFromFS performs the explicit repository-rooted source
// read followed by the same all-or-nothing State import.
func (s State) ImportLegacyVotesFromFS(fsys fs.FS, catalog VoteCatalog, resolver LegacyVotePlayerIDResolver) (State, error) {
	files, err := ReadLegacyVoteFiles(fsys)
	if err != nil {
		return State{}, err
	}
	return s.ImportLegacyVotes(LegacyVoteImportInput{Records: files, Catalog: catalog, Resolver: resolver})
}

// ImportLegacyVoteFiles is a convenience alias for explicit record input.
func (s State) ImportLegacyVoteFiles(files []LegacyVoteFile, catalog VoteCatalog, resolver LegacyVotePlayerIDResolver) (State, error) {
	return s.ImportLegacyVotes(LegacyVoteImportInput{Records: files, Catalog: catalog, Resolver: resolver})
}
