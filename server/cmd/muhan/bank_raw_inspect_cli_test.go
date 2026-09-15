package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func legacyBankRawInspectionFixture(name string) []byte {
	const objectBytes = 376
	const countBytes = 4
	raw := make([]byte, objectBytes+countBytes)
	copy(raw[:80], name)
	raw[len(name)] = 0
	// The audited raw layout stores a native int child count after each
	// object. A zero count produces a single valid root with no child items.
	binary.LittleEndian.PutUint32(raw[objectBytes:], 0)
	return raw
}

func makeLegacyBankRawInspectionTree(t *testing.T, raw []byte) (string, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	player := filepath.Join(root, "player")
	bank := filepath.Join(player, "bank")
	if err := os.Mkdir(player, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(bank, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(bank, "Alice")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	return root, path
}

func TestValidateBankRawInspectionFlags(t *testing.T) {
	if options, err := validateBankRawInspectionFlags("", "", false); err != nil || options != (bankRawInspectionOptions{}) {
		t.Fatalf("empty options=%+v err=%v", options, err)
	}
	if _, err := validateBankRawInspectionFlags("", "Alice", false); !errors.Is(err, errBankRawInspectionRootRequired) {
		t.Fatalf("missing root error=%v", err)
	}
	if _, err := validateBankRawInspectionFlags("/tmp/muhan", "", false); !errors.Is(err, errBankRawInspectionNameRequired) {
		t.Fatalf("missing player error=%v", err)
	}
	if _, err := validateBankRawInspectionFlags("/tmp/muhan", "../Alice", false); err == nil {
		t.Fatal("path traversal player name unexpectedly accepted")
	}
	if _, err := validateBankRawInspectionFlags("", "", true); !errors.Is(err, errBankRawInspectionRootRequired) {
		t.Fatalf("dry-run without root error=%v", err)
	}
	options, err := validateBankRawInspectionFlags("/tmp/muhan", "Alice", true)
	if err != nil || options.Root != "/tmp/muhan" || options.PlayerName != "Alice" || !options.DryRun {
		t.Fatalf("valid options=%+v err=%v", options, err)
	}
}

func TestInspectLegacyBankRawReviewIsMetadataOnly(t *testing.T) {
	raw := legacyBankRawInspectionFixture("fixture-secret")
	root, path := makeLegacyBankRawInspectionTree(t, raw)
	report, err := InspectLegacyBankRawReview(root, "Alice")
	if err != nil {
		t.Fatalf("inspect raw bank: %v", err)
	}
	if report.Version != bankRawInspectionVersion || report.ArtifactFormat != bankRawInspectionArtifactFormat ||
		report.ParserVersion != world.LegacyBankSnapshotRawV1ParserVersion || report.ABI != world.LegacyBankSnapshotRawV1ABI ||
		!report.RequiresOperatorReview || report.Result != bankRawInspectionResult {
		t.Fatalf("report header=%+v", report)
	}
	source := report.Source
	wantDigest := sha256.Sum256(raw)
	if source.Root != root || source.SourcePath != path || source.RelativePath != filepath.Join("player", "bank", "Alice") ||
		source.PlayerName != "Alice" || source.SourceOctets != int64(len(raw)) || source.SourceSHA256 != hex.EncodeToString(wantDigest[:]) ||
		source.CanonicalOctets <= 0 || source.CanonicalSHA256 == "" || source.RootCount != 1 || source.NodeCount != 1 ||
		source.Mode != "0600" || source.Nlink != 1 {
		t.Fatalf("source metadata=%+v", source)
	}
	encoded, err := MarshalLegacyBankRawReview(report)
	if err != nil {
		t.Fatalf("marshal raw review: %v", err)
	}
	if !bytes.HasSuffix(encoded, []byte("\n")) || !json.Valid(encoded) || strings.Contains(string(encoded), "fixture-secret") {
		t.Fatalf("metadata report leaked payload or is invalid: %s", encoded)
	}
}

func TestLegacyBankRawInspectionCLIEmitsMetadataWithoutDatabase(t *testing.T) {
	raw := legacyBankRawInspectionFixture("cli-object-secret")
	root, _ := makeLegacyBankRawInspectionTree(t, raw)
	binaryPath := filepath.Join(t.TempDir(), "muhan")
	if output, err := exec.Command("go", "build", "-o", binaryPath, ".").CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	command := exec.Command(binaryPath,
		"-inspect-bank-raw-root", root,
		"-inspect-bank-raw-player", "Alice",
		"-inspect-bank-raw-dry-run",
	)
	command.Env = environmentWithoutDatabaseOrListener()
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("legacy raw bank inspection failed: %v\n%s", err, output)
	}
	if !json.Valid(output) || !bytes.HasSuffix(output, []byte("\n")) {
		t.Fatalf("raw inspection output is not JSON: %s", output)
	}
	var report bankRawInspectionReport
	if err := json.Unmarshal(output, &report); err != nil {
		t.Fatalf("decode raw inspection output: %v", err)
	}
	if report.Source.PlayerName != "Alice" || report.Source.RootCount != 1 || report.Source.NodeCount != 1 || report.Result != "validated" {
		t.Fatalf("CLI report=%+v", report)
	}
	if strings.Contains(string(output), "cli-object-secret") || strings.Contains(string(output), "DATABASE_URL") {
		t.Fatalf("raw inspection output leaked payload or database details: %s", output)
	}
}
