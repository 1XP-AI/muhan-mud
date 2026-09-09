package world

// This file is the filesystem half of the legacy-bank migration boundary. It
// only locates and copies one raw bank file; decoding the native object stream
// remains the responsibility of legacy_bank_raw_v1.go. In particular, this
// code never admits a bank account, starts the game runtime, or writes a DB.
//
// The native C locator walks MUHAN_HOME/player/bank/<name> using directory
// descriptors and no-follow opens. The Go implementation keeps the same
// layout and checks, while requiring callers to provide an explicit absolute
// root so a process environment cannot silently redirect an import.

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// LegacyBankFileLocatorV1ParserVersion identifies this filesystem boundary,
	// not the native object parser. The two stages are intentionally audited
	// independently.
	LegacyBankFileLocatorV1ParserVersion = "go-bank-file-locator-v1"

	// LegacyBankFileLocatorV1MaxBytes is the maximum raw source accepted by the
	// locator. It matches the C bank-evidence octet cap and the raw parser's
	// bound, so an operator cannot use the locator to allocate an unbounded
	// migration payload.
	LegacyBankFileLocatorV1MaxBytes = legacyBankRawV1MaxOctets

	legacyBankFileLocatorMaxNameBytes             = 14
	legacyBankFileLocatorMinNameCodepoints        = 1
	legacyBankFileLocatorMaxNameCodepoints        = 12
	legacyBankFileLocatorDirectoryMode     uint32 = 0o700
	legacyBankFileLocatorRegularMode       uint32 = 0o600
)

var (
	// ErrLegacyBankFileLocatorInvalidRoot means that the caller did not supply
	// an explicit absolute root or supplied a root containing a traversal
	// component. A relative root is never resolved against the process cwd.
	ErrLegacyBankFileLocatorInvalidRoot = errors.New("invalid legacy bank locator root")
	// ErrLegacyBankFileLocatorInvalidName is returned before any filesystem
	// operation when the requested player name is not a C-compatible canonical
	// component.
	ErrLegacyBankFileLocatorInvalidName = errors.New("invalid legacy bank locator player name")
	// ErrLegacyBankFileLocatorPathTraversal is kept separate so callers can
	// quarantine an explicit path attack without treating it as a missing file.
	ErrLegacyBankFileLocatorPathTraversal = errors.New("legacy bank locator path traversal rejected")
	ErrLegacyBankFileLocatorNotFound      = errors.New("legacy bank file not found")
	ErrLegacyBankFileLocatorUnsafe        = errors.New("unsafe legacy bank file or directory")
	ErrLegacyBankFileLocatorSizeLimit     = errors.New("legacy bank file exceeds size limit")
	ErrLegacyBankFileLocatorChanged       = errors.New("legacy bank file changed during inspection")
	ErrLegacyBankFileLocatorRead          = errors.New("legacy bank file read failed")
	ErrLegacyBankFileLocatorUnsupportedOS = errors.New("legacy bank locator unsupported on this operating system")
)

// legacyBankFileStat is the small, platform-neutral subset of struct stat
// needed by the locator. Platform files fill it from fstat/fstatat without
// exposing native struct layout to the migration API.
type legacyBankFileStat struct {
	dev       uint64
	ino       uint64
	mode      uint32
	uid       uint64
	gid       uint64
	nlink     uint64
	size      int64
	mtimeSec  int64
	mtimeNsec int64
	ctimeSec  int64
	ctimeNsec int64
	isDir     bool
	isRegular bool
}

// LegacyBankFileMetadataV1 describes only the descriptor-bound source entry.
// It intentionally contains no decoded object, account, character, or
// credential fields. Path is a display/evidence path; Source is returned
// separately and is never persisted by this package.
type LegacyBankFileMetadataV1 struct {
	Root         string
	Path         string
	RelativePath string
	PlayerName   string
	Size         int64
	Mode         fs.FileMode
	ModeBits     uint32
	UID          uint64
	GID          uint64
	Nlink        uint64
	Device       uint64
	Inode        uint64
	ModTime      time.Time
	ChangeTime   time.Time
}

// LegacyBankSnapshotRawV1Source is the owned result of the safe locator. It
// is deliberately a source artifact, not a BankSnapshotV1 and not a gameplay
// state object. Callers may mutate Source after return without affecting the
// file descriptor or a later call.
type LegacyBankSnapshotRawV1Source struct {
	Metadata LegacyBankFileMetadataV1
	Source   []byte
	SHA256   [sha256.Size]byte
	Digest   [sha256.Size]byte
}

// Clone returns a detached source result suitable for handing to another
// migration stage. The fixed-size metadata and digest values are copied by
// value; Source is copied explicitly.
func (s LegacyBankSnapshotRawV1Source) Clone() LegacyBankSnapshotRawV1Source {
	s.Source = append([]byte(nil), s.Source...)
	return s
}

// Aliases make the boundary readable to callers that refer to the artifact
// as a file rather than a source. They do not add a second representation.
type LegacyBankFileSourceV1 = LegacyBankSnapshotRawV1Source
type LegacyBankSnapshotRawV1LocatedFile = LegacyBankSnapshotRawV1Source

// LegacyBankFileLocatorV1 holds an explicit absolute root. Constructing one
// validates only the lexical root contract; each Locate call reopens and
// revalidates the directory tree and current euid ownership.
type LegacyBankFileLocatorV1 struct {
	root string
}

// NewLegacyBankFileLocatorV1 creates a migration-only locator for root.
func NewLegacyBankFileLocatorV1(root string) (LegacyBankFileLocatorV1, error) {
	clean, err := validateLegacyBankFileLocatorRoot(root)
	if err != nil {
		return LegacyBankFileLocatorV1{}, err
	}
	return LegacyBankFileLocatorV1{root: clean}, nil
}

// Root returns the cleaned, caller-supplied root. It does not resolve
// symlinks, and therefore carries no authority beyond the next Locate call's
// descriptor walk.
func (l LegacyBankFileLocatorV1) Root() string { return l.root }

// Locate reads one raw bank source under the locator's root.
func (l LegacyBankFileLocatorV1) Locate(playerName string) (LegacyBankSnapshotRawV1Source, error) {
	return locateLegacyBankSnapshotRawV1(l.root, playerName)
}

// LocateLegacyBankSnapshotRawV1 opens root/player/bank/playerName using a
// descriptor-anchored no-follow walk and returns an owned source copy plus
// metadata. It does not parse the native object stream; pass Source to
// InspectLegacyBankSnapshotRawV1 when an explicit conversion is approved.
func LocateLegacyBankSnapshotRawV1(root, playerName string) (LegacyBankSnapshotRawV1Source, error) {
	return locateLegacyBankSnapshotRawV1(root, playerName)
}

// LocateLegacyBankFileV1 is a descriptive alias for
// LocateLegacyBankSnapshotRawV1.
func LocateLegacyBankFileV1(root, playerName string) (LegacyBankFileSourceV1, error) {
	return LocateLegacyBankSnapshotRawV1(root, playerName)
}

// ReadLegacyBankSnapshotRawV1 is an explicit read-side alias. It remains
// migration-only and returns no live file descriptor.
func ReadLegacyBankSnapshotRawV1(root, playerName string) (LegacyBankSnapshotRawV1Source, error) {
	return LocateLegacyBankSnapshotRawV1(root, playerName)
}

// ValidateLegacyBankFileLocatorPlayerNameV1 reproduces C
// player_name_is_valid for a single path component. UTF-8, byte/codepoint
// bounds, control bytes, and separators are checked before descriptor access.
func ValidateLegacyBankFileLocatorPlayerNameV1(playerName string) error {
	if playerName == "" || !utf8.ValidString(playerName) {
		return ErrLegacyBankFileLocatorInvalidName
	}
	if len(playerName) > legacyBankFileLocatorMaxNameBytes {
		return ErrLegacyBankFileLocatorInvalidName
	}
	codepoints := utf8.RuneCountInString(playerName)
	if codepoints < legacyBankFileLocatorMinNameCodepoints || codepoints > legacyBankFileLocatorMaxNameCodepoints {
		return ErrLegacyBankFileLocatorInvalidName
	}
	if playerName == "." || playerName == ".." {
		return ErrLegacyBankFileLocatorPathTraversal
	}
	for _, b := range []byte(playerName) {
		if b < 32 || b == 127 {
			return ErrLegacyBankFileLocatorInvalidName
		}
		if b == '/' || b == '\\' || b == ':' {
			return ErrLegacyBankFileLocatorPathTraversal
		}
	}
	return nil
}

func validateLegacyBankFileLocatorRoot(root string) (string, error) {
	if root == "" || strings.IndexByte(root, 0) >= 0 || !filepath.IsAbs(root) {
		return "", ErrLegacyBankFileLocatorInvalidRoot
	}
	// Do not silently normalize an explicit traversal component. This is
	// stricter than the C environment resolver by design: the Go migration
	// caller must name its trust root directly.
	for _, component := range strings.Split(root, string(filepath.Separator)) {
		if component == ".." {
			return "", fmt.Errorf("%w: root contains ..", ErrLegacyBankFileLocatorPathTraversal)
		}
	}
	clean := filepath.Clean(root)
	if clean == "." || clean == string(filepath.Separator) && root != string(filepath.Separator) {
		return "", ErrLegacyBankFileLocatorInvalidRoot
	}
	return clean, nil
}

func locateLegacyBankSnapshotRawV1(root, playerName string) (result LegacyBankSnapshotRawV1Source, err error) {
	root, err = validateLegacyBankFileLocatorRoot(root)
	if err != nil {
		return LegacyBankSnapshotRawV1Source{}, err
	}
	if err := ValidateLegacyBankFileLocatorPlayerNameV1(playerName); err != nil {
		return LegacyBankSnapshotRawV1Source{}, err
	}

	euid := uint64(os.Geteuid())
	file, before, err := openLegacyBankTree(root, playerName, euid)
	if err != nil {
		return LegacyBankSnapshotRawV1Source{}, err
	}
	closed := false
	defer func() {
		if !closed {
			if closeErr := file.Close(); err == nil && closeErr != nil {
				result = LegacyBankSnapshotRawV1Source{}
				err = fmt.Errorf("%w: close source: %v", ErrLegacyBankFileLocatorRead, closeErr)
			}
		}
	}()

	if before.size < 0 {
		return LegacyBankSnapshotRawV1Source{}, ErrLegacyBankFileLocatorUnsafe
	}
	if before.size > int64(LegacyBankFileLocatorV1MaxBytes) {
		return LegacyBankSnapshotRawV1Source{}, ErrLegacyBankFileLocatorSizeLimit
	}
	if _, seekErr := file.Seek(0, io.SeekStart); seekErr != nil {
		return LegacyBankSnapshotRawV1Source{}, fmt.Errorf("%w: seek: %v", ErrLegacyBankFileLocatorRead, seekErr)
	}
	raw := make([]byte, int(before.size))
	if _, readErr := io.ReadFull(file, raw); readErr != nil {
		return LegacyBankSnapshotRawV1Source{}, fmt.Errorf("%w: read: %v", ErrLegacyBankFileLocatorRead, readErr)
	}

	afterRead, statErr := legacyBankFstat(file)
	if statErr != nil {
		return LegacyBankSnapshotRawV1Source{}, fmt.Errorf("%w: post-read stat: %v", ErrLegacyBankFileLocatorRead, statErr)
	}
	if !legacyBankFileSafe(afterRead, euid) || !legacyBankSameFile(before, afterRead) {
		return LegacyBankSnapshotRawV1Source{}, ErrLegacyBankFileLocatorChanged
	}

	// Rewalk from the named root. The source fd is anchored to the original
	// bank directory, so this second walk is what catches a root/player/bank
	// replacement that occurred after the first open.
	fresh, freshStat, freshErr := openLegacyBankTree(root, playerName, euid)
	if freshErr != nil {
		if errors.Is(freshErr, ErrLegacyBankFileLocatorNotFound) {
			return LegacyBankSnapshotRawV1Source{}, ErrLegacyBankFileLocatorChanged
		}
		return LegacyBankSnapshotRawV1Source{}, freshErr
	}
	if closeErr := fresh.Close(); closeErr != nil {
		return LegacyBankSnapshotRawV1Source{}, fmt.Errorf("%w: close rewalk: %v", ErrLegacyBankFileLocatorRead, closeErr)
	}
	if !legacyBankSameFile(before, freshStat) {
		return LegacyBankSnapshotRawV1Source{}, ErrLegacyBankFileLocatorChanged
	}

	if closeErr := file.Close(); closeErr != nil {
		closed = true
		return LegacyBankSnapshotRawV1Source{}, fmt.Errorf("%w: close source: %v", ErrLegacyBankFileLocatorRead, closeErr)
	}
	closed = true

	owned := make([]byte, len(raw))
	copy(owned, raw)
	digest := sha256.Sum256(owned)
	metadata := legacyBankFileMetadata(root, playerName, before)
	return LegacyBankSnapshotRawV1Source{
		Metadata: metadata,
		Source:   owned,
		SHA256:   digest,
		Digest:   digest,
	}, nil
}

func openLegacyBankTree(root, playerName string, euid uint64) (*os.File, legacyBankFileStat, error) {
	rootFile, err := legacyBankOpenRoot(root)
	if err != nil {
		return nil, legacyBankFileStat{}, classifyLegacyBankLocatorOpenError(err)
	}
	closeRoot := true
	defer func() {
		if closeRoot {
			_ = rootFile.Close()
		}
	}()
	rootStat, err := legacyBankFstat(rootFile)
	if err != nil {
		return nil, legacyBankFileStat{}, fmt.Errorf("%w: root stat: %v", ErrLegacyBankFileLocatorUnsafe, err)
	}
	if !legacyBankDirectorySafe(rootStat, euid) {
		return nil, legacyBankFileStat{}, ErrLegacyBankFileLocatorUnsafe
	}

	playerDir, _, err := openLegacyBankDirectory(rootFile, "player", euid)
	if err != nil {
		return nil, legacyBankFileStat{}, err
	}
	bankDir, _, err := openLegacyBankDirectory(playerDir, "bank", euid)
	if err != nil {
		_ = playerDir.Close()
		return nil, legacyBankFileStat{}, err
	}
	file, fileStat, err := openLegacyBankFile(bankDir, playerName, euid)
	_ = bankDir.Close()
	_ = playerDir.Close()
	if err != nil {
		return nil, legacyBankFileStat{}, err
	}
	if closeErr := rootFile.Close(); closeErr != nil {
		_ = file.Close()
		return nil, legacyBankFileStat{}, fmt.Errorf("%w: close root: %v", ErrLegacyBankFileLocatorRead, closeErr)
	}
	closeRoot = false
	return file, fileStat, nil
}

func openLegacyBankDirectory(parent *os.File, name string, euid uint64) (*os.File, legacyBankFileStat, error) {
	before, err := legacyBankFstatAt(parent, name)
	if err != nil {
		return nil, legacyBankFileStat{}, classifyLegacyBankLocatorOpenError(err)
	}
	if !legacyBankDirectorySafe(before, euid) {
		return nil, legacyBankFileStat{}, ErrLegacyBankFileLocatorUnsafe
	}
	child, err := legacyBankOpenAt(parent, name, legacyBankDirectoryOpenFlags)
	if err != nil {
		return nil, legacyBankFileStat{}, classifyLegacyBankLocatorOpenError(err)
	}
	opened, err := legacyBankFstat(child)
	if err != nil {
		_ = child.Close()
		return nil, legacyBankFileStat{}, fmt.Errorf("%w: directory stat: %v", ErrLegacyBankFileLocatorUnsafe, err)
	}
	after, err := legacyBankFstatAt(parent, name)
	if err != nil || !legacyBankDirectorySafe(opened, euid) ||
		!legacyBankDirectorySafe(after, euid) || !legacyBankSameEntry(before, opened) || !legacyBankSameEntry(opened, after) {
		_ = child.Close()
		if err != nil {
			return nil, legacyBankFileStat{}, classifyLegacyBankLocatorOpenError(err)
		}
		return nil, legacyBankFileStat{}, ErrLegacyBankFileLocatorChanged
	}
	return child, opened, nil
}

func openLegacyBankFile(parent *os.File, name string, euid uint64) (*os.File, legacyBankFileStat, error) {
	before, err := legacyBankFstatAt(parent, name)
	if err != nil {
		return nil, legacyBankFileStat{}, classifyLegacyBankLocatorOpenError(err)
	}
	if !legacyBankFileSafe(before, euid) {
		if before.size > int64(LegacyBankFileLocatorV1MaxBytes) {
			return nil, legacyBankFileStat{}, ErrLegacyBankFileLocatorSizeLimit
		}
		return nil, legacyBankFileStat{}, ErrLegacyBankFileLocatorUnsafe
	}
	file, err := legacyBankOpenAt(parent, name, legacyBankFileOpenFlags)
	if err != nil {
		return nil, legacyBankFileStat{}, classifyLegacyBankLocatorOpenError(err)
	}
	opened, err := legacyBankFstat(file)
	if err != nil {
		_ = file.Close()
		return nil, legacyBankFileStat{}, fmt.Errorf("%w: file stat: %v", ErrLegacyBankFileLocatorUnsafe, err)
	}
	after, err := legacyBankFstatAt(parent, name)
	if err != nil || !legacyBankFileSafe(opened, euid) || !legacyBankFileSafe(after, euid) ||
		!legacyBankSameEntry(before, opened) || !legacyBankSameEntry(opened, after) {
		_ = file.Close()
		if err != nil {
			return nil, legacyBankFileStat{}, classifyLegacyBankLocatorOpenError(err)
		}
		return nil, legacyBankFileStat{}, ErrLegacyBankFileLocatorChanged
	}
	if opened.size > int64(LegacyBankFileLocatorV1MaxBytes) {
		_ = file.Close()
		return nil, legacyBankFileStat{}, ErrLegacyBankFileLocatorSizeLimit
	}
	return file, opened, nil
}

func classifyLegacyBankLocatorOpenError(err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %w", ErrLegacyBankFileLocatorNotFound, err)
	}
	if errors.Is(err, ErrLegacyBankFileLocatorUnsupportedOS) {
		return err
	}
	return fmt.Errorf("%w: %v", ErrLegacyBankFileLocatorUnsafe, err)
}

func legacyBankDirectorySafe(stat legacyBankFileStat, euid uint64) bool {
	return stat.isDir && stat.uid == euid && stat.mode&0o7777 == legacyBankFileLocatorDirectoryMode
}

func legacyBankFileSafe(stat legacyBankFileStat, euid uint64) bool {
	return stat.isRegular && stat.nlink == 1 && stat.uid == euid && stat.mode&0o7777 == legacyBankFileLocatorRegularMode &&
		stat.size >= 0 && stat.size <= int64(LegacyBankFileLocatorV1MaxBytes)
}

func legacyBankSameEntry(left, right legacyBankFileStat) bool {
	return left.dev == right.dev && left.ino == right.ino && left.mode == right.mode && left.uid == right.uid &&
		left.gid == right.gid && left.nlink == right.nlink
}

func legacyBankSameFile(left, right legacyBankFileStat) bool {
	return legacyBankSameEntry(left, right) && left.size == right.size && left.mtimeSec == right.mtimeSec &&
		left.mtimeNsec == right.mtimeNsec && left.ctimeSec == right.ctimeSec && left.ctimeNsec == right.ctimeNsec
}

func legacyBankFileMetadata(root, playerName string, stat legacyBankFileStat) LegacyBankFileMetadataV1 {
	relative := filepath.Join("player", "bank", playerName)
	return LegacyBankFileMetadataV1{
		Root:         root,
		Path:         filepath.Join(root, relative),
		RelativePath: relative,
		PlayerName:   playerName,
		Size:         stat.size,
		Mode:         fs.FileMode(stat.mode & 0o7777),
		ModeBits:     stat.mode & 0o7777,
		UID:          stat.uid,
		GID:          stat.gid,
		Nlink:        stat.nlink,
		Device:       stat.dev,
		Inode:        stat.ino,
		ModTime:      time.Unix(stat.mtimeSec, stat.mtimeNsec),
		ChangeTime:   time.Unix(stat.ctimeSec, stat.ctimeNsec),
	}
}
