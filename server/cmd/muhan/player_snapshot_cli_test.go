package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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

func TestValidatePlayerSnapshotImportFlags(t *testing.T) {
	if options, err := validatePlayerSnapshotImportFlags("", false); err != nil || options.ManifestPath != "" {
		t.Fatalf("normal mode options=%+v err=%v", options, err)
	}
	if _, err := validatePlayerSnapshotImportFlags("", true); err == nil {
		t.Fatal("dry-run without a manifest was accepted")
	}
	options, err := validatePlayerSnapshotImportFlags("manifest.json", true)
	if err != nil || options.ManifestPath != "manifest.json" || !options.DryRun {
		t.Fatalf("options=%+v err=%v", options, err)
	}
}

func TestReadPlayerSnapshotImportManifestValidatesPrivateReviewedInputs(t *testing.T) {
	dir := t.TempDir()
	manifestPath, snapshotPath, raw, itemCount := writePlayerSnapshotManifestFixture(t, dir, "Alice", "ALICE")
	batch, err := readPlayerSnapshotImportManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if batch.WorldID != "manifest-world" || len(batch.Requests) != 1 {
		t.Fatalf("batch=%+v", batch)
	}
	request := batch.Requests[0]
	if request.AccountName != "Alice" || request.ExpectedRevision != 0 || request.PlayerID != "legacy-cli-1" || request.WorldID != batch.WorldID {
		t.Fatalf("request=%+v", request)
	}
	if string(request.Snapshot) != string(raw) || request.CommandID != "manifest-import-1" || len(request.ItemIDs) != itemCount {
		t.Fatalf("request snapshot/manifest=%+v", request)
	}
	if _, err := os.Stat(snapshotPath); err != nil {
		t.Fatal(err)
	}

	manifestRaw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(manifestRaw), "password") {
		t.Fatal("manifest contains a plaintext password field")
	}
}

func TestReadPlayerSnapshotImportManifestRejectsUnknownSecretAndDigestChanges(t *testing.T) {
	dir := t.TempDir()
	manifestPath, snapshotPath, _, _ := writePlayerSnapshotManifestFixture(t, dir, "Alice", "Alice")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatal(err)
	}
	records := object["records"].([]any)
	record := records[0].(map[string]any)
	record["password"] = "should-never-be-accepted"
	unknownRaw, err := json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, unknownRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readPlayerSnapshotImportManifest(manifestPath); err == nil {
		t.Fatal("unknown plaintext password field was accepted")
	}

	// Restore a valid manifest, then bind it to a different source digest.
	manifestPath, _, _, _ = writePlayerSnapshotManifestFixture(t, dir, "Alice", "Alice")
	if err := os.WriteFile(snapshotPath, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readPlayerSnapshotImportManifest(manifestPath); err == nil {
		t.Fatal("tampered snapshot was accepted")
	}

	publicPath := filepath.Join(dir, "public-manifest.json")
	validRaw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(publicPath, validRaw, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := readPlayerSnapshotImportManifest(publicPath); !errors.Is(err, errPlayerSnapshotManifestNotPrivate) {
		t.Fatalf("public manifest err=%v", err)
	}
}

func TestReadPlayerSnapshotImportManifestRejectsDuplicateIdentityBeforeWrites(t *testing.T) {
	dir := t.TempDir()
	manifestPath, _, _, _ := writePlayerSnapshotManifestFixture(t, dir, "Alice", "Alice")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest playerSnapshotImportManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Records = append(manifest.Records, manifest.Records[0])
	duplicateRaw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, duplicateRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readPlayerSnapshotImportManifest(manifestPath); err == nil {
		t.Fatal("duplicate manifest identity was accepted")
	}
}

func TestPlayerSnapshotManifestDryRunDoesNotRequireDatabase(t *testing.T) {
	dir := t.TempDir()
	manifestPath, _, _, _ := writePlayerSnapshotManifestFixture(t, dir, "Alice", "Alice")
	binary := filepath.Join(dir, "muhan")
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	command := exec.Command(binary, "-import-player-snapshot-manifest", manifestPath, "-import-player-snapshot-manifest-dry-run")
	for _, value := range os.Environ() {
		if strings.HasPrefix(value, "DATABASE_URL=") || strings.HasPrefix(value, "ALLOWED_ORIGINS=") {
			continue
		}
		command.Env = append(command.Env, value)
	}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("dry-run failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "manifest validated") || strings.Contains(string(output), "DATABASE_URL") {
		t.Fatalf("unexpected dry-run output: %s", output)
	}
}

func writePlayerSnapshotManifestFixture(t *testing.T, dir, fixtureName, accountName string) (manifestPath, snapshotPath string, raw []byte, itemCount int) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	hexRaw, err := os.ReadFile(filepath.Join(root, "tests", "fixtures", "player_snapshot_v1_legacy_decoder_minimal.hex"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err = hex.DecodeString(strings.Join(strings.Fields(string(hexRaw)), ""))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := world.DecodePlayerSnapshotV1(raw)
	if err != nil {
		t.Fatal(err)
	}
	var nameBytes [80]byte
	copy(nameBytes[:], fixtureName)
	snapshot.Name = nameBytes
	raw, err = world.EncodePlayerSnapshotV1(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	snapshotPath = filepath.Join(dir, "snapshot.bin")
	if err := os.WriteFile(snapshotPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	hash, err := identity.HashPassword([]byte("pw1234"))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	itemCount = len(snapshot.Inventory.Nodes)
	itemIDs := make([]string, itemCount)
	for index := range itemIDs {
		itemIDs[index] = "manifest-item-" + string(rune('a'+index))
	}
	expectedRevision := int64(0)
	manifest := playerSnapshotImportManifest{
		Version: 1,
		WorldID: "manifest-world",
		Records: []playerSnapshotImportManifestRecord{{
			CommandID:         "manifest-import-1",
			ExpectedRevision:  &expectedRevision,
			AccountName:       accountName,
			CredentialHashB64: base64.StdEncoding.EncodeToString(hash),
			PlayerID:          "legacy-cli-1",
			SnapshotFile:      filepath.Base(snapshotPath),
			SourceSHA256:      hex.EncodeToString(digest[:]),
			ItemIDs:           itemIDs,
		}},
	}
	manifestRaw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath = filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(manifestPath, manifestRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	return manifestPath, snapshotPath, raw, itemCount
}
