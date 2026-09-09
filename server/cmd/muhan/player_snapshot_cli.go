package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/1XP-Inc/muhan-mud/server/internal/identity"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
	"golang.org/x/crypto/bcrypt"
)

const (
	maxPlayerSnapshotManifestBytes        int64 = 8 << 20
	maxPlayerSnapshotManifestRecords            = 1024
	maxPlayerSnapshotManifestPayloadBytes int64 = 64 << 20
	maxPlayerSnapshotFileBytes            int64 = 5 << 20
)

var (
	errPlayerSnapshotManifestPathRequired = errors.New("player snapshot manifest path is required")
	errPlayerSnapshotManifestNotPrivate   = errors.New("player snapshot manifest must be a private 0600 regular file")
	errPlayerSnapshotManifestNotRegular   = errors.New("player snapshot manifest must be a regular file")
	errPlayerSnapshotManifestTooLarge     = errors.New("player snapshot manifest exceeds the size limit")
	errPlayerSnapshotManifestInvalid      = errors.New("invalid player snapshot import manifest")
	errPlayerSnapshotFileNotPrivate       = errors.New("player snapshot file must be a private 0600 regular file")
	errPlayerSnapshotFileNotRegular       = errors.New("player snapshot file must be a regular file")
	errPlayerSnapshotFileTooLarge         = errors.New("player snapshot file exceeds the size limit")
)

// playerSnapshotImportManifest is deliberately an operator-only contract. It
// contains a bcrypt hash, never a plaintext password. Snapshot bytes remain in
// separate private files so a reviewed manifest can bind each file by SHA-256.
type playerSnapshotImportManifest struct {
	Version int                                  `json:"version"`
	WorldID string                               `json:"world_id"`
	Records []playerSnapshotImportManifestRecord `json:"records"`
}

type playerSnapshotImportManifestRecord struct {
	CommandID         string   `json:"command_id"`
	ExpectedRevision  *int64   `json:"expected_revision"`
	AccountName       string   `json:"account_name"`
	CredentialHashB64 string   `json:"credential_hash_b64"`
	PlayerID          string   `json:"player_id"`
	SnapshotFile      string   `json:"snapshot_file"`
	SourceSHA256      string   `json:"source_sha256"`
	ItemIDs           []string `json:"item_ids"`
}

type playerSnapshotImportBatch struct {
	WorldID  string
	Requests []storage.PlayerSnapshotImport
}

type playerSnapshotImportOptions struct {
	ManifestPath string
	DryRun       bool
}

func validatePlayerSnapshotImportFlags(path string, dryRun bool) (playerSnapshotImportOptions, error) {
	if path == "" {
		if dryRun {
			return playerSnapshotImportOptions{}, errors.New("-import-player-snapshot-manifest-dry-run requires -import-player-snapshot-manifest")
		}
		return playerSnapshotImportOptions{}, nil
	}
	return playerSnapshotImportOptions{ManifestPath: path, DryRun: dryRun}, nil
}

// readPlayerSnapshotImportManifest fully validates all files before any DB
// write. This makes malformed later records fail before an earlier record can
// be committed, while the per-record storage operation remains atomic.
func readPlayerSnapshotImportManifest(path string) (playerSnapshotImportBatch, error) {
	if path == "" {
		return playerSnapshotImportBatch{}, errPlayerSnapshotManifestPathRequired
	}
	manifestRaw, err := readPrivateBoundedFile(path, maxPlayerSnapshotManifestBytes, errPlayerSnapshotManifestNotPrivate, errPlayerSnapshotManifestNotRegular, errPlayerSnapshotManifestTooLarge)
	if err != nil {
		return playerSnapshotImportBatch{}, err
	}
	var manifest playerSnapshotImportManifest
	decoder := json.NewDecoder(bytes.NewReader(manifestRaw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return playerSnapshotImportBatch{}, fmt.Errorf("%w: %v", errPlayerSnapshotManifestInvalid, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return playerSnapshotImportBatch{}, fmt.Errorf("%w: trailing JSON values", errPlayerSnapshotManifestInvalid)
		}
		return playerSnapshotImportBatch{}, fmt.Errorf("%w: trailing JSON: %v", errPlayerSnapshotManifestInvalid, err)
	}
	if manifest.Version != 1 || len(manifest.WorldID) > 128 || !validOpaqueImportID(manifest.WorldID) {
		return playerSnapshotImportBatch{}, fmt.Errorf("%w: version or world_id", errPlayerSnapshotManifestInvalid)
	}
	if len(manifest.Records) == 0 || len(manifest.Records) > maxPlayerSnapshotManifestRecords {
		return playerSnapshotImportBatch{}, fmt.Errorf("%w: record count", errPlayerSnapshotManifestInvalid)
	}

	baseDir := filepath.Dir(path)
	batch := playerSnapshotImportBatch{WorldID: manifest.WorldID, Requests: make([]storage.PlayerSnapshotImport, 0, len(manifest.Records))}
	seenCommands := make(map[string]struct{}, len(manifest.Records))
	seenPlayers := make(map[string]struct{}, len(manifest.Records))
	seenNames := make(map[string]struct{}, len(manifest.Records))
	seenSources := make(map[[sha256.Size]byte]struct{}, len(manifest.Records))
	var totalSnapshotBytes int64
	for index, record := range manifest.Records {
		request, sourceDigest, err := readPlayerSnapshotImportRecord(baseDir, manifest.WorldID, record)
		if err != nil {
			return playerSnapshotImportBatch{}, fmt.Errorf("%w: record %d: %v", errPlayerSnapshotManifestInvalid, index+1, err)
		}
		if _, exists := seenCommands[request.CommandID]; exists {
			return playerSnapshotImportBatch{}, fmt.Errorf("%w: duplicate command_id", errPlayerSnapshotManifestInvalid)
		}
		if _, exists := seenPlayers[request.PlayerID]; exists {
			return playerSnapshotImportBatch{}, fmt.Errorf("%w: duplicate player_id", errPlayerSnapshotManifestInvalid)
		}
		canonicalName, err := identity.CanonicalName(request.AccountName)
		if err != nil {
			return playerSnapshotImportBatch{}, fmt.Errorf("%w: account_name", errPlayerSnapshotManifestInvalid)
		}
		if _, exists := seenNames[canonicalName]; exists {
			return playerSnapshotImportBatch{}, fmt.Errorf("%w: duplicate account_name", errPlayerSnapshotManifestInvalid)
		}
		if _, exists := seenSources[sourceDigest]; exists {
			return playerSnapshotImportBatch{}, fmt.Errorf("%w: duplicate source_sha256", errPlayerSnapshotManifestInvalid)
		}
		seenCommands[request.CommandID] = struct{}{}
		seenPlayers[request.PlayerID] = struct{}{}
		seenNames[canonicalName] = struct{}{}
		seenSources[sourceDigest] = struct{}{}
		totalSnapshotBytes += int64(len(request.Snapshot))
		if totalSnapshotBytes > maxPlayerSnapshotManifestPayloadBytes {
			return playerSnapshotImportBatch{}, fmt.Errorf("%w: aggregate snapshot bytes", errPlayerSnapshotManifestInvalid)
		}
		batch.Requests = append(batch.Requests, request)
	}
	return batch, nil
}

func readPlayerSnapshotImportRecord(baseDir, worldID string, record playerSnapshotImportManifestRecord) (storage.PlayerSnapshotImport, [sha256.Size]byte, error) {
	var zero [sha256.Size]byte
	if !validOpaqueImportID(record.CommandID) || len(record.CommandID) > 128 {
		return storage.PlayerSnapshotImport{}, zero, errors.New("invalid command_id")
	}
	if record.ExpectedRevision == nil || *record.ExpectedRevision < 0 {
		return storage.PlayerSnapshotImport{}, zero, errors.New("expected_revision must be non-negative")
	}
	if !validOpaqueImportID(record.PlayerID) || len(record.PlayerID) > 128 {
		return storage.PlayerSnapshotImport{}, zero, errors.New("invalid player_id")
	}
	canonicalName, err := identity.CanonicalName(record.AccountName)
	if err != nil {
		return storage.PlayerSnapshotImport{}, zero, errors.New("invalid account_name")
	}
	if record.CredentialHashB64 == "" {
		return storage.PlayerSnapshotImport{}, zero, errors.New("credential_hash_b64 is required")
	}
	credentialHash, err := base64.StdEncoding.Strict().DecodeString(record.CredentialHashB64)
	if err != nil || !validBcryptHash(credentialHash) {
		return storage.PlayerSnapshotImport{}, zero, errors.New("credential_hash_b64 is not a supported bcrypt hash")
	}
	if record.SnapshotFile == "" {
		return storage.PlayerSnapshotImport{}, zero, errors.New("snapshot_file is required")
	}
	snapshotPath := record.SnapshotFile
	if !filepath.IsAbs(snapshotPath) {
		snapshotPath = filepath.Join(baseDir, snapshotPath)
	}
	snapshotRaw, err := readPrivateBoundedFile(filepath.Clean(snapshotPath), maxPlayerSnapshotFileBytes, errPlayerSnapshotFileNotPrivate, errPlayerSnapshotFileNotRegular, errPlayerSnapshotFileTooLarge)
	if err != nil {
		return storage.PlayerSnapshotImport{}, zero, fmt.Errorf("snapshot_file: %w", err)
	}
	sourceDigest, err := parseSHA256Hex(record.SourceSHA256)
	if err != nil {
		return storage.PlayerSnapshotImport{}, zero, errors.New("source_sha256 must be lowercase hexadecimal SHA-256")
	}
	actualDigest := sha256.Sum256(snapshotRaw)
	if actualDigest != sourceDigest {
		return storage.PlayerSnapshotImport{}, zero, errors.New("snapshot source_sha256 mismatch")
	}
	inspection, err := world.InspectPlayerSnapshotV1(snapshotRaw)
	if err != nil {
		return storage.PlayerSnapshotImport{}, zero, fmt.Errorf("snapshot validation: %w", err)
	}
	canonicalWire, err := world.EncodePlayerSnapshotV1(inspection.Snapshot)
	if err != nil || !bytes.Equal(canonicalWire, snapshotRaw) {
		return storage.PlayerSnapshotImport{}, zero, errors.New("snapshot is not canonical")
	}
	itemIDs := append([]string(nil), record.ItemIDs...)
	if len(itemIDs) != len(inspection.Snapshot.Inventory.Nodes) {
		return storage.PlayerSnapshotImport{}, zero, errors.New("item_ids count does not match snapshot graph")
	}
	seenItems := make(map[string]struct{}, len(itemIDs))
	for _, itemID := range itemIDs {
		if !validOpaqueImportID(itemID) || len(itemID) > 128 {
			return storage.PlayerSnapshotImport{}, zero, errors.New("invalid item_id")
		}
		if _, exists := seenItems[itemID]; exists {
			return storage.PlayerSnapshotImport{}, zero, errors.New("duplicate item_id")
		}
		seenItems[itemID] = struct{}{}
	}
	return storage.PlayerSnapshotImport{
		WorldID:          worldID,
		CommandID:        record.CommandID,
		ExpectedRevision: *record.ExpectedRevision,
		AccountName:      canonicalName,
		CredentialHash:   credentialHash,
		PlayerID:         record.PlayerID,
		Snapshot:         snapshotRaw,
		ItemIDs:          itemIDs,
	}, sourceDigest, nil
}

func parseSHA256Hex(value string) ([sha256.Size]byte, error) {
	var digest [sha256.Size]byte
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return digest, errors.New("invalid SHA-256")
	}
	raw, err := hex.DecodeString(value)
	if err != nil || len(raw) != sha256.Size {
		return digest, errors.New("invalid SHA-256")
	}
	copy(digest[:], raw)
	return digest, nil
}

func validOpaqueImportID(value string) bool {
	if value == "" || !utf8.ValidString(value) {
		return false
	}
	for _, b := range []byte(value) {
		if b < 32 || b == 127 || b == '/' || b == '\\' {
			return false
		}
	}
	return true
}

func validBcryptHash(hash []byte) bool {
	if len(hash) == 0 {
		return false
	}
	cost, err := bcrypt.Cost(hash)
	return err == nil && cost == bcrypt.DefaultCost
}

func readPrivateBoundedFile(path string, maxBytes int64, notPrivate, notRegular, tooLarge error) ([]byte, error) {
	if path == "" {
		return nil, errors.New("file path is required")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, notRegular
	}
	if info.Mode().Perm() != 0o600 {
		return nil, notPrivate
	}
	if info.Size() <= 0 || info.Size() > maxBytes {
		return nil, tooLarge
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || int64(len(raw)) > maxBytes {
		return nil, tooLarge
	}
	return raw, nil
}

func runPlayerSnapshotImport(ctx context.Context, repo *storage.Postgres, batch playerSnapshotImportBatch) error {
	if repo == nil {
		return errors.New("nil postgres store")
	}
	if len(batch.Requests) == 0 {
		return errPlayerSnapshotManifestInvalid
	}
	for index, request := range batch.Requests {
		result, err := repo.ImportPlayerSnapshot(ctx, request)
		if err != nil {
			return fmt.Errorf("manifest record %d command %q: %w", index+1, request.CommandID, err)
		}
		logPlayerSnapshotImportResult(index+1, result)
	}
	return nil
}

func logPlayerSnapshotImportResult(index int, result storage.PlayerSnapshotImportResult) {
	mode := "imported"
	if result.Replayed {
		mode = "replayed"
	}
	// Hashes are safe migration evidence; raw snapshot/password material is
	// intentionally never logged.
	fmt.Printf("player snapshot %d %s: player_id=%s source_sha256=%x canonical_sha256=%x inventory_nodes=%d\n", index, mode, result.PlayerID, result.SourceSHA256, result.CanonicalSHA256, result.InventoryNodeCount)
}
