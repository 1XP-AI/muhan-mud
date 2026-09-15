package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/identity"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestValidatePlayerSnapshotManifestBuildFlags(t *testing.T) {
	if options, err := validatePlayerSnapshotManifestBuildFlags("", "", "", false); err != nil || options.ReviewPath != "" {
		t.Fatalf("empty options=%+v err=%v", options, err)
	}
	cases := []struct {
		name    string
		review  string
		mapping string
		output  string
		dryRun  bool
		want    error
	}{
		{name: "review-required", mapping: "mapping.json", output: "manifest.json", want: errPlayerSnapshotManifestBuildReviewRequired},
		{name: "mapping-required", review: "review.json", output: "manifest.json", want: errPlayerSnapshotManifestBuildMappingRequired},
		{name: "output-required", review: "review.json", mapping: "mapping.json", want: errPlayerSnapshotManifestBuildOutputRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := validatePlayerSnapshotManifestBuildFlags(tc.review, tc.mapping, tc.output, tc.dryRun)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want=%v", err, tc.want)
			}
		})
	}
	options, err := validatePlayerSnapshotManifestBuildFlags("review.json", "mapping.json", "", true)
	if err != nil || !options.DryRun {
		t.Fatalf("dry-run options=%+v err=%v", options, err)
	}
	if _, err := validatePlayerSnapshotManifestBuildFlags("", "", "", true); !errors.Is(err, errPlayerSnapshotManifestBuildReviewRequired) {
		t.Fatalf("stray dry-run err=%v", err)
	}
}

func TestBuildPlayerSnapshotImportManifestFromReviewedRawConversion(t *testing.T) {
	reviewPath, mappingPath, outputDir := prepareReviewedPlayerSnapshotMapping(t, "Alice", nil)
	batch, manifestRaw, err := buildPlayerSnapshotImportManifest(reviewPath, mappingPath)
	if err != nil {
		t.Fatal(err)
	}
	if batch.WorldID != "muhan-01" || len(batch.Requests) != 1 || batch.Requests[0].AccountName != "Alice" {
		t.Fatalf("batch=%+v", batch)
	}
	if len(batch.Requests[0].CredentialHash) == 0 || len(batch.Requests[0].Snapshot) == 0 {
		t.Fatalf("batch omitted credential or snapshot: %+v", batch.Requests[0])
	}
	if bytesContainsAny(manifestRaw, "pw1234", "password") {
		t.Fatalf("manifest contains plaintext credential material: %s", manifestRaw)
	}
	manifestPath := filepath.Join(outputDir, "player-import-manifest.json")
	if err := writePlayerSnapshotManifestBuild(manifestPath, manifestRaw, reviewPath); err != nil {
		t.Fatal(err)
	}
	if err := writePlayerSnapshotManifestBuild(manifestPath, manifestRaw, reviewPath); err != nil {
		t.Fatalf("idempotent manifest output failed: %v", err)
	}
	info, err := os.Stat(manifestPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("manifest mode=%v err=%v", info.Mode().Perm(), err)
	}
	imported, err := readPlayerSnapshotImportManifest(manifestPath)
	if err != nil {
		t.Fatalf("built manifest is not consumable: %v", err)
	}
	if imported.WorldID != "muhan-01" || len(imported.Requests) != 1 || imported.Requests[0].PlayerID != "legacy-player-1" {
		t.Fatalf("imported=%+v", imported)
	}
}

func TestBuildPlayerSnapshotImportManifestRejectsUnknownCredentialAndIdentityMismatch(t *testing.T) {
	reviewPath, mappingPath, _ := prepareReviewedPlayerSnapshotMapping(t, "Alice", nil)
	raw, err := os.ReadFile(mappingPath)
	if err != nil {
		t.Fatal(err)
	}
	var mapping playerSnapshotIdentityMapping
	if err := json.Unmarshal(raw, &mapping); err != nil {
		t.Fatal(err)
	}
	mapping.Records[0].AccountName = "Bob"
	mismatch, err := json.Marshal(mapping)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mappingPath, mismatch, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := buildPlayerSnapshotImportManifest(reviewPath, mappingPath); !errors.Is(err, errPlayerSnapshotManifestBuildInvalid) {
		t.Fatalf("account mismatch err=%v", err)
	}

	var unknown map[string]any
	if err := json.Unmarshal(raw, &unknown); err != nil {
		t.Fatal(err)
	}
	unknown["password"] = "pw1234"
	unknownRaw, err := json.Marshal(unknown)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mappingPath, unknownRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := buildPlayerSnapshotImportManifest(reviewPath, mappingPath); !errors.Is(err, errPlayerSnapshotManifestBuildInvalid) {
		t.Fatalf("unknown password field err=%v", err)
	}
}

func TestBuildPlayerSnapshotManifestDryRunDoesNotRequireDatabase(t *testing.T) {
	reviewPath, mappingPath, _ := prepareReviewedPlayerSnapshotMapping(t, "Alice", nil)
	binaryPath := filepath.Join(t.TempDir(), "muhan")
	if output, err := exec.Command("go", "build", "-o", binaryPath, ".").CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	command := exec.Command(binaryPath,
		"-build-player-snapshot-manifest-review", reviewPath,
		"-build-player-snapshot-manifest-mapping", mappingPath,
		"-build-player-snapshot-manifest-dry-run",
	)
	for _, value := range os.Environ() {
		if strings.HasPrefix(value, "DATABASE_URL=") || strings.HasPrefix(value, "ALLOWED_ORIGINS=") {
			continue
		}
		command.Env = append(command.Env, value)
	}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("manifest build dry-run failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "manifest build validated") || strings.Contains(string(output), "DATABASE_URL") {
		t.Fatalf("unexpected dry-run output: %s", output)
	}
}

func TestWritePlayerSnapshotManifestBuildRejectsDifferentDirectory(t *testing.T) {
	reviewPath, mappingPath, _ := prepareReviewedPlayerSnapshotMapping(t, "Alice", nil)
	_, manifestRaw, err := buildPlayerSnapshotImportManifest(reviewPath, mappingPath)
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "manifest.json")
	if err := writePlayerSnapshotManifestBuild(outside, manifestRaw, reviewPath); !errors.Is(err, errPlayerSnapshotManifestBuildOutputInvalid) {
		t.Fatalf("outside output err=%v", err)
	}
}

func prepareReviewedPlayerSnapshotMapping(t *testing.T, accountName string, itemIDs []string) (reviewPath, mappingPath, outputDir string) {
	t.Helper()
	root := t.TempDir()
	sourceDir := filepath.Join(root, "raw")
	outputDir = filepath.Join(root, "reviewed")
	if err := os.Mkdir(sourceDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outputDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "alice.raw"), rawConversionFixture(accountName, 1), 0o600); err != nil {
		t.Fatal(err)
	}
	conversion, err := convertPlayerSnapshotRawDirectory(playerSnapshotRawConversionOptions{
		SourceDir: sourceDir, WorldID: "muhan-01", OutputDir: outputDir, ABI: world.LegacyPlayerSnapshotRawV1ABI,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := writePlayerSnapshotRawConversion(conversion, outputDir); err != nil {
		t.Fatal(err)
	}
	reviewPath = filepath.Join(outputDir, playerSnapshotRawReviewFile)
	hash, err := identity.HashPassword([]byte("pw1234"))
	if err != nil {
		t.Fatal(err)
	}
	if itemIDs == nil {
		itemIDs = []string{}
	}
	expectedRevision := int64(0)
	mapping := playerSnapshotIdentityMapping{
		Version: 1, WorldID: "muhan-01",
		Records: []playerSnapshotIdentityMappingRecord{{
			SnapshotFile: conversion.Records[0].SnapshotPath, CommandID: "import-player-1", ExpectedRevision: &expectedRevision,
			AccountName: accountName, CredentialHashB64: base64.StdEncoding.EncodeToString(hash), PlayerID: "legacy-player-1", ItemIDs: itemIDs,
		}},
	}
	mappingRaw, err := json.Marshal(mapping)
	if err != nil {
		t.Fatal(err)
	}
	mappingPath = filepath.Join(outputDir, "identity-mapping.json")
	if err := os.WriteFile(mappingPath, mappingRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	return reviewPath, mappingPath, outputDir
}

func bytesContainsAny(raw []byte, values ...string) bool {
	for _, value := range values {
		if strings.Contains(string(raw), value) {
			return true
		}
	}
	return false
}
