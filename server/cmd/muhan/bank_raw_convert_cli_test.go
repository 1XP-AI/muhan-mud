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

func TestValidateBankRawConversionFlags(t *testing.T) {
	if options, err := validateBankRawConversionFlags("", "", "", "", false); err != nil || options != (bankRawConversionOptions{}) {
		t.Fatalf("empty options=%+v err=%v", options, err)
	}
	if _, err := validateBankRawConversionFlags("", "Alice", "", world.LegacyBankSnapshotRawV1ABI, true); !errors.Is(err, errBankRawConversionRootRequired) {
		t.Fatalf("missing root error=%v", err)
	}
	if _, err := validateBankRawConversionFlags("/tmp/muhan", "Alice", "", "", true); !errors.Is(err, errBankRawConversionABIRequired) {
		t.Fatalf("missing ABI error=%v", err)
	}
	if _, err := validateBankRawConversionFlags("/tmp/muhan", "Alice", "", world.LegacyBankSnapshotRawV1ABI, false); !errors.Is(err, errBankRawConversionOutputRequired) {
		t.Fatalf("missing output error=%v", err)
	}
	options, err := validateBankRawConversionFlags("/tmp/muhan", "Alice", "/tmp/out/bank.bin", world.LegacyBankSnapshotRawV1ABI, false)
	if err != nil || options.Root != "/tmp/muhan" || options.PlayerName != "Alice" || options.OutputPath != "/tmp/out/bank.bin" || options.ABI != world.LegacyBankSnapshotRawV1ABI {
		t.Fatalf("valid options=%+v err=%v", options, err)
	}
}

func TestConvertLegacyBankRawSourceProducesCanonicalReviewWithoutIdentityClaim(t *testing.T) {
	raw := legacyBankRawInspectionFixture("conversion-object-secret")
	root, _ := makeLegacyBankRawInspectionTree(t, raw)
	result, err := convertLegacyBankRawSource(bankRawConversionOptions{Root: root, PlayerName: "Alice", ABI: world.LegacyBankSnapshotRawV1ABI})
	if err != nil {
		t.Fatalf("convert raw bank: %v", err)
	}
	if result.SourceRelative != filepath.Join("player", "bank", "Alice") || result.PlayerName != "Alice" ||
		result.SourceOctets != int64(len(raw)) || result.CanonicalOctets != int64(len(result.Canonical)) || result.RootCount != 1 || result.NodeCount != 1 {
		t.Fatalf("conversion result=%+v", result)
	}
	if _, err := world.InspectBankSnapshotV1(result.Canonical); err != nil {
		t.Fatalf("canonical artifact is not a BankSnapshotV1: %v", err)
	}
	outputDir := t.TempDir()
	if err := os.Chmod(outputDir, 0o700); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(outputDir, "bank.bin")
	reviewPath, err := writeBankRawConversion(result, outputPath)
	if err != nil {
		t.Fatalf("write conversion: %v", err)
	}
	if reviewPath != outputPath+bankRawConversionReviewSuffix {
		t.Fatalf("review path=%q", reviewPath)
	}
	for _, path := range []string{outputPath, reviewPath} {
		info, statErr := os.Stat(path)
		if statErr != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("output %s info=%v err=%v", path, info, statErr)
		}
	}
	reviewRaw, err := os.ReadFile(reviewPath)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(reviewRaw) || !bytes.HasSuffix(reviewRaw, []byte("\n")) || strings.Contains(string(reviewRaw), "conversion-object-secret") {
		t.Fatalf("review leaked object payload or is invalid: %s", reviewRaw)
	}
	var review bankRawConversionReview
	if err := json.Unmarshal(reviewRaw, &review); err != nil {
		t.Fatal(err)
	}
	if err := validateBankRawConversionReview(review); err != nil || review.Records[0].SourcePlayerName != "Alice" || review.Records[0].CanonicalFile != "bank.bin" {
		t.Fatalf("review=%+v err=%v", review, err)
	}
	if _, err := writeBankRawConversion(result, outputPath); err != nil {
		t.Fatalf("same-byte conversion replay rejected: %v", err)
	}
	if err := os.WriteFile(outputPath, []byte("different"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := writeBankRawConversion(result, outputPath); !errors.Is(err, errBankRawConversionConflict) {
		t.Fatalf("changed output error=%v", err)
	}
	if _, err := writeBankRawConversion(result, root); !errors.Is(err, errBankRawConversionOverlap) {
		t.Fatalf("source-root output error=%v", err)
	}
}

func TestLegacyBankRawConversionCLIIsDBFreeAndDryRunDoesNotWrite(t *testing.T) {
	raw := legacyBankRawInspectionFixture("cli-conversion-secret")
	root, _ := makeLegacyBankRawInspectionTree(t, raw)
	outputDir := t.TempDir()
	if err := os.Chmod(outputDir, 0o700); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(outputDir, "bank.bin")
	binaryPath := filepath.Join(t.TempDir(), "muhan")
	if output, err := exec.Command("go", "build", "-o", binaryPath, ".").CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	command := exec.Command(binaryPath,
		"-convert-bank-raw-root", root,
		"-convert-bank-raw-player", "Alice",
		"-convert-bank-raw-abi", world.LegacyBankSnapshotRawV1ABI,
		"-convert-bank-cdto-output", outputPath,
		"-convert-bank-raw-dry-run",
	)
	command.Env = environmentWithoutDatabaseOrListener()
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("legacy raw bank conversion dry-run failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "legacy raw bank conversion validated") || strings.Contains(string(output), "DATABASE_URL") || strings.Contains(string(output), "cli-conversion-secret") {
		t.Fatalf("unexpected dry-run output: %s", output)
	}
	if _, err := os.Lstat(outputPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry-run wrote output: err=%v", err)
	}

	command = exec.Command(binaryPath,
		"-convert-bank-raw-root", root,
		"-convert-bank-raw-player", "Alice",
		"-convert-bank-raw-abi", world.LegacyBankSnapshotRawV1ABI,
		"-convert-bank-cdto-output", outputPath,
	)
	command.Env = environmentWithoutDatabaseOrListener()
	output, err = command.CombinedOutput()
	if err != nil {
		t.Fatalf("legacy raw bank conversion failed: %v\n%s", err, output)
	}
	canonical, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := world.InspectBankSnapshotV1(canonical); err != nil {
		t.Fatalf("CLI canonical artifact invalid: %v", err)
	}
	if strings.Contains(string(output), "cli-conversion-secret") || strings.Contains(string(output), "DATABASE_URL") {
		t.Fatalf("conversion output leaked payload or database details: %s", output)
	}
}
