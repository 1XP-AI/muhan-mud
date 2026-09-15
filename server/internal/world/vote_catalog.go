package world

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strconv"
	"strings"
)

const (
	// VoteIssuePath is the repository-relative path used by command11.c after
	// POSTPATH is resolved.  The loader never searches aliases or a client
	// supplied path.
	VoteIssuePath = "post/ISSUE"

	// command11.c uses fgets(tmp, 256) for the header and fgets(tmp, 1024)
	// for the question/options.  These limits are on bytes before decoding.
	VoteIssueHeaderBytes = 255
	VoteIssueTextBytes   = 1023
)

var (
	ErrVoteIssueSourceUnavailable = errors.New("legacy ISSUE source is unavailable")
	ErrVoteIssueSourceInvalid     = errors.New("legacy ISSUE source is invalid")
	ErrVoteIssueMalformed         = errors.New("legacy ISSUE header or text is malformed")
	ErrVoteIssueTruncated         = errors.New("legacy ISSUE source is truncated")
	ErrVoteIssueTrailing          = errors.New("legacy ISSUE source has trailing records")
)

// Source-oriented aliases keep migration callers from inventing a second
// error vocabulary for the same explicit file boundary.
var (
	ErrLegacyVoteIssueUnavailable = ErrVoteIssueSourceUnavailable
	ErrLegacyVoteIssueInvalid     = ErrVoteIssueSourceInvalid
	ErrLegacyVoteIssueMalformed   = ErrVoteIssueMalformed
	ErrLegacyVoteIssueTruncated   = ErrVoteIssueTruncated
	ErrLegacyVoteIssueTrailing    = ErrVoteIssueTrailing
	ErrVoteCatalogSourceMissing   = ErrVoteIssueSourceUnavailable
)

// VoteCatalogSource is the reviewed, immutable source evidence for one
// legacy ISSUE file.  Bytes are retained for an offline audit only; gameplay
// receives Catalog and its digest, never this raw payload.
type VoteCatalogSource struct {
	Path    string
	Catalog VoteCatalog
	Bytes   []byte
	SHA256  [32]byte
}

// VoteIssueSource is a descriptive alias used by migration tools.
type VoteIssueSource = VoteCatalogSource

func cloneVoteCatalogSource(source VoteCatalogSource) VoteCatalogSource {
	source.Catalog = source.Catalog.CloneForSource()
	source.Bytes = append([]byte(nil), source.Bytes...)
	return source
}

// CloneForSource returns an owned catalog copy without changing the public
// Clone contract used by command/session code.  The catalog has already been
// validated by every parser in this file.
func (c VoteCatalog) CloneForSource() VoteCatalog {
	c.Issue.Options = append([]string(nil), c.Issue.Options...)
	return c
}

// splitVoteIssueLines mirrors fgets line boundaries while accepting the
// final record without a newline.  Exactly one terminal newline is framing,
// while a second blank record remains visible and is rejected as trailing
// data rather than silently ignored.
func splitVoteIssueLines(raw []byte) [][]byte {
	if len(raw) == 0 {
		return nil
	}
	parts := bytes.Split(raw, []byte{'\n'})
	if len(parts) > 0 && len(parts[len(parts)-1]) == 0 {
		parts = parts[:len(parts)-1]
	}
	for index, part := range parts {
		if len(part) > 0 && part[len(part)-1] == '\r' {
			parts[index] = part[:len(part)-1]
		}
	}
	return parts
}

func decodeVoteIssueLine(raw []byte, maxBytes int, lineNumber int) (string, error) {
	if len(raw) > maxBytes {
		return "", fmt.Errorf("%w: line %d exceeds %d bytes", ErrVoteIssueMalformed, lineNumber, maxBytes)
	}
	text, err := decodeTalkText(raw)
	if err != nil {
		return "", fmt.Errorf("%w: line %d: %v", ErrVoteIssueMalformed, lineNumber, err)
	}
	return text, nil
}

func parseVoteIssueHeader(raw []byte) (int, error) {
	header, err := decodeVoteIssueLine(raw, VoteIssueHeaderBytes, 1)
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(header)
	// sscanf("%d %s", ...) is the source shape.  The class token is currently
	// commented out in C, but retaining at most one token keeps the accepted
	// source bounded and deterministic.
	if len(fields) < 1 || len(fields) > 2 {
		return 0, fmt.Errorf("%w: expected number and optional class", ErrVoteIssueMalformed)
	}
	number, err := strconv.Atoi(fields[0])
	if err != nil || number < 1 || number > VoteMaxSelections {
		return 0, fmt.Errorf("%w: invalid option count", ErrVoteIssueMalformed)
	}
	if len(fields) == 2 {
		if !validVoteText(fields[1], false) || len([]byte(fields[1])) > VoteIssueHeaderBytes {
			return 0, fmt.Errorf("%w: invalid class token", ErrVoteIssueMalformed)
		}
	}
	return number, nil
}

// ParseVoteCatalog parses the exact line layout consumed by command11.c:
// `number [class]`, one question line, then number option lines.  It rejects
// truncation, hidden records, bad encodings, and oversized lines before a
// VoteCatalog can reach a reducer.
func ParseVoteCatalog(raw []byte) (VoteCatalog, error) {
	if len(raw) == 0 {
		return VoteCatalog{}, ErrVoteCatalogUnavailable
	}
	lines := splitVoteIssueLines(raw)
	if len(lines) == 0 {
		return VoteCatalog{}, ErrVoteIssueTruncated
	}
	number, err := parseVoteIssueHeader(lines[0])
	if err != nil {
		return VoteCatalog{}, err
	}
	expected := number + 2
	if len(lines) < expected {
		return VoteCatalog{}, fmt.Errorf("%w: got %d lines, expected %d", ErrVoteIssueTruncated, len(lines), expected)
	}
	if len(lines) > expected {
		return VoteCatalog{}, fmt.Errorf("%w: got %d lines, expected %d", ErrVoteIssueTrailing, len(lines), expected)
	}
	prompt, err := decodeVoteIssueLine(lines[1], VoteIssueTextBytes, 2)
	if err != nil {
		return VoteCatalog{}, err
	}
	options := make([]string, number)
	for index := range options {
		lineNumber := index + 3
		options[index], err = decodeVoteIssueLine(lines[index+2], VoteIssueTextBytes, lineNumber)
		if err != nil {
			return VoteCatalog{}, err
		}
	}
	catalog := VoteCatalog{Issue: VoteIssue{Number: number, Prompt: prompt, Options: options}}
	if err := catalog.Validate(); err != nil {
		return VoteCatalog{}, fmt.Errorf("%w: %v", ErrVoteIssueMalformed, err)
	}
	return catalog, nil
}

// ParseLegacyVoteIssue is the source-oriented spelling of ParseVoteCatalog.
func ParseLegacyVoteIssue(raw []byte) (VoteCatalog, error) { return ParseVoteCatalog(raw) }

// InspectVoteCatalog parses one raw ISSUE file and returns immutable source
// evidence.  The SHA-256 binds migration review to the exact bytes while the
// catalog digest binds gameplay to the decoded question/options snapshot.
func InspectVoteCatalog(pathName string, raw []byte) (VoteCatalogSource, error) {
	catalog, err := ParseVoteCatalog(raw)
	if err != nil {
		return VoteCatalogSource{}, err
	}
	if pathName == "" {
		pathName = VoteIssuePath
	}
	return VoteCatalogSource{Path: pathName, Catalog: catalog.CloneForSource(), Bytes: append([]byte(nil), raw...), SHA256: sha256.Sum256(raw)}, nil
}

// ReadLegacyVoteCatalog reads only post/ISSUE from a repository-rooted FS.
// A missing file is an unresolved migration source, not an empty issue.
func ReadLegacyVoteCatalog(fsys fs.FS) (VoteCatalogSource, error) {
	if fsys == nil {
		return VoteCatalogSource{}, ErrVoteIssueSourceUnavailable
	}
	info, err := fs.Stat(fsys, VoteIssuePath)
	if err != nil {
		return VoteCatalogSource{}, fmt.Errorf("%w: %s: %v", ErrVoteIssueSourceUnavailable, VoteIssuePath, err)
	}
	if info.IsDir() || info.Mode()&fs.ModeType != 0 {
		return VoteCatalogSource{}, fmt.Errorf("%w: %s is not a regular file", ErrVoteIssueSourceInvalid, VoteIssuePath)
	}
	raw, err := fs.ReadFile(fsys, VoteIssuePath)
	if err != nil {
		return VoteCatalogSource{}, fmt.Errorf("%w: read %s: %v", ErrVoteIssueSourceUnavailable, VoteIssuePath, err)
	}
	return InspectVoteCatalog(VoteIssuePath, raw)
}

// ReadLegacyVoteCatalogFromPostRoot is for an fs.FS rooted at POSTPATH.
func ReadLegacyVoteCatalogFromPostRoot(fsys fs.FS) (VoteCatalogSource, error) {
	if fsys == nil {
		return VoteCatalogSource{}, ErrVoteIssueSourceUnavailable
	}
	info, err := fs.Stat(fsys, "ISSUE")
	if err != nil {
		return VoteCatalogSource{}, fmt.Errorf("%w: ISSUE: %v", ErrVoteIssueSourceUnavailable, err)
	}
	if info.IsDir() || info.Mode()&fs.ModeType != 0 {
		return VoteCatalogSource{}, fmt.Errorf("%w: ISSUE is not a regular file", ErrVoteIssueSourceInvalid)
	}
	raw, err := fs.ReadFile(fsys, "ISSUE")
	if err != nil {
		return VoteCatalogSource{}, fmt.Errorf("%w: read ISSUE: %v", ErrVoteIssueSourceUnavailable, err)
	}
	return InspectVoteCatalog(VoteIssuePath, raw)
}

// LoadLegacyVoteCatalog is the catalog-only convenience API.
func LoadLegacyVoteCatalog(fsys fs.FS) (VoteCatalog, error) {
	source, err := ReadLegacyVoteCatalog(fsys)
	if err != nil {
		return VoteCatalog{}, err
	}
	return source.Catalog.CloneForSource(), nil
}

// LoadVoteCatalog is a concise alias used by command bootstrap code.
func LoadVoteCatalog(fsys fs.FS) (VoteCatalog, error) { return LoadLegacyVoteCatalog(fsys) }

// LoadVoteCatalogFile admits an explicitly configured absolute/relative file
// for the long-running server.  It does not search POSTPATH or accept a
// terminal-provided path.
func LoadVoteCatalogFile(filename string) (VoteCatalogSource, error) {
	if filename == "" {
		return VoteCatalogSource{}, ErrVoteIssueSourceUnavailable
	}
	clean := path.Clean(filename)
	if clean == "." || clean == "/" {
		return VoteCatalogSource{}, ErrVoteIssueSourceInvalid
	}
	info, err := os.Stat(filename)
	if err != nil {
		return VoteCatalogSource{}, fmt.Errorf("%w: %s: %v", ErrVoteIssueSourceUnavailable, filename, err)
	}
	if info.IsDir() || info.Mode()&fs.ModeType != 0 {
		return VoteCatalogSource{}, fmt.Errorf("%w: %s is not a regular file", ErrVoteIssueSourceInvalid, filename)
	}
	raw, err := os.ReadFile(filename)
	if err != nil {
		return VoteCatalogSource{}, fmt.Errorf("%w: read %s: %v", ErrVoteIssueSourceUnavailable, filename, err)
	}
	return InspectVoteCatalog(filename, raw)
}

// Clone returns independent source evidence for an audit caller.
func (s VoteCatalogSource) Clone() VoteCatalogSource { return cloneVoteCatalogSource(s) }
