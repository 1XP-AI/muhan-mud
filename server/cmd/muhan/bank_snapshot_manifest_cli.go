package main

// This file is the DB-free promotion boundary for bank artifacts.  The raw
// converter produces a kind-8 snapshot plus metadata-only review; this
// command combines that review with an operator-owned identity/item mapping
// and creates a manifest consumed by the explicit apply path below.  No name
// in a legacy file is treated as an account or character claim.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/1XP-Inc/muhan-mud/server/internal/identity"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

const (
	maxBankSnapshotManifestBytes        int64 = 8 << 20
	maxBankSnapshotManifestRecords            = 1024
	maxBankSnapshotManifestPayloadBytes int64 = 64 << 20
	maxBankSnapshotManifestFileBytes    int64 = 4 << 20
)

var (
	errBankSnapshotManifestPathRequired       = errors.New("bank snapshot manifest path is required")
	errBankSnapshotManifestNotPrivate         = errors.New("bank snapshot manifest must be a private 0600 regular file")
	errBankSnapshotManifestNotRegular         = errors.New("bank snapshot manifest must be a regular file")
	errBankSnapshotManifestTooLarge           = errors.New("bank snapshot manifest exceeds the size limit")
	errBankSnapshotManifestInvalid            = errors.New("invalid bank snapshot import manifest")
	errBankSnapshotManifestApplyRequired      = errors.New("bank snapshot manifest apply requires -import-bank-snapshot-manifest")
	errBankSnapshotManifestDryRunRequired     = errors.New("bank snapshot manifest dry-run requires -import-bank-snapshot-manifest")
	errBankSnapshotManifestApplyDryRun        = errors.New("bank snapshot manifest apply and dry-run modes are mutually exclusive")
	errBankSnapshotManifestFileNotPrivate     = errors.New("bank snapshot artifact must be a private 0600 regular file")
	errBankSnapshotManifestFileNotRegular     = errors.New("bank snapshot artifact must be a regular file")
	errBankSnapshotManifestFileTooLarge       = errors.New("bank snapshot artifact exceeds the size limit")
	errBankSnapshotManifestBuildReviewNeeded  = errors.New("bank snapshot manifest build review path is required")
	errBankSnapshotManifestBuildMappingNeeded = errors.New("bank snapshot manifest build identity mapping path is required")
	errBankSnapshotManifestBuildOutputNeeded  = errors.New("bank snapshot manifest build output path is required")
	errBankSnapshotManifestBuildInvalid       = errors.New("invalid bank snapshot manifest build input")
	errBankSnapshotManifestBuildOutputInvalid = errors.New("bank snapshot manifest output must be a private 0600 file beside the review")
	errBankSnapshotManifestBuildConflict      = errors.New("bank snapshot manifest output already contains different bytes")
	errBankSnapshotManifestBuildOverlap       = errors.New("bank snapshot manifest output must not overlap the review source")
)

// bankSnapshotImportOptions is intentionally explicit: a path alone only
// validates the artifact. Apply is required before any PostgreSQL connection
// or world mutation is attempted.
type bankSnapshotImportOptions struct {
	ManifestPath string
	DryRun       bool
	Apply        bool
}

type bankSnapshotImportManifest struct {
	Version int                                `json:"version"`
	WorldID string                             `json:"world_id"`
	Records []bankSnapshotImportManifestRecord `json:"records"`
}

type bankSnapshotImportManifestRecord struct {
	CommandID        string   `json:"command_id"`
	ExpectedRevision *int64   `json:"expected_revision"`
	AccountName      string   `json:"account_name"`
	PlayerID         string   `json:"player_id"`
	SnapshotFile     string   `json:"snapshot_file"`
	SnapshotSHA256   string   `json:"snapshot_sha256"`
	ItemIDs          []string `json:"item_ids"`
}

type bankSnapshotImportBatch struct {
	WorldID  string
	Requests []storage.BankSnapshotImport
}

// bankSnapshotIdentityMapping is supplied after the metadata-only bank
// review.  AccountName must agree with the reviewed legacy player name, but
// the mapping's PlayerID and ItemIDs remain caller-owned immutable IDs.
type bankSnapshotIdentityMapping struct {
	Version int                                 `json:"version"`
	WorldID string                              `json:"world_id"`
	Records []bankSnapshotIdentityMappingRecord `json:"records"`
}

type bankSnapshotIdentityMappingRecord struct {
	SnapshotFile     string   `json:"snapshot_file"`
	CommandID        string   `json:"command_id"`
	ExpectedRevision *int64   `json:"expected_revision"`
	AccountName      string   `json:"account_name"`
	PlayerID         string   `json:"player_id"`
	ItemIDs          []string `json:"item_ids"`
}

type bankSnapshotManifestBuildOptions struct {
	ReviewPath  string
	MappingPath string
	OutputPath  string
	DryRun      bool
}

func validateBankSnapshotImportFlags(path string, dryRun, apply bool) (bankSnapshotImportOptions, error) {
	if path == "" {
		if dryRun {
			return bankSnapshotImportOptions{}, errBankSnapshotManifestDryRunRequired
		}
		if apply {
			return bankSnapshotImportOptions{}, errBankSnapshotManifestApplyRequired
		}
		return bankSnapshotImportOptions{}, nil
	}
	if dryRun && apply {
		return bankSnapshotImportOptions{}, errBankSnapshotManifestApplyDryRun
	}
	return bankSnapshotImportOptions{ManifestPath: path, DryRun: !apply || dryRun, Apply: apply}, nil
}

func validateBankSnapshotManifestBuildFlags(reviewPath, mappingPath, outputPath string, dryRun bool) (bankSnapshotManifestBuildOptions, error) {
	if reviewPath == "" {
		if mappingPath != "" || outputPath != "" || dryRun {
			return bankSnapshotManifestBuildOptions{}, errBankSnapshotManifestBuildReviewNeeded
		}
		return bankSnapshotManifestBuildOptions{}, nil
	}
	if mappingPath == "" {
		return bankSnapshotManifestBuildOptions{}, errBankSnapshotManifestBuildMappingNeeded
	}
	if !dryRun && outputPath == "" {
		return bankSnapshotManifestBuildOptions{}, errBankSnapshotManifestBuildOutputNeeded
	}
	return bankSnapshotManifestBuildOptions{ReviewPath: reviewPath, MappingPath: mappingPath, OutputPath: outputPath, DryRun: dryRun}, nil
}

// readBankSnapshotImportManifest validates every artifact before returning
// any request.  Snapshot bytes are kept in memory only for the subsequent
// explicit Apply call; they are never logged or included in the receipt.
func readBankSnapshotImportManifest(path string) (bankSnapshotImportBatch, error) {
	if path == "" {
		return bankSnapshotImportBatch{}, errBankSnapshotManifestPathRequired
	}
	manifestAbs, err := filepath.Abs(path)
	if err != nil {
		return bankSnapshotImportBatch{}, err
	}
	manifestRaw, err := readPrivateBoundedFile(manifestAbs, maxBankSnapshotManifestBytes, errBankSnapshotManifestNotPrivate, errBankSnapshotManifestNotRegular, errBankSnapshotManifestTooLarge)
	if err != nil {
		return bankSnapshotImportBatch{}, err
	}
	var manifest bankSnapshotImportManifest
	if err := decodeSingleJSON(manifestRaw, &manifest); err != nil {
		return bankSnapshotImportBatch{}, fmt.Errorf("%w: %v", errBankSnapshotManifestInvalid, err)
	}
	if manifest.Version != 1 || !validOpaqueImportID(manifest.WorldID) || len(manifest.WorldID) > 128 || len(manifest.Records) == 0 || len(manifest.Records) > maxBankSnapshotManifestRecords {
		return bankSnapshotImportBatch{}, fmt.Errorf("%w: version, world, or record count", errBankSnapshotManifestInvalid)
	}
	baseDir := filepath.Dir(manifestAbs)
	if _, err := privateDirectoryInfo(baseDir, errBankSnapshotManifestInvalid); err != nil {
		return bankSnapshotImportBatch{}, fmt.Errorf("%w: manifest directory: %v", errBankSnapshotManifestInvalid, err)
	}
	batch := bankSnapshotImportBatch{WorldID: manifest.WorldID, Requests: make([]storage.BankSnapshotImport, 0, len(manifest.Records))}
	seenCommands := make(map[string]struct{}, len(manifest.Records))
	seenPlayers := make(map[string]struct{}, len(manifest.Records))
	seenAccounts := make(map[string]struct{}, len(manifest.Records))
	seenDigests := make(map[[sha256.Size]byte]struct{}, len(manifest.Records))
	seenItemIDs := make(map[string]struct{})
	var totalBytes int64
	for index, record := range manifest.Records {
		request, digest, err := readBankSnapshotImportRecord(baseDir, manifest.WorldID, record)
		if err != nil {
			return bankSnapshotImportBatch{}, fmt.Errorf("%w: record %d: %v", errBankSnapshotManifestInvalid, index+1, err)
		}
		if _, exists := seenCommands[request.CommandID]; exists {
			return bankSnapshotImportBatch{}, fmt.Errorf("%w: duplicate command_id", errBankSnapshotManifestInvalid)
		}
		if _, exists := seenPlayers[request.PlayerID]; exists {
			return bankSnapshotImportBatch{}, fmt.Errorf("%w: duplicate player_id", errBankSnapshotManifestInvalid)
		}
		if _, exists := seenAccounts[request.AccountName]; exists {
			return bankSnapshotImportBatch{}, fmt.Errorf("%w: duplicate account_name", errBankSnapshotManifestInvalid)
		}
		if _, exists := seenDigests[digest]; exists {
			return bankSnapshotImportBatch{}, fmt.Errorf("%w: duplicate snapshot_sha256", errBankSnapshotManifestInvalid)
		}
		seenCommands[request.CommandID] = struct{}{}
		seenPlayers[request.PlayerID] = struct{}{}
		seenAccounts[request.AccountName] = struct{}{}
		seenDigests[digest] = struct{}{}
		for _, itemID := range request.ItemIDs {
			if _, exists := seenItemIDs[itemID]; exists {
				return bankSnapshotImportBatch{}, fmt.Errorf("%w: duplicate item_id", errBankSnapshotManifestInvalid)
			}
			seenItemIDs[itemID] = struct{}{}
		}
		totalBytes += int64(len(request.Snapshot))
		if totalBytes > maxBankSnapshotManifestPayloadBytes {
			return bankSnapshotImportBatch{}, fmt.Errorf("%w: aggregate snapshot bytes", errBankSnapshotManifestInvalid)
		}
		batch.Requests = append(batch.Requests, request)
	}
	return batch, nil
}

func readBankSnapshotImportRecord(baseDir, worldID string, record bankSnapshotImportManifestRecord) (storage.BankSnapshotImport, [sha256.Size]byte, error) {
	var zero [sha256.Size]byte
	if !validOpaqueImportID(record.CommandID) || len(record.CommandID) > 128 {
		return storage.BankSnapshotImport{}, zero, errors.New("invalid command_id")
	}
	if record.ExpectedRevision == nil || *record.ExpectedRevision < 0 {
		return storage.BankSnapshotImport{}, zero, errors.New("expected_revision must be non-negative")
	}
	canonicalName, err := identity.CanonicalName(record.AccountName)
	if err != nil || canonicalName != record.AccountName {
		return storage.BankSnapshotImport{}, zero, errors.New("account_name must be canonical")
	}
	if !validOpaqueImportID(record.PlayerID) || len(record.PlayerID) > 128 {
		return storage.BankSnapshotImport{}, zero, errors.New("invalid player_id")
	}
	if !validRelativeSnapshotPath(record.SnapshotFile) {
		return storage.BankSnapshotImport{}, zero, errors.New("snapshot_file must be a relative private path")
	}
	snapshotPath := filepath.Join(baseDir, filepath.FromSlash(record.SnapshotFile))
	if filepath.Clean(snapshotPath) == filepath.Clean(baseDir) {
		return storage.BankSnapshotImport{}, zero, errors.New("snapshot_file must name a file")
	}
	snapshotRaw, err := readPrivateBoundedFile(snapshotPath, maxBankSnapshotManifestFileBytes, errBankSnapshotManifestFileNotPrivate, errBankSnapshotManifestFileNotRegular, errBankSnapshotManifestFileTooLarge)
	if err != nil {
		return storage.BankSnapshotImport{}, zero, fmt.Errorf("snapshot_file: %v", err)
	}
	digest, err := parseSHA256Hex(record.SnapshotSHA256)
	if err != nil {
		return storage.BankSnapshotImport{}, zero, errors.New("snapshot_sha256 must be lowercase hexadecimal SHA-256")
	}
	actualDigest := sha256.Sum256(snapshotRaw)
	if actualDigest != digest {
		return storage.BankSnapshotImport{}, zero, errors.New("snapshot_sha256 mismatch")
	}
	inspection, err := world.InspectBankSnapshotV1(snapshotRaw)
	if err != nil {
		return storage.BankSnapshotImport{}, zero, fmt.Errorf("snapshot validation: %v", err)
	}
	canonicalWire, err := world.EncodeBankSnapshotV1(inspection.Snapshot)
	if err != nil || !bytes.Equal(canonicalWire, snapshotRaw) {
		return storage.BankSnapshotImport{}, zero, errors.New("snapshot is not canonical")
	}
	itemCount := len(inspection.Snapshot.Root.Nodes) - 1
	if itemCount < 0 || len(record.ItemIDs) != itemCount {
		return storage.BankSnapshotImport{}, zero, errors.New("item_ids count does not match bank graph")
	}
	itemIDs := append([]string(nil), record.ItemIDs...)
	seenItems := make(map[string]struct{}, len(itemIDs))
	for _, itemID := range itemIDs {
		if !validOpaqueImportID(itemID) || len(itemID) > 128 {
			return storage.BankSnapshotImport{}, zero, errors.New("invalid item_id")
		}
		if _, exists := seenItems[itemID]; exists {
			return storage.BankSnapshotImport{}, zero, errors.New("duplicate item_id")
		}
		seenItems[itemID] = struct{}{}
	}
	return storage.BankSnapshotImport{
		WorldID: worldID, CommandID: record.CommandID, ExpectedRevision: *record.ExpectedRevision,
		AccountName: canonicalName, PlayerID: record.PlayerID, Snapshot: append([]byte(nil), snapshotRaw...), ItemIDs: itemIDs,
	}, digest, nil
}

// buildBankSnapshotImportManifest rechecks the converter's review, canonical
// artifact bytes, source/player-name evidence, and operator mapping before
// emitting a normal import manifest. It never opens PostgreSQL.
func buildBankSnapshotImportManifest(reviewPath, mappingPath string) (bankSnapshotImportBatch, []byte, error) {
	if reviewPath == "" || mappingPath == "" {
		return bankSnapshotImportBatch{}, nil, errBankSnapshotManifestBuildInvalid
	}
	reviewAbs, err := filepath.Abs(reviewPath)
	if err != nil {
		return bankSnapshotImportBatch{}, nil, err
	}
	mappingAbs, err := filepath.Abs(mappingPath)
	if err != nil {
		return bankSnapshotImportBatch{}, nil, err
	}
	reviewDir := filepath.Dir(reviewAbs)
	if filepath.Dir(mappingAbs) != reviewDir {
		return bankSnapshotImportBatch{}, nil, errBankSnapshotManifestBuildOutputInvalid
	}
	if _, err := privateDirectoryInfo(reviewDir, errBankSnapshotManifestBuildInvalid); err != nil {
		return bankSnapshotImportBatch{}, nil, fmt.Errorf("%w: review directory: %v", errBankSnapshotManifestBuildInvalid, err)
	}
	reviewRaw, err := readPrivateBoundedFile(reviewAbs, maxBankSnapshotManifestBytes, errBankSnapshotManifestBuildInvalid, errBankSnapshotManifestBuildInvalid, errBankSnapshotManifestBuildInvalid)
	if err != nil {
		return bankSnapshotImportBatch{}, nil, fmt.Errorf("%w: review: %v", errBankSnapshotManifestBuildInvalid, err)
	}
	var review bankRawConversionReview
	if err := decodeSingleJSON(reviewRaw, &review); err != nil {
		return bankSnapshotImportBatch{}, nil, fmt.Errorf("%w: review JSON: %v", errBankSnapshotManifestBuildInvalid, err)
	}
	if err := validateBankRawConversionReview(review); err != nil {
		return bankSnapshotImportBatch{}, nil, fmt.Errorf("%w: review contract: %v", errBankSnapshotManifestBuildInvalid, err)
	}
	mappingRaw, err := readPrivateBoundedFile(mappingAbs, maxBankSnapshotManifestBytes, errBankSnapshotManifestBuildInvalid, errBankSnapshotManifestBuildInvalid, errBankSnapshotManifestBuildInvalid)
	if err != nil {
		return bankSnapshotImportBatch{}, nil, fmt.Errorf("%w: mapping: %v", errBankSnapshotManifestBuildInvalid, err)
	}
	var mapping bankSnapshotIdentityMapping
	if err := decodeSingleJSON(mappingRaw, &mapping); err != nil {
		return bankSnapshotImportBatch{}, nil, fmt.Errorf("%w: mapping JSON: %v", errBankSnapshotManifestBuildInvalid, err)
	}
	if mapping.Version != 1 || !validOpaqueImportID(mapping.WorldID) || len(mapping.WorldID) > 128 || len(mapping.Records) != len(review.Records) {
		return bankSnapshotImportBatch{}, nil, fmt.Errorf("%w: mapping version/world/record count", errBankSnapshotManifestBuildInvalid)
	}
	bySnapshot := make(map[string]bankSnapshotIdentityMappingRecord, len(mapping.Records))
	seenCommands := make(map[string]struct{}, len(mapping.Records))
	seenPlayers := make(map[string]struct{}, len(mapping.Records))
	seenAccounts := make(map[string]struct{}, len(mapping.Records))
	seenItemIDs := make(map[string]struct{})
	for index, record := range mapping.Records {
		if !validRelativeSnapshotPath(record.SnapshotFile) {
			return bankSnapshotImportBatch{}, nil, fmt.Errorf("%w: mapping record %d snapshot_file", errBankSnapshotManifestBuildInvalid, index+1)
		}
		if _, exists := bySnapshot[record.SnapshotFile]; exists {
			return bankSnapshotImportBatch{}, nil, fmt.Errorf("%w: duplicate snapshot_file", errBankSnapshotManifestBuildInvalid)
		}
		if !validOpaqueImportID(record.CommandID) || len(record.CommandID) > 128 || record.ExpectedRevision == nil || *record.ExpectedRevision < 0 {
			return bankSnapshotImportBatch{}, nil, fmt.Errorf("%w: mapping record %d command/revision", errBankSnapshotManifestBuildInvalid, index+1)
		}
		if !validOpaqueImportID(record.PlayerID) || len(record.PlayerID) > 128 {
			return bankSnapshotImportBatch{}, nil, fmt.Errorf("%w: mapping record %d player_id", errBankSnapshotManifestBuildInvalid, index+1)
		}
		canonicalName, nameErr := identity.CanonicalName(record.AccountName)
		if nameErr != nil || canonicalName != record.AccountName {
			return bankSnapshotImportBatch{}, nil, fmt.Errorf("%w: mapping record %d account_name", errBankSnapshotManifestBuildInvalid, index+1)
		}
		if _, exists := seenCommands[record.CommandID]; exists {
			return bankSnapshotImportBatch{}, nil, fmt.Errorf("%w: duplicate command_id", errBankSnapshotManifestBuildInvalid)
		}
		if _, exists := seenPlayers[record.PlayerID]; exists {
			return bankSnapshotImportBatch{}, nil, fmt.Errorf("%w: duplicate player_id", errBankSnapshotManifestBuildInvalid)
		}
		if _, exists := seenAccounts[canonicalName]; exists {
			return bankSnapshotImportBatch{}, nil, fmt.Errorf("%w: duplicate account_name", errBankSnapshotManifestBuildInvalid)
		}
		for _, itemID := range record.ItemIDs {
			if !validOpaqueImportID(itemID) || len(itemID) > 128 {
				return bankSnapshotImportBatch{}, nil, fmt.Errorf("%w: mapping record %d item_id", errBankSnapshotManifestBuildInvalid, index+1)
			}
			if _, exists := seenItemIDs[itemID]; exists {
				return bankSnapshotImportBatch{}, nil, fmt.Errorf("%w: duplicate item_id", errBankSnapshotManifestBuildInvalid)
			}
			seenItemIDs[itemID] = struct{}{}
		}
		seenCommands[record.CommandID] = struct{}{}
		seenPlayers[record.PlayerID] = struct{}{}
		seenAccounts[canonicalName] = struct{}{}
		record.AccountName = canonicalName
		record.ItemIDs = append([]string(nil), record.ItemIDs...)
		bySnapshot[record.SnapshotFile] = record
	}

	manifest := bankSnapshotImportManifest{Version: 1, WorldID: mapping.WorldID, Records: make([]bankSnapshotImportManifestRecord, 0, len(review.Records))}
	batch := bankSnapshotImportBatch{WorldID: mapping.WorldID, Requests: make([]storage.BankSnapshotImport, 0, len(review.Records))}
	seenDigests := make(map[[sha256.Size]byte]struct{}, len(review.Records))
	var totalBytes int64
	for index, reviewRecord := range review.Records {
		if !validRelativeSnapshotPath(reviewRecord.CanonicalFile) || filepath.Base(reviewRecord.CanonicalFile) != reviewRecord.CanonicalFile {
			return bankSnapshotImportBatch{}, nil, fmt.Errorf("%w: review record %d canonical_file", errBankSnapshotManifestBuildInvalid, index+1)
		}
		mappingRecord, ok := bySnapshot[reviewRecord.CanonicalFile]
		if !ok {
			return bankSnapshotImportBatch{}, nil, fmt.Errorf("%w: mapping missing %s", errBankSnapshotManifestBuildInvalid, reviewRecord.CanonicalFile)
		}
		canonicalPath := filepath.Join(reviewDir, filepath.FromSlash(reviewRecord.CanonicalFile))
		snapshotRaw, readErr := readPrivateBoundedFile(canonicalPath, maxBankSnapshotManifestFileBytes, errBankSnapshotManifestBuildInvalid, errBankSnapshotManifestBuildInvalid, errBankSnapshotManifestBuildInvalid)
		if readErr != nil {
			return bankSnapshotImportBatch{}, nil, fmt.Errorf("%w: canonical %s: %v", errBankSnapshotManifestBuildInvalid, reviewRecord.CanonicalFile, readErr)
		}
		canonicalDigest := sha256.Sum256(snapshotRaw)
		reviewDigest, digestErr := parseSHA256Hex(reviewRecord.CanonicalSHA256)
		if digestErr != nil || reviewDigest != canonicalDigest || reviewRecord.CanonicalOctets != int64(len(snapshotRaw)) {
			return bankSnapshotImportBatch{}, nil, fmt.Errorf("%w: canonical digest/size %s", errBankSnapshotManifestBuildInvalid, reviewRecord.CanonicalFile)
		}
		if _, exists := seenDigests[canonicalDigest]; exists {
			return bankSnapshotImportBatch{}, nil, fmt.Errorf("%w: duplicate canonical digest", errBankSnapshotManifestBuildInvalid)
		}
		seenDigests[canonicalDigest] = struct{}{}
		inspection, inspectErr := world.InspectBankSnapshotV1(snapshotRaw)
		if inspectErr != nil {
			return bankSnapshotImportBatch{}, nil, fmt.Errorf("%w: canonical validation %s: %v", errBankSnapshotManifestBuildInvalid, reviewRecord.CanonicalFile, inspectErr)
		}
		canonicalWire, encodeErr := world.EncodeBankSnapshotV1(inspection.Snapshot)
		if encodeErr != nil || !bytes.Equal(canonicalWire, snapshotRaw) {
			return bankSnapshotImportBatch{}, nil, fmt.Errorf("%w: non-canonical artifact %s", errBankSnapshotManifestBuildInvalid, reviewRecord.CanonicalFile)
		}
		if reviewRecord.RootCount != 1 || reviewRecord.NodeCount != len(inspection.Snapshot.Root.Nodes) || len(mappingRecord.ItemIDs) != len(inspection.Snapshot.Root.Nodes)-1 {
			return bankSnapshotImportBatch{}, nil, fmt.Errorf("%w: graph count %s", errBankSnapshotManifestBuildInvalid, reviewRecord.CanonicalFile)
		}
		canonicalSourceName, nameErr := identity.CanonicalName(reviewRecord.SourcePlayerName)
		if nameErr != nil || canonicalSourceName != mappingRecord.AccountName {
			return bankSnapshotImportBatch{}, nil, fmt.Errorf("%w: account does not match reviewed source %s", errBankSnapshotManifestBuildInvalid, reviewRecord.CanonicalFile)
		}
		// The raw source digest remains review evidence. The import contract
		// hashes the canonical artifact that is actually attached to the world.
		expectedRevision := *mappingRecord.ExpectedRevision
		manifest.Records = append(manifest.Records, bankSnapshotImportManifestRecord{
			CommandID: mappingRecord.CommandID, ExpectedRevision: &expectedRevision,
			AccountName: mappingRecord.AccountName, PlayerID: mappingRecord.PlayerID,
			SnapshotFile: reviewRecord.CanonicalFile, SnapshotSHA256: fmt.Sprintf("%x", canonicalDigest),
			ItemIDs: append([]string(nil), mappingRecord.ItemIDs...),
		})
		batch.Requests = append(batch.Requests, storage.BankSnapshotImport{
			WorldID: mapping.WorldID, CommandID: mappingRecord.CommandID, ExpectedRevision: expectedRevision,
			AccountName: mappingRecord.AccountName, PlayerID: mappingRecord.PlayerID,
			Snapshot: append([]byte(nil), snapshotRaw...), ItemIDs: append([]string(nil), mappingRecord.ItemIDs...),
		})
		totalBytes += int64(len(snapshotRaw))
		if totalBytes > maxBankSnapshotManifestPayloadBytes {
			return bankSnapshotImportBatch{}, nil, fmt.Errorf("%w: aggregate snapshot bytes", errBankSnapshotManifestBuildInvalid)
		}
	}
	if len(bySnapshot) != len(manifest.Records) {
		return bankSnapshotImportBatch{}, nil, fmt.Errorf("%w: mapping has extra snapshot", errBankSnapshotManifestBuildInvalid)
	}
	manifestRaw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return bankSnapshotImportBatch{}, nil, fmt.Errorf("%w: manifest JSON: %v", errBankSnapshotManifestBuildInvalid, err)
	}
	manifestRaw = append(manifestRaw, '\n')
	if int64(len(manifestRaw)) > maxBankSnapshotManifestBytes {
		return bankSnapshotImportBatch{}, nil, fmt.Errorf("%w: manifest size", errBankSnapshotManifestBuildInvalid)
	}
	return batch, manifestRaw, nil
}

func writeBankSnapshotManifestBuild(outputPath string, manifestRaw []byte, reviewPath string) error {
	if outputPath == "" {
		return errBankSnapshotManifestBuildOutputNeeded
	}
	outputAbs, err := filepath.Abs(outputPath)
	if err != nil {
		return errBankSnapshotManifestBuildOutputInvalid
	}
	reviewAbs, err := filepath.Abs(reviewPath)
	if err != nil {
		return errBankSnapshotManifestBuildOutputInvalid
	}
	if filepath.Dir(outputAbs) != filepath.Dir(reviewAbs) {
		return errBankSnapshotManifestBuildOutputInvalid
	}
	if filepath.Clean(outputAbs) == filepath.Clean(reviewAbs) {
		return errBankSnapshotManifestBuildOverlap
	}
	if _, err := privateDirectoryInfo(filepath.Dir(outputAbs), errBankSnapshotManifestBuildOutputInvalid); err != nil {
		return err
	}
	if info, err := os.Lstat(outputAbs); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			return errBankSnapshotManifestBuildOutputInvalid
		}
		stored, readErr := os.ReadFile(outputAbs)
		if readErr != nil {
			return readErr
		}
		if !bytes.Equal(stored, manifestRaw) {
			return errBankSnapshotManifestBuildConflict
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := writePrivateImmutableFile(outputAbs, manifestRaw); err != nil {
		return fmt.Errorf("%w: %v", errBankSnapshotManifestBuildOutputInvalid, err)
	}
	return nil
}

// runBankSnapshotImport is the only command-to-storage bridge for this
// manifest. Each request is already fully validated; receipts make an
// interrupted batch resumable with the same command IDs and revisions.
func runBankSnapshotImport(ctx context.Context, repo *storage.Postgres, batch bankSnapshotImportBatch) error {
	if ctx == nil || repo == nil || len(batch.Requests) == 0 {
		return errBankSnapshotManifestInvalid
	}
	for _, request := range batch.Requests {
		result, err := repo.ImportBankSnapshot(ctx, request)
		if err != nil {
			return fmt.Errorf("bank snapshot import command %q: %w", request.CommandID, err)
		}
		mode := "imported"
		if result.Replayed {
			mode = "replayed"
		}
		fmt.Printf("bank snapshot %s: player=%s balance=%d items=%d source_sha256=%x canonical_sha256=%x\n", mode, result.PlayerID, result.Balance, result.ItemCount, result.SourceSHA256, result.CanonicalSHA256)
	}
	return nil
}
