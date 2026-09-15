package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/1XP-Inc/muhan-mud/server/internal/identity"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

const (
	maxPlayerSnapshotRawConversionFiles       = maxPlayerSnapshotInspectionFiles
	maxPlayerSnapshotRawConversionBytes int64 = maxPlayerSnapshotManifestPayloadBytes
	playerSnapshotRawReviewFile               = "player-snapshot-review.json"
)

var (
	errPlayerSnapshotRawConversionSourceRequired = errors.New("raw player snapshot source directory is required")
	errPlayerSnapshotRawConversionWorldRequired  = errors.New("raw player snapshot world is required")
	errPlayerSnapshotRawConversionOutputRequired = errors.New("raw player snapshot output directory is required")
	errPlayerSnapshotRawConversionABIRequired    = errors.New("raw player snapshot ABI contract is required")
	errPlayerSnapshotRawConversionSourceInvalid  = errors.New("raw player snapshot source directory must be a private 0700 directory")
	errPlayerSnapshotRawConversionOutputInvalid  = errors.New("raw player snapshot output directory must be a private 0700 directory")
	errPlayerSnapshotRawConversionOutputOverlap  = errors.New("raw player snapshot source and output directories must be separate")
	errPlayerSnapshotRawConversionSourceFile     = errors.New("raw player snapshot source file must be a private 0600 regular file")
	errPlayerSnapshotRawConversionEmpty          = errors.New("raw player snapshot source directory is empty")
	errPlayerSnapshotRawConversionTooMany        = errors.New("raw player snapshot source directory has too many files")
	errPlayerSnapshotRawConversionTooLarge       = errors.New("raw player snapshot source exceeds the size limit")
	errPlayerSnapshotRawConversionNameConflict   = errors.New("raw player snapshot names are not unique")
	errPlayerSnapshotRawConversionOutputConflict = errors.New("raw player snapshot output already contains different bytes")
)

type playerSnapshotRawConversionOptions struct {
	SourceDir string
	WorldID   string
	OutputDir string
	ABI       string
	DryRun    bool
}

type playerSnapshotRawConversionBatch struct {
	WorldID string
	Records []playerSnapshotRawConversionRecord
}

type playerSnapshotRawConversionRecord struct {
	SourcePath           string
	SnapshotPath         string
	SuggestedAccountName string
	SourceSHA256         [sha256.Size]byte
	CanonicalSHA256      [sha256.Size]byte
	SourceOctets         int64
	InventoryNodeCount   int
	CanonicalSnapshot    []byte
}

// playerSnapshotRawReview is intentionally not playerSnapshotImportManifest.
// It cannot be consumed by the import command because it has no player ID,
// command ID, item IDs, or credential hash. Those values require a separate
// operator review and are never inferred from a raw file.
type playerSnapshotRawReview struct {
	Version                int                             `json:"version"`
	WorldID                string                          `json:"world_id"`
	RawABI                 string                          `json:"raw_abi"`
	ParserVersion          string                          `json:"parser_version"`
	RequiresIdentityReview bool                            `json:"requires_identity_review"`
	Records                []playerSnapshotRawReviewRecord `json:"records"`
}

type playerSnapshotRawReviewRecord struct {
	SourceFile           string `json:"source_file"`
	SnapshotFile         string `json:"snapshot_file"`
	SuggestedAccountName string `json:"suggested_account_name"`
	SourceSHA256         string `json:"source_sha256"`
	CanonicalSHA256      string `json:"canonical_sha256"`
	SourceOctets         int64  `json:"source_octets"`
	InventoryNodeCount   int    `json:"inventory_node_count"`
}

func validatePlayerSnapshotRawConversionFlags(sourceDir, worldID, outputDir, abi string, dryRun bool) (playerSnapshotRawConversionOptions, error) {
	if sourceDir == "" {
		if worldID != "" || outputDir != "" || abi != "" || dryRun {
			return playerSnapshotRawConversionOptions{}, errPlayerSnapshotRawConversionSourceRequired
		}
		return playerSnapshotRawConversionOptions{}, nil
	}
	if worldID == "" || len(worldID) > 128 || !validOpaqueImportID(worldID) {
		return playerSnapshotRawConversionOptions{}, errPlayerSnapshotRawConversionWorldRequired
	}
	if err := world.ValidateLegacyPlayerSnapshotRawV1ABI(abi); err != nil {
		if abi == "" {
			return playerSnapshotRawConversionOptions{}, errPlayerSnapshotRawConversionABIRequired
		}
		return playerSnapshotRawConversionOptions{}, err
	}
	if !dryRun && outputDir == "" {
		return playerSnapshotRawConversionOptions{}, errPlayerSnapshotRawConversionOutputRequired
	}
	return playerSnapshotRawConversionOptions{SourceDir: sourceDir, WorldID: worldID, OutputDir: outputDir, ABI: abi, DryRun: dryRun}, nil
}

func convertPlayerSnapshotRawDirectory(options playerSnapshotRawConversionOptions) (playerSnapshotRawConversionBatch, error) {
	if options.SourceDir == "" {
		return playerSnapshotRawConversionBatch{}, errPlayerSnapshotRawConversionSourceRequired
	}
	if err := world.ValidateLegacyPlayerSnapshotRawV1ABI(options.ABI); err != nil {
		return playerSnapshotRawConversionBatch{}, err
	}
	if len(options.WorldID) > 128 || !validOpaqueImportID(options.WorldID) {
		return playerSnapshotRawConversionBatch{}, errPlayerSnapshotRawConversionWorldRequired
	}
	if _, err := privateDirectoryInfo(options.SourceDir, errPlayerSnapshotRawConversionSourceInvalid); err != nil {
		return playerSnapshotRawConversionBatch{}, err
	}
	if options.OutputDir != "" {
		if _, err := privateDirectoryInfo(options.OutputDir, errPlayerSnapshotRawConversionOutputInvalid); err != nil {
			return playerSnapshotRawConversionBatch{}, err
		}
		if pathsOverlap(options.SourceDir, options.OutputDir) {
			return playerSnapshotRawConversionBatch{}, errPlayerSnapshotRawConversionOutputOverlap
		}
	}

	batch := playerSnapshotRawConversionBatch{WorldID: options.WorldID, Records: make([]playerSnapshotRawConversionRecord, 0)}
	seenNames := make(map[string]struct{})
	totalBytes := int64(0)
	err := filepath.WalkDir(options.SourceDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == options.SourceDir {
			return nil
		}
		relative, relErr := filepath.Rel(options.SourceDir, path)
		if relErr != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return errPlayerSnapshotRawConversionSourceInvalid
		}
		relative = filepath.ToSlash(relative)
		if !validInspectionSourcePath(relative) {
			return errPlayerSnapshotRawConversionSourceInvalid
		}
		if entry.IsDir() {
			info, infoErr := entry.Info()
			if infoErr != nil {
				return infoErr
			}
			if info.Mode().Perm() != 0o700 {
				return errPlayerSnapshotRawConversionSourceInvalid
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errPlayerSnapshotRawConversionSourceFile
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			return errPlayerSnapshotRawConversionSourceFile
		}
		if info.Size() <= 0 || info.Size() > maxPlayerSnapshotFileBytes {
			return errPlayerSnapshotRawConversionTooLarge
		}
		if len(batch.Records) >= maxPlayerSnapshotRawConversionFiles {
			return errPlayerSnapshotRawConversionTooMany
		}
		raw, stable, readErr := readStableSnapshotFile(path, info)
		if readErr != nil {
			return readErr
		}
		if !stable {
			return fmt.Errorf("%w: source file changed during conversion", errPlayerSnapshotRawConversionSourceFile)
		}
		totalBytes += int64(len(raw))
		if totalBytes > maxPlayerSnapshotRawConversionBytes {
			return errPlayerSnapshotRawConversionTooLarge
		}
		inspection, inspectErr := world.InspectLegacyPlayerSnapshotRawV1(raw)
		if inspectErr != nil {
			return fmt.Errorf("%s: %w", relative, inspectErr)
		}
		canonical, encodeErr := world.EncodePlayerSnapshotV1(inspection.Snapshot)
		if encodeErr != nil {
			return fmt.Errorf("%s: canonical snapshot: %w", relative, encodeErr)
		}
		name := strings.TrimRight(string(inspection.Snapshot.Name[:]), "\x00")
		canonicalName, nameErr := identity.CanonicalName(name)
		if nameErr != nil {
			return fmt.Errorf("%s: %w", relative, errPlayerSnapshotRawConversionNameConflict)
		}
		if _, exists := seenNames[canonicalName]; exists {
			return fmt.Errorf("%s: %w (%s)", relative, errPlayerSnapshotRawConversionNameConflict, canonicalName)
		}
		seenNames[canonicalName] = struct{}{}
		batch.Records = append(batch.Records, playerSnapshotRawConversionRecord{
			SourcePath: relative, SnapshotPath: relative + ".cdto", SuggestedAccountName: canonicalName,
			SourceSHA256: sha256.Sum256(raw), CanonicalSHA256: sha256.Sum256(canonical),
			SourceOctets: int64(len(raw)), InventoryNodeCount: len(inspection.Snapshot.Inventory.Nodes),
			CanonicalSnapshot: append([]byte(nil), canonical...),
		})
		return nil
	})
	if err != nil {
		return playerSnapshotRawConversionBatch{}, err
	}
	if len(batch.Records) == 0 {
		return playerSnapshotRawConversionBatch{}, errPlayerSnapshotRawConversionEmpty
	}
	return batch, nil
}

func privateDirectoryInfo(path string, invalid error) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm() != 0o700 {
		return nil, invalid
	}
	return info, nil
}

func pathsOverlap(first, second string) bool {
	firstAbs, firstErr := filepath.Abs(first)
	secondAbs, secondErr := filepath.Abs(second)
	if firstErr != nil || secondErr != nil {
		return true
	}
	return pathContains(firstAbs, secondAbs) || pathContains(secondAbs, firstAbs)
}

func pathContains(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	if err != nil {
		return true
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

func writePlayerSnapshotRawConversion(batch playerSnapshotRawConversionBatch, outputDir string) error {
	if outputDir == "" {
		return errPlayerSnapshotRawConversionOutputRequired
	}
	if _, err := privateDirectoryInfo(outputDir, errPlayerSnapshotRawConversionOutputInvalid); err != nil {
		return err
	}
	type outputFile struct {
		path     string
		contents []byte
	}
	outputs := make([]outputFile, 0, len(batch.Records)+1)
	for _, record := range batch.Records {
		path := filepath.Join(outputDir, filepath.FromSlash(record.SnapshotPath))
		outputs = append(outputs, outputFile{path: path, contents: record.CanonicalSnapshot})
	}
	reviewRaw, err := marshalPlayerSnapshotRawReview(batch)
	if err != nil {
		return err
	}
	outputs = append(outputs, outputFile{path: filepath.Join(outputDir, playerSnapshotRawReviewFile), contents: reviewRaw})
	// Check every destination before creating any output file. This keeps a
	// deterministic changed-output conflict from leaving an earlier record
	// partially published.
	for _, output := range outputs {
		if err := validatePrivateOutputParent(filepath.Dir(output.path), outputDir); err != nil {
			return err
		}
		if err := checkImmutableOutput(output.path, output.contents); err != nil {
			return err
		}
	}
	for _, output := range outputs {
		if err := ensurePrivateDirectory(filepath.Dir(output.path), outputDir); err != nil {
			return fmt.Errorf("prepare %s: %w", output.path, err)
		}
		if err := writePrivateImmutableFile(output.path, output.contents); err != nil {
			return fmt.Errorf("write %s: %w", output.path, err)
		}
	}
	return nil
}

func marshalPlayerSnapshotRawReview(batch playerSnapshotRawConversionBatch) ([]byte, error) {
	review := playerSnapshotRawReview{Version: 1, WorldID: batch.WorldID,
		RawABI: world.LegacyPlayerSnapshotRawV1ABI, ParserVersion: world.LegacyPlayerSnapshotRawV1ParserVersion,
		RequiresIdentityReview: true, Records: make([]playerSnapshotRawReviewRecord, 0, len(batch.Records))}
	for _, record := range batch.Records {
		review.Records = append(review.Records, playerSnapshotRawReviewRecord{
			SourceFile: record.SourcePath, SnapshotFile: record.SnapshotPath,
			SuggestedAccountName: record.SuggestedAccountName,
			SourceSHA256:         fmt.Sprintf("%x", record.SourceSHA256), CanonicalSHA256: fmt.Sprintf("%x", record.CanonicalSHA256),
			SourceOctets: record.SourceOctets, InventoryNodeCount: record.InventoryNodeCount,
		})
	}
	raw, err := json.MarshalIndent(review, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

func validatePrivateOutputParent(path, root string) error {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return errPlayerSnapshotRawConversionOutputInvalid
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return errPlayerSnapshotRawConversionOutputInvalid
	}
	relative, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errPlayerSnapshotRawConversionOutputInvalid
	}
	current := rootAbs
	if _, err := privateDirectoryInfo(current, errPlayerSnapshotRawConversionOutputInvalid); err != nil {
		return err
	}
	if relative == "." {
		return nil
	}
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		if part == "" || part == "." || part == ".." {
			return errPlayerSnapshotRawConversionOutputInvalid
		}
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if errors.Is(statErr, os.ErrNotExist) {
			// The remainder is also absent; ensurePrivateDirectory will create
			// it only after all output conflicts have been preflighted.
			return nil
		}
		if statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm() != 0o700 {
			return errPlayerSnapshotRawConversionOutputInvalid
		}
	}
	return nil
}

func checkImmutableOutput(path string, contents []byte) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return errPlayerSnapshotRawConversionOutputConflict
	}
	stored, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if bytes.Equal(stored, contents) {
		return nil
	}
	return errPlayerSnapshotRawConversionOutputConflict
}

func ensurePrivateDirectory(path, root string) error {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return errPlayerSnapshotRawConversionOutputInvalid
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return errPlayerSnapshotRawConversionOutputInvalid
	}
	relative, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errPlayerSnapshotRawConversionOutputInvalid
	}
	if relative == "." {
		_, err := privateDirectoryInfo(rootAbs, errPlayerSnapshotRawConversionOutputInvalid)
		return err
	}
	current := rootAbs
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		if part == "" || part == "." || part == ".." {
			return errPlayerSnapshotRawConversionOutputInvalid
		}
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if errors.Is(statErr, os.ErrNotExist) {
			if err := os.Mkdir(current, 0o700); err != nil {
				return err
			}
			continue
		}
		if statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm() != 0o700 {
			return errPlayerSnapshotRawConversionOutputInvalid
		}
	}
	return nil
}

func writePrivateImmutableFile(path string, contents []byte) error {
	if path == "" || len(contents) == 0 {
		return errPlayerSnapshotRawConversionOutputConflict
	}
	parent := filepath.Dir(path)
	if _, err := privateDirectoryInfo(parent, errPlayerSnapshotRawConversionOutputInvalid); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			return errPlayerSnapshotRawConversionOutputConflict
		}
		stored, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if bytes.Equal(stored, contents) {
			return nil
		}
		return errPlayerSnapshotRawConversionOutputConflict
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temporary, err := os.CreateTemp(parent, ".muhan-player-convert-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	cleanup := func() {
		temporary.Close()
		_ = os.Remove(temporaryPath)
	}
	if err := temporary.Chmod(0o600); err != nil {
		cleanup()
		return err
	}
	if _, err := temporary.Write(contents); err != nil {
		cleanup()
		return err
	}
	if err := temporary.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return err
	}
	if err := os.Link(temporaryPath, path); err != nil {
		if info, statErr := os.Lstat(path); statErr == nil && info.Mode().IsRegular() && info.Mode().Perm() == 0o600 {
			stored, readErr := os.ReadFile(path)
			if readErr == nil && bytes.Equal(stored, contents) {
				_ = os.Remove(temporaryPath)
				return nil
			}
		}
		_ = os.Remove(temporaryPath)
		return err
	}
	if err := os.Remove(temporaryPath); err != nil {
		return err
	}
	return syncDirectory(parent)
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
