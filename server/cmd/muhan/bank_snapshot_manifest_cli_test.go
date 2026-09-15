package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestValidateBankSnapshotManifestFlags(t *testing.T) {
	if options, err := validateBankSnapshotImportFlags("", false, false); err != nil || options != (bankSnapshotImportOptions{}) {
		t.Fatalf("empty import options=%+v err=%v", options, err)
	}
	if _, err := validateBankSnapshotImportFlags("", true, false); !errors.Is(err, errBankSnapshotManifestDryRunRequired) {
		t.Fatalf("missing manifest dry-run err=%v", err)
	}
	if _, err := validateBankSnapshotImportFlags("", false, true); !errors.Is(err, errBankSnapshotManifestApplyRequired) {
		t.Fatalf("missing manifest apply err=%v", err)
	}
	if _, err := validateBankSnapshotImportFlags("manifest.json", true, true); !errors.Is(err, errBankSnapshotManifestApplyDryRun) {
		t.Fatalf("apply+dry-run err=%v", err)
	}
	options, err := validateBankSnapshotImportFlags("manifest.json", false, false)
	if err != nil || !options.DryRun || options.Apply {
		t.Fatalf("path-only options=%+v err=%v", options, err)
	}
	options, err = validateBankSnapshotImportFlags("manifest.json", false, true)
	if err != nil || options.DryRun || !options.Apply {
		t.Fatalf("apply options=%+v err=%v", options, err)
	}

	if options, err := validateBankSnapshotManifestBuildFlags("", "", "", false); err != nil || options != (bankSnapshotManifestBuildOptions{}) {
		t.Fatalf("empty build options=%+v err=%v", options, err)
	}
	if _, err := validateBankSnapshotManifestBuildFlags("review.json", "", "out.json", false); !errors.Is(err, errBankSnapshotManifestBuildMappingNeeded) {
		t.Fatalf("missing mapping err=%v", err)
	}
	if _, err := validateBankSnapshotManifestBuildFlags("review.json", "mapping.json", "", false); !errors.Is(err, errBankSnapshotManifestBuildOutputNeeded) {
		t.Fatalf("missing output err=%v", err)
	}
	if options, err := validateBankSnapshotManifestBuildFlags("review.json", "mapping.json", "", true); err != nil || !options.DryRun {
		t.Fatalf("dry-run build options=%+v err=%v", options, err)
	}
}

func TestBuildBankSnapshotImportManifestRechecksReviewAndIdentity(t *testing.T) {
	paths := prepareBankSnapshotReview(t)
	expectedRevision := int64(0)
	mappingPath := writeBankSnapshotIdentityMapping(t, paths.dir, bankSnapshotIdentityMapping{
		Version: 1, WorldID: "world-bank", Records: []bankSnapshotIdentityMappingRecord{{
			SnapshotFile: filepath.Base(paths.snapshotPath), CommandID: "bank-import-1", ExpectedRevision: &expectedRevision,
			AccountName: "Alice", PlayerID: "player-1", ItemIDs: []string{},
		}},
	})
	batch, manifestRaw, err := buildBankSnapshotImportManifest(paths.reviewPath, mappingPath)
	if err != nil {
		t.Fatal(err)
	}
	if batch.WorldID != "world-bank" || len(batch.Requests) != 1 || batch.Requests[0].AccountName != "Alice" || batch.Requests[0].PlayerID != "player-1" || len(batch.Requests[0].Snapshot) == 0 {
		t.Fatalf("batch=%+v", batch)
	}
	if bytes.Contains(manifestRaw, []byte("conversion-object-secret")) || !bytes.Contains(manifestRaw, []byte("snapshot_sha256")) {
		t.Fatalf("manifest leaked payload or omitted digest: %s", manifestRaw)
	}
	manifestPath := filepath.Join(paths.dir, "bank-import-manifest.json")
	if err := writeBankSnapshotManifestBuild(manifestPath, manifestRaw, paths.reviewPath); err != nil {
		t.Fatal(err)
	}
	if err := writeBankSnapshotManifestBuild(manifestPath, manifestRaw, paths.reviewPath); err != nil {
		t.Fatalf("same-byte replay: %v", err)
	}
	loaded, err := readBankSnapshotImportManifest(manifestPath)
	if err != nil || len(loaded.Requests) != 1 || loaded.Requests[0].PlayerID != "player-1" {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}

	var mapping bankSnapshotIdentityMapping
	mappingRaw, err := os.ReadFile(mappingPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(mappingRaw, &mapping); err != nil {
		t.Fatal(err)
	}
	mapping.Records[0].AccountName = "Bob"
	if err := os.WriteFile(mappingPath, mustJSON(mapping), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := buildBankSnapshotImportManifest(paths.reviewPath, mappingPath); !errors.Is(err, errBankSnapshotManifestBuildInvalid) {
		t.Fatalf("source/account mismatch err=%v", err)
	}
}

func TestReadBankSnapshotImportManifestRejectsSensitiveUnknownAndChangedArtifact(t *testing.T) {
	paths := prepareBankSnapshotReview(t)
	expectedRevision := int64(0)
	mappingPath := writeBankSnapshotIdentityMapping(t, paths.dir, bankSnapshotIdentityMapping{
		Version: 1, WorldID: "world-bank", Records: []bankSnapshotIdentityMappingRecord{{
			SnapshotFile: filepath.Base(paths.snapshotPath), CommandID: "bank-import-2", ExpectedRevision: &expectedRevision,
			AccountName: "Alice", PlayerID: "player-2", ItemIDs: []string{},
		}},
	})
	_, manifestRaw, err := buildBankSnapshotImportManifest(paths.reviewPath, mappingPath)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(manifestRaw, &object); err != nil {
		t.Fatal(err)
	}
	object["password"] = "must-not-be-accepted"
	unknownPath := filepath.Join(paths.dir, "unknown-manifest.json")
	if err := os.WriteFile(unknownPath, mustJSON(object), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readBankSnapshotImportManifest(unknownPath); err == nil {
		t.Fatal("unknown sensitive field accepted")
	}
	if err := os.WriteFile(paths.snapshotPath, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(paths.dir, "changed-manifest.json")
	if err := os.WriteFile(manifestPath, manifestRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readBankSnapshotImportManifest(manifestPath); err == nil {
		t.Fatal("changed canonical artifact accepted")
	}
}

func TestBankSnapshotManifestCLIIsDBFreeForValidationAndBuild(t *testing.T) {
	paths := prepareBankSnapshotReview(t)
	expectedRevision := int64(0)
	mappingPath := writeBankSnapshotIdentityMapping(t, paths.dir, bankSnapshotIdentityMapping{
		Version: 1, WorldID: "world-bank-cli", Records: []bankSnapshotIdentityMappingRecord{{
			SnapshotFile: filepath.Base(paths.snapshotPath), CommandID: "bank-cli", ExpectedRevision: &expectedRevision,
			AccountName: "Alice", PlayerID: "player-cli", ItemIDs: []string{},
		}},
	})
	binaryPath := filepath.Join(paths.dir, "muhan")
	if output, err := exec.Command("go", "build", "-o", binaryPath, ".").CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	env := environmentWithoutDatabaseOrListener()
	dryRun := exec.Command(binaryPath, "-build-bank-snapshot-manifest-review", paths.reviewPath, "-build-bank-snapshot-manifest-mapping", mappingPath, "-build-bank-snapshot-manifest-dry-run")
	dryRun.Env = env
	output, err := dryRun.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "bank snapshot manifest build validated") || strings.Contains(string(output), "DATABASE_URL") {
		t.Fatalf("build dry-run output=%s err=%v", output, err)
	}
	manifestPath := filepath.Join(paths.dir, "cli-bank-manifest.json")
	write := exec.Command(binaryPath, "-build-bank-snapshot-manifest-review", paths.reviewPath, "-build-bank-snapshot-manifest-mapping", mappingPath, "-build-bank-snapshot-manifest-output", manifestPath)
	write.Env = env
	output, err = write.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "bank snapshot import manifest built") {
		t.Fatalf("build output=%s err=%v", output, err)
	}
	if info, err := os.Stat(manifestPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("manifest mode=%v err=%v", info, err)
	}
	pathOnly := exec.Command(binaryPath, "-import-bank-snapshot-manifest", manifestPath)
	pathOnly.Env = env
	output, err = pathOnly.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "bank snapshot manifest validated") || strings.Contains(string(output), "DATABASE_URL") {
		t.Fatalf("path-only output=%s err=%v", output, err)
	}
}

type bankSnapshotReviewPaths struct {
	dir          string
	snapshotPath string
	reviewPath   string
}

func prepareBankSnapshotReview(t *testing.T) bankSnapshotReviewPaths {
	t.Helper()
	raw := legacyBankRawInspectionFixture("conversion-object-secret")
	sourceRoot, _ := makeLegacyBankRawInspectionTree(t, raw)
	conversion, err := convertLegacyBankRawSource(bankRawConversionOptions{Root: sourceRoot, PlayerName: "Alice", ABI: world.LegacyBankSnapshotRawV1ABI})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	snapshotPath := filepath.Join(dir, "bank.bin")
	reviewPath, err := writeBankRawConversion(conversion, snapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	return bankSnapshotReviewPaths{dir: dir, snapshotPath: snapshotPath, reviewPath: reviewPath}
}

func writeBankSnapshotIdentityMapping(t *testing.T, dir string, mapping bankSnapshotIdentityMapping) string {
	t.Helper()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "bank-identity-mapping.json")
	if err := os.WriteFile(path, mustJSON(mapping), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustJSON(value any) []byte {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return raw
}
