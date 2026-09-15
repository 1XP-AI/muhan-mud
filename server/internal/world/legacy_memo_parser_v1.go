package world

// This file parses the text emitted by command12.c:memo.  The source format
// is intentionally kept separate from the live State.Memos aggregate: a
// caller must provide an audited ABI, the canonical recipient ID, and an
// explicit sender-name-to-ID mapping.  Names in a legacy header never claim
// an account, and neither raw paths nor credentials appear in the result.

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// LegacyMemoFileV1ABI is the complete contract for the bytes emitted by
	// command12.c:memo.  ctime has no timezone field, so the default parser
	// interpretation is UTC; operators that know the source timezone can set
	// TimestampLocation explicitly.
	LegacyMemoFileV1ABI = "legacy-memo-file-v1;path=player/fal/<canonical-name>;encoding=utf-8;newline=lf;record-lines=2;ctime-header-same-line;ctime=Mon Jan _2 15:04:05 2006;body-prefix=>>>>> ;body-max=80;timestamp-years=1970..2100"

	// Aliases keep the ABI visible to callers that name the parser rather than
	// the source file.  They intentionally refer to the same exact contract.
	LegacyMemoFileLocatorV1ABI = LegacyMemoFileV1ABI
	LegacyMemoParserV1ABI      = LegacyMemoFileV1ABI

	LegacyMemoParserV1ParserVersion = "go-legacy-memo-parser-v1"
	LegacyMemoFileV1ParserVersion   = LegacyMemoParserV1ParserVersion

	LegacyMemoTimestampFormat  = "Mon Jan _2 15:04:05 2006"
	LegacyMemoTimestampMinYear = 1970
	LegacyMemoTimestampMaxYear = 2100

	// A line-count bound is independent of the byte bound.  It prevents an
	// input made of many tiny lines from causing an unbounded record slice.
	LegacyMemoFileV1MaxLines   = 120000
	LegacyMemoFileV1MaxRecords = LegacyMemoFileV1MaxLines / 2

	LegacyMemoFileV1MaxBytes = LegacyMemoFileLocatorV1MaxBytes
)

var (
	ErrLegacyMemoUnsupportedABI   = errors.New("unsupported legacy memo ABI")
	ErrLegacyMemoMalformed        = errors.New("malformed legacy memo file")
	ErrLegacyMemoTruncated        = errors.New("truncated legacy memo file")
	ErrLegacyMemoTrailingBytes    = errors.New("trailing legacy memo file bytes")
	ErrLegacyMemoLineCount        = errors.New("invalid legacy memo line count")
	ErrLegacyMemoInvalidUTF8      = errors.New("legacy memo file is not valid UTF-8")
	ErrLegacyMemoInvalidTimestamp = errors.New("invalid legacy memo timestamp")
	ErrLegacyMemoTimestampBound   = errors.New("legacy memo timestamp outside bound")
	ErrLegacyMemoInvalidHeader    = errors.New("invalid legacy memo header")
	ErrLegacyMemoInvalidBody      = errors.New("invalid legacy memo body")
	ErrLegacyMemoBodyTooLong      = errors.New("legacy memo body exceeds byte limit")
	ErrLegacyMemoIdentityMapping  = errors.New("legacy memo identity mapping is required")
	ErrLegacyMemoIdentityConflict = errors.New("legacy memo identity mapping conflicts")
	ErrLegacyMemoRecordLimit      = errors.New("legacy memo record limit exceeded")
	ErrLegacyMemoSizeLimit        = errors.New("legacy memo source exceeds size limit")

	// Compatibility spellings for raw-parser callers.
	ErrLegacyMemoRawUnsupportedABI = ErrLegacyMemoUnsupportedABI
	ErrLegacyMemoRawMalformed      = ErrLegacyMemoMalformed
	ErrLegacyMemoRawTruncated      = ErrLegacyMemoTruncated
	ErrLegacyMemoRawTrailingBytes  = ErrLegacyMemoTrailingBytes
)

// LegacyMemoIdentityMappingV1 is the only identity input accepted by the
// parser.  RecipientID is explicit even when RecipientName is present.  A
// sender's display name is merely a lookup key; the parser never derives an
// account or character ID from that name.
type LegacyMemoIdentityMappingV1 struct {
	RecipientName string
	RecipientID   string
	SenderIDs     map[string]string
	// SenderIDByName is a descriptive compatibility spelling.  If both maps
	// are supplied they must contain exactly the same mapping.
	SenderIDByName map[string]string
}

// LegacyMemoParserOptionsV1 binds all values not carried by the legacy text
// itself.  ABI is required on every call because legacy files have no marker.
type LegacyMemoParserOptionsV1 struct {
	ABI            string
	RecipientName  string
	RecipientID    string
	SenderIDs      map[string]string
	SenderIDByName map[string]string

	// TimestampLocation defaults to UTC for deterministic replay.  ctime's
	// textual output has no zone, so an operator may provide the known source
	// location when preserving wall-clock meaning is required.
	TimestampLocation *time.Location
}

// Compatibility aliases for callers using a shorter noun.
type LegacyMemoParseOptionsV1 = LegacyMemoParserOptionsV1
type LegacyMemoIdentityMapV1 = LegacyMemoIdentityMappingV1

// LegacyMemoFileV1 is a pointer-free, source-neutral projection of one
// recipient's legacy fal file.  Memos and Records intentionally point at the
// same ordered slice; Records is retained for callers that use row language.
// Neither field contains a raw path or credential.
type LegacyMemoFileV1 struct {
	RecipientName string
	RecipientID   string
	Memos         []CharacterMemo
	Records       []CharacterMemo
	SourceSHA256  [sha256.Size]byte
	SourceOctets  int
	LineCount     int
}

type LegacyMemoAggregateV1 = LegacyMemoFileV1
type LegacyCharacterMemosV1 = LegacyMemoFileV1

func (f LegacyMemoFileV1) Clone() LegacyMemoFileV1 {
	if f.Memos != nil {
		memos := make([]CharacterMemo, len(f.Memos))
		copy(memos, f.Memos)
		f.Memos = memos
	}
	if f.Records != nil {
		records := make([]CharacterMemo, len(f.Records))
		copy(records, f.Records)
		f.Records = records
	}
	return f
}

// ValidateLegacyMemoFileV1ABI requires the exact audited text contract.
func ValidateLegacyMemoFileV1ABI(contract string) error {
	if contract != LegacyMemoFileV1ABI {
		return ErrLegacyMemoUnsupportedABI
	}
	return nil
}

func ValidateLegacyMemoParserV1ABI(contract string) error {
	return ValidateLegacyMemoFileV1ABI(contract)
}

// NewLegacyMemoParserOptionsV1 is a convenience constructor that still
// records the explicit ABI in the resulting options value.  A direct struct
// literal is equally valid when a caller wants a non-UTC source location.
func NewLegacyMemoParserOptionsV1(recipientName, recipientID string, senderIDs map[string]string) LegacyMemoParserOptionsV1 {
	return LegacyMemoParserOptionsV1{
		ABI: LegacyMemoFileV1ABI, RecipientName: recipientName, RecipientID: recipientID,
		SenderIDs: senderIDs,
	}
}

// ParseLegacyMemoFileV1 parses complete command12.c:memo records.  Input is
// copied before inspection, and only the digest/size are retained in the
// returned aggregate.  Any partial record, unknown line, trailing byte,
// malformed UTF-8, invalid timestamp, or missing identity mapping rejects
// the whole source.
func ParseLegacyMemoFileV1(raw []byte, options LegacyMemoParserOptionsV1) (LegacyMemoFileV1, error) {
	if err := ValidateLegacyMemoFileV1ABI(options.ABI); err != nil {
		return LegacyMemoFileV1{}, err
	}
	normalized, senderIDs, location, err := normalizeLegacyMemoParserOptions(options)
	if err != nil {
		return LegacyMemoFileV1{}, err
	}
	if len(raw) > LegacyMemoFileV1MaxBytes {
		return LegacyMemoFileV1{}, ErrLegacyMemoSizeLimit
	}
	source := append([]byte(nil), raw...)
	digest := sha256.Sum256(source)
	if len(source) == 0 {
		return emptyLegacyMemoFile(normalized, digest), nil
	}
	if !utf8.Valid(source) {
		return LegacyMemoFileV1{}, fmt.Errorf("%w: %w", ErrLegacyMemoMalformed, ErrLegacyMemoInvalidUTF8)
	}
	if source[len(source)-1] != '\n' {
		return LegacyMemoFileV1{}, fmt.Errorf("%w: source does not end with LF", ErrLegacyMemoTrailingBytes)
	}

	lines := bytes.Split(source, []byte{'\n'})
	lineCount := len(lines) - 1 // the final empty split is the required LF
	if lineCount > LegacyMemoFileV1MaxLines {
		return LegacyMemoFileV1{}, fmt.Errorf("%w: %d lines", ErrLegacyMemoRecordLimit, lineCount)
	}
	if lineCount == 0 || lineCount%2 != 0 {
		return LegacyMemoFileV1{}, fmt.Errorf("%w: got %d lines", ErrLegacyMemoLineCount, lineCount)
	}
	recordCount := lineCount / 2
	if recordCount > LegacyMemoFileV1MaxRecords {
		return LegacyMemoFileV1{}, ErrLegacyMemoRecordLimit
	}

	records := make([]CharacterMemo, 0, recordCount)
	for recordIndex := 0; recordIndex < recordCount; recordIndex++ {
		line := recordIndex * 2
		firstLine := string(lines[line])
		if len(firstLine) < len("Wed Sep 10 12:34:56 2026") {
			return LegacyMemoFileV1{}, fmt.Errorf("%w: record %d timestamp/header line", ErrLegacyMemoMalformed, recordIndex)
		}
		createdAt, err := parseLegacyMemoTimestampV1(firstLine[:len("Wed Sep 10 12:34:56 2026")], location)
		if err != nil {
			return LegacyMemoFileV1{}, fmt.Errorf("%w: record %d: %w", ErrLegacyMemoMalformed, recordIndex, err)
		}
		senderName, err := parseLegacyMemoHeaderV1(firstLine[len("Wed Sep 10 12:34:56 2026"):])
		if err != nil {
			return LegacyMemoFileV1{}, fmt.Errorf("%w: record %d: %w", ErrLegacyMemoMalformed, recordIndex, err)
		}
		senderID, ok := senderIDs[senderName]
		if !ok {
			return LegacyMemoFileV1{}, fmt.Errorf("%w: sender %q has no explicit ID", ErrLegacyMemoIdentityMapping, senderName)
		}
		body, err := parseLegacyMemoBodyV1(string(lines[line+1]))
		if err != nil {
			return LegacyMemoFileV1{}, fmt.Errorf("%w: record %d: %w", ErrLegacyMemoMalformed, recordIndex, err)
		}
		records = append(records, CharacterMemo{
			ID:         legacyMemoRecordIDV1(digest, recordIndex),
			SenderID:   senderID,
			SenderName: senderName,
			Body:       body,
			CreatedAt:  createdAt,
		})
	}

	return LegacyMemoFileV1{
		RecipientName: normalized.RecipientName,
		RecipientID:   normalized.RecipientID,
		Memos:         records,
		Records:       records,
		SourceSHA256:  digest,
		SourceOctets:  len(source),
		LineCount:     lineCount,
	}, nil
}

// DecodeLegacyMemoFileV1 is a parser-named compatibility alias.
func DecodeLegacyMemoFileV1(raw []byte, options LegacyMemoParserOptionsV1) (LegacyMemoFileV1, error) {
	return ParseLegacyMemoFileV1(raw, options)
}

func ParseLegacyMemoV1(raw []byte, options LegacyMemoParserOptionsV1) (LegacyMemoFileV1, error) {
	return ParseLegacyMemoFileV1(raw, options)
}

// Parse uses the recipient name proven by the descriptor-bound locator when
// the caller has not repeated it in options.  A conflicting name is rejected
// before parsing so a valid file cannot be relabeled by an accidental mapping.
func (s LegacyMemoFileSourceV1) Parse(options LegacyMemoParserOptionsV1) (LegacyMemoFileV1, error) {
	if options.RecipientName == "" {
		options.RecipientName = s.Metadata.RecipientName
	} else if s.Metadata.RecipientName != "" && options.RecipientName != s.Metadata.RecipientName {
		return LegacyMemoFileV1{}, fmt.Errorf("%w: source recipient name mismatch", ErrLegacyMemoIdentityConflict)
	}
	return ParseLegacyMemoFileV1(s.Source, options)
}

func ParseLegacyMemoFileSourceV1(source LegacyMemoFileSourceV1, options LegacyMemoParserOptionsV1) (LegacyMemoFileV1, error) {
	return source.Parse(options)
}

func emptyLegacyMemoFile(options LegacyMemoParserOptionsV1, digest [sha256.Size]byte) LegacyMemoFileV1 {
	records := make([]CharacterMemo, 0)
	return LegacyMemoFileV1{
		RecipientName: options.RecipientName,
		RecipientID:   options.RecipientID,
		Memos:         records,
		Records:       records,
		SourceSHA256:  digest,
		SourceOctets:  0,
		LineCount:     0,
	}
}

func normalizeLegacyMemoParserOptions(options LegacyMemoParserOptionsV1) (LegacyMemoParserOptionsV1, map[string]string, *time.Location, error) {
	if options.RecipientName != "" {
		if err := ValidateLegacyMemoFileLocatorNameV1(options.RecipientName); err != nil {
			return LegacyMemoParserOptionsV1{}, nil, nil, fmt.Errorf("%w: recipient name", ErrLegacyMemoIdentityMapping)
		}
	}
	if !validLegacyMemoIdentityID(options.RecipientID) {
		return LegacyMemoParserOptionsV1{}, nil, nil, fmt.Errorf("%w: recipient ID", ErrLegacyMemoIdentityMapping)
	}
	if options.SenderIDs != nil && options.SenderIDByName != nil && !equalStringMap(options.SenderIDs, options.SenderIDByName) {
		return LegacyMemoParserOptionsV1{}, nil, nil, ErrLegacyMemoIdentityConflict
	}
	senderIDs := options.SenderIDs
	if senderIDs == nil {
		senderIDs = options.SenderIDByName
	}
	if senderIDs == nil {
		return LegacyMemoParserOptionsV1{}, nil, nil, ErrLegacyMemoIdentityMapping
	}
	copySenderIDs := make(map[string]string, len(senderIDs))
	seenIDs := make(map[string]string, len(senderIDs))
	for senderName, senderID := range senderIDs {
		if err := ValidateLegacyMemoFileLocatorNameV1(senderName); err != nil {
			return LegacyMemoParserOptionsV1{}, nil, nil, fmt.Errorf("%w: sender name", ErrLegacyMemoIdentityMapping)
		}
		if !validLegacyMemoIdentityID(senderID) {
			return LegacyMemoParserOptionsV1{}, nil, nil, fmt.Errorf("%w: sender ID", ErrLegacyMemoIdentityMapping)
		}
		if previous, exists := seenIDs[senderID]; exists && previous != senderName {
			return LegacyMemoParserOptionsV1{}, nil, nil, fmt.Errorf("%w: ID reused by %q and %q", ErrLegacyMemoIdentityConflict, previous, senderName)
		}
		seenIDs[senderID] = senderName
		copySenderIDs[senderName] = senderID
	}
	location := options.TimestampLocation
	if location == nil {
		location = time.UTC
	}
	return options, copySenderIDs, location, nil
}

func validLegacyMemoIdentityID(id string) bool {
	if !validMemoID(id) || strings.ContainsAny(id, "/\\") {
		return false
	}
	return true
}

func equalStringMap(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func parseLegacyMemoTimestampV1(line string, location *time.Location) (time.Time, error) {
	if len(line) != len("Wed Sep 10 12:34:56 2026") {
		return time.Time{}, ErrLegacyMemoInvalidTimestamp
	}
	for _, b := range []byte(line) {
		if b < 0x20 || b > 0x7e {
			return time.Time{}, ErrLegacyMemoInvalidTimestamp
		}
	}
	parsed, err := time.ParseInLocation(LegacyMemoTimestampFormat, line, location)
	if err != nil || parsed.Format(LegacyMemoTimestampFormat) != line {
		return time.Time{}, ErrLegacyMemoInvalidTimestamp
	}
	if parsed.Year() < LegacyMemoTimestampMinYear || parsed.Year() > LegacyMemoTimestampMaxYear {
		return time.Time{}, ErrLegacyMemoTimestampBound
	}
	return parsed.Round(0), nil
}

func parseLegacyMemoHeaderV1(line string) (string, error) {
	const prefix = " 에 ["
	const suffix = "] 님이 남기신 메모 : "
	if !strings.HasPrefix(line, prefix) || !strings.HasSuffix(line, suffix) {
		return "", ErrLegacyMemoInvalidHeader
	}
	name := line[len(prefix) : len(line)-len(suffix)]
	if name == "" || !utf8.ValidString(name) {
		return "", ErrLegacyMemoInvalidHeader
	}
	if err := ValidateLegacyMemoFileLocatorNameV1(name); err != nil {
		return "", ErrLegacyMemoInvalidHeader
	}
	return name, nil
}

func parseLegacyMemoBodyV1(line string) (string, error) {
	const prefix = ">>>>> "
	if !strings.HasPrefix(line, prefix) {
		return "", ErrLegacyMemoInvalidBody
	}
	body := line[len(prefix):]
	if err := ValidateMemoBody(body); err != nil {
		if errors.Is(err, ErrMemoBodyTooLong) {
			return "", fmt.Errorf("%w: %w", ErrLegacyMemoInvalidBody, ErrLegacyMemoBodyTooLong)
		}
		return "", fmt.Errorf("%w: %v", ErrLegacyMemoInvalidBody, err)
	}
	if len(body) > MaxMemoBodyBytes {
		return "", fmt.Errorf("%w: %w", ErrLegacyMemoInvalidBody, ErrLegacyMemoBodyTooLong)
	}
	return body, nil
}

func legacyMemoRecordIDV1(digest [sha256.Size]byte, index int) string {
	// Eight digest bytes plus the record index are sufficient to make IDs
	// deterministic within a reviewed source while remaining comfortably below
	// MaxMemoIDBytes.  The full source digest is retained separately as evidence.
	return "legacy-memo-v1-" + fmt.Sprintf("%x", digest[:8]) + "-" + strconv.Itoa(index)
}
