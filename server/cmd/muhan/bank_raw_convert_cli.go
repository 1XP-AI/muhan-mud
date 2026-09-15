package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

const bankRawConversionReviewSuffix = ".review.json"

var (
	errBankRawConversionRootRequired   = errors.New("legacy bank raw conversion root is required")
	errBankRawConversionABIRequired    = errors.New("legacy bank raw conversion ABI is required")
	errBankRawConversionOutputRequired = errors.New("legacy bank raw conversion output is required")
	errBankRawConversionOutputInvalid  = errors.New("legacy bank raw conversion output must be a private 0600 file")
	errBankRawConversionOverlap        = errors.New("legacy bank raw conversion output must be outside the source root")
	errBankRawConversionInvalid        = errors.New("invalid legacy bank raw conversion review")
	errBankRawConversionConflict       = errors.New("legacy bank raw conversion output already contains different bytes")
)

type bankRawConversionOptions struct {
	Root       string
	PlayerName string
	OutputPath string
	ABI        string
	DryRun     bool
}

type bankRawConversionResult struct {
	SourceRoot      string
	SourceRelative  string
	PlayerName      string
	SourceSHA256    [sha256.Size]byte
	CanonicalSHA256 [sha256.Size]byte
	SourceOctets    int64
	CanonicalOctets int64
	RootCount       int
	NodeCount       int
	Canonical       []byte
}

// bankRawConversionReview is intentionally not an import manifest. It has no
// world/player ID, command ID, item IDs, account claim, or credential. An
// operator must compare the source name and approve a separate import request.
type bankRawConversionReview struct {
	Version                int                             `json:"version"`
	RawABI                 string                          `json:"raw_abi"`
	ParserVersion          string                          `json:"parser_version"`
	RequiresIdentityReview bool                            `json:"requires_identity_review"`
	Records                []bankRawConversionReviewRecord `json:"records"`
}

type bankRawConversionReviewRecord struct {
	SourceFile       string `json:"source_file"`
	CanonicalFile    string `json:"canonical_file"`
	SourcePlayerName string `json:"source_player_name"`
	SourceSHA256     string `json:"source_sha256"`
	CanonicalSHA256  string `json:"canonical_sha256"`
	SourceOctets     int64  `json:"source_octets"`
	CanonicalOctets  int64  `json:"canonical_octets"`
	RootCount        int    `json:"root_count"`
	NodeCount        int    `json:"node_count"`
}

func validateBankRawConversionFlags(root, playerName, outputPath, abi string, dryRun bool) (bankRawConversionOptions, error) {
	if root == "" {
		if playerName != "" || outputPath != "" || abi != "" || dryRun {
			return bankRawConversionOptions{}, errBankRawConversionRootRequired
		}
		return bankRawConversionOptions{}, nil
	}
	if err := world.ValidateLegacyBankFileLocatorPlayerNameV1(playerName); err != nil {
		if playerName == "" {
			return bankRawConversionOptions{}, errBankRawInspectionNameRequired
		}
		return bankRawConversionOptions{}, fmt.Errorf("invalid legacy bank raw conversion player name: %w", err)
	}
	if err := world.ValidateLegacyBankSnapshotRawV1ABI(abi); err != nil {
		if abi == "" {
			return bankRawConversionOptions{}, errBankRawConversionABIRequired
		}
		return bankRawConversionOptions{}, err
	}
	if !dryRun && outputPath == "" {
		return bankRawConversionOptions{}, errBankRawConversionOutputRequired
	}
	return bankRawConversionOptions{Root: root, PlayerName: playerName, OutputPath: outputPath, ABI: abi, DryRun: dryRun}, nil
}

func convertLegacyBankRawSource(options bankRawConversionOptions) (bankRawConversionResult, error) {
	if options.Root == "" {
		return bankRawConversionResult{}, errBankRawConversionRootRequired
	}
	if err := world.ValidateLegacyBankSnapshotRawV1ABI(options.ABI); err != nil {
		return bankRawConversionResult{}, err
	}
	located, err := world.LocateLegacyBankSnapshotRawV1(options.Root, options.PlayerName)
	if err != nil {
		return bankRawConversionResult{}, err
	}
	inspection, err := world.InspectLegacyBankSnapshotRawV1(located.Source)
	if err != nil {
		return bankRawConversionResult{}, err
	}
	canonicalDigest := sha256.Sum256(inspection.Canonical)
	if located.SHA256 != inspection.SHA256 || located.Metadata.Size != int64(len(inspection.Source)) {
		return bankRawConversionResult{}, errors.New("legacy bank raw source changed between locator and conversion")
	}
	return bankRawConversionResult{
		SourceRoot:      located.Metadata.Root,
		SourceRelative:  filepath.ToSlash(located.Metadata.RelativePath),
		PlayerName:      located.Metadata.PlayerName,
		SourceSHA256:    located.SHA256,
		CanonicalSHA256: canonicalDigest,
		SourceOctets:    int64(len(located.Source)),
		CanonicalOctets: int64(len(inspection.Canonical)),
		RootCount:       countBankRawRoots(inspection.Snapshot.Root),
		NodeCount:       len(inspection.Snapshot.Root.Nodes),
		Canonical:       append([]byte(nil), inspection.Canonical...),
	}, nil
}

func marshalBankRawConversionReview(result bankRawConversionResult, outputPath string) ([]byte, error) {
	if outputPath == "" {
		return nil, errBankRawConversionOutputRequired
	}
	review := bankRawConversionReview{
		Version: 1, RawABI: world.LegacyBankSnapshotRawV1ABI,
		ParserVersion:          world.LegacyBankSnapshotRawV1ParserVersion,
		RequiresIdentityReview: true,
		Records: []bankRawConversionReviewRecord{{
			SourceFile: result.SourceRelative, CanonicalFile: filepath.Base(outputPath), SourcePlayerName: result.PlayerName,
			SourceSHA256: fmt.Sprintf("%x", result.SourceSHA256), CanonicalSHA256: fmt.Sprintf("%x", result.CanonicalSHA256),
			SourceOctets: result.SourceOctets, CanonicalOctets: result.CanonicalOctets,
			RootCount: result.RootCount, NodeCount: result.NodeCount,
		}},
	}
	if err := validateBankRawConversionReview(review); err != nil {
		return nil, err
	}
	raw, err := json.MarshalIndent(review, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

func validateBankRawConversionReview(review bankRawConversionReview) error {
	if review.Version != 1 || review.RawABI != world.LegacyBankSnapshotRawV1ABI ||
		review.ParserVersion != world.LegacyBankSnapshotRawV1ParserVersion || !review.RequiresIdentityReview ||
		len(review.Records) != 1 {
		return errBankRawConversionInvalid
	}
	record := review.Records[0]
	if !validRelativeSnapshotPath(record.SourceFile) || record.CanonicalFile == "" || filepath.Base(record.CanonicalFile) != record.CanonicalFile ||
		!validInspectionSourcePath(record.CanonicalFile) || strings.Contains(record.CanonicalFile, "\\") ||
		world.ValidateLegacyBankFileLocatorPlayerNameV1(record.SourcePlayerName) != nil ||
		record.SourceOctets <= 0 || record.SourceOctets > int64(world.LegacyBankFileLocatorV1MaxBytes) ||
		record.CanonicalOctets <= 0 || record.CanonicalOctets > int64(world.LegacyBankFileLocatorV1MaxBytes) ||
		record.RootCount != 1 || record.NodeCount < 1 || !isSHA256Hex(record.SourceSHA256) || !isSHA256Hex(record.CanonicalSHA256) {
		return errBankRawConversionInvalid
	}
	return nil
}

func writeBankRawConversion(result bankRawConversionResult, outputPath string) (string, error) {
	if outputPath == "" {
		return "", errBankRawConversionOutputRequired
	}
	outputPath, err := filepath.Abs(filepath.Clean(outputPath))
	if err != nil {
		return "", errBankRawConversionOutputInvalid
	}
	parent := filepath.Dir(outputPath)
	if _, err := privateDirectoryInfo(parent, errBankRawConversionOutputInvalid); err != nil {
		return "", err
	}
	if pathsOverlap(result.SourceRoot, parent) {
		return "", errBankRawConversionOverlap
	}
	reviewPath := outputPath + bankRawConversionReviewSuffix
	reviewRaw, err := marshalBankRawConversionReview(result, outputPath)
	if err != nil {
		return "", err
	}
	if err := checkBankRawConversionOutput(outputPath, result.Canonical); err != nil {
		return "", err
	}
	if err := checkBankRawConversionOutput(reviewPath, reviewRaw); err != nil {
		return "", err
	}
	if err := writePrivateImmutableFile(outputPath, result.Canonical); err != nil {
		return "", fmt.Errorf("write canonical bank snapshot: %w", err)
	}
	if err := writePrivateImmutableFile(reviewPath, reviewRaw); err != nil {
		return "", fmt.Errorf("write bank snapshot review: %w", err)
	}
	return reviewPath, nil
}

func checkBankRawConversionOutput(path string, contents []byte) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return errBankRawConversionConflict
	}
	stored, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if string(stored) == string(contents) {
		return nil
	}
	return errBankRawConversionConflict
}
