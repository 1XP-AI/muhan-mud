package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

// Bank snapshot inspection is deliberately smaller than the generic player
// inspection surface. A bank artifact is a complete kind-8 CDTO wire, not a
// player file and not a database record. Keep the bounds here in sync with
// world.BankSnapshotV1's four-megabyte artifact contract.
const (
	maxBankSnapshotInspectionFiles       = 1024
	maxBankSnapshotInspectionFileBytes   = int64(4 << 20)
	maxBankSnapshotInspectionTotalBytes  = int64(64 << 20)
	bankSnapshotInspectionArtifactFormat = "bank-snapshot-v1"
	bankSnapshotInspectionParser         = "go-bank-snapshot-v1"
	bankSnapshotInspectionABI            = "cdto-v1"
)

var (
	errBankSnapshotInspectionInputRequired    = errors.New("bank snapshot inspection requires a directory or file")
	errBankSnapshotInspectionInputsExclusive  = errors.New("bank snapshot inspection directory and file are mutually exclusive")
	errBankSnapshotInspectionDirectoryInvalid = errors.New("bank snapshot inspection directory must be a private 0700 directory")
	errBankSnapshotInspectionFileInvalid      = errors.New("bank snapshot inspection file must be a private 0600 regular file")
	errBankSnapshotInspectionFileTooLarge     = errors.New("bank snapshot inspection file exceeds the size limit")
	errBankSnapshotInspectionFileEmpty        = errors.New("bank snapshot inspection file is empty")
	errBankSnapshotInspectionFileChanged      = errors.New("bank snapshot inspection file changed during inspection")
	errBankSnapshotInspectionTooManyFiles     = errors.New("bank snapshot inspection directory has too many .bin files")
	errBankSnapshotInspectionTooLarge         = errors.New("bank snapshot inspection payload exceeds the size limit")
	errBankSnapshotInspectionEmpty            = errors.New("bank snapshot inspection directory contains no .bin files")
	errBankSnapshotInspectionInvalidReport    = errors.New("invalid bank snapshot review report")
)

// bankSnapshotInspectionOptions is the DB-free input contract that main.go
// can wire later. An empty value means normal server mode; callers selecting
// this mode must provide exactly one of Directory and File.
type bankSnapshotInspectionOptions struct {
	Directory string
	File      string
}

// bankSnapshotReviewReport is intentionally metadata-only. It contains no
// CDTO payload, object text, identity, account, gold, or credential material.
// The report can therefore be handed to an operator for review without
// creating a game or database authority.
type bankSnapshotReviewReport struct {
	Version                int                        `json:"version"`
	ArtifactFormat         string                     `json:"artifact_format"`
	ParserVersion          string                     `json:"parser_version"`
	ABI                    string                     `json:"abi"`
	RequiresOperatorReview bool                       `json:"requires_operator_review"`
	Records                []bankSnapshotReviewRecord `json:"records"`
}

type bankSnapshotReviewRecord struct {
	SourceFile      string `json:"source_file"`
	SourceSHA256    string `json:"source_sha256"`
	SourceOctets    int64  `json:"source_octets"`
	CanonicalSHA256 string `json:"canonical_sha256"`
	CanonicalOctets int64  `json:"canonical_octets"`
	RootCount       int    `json:"root_count"`
	NodeCount       int    `json:"node_count"`
	Result          string `json:"result"`
}

// Aliases keep the terminology usable by callers that think of this as an
// inspection report rather than a review report, without introducing a
// second JSON schema.
type bankSnapshotInspectionReport = bankSnapshotReviewReport
type bankSnapshotInspectionRecord = bankSnapshotReviewRecord

// validateBankSnapshotInspectionFlags validates the future CLI flag pair.
// Unlike import/inspection modes that write a ledger, this mode is always
// DB-free, so there is no dry-run flag and no database-dependent branch.
func validateBankSnapshotInspectionFlags(directory, file string) (bankSnapshotInspectionOptions, error) {
	if directory == "" && file == "" {
		return bankSnapshotInspectionOptions{}, nil
	}
	if directory != "" && file != "" {
		return bankSnapshotInspectionOptions{}, errBankSnapshotInspectionInputsExclusive
	}
	if directory == file {
		return bankSnapshotInspectionOptions{}, errBankSnapshotInspectionInputRequired
	}
	return bankSnapshotInspectionOptions{Directory: directory, File: file}, nil
}

// InspectBankSnapshotReview reads and verifies either all .bin files under a
// private directory or one explicitly designated file. It performs no writes,
// database access, identity lookup, or game-runtime operation.
func InspectBankSnapshotReview(directory, file string) (bankSnapshotReviewReport, error) {
	options, err := validateBankSnapshotInspectionFlags(directory, file)
	if err != nil {
		return bankSnapshotReviewReport{}, err
	}
	if options.Directory == "" && options.File == "" {
		return bankSnapshotReviewReport{}, errBankSnapshotInspectionInputRequired
	}
	return inspectBankSnapshot(options)
}

// inspectBankSnapshot is the pure orchestration boundary used by the future
// CLI wiring. It builds the complete report before returning it, so malformed
// input never yields a partial report that could be mistaken for evidence.
func inspectBankSnapshot(options bankSnapshotInspectionOptions) (bankSnapshotReviewReport, error) {
	if options.Directory == "" && options.File == "" {
		return bankSnapshotReviewReport{}, errBankSnapshotInspectionInputRequired
	}
	if options.Directory != "" && options.File != "" {
		return bankSnapshotReviewReport{}, errBankSnapshotInspectionInputsExclusive
	}
	if options.File != "" {
		return inspectBankSnapshotDesignatedFile(options.File)
	}
	return inspectBankSnapshotDirectory(options.Directory)
}

// inspectBankSnapshotArtifacts is a descriptive alias for the exported
// review entrypoint used by command wiring and tests.
func inspectBankSnapshotArtifacts(directory, file string) (bankSnapshotReviewReport, error) {
	return InspectBankSnapshotReview(directory, file)
}

func inspectBankSnapshotDirectory(directory string) (bankSnapshotReviewReport, error) {
	if directory == "" {
		return bankSnapshotReviewReport{}, errBankSnapshotInspectionInputRequired
	}
	rootInfo, err := bankSnapshotPrivateDirectoryInfo(directory)
	if err != nil {
		return bankSnapshotReviewReport{}, err
	}

	type candidate struct {
		path     string
		relative string
		size     int64
	}
	candidates := make([]candidate, 0)
	var listedBytes int64
	err = filepath.WalkDir(directory, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == directory {
			return nil
		}
		relative, relErr := filepath.Rel(directory, path)
		if relErr != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return errBankSnapshotInspectionDirectoryInvalid
		}
		relative = filepath.ToSlash(relative)
		if !validBankSnapshotRelativePath(relative) {
			return errBankSnapshotInspectionDirectoryInvalid
		}
		// A link anywhere under the supplied private tree is rejected, even if
		// its name is not .bin. Otherwise an operator could not tell whether a
		// seemingly irrelevant link was intentionally hidden from review.
		if entry.Type()&os.ModeSymlink != 0 {
			return errBankSnapshotInspectionFileInvalid
		}
		if entry.IsDir() {
			info, infoErr := entry.Info()
			if infoErr != nil {
				return infoErr
			}
			if info.Mode().Perm() != 0o700 {
				return errBankSnapshotInspectionDirectoryInvalid
			}
			return nil
		}
		if !strings.HasSuffix(relative, ".bin") {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			return errBankSnapshotInspectionFileInvalid
		}
		if info.Size() <= 0 {
			return errBankSnapshotInspectionFileEmpty
		}
		if info.Size() > maxBankSnapshotInspectionFileBytes {
			return errBankSnapshotInspectionFileTooLarge
		}
		if len(candidates) >= maxBankSnapshotInspectionFiles {
			return errBankSnapshotInspectionTooManyFiles
		}
		listedBytes += info.Size()
		if listedBytes > maxBankSnapshotInspectionTotalBytes {
			return errBankSnapshotInspectionTooLarge
		}
		candidates = append(candidates, candidate{path: path, relative: relative, size: info.Size()})
		return nil
	})
	if err != nil {
		return bankSnapshotReviewReport{}, err
	}
	if stableDirErr := bankSnapshotDirectoryStillPrivate(directory, rootInfo); stableDirErr != nil {
		return bankSnapshotReviewReport{}, stableDirErr
	}
	if len(candidates) == 0 {
		return bankSnapshotReviewReport{}, errBankSnapshotInspectionEmpty
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].relative < candidates[j].relative
	})

	report := newBankSnapshotReviewReport()
	var readBytes int64
	for _, item := range candidates {
		raw, readErr := readBankSnapshotInspectionFile(item.path)
		if readErr != nil {
			return bankSnapshotReviewReport{}, fmt.Errorf("%s: %w", item.relative, readErr)
		}
		readBytes += int64(len(raw))
		if readBytes > maxBankSnapshotInspectionTotalBytes {
			return bankSnapshotReviewReport{}, errBankSnapshotInspectionTooLarge
		}
		record, inspectErr := inspectBankSnapshotBytes(item.relative, raw)
		if inspectErr != nil {
			return bankSnapshotReviewReport{}, fmt.Errorf("%s: %w", item.relative, inspectErr)
		}
		report.Records = append(report.Records, record)
	}
	if stableDirErr := bankSnapshotDirectoryStillPrivate(directory, rootInfo); stableDirErr != nil {
		return bankSnapshotReviewReport{}, stableDirErr
	}
	return report, nil
}

func inspectBankSnapshotDesignatedFile(path string) (bankSnapshotReviewReport, error) {
	if path == "" {
		return bankSnapshotReviewReport{}, errBankSnapshotInspectionInputRequired
	}
	parent := filepath.Dir(path)
	parentInfo, err := bankSnapshotPrivateDirectoryInfo(parent)
	if err != nil {
		return bankSnapshotReviewReport{}, err
	}
	raw, err := readBankSnapshotInspectionFile(path)
	if err != nil {
		return bankSnapshotReviewReport{}, err
	}
	report := newBankSnapshotReviewReport()
	record, err := inspectBankSnapshotBytes(filepath.ToSlash(filepath.Clean(path)), raw)
	if err != nil {
		return bankSnapshotReviewReport{}, err
	}
	report.Records = append(report.Records, record)
	if stableDirErr := bankSnapshotDirectoryStillPrivate(parent, parentInfo); stableDirErr != nil {
		return bankSnapshotReviewReport{}, stableDirErr
	}
	return report, nil
}

func newBankSnapshotReviewReport() bankSnapshotReviewReport {
	return bankSnapshotReviewReport{
		Version:                1,
		ArtifactFormat:         bankSnapshotInspectionArtifactFormat,
		ParserVersion:          bankSnapshotInspectionParser,
		ABI:                    bankSnapshotInspectionABI,
		RequiresOperatorReview: true,
		Records:                make([]bankSnapshotReviewRecord, 0),
	}
}

func bankSnapshotPrivateDirectoryInfo(path string) (os.FileInfo, error) {
	if path == "" {
		return nil, errBankSnapshotInspectionDirectoryInvalid
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm() != 0o700 {
		return nil, errBankSnapshotInspectionDirectoryInvalid
	}
	return info, nil
}

func bankSnapshotDirectoryStillPrivate(path string, before os.FileInfo) error {
	after, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !os.SameFile(before, after) || after.Mode()&os.ModeSymlink != 0 || !after.IsDir() || after.Mode().Perm() != 0o700 {
		return errBankSnapshotInspectionDirectoryInvalid
	}
	return nil
}

func readBankSnapshotInspectionFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errBankSnapshotInspectionFileInvalid
	}
	if info.Mode().Perm() != 0o600 {
		return nil, errBankSnapshotInspectionFileInvalid
	}
	if info.Size() <= 0 {
		return nil, errBankSnapshotInspectionFileEmpty
	}
	if info.Size() > maxBankSnapshotInspectionFileBytes {
		return nil, errBankSnapshotInspectionFileTooLarge
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !sameBankSnapshotInspectionFile(info, opened) {
		return nil, errBankSnapshotInspectionFileChanged
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxBankSnapshotInspectionFileBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > maxBankSnapshotInspectionFileBytes {
		return nil, errBankSnapshotInspectionFileTooLarge
	}
	after, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !sameBankSnapshotInspectionFile(opened, after) || int64(len(raw)) != after.Size() {
		return nil, errBankSnapshotInspectionFileChanged
	}
	if len(raw) == 0 {
		return nil, errBankSnapshotInspectionFileEmpty
	}
	return raw, nil
}

func sameBankSnapshotInspectionFile(left, right os.FileInfo) bool {
	return os.SameFile(left, right) && left.Mode().IsRegular() && right.Mode().IsRegular() &&
		left.Mode().Perm() == 0o600 && right.Mode().Perm() == 0o600 && left.Size() == right.Size()
}

func inspectBankSnapshotBytes(sourceFile string, raw []byte) (bankSnapshotReviewRecord, error) {
	if !validBankSnapshotSourcePath(sourceFile) {
		return bankSnapshotReviewRecord{}, errBankSnapshotInspectionInvalidReport
	}
	if len(raw) <= 0 {
		return bankSnapshotReviewRecord{}, errBankSnapshotInspectionFileEmpty
	}
	if int64(len(raw)) > maxBankSnapshotInspectionFileBytes {
		return bankSnapshotReviewRecord{}, errBankSnapshotInspectionFileTooLarge
	}

	decoded, err := world.DecodeBankSnapshotV1(raw)
	if err != nil {
		return bankSnapshotReviewRecord{}, fmt.Errorf("decode: %w", err)
	}
	inspection, err := world.InspectBankSnapshotV1(raw)
	if err != nil {
		return bankSnapshotReviewRecord{}, fmt.Errorf("inspect: %w", err)
	}
	expectedDigest := sha256.Sum256(raw)
	if inspection.SHA256 != expectedDigest {
		return bankSnapshotReviewRecord{}, errors.New("inspect digest does not match source bytes")
	}
	verified, err := world.VerifyBankSnapshotV1(raw, expectedDigest)
	if err != nil {
		return bankSnapshotReviewRecord{}, fmt.Errorf("verify: %w", err)
	}

	// Re-encode all three independently returned values. This checks that the
	// metadata report is based on the same detached canonical graph at every
	// API boundary, rather than silently trusting one decoder result.
	decodedWire, err := world.EncodeBankSnapshotV1(decoded)
	if err != nil {
		return bankSnapshotReviewRecord{}, fmt.Errorf("decode re-encode: %w", err)
	}
	inspectionWire, err := world.EncodeBankSnapshotV1(inspection.Snapshot)
	if err != nil {
		return bankSnapshotReviewRecord{}, fmt.Errorf("inspect re-encode: %w", err)
	}
	verifiedWire, err := world.EncodeBankSnapshotV1(verified)
	if err != nil {
		return bankSnapshotReviewRecord{}, fmt.Errorf("verify re-encode: %w", err)
	}
	if !bytes.Equal(decodedWire, raw) || !bytes.Equal(inspectionWire, raw) || !bytes.Equal(verifiedWire, raw) ||
		!bytes.Equal(decodedWire, inspectionWire) || !bytes.Equal(decodedWire, verifiedWire) {
		return bankSnapshotReviewRecord{}, errors.New("bank snapshot is not canonical")
	}

	rootCount := 0
	for _, node := range verified.Root.Nodes {
		if node.ParentIndex == nil {
			rootCount++
		}
	}
	if len(verified.Root.Nodes) == 0 || verified.Root.Nodes[0].ParentIndex != nil || rootCount != 1 {
		return bankSnapshotReviewRecord{}, errors.New("bank snapshot root metadata is invalid")
	}
	canonicalDigest := sha256.Sum256(verifiedWire)
	return bankSnapshotReviewRecord{
		SourceFile:      sourceFile,
		SourceSHA256:    hex.EncodeToString(expectedDigest[:]),
		SourceOctets:    int64(len(raw)),
		CanonicalSHA256: hex.EncodeToString(canonicalDigest[:]),
		CanonicalOctets: int64(len(verifiedWire)),
		RootCount:       rootCount,
		NodeCount:       len(verified.Root.Nodes),
		Result:          "validated",
	}, nil
}

func validBankSnapshotSourcePath(value string) bool {
	if value == "" || len(value) > 4096 || !utf8.ValidString(value) {
		return false
	}
	for _, b := range []byte(value) {
		if b < 32 || b == 127 {
			return false
		}
	}
	return true
}

func validBankSnapshotRelativePath(value string) bool {
	if !validBankSnapshotSourcePath(value) || strings.Contains(value, "\\") || filepath.IsAbs(value) {
		return false
	}
	clean := filepath.Clean(filepath.FromSlash(value))
	return clean != "." && clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}

// MarshalBankSnapshotReview validates and serializes a metadata-only review
// report. JSON output is deterministic and always ends in one newline.
func MarshalBankSnapshotReview(report bankSnapshotReviewReport) ([]byte, error) {
	if err := validateBankSnapshotReviewReport(report); err != nil {
		return nil, err
	}
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

func marshalBankSnapshotReview(report bankSnapshotReviewReport) ([]byte, error) {
	return MarshalBankSnapshotReview(report)
}

// InspectBankSnapshotReviewJSON is the DB-free JSON boundary intended for a
// future command handler. It returns no JSON on any rejected input.
func InspectBankSnapshotReviewJSON(directory, file string) ([]byte, error) {
	report, err := InspectBankSnapshotReview(directory, file)
	if err != nil {
		return nil, err
	}
	return MarshalBankSnapshotReview(report)
}

func inspectBankSnapshotArtifactsJSON(directory, file string) ([]byte, error) {
	return InspectBankSnapshotReviewJSON(directory, file)
}

func validateBankSnapshotReviewReport(report bankSnapshotReviewReport) error {
	if report.Version != 1 || report.ArtifactFormat != bankSnapshotInspectionArtifactFormat ||
		report.ParserVersion != bankSnapshotInspectionParser || report.ABI != bankSnapshotInspectionABI ||
		!report.RequiresOperatorReview || len(report.Records) == 0 || len(report.Records) > maxBankSnapshotInspectionFiles {
		return errBankSnapshotInspectionInvalidReport
	}
	for _, record := range report.Records {
		if !validBankSnapshotSourcePath(record.SourceFile) || record.Result != "validated" ||
			record.SourceOctets <= 0 || record.SourceOctets > maxBankSnapshotInspectionFileBytes ||
			record.CanonicalOctets != record.SourceOctets || !validLowerHexDigest(record.SourceSHA256) ||
			record.CanonicalSHA256 != record.SourceSHA256 || record.RootCount != 1 || record.NodeCount <= 0 {
			return errBankSnapshotInspectionInvalidReport
		}
	}
	return nil
}

func validLowerHexDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && value == strings.ToLower(value)
}
