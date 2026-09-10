package world

// This file is the migration-only filesystem boundary for the legacy C
// family ledger.  It locates and copies family/family_list and the numbered
// family_member_<n> files, then parses them only when an operator supplies an
// explicit, reviewed character-ID map.  The Go game runtime never discovers
// identities from these files and never receives a raw path or credential.

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/1XP-Inc/muhan-mud/server/internal/identity"
)

const (
	LegacyFamilyFileLocatorV1ParserVersion = "go-family-file-locator-v1"
	// The C arrays are 256 rows wide, with a 15-byte name slot.  This limit is
	// intentionally larger than the encoded rows but small enough that a
	// damaged source cannot become an unbounded migration allocation.
	LegacyFamilyFileLocatorV1MaxBytes    int64  = 64 << 10
	legacyFamilyFileLocatorDirectoryMode uint32 = 0o700
	legacyFamilyFileLocatorRegularMode   uint32 = 0o600
	legacyFamilyFileLocatorMaxLines             = 257 // 256 rows plus sentinel
	legacyFamilyFileLocatorMaxTokenBytes        = 256
)

var (
	ErrLegacyFamilyFileLocatorInvalidRoot   = errors.New("invalid legacy family locator root")
	ErrLegacyFamilyFileLocatorPathTraversal = errors.New("legacy family locator path traversal rejected")
	ErrLegacyFamilyFileLocatorNotFound      = errors.New("legacy family file not found")
	ErrLegacyFamilyFileLocatorUnsafe        = errors.New("unsafe legacy family file or directory")
	ErrLegacyFamilyFileLocatorSizeLimit     = errors.New("legacy family file exceeds size limit")
	ErrLegacyFamilyFileLocatorChanged       = errors.New("legacy family file changed during inspection")
	ErrLegacyFamilyFileLocatorRead          = errors.New("legacy family file read failed")
	ErrLegacyFamilyFileLocatorUnsupportedOS = errors.New("legacy family locator unsupported on this operating system")
	ErrLegacyFamilySourceMalformed          = errors.New("malformed legacy family source")
	ErrLegacyFamilySourceIncomplete         = errors.New("incomplete legacy family source")
	ErrLegacyFamilyIdentityMapInvalid       = errors.New("invalid legacy family identity map")
)

// LegacyFamilyFileMetadataV1 describes only the descriptor-bound source
// entry.  It is evidence for an operator, not an account selector.
type LegacyFamilyFileMetadataV1 struct {
	Root         string
	Path         string
	RelativePath string
	Size         int64
	ModeBits     uint32
	UID          uint64
	GID          uint64
	Nlink        uint64
	Device       uint64
	Inode        uint64
}

// LegacyFamilyFileSourceV1 is an owned copy of one raw family file.  Source
// is intentionally kept outside the canonical FamilyState until parsing and
// explicit ID mapping have succeeded.
type LegacyFamilyFileSourceV1 struct {
	Metadata LegacyFamilyFileMetadataV1
	Source   []byte
	SHA256   [sha256.Size]byte
}

func (s LegacyFamilyFileSourceV1) Clone() LegacyFamilyFileSourceV1 {
	s.Source = append([]byte(nil), s.Source...)
	return s
}

// LegacyFamilyRawFilesV1 contains the two source domains needed to reproduce
// load_family/edit_member.  MemberFiles has an entry for every family row,
// including an empty ledger whose file contains only the sentinel.
type LegacyFamilyRawFilesV1 struct {
	FamilyList  LegacyFamilyFileSourceV1
	MemberFiles map[int16]LegacyFamilyFileSourceV1
}

func (s LegacyFamilyRawFilesV1) Clone() LegacyFamilyRawFilesV1 {
	next := LegacyFamilyRawFilesV1{FamilyList: s.FamilyList.Clone(), MemberFiles: make(map[int16]LegacyFamilyFileSourceV1, len(s.MemberFiles))}
	for id, source := range s.MemberFiles {
		next.MemberFiles[id] = source.Clone()
	}
	return next
}

// LegacyFamilyIdentityMapV1 is the reviewed bridge from legacy display names
// to immutable server-owned character IDs.  The map is supplied by the
// operator; this package never looks up an account or claims one by name.
type LegacyFamilyIdentityMapV1 struct {
	Families map[int16]LegacyFamilyIdentityFamilyV1 `json:"families"`
}

type LegacyFamilyIdentityFamilyV1 struct {
	BossID  string            `json:"boss_id"`
	Members map[string]string `json:"members"` // canonical legacy name -> character ID
}

// LegacyFamilyInspectionV1 is the manifest-ready, pointer-free aggregate
// produced after both source parsing and explicit identity review.
type LegacyFamilyInspectionV1 struct {
	Catalog FamilyCatalog
	Family  FamilyState
	BossIDs map[int16]string
	SHA256  [sha256.Size]byte
}

// LegacyFamilyFileLocatorV1 stores an explicit absolute root.  Each call
// reopens and revalidates the descriptor tree, so a caller cannot retain
// authority after the underlying directory has been replaced.
type LegacyFamilyFileLocatorV1 struct{ root string }

func NewLegacyFamilyFileLocatorV1(root string) (LegacyFamilyFileLocatorV1, error) {
	clean, err := validateLegacyFamilyFileLocatorRoot(root)
	if err != nil {
		return LegacyFamilyFileLocatorV1{}, err
	}
	return LegacyFamilyFileLocatorV1{root: clean}, nil
}

func (l LegacyFamilyFileLocatorV1) Root() string { return l.root }

func (l LegacyFamilyFileLocatorV1) Locate() (LegacyFamilyRawFilesV1, error) {
	return LocateLegacyFamilyRawFilesV1(l.root)
}

func ValidateLegacyFamilyFileLocatorRootV1(root string) error {
	_, err := validateLegacyFamilyFileLocatorRoot(root)
	return err
}

func validateLegacyFamilyFileLocatorRoot(root string) (string, error) {
	if root == "" || strings.IndexByte(root, 0) >= 0 || !filepath.IsAbs(root) {
		return "", ErrLegacyFamilyFileLocatorInvalidRoot
	}
	for _, component := range strings.Split(root, string(filepath.Separator)) {
		if component == ".." {
			return "", ErrLegacyFamilyFileLocatorPathTraversal
		}
	}
	clean := filepath.Clean(root)
	if clean == "." || (clean == string(filepath.Separator) && root != string(filepath.Separator)) {
		return "", ErrLegacyFamilyFileLocatorInvalidRoot
	}
	return clean, nil
}

// LocateLegacyFamilyRawFilesV1 copies family_list and every member file named
// by its parsed family IDs.  The list is parsed before member paths are built,
// preventing a caller-controlled filename from entering the descriptor walk.
func LocateLegacyFamilyRawFilesV1(root string) (LegacyFamilyRawFilesV1, error) {
	root, err := validateLegacyFamilyFileLocatorRoot(root)
	if err != nil {
		return LegacyFamilyRawFilesV1{}, err
	}
	familyList, err := locateLegacyFamilyFileV1(root, "family_list")
	if err != nil {
		return LegacyFamilyRawFilesV1{}, err
	}
	ids, err := parseLegacyFamilyListIDs(familyList.Source)
	if err != nil {
		return LegacyFamilyRawFilesV1{}, err
	}
	if err := validateLegacyFamilyDirectoryEntriesV1(root, ids); err != nil {
		return LegacyFamilyRawFilesV1{}, err
	}
	memberFiles := make(map[int16]LegacyFamilyFileSourceV1, len(ids))
	for _, id := range ids {
		name := "family_member_" + strconv.Itoa(int(id))
		source, locateErr := locateLegacyFamilyFileV1(root, name)
		if locateErr != nil {
			return LegacyFamilyRawFilesV1{}, fmt.Errorf("family %d: %w", id, locateErr)
		}
		memberFiles[id] = source
	}
	result := LegacyFamilyRawFilesV1{FamilyList: familyList, MemberFiles: memberFiles}
	return result, nil
}

func validateLegacyFamilyDirectoryEntriesV1(root string, ids []int16) error {
	dir, err := openLegacyFamilyDirectoryV1(root, uint64(os.Geteuid()))
	if err != nil {
		return err
	}
	defer dir.Close()
	allowed := map[string]struct{}{"family_list": {}}
	for _, id := range ids {
		allowed["family_member_"+strconv.Itoa(int(id))] = struct{}{}
	}
	names, err := dir.Readdirnames(-1)
	if err != nil {
		return fmt.Errorf("%w: family directory enumeration: %v", ErrLegacyFamilyFileLocatorRead, err)
	}
	for _, name := range names {
		if _, ok := allowed[name]; !ok {
			return fmt.Errorf("%w: unexpected family entry %q", ErrLegacyFamilyFileLocatorUnsafe, name)
		}
		stat, statErr := legacyBankFstatAt(dir, name)
		if statErr != nil || !legacyFamilyFileSafeV1(stat, uint64(os.Geteuid())) {
			return fmt.Errorf("%w: unsafe family entry %q", ErrLegacyFamilyFileLocatorUnsafe, name)
		}
	}
	return nil
}

func locateLegacyFamilyFileV1(root, name string) (result LegacyFamilyFileSourceV1, err error) {
	euid := uint64(os.Geteuid())
	if err := validateLegacyFamilyEntryNameV1(name); err != nil {
		return LegacyFamilyFileSourceV1{}, err
	}
	familyDir, err := openLegacyFamilyDirectoryV1(root, euid)
	if err != nil {
		return LegacyFamilyFileSourceV1{}, err
	}
	file, before, err := openLegacyFamilyEntryV1(familyDir, name, euid)
	_ = familyDir.Close()
	if err != nil {
		return LegacyFamilyFileSourceV1{}, err
	}
	closed := false
	defer func() {
		if !closed {
			if closeErr := file.Close(); err == nil && closeErr != nil {
				err = fmt.Errorf("%w: close: %v", ErrLegacyFamilyFileLocatorRead, closeErr)
				result = LegacyFamilyFileSourceV1{}
			}
		}
	}()
	if before.size < 0 || before.size > LegacyFamilyFileLocatorV1MaxBytes {
		if before.size > LegacyFamilyFileLocatorV1MaxBytes {
			return LegacyFamilyFileSourceV1{}, ErrLegacyFamilyFileLocatorSizeLimit
		}
		return LegacyFamilyFileSourceV1{}, ErrLegacyFamilyFileLocatorUnsafe
	}
	raw := make([]byte, int(before.size))
	if _, readErr := io.ReadFull(file, raw); readErr != nil {
		return LegacyFamilyFileSourceV1{}, fmt.Errorf("%w: read: %v", ErrLegacyFamilyFileLocatorRead, readErr)
	}
	after, statErr := legacyBankFstat(file)
	if statErr != nil {
		return LegacyFamilyFileSourceV1{}, fmt.Errorf("%w: post-read stat: %v", ErrLegacyFamilyFileLocatorRead, statErr)
	}
	if !legacyFamilySameFileV1(before, after) {
		return LegacyFamilyFileSourceV1{}, ErrLegacyFamilyFileLocatorChanged
	}
	// A second descriptor walk catches replacement of root/family/file after
	// the first open, while the source descriptor remains pinned to its inode.
	fresh, freshErr := locateLegacyFamilyMetadataV1(root, name, euid)
	if freshErr != nil {
		if errors.Is(freshErr, ErrLegacyFamilyFileLocatorNotFound) {
			return LegacyFamilyFileSourceV1{}, ErrLegacyFamilyFileLocatorChanged
		}
		return LegacyFamilyFileSourceV1{}, freshErr
	}
	if !legacyFamilySameFileV1(before, fresh) {
		return LegacyFamilyFileSourceV1{}, ErrLegacyFamilyFileLocatorChanged
	}
	if closeErr := file.Close(); closeErr != nil {
		closed = true
		return LegacyFamilyFileSourceV1{}, fmt.Errorf("%w: close: %v", ErrLegacyFamilyFileLocatorRead, closeErr)
	}
	closed = true
	owned := append([]byte(nil), raw...)
	return LegacyFamilyFileSourceV1{
		Metadata: legacyFamilyMetadataV1(root, name, before),
		Source:   owned,
		SHA256:   sha256.Sum256(owned),
	}, nil
}

// validateLegacyFamilyEntryNameV1 keeps the internal descriptor helper a
// basename-only operation.  LocateLegacyFamilyRawFilesV1 currently supplies
// names generated from the parsed numeric IDs, but retaining this check at the
// filesystem boundary prevents a future caller from passing a slash-bearing
// family_member_../... string to fstatat/openat.
func validateLegacyFamilyEntryNameV1(name string) error {
	if name == "family_list" {
		return nil
	}
	const prefix = "family_member_"
	if !strings.HasPrefix(name, prefix) {
		return ErrLegacyFamilyFileLocatorPathTraversal
	}
	suffix := strings.TrimPrefix(name, prefix)
	if suffix == "" || strings.ContainsAny(suffix, "/\\\x00") {
		return ErrLegacyFamilyFileLocatorPathTraversal
	}
	id, err := strconv.Atoi(suffix)
	if err != nil || strconv.Itoa(id) != suffix || id < 1 || id > int(FamilyMaxID) {
		return ErrLegacyFamilyFileLocatorUnsafe
	}
	return nil
}

func openLegacyFamilyDirectoryV1(root string, euid uint64) (*os.File, error) {
	rootFile, err := legacyBankOpenRoot(root)
	if err != nil {
		return nil, classifyLegacyFamilyOpenErrorV1(err)
	}
	rootStat, statErr := legacyBankFstat(rootFile)
	if statErr != nil || !legacyFamilyDirectorySafeV1(rootStat, euid) {
		_ = rootFile.Close()
		if statErr != nil {
			return nil, fmt.Errorf("%w: root stat: %v", ErrLegacyFamilyFileLocatorUnsafe, statErr)
		}
		return nil, ErrLegacyFamilyFileLocatorUnsafe
	}
	before, statErr := legacyBankFstatAt(rootFile, "family")
	if statErr != nil || !legacyFamilyDirectorySafeV1(before, euid) {
		_ = rootFile.Close()
		if statErr != nil {
			return nil, classifyLegacyFamilyOpenErrorV1(statErr)
		}
		return nil, ErrLegacyFamilyFileLocatorUnsafe
	}
	familyDir, openErr := legacyBankOpenAt(rootFile, "family", legacyBankDirectoryOpenFlags)
	if openErr != nil {
		_ = rootFile.Close()
		return nil, classifyLegacyFamilyOpenErrorV1(openErr)
	}
	opened, statErr := legacyBankFstat(familyDir)
	after, afterErr := legacyBankFstatAt(rootFile, "family")
	_ = rootFile.Close()
	if statErr != nil || afterErr != nil || !legacyFamilyDirectorySafeV1(opened, euid) || !legacyFamilyDirectorySafeV1(after, euid) || !legacyBankSameEntry(before, opened) || !legacyBankSameEntry(opened, after) {
		_ = familyDir.Close()
		if statErr != nil || afterErr != nil {
			return nil, fmt.Errorf("%w: family stat: %v", ErrLegacyFamilyFileLocatorUnsafe, statErr)
		}
		return nil, ErrLegacyFamilyFileLocatorChanged
	}
	// The opened descriptor is already no-follow and inode-bound.  The file
	// entry is checked again by openLegacyFamilyEntryV1.
	return familyDir, nil
}

func openLegacyFamilyEntryV1(parent *os.File, name string, euid uint64) (*os.File, legacyBankFileStat, error) {
	before, err := legacyBankFstatAt(parent, name)
	if err != nil {
		return nil, legacyBankFileStat{}, classifyLegacyFamilyOpenErrorV1(err)
	}
	if !legacyFamilyFileSafeV1(before, euid) {
		if before.size > LegacyFamilyFileLocatorV1MaxBytes {
			return nil, legacyBankFileStat{}, ErrLegacyFamilyFileLocatorSizeLimit
		}
		return nil, legacyBankFileStat{}, ErrLegacyFamilyFileLocatorUnsafe
	}
	file, err := legacyBankOpenAt(parent, name, legacyBankFileOpenFlags)
	if err != nil {
		return nil, legacyBankFileStat{}, classifyLegacyFamilyOpenErrorV1(err)
	}
	opened, statErr := legacyBankFstat(file)
	after, afterErr := legacyBankFstatAt(parent, name)
	if statErr != nil || afterErr != nil || !legacyFamilyFileSafeV1(opened, euid) || !legacyFamilyFileSafeV1(after, euid) || !legacyBankSameEntry(before, opened) || !legacyBankSameEntry(opened, after) {
		_ = file.Close()
		if statErr != nil || afterErr != nil {
			return nil, legacyBankFileStat{}, ErrLegacyFamilyFileLocatorChanged
		}
		return nil, legacyBankFileStat{}, ErrLegacyFamilyFileLocatorChanged
	}
	return file, opened, nil
}

func locateLegacyFamilyMetadataV1(root, name string, euid uint64) (legacyBankFileStat, error) {
	dir, err := openLegacyFamilyDirectoryV1(root, euid)
	if err != nil {
		return legacyBankFileStat{}, err
	}
	defer dir.Close()
	file, stat, err := openLegacyFamilyEntryV1(dir, name, euid)
	if file != nil {
		_ = file.Close()
	}
	return stat, err
}

func classifyLegacyFamilyOpenErrorV1(err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %w", ErrLegacyFamilyFileLocatorNotFound, err)
	}
	if errors.Is(err, ErrLegacyBankFileLocatorUnsupportedOS) {
		return ErrLegacyFamilyFileLocatorUnsupportedOS
	}
	if errors.Is(err, ErrLegacyFamilyFileLocatorUnsupportedOS) {
		return err
	}
	return fmt.Errorf("%w: %v", ErrLegacyFamilyFileLocatorUnsafe, err)
}

func legacyFamilyDirectorySafeV1(stat legacyBankFileStat, euid uint64) bool {
	return stat.isDir && stat.uid == euid && stat.mode&0o7777 == legacyFamilyFileLocatorDirectoryMode
}

func legacyFamilyFileSafeV1(stat legacyBankFileStat, euid uint64) bool {
	return stat.isRegular && stat.nlink == 1 && stat.uid == euid && stat.mode&0o7777 == legacyFamilyFileLocatorRegularMode && stat.size >= 0 && stat.size <= LegacyFamilyFileLocatorV1MaxBytes
}

func legacyFamilySameFileV1(left, right legacyBankFileStat) bool {
	return legacyBankSameEntry(left, right) && left.size == right.size && left.mtimeSec == right.mtimeSec && left.mtimeNsec == right.mtimeNsec && left.ctimeSec == right.ctimeSec && left.ctimeNsec == right.ctimeNsec
}

func legacyFamilyMetadataV1(root, name string, stat legacyBankFileStat) LegacyFamilyFileMetadataV1 {
	relative := filepath.Join("family", name)
	return LegacyFamilyFileMetadataV1{Root: root, Path: filepath.Join(root, relative), RelativePath: relative, Size: stat.size, ModeBits: stat.mode & 0o7777, UID: stat.uid, GID: stat.gid, Nlink: stat.nlink, Device: stat.dev, Inode: stat.ino}
}

func parseLegacyFamilyListIDs(raw []byte) ([]int16, error) {
	if len(raw) > int(LegacyFamilyFileLocatorV1MaxBytes) {
		return nil, ErrLegacyFamilyFileLocatorSizeLimit
	}
	seen := make(map[int16]struct{})
	ids := make([]int16, 0, FamilyMaxID)
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 256), int(LegacyFamilyFileLocatorV1MaxBytes)+1)
	seenSentinel := false
	lineCount := 0
	for scanner.Scan() {
		lineCount++
		if lineCount > legacyFamilyFileLocatorMaxLines {
			return nil, fmt.Errorf("%w: family_list has too many lines", ErrLegacyFamilySourceMalformed)
		}
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !utf8.ValidString(line) || strings.ContainsAny(line, "\x00\r") {
			return nil, fmt.Errorf("%w: invalid family_list encoding", ErrLegacyFamilySourceMalformed)
		}
		fields := strings.Fields(line)
		if len(fields) != 4 || seenSentinel {
			return nil, fmt.Errorf("%w: family_list row", ErrLegacyFamilySourceMalformed)
		}
		id, err := parseLegacyFamilyInt(fields[0])
		if err != nil {
			return nil, fmt.Errorf("%w: family id", ErrLegacyFamilySourceMalformed)
		}
		if id == 16 {
			if err := validateLegacyFamilyToken(fields[1]); err != nil {
				return nil, fmt.Errorf("%w: family sentinel", ErrLegacyFamilySourceMalformed)
			}
			if err := validateLegacyFamilyToken(fields[2]); err != nil {
				return nil, fmt.Errorf("%w: family sentinel", ErrLegacyFamilySourceMalformed)
			}
			if _, err := parseLegacyFamilyInt(fields[3]); err != nil {
				return nil, fmt.Errorf("%w: family sentinel fee", ErrLegacyFamilySourceMalformed)
			}
			seenSentinel = true
			continue
		}
		if id < 1 || id > int(FamilyMaxID) {
			return nil, fmt.Errorf("%w: family id %d", ErrLegacyFamilySourceMalformed, id)
		}
		familyID := int16(id)
		if _, exists := seen[familyID]; exists {
			return nil, fmt.Errorf("%w: duplicate family id %d", ErrLegacyFamilySourceMalformed, id)
		}
		if err := validateLegacyFamilyToken(fields[1]); err != nil {
			return nil, fmt.Errorf("%w: family name", ErrLegacyFamilySourceMalformed)
		}
		if err := validateLegacyFamilyToken(fields[2]); err != nil {
			return nil, fmt.Errorf("%w: family name", ErrLegacyFamilySourceMalformed)
		}
		if _, err := parseLegacyFamilyInt(fields[3]); err != nil {
			return nil, fmt.Errorf("%w: family fee", ErrLegacyFamilySourceMalformed)
		}
		seen[familyID] = struct{}{}
		ids = append(ids, familyID)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%w: family_list read", ErrLegacyFamilySourceMalformed)
	}
	if !seenSentinel {
		return nil, fmt.Errorf("%w: family_list sentinel missing", ErrLegacyFamilySourceIncomplete)
	}
	return ids, nil
}

func parseLegacyFamilyInt(value string) (int, error) {
	if value == "" || len(value) > 12 {
		return 0, errors.New("invalid integer")
	}
	return strconv.Atoi(value)
}

func validateLegacyFamilyToken(value string) error {
	if value == "" || len(value) > legacyFamilyFileLocatorMaxTokenBytes || !utf8.ValidString(value) || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\x00\r\n") {
		return errors.New("invalid token")
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f || r == '\u2028' || r == '\u2029' {
			return errors.New("invalid token")
		}
	}
	return nil
}

// InspectLegacyFamilyRawFilesV1 converts the located files into canonical
// family aggregates.  Every display name must be present in the reviewed map;
// no name-only row is admitted.
func InspectLegacyFamilyRawFilesV1(source LegacyFamilyRawFilesV1, identities LegacyFamilyIdentityMapV1) (LegacyFamilyInspectionV1, error) {
	if source.FamilyList.Source == nil || source.MemberFiles == nil {
		return LegacyFamilyInspectionV1{}, ErrLegacyFamilySourceIncomplete
	}
	catalog, listRows, err := parseLegacyFamilyList(source.FamilyList.Source)
	if err != nil {
		return LegacyFamilyInspectionV1{}, err
	}
	if err := validateLegacyFamilyIdentityMap(identities, listRows); err != nil {
		return LegacyFamilyInspectionV1{}, err
	}
	if len(source.MemberFiles) != len(listRows) {
		return LegacyFamilyInspectionV1{}, fmt.Errorf("%w: member file set differs from family_list", ErrLegacyFamilySourceIncomplete)
	}
	family := FamilyState{Members: make(map[int16][]FamilyMember, len(listRows))}
	bossIDs := make(map[int16]string, len(listRows))
	for id, row := range listRows {
		file, ok := source.MemberFiles[id]
		if !ok || file.Source == nil {
			return LegacyFamilyInspectionV1{}, fmt.Errorf("%w: member file %d", ErrLegacyFamilySourceIncomplete, id)
		}
		mapping := identities.Families[id]
		members, err := parseLegacyFamilyMemberFile(file.Source, row.Name, mapping.Members)
		if err != nil {
			return LegacyFamilyInspectionV1{}, fmt.Errorf("family %d: %w", id, err)
		}
		bossID := mapping.BossID
		bossMember, found := false, false
		for _, member := range members {
			if member.Name == row.Boss {
				bossMember = true
				if member.ID == bossID {
					found = true
				}
			}
		}
		if !bossMember || !found {
			return LegacyFamilyInspectionV1{}, fmt.Errorf("%w: family %d boss identity", ErrLegacyFamilyIdentityMapInvalid, id)
		}
		family.Members[id] = members
		bossIDs[id] = bossID
	}
	if err := family.Validate(); err != nil {
		return LegacyFamilyInspectionV1{}, fmt.Errorf("%w: family state: %v", ErrLegacyFamilySourceMalformed, err)
	}
	if err := catalog.Validate(); err != nil {
		return LegacyFamilyInspectionV1{}, fmt.Errorf("%w: family catalog: %v", ErrLegacyFamilySourceMalformed, err)
	}
	payload, err := marshalLegacyFamilyInspection(catalog, family, bossIDs)
	if err != nil {
		return LegacyFamilyInspectionV1{}, err
	}
	return LegacyFamilyInspectionV1{Catalog: catalog, Family: family, BossIDs: bossIDs, SHA256: sha256.Sum256(payload)}, nil
}

type legacyFamilyListRowV1 struct {
	ID   int16
	Name string
	Boss string
	Fee  int64
}

func parseLegacyFamilyList(raw []byte) (FamilyCatalog, map[int16]legacyFamilyListRowV1, error) {
	if len(raw) > int(LegacyFamilyFileLocatorV1MaxBytes) {
		return FamilyCatalog{}, nil, ErrLegacyFamilyFileLocatorSizeLimit
	}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 256), int(LegacyFamilyFileLocatorV1MaxBytes)+1)
	rows := make(map[int16]legacyFamilyListRowV1)
	seenSentinel := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !utf8.ValidString(line) || strings.ContainsAny(line, "\x00\r") || seenSentinel {
			return FamilyCatalog{}, nil, fmt.Errorf("%w: family_list row", ErrLegacyFamilySourceMalformed)
		}
		fields := strings.Fields(line)
		if len(fields) != 4 {
			return FamilyCatalog{}, nil, fmt.Errorf("%w: family_list columns", ErrLegacyFamilySourceMalformed)
		}
		id, intErr := parseLegacyFamilyInt(fields[0])
		fee, feeErr := parseLegacyFamilyInt(fields[3])
		if intErr != nil || feeErr != nil {
			return FamilyCatalog{}, nil, fmt.Errorf("%w: family_list integer", ErrLegacyFamilySourceMalformed)
		}
		if id == 16 {
			if err := validateLegacyFamilyToken(fields[1]); err != nil || validateLegacyFamilyToken(fields[2]) != nil {
				return FamilyCatalog{}, nil, fmt.Errorf("%w: family sentinel", ErrLegacyFamilySourceMalformed)
			}
			if _, err := parseLegacyFamilyInt(fields[3]); err != nil {
				return FamilyCatalog{}, nil, fmt.Errorf("%w: family sentinel fee", ErrLegacyFamilySourceMalformed)
			}
			seenSentinel = true
			continue
		}
		if id < 1 || id > int(FamilyMaxID) || fee < 0 || fee > int(^uint32(0)>>1) || validateLegacyFamilyToken(fields[1]) != nil || validateLegacyFamilyToken(fields[2]) != nil {
			return FamilyCatalog{}, nil, fmt.Errorf("%w: family_list values", ErrLegacyFamilySourceMalformed)
		}
		name, nameErr := identity.CanonicalName(fields[1])
		boss, bossErr := identity.CanonicalName(fields[2])
		if nameErr != nil || bossErr != nil || name != fields[1] || boss != fields[2] {
			return FamilyCatalog{}, nil, fmt.Errorf("%w: family_list names are not canonical", ErrLegacyFamilySourceMalformed)
		}
		key := int16(id)
		if _, exists := rows[key]; exists {
			return FamilyCatalog{}, nil, fmt.Errorf("%w: duplicate family id", ErrLegacyFamilySourceMalformed)
		}
		rows[key] = legacyFamilyListRowV1{ID: key, Name: name, Boss: boss, Fee: int64(fee)}
	}
	if err := scanner.Err(); err != nil {
		return FamilyCatalog{}, nil, fmt.Errorf("%w: family_list scan", ErrLegacyFamilySourceMalformed)
	}
	if !seenSentinel {
		return FamilyCatalog{}, nil, ErrLegacyFamilySourceIncomplete
	}
	families := make(map[int16]FamilyDefinition, len(rows))
	for id, row := range rows {
		families[id] = FamilyDefinition{ID: id, Name: row.Name, Boss: row.Boss, Fee: row.Fee}
	}
	return FamilyCatalog{Families: families}, rows, nil
}

func validateLegacyFamilyIdentityMap(input LegacyFamilyIdentityMapV1, rows map[int16]legacyFamilyListRowV1) error {
	if input.Families == nil || len(input.Families) != len(rows) {
		return ErrLegacyFamilyIdentityMapInvalid
	}
	seenIDs := make(map[string]struct{})
	for id, row := range rows {
		family, ok := input.Families[id]
		if !ok || family.Members == nil || family.BossID == "" {
			return fmt.Errorf("%w: family %d missing mapping", ErrLegacyFamilyIdentityMapInvalid, id)
		}
		if !validFamilyMemberID(family.BossID) {
			return fmt.Errorf("%w: family %d boss id", ErrLegacyFamilyIdentityMapInvalid, id)
		}
		bossID, ok := family.Members[row.Boss]
		if !ok || bossID != family.BossID {
			return fmt.Errorf("%w: family %d boss name not mapped", ErrLegacyFamilyIdentityMapInvalid, id)
		}
		for name, characterID := range family.Members {
			canonical, err := identity.CanonicalName(name)
			if err != nil || canonical != name || validateLegacyFamilyToken(name) != nil || !validFamilyMemberID(characterID) {
				return fmt.Errorf("%w: family %d member mapping", ErrLegacyFamilyIdentityMapInvalid, id)
			}
			if _, exists := seenIDs[characterID]; exists {
				return fmt.Errorf("%w: duplicate character id", ErrLegacyFamilyIdentityMapInvalid)
			}
			seenIDs[characterID] = struct{}{}
		}
	}
	for id := range input.Families {
		if _, ok := rows[id]; !ok {
			return fmt.Errorf("%w: unknown family mapping %d", ErrLegacyFamilyIdentityMapInvalid, id)
		}
	}
	return nil
}

func parseLegacyFamilyMemberFile(raw []byte, familyName string, mapping map[string]string) ([]FamilyMember, error) {
	if len(raw) > int(LegacyFamilyFileLocatorV1MaxBytes) {
		return nil, ErrLegacyFamilyFileLocatorSizeLimit
	}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 256), int(LegacyFamilyFileLocatorV1MaxBytes)+1)
	members := make([]FamilyMember, 0, 16)
	seenNames := make(map[string]struct{})
	seenSentinel := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !utf8.ValidString(line) || strings.ContainsAny(line, "\x00\r") || seenSentinel {
			return nil, fmt.Errorf("%w: member row", ErrLegacyFamilySourceMalformed)
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, fmt.Errorf("%w: member columns", ErrLegacyFamilySourceMalformed)
		}
		classValue, err := parseLegacyFamilyInt(fields[0])
		if err != nil || validateLegacyFamilyToken(fields[1]) != nil {
			return nil, fmt.Errorf("%w: member values", ErrLegacyFamilySourceMalformed)
		}
		if classValue == 0 {
			if fields[1] != familyName {
				return nil, fmt.Errorf("%w: member sentinel family mismatch", ErrLegacyFamilySourceMalformed)
			}
			seenSentinel = true
			continue
		}
		if classValue < 1 || classValue > 255 {
			return nil, fmt.Errorf("%w: member class", ErrLegacyFamilySourceMalformed)
		}
		characterID, ok := mapping[fields[1]]
		if !ok || !validFamilyMemberID(characterID) {
			return nil, fmt.Errorf("%w: unmapped member %q", ErrLegacyFamilyIdentityMapInvalid, fields[1])
		}
		if _, exists := seenNames[fields[1]]; exists {
			return nil, fmt.Errorf("%w: duplicate member name", ErrLegacyFamilySourceMalformed)
		}
		seenNames[fields[1]] = struct{}{}
		members = append(members, FamilyMember{ID: characterID, Name: fields[1], Class: byte(classValue)})
		if len(members) > 256 {
			return nil, fmt.Errorf("%w: too many members", ErrLegacyFamilySourceMalformed)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%w: member scan", ErrLegacyFamilySourceMalformed)
	}
	if !seenSentinel {
		return nil, ErrLegacyFamilySourceIncomplete
	}
	if len(seenNames) != len(mapping) {
		return nil, fmt.Errorf("%w: identity map contains a member absent from source", ErrLegacyFamilyIdentityMapInvalid)
	}
	for name := range mapping {
		if _, ok := seenNames[name]; !ok {
			return nil, fmt.Errorf("%w: identity map contains unknown member %q", ErrLegacyFamilyIdentityMapInvalid, name)
		}
	}
	return members, nil
}

func marshalLegacyFamilyInspection(catalog FamilyCatalog, family FamilyState, bossIDs map[int16]string) ([]byte, error) {
	// encoding/json gives deterministic map-key order for integer keys and is
	// used only for an evidence digest, not as an on-disk runtime format.
	return json.Marshal(struct {
		Catalog FamilyCatalog    `json:"catalog"`
		Family  FamilyState      `json:"family"`
		BossIDs map[int16]string `json:"boss_ids"`
	}{catalog, family, bossIDs})
}
