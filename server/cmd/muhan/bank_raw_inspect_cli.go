package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

const (
	bankRawInspectionVersion        = 1
	bankRawInspectionArtifactFormat = "legacy-bank-raw-v1"
	bankRawInspectionResult         = "validated"
)

var (
	errBankRawInspectionRootRequired  = errors.New("legacy bank raw inspection root is required")
	errBankRawInspectionNameRequired  = errors.New("legacy bank raw inspection player name is required")
	errBankRawInspectionInvalidReport = errors.New("invalid legacy bank raw inspection report")
)

// bankRawInspectionOptions is the DB-free source contract for the legacy raw
// bank locator. Root is an explicit MUHAN_HOME-like absolute directory and
// PlayerName is one canonical path component under player/bank.
type bankRawInspectionOptions struct {
	Root       string
	PlayerName string
	DryRun     bool
}

// bankRawInspectionReport contains only provenance and parser evidence. It
// deliberately does not expose any decoded object, account, balance, item,
// password, or raw bytes.
type bankRawInspectionReport struct {
	Version                int                     `json:"version"`
	ArtifactFormat         string                  `json:"artifact_format"`
	ParserVersion          string                  `json:"parser_version"`
	ABI                    string                  `json:"abi"`
	RequiresOperatorReview bool                    `json:"requires_operator_review"`
	Source                 bankRawInspectionSource `json:"source"`
	Result                 string                  `json:"result"`
}

type bankRawInspectionSource struct {
	Root             string `json:"root"`
	SourcePath       string `json:"source_path"`
	RelativePath     string `json:"relative_path"`
	PlayerName       string `json:"player_name"`
	SourceSHA256     string `json:"source_sha256"`
	SourceOctets     int64  `json:"source_octets"`
	CanonicalSHA256  string `json:"canonical_sha256"`
	CanonicalOctets  int64  `json:"canonical_octets"`
	RootCount        int    `json:"root_count"`
	NodeCount        int    `json:"node_count"`
	Mode             string `json:"mode"`
	UID              uint64 `json:"uid"`
	GID              uint64 `json:"gid"`
	Nlink            uint64 `json:"nlink"`
	Device           uint64 `json:"device"`
	Inode            uint64 `json:"inode"`
	ModificationTime string `json:"modification_time"`
	ChangeTime       string `json:"change_time"`
}

func validateBankRawInspectionFlags(root, playerName string, dryRun bool) (bankRawInspectionOptions, error) {
	if root == "" {
		if playerName != "" || dryRun {
			return bankRawInspectionOptions{}, errBankRawInspectionRootRequired
		}
		return bankRawInspectionOptions{}, nil
	}
	if playerName == "" {
		return bankRawInspectionOptions{}, errBankRawInspectionNameRequired
	}
	if err := world.ValidateLegacyBankFileLocatorPlayerNameV1(playerName); err != nil {
		return bankRawInspectionOptions{}, fmt.Errorf("invalid legacy bank raw inspection player name: %w", err)
	}
	return bankRawInspectionOptions{Root: root, PlayerName: playerName, DryRun: dryRun}, nil
}

// InspectLegacyBankRawReview locates and parses one audited LP64 raw bank
// file. The exact ABI is checked before bytes enter the parser; no database
// connection or live game state is touched.
func InspectLegacyBankRawReview(root, playerName string) (bankRawInspectionReport, error) {
	options, err := validateBankRawInspectionFlags(root, playerName, false)
	if err != nil {
		return bankRawInspectionReport{}, err
	}
	if options.Root == "" {
		return bankRawInspectionReport{}, errBankRawInspectionRootRequired
	}
	if err := world.ValidateLegacyBankSnapshotRawV1ABI(world.LegacyBankSnapshotRawV1ABI); err != nil {
		return bankRawInspectionReport{}, fmt.Errorf("legacy bank raw ABI rejected: %w", err)
	}
	located, err := world.LocateLegacyBankSnapshotRawV1(options.Root, options.PlayerName)
	if err != nil {
		return bankRawInspectionReport{}, err
	}
	inspection, err := world.InspectLegacyBankSnapshotRawV1(located.Source)
	if err != nil {
		return bankRawInspectionReport{}, err
	}
	canonicalDigest := sha256.Sum256(inspection.Canonical)
	if located.SHA256 != inspection.SHA256 || located.Metadata.Size != int64(len(inspection.Source)) {
		return bankRawInspectionReport{}, errors.New("legacy bank raw source changed between locator and parser")
	}
	metadata := located.Metadata
	return bankRawInspectionReport{
		Version:                bankRawInspectionVersion,
		ArtifactFormat:         bankRawInspectionArtifactFormat,
		ParserVersion:          world.LegacyBankSnapshotRawV1ParserVersion,
		ABI:                    world.LegacyBankSnapshotRawV1ABI,
		RequiresOperatorReview: true,
		Source: bankRawInspectionSource{
			Root:             metadata.Root,
			SourcePath:       metadata.Path,
			RelativePath:     metadata.RelativePath,
			PlayerName:       metadata.PlayerName,
			SourceSHA256:     hex.EncodeToString(located.SHA256[:]),
			SourceOctets:     int64(len(located.Source)),
			CanonicalSHA256:  hex.EncodeToString(canonicalDigest[:]),
			CanonicalOctets:  int64(len(inspection.Canonical)),
			RootCount:        countBankRawRoots(inspection.Snapshot.Root),
			NodeCount:        len(inspection.Snapshot.Root.Nodes),
			Mode:             fmt.Sprintf("%04o", metadata.ModeBits),
			UID:              metadata.UID,
			GID:              metadata.GID,
			Nlink:            metadata.Nlink,
			Device:           metadata.Device,
			Inode:            metadata.Inode,
			ModificationTime: metadata.ModTime.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
			ChangeTime:       metadata.ChangeTime.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
		},
		Result: bankRawInspectionResult,
	}, nil
}

func countBankRawRoots(graph world.PlayerSnapshotObjectGraphV1) int {
	roots := 0
	for _, node := range graph.Nodes {
		if node.ParentIndex == nil {
			roots++
		}
	}
	return roots
}

func MarshalLegacyBankRawReview(report bankRawInspectionReport) ([]byte, error) {
	if err := validateBankRawInspectionReport(report); err != nil {
		return nil, err
	}
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

func InspectLegacyBankRawReviewJSON(root, playerName string) ([]byte, error) {
	report, err := InspectLegacyBankRawReview(root, playerName)
	if err != nil {
		return nil, err
	}
	return MarshalLegacyBankRawReview(report)
}

func validateBankRawInspectionReport(report bankRawInspectionReport) error {
	if report.Version != bankRawInspectionVersion ||
		report.ArtifactFormat != bankRawInspectionArtifactFormat ||
		report.ParserVersion != world.LegacyBankSnapshotRawV1ParserVersion ||
		report.ABI != world.LegacyBankSnapshotRawV1ABI ||
		!report.RequiresOperatorReview || report.Result != bankRawInspectionResult {
		return errBankRawInspectionInvalidReport
	}
	source := report.Source
	if source.Root == "" || source.SourcePath == "" || source.RelativePath == "" || source.PlayerName == "" ||
		!utf8.ValidString(source.Root) || !utf8.ValidString(source.SourcePath) || !utf8.ValidString(source.RelativePath) ||
		!utf8.ValidString(source.PlayerName) || strings.ContainsAny(source.Root, "\x00\r\n") ||
		strings.ContainsAny(source.SourcePath, "\x00\r\n") || strings.ContainsAny(source.RelativePath, "\x00\r\n") ||
		source.SourceOctets <= 0 || source.SourceOctets > int64(world.LegacyBankFileLocatorV1MaxBytes) ||
		source.CanonicalOctets <= 0 || source.CanonicalOctets > int64(world.LegacyBankFileLocatorV1MaxBytes) ||
		!isSHA256Hex(source.SourceSHA256) || !isSHA256Hex(source.CanonicalSHA256) ||
		source.RootCount != 1 || source.NodeCount < 1 || source.Mode != "0600" || source.Nlink != 1 ||
		source.ModificationTime == "" || source.ChangeTime == "" {
		return errBankRawInspectionInvalidReport
	}
	if err := world.ValidateLegacyBankFileLocatorPlayerNameV1(source.PlayerName); err != nil {
		return errBankRawInspectionInvalidReport
	}
	return nil
}

func isSHA256Hex(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
