package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

const (
	maxPlayerSnapshotInspectionFiles   = 1024
	playerSnapshotInspectionParser     = "go-player-snapshot-v1"
	playerSnapshotInspectionABI        = "cdto-v1"
	playerSnapshotInspectionFormatCDTO = "cdto-v1"
	playerSnapshotInspectionFormatRaw  = "legacy-player-raw-v1"
)

var (
	errPlayerSnapshotInspectionDirRequired       = errors.New("player snapshot inspection directory is required")
	errPlayerSnapshotInspectionWorldRequired     = errors.New("player snapshot inspection world is required")
	errPlayerSnapshotInspectionDirInvalid        = errors.New("player snapshot inspection directory must be a private 0700 directory")
	errPlayerSnapshotInspectionTooMany           = errors.New("player snapshot inspection directory has too many files")
	errPlayerSnapshotInspectionAggregateTooLarge = errors.New("player snapshot inspection payload exceeds the size limit")
)

type playerSnapshotInspectionOptions struct {
	Directory string
	WorldID   string
	DryRun    bool
	Format    string
}

type playerSnapshotInspectionBatch struct {
	WorldID string
	Reports []storage.PlayerSnapshotInspection
}

func validatePlayerSnapshotInspectionFlags(directory, worldID string, dryRun bool) (playerSnapshotInspectionOptions, error) {
	return validatePlayerSnapshotInspectionFlagsWithFormat(directory, worldID, dryRun, playerSnapshotInspectionFormatCDTO)
}

func validatePlayerSnapshotInspectionFlagsWithFormat(directory, worldID string, dryRun bool, format string) (playerSnapshotInspectionOptions, error) {
	if directory == "" {
		if worldID != "" || dryRun || format != playerSnapshotInspectionFormatCDTO {
			return playerSnapshotInspectionOptions{}, errPlayerSnapshotInspectionDirRequired
		}
		return playerSnapshotInspectionOptions{}, nil
	}
	if format != playerSnapshotInspectionFormatCDTO && format != playerSnapshotInspectionFormatRaw {
		return playerSnapshotInspectionOptions{}, errors.New("unsupported player snapshot inspection format")
	}
	if worldID == "" {
		return playerSnapshotInspectionOptions{}, errPlayerSnapshotInspectionWorldRequired
	}
	if len(worldID) > 128 || !validOpaqueImportID(worldID) {
		return playerSnapshotInspectionOptions{}, errors.New("invalid player snapshot inspection world")
	}
	return playerSnapshotInspectionOptions{Directory: directory, WorldID: worldID, DryRun: dryRun, Format: format}, nil
}

// inspectPlayerSnapshotDirectory scans only the supplied tree and produces
// metadata. It never returns raw payload bytes, and it completes the whole
// scan before the caller can write any ledger row.
func inspectPlayerSnapshotDirectory(directory, worldID string) (playerSnapshotInspectionBatch, error) {
	return inspectPlayerSnapshotDirectoryWithFormat(directory, worldID, playerSnapshotInspectionFormatCDTO)
}

func inspectPlayerSnapshotDirectoryWithFormat(directory, worldID, format string) (playerSnapshotInspectionBatch, error) {
	if directory == "" {
		return playerSnapshotInspectionBatch{}, errPlayerSnapshotInspectionDirRequired
	}
	if format != playerSnapshotInspectionFormatCDTO && format != playerSnapshotInspectionFormatRaw {
		return playerSnapshotInspectionBatch{}, errors.New("unsupported player snapshot inspection format")
	}
	if len(worldID) > 128 || !validOpaqueImportID(worldID) {
		return playerSnapshotInspectionBatch{}, errPlayerSnapshotInspectionWorldRequired
	}
	rootInfo, err := os.Lstat(directory)
	if err != nil {
		return playerSnapshotInspectionBatch{}, err
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() || rootInfo.Mode().Perm() != 0o700 {
		return playerSnapshotInspectionBatch{}, errPlayerSnapshotInspectionDirInvalid
	}
	batch := playerSnapshotInspectionBatch{WorldID: worldID}
	var totalBytes int64
	err = filepath.WalkDir(directory, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == directory {
			return nil
		}
		relative, relErr := filepath.Rel(directory, path)
		if relErr != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || relative == ".." {
			return errPlayerSnapshotInspectionDirInvalid
		}
		relative = filepath.ToSlash(relative)
		if !validInspectionSourcePath(relative) {
			return errPlayerSnapshotInspectionDirInvalid
		}
		if len(batch.Reports) >= maxPlayerSnapshotInspectionFiles {
			return errPlayerSnapshotInspectionTooMany
		}
		if entry.IsDir() {
			info, infoErr := entry.Info()
			if infoErr != nil {
				return infoErr
			}
			if info.Mode().Perm() != 0o700 {
				return errPlayerSnapshotInspectionDirInvalid
			}
			return nil
		}
		report, bytesRead, fileErr := inspectPlayerSnapshotFileWithFormat(path, relative, format)
		if fileErr != nil {
			return fileErr
		}
		totalBytes += bytesRead
		if totalBytes > maxPlayerSnapshotManifestPayloadBytes {
			return errPlayerSnapshotInspectionAggregateTooLarge
		}
		batch.Reports = append(batch.Reports, report)
		return nil
	})
	if err != nil {
		return playerSnapshotInspectionBatch{}, err
	}
	if len(batch.Reports) == 0 {
		return playerSnapshotInspectionBatch{}, errors.New("player snapshot inspection directory is empty")
	}
	return batch, nil
}

func validInspectionSourcePath(value string) bool {
	if value == "" || len(value) > 1024 || !utf8.ValidString(value) {
		return false
	}
	for _, b := range []byte(value) {
		if b < 32 || b == 127 {
			return false
		}
	}
	return true
}

func inspectPlayerSnapshotFile(path, relative string) (storage.PlayerSnapshotInspection, int64, error) {
	return inspectPlayerSnapshotFileWithFormat(path, relative, playerSnapshotInspectionFormatCDTO)
}

func inspectPlayerSnapshotFileWithFormat(path, relative, format string) (storage.PlayerSnapshotInspection, int64, error) {
	parserVersion, abi, err := playerSnapshotInspectionContract(format)
	if err != nil {
		return storage.PlayerSnapshotInspection{}, 0, err
	}
	base := storage.PlayerSnapshotInspection{
		SourcePath: relative, ParserVersion: parserVersion, ABI: abi,
	}
	info, err := os.Lstat(path)
	if err != nil {
		return storage.PlayerSnapshotInspection{}, 0, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		base.Result = "quarantined"
		base.QuarantineReason = "symbolic link is not inspected"
		return base, 0, nil
	}
	if !info.Mode().IsRegular() {
		base.Result = "quarantined"
		base.QuarantineReason = "non-regular file is not inspected"
		return base, 0, nil
	}
	if info.Size() < 0 || info.Size() > maxPlayerSnapshotFileBytes {
		if info.Size() > maxPlayerSnapshotManifestPayloadBytes {
			return storage.PlayerSnapshotInspection{}, 0, errPlayerSnapshotInspectionAggregateTooLarge
		}
		base.SourceOctets = maxInt64AtLeastZero(info.Size())
		base.Result = "quarantined"
		base.QuarantineReason = "snapshot file exceeds the per-file size limit"
		return base, 0, nil
	}
	if info.Mode().Perm() != 0o600 {
		base.SourceOctets = info.Size()
		base.Result = "quarantined"
		base.QuarantineReason = "snapshot file is not private 0600"
		return base, 0, nil
	}
	if info.Size() == 0 {
		base.Result = "quarantined"
		base.QuarantineReason = "snapshot file is empty"
		return base, 0, nil
	}
	raw, stable, err := readStableSnapshotFile(path, info)
	if err != nil {
		return storage.PlayerSnapshotInspection{}, 0, err
	}
	if !stable {
		base.Result = "quarantined"
		base.QuarantineReason = "snapshot file changed during inspection"
		return base, 0, nil
	}
	digest := sha256.Sum256(raw)
	base.SourceSHA256 = digest
	base.SourceOctets = int64(len(raw))
	var snapshot world.PlayerSnapshotV1
	var inspectErr error
	switch format {
	case playerSnapshotInspectionFormatRaw:
		inspection, rawErr := world.InspectLegacyPlayerSnapshotRawV1(raw)
		if rawErr == nil {
			snapshot = inspection.Snapshot
		}
		inspectErr = rawErr
	default:
		inspection, cdtoErr := world.InspectPlayerSnapshotV1(raw)
		if cdtoErr == nil {
			snapshot = inspection.Snapshot
		}
		inspectErr = cdtoErr
	}
	if inspectErr != nil {
		base.Result = "quarantined"
		base.QuarantineReason = boundedInspectionReason(inspectErr)
		return base, int64(len(raw)), nil
	}
	canonical, encodeErr := world.EncodePlayerSnapshotV1(snapshot)
	if encodeErr != nil || (format == playerSnapshotInspectionFormatCDTO && !bytes.Equal(canonical, raw)) {
		base.Result = "quarantined"
		base.QuarantineReason = "snapshot is not canonical"
		return base, int64(len(raw)), nil
	}
	base.Result = "validated"
	base.InventoryNodeCount = len(snapshot.Inventory.Nodes)
	return base, int64(len(raw)), nil
}

func playerSnapshotInspectionContract(format string) (string, string, error) {
	switch format {
	case playerSnapshotInspectionFormatCDTO:
		return playerSnapshotInspectionParser, playerSnapshotInspectionABI, nil
	case playerSnapshotInspectionFormatRaw:
		return world.LegacyPlayerSnapshotRawV1ParserVersion, world.LegacyPlayerSnapshotRawV1ABI, nil
	default:
		return "", "", errors.New("unsupported player snapshot inspection format")
	}
}

func readStableSnapshotFile(path string, before os.FileInfo) ([]byte, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, false, err
	}
	if !os.SameFile(before, opened) || opened.Mode().Perm() != 0o600 || opened.Size() != before.Size() {
		return nil, false, nil
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxPlayerSnapshotFileBytes+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(raw)) > maxPlayerSnapshotFileBytes {
		return nil, false, nil
	}
	after, err := file.Stat()
	if err != nil {
		return nil, false, err
	}
	if !os.SameFile(opened, after) || after.Size() != int64(len(raw)) {
		return nil, false, nil
	}
	return raw, true, nil
}

func boundedInspectionReason(err error) string {
	if err == nil {
		return "snapshot validation failed"
	}
	reason := strings.TrimSpace(err.Error())
	if reason == "" {
		return "snapshot validation failed"
	}
	if !utf8.ValidString(reason) {
		return "snapshot validation failed"
	}
	var builder strings.Builder
	for _, r := range reason {
		if r < 0x20 || r == 0x7f {
			builder.WriteByte('?')
		} else {
			builder.WriteRune(r)
		}
		if builder.Len() >= 512 {
			break
		}
	}
	result := builder.String()
	if result == "" {
		return "snapshot validation failed"
	}
	return result
}

func maxInt64AtLeastZero(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}

func runPlayerSnapshotInspection(ctx context.Context, repo *storage.Postgres, batch playerSnapshotInspectionBatch) error {
	if repo == nil {
		return errors.New("nil postgres store")
	}
	if len(batch.Reports) == 0 {
		return errors.New("empty player snapshot inspection batch")
	}
	inserted, replayed, quarantined := 0, 0, 0
	for index, report := range batch.Reports {
		wasReplayed, err := repo.RecordPlayerSnapshotInspection(ctx, report)
		if err != nil {
			return fmt.Errorf("inspection record %d (%s): %w", index+1, report.SourcePath, err)
		}
		if wasReplayed {
			replayed++
		} else {
			inserted++
		}
		if report.Result == "quarantined" {
			quarantined++
		}
	}
	fmt.Printf("player snapshot inspection recorded: world=%s files=%d inserted=%d replayed=%d quarantined=%d\n", batch.WorldID, len(batch.Reports), inserted, replayed, quarantined)
	return nil
}
