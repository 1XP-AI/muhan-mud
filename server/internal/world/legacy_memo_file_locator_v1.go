package world

// This file is the filesystem half of the legacy memo migration boundary.
// It only locates and copies one raw player/fal file.  The parser in
// legacy_memo_parser_v1.go is a separate, explicitly ABI-bound stage.  No
// function in this file admits an account, starts the game runtime, or writes
// a database.
//
// command12.c writes memo records below PLAYERPATH/fal/<name>.  The walk is
// descriptor anchored and no-follow at every named component.  The existing
// platform syscall shim used by the bank locator is deliberately reused here:
// it provides openat/fstatat without putting native stat layout in this API.

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

	"github.com/1XP-Inc/muhan-mud/server/internal/identity"
)

const (
	// LegacyMemoFileLocatorV1ParserVersion identifies the filesystem boundary,
	// not the text parser.  They are audited independently because the source
	// file has no self-describing format marker.
	LegacyMemoFileLocatorV1ParserVersion = "go-legacy-memo-file-locator-v1"

	// The file bound is intentionally finite even though the C writer appends
	// without an aggregate cap.  It keeps a migration process from allocating
	// unbounded memory for a corrupt or adversarial source file.
	LegacyMemoFileLocatorV1MaxBytes = 4 * 1024 * 1024

	legacyMemoFileLocatorMaxNameBytes             = 14
	legacyMemoFileLocatorMinNameCodepoints        = 1
	legacyMemoFileLocatorMaxNameCodepoints        = 12
	legacyMemoFileLocatorDirectoryMode     uint32 = 0o700
	legacyMemoFileLocatorRegularMode       uint32 = 0o600
)

var (
	ErrLegacyMemoFileLocatorInvalidRoot   = errors.New("invalid legacy memo locator root")
	ErrLegacyMemoFileLocatorInvalidName   = errors.New("invalid legacy memo locator recipient name")
	ErrLegacyMemoFileLocatorNotFound      = errors.New("legacy memo file not found")
	ErrLegacyMemoFileLocatorUnsafe        = errors.New("unsafe legacy memo file or directory")
	ErrLegacyMemoFileLocatorSizeLimit     = errors.New("legacy memo file exceeds size limit")
	ErrLegacyMemoFileLocatorChanged       = errors.New("legacy memo file changed during inspection")
	ErrLegacyMemoFileLocatorRead          = errors.New("legacy memo file read failed")
	ErrLegacyMemoFileLocatorUnsupportedOS = errors.New("legacy memo locator unsupported on this operating system")

	// Keep traversal distinct so an operator can quarantine an explicit path
	// attack without treating it as a missing recipient file.
	ErrLegacyMemoFileLocatorPathTraversal = errors.New("legacy memo locator path traversal rejected")
	// A non-canonical component is different from a path attack, but both are
	// rejected before any filesystem operation.
	ErrLegacyMemoFileLocatorNonCanonicalName = errors.New("legacy memo locator name is not canonical")
)

// legacyMemoFileStat is the platform-neutral subset needed for the memo
// locator.  The fields are populated by the existing no-follow syscall shim.
type legacyMemoFileStat struct {
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

// LegacyMemoFileMetadataV1 contains only descriptor-bound filesystem
// evidence.  Path is useful to an operator reviewing a quarantine report; it
// is never copied into CharacterMemo or a runtime request.
type LegacyMemoFileMetadataV1 struct {
	Root          string
	Path          string
	RelativePath  string
	RecipientName string
	Size          int64
	Mode          fs.FileMode
	ModeBits      uint32
	UID           uint64
	GID           uint64
	Nlink         uint64
	Device        uint64
	Inode         uint64
	ModTime       time.Time
	ChangeTime    time.Time
}

// LegacyMemoFileSourceV1 is an owned copy of one raw file.  Source is
// intentionally kept separate from the canonical parser result, and callers
// must not pass it to the game runtime.
type LegacyMemoFileSourceV1 struct {
	Metadata LegacyMemoFileMetadataV1
	Source   []byte
	SHA256   [sha256.Size]byte
	Digest   [sha256.Size]byte
}

func (s LegacyMemoFileSourceV1) Clone() LegacyMemoFileSourceV1 {
	s.Source = append([]byte(nil), s.Source...)
	return s
}

// Descriptive aliases make the migration boundary usable by callers that
// refer to the source as a located file or a character-memo source.
type LegacyMemoFileLocatedV1 = LegacyMemoFileSourceV1
type LegacyCharacterMemoFileSourceV1 = LegacyMemoFileSourceV1

// LegacyMemoFileLocatorV1 holds an explicit, caller-supplied absolute root.
// Every Locate call reopens and revalidates the tree; the locator does not
// resolve environment variables or retain a live file descriptor.
type LegacyMemoFileLocatorV1 struct {
	root string
}

func NewLegacyMemoFileLocatorV1(root string) (LegacyMemoFileLocatorV1, error) {
	clean, err := validateLegacyMemoFileLocatorRoot(root)
	if err != nil {
		return LegacyMemoFileLocatorV1{}, err
	}
	return LegacyMemoFileLocatorV1{root: clean}, nil
}

func (l LegacyMemoFileLocatorV1) Root() string { return l.root }

func (l LegacyMemoFileLocatorV1) Locate(recipientName string) (LegacyMemoFileSourceV1, error) {
	return locateLegacyMemoFileV1(l.root, recipientName)
}

// LocateLegacyMemoFileV1 opens root/player/fal/recipientName with a
// descriptor-anchored no-follow walk and returns an owned byte copy plus
// restricted metadata.  It does not parse the C text format.
func LocateLegacyMemoFileV1(root, recipientName string) (LegacyMemoFileSourceV1, error) {
	return locateLegacyMemoFileV1(root, recipientName)
}

// ReadLegacyMemoFileV1 is an explicit read-side spelling of the same
// migration-only operation.
func ReadLegacyMemoFileV1(root, recipientName string) (LegacyMemoFileSourceV1, error) {
	return LocateLegacyMemoFileV1(root, recipientName)
}

// ValidateLegacyMemoFileLocatorNameV1 requires the exact canonical name that
// the caller intends to map.  It does not lowercase or otherwise repair a
// name, and therefore cannot silently redirect a file lookup.
func ValidateLegacyMemoFileLocatorNameV1(name string) error {
	if name == "" || !utf8.ValidString(name) {
		return ErrLegacyMemoFileLocatorInvalidName
	}
	if len(name) > legacyMemoFileLocatorMaxNameBytes {
		return ErrLegacyMemoFileLocatorInvalidName
	}
	codepoints := utf8.RuneCountInString(name)
	if codepoints < legacyMemoFileLocatorMinNameCodepoints || codepoints > legacyMemoFileLocatorMaxNameCodepoints {
		return ErrLegacyMemoFileLocatorInvalidName
	}
	if name == "." || name == ".." {
		return ErrLegacyMemoFileLocatorPathTraversal
	}
	for _, b := range []byte(name) {
		if b < 32 || b == 127 {
			return ErrLegacyMemoFileLocatorInvalidName
		}
		if b == '/' || b == '\\' || b == ':' {
			return ErrLegacyMemoFileLocatorPathTraversal
		}
	}
	canonical, err := identity.CanonicalName(name)
	if err != nil {
		return ErrLegacyMemoFileLocatorInvalidName
	}
	if canonical != name {
		return ErrLegacyMemoFileLocatorNonCanonicalName
	}
	return nil
}

// ValidateLegacyMemoFileLocatorPlayerNameV1 is a compatibility spelling for
// callers that use player terminology for the recipient path component.
func ValidateLegacyMemoFileLocatorPlayerNameV1(name string) error {
	return ValidateLegacyMemoFileLocatorNameV1(name)
}

func validateLegacyMemoFileLocatorRoot(root string) (string, error) {
	if root == "" || strings.IndexByte(root, 0) >= 0 || !filepath.IsAbs(root) {
		return "", ErrLegacyMemoFileLocatorInvalidRoot
	}
	for _, component := range strings.Split(root, string(filepath.Separator)) {
		if component == ".." {
			return "", fmt.Errorf("%w: root contains ..", ErrLegacyMemoFileLocatorPathTraversal)
		}
	}
	clean := filepath.Clean(root)
	// A process-wide filesystem root is too broad to be an explicit source
	// root.  A caller can still name a dedicated temporary or mounted root.
	if clean == "." || clean == string(filepath.Separator) {
		return "", ErrLegacyMemoFileLocatorInvalidRoot
	}
	return clean, nil
}

func locateLegacyMemoFileV1(root, recipientName string) (result LegacyMemoFileSourceV1, err error) {
	root, err = validateLegacyMemoFileLocatorRoot(root)
	if err != nil {
		return LegacyMemoFileSourceV1{}, err
	}
	if err := ValidateLegacyMemoFileLocatorNameV1(recipientName); err != nil {
		return LegacyMemoFileSourceV1{}, err
	}

	euid := uint64(os.Geteuid())
	file, before, err := openLegacyMemoTree(root, recipientName, euid)
	if err != nil {
		return LegacyMemoFileSourceV1{}, err
	}
	closed := false
	defer func() {
		if !closed {
			if closeErr := file.Close(); err == nil && closeErr != nil {
				result = LegacyMemoFileSourceV1{}
				err = fmt.Errorf("%w: close source: %v", ErrLegacyMemoFileLocatorRead, closeErr)
			}
		}
	}()

	if before.size < 0 {
		return LegacyMemoFileSourceV1{}, ErrLegacyMemoFileLocatorUnsafe
	}
	if before.size > int64(LegacyMemoFileLocatorV1MaxBytes) {
		return LegacyMemoFileSourceV1{}, ErrLegacyMemoFileLocatorSizeLimit
	}
	if _, seekErr := file.Seek(0, io.SeekStart); seekErr != nil {
		return LegacyMemoFileSourceV1{}, fmt.Errorf("%w: seek: %v", ErrLegacyMemoFileLocatorRead, seekErr)
	}
	raw := make([]byte, int(before.size))
	if _, readErr := io.ReadFull(file, raw); readErr != nil {
		return LegacyMemoFileSourceV1{}, fmt.Errorf("%w: read: %v", ErrLegacyMemoFileLocatorRead, readErr)
	}
	afterRead, statErr := legacyMemoFstat(file)
	if statErr != nil {
		return LegacyMemoFileSourceV1{}, fmt.Errorf("%w: post-read stat: %v", ErrLegacyMemoFileLocatorRead, statErr)
	}
	if !legacyMemoFileSafe(afterRead, euid) || !legacyMemoSameFile(before, afterRead) {
		return LegacyMemoFileSourceV1{}, ErrLegacyMemoFileLocatorChanged
	}

	// A fresh descriptor walk catches replacement of root/player/fal or the
	// named leaf after the first walk, even if an attacker restores a similar
	// path before the read-side stat.
	fresh, freshStat, freshErr := openLegacyMemoTree(root, recipientName, euid)
	if freshErr != nil {
		if errors.Is(freshErr, ErrLegacyMemoFileLocatorNotFound) {
			return LegacyMemoFileSourceV1{}, ErrLegacyMemoFileLocatorChanged
		}
		return LegacyMemoFileSourceV1{}, freshErr
	}
	if closeErr := fresh.Close(); closeErr != nil {
		return LegacyMemoFileSourceV1{}, fmt.Errorf("%w: close rewalk: %v", ErrLegacyMemoFileLocatorRead, closeErr)
	}
	if !legacyMemoSameFile(before, freshStat) {
		return LegacyMemoFileSourceV1{}, ErrLegacyMemoFileLocatorChanged
	}

	if closeErr := file.Close(); closeErr != nil {
		closed = true
		return LegacyMemoFileSourceV1{}, fmt.Errorf("%w: close source: %v", ErrLegacyMemoFileLocatorRead, closeErr)
	}
	closed = true
	owned := make([]byte, len(raw))
	copy(owned, raw)
	digest := sha256.Sum256(owned)
	return LegacyMemoFileSourceV1{
		Metadata: legacyMemoFileMetadata(root, recipientName, before),
		Source:   owned,
		SHA256:   digest,
		Digest:   digest,
	}, nil
}

func openLegacyMemoTree(root, recipientName string, euid uint64) (*os.File, legacyMemoFileStat, error) {
	rootFile, err := legacyMemoOpenRoot(root)
	if err != nil {
		return nil, legacyMemoFileStat{}, classifyLegacyMemoLocatorOpenError(err)
	}
	closeRoot := true
	defer func() {
		if closeRoot {
			_ = rootFile.Close()
		}
	}()
	rootStat, err := legacyMemoFstat(rootFile)
	if err != nil {
		return nil, legacyMemoFileStat{}, fmt.Errorf("%w: root stat: %v", ErrLegacyMemoFileLocatorUnsafe, err)
	}
	if !legacyMemoDirectorySafe(rootStat, euid) {
		return nil, legacyMemoFileStat{}, ErrLegacyMemoFileLocatorUnsafe
	}

	playerDir, _, err := openLegacyMemoDirectory(rootFile, "player", euid)
	if err != nil {
		return nil, legacyMemoFileStat{}, err
	}
	falDir, _, err := openLegacyMemoDirectory(playerDir, "fal", euid)
	if err != nil {
		_ = playerDir.Close()
		return nil, legacyMemoFileStat{}, err
	}
	file, fileStat, err := openLegacyMemoFile(falDir, recipientName, euid)
	_ = falDir.Close()
	_ = playerDir.Close()
	if err != nil {
		return nil, legacyMemoFileStat{}, err
	}
	if closeErr := rootFile.Close(); closeErr != nil {
		_ = file.Close()
		return nil, legacyMemoFileStat{}, fmt.Errorf("%w: close root: %v", ErrLegacyMemoFileLocatorRead, closeErr)
	}
	closeRoot = false
	return file, fileStat, nil
}

func openLegacyMemoDirectory(parent *os.File, name string, euid uint64) (*os.File, legacyMemoFileStat, error) {
	before, err := legacyMemoFstatAt(parent, name)
	if err != nil {
		return nil, legacyMemoFileStat{}, classifyLegacyMemoLocatorOpenError(err)
	}
	if !legacyMemoDirectorySafe(before, euid) {
		return nil, legacyMemoFileStat{}, ErrLegacyMemoFileLocatorUnsafe
	}
	child, err := legacyMemoOpenAt(parent, name, legacyBankDirectoryOpenFlags)
	if err != nil {
		return nil, legacyMemoFileStat{}, classifyLegacyMemoLocatorOpenError(err)
	}
	opened, err := legacyMemoFstat(child)
	if err != nil {
		_ = child.Close()
		return nil, legacyMemoFileStat{}, fmt.Errorf("%w: directory stat: %v", ErrLegacyMemoFileLocatorUnsafe, err)
	}
	after, err := legacyMemoFstatAt(parent, name)
	if err != nil || !legacyMemoDirectorySafe(opened, euid) ||
		!legacyMemoDirectorySafe(after, euid) || !legacyMemoSameEntry(before, opened) || !legacyMemoSameEntry(opened, after) {
		_ = child.Close()
		if err != nil {
			return nil, legacyMemoFileStat{}, classifyLegacyMemoLocatorOpenError(err)
		}
		return nil, legacyMemoFileStat{}, ErrLegacyMemoFileLocatorChanged
	}
	return child, opened, nil
}

func openLegacyMemoFile(parent *os.File, name string, euid uint64) (*os.File, legacyMemoFileStat, error) {
	before, err := legacyMemoFstatAt(parent, name)
	if err != nil {
		return nil, legacyMemoFileStat{}, classifyLegacyMemoLocatorOpenError(err)
	}
	if !legacyMemoFileSafe(before, euid) {
		if before.size > int64(LegacyMemoFileLocatorV1MaxBytes) {
			return nil, legacyMemoFileStat{}, ErrLegacyMemoFileLocatorSizeLimit
		}
		return nil, legacyMemoFileStat{}, ErrLegacyMemoFileLocatorUnsafe
	}
	file, err := legacyMemoOpenAt(parent, name, legacyBankFileOpenFlags)
	if err != nil {
		return nil, legacyMemoFileStat{}, classifyLegacyMemoLocatorOpenError(err)
	}
	opened, err := legacyMemoFstat(file)
	if err != nil {
		_ = file.Close()
		return nil, legacyMemoFileStat{}, fmt.Errorf("%w: file stat: %v", ErrLegacyMemoFileLocatorUnsafe, err)
	}
	after, err := legacyMemoFstatAt(parent, name)
	if err != nil || !legacyMemoFileSafe(opened, euid) || !legacyMemoFileSafe(after, euid) ||
		!legacyMemoSameEntry(before, opened) || !legacyMemoSameEntry(opened, after) {
		_ = file.Close()
		if err != nil {
			return nil, legacyMemoFileStat{}, classifyLegacyMemoLocatorOpenError(err)
		}
		return nil, legacyMemoFileStat{}, ErrLegacyMemoFileLocatorChanged
	}
	if opened.size > int64(LegacyMemoFileLocatorV1MaxBytes) {
		_ = file.Close()
		return nil, legacyMemoFileStat{}, ErrLegacyMemoFileLocatorSizeLimit
	}
	return file, opened, nil
}

func classifyLegacyMemoLocatorOpenError(err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %w", ErrLegacyMemoFileLocatorNotFound, err)
	}
	if errors.Is(err, ErrLegacyBankFileLocatorUnsupportedOS) {
		return ErrLegacyMemoFileLocatorUnsupportedOS
	}
	if errors.Is(err, ErrLegacyMemoFileLocatorUnsupportedOS) {
		return err
	}
	return fmt.Errorf("%w: %v", ErrLegacyMemoFileLocatorUnsafe, err)
}

func legacyMemoDirectorySafe(stat legacyMemoFileStat, euid uint64) bool {
	return stat.isDir && stat.uid == euid && stat.mode&0o7777 == legacyMemoFileLocatorDirectoryMode
}

func legacyMemoFileSafe(stat legacyMemoFileStat, euid uint64) bool {
	return stat.isRegular && stat.nlink == 1 && stat.uid == euid && stat.mode&0o7777 == legacyMemoFileLocatorRegularMode &&
		stat.size >= 0 && stat.size <= int64(LegacyMemoFileLocatorV1MaxBytes)
}

func legacyMemoSameEntry(left, right legacyMemoFileStat) bool {
	return left.dev == right.dev && left.ino == right.ino && left.mode == right.mode && left.uid == right.uid &&
		left.gid == right.gid && left.nlink == right.nlink
}

func legacyMemoSameFile(left, right legacyMemoFileStat) bool {
	return legacyMemoSameEntry(left, right) && left.size == right.size && left.mtimeSec == right.mtimeSec &&
		left.mtimeNsec == right.mtimeNsec && left.ctimeSec == right.ctimeSec && left.ctimeNsec == right.ctimeNsec
}

func legacyMemoFileMetadata(root, recipientName string, stat legacyMemoFileStat) LegacyMemoFileMetadataV1 {
	relative := filepath.Join("player", "fal", recipientName)
	return LegacyMemoFileMetadataV1{
		Root:          root,
		Path:          filepath.Join(root, relative),
		RelativePath:  relative,
		RecipientName: recipientName,
		Size:          stat.size,
		Mode:          fs.FileMode(stat.mode & 0o7777),
		ModeBits:      stat.mode & 0o7777,
		UID:           stat.uid,
		GID:           stat.gid,
		Nlink:         stat.nlink,
		Device:        stat.dev,
		Inode:         stat.ino,
		ModTime:       time.Unix(stat.mtimeSec, stat.mtimeNsec),
		ChangeTime:    time.Unix(stat.ctimeSec, stat.ctimeNsec),
	}
}

// The platform-specific functions below intentionally delegate to the bank
// locator's audited syscall shim.  Keeping a second copy of openat/fstatat
// numbers in the memo package would create an ABI drift hazard.
func legacyMemoOpenRoot(path string) (*os.File, error) { return legacyBankOpenRoot(path) }

func legacyMemoOpenAt(parent *os.File, name string, flags int) (*os.File, error) {
	return legacyBankOpenAt(parent, name, flags)
}

func legacyMemoFstat(file *os.File) (legacyMemoFileStat, error) {
	stat, err := legacyBankFstat(file)
	if err != nil {
		return legacyMemoFileStat{}, err
	}
	return legacyMemoFromBankStat(stat), nil
}

func legacyMemoFstatAt(parent *os.File, name string) (legacyMemoFileStat, error) {
	stat, err := legacyBankFstatAt(parent, name)
	if err != nil {
		return legacyMemoFileStat{}, err
	}
	return legacyMemoFromBankStat(stat), nil
}

func legacyMemoFromBankStat(stat legacyBankFileStat) legacyMemoFileStat {
	return legacyMemoFileStat{
		dev: stat.dev, ino: stat.ino, mode: stat.mode, uid: stat.uid, gid: stat.gid,
		nlink: stat.nlink, size: stat.size, mtimeSec: stat.mtimeSec, mtimeNsec: stat.mtimeNsec,
		ctimeSec: stat.ctimeSec, ctimeNsec: stat.ctimeNsec, isDir: stat.isDir, isRegular: stat.isRegular,
	}
}
