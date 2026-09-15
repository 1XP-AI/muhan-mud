package world

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	pathpkg "path"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/encoding/korean"
)

const (
	// These limits are the data limits of files3.c's fgets buffers.  The
	// newline is not part of a stored key/response, so a key may contain at
	// most 79 source bytes and a response at most 1023 source bytes.
	maxTalkKeyBytes      = 79
	maxTalkResponseBytes = 1023
	maxNPCTalkLevel      = 255
)

var (
	// ErrTalkCatalogEmpty means that the supplied filesystem contained no
	// addressable <name>-<level> talk file. Non-addressable historical files
	// are intentionally ignored because load_crt_tlk never opens them.
	ErrTalkCatalogEmpty = errors.New("NPC talk catalog is empty")
)

// TalkActionKind is the bounded action vocabulary parsed by talk_crt_act in
// files3.c. Action execution is deliberately outside this static catalog;
// callers must admit each side effect separately at the world reducer.
type TalkActionKind uint8

const (
	TalkActionNone TalkActionKind = iota
	TalkActionAttack
	TalkActionAction
	TalkActionCast
	TalkActionGive
)

func (k TalkActionKind) String() string {
	switch k {
	case TalkActionAttack:
		return "ATTACK"
	case TalkActionAction:
		return "ACTION"
	case TalkActionCast:
		return "CAST"
	case TalkActionGive:
		return "GIVE"
	default:
		return ""
	}
}

// TalkAction is the parsed side-effect descriptor on a topic. Name is the
// action argument (social action, spell name, or object number); Target is
// the optional fourth C token, normally PLAYER. A zero Kind is a plain
// response topic or an unrecognised C directive, matching talk_crt_act's
// type=0 fallback.
type TalkAction struct {
	Kind   TalkActionKind `json:"kind,omitempty"`
	Name   string         `json:"name,omitempty"`
	Target string         `json:"target,omitempty"`
}

// TalkTopic is one ordered key/response pair from a talk file. Duplicate
// keys remain ordered because C walks the linked list and returns the first
// matching entry.
type TalkTopic struct {
	Key      string     `json:"key"`
	Response string     `json:"response"`
	Action   TalkAction `json:"action,omitempty"`
}

// TalkFile is an owned, immutable-at-rest view of one source talk file.
// Topics are copied on every public lookup so callers cannot mutate a
// catalog that may be shared by world sessions.
type TalkFile struct {
	Path   string      `json:"path"`
	Topics []TalkTopic `json:"topics"`
}

// Topic returns the first exact key, preserving command8.c's strcmp/list
// behavior. Prefixes, case folding, and occurrence matching belong to the
// command boundary and are intentionally not inferred here.
func (f TalkFile) Topic(key string) (TalkTopic, bool) {
	for _, topic := range f.Topics {
		if topic.Key == key {
			return topic, true
		}
	}
	return TalkTopic{}, false
}

// TalkCatalog is an immutable static catalog indexed by the exact file path
// that load_crt_tlk constructs: <name-with-spaces-replaced-by-underscores>-<level>.
type TalkCatalog struct {
	files   map[string]TalkFile
	ignored []string
}

// NPCTalkCatalog is a descriptive alias for callers that keep NPC resources
// separate from other static talk content.
type NPCTalkCatalog = TalkCatalog

// LoadTalkCatalog reads an fs.FS rooted at objmon/talk. Only a regular,
// top-level file with the canonical decimal <name>-<level> shape is
// addressable by C and admitted. Orphan drafts and noncanonical path aliases
// are retained in IgnoredPaths for audit but never become gameplay topics.
//
// File bodies are decoded as UTF-8 when valid, otherwise as the legacy
// EUC-KR/CP949-compatible encoding used by resources_utf8. Replacement-rune
// output is rejected so a corrupted legacy byte cannot become player-visible
// text.
func LoadTalkCatalog(fsys fs.FS) (TalkCatalog, error) {
	if fsys == nil {
		return TalkCatalog{}, fmt.Errorf("missing NPC talk filesystem")
	}
	catalog := TalkCatalog{files: map[string]TalkFile{}}
	err := fs.WalkDir(fsys, ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&fs.ModeType != 0 {
			catalog.ignored = append(catalog.ignored, name)
			return nil
		}
		if !isCanonicalTalkPath(name) {
			catalog.ignored = append(catalog.ignored, name)
			return nil
		}
		raw, err := fs.ReadFile(fsys, name)
		if err != nil {
			return fmt.Errorf("read NPC talk file %q: %w", name, err)
		}
		file, err := decodeTalkFile(name, raw)
		if err != nil {
			return fmt.Errorf("decode NPC talk file %q: %w", name, err)
		}
		catalog.files[name] = file
		return nil
	})
	if err != nil {
		return TalkCatalog{}, err
	}
	sort.Strings(catalog.ignored)
	if len(catalog.files) == 0 {
		return TalkCatalog{}, ErrTalkCatalogEmpty
	}
	return catalog, nil
}

// LoadNPCTalkCatalog is the NPC-specific spelling of LoadTalkCatalog.
func LoadNPCTalkCatalog(fsys fs.FS) (TalkCatalog, error) {
	return LoadTalkCatalog(fsys)
}

// LoadResourceTalkCatalog adapts the checked-in resources_utf8 root to the
// talk-directory rooted loader. It does not silently fall back to objmon/
// when the canonical directory is missing.
func LoadResourceTalkCatalog(resourceRoot fs.FS) (TalkCatalog, error) {
	if resourceRoot == nil {
		return TalkCatalog{}, fmt.Errorf("missing resource filesystem")
	}
	talkRoot, err := fs.Sub(resourceRoot, "objmon/talk")
	if err != nil {
		return TalkCatalog{}, fmt.Errorf("open objmon/talk: %w", err)
	}
	return LoadTalkCatalog(talkRoot)
}

// LoadResourceNPCTalkCatalog is the resource-root spelling for NPC callers.
func LoadResourceNPCTalkCatalog(resourceRoot fs.FS) (TalkCatalog, error) {
	return LoadResourceTalkCatalog(resourceRoot)
}

// Len reports the number of addressable talk files.
func (c TalkCatalog) Len() int { return len(c.files) }

// IgnoredPaths returns sorted non-addressable paths. The returned slice is
// owned by the caller.
func (c TalkCatalog) IgnoredPaths() []string {
	return append([]string(nil), c.ignored...)
}

// File returns an exact canonical path lookup. It is useful for migration
// tooling that already has the C path and does not need to reconstruct it
// from an NPC name and level.
func (c TalkCatalog) File(path string) (TalkFile, bool) {
	file, ok := c.files[path]
	if !ok {
		return TalkFile{}, false
	}
	return cloneTalkFile(file), true
}

// Lookup resolves the same path as load_crt_tlk: spaces in the NPC display
// name become underscores, while all other characters remain exact.
func (c TalkCatalog) Lookup(name string, level int) (TalkFile, bool, error) {
	path, err := NPCTalkPath(name, level)
	if err != nil {
		return TalkFile{}, false, err
	}
	file, ok := c.files[path]
	if !ok {
		return TalkFile{}, false, nil
	}
	return cloneTalkFile(file), true, nil
}

// LookupTopic resolves one exact topic from the addressable file. A missing
// file and a missing key are both represented by ok=false; malformed names
// and levels remain errors so callers cannot turn path traversal into a
// fallback lookup.
func (c TalkCatalog) LookupTopic(name string, level int, key string) (TalkTopic, bool, error) {
	file, ok, err := c.Lookup(name, level)
	if err != nil || !ok {
		return TalkTopic{}, false, err
	}
	if key == "" || !validTalkText(key) {
		return TalkTopic{}, false, fmt.Errorf("invalid NPC talk key")
	}
	topic, ok := file.Topic(key)
	return topic, ok, nil
}

// NPCTalkPath constructs the path load_crt_tlk uses for a canonical NPC.
// Legacy creature levels are one unsigned byte, so wider values are rejected
// instead of producing a file name C could never open.
func NPCTalkPath(name string, level int) (string, error) {
	if level < 0 || level > maxNPCTalkLevel {
		return "", fmt.Errorf("NPC talk level %d is outside 0..%d", level, maxNPCTalkLevel)
	}
	if !validTalkName(name) {
		return "", fmt.Errorf("invalid NPC talk name")
	}
	name = strings.ReplaceAll(name, " ", "_")
	if name == "" || name == "." || name == ".." {
		return "", fmt.Errorf("invalid NPC talk name")
	}
	return fmt.Sprintf("%s-%d", name, level), nil
}

func cloneTalkFile(file TalkFile) TalkFile {
	file.Topics = append([]TalkTopic(nil), file.Topics...)
	return file
}

func validTalkName(name string) bool {
	if name == "" || !utf8.ValidString(name) || strings.TrimSpace(name) != name {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == '/' || r == '\\' {
			return false
		}
	}
	return true
}

func isCanonicalTalkPath(name string) bool {
	clean := pathpkg.Clean(name)
	if clean != name || clean == "." || pathpkg.Dir(name) != "." {
		return false
	}
	base := pathpkg.Base(name)
	dash := strings.LastIndexByte(base, '-')
	if dash <= 0 || dash == len(base)-1 || !validTalkName(base[:dash]) {
		return false
	}
	// load_crt_tlk replaces every literal space in the creature name with
	// an underscore before opening the file; a source filename containing a
	// space is therefore not addressable by the legacy path contract.
	if strings.Contains(base[:dash], " ") {
		return false
	}
	suffix := base[dash+1:]
	for _, r := range suffix {
		if r < '0' || r > '9' {
			return false
		}
	}
	level, err := strconv.Atoi(suffix)
	return err == nil && level >= 0 && level <= maxNPCTalkLevel && suffix == strconv.Itoa(level)
}

type talkLine struct {
	text         string
	rawSize      int
	syntheticEOF bool
}

func decodeTalkFile(path string, raw []byte) (TalkFile, error) {
	lines, err := decodeTalkLines(raw)
	if err != nil {
		return TalkFile{}, err
	}
	file := TalkFile{Path: path}
	for i := 0; i < len(lines); {
		keyLine := lines[i]
		if strings.TrimSpace(keyLine.text) == "" {
			// C's fgets loop stops after the final blank separator. An
			// internal blank key, however, would shift every subsequent
			// key/response pair and is rejected rather than guessed.
			if allTalkLinesBlank(lines[i:]) {
				break
			}
			return TalkFile{}, fmt.Errorf("blank key line %d", i+1)
		}
		if i+1 >= len(lines) || lines[i+1].syntheticEOF {
			return TalkFile{}, fmt.Errorf("missing response for key line %d", i+1)
		}
		responseLine := lines[i+1]
		if keyLine.rawSize > maxTalkKeyBytes {
			return TalkFile{}, fmt.Errorf("key line %d exceeds %d source bytes", i+1, maxTalkKeyBytes)
		}
		if responseLine.rawSize > maxTalkResponseBytes {
			return TalkFile{}, fmt.Errorf("response line %d exceeds %d source bytes", i+2, maxTalkResponseBytes)
		}
		key, action, err := parseTalkDirective(keyLine.text)
		if err != nil {
			return TalkFile{}, fmt.Errorf("key line %d: %w", i+1, err)
		}
		if !validTalkText(responseLine.text) {
			return TalkFile{}, fmt.Errorf("invalid response line %d", i+2)
		}
		file.Topics = append(file.Topics, TalkTopic{Key: key, Response: responseLine.text, Action: action})
		i += 2
	}
	if len(file.Topics) == 0 {
		return TalkFile{}, fmt.Errorf("no talk topics")
	}
	return file, nil
}

func decodeTalkLines(raw []byte) ([]talkLine, error) {
	parts := bytes.Split(raw, []byte{'\n'})
	lines := make([]talkLine, 0, len(parts))
	for i, part := range parts {
		if len(part) > 0 && part[len(part)-1] == '\r' {
			part = part[:len(part)-1]
		}
		if len(part) > maxTalkResponseBytes {
			return nil, fmt.Errorf("line %d exceeds %d source bytes", i+1, maxTalkResponseBytes)
		}
		text, err := decodeTalkText(part)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		lines = append(lines, talkLine{
			text:         text,
			rawSize:      len(part),
			syntheticEOF: i == len(parts)-1 && len(part) == 0 && len(raw) > 0 && raw[len(raw)-1] == '\n',
		})
	}
	return lines, nil
}

func decodeTalkText(raw []byte) (string, error) {
	var text string
	if utf8.Valid(raw) {
		text = string(raw)
	} else {
		decoded, err := korean.EUCKR.NewDecoder().Bytes(raw)
		if err != nil {
			return "", fmt.Errorf("invalid UTF-8/EUC-KR text: %w", err)
		}
		text = string(decoded)
	}
	if !utf8.ValidString(text) || strings.ContainsRune(text, '\ufffd') || !validTalkText(text) {
		return "", fmt.Errorf("invalid UTF-8/EUC-KR text")
	}
	return text, nil
}

func allTalkLinesBlank(lines []talkLine) bool {
	for _, line := range lines {
		if strings.TrimSpace(line.text) != "" {
			return false
		}
	}
	return true
}

func validTalkText(text string) bool {
	if !utf8.ValidString(text) {
		return false
	}
	for _, r := range text {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func parseTalkDirective(line string) (string, TalkAction, error) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "", TalkAction{}, fmt.Errorf("empty talk key")
	}
	key := cTalkToken(fields[0])
	if key == "" {
		return "", TalkAction{}, fmt.Errorf("empty talk key")
	}
	if !validTalkText(key) || len([]byte(key)) > maxTalkKeyBytes {
		return "", TalkAction{}, fmt.Errorf("invalid talk key")
	}
	action := TalkAction{}
	if len(fields) < 2 {
		return key, action, nil
	}
	switch fields[1] {
	case "ATTACK":
		action.Kind = TalkActionAttack
	case "ACTION", "CAST":
		if len(fields) < 3 {
			return key, action, nil
		}
		action.Name = fields[2]
		if len(fields) > 3 {
			action.Target = fields[3]
		}
		if fields[1] == "ACTION" {
			action.Kind = TalkActionAction
		} else {
			action.Kind = TalkActionCast
		}
	case "GIVE":
		if len(fields) < 3 {
			return key, action, nil
		}
		action.Kind = TalkActionGive
		action.Name = fields[2]
	}
	return key, action, nil
}

// cTalkToken mirrors the byte-level token prefix that talk_crt_act accepts:
// ASCII letters/digits and '-' are accepted, while non-ASCII bytes are part
// of a Korean token. ASCII punctuation terminates the key (for example
// "wizard's" is stored as "wizard").
func cTalkToken(token string) string {
	var out []rune
	for _, r := range token {
		if r == '-' || unicode.IsLetter(r) || unicode.IsDigit(r) || r >= utf8.RuneSelf {
			out = append(out, r)
			continue
		}
		break
	}
	return string(out)
}
