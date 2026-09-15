package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestValidatePlayerSnapshotRawConversionFlags(t *testing.T) {
	if options, err := validatePlayerSnapshotRawConversionFlags("", "", "", "", false); err != nil || options.SourceDir != "" {
		t.Fatalf("empty conversion options=%+v err=%v", options, err)
	}
	cases := []struct {
		name   string
		source string
		world  string
		output string
		abi    string
		dry    bool
		want   error
	}{
		{name: "source-requires-world", source: "raw", abi: world.LegacyPlayerSnapshotRawV1ABI, want: errPlayerSnapshotRawConversionWorldRequired},
		{name: "source-requires-abi", source: "raw", world: "muhan-01", want: errPlayerSnapshotRawConversionABIRequired},
		{name: "source-rejects-abi", source: "raw", world: "muhan-01", abi: "other", want: world.ErrLegacyPlayerSnapshotRawUnsupportedABI},
		{name: "write-requires-output", source: "raw", world: "muhan-01", abi: world.LegacyPlayerSnapshotRawV1ABI, want: errPlayerSnapshotRawConversionOutputRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := validatePlayerSnapshotRawConversionFlags(tc.source, tc.world, tc.output, tc.abi, tc.dry)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want=%v", err, tc.want)
			}
		})
	}
	if options, err := validatePlayerSnapshotRawConversionFlags("raw", "muhan-01", "", world.LegacyPlayerSnapshotRawV1ABI, true); err != nil || !options.DryRun {
		t.Fatalf("dry-run options=%+v err=%v", options, err)
	}
}

func TestConvertPlayerSnapshotRawDirectoryWritesReviewedCDTO(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "raw")
	output := filepath.Join(root, "cdto")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(output, 0o700); err != nil {
		t.Fatal(err)
	}
	shard := filepath.Join(source, "a1")
	if err := os.Mkdir(shard, 0o700); err != nil {
		t.Fatal(err)
	}
	raw := rawConversionFixture("Alice", 1)
	rawPath := filepath.Join(shard, "legacy-player")
	if err := os.WriteFile(rawPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	options := playerSnapshotRawConversionOptions{
		SourceDir: source, WorldID: "muhan-01", OutputDir: output,
		ABI: world.LegacyPlayerSnapshotRawV1ABI,
	}
	batch, err := convertPlayerSnapshotRawDirectory(options)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Records) != 1 || batch.Records[0].SuggestedAccountName != "Alice" || batch.Records[0].SnapshotPath != "a1/legacy-player.cdto" {
		t.Fatalf("conversion batch=%+v", batch)
	}
	if err := writePlayerSnapshotRawConversion(batch, output); err != nil {
		t.Fatal(err)
	}
	// A second invocation is an exact replay, not an overwrite.
	if err := writePlayerSnapshotRawConversion(batch, output); err != nil {
		t.Fatalf("idempotent output failed: %v", err)
	}
	cdtoPath := filepath.Join(output, "a1", "legacy-player.cdto")
	cdto, err := os.ReadFile(cdtoPath)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(cdtoPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("CDTO mode=%v err=%v", info.Mode().Perm(), err)
	}
	snapshot, err := world.DecodePlayerSnapshotV1(cdto)
	if err != nil || string(snapshot.Name[:5]) != "Alice" {
		t.Fatalf("CDTO snapshot=%+v err=%v", snapshot, err)
	}
	reviewPath := filepath.Join(output, playerSnapshotRawReviewFile)
	reviewRaw, err := os.ReadFile(reviewPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(reviewRaw, []byte("legacy-secret")) || bytes.Contains(reviewRaw, []byte("password")) {
		t.Fatalf("review contains credential material: %s", reviewRaw)
	}
	var review playerSnapshotRawReview
	if err := json.Unmarshal(reviewRaw, &review); err != nil {
		t.Fatal(err)
	}
	if review.Version != 1 || review.WorldID != "muhan-01" || !review.RequiresIdentityReview || len(review.Records) != 1 {
		t.Fatalf("review=%+v", review)
	}
	if review.Records[0].SourceSHA256 != hexDigest(raw) || review.Records[0].CanonicalSHA256 != hexDigest(cdto) {
		t.Fatalf("review hashes=%+v", review.Records[0])
	}
}

func TestConvertPlayerSnapshotRawDirectoryRejectsDuplicateAndOutputConflict(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "raw")
	output := filepath.Join(root, "cdto")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(output, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"one", "two"} {
		if err := os.WriteFile(filepath.Join(source, file), rawConversionFixture("Alice", 1), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	_, err := convertPlayerSnapshotRawDirectory(playerSnapshotRawConversionOptions{SourceDir: source, WorldID: "muhan-01", OutputDir: output, ABI: world.LegacyPlayerSnapshotRawV1ABI})
	if !errors.Is(err, errPlayerSnapshotRawConversionNameConflict) {
		t.Fatalf("duplicate names err=%v", err)
	}

	// Recreate a single source and prove that a changed canonical payload cannot
	// replace an already-reviewed output artifact.
	if err := os.Remove(filepath.Join(source, "two")); err != nil {
		t.Fatal(err)
	}
	batch, err := convertPlayerSnapshotRawDirectory(playerSnapshotRawConversionOptions{SourceDir: source, WorldID: "muhan-01", OutputDir: output, ABI: world.LegacyPlayerSnapshotRawV1ABI})
	if err != nil {
		t.Fatal(err)
	}
	if err := writePlayerSnapshotRawConversion(batch, output); err != nil {
		t.Fatal(err)
	}
	changed := rawConversionFixture("Alice", 2)
	if err := os.WriteFile(filepath.Join(source, "one"), changed, 0o600); err != nil {
		t.Fatal(err)
	}
	changedBatch, err := convertPlayerSnapshotRawDirectory(playerSnapshotRawConversionOptions{SourceDir: source, WorldID: "muhan-01", OutputDir: output, ABI: world.LegacyPlayerSnapshotRawV1ABI})
	if err != nil {
		t.Fatal(err)
	}
	if err := writePlayerSnapshotRawConversion(changedBatch, output); !errors.Is(err, errPlayerSnapshotRawConversionOutputConflict) {
		t.Fatalf("changed output err=%v", err)
	}
}

func TestConvertPlayerSnapshotRawDirectoryRejectsOverlappingOutput(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "raw")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(source, "converted")
	if err := os.Mkdir(child, 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := convertPlayerSnapshotRawDirectory(playerSnapshotRawConversionOptions{SourceDir: source, WorldID: "muhan-01", OutputDir: child, ABI: world.LegacyPlayerSnapshotRawV1ABI})
	if !errors.Is(err, errPlayerSnapshotRawConversionOutputOverlap) {
		t.Fatalf("overlap err=%v", err)
	}
}

func TestPlayerSnapshotRawConversionDryRunDoesNotRequireDatabase(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "raw")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "alice"), rawConversionFixture("Alice", 1), 0o600); err != nil {
		t.Fatal(err)
	}
	binaryPath := filepath.Join(root, "muhan")
	if output, err := exec.Command("go", "build", "-o", binaryPath, ".").CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	command := exec.Command(binaryPath,
		"-convert-player-snapshot-raw-dir", source,
		"-convert-player-snapshot-raw-world", "muhan-01",
		"-convert-player-snapshot-raw-abi", world.LegacyPlayerSnapshotRawV1ABI,
		"-convert-player-snapshot-raw-dry-run",
	)
	for _, value := range os.Environ() {
		if strings.HasPrefix(value, "DATABASE_URL=") || strings.HasPrefix(value, "ALLOWED_ORIGINS=") {
			continue
		}
		command.Env = append(command.Env, value)
	}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("raw conversion dry-run failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "legacy raw player conversion validated") || strings.Contains(string(output), "DATABASE_URL") {
		t.Fatalf("unexpected dry-run output: %s", output)
	}
}

func TestWritePlayerSnapshotRawConversionPreflightsAllOutputs(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "raw")
	output := filepath.Join(root, "cdto")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(output, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "a"), rawConversionFixture("Alice", 1), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "b"), rawConversionFixture("Bob", 2), 0o600); err != nil {
		t.Fatal(err)
	}
	batch, err := convertPlayerSnapshotRawDirectory(playerSnapshotRawConversionOptions{SourceDir: source, WorldID: "muhan-01", OutputDir: output, ABI: world.LegacyPlayerSnapshotRawV1ABI})
	if err != nil {
		t.Fatal(err)
	}
	conflictPath := filepath.Join(output, filepath.FromSlash(batch.Records[1].SnapshotPath))
	if err := os.MkdirAll(filepath.Dir(conflictPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(conflictPath, []byte("different reviewed bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writePlayerSnapshotRawConversion(batch, output); !errors.Is(err, errPlayerSnapshotRawConversionOutputConflict) {
		t.Fatalf("preflight conflict err=%v", err)
	}
	firstPath := filepath.Join(output, filepath.FromSlash(batch.Records[0].SnapshotPath))
	if _, err := os.Stat(firstPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("first output was partially written: err=%v", err)
	}
}

func rawConversionFixture(name string, value uint16) []byte {
	raw := make([]byte, 1952+4)
	copy(raw[0:80], name)
	copy(raw[240:255], "legacy-secret")
	raw[319] = 0
	binary.LittleEndian.PutUint16(raw[332:334], value)
	binary.LittleEndian.PutUint16(raw[334:336], value)
	binary.LittleEndian.PutUint16(raw[336:338], value)
	binary.LittleEndian.PutUint16(raw[338:340], value)
	binary.LittleEndian.PutUint32(raw[1952:1956], 0)
	return raw
}

func hexDigest(raw []byte) string {
	digest := sha256.Sum256(raw)
	return strings.ToLower(fmtHex(digest[:]))
}

func fmtHex(raw []byte) string {
	const alphabet = "0123456789abcdef"
	result := make([]byte, len(raw)*2)
	for index, value := range raw {
		result[index*2] = alphabet[value>>4]
		result[index*2+1] = alphabet[value&0x0f]
	}
	return string(result)
}
