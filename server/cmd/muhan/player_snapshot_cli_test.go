package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
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

func TestValidatePlayerSnapshotInspectionFlags(t *testing.T) {
	if options, err := validatePlayerSnapshotInspectionFlags("", "", false); err != nil || options.Directory != "" {
		t.Fatalf("normal mode options=%+v err=%v", options, err)
	}
	if _, err := validatePlayerSnapshotInspectionFlags("", "world", false); !errors.Is(err, errPlayerSnapshotInspectionDirRequired) {
		t.Fatalf("world without directory err=%v", err)
	}
	if _, err := validatePlayerSnapshotInspectionFlags("snapshots", "", false); !errors.Is(err, errPlayerSnapshotInspectionWorldRequired) {
		t.Fatalf("directory without world err=%v", err)
	}
	options, err := validatePlayerSnapshotInspectionFlags("snapshots", "muhan-01", true)
	if err != nil || options.Directory != "snapshots" || options.WorldID != "muhan-01" || !options.DryRun {
		t.Fatalf("options=%+v err=%v", options, err)
	}
	rawOptions, err := validatePlayerSnapshotInspectionFlagsWithFormat("snapshots", "muhan-01", true, playerSnapshotInspectionFormatRaw)
	if err != nil || rawOptions.Format != playerSnapshotInspectionFormatRaw {
		t.Fatalf("raw options=%+v err=%v", rawOptions, err)
	}
	if _, err := validatePlayerSnapshotInspectionFlagsWithFormat("snapshots", "muhan-01", false, "unknown"); err == nil {
		t.Fatal("unknown inspection format was accepted")
	}
}

func TestInspectPlayerSnapshotDirectoryAcceptsAuditedLegacyRawFormat(t *testing.T) {
	dir := t.TempDir()
	rawDir := filepath.Join(dir, "raw")
	if err := os.Mkdir(rawDir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw := make([]byte, 1956)
	copy(raw[0:80], []byte("raw-player"))
	raw[319] = 0 // PLAYER in the audited C build.
	binary.LittleEndian.PutUint16(raw[332:334], 1)
	binary.LittleEndian.PutUint16(raw[334:336], 1)
	binary.LittleEndian.PutUint16(raw[336:338], 1)
	binary.LittleEndian.PutUint16(raw[338:340], 1)
	binary.LittleEndian.PutUint32(raw[1952:1956], 0)
	path := filepath.Join(rawDir, "player.raw")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	batch, err := inspectPlayerSnapshotDirectoryWithFormat(rawDir, "muhan-01", playerSnapshotInspectionFormatRaw)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Reports) != 1 {
		t.Fatalf("reports=%+v", batch.Reports)
	}
	report := batch.Reports[0]
	if report.Result != "validated" || report.ParserVersion != world.LegacyPlayerSnapshotRawV1ParserVersion || report.ABI != world.LegacyPlayerSnapshotRawV1ABI || report.SourceOctets != int64(len(raw)) || report.InventoryNodeCount != 0 {
		t.Fatalf("raw report=%+v", report)
	}
}

func TestInspectPlayerSnapshotDirectoryProducesDeterministicQuarantineEvidence(t *testing.T) {
	dir := t.TempDir()
	_, _, raw, _ := writePlayerSnapshotManifestFixture(t, dir, "Alice", "Alice")
	snapshotDir := filepath.Join(dir, "snapshots")
	if err := os.Mkdir(snapshotDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snapshotDir, "a-valid.cdto"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snapshotDir, "b-malformed.cdto"), []byte("bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snapshotDir, "c-public.cdto"), raw, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a-valid.cdto", filepath.Join(snapshotDir, "d-link.cdto")); err != nil {
		t.Fatal(err)
	}
	batch, err := inspectPlayerSnapshotDirectory(snapshotDir, "muhan-01")
	if err != nil {
		t.Fatal(err)
	}
	if batch.WorldID != "muhan-01" || len(batch.Reports) != 4 {
		t.Fatalf("batch=%+v", batch)
	}
	wantPaths := []string{"a-valid.cdto", "b-malformed.cdto", "c-public.cdto", "d-link.cdto"}
	for index, report := range batch.Reports {
		if report.SourcePath != wantPaths[index] {
			t.Fatalf("report[%d]=%+v, want path %q", index, report, wantPaths[index])
		}
		if report.ParserVersion != playerSnapshotInspectionParser || report.ABI != playerSnapshotInspectionABI {
			t.Fatalf("report[%d] parser metadata=%+v", index, report)
		}
	}
	if batch.Reports[0].Result != "validated" || batch.Reports[0].SourceOctets != int64(len(raw)) || batch.Reports[0].SourceSHA256 != sha256.Sum256(raw) {
		t.Fatalf("valid report=%+v", batch.Reports[0])
	}
	if batch.Reports[1].Result != "quarantined" || batch.Reports[1].SourceOctets != 3 || batch.Reports[1].SourceSHA256 == ([32]byte{}) {
		t.Fatalf("malformed report=%+v", batch.Reports[1])
	}
	if batch.Reports[2].Result != "quarantined" || batch.Reports[2].SourceSHA256 != ([32]byte{}) {
		t.Fatalf("public report=%+v", batch.Reports[2])
	}
	if batch.Reports[3].Result != "quarantined" || batch.Reports[3].SourceSHA256 != ([32]byte{}) {
		t.Fatalf("symlink report=%+v", batch.Reports[3])
	}
}

func TestInspectPlayerSnapshotDirectoryRequiresPrivateRootAndNoPartialBatch(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := inspectPlayerSnapshotDirectory(dir, "muhan-01"); !errors.Is(err, errPlayerSnapshotInspectionDirInvalid) {
		t.Fatalf("public root err=%v", err)
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

func TestPlayerSnapshotInspectionDryRunDoesNotRequireDatabase(t *testing.T) {
	dir := t.TempDir()
	_, _, raw, _ := writePlayerSnapshotManifestFixture(t, dir, "Alice", "Alice")
	snapshotDir := filepath.Join(dir, "snapshots")
	if err := os.Mkdir(snapshotDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snapshotDir, "alice.cdto"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "muhan")
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	command := exec.Command(binary,
		"-inspect-player-snapshot-dir", snapshotDir,
		"-inspect-player-snapshot-world", "muhan-01",
		"-inspect-player-snapshot-dry-run",
	)
	for _, value := range os.Environ() {
		if strings.HasPrefix(value, "DATABASE_URL=") || strings.HasPrefix(value, "ALLOWED_ORIGINS=") {
			continue
		}
		command.Env = append(command.Env, value)
	}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("inspection dry-run failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "inspection validated") || strings.Contains(string(output), "DATABASE_URL") {
		t.Fatalf("unexpected inspection dry-run output: %s", output)
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
