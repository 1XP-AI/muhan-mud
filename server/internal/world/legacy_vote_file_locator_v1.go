package world

// This file is the migration-only filesystem boundary for the legacy
// player/vote/<name>_v files.  It deliberately stops at an owned byte copy:
// identity mapping, ISSUE validation, and canonical VoteState installation
// remain separate review/import stages in vote_import.go.
//
// The legacy C code stores one choice byte per ISSUE in a player-rooted
// directory.  A migration process must not follow a renamed directory or a
// symlink supplied by the source tree, so this locator reuses the audited
// openat/fstatat syscall shim used by the bank/memo locators while keeping its
// own error and path contract.

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/1XP-Inc/muhan-mud/server/internal/identity"
)

const (
	LegacyVoteFileLocatorV1ParserVersion = "go-legacy-vote-file-locator-v1"
	// The issue parser rejects any payload whose length differs from the
	// reviewed ISSUE number.  The locator still keeps an independent finite
	// bound so a corrupt file cannot cause an unbounded allocation before that
	// parser runs.
	LegacyVoteFileLocatorV1MaxBytes = 64 << 10

	legacyVoteFileLocatorMaxNameBytes             = 14
	legacyVoteFileLocatorMinNameCodepoints        = 1
	legacyVoteFileLocatorMaxNameCodepoints        = 12
	legacyVoteFileLocatorDirectoryMode     uint32 = 0o700
	legacyVoteFileLocatorRegularMode       uint32 = 0o600
)

var (
	ErrLegacyVoteFileLocatorInvalidRoot   = errors.New("invalid legacy vote locator root")
	ErrLegacyVoteFileLocatorInvalidName   = errors.New("invalid legacy vote locator player name")
	ErrLegacyVoteFileLocatorPathTraversal = errors.New("legacy vote locator path traversal rejected")
	ErrLegacyVoteFileLocatorNotFound      = errors.New("legacy vote file not found")
	ErrLegacyVoteFileLocatorUnsafe        = errors.New("unsafe legacy vote file or directory")
	ErrLegacyVoteFileLocatorSizeLimit     = errors.New("legacy vote file exceeds size limit")
	ErrLegacyVoteFileLocatorChanged       = errors.New("legacy vote file changed during inspection")
	ErrLegacyVoteFileLocatorRead          = errors.New("legacy vote file read failed")
	ErrLegacyVoteFileLocatorUnsupportedOS = errors.New("legacy vote locator unsupported on this operating system")
	ErrLegacyVoteFileLocatorUnexpected    = errors.New("unexpected legacy vote directory entry")
)

// LegacyVoteFileMetadataV1 is filesystem evidence only.  It is not copied
// into a VoteState or a runtime command request.
type LegacyVoteFileMetadataV1 struct {
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

// LegacyVoteFileSourceV1 owns the bytes returned from one raw file.  The
// digest is over Source and is useful to a later operator review manifest.
type LegacyVoteFileSourceV1 struct {
	Metadata LegacyVoteFileMetadataV1
	Source   []byte
	SHA256   [sha256.Size]byte
	Digest   [sha256.Size]byte
}

func (s LegacyVoteFileSourceV1) Clone() LegacyVoteFileSourceV1 {
	s.Source = append([]byte(nil), s.Source...)
	return s
}

// ImportRecord exposes an owned source as the existing vote importer input
// shape without carrying filesystem metadata into the canonical state.
func (s LegacyVoteFileSourceV1) ImportRecord() LegacyVoteFile {
	return LegacyVoteFile{Path: s.Metadata.RelativePath, Bytes: append([]byte(nil), s.Source...)}
}

// ReadLegacyVoteFilesFromRootV1 is the explicit-root spelling used by
// migration tooling. It is intentionally separate from ReadLegacyVoteFiles,
// whose fs.FS API is retained for synthetic fixtures and compatibility.
func ReadLegacyVoteFilesFromRootV1(root string) ([]LegacyVoteFile, error) {
	sources, err := LocateLegacyVoteRawFilesV1(root)
	if err != nil {
		return nil, err
	}
	files := make([]LegacyVoteFile, len(sources))
	for i, source := range sources {
		files[i] = source.ImportRecord()
	}
	return files, nil
}

// LegacyVoteFileLocatorV1 stores only an explicit absolute source root.  Each
// Locate call reopens and revalidates the tree, so replacing the source tree
// cannot preserve authority from an earlier call.
type LegacyVoteFileLocatorV1 struct{ root string }

func NewLegacyVoteFileLocatorV1(root string) (LegacyVoteFileLocatorV1, error) {
	clean, err := validateLegacyVoteFileLocatorRoot(root)
	if err != nil {
		return LegacyVoteFileLocatorV1{}, err
	}
	return LegacyVoteFileLocatorV1{root: clean}, nil
}

func (l LegacyVoteFileLocatorV1) Root() string { return l.root }

func (l LegacyVoteFileLocatorV1) Locate(playerName string) (LegacyVoteFileSourceV1, error) {
	return LocateLegacyVoteFileV1(l.root, playerName)
}

// ValidateLegacyVoteFileLocatorPlayerNameV1 accepts exactly the canonical
// path component used by the game.  No case folding or Unicode normalization
// is performed here.
func ValidateLegacyVoteFileLocatorPlayerNameV1(playerName string) error {
	if playerName == "" || !utf8.ValidString(playerName) {
		return ErrLegacyVoteFileLocatorInvalidName
	}
	if len(playerName) > legacyVoteFileLocatorMaxNameBytes {
		return ErrLegacyVoteFileLocatorInvalidName
	}
	runes := utf8.RuneCountInString(playerName)
	if runes < legacyVoteFileLocatorMinNameCodepoints || runes > legacyVoteFileLocatorMaxNameCodepoints {
		return ErrLegacyVoteFileLocatorInvalidName
	}
	if playerName == "." || playerName == ".." {
		return ErrLegacyVoteFileLocatorPathTraversal
	}
	for _, b := range []byte(playerName) {
		if b < 32 || b == 127 {
			return ErrLegacyVoteFileLocatorInvalidName
		}
		if b == '/' || b == '\\' || b == ':' {
			return ErrLegacyVoteFileLocatorPathTraversal
		}
	}
	canonical, err := identity.CanonicalName(playerName)
	if err != nil {
		return ErrLegacyVoteFileLocatorInvalidName
	}
	if canonical != playerName {
		return ErrLegacyVoteFileLocatorInvalidName
	}
	return nil
}

func validateLegacyVoteFileLocatorRoot(root string) (string, error) {
	if root == "" || strings.IndexByte(root, 0) >= 0 || !filepath.IsAbs(root) {
		return "", ErrLegacyVoteFileLocatorInvalidRoot
	}
	for _, component := range strings.Split(root, string(filepath.Separator)) {
		if component == ".." {
			return "", ErrLegacyVoteFileLocatorPathTraversal
		}
	}
	clean := filepath.Clean(root)
	if clean == "." || clean == string(filepath.Separator) {
		return "", ErrLegacyVoteFileLocatorInvalidRoot
	}
	return clean, nil
}

// LocateLegacyVoteFileV1 copies root/player/vote/<playerName>_v and returns
// only filesystem evidence plus an owned byte slice.
func LocateLegacyVoteFileV1(root, playerName string) (result LegacyVoteFileSourceV1, err error) {
	root, err = validateLegacyVoteFileLocatorRoot(root)
	if err != nil {
		return LegacyVoteFileSourceV1{}, err
	}
	if err := ValidateLegacyVoteFileLocatorPlayerNameV1(playerName); err != nil {
		return LegacyVoteFileSourceV1{}, err
	}

	euid := uint64(os.Geteuid())
	file, before, err := openLegacyVoteTree(root, playerName, euid)
	if err != nil {
		return LegacyVoteFileSourceV1{}, err
	}
	closed := false
	defer func() {
		if !closed {
			if closeErr := file.Close(); err == nil && closeErr != nil {
				result = LegacyVoteFileSourceV1{}
				err = fmt.Errorf("%w: close source: %v", ErrLegacyVoteFileLocatorRead, closeErr)
			}
		}
	}()
	if before.size < 0 {
		return LegacyVoteFileSourceV1{}, ErrLegacyVoteFileLocatorUnsafe
	}
	if before.size > int64(LegacyVoteFileLocatorV1MaxBytes) {
		return LegacyVoteFileSourceV1{}, ErrLegacyVoteFileLocatorSizeLimit
	}
	if _, seekErr := file.Seek(0, io.SeekStart); seekErr != nil {
		return LegacyVoteFileSourceV1{}, fmt.Errorf("%w: seek: %v", ErrLegacyVoteFileLocatorRead, seekErr)
	}
	raw := make([]byte, int(before.size))
	if _, readErr := io.ReadFull(file, raw); readErr != nil {
		return LegacyVoteFileSourceV1{}, fmt.Errorf("%w: read: %v", ErrLegacyVoteFileLocatorRead, readErr)
	}
	after, statErr := legacyBankFstat(file)
	if statErr != nil {
		return LegacyVoteFileSourceV1{}, fmt.Errorf("%w: post-read stat: %v", ErrLegacyVoteFileLocatorRead, statErr)
	}
	if !legacyVoteFileSafe(after, euid) || !legacyBankSameFile(before, after) {
		return LegacyVoteFileSourceV1{}, ErrLegacyVoteFileLocatorChanged
	}

	fresh, freshStat, freshErr := openLegacyVoteTree(root, playerName, euid)
	if freshErr != nil {
		if errors.Is(freshErr, ErrLegacyVoteFileLocatorNotFound) {
			return LegacyVoteFileSourceV1{}, ErrLegacyVoteFileLocatorChanged
		}
		return LegacyVoteFileSourceV1{}, freshErr
	}
	if closeErr := fresh.Close(); closeErr != nil {
		return LegacyVoteFileSourceV1{}, fmt.Errorf("%w: close rewalk: %v", ErrLegacyVoteFileLocatorRead, closeErr)
	}
	if !legacyBankSameFile(before, freshStat) {
		return LegacyVoteFileSourceV1{}, ErrLegacyVoteFileLocatorChanged
	}
	if closeErr := file.Close(); closeErr != nil {
		closed = true
		return LegacyVoteFileSourceV1{}, fmt.Errorf("%w: close source: %v", ErrLegacyVoteFileLocatorRead, closeErr)
	}
	closed = true
	owned := append([]byte(nil), raw...)
	digest := sha256.Sum256(owned)
	return LegacyVoteFileSourceV1{
		Metadata: legacyVoteFileMetadata(root, playerName, before),
		Source:   owned,
		SHA256:   digest,
		Digest:   digest,
	}, nil
}

// LocateLegacyVoteRawFilesV1 enumerates one explicit vote directory and
// locates every <canonical-name>_v file in lexical order.  It rejects every
// other entry and rechecks the directory after reading so a newly introduced
// or replaced file cannot be silently omitted from a migration batch.
func LocateLegacyVoteRawFilesV1(root string) ([]LegacyVoteFileSourceV1, error) {
	root, err := validateLegacyVoteFileLocatorRoot(root)
	if err != nil {
		return nil, err
	}
	euid := uint64(os.Geteuid())
	entries, err := legacyVoteDirectoryEntries(root, euid)
	if err != nil {
		return nil, err
	}
	result := make([]LegacyVoteFileSourceV1, 0, len(entries))
	for _, name := range entries {
		playerName := strings.TrimSuffix(name, "_v")
		source, locateErr := LocateLegacyVoteFileV1(root, playerName)
		if locateErr != nil {
			return nil, fmt.Errorf("%s: %w", name, locateErr)
		}
		result = append(result, source)
	}
	second, err := legacyVoteDirectoryEntries(root, euid)
	if err != nil {
		return nil, ErrLegacyVoteFileLocatorChanged
	}
	if len(entries) != len(second) {
		return nil, ErrLegacyVoteFileLocatorChanged
	}
	for i := range entries {
		if entries[i] != second[i] {
			return nil, ErrLegacyVoteFileLocatorChanged
		}
	}
	return result, nil
}

func legacyVoteDirectoryEntries(root string, euid uint64) ([]string, error) {
	rootFile, err := legacyBankOpenRoot(root)
	if err != nil {
		return nil, classifyLegacyVoteLocatorOpenError(err)
	}
	defer rootFile.Close()
	rootStat, err := legacyBankFstat(rootFile)
	if err != nil || !legacyVoteDirectorySafe(rootStat, euid) {
		return nil, ErrLegacyVoteFileLocatorUnsafe
	}
	playerDir, _, err := openLegacyVoteDirectory(rootFile, "player", euid)
	if err != nil {
		return nil, err
	}
	defer playerDir.Close()
	voteDir, _, err := openLegacyVoteDirectory(playerDir, "vote", euid)
	if err != nil {
		return nil, err
	}
	defer voteDir.Close()
	names, err := voteDir.Readdirnames(-1)
	if err != nil {
		return nil, fmt.Errorf("%w: enumerate vote directory: %v", ErrLegacyVoteFileLocatorRead, err)
	}
	sort.Strings(names)
	for _, name := range names {
		if !strings.HasSuffix(name, "_v") || len(name) <= 2 {
			return nil, fmt.Errorf("%w: %q", ErrLegacyVoteFileLocatorUnexpected, name)
		}
		playerName := strings.TrimSuffix(name, "_v")
		if err := ValidateLegacyVoteFileLocatorPlayerNameV1(playerName); err != nil {
			return nil, fmt.Errorf("%w: %q", ErrLegacyVoteFileLocatorUnexpected, name)
		}
		stat, statErr := legacyBankFstatAt(voteDir, name)
		if statErr != nil {
			return nil, ErrLegacyVoteFileLocatorChanged
		}
		if !legacyVoteFileSafe(stat, euid) {
			if stat.size > int64(LegacyVoteFileLocatorV1MaxBytes) {
				return nil, ErrLegacyVoteFileLocatorSizeLimit
			}
			return nil, ErrLegacyVoteFileLocatorUnsafe
		}
	}
	return names, nil
}

func openLegacyVoteTree(root, playerName string, euid uint64) (*os.File, legacyBankFileStat, error) {
	rootFile, err := legacyBankOpenRoot(root)
	if err != nil {
		return nil, legacyBankFileStat{}, classifyLegacyVoteLocatorOpenError(err)
	}
	closeRoot := true
	defer func() {
		if closeRoot {
			_ = rootFile.Close()
		}
	}()
	rootStat, err := legacyBankFstat(rootFile)
	if err != nil || !legacyVoteDirectorySafe(rootStat, euid) {
		return nil, legacyBankFileStat{}, ErrLegacyVoteFileLocatorUnsafe
	}
	playerDir, _, err := openLegacyVoteDirectory(rootFile, "player", euid)
	if err != nil {
		return nil, legacyBankFileStat{}, err
	}
	voteDir, _, err := openLegacyVoteDirectory(playerDir, "vote", euid)
	_ = playerDir.Close()
	if err != nil {
		return nil, legacyBankFileStat{}, err
	}
	file, stat, err := openLegacyVoteFile(voteDir, playerName+"_v", euid)
	_ = voteDir.Close()
	if err != nil {
		return nil, legacyBankFileStat{}, err
	}
	if closeErr := rootFile.Close(); closeErr != nil {
		_ = file.Close()
		return nil, legacyBankFileStat{}, fmt.Errorf("%w: close root: %v", ErrLegacyVoteFileLocatorRead, closeErr)
	}
	closeRoot = false
	return file, stat, nil
}

func openLegacyVoteDirectory(parent *os.File, name string, euid uint64) (*os.File, legacyBankFileStat, error) {
	before, err := legacyBankFstatAt(parent, name)
	if err != nil {
		return nil, legacyBankFileStat{}, classifyLegacyVoteLocatorOpenError(err)
	}
	if !legacyVoteDirectorySafe(before, euid) {
		return nil, legacyBankFileStat{}, ErrLegacyVoteFileLocatorUnsafe
	}
	child, err := legacyBankOpenAt(parent, name, legacyBankDirectoryOpenFlags)
	if err != nil {
		return nil, legacyBankFileStat{}, classifyLegacyVoteLocatorOpenError(err)
	}
	opened, err := legacyBankFstat(child)
	if err != nil {
		_ = child.Close()
		return nil, legacyBankFileStat{}, ErrLegacyVoteFileLocatorUnsafe
	}
	after, err := legacyBankFstatAt(parent, name)
	if err != nil || !legacyVoteDirectorySafe(opened, euid) || !legacyVoteDirectorySafe(after, euid) ||
		!legacyBankSameEntry(before, opened) || !legacyBankSameEntry(opened, after) {
		_ = child.Close()
		if err != nil {
			return nil, legacyBankFileStat{}, classifyLegacyVoteLocatorOpenError(err)
		}
		return nil, legacyBankFileStat{}, ErrLegacyVoteFileLocatorChanged
	}
	return child, opened, nil
}

func openLegacyVoteFile(parent *os.File, name string, euid uint64) (*os.File, legacyBankFileStat, error) {
	before, err := legacyBankFstatAt(parent, name)
	if err != nil {
		return nil, legacyBankFileStat{}, classifyLegacyVoteLocatorOpenError(err)
	}
	if !legacyVoteFileSafe(before, euid) {
		if before.size > int64(LegacyVoteFileLocatorV1MaxBytes) {
			return nil, legacyBankFileStat{}, ErrLegacyVoteFileLocatorSizeLimit
		}
		return nil, legacyBankFileStat{}, ErrLegacyVoteFileLocatorUnsafe
	}
	file, err := legacyBankOpenAt(parent, name, legacyBankFileOpenFlags)
	if err != nil {
		return nil, legacyBankFileStat{}, classifyLegacyVoteLocatorOpenError(err)
	}
	opened, err := legacyBankFstat(file)
	if err != nil {
		_ = file.Close()
		return nil, legacyBankFileStat{}, ErrLegacyVoteFileLocatorUnsafe
	}
	after, err := legacyBankFstatAt(parent, name)
	if err != nil || !legacyVoteFileSafe(opened, euid) || !legacyVoteFileSafe(after, euid) ||
		!legacyBankSameEntry(before, opened) || !legacyBankSameEntry(opened, after) {
		_ = file.Close()
		if err != nil {
			return nil, legacyBankFileStat{}, classifyLegacyVoteLocatorOpenError(err)
		}
		return nil, legacyBankFileStat{}, ErrLegacyVoteFileLocatorChanged
	}
	if opened.size > int64(LegacyVoteFileLocatorV1MaxBytes) {
		_ = file.Close()
		return nil, legacyBankFileStat{}, ErrLegacyVoteFileLocatorSizeLimit
	}
	return file, opened, nil
}

func classifyLegacyVoteLocatorOpenError(err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %w", ErrLegacyVoteFileLocatorNotFound, err)
	}
	if errors.Is(err, ErrLegacyBankFileLocatorUnsupportedOS) {
		return ErrLegacyVoteFileLocatorUnsupportedOS
	}
	return fmt.Errorf("%w: %v", ErrLegacyVoteFileLocatorUnsafe, err)
}

func legacyVoteDirectorySafe(stat legacyBankFileStat, euid uint64) bool {
	return stat.isDir && stat.uid == euid && stat.mode&0o7777 == legacyVoteFileLocatorDirectoryMode
}

func legacyVoteFileSafe(stat legacyBankFileStat, euid uint64) bool {
	return stat.isRegular && stat.nlink == 1 && stat.uid == euid && stat.mode&0o7777 == legacyVoteFileLocatorRegularMode &&
		stat.size >= 0 && stat.size <= int64(LegacyVoteFileLocatorV1MaxBytes)
}

func legacyVoteFileMetadata(root, playerName string, stat legacyBankFileStat) LegacyVoteFileMetadataV1 {
	relative := filepath.Join("player", "vote", playerName+"_v")
	return LegacyVoteFileMetadataV1{
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
