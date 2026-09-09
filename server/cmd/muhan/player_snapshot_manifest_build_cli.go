package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/1XP-Inc/muhan-mud/server/internal/identity"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

const maxPlayerSnapshotManifestMappingBytes int64 = maxPlayerSnapshotManifestBytes

var (
	errPlayerSnapshotManifestBuildReviewRequired  = errors.New("player snapshot review path is required")
	errPlayerSnapshotManifestBuildMappingRequired = errors.New("player snapshot identity mapping path is required")
	errPlayerSnapshotManifestBuildOutputRequired  = errors.New("player snapshot manifest output path is required")
	errPlayerSnapshotManifestBuildInvalid         = errors.New("invalid player snapshot manifest build input")
	errPlayerSnapshotManifestBuildOutputInvalid   = errors.New("player snapshot manifest output must be a private 0600 file beside the review")
)

type playerSnapshotManifestBuildOptions struct {
	ReviewPath  string
	MappingPath string
	OutputPath  string
	DryRun      bool
}

// playerSnapshotIdentityMapping is supplied by an operator after reviewing
// player-snapshot-review.json. It contains no plaintext password field; the
// credential is already a bcrypt hash produced by a separate trusted step.
type playerSnapshotIdentityMapping struct {
	Version int                                   `json:"version"`
	WorldID string                                `json:"world_id"`
	Records []playerSnapshotIdentityMappingRecord `json:"records"`
}

type playerSnapshotIdentityMappingRecord struct {
	SnapshotFile      string   `json:"snapshot_file"`
	CommandID         string   `json:"command_id"`
	ExpectedRevision  *int64   `json:"expected_revision"`
	AccountName       string   `json:"account_name"`
	CredentialHashB64 string   `json:"credential_hash_b64"`
	PlayerID          string   `json:"player_id"`
	ItemIDs           []string `json:"item_ids"`
}

func validatePlayerSnapshotManifestBuildFlags(reviewPath, mappingPath, outputPath string, dryRun bool) (playerSnapshotManifestBuildOptions, error) {
	if reviewPath == "" {
		if mappingPath != "" || outputPath != "" || dryRun {
			return playerSnapshotManifestBuildOptions{}, errPlayerSnapshotManifestBuildReviewRequired
		}
		return playerSnapshotManifestBuildOptions{}, nil
	}
	if mappingPath == "" {
		return playerSnapshotManifestBuildOptions{}, errPlayerSnapshotManifestBuildMappingRequired
	}
	if !dryRun && outputPath == "" {
		return playerSnapshotManifestBuildOptions{}, errPlayerSnapshotManifestBuildOutputRequired
	}
	return playerSnapshotManifestBuildOptions{ReviewPath: reviewPath, MappingPath: mappingPath, OutputPath: outputPath, DryRun: dryRun}, nil
}

// buildPlayerSnapshotImportManifest verifies the generated raw review, every
// canonical CDTO file, and an explicit operator mapping before returning a
// normal import batch. It never reads a raw password field or talks to DB.
func buildPlayerSnapshotImportManifest(reviewPath, mappingPath string) (playerSnapshotImportBatch, []byte, error) {
	if reviewPath == "" || mappingPath == "" {
		return playerSnapshotImportBatch{}, nil, errPlayerSnapshotManifestBuildInvalid
	}
	reviewRaw, err := readPrivateBoundedFile(reviewPath, maxPlayerSnapshotManifestBytes, errPlayerSnapshotManifestNotPrivate, errPlayerSnapshotManifestNotRegular, errPlayerSnapshotManifestTooLarge)
	if err != nil {
		return playerSnapshotImportBatch{}, nil, err
	}
	var review playerSnapshotRawReview
	if err := decodeSingleJSON(reviewRaw, &review); err != nil {
		return playerSnapshotImportBatch{}, nil, fmt.Errorf("%w: review JSON: %v", errPlayerSnapshotManifestBuildInvalid, err)
	}
	if review.Version != 1 || !review.RequiresIdentityReview || review.WorldID == "" || len(review.WorldID) > 128 || !validOpaqueImportID(review.WorldID) ||
		review.RawABI != world.LegacyPlayerSnapshotRawV1ABI || review.ParserVersion != world.LegacyPlayerSnapshotRawV1ParserVersion ||
		len(review.Records) == 0 || len(review.Records) > maxPlayerSnapshotManifestRecords {
		return playerSnapshotImportBatch{}, nil, fmt.Errorf("%w: review contract", errPlayerSnapshotManifestBuildInvalid)
	}
	mappingRaw, err := readPrivateBoundedFile(mappingPath, maxPlayerSnapshotManifestMappingBytes, errPlayerSnapshotManifestNotPrivate, errPlayerSnapshotManifestNotRegular, errPlayerSnapshotManifestTooLarge)
	if err != nil {
		return playerSnapshotImportBatch{}, nil, err
	}
	var mapping playerSnapshotIdentityMapping
	if err := decodeSingleJSON(mappingRaw, &mapping); err != nil {
		return playerSnapshotImportBatch{}, nil, fmt.Errorf("%w: mapping JSON: %v", errPlayerSnapshotManifestBuildInvalid, err)
	}
	if mapping.Version != 1 || mapping.WorldID != review.WorldID || len(mapping.Records) != len(review.Records) {
		return playerSnapshotImportBatch{}, nil, fmt.Errorf("%w: mapping version/world/record count", errPlayerSnapshotManifestBuildInvalid)
	}
	reviewAbs, err := filepath.Abs(reviewPath)
	if err != nil {
		return playerSnapshotImportBatch{}, nil, err
	}
	reviewDir := filepath.Dir(reviewAbs)
	if _, err := privateDirectoryInfo(reviewDir, errPlayerSnapshotManifestBuildInvalid); err != nil {
		return playerSnapshotImportBatch{}, nil, fmt.Errorf("%w: review directory: %v", errPlayerSnapshotManifestBuildInvalid, err)
	}
	mappingAbs, err := filepath.Abs(mappingPath)
	if err != nil {
		return playerSnapshotImportBatch{}, nil, err
	}
	if _, err := privateDirectoryInfo(filepath.Dir(mappingAbs), errPlayerSnapshotManifestBuildInvalid); err != nil {
		return playerSnapshotImportBatch{}, nil, fmt.Errorf("%w: mapping directory: %v", errPlayerSnapshotManifestBuildInvalid, err)
	}
	seenReviewFiles := make(map[string]struct{}, len(review.Records))
	seenMappingFiles := make(map[string]struct{}, len(mapping.Records))
	seenCommands := make(map[string]struct{}, len(mapping.Records))
	seenPlayers := make(map[string]struct{}, len(mapping.Records))
	seenAccounts := make(map[string]struct{}, len(mapping.Records))
	seenItems := make(map[string]struct{})
	seenCanonicalDigests := make(map[[sha256.Size]byte]struct{}, len(review.Records))
	bySnapshot := make(map[string]playerSnapshotIdentityMappingRecord, len(mapping.Records))
	for index, record := range mapping.Records {
		if !validRelativeSnapshotPath(record.SnapshotFile) {
			return playerSnapshotImportBatch{}, nil, fmt.Errorf("%w: mapping record %d snapshot_file", errPlayerSnapshotManifestBuildInvalid, index+1)
		}
		if _, exists := seenMappingFiles[record.SnapshotFile]; exists {
			return playerSnapshotImportBatch{}, nil, fmt.Errorf("%w: duplicate mapping snapshot_file", errPlayerSnapshotManifestBuildInvalid)
		}
		seenMappingFiles[record.SnapshotFile] = struct{}{}
		if !validOpaqueImportID(record.CommandID) || len(record.CommandID) > 128 || record.ExpectedRevision == nil || *record.ExpectedRevision < 0 ||
			!validOpaqueImportID(record.PlayerID) || len(record.PlayerID) > 128 {
			return playerSnapshotImportBatch{}, nil, fmt.Errorf("%w: mapping record %d identity", errPlayerSnapshotManifestBuildInvalid, index+1)
		}
		canonicalAccount, accountErr := identity.CanonicalName(record.AccountName)
		if accountErr != nil {
			return playerSnapshotImportBatch{}, nil, fmt.Errorf("%w: mapping record %d account", errPlayerSnapshotManifestBuildInvalid, index+1)
		}
		if _, hashErr := decodeBcryptHash(record.CredentialHashB64); hashErr != nil {
			return playerSnapshotImportBatch{}, nil, fmt.Errorf("%w: mapping record %d credential hash", errPlayerSnapshotManifestBuildInvalid, index+1)
		}
		if _, exists := seenCommands[record.CommandID]; exists {
			return playerSnapshotImportBatch{}, nil, fmt.Errorf("%w: duplicate command_id", errPlayerSnapshotManifestBuildInvalid)
		}
		if _, exists := seenPlayers[record.PlayerID]; exists {
			return playerSnapshotImportBatch{}, nil, fmt.Errorf("%w: duplicate player_id", errPlayerSnapshotManifestBuildInvalid)
		}
		if _, exists := seenAccounts[canonicalAccount]; exists {
			return playerSnapshotImportBatch{}, nil, fmt.Errorf("%w: duplicate account_name", errPlayerSnapshotManifestBuildInvalid)
		}
		seenCommands[record.CommandID] = struct{}{}
		seenPlayers[record.PlayerID] = struct{}{}
		seenAccounts[canonicalAccount] = struct{}{}
		itemIDs := append([]string(nil), record.ItemIDs...)
		for _, itemID := range itemIDs {
			if !validOpaqueImportID(itemID) || len(itemID) > 128 {
				return playerSnapshotImportBatch{}, nil, fmt.Errorf("%w: mapping record %d item ID", errPlayerSnapshotManifestBuildInvalid, index+1)
			}
			if _, exists := seenItems[itemID]; exists {
				return playerSnapshotImportBatch{}, nil, fmt.Errorf("%w: duplicate item ID", errPlayerSnapshotManifestBuildInvalid)
			}
			seenItems[itemID] = struct{}{}
		}
		record.AccountName = canonicalAccount
		record.ItemIDs = itemIDs
		bySnapshot[record.SnapshotFile] = record
	}
	manifest := playerSnapshotImportManifest{Version: 1, WorldID: review.WorldID, Records: make([]playerSnapshotImportManifestRecord, 0, len(review.Records))}
	batch := playerSnapshotImportBatch{WorldID: review.WorldID, Requests: make([]storage.PlayerSnapshotImport, 0, len(review.Records))}
	var totalSnapshotBytes int64
	for index, reviewRecord := range review.Records {
		if !validRelativeSnapshotPath(reviewRecord.SnapshotFile) || !validRelativeSnapshotPath(reviewRecord.SourceFile) {
			return playerSnapshotImportBatch{}, nil, fmt.Errorf("%w: review record %d path", errPlayerSnapshotManifestBuildInvalid, index+1)
		}
		if _, sourceHashErr := parseSHA256Hex(reviewRecord.SourceSHA256); sourceHashErr != nil {
			return playerSnapshotImportBatch{}, nil, fmt.Errorf("%w: review record %d source hash", errPlayerSnapshotManifestBuildInvalid, index+1)
		}
		if _, exists := seenReviewFiles[reviewRecord.SnapshotFile]; exists {
			return playerSnapshotImportBatch{}, nil, fmt.Errorf("%w: duplicate review snapshot_file", errPlayerSnapshotManifestBuildInvalid)
		}
		seenReviewFiles[reviewRecord.SnapshotFile] = struct{}{}
		mappingRecord, exists := bySnapshot[reviewRecord.SnapshotFile]
		if !exists {
			return playerSnapshotImportBatch{}, nil, fmt.Errorf("%w: mapping missing snapshot %s", errPlayerSnapshotManifestBuildInvalid, reviewRecord.SnapshotFile)
		}
		canonicalPath := filepath.Join(reviewDir, filepath.FromSlash(reviewRecord.SnapshotFile))
		snapshotRaw, readErr := readPrivateBoundedFile(canonicalPath, maxPlayerSnapshotFileBytes, errPlayerSnapshotFileNotPrivate, errPlayerSnapshotFileNotRegular, errPlayerSnapshotFileTooLarge)
		if readErr != nil {
			return playerSnapshotImportBatch{}, nil, fmt.Errorf("%w: CDTO %s: %v", errPlayerSnapshotManifestBuildInvalid, reviewRecord.SnapshotFile, readErr)
		}
		canonicalDigest := sha256.Sum256(snapshotRaw)
		reviewDigest, digestErr := parseSHA256Hex(reviewRecord.CanonicalSHA256)
		if digestErr != nil || reviewDigest != canonicalDigest {
			return playerSnapshotImportBatch{}, nil, fmt.Errorf("%w: CDTO hash %s", errPlayerSnapshotManifestBuildInvalid, reviewRecord.SnapshotFile)
		}
		if _, exists := seenCanonicalDigests[canonicalDigest]; exists {
			return playerSnapshotImportBatch{}, nil, fmt.Errorf("%w: duplicate CDTO hash", errPlayerSnapshotManifestBuildInvalid)
		}
		seenCanonicalDigests[canonicalDigest] = struct{}{}
		totalSnapshotBytes += int64(len(snapshotRaw))
		if totalSnapshotBytes > maxPlayerSnapshotManifestPayloadBytes {
			return playerSnapshotImportBatch{}, nil, fmt.Errorf("%w: aggregate snapshot bytes", errPlayerSnapshotManifestBuildInvalid)
		}
		if reviewRecord.SourceOctets <= 0 || reviewRecord.SourceOctets > maxPlayerSnapshotFileBytes || reviewRecord.InventoryNodeCount < 0 {
			return playerSnapshotImportBatch{}, nil, fmt.Errorf("%w: review metadata %s", errPlayerSnapshotManifestBuildInvalid, reviewRecord.SnapshotFile)
		}
		inspection, inspectErr := world.InspectPlayerSnapshotV1(snapshotRaw)
		if inspectErr != nil {
			return playerSnapshotImportBatch{}, nil, fmt.Errorf("%w: CDTO validation %s: %v", errPlayerSnapshotManifestBuildInvalid, reviewRecord.SnapshotFile, inspectErr)
		}
		canonicalWire, encodeErr := world.EncodePlayerSnapshotV1(inspection.Snapshot)
		if encodeErr != nil || !bytes.Equal(canonicalWire, snapshotRaw) {
			return playerSnapshotImportBatch{}, nil, fmt.Errorf("%w: non-canonical CDTO %s", errPlayerSnapshotManifestBuildInvalid, reviewRecord.SnapshotFile)
		}
		if reviewRecord.InventoryNodeCount != len(inspection.Snapshot.Inventory.Nodes) || len(mappingRecord.ItemIDs) != len(inspection.Snapshot.Inventory.Nodes) {
			return playerSnapshotImportBatch{}, nil, fmt.Errorf("%w: graph count %s", errPlayerSnapshotManifestBuildInvalid, reviewRecord.SnapshotFile)
		}
		snapshotName := strings.TrimRight(string(inspection.Snapshot.Name[:]), "\x00")
		canonicalSnapshotName, nameErr := identity.CanonicalName(snapshotName)
		if nameErr != nil || canonicalSnapshotName != mappingRecord.AccountName {
			return playerSnapshotImportBatch{}, nil, fmt.Errorf("%w: account does not match CDTO %s", errPlayerSnapshotManifestBuildInvalid, reviewRecord.SnapshotFile)
		}
		expectedRevision := *mappingRecord.ExpectedRevision
		manifest.Records = append(manifest.Records, playerSnapshotImportManifestRecord{
			CommandID: mappingRecord.CommandID, ExpectedRevision: &expectedRevision,
			AccountName: mappingRecord.AccountName, CredentialHashB64: mappingRecord.CredentialHashB64,
			PlayerID: mappingRecord.PlayerID, SnapshotFile: reviewRecord.SnapshotFile,
			SourceSHA256: fmt.Sprintf("%x", canonicalDigest), ItemIDs: append([]string(nil), mappingRecord.ItemIDs...),
		})
		batch.Requests = append(batch.Requests, storage.PlayerSnapshotImport{
			WorldID: review.WorldID, CommandID: mappingRecord.CommandID, ExpectedRevision: expectedRevision,
			AccountName: mappingRecord.AccountName, CredentialHash: append([]byte(nil), mustDecodeBcrypt(mappingRecord.CredentialHashB64)...),
			PlayerID: mappingRecord.PlayerID, Snapshot: append([]byte(nil), snapshotRaw...), ItemIDs: append([]string(nil), mappingRecord.ItemIDs...),
		})
	}
	if len(seenReviewFiles) != len(seenMappingFiles) {
		return playerSnapshotImportBatch{}, nil, fmt.Errorf("%w: mapping has extra snapshot", errPlayerSnapshotManifestBuildInvalid)
	}
	manifestRaw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return playerSnapshotImportBatch{}, nil, err
	}
	manifestRaw = append(manifestRaw, '\n')
	if int64(len(manifestRaw)) > maxPlayerSnapshotManifestBytes {
		return playerSnapshotImportBatch{}, nil, fmt.Errorf("%w: import manifest size", errPlayerSnapshotManifestBuildInvalid)
	}
	return batch, manifestRaw, nil
}

func decodeSingleJSON(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("trailing JSON values")
		}
		return err
	}
	return nil
}

func validRelativeSnapshotPath(value string) bool {
	if !validInspectionSourcePath(value) || strings.Contains(value, "\\") || filepath.IsAbs(value) {
		return false
	}
	clean := filepath.Clean(filepath.FromSlash(value))
	return clean != "." && clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}

func mustDecodeBcrypt(value string) []byte {
	raw, _ := decodeBcryptHash(value)
	return raw
}

func decodeBcryptHash(value string) ([]byte, error) {
	raw, err := base64.StdEncoding.Strict().DecodeString(value)
	if err != nil || !validBcryptHash(raw) {
		return nil, errors.New("unsupported bcrypt hash")
	}
	return raw, nil
}

func writePlayerSnapshotManifestBuild(outputPath string, manifestRaw []byte, reviewPath string) error {
	if outputPath == "" {
		return errPlayerSnapshotManifestBuildOutputRequired
	}
	reviewAbs, err := filepath.Abs(reviewPath)
	if err != nil {
		return err
	}
	outputAbs, err := filepath.Abs(outputPath)
	if err != nil {
		return err
	}
	if filepath.Dir(reviewAbs) != filepath.Dir(outputAbs) {
		return errPlayerSnapshotManifestBuildOutputInvalid
	}
	if _, err := privateDirectoryInfo(filepath.Dir(outputAbs), errPlayerSnapshotManifestBuildOutputInvalid); err != nil {
		return err
	}
	info, err := os.Lstat(outputAbs)
	if err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600) {
		return errPlayerSnapshotManifestBuildOutputInvalid
	}
	return writePrivateImmutableFile(outputAbs, manifestRaw)
}
