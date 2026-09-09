package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func bankSnapshotInspectionFixture(t *testing.T, name string, withChild bool) []byte {
	t.Helper()
	var root world.PlayerSnapshotObjectV1
	copy(root.Name[:], name)
	root.Name[len(name)] = 0
	root.Description[0] = 'i'
	root.Description[1] = 0
	root.UseOutput[0] = 0
	root.Keys[0][0] = 'b'
	root.Keys[0][1] = 0
	graph := world.PlayerSnapshotObjectGraphV1{
		Nodes: []world.PlayerSnapshotObjectNodeV1{{Object: root, ChildIndex: 0}},
	}
	if withChild {
		var child world.PlayerSnapshotObjectV1
		copy(child.Name[:], "child")
		child.Name[5] = 0
		child.Description[0] = 0
		child.UseOutput[0] = 0
		child.Keys[0][0] = 'c'
		child.Keys[0][1] = 0
		parent := uint32(0)
		graph.Nodes = append(graph.Nodes, world.PlayerSnapshotObjectNodeV1{Object: child, ParentIndex: &parent, ChildIndex: 0})
	}
	raw, err := world.EncodeBankSnapshotV1(world.BankSnapshotV1{Root: graph})
	if err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	return raw
}

func writeBankSnapshotInspectionFile(t *testing.T, path string, raw []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, raw, mode); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod %s: %v", path, err)
	}
}

func TestValidateBankSnapshotInspectionFlagsRequiresOneSource(t *testing.T) {
	if options, err := validateBankSnapshotInspectionFlags("", ""); err != nil || options != (bankSnapshotInspectionOptions{}) {
		t.Fatalf("empty options=%+v err=%v", options, err)
	}
	if options, err := validateBankSnapshotInspectionFlags("private", ""); err != nil || options.Directory != "private" {
		t.Fatalf("directory options=%+v err=%v", options, err)
	}
	if options, err := validateBankSnapshotInspectionFlags("", "bank.bin"); err != nil || options.File != "bank.bin" {
		t.Fatalf("file options=%+v err=%v", options, err)
	}
	if _, err := validateBankSnapshotInspectionFlags("private", "bank.bin"); !errors.Is(err, errBankSnapshotInspectionInputsExclusive) {
		t.Fatalf("mixed options err=%v", err)
	}
	if _, err := InspectBankSnapshotReview("", ""); !errors.Is(err, errBankSnapshotInspectionInputRequired) {
		t.Fatalf("empty review err=%v", err)
	}
}

func TestInspectBankSnapshotDirectoryIsLexicalAndMetadataOnly(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(dir, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	small := bankSnapshotInspectionFixture(t, "bank-a", false)
	large := bankSnapshotInspectionFixture(t, "bank-z", true)
	writeBankSnapshotInspectionFile(t, filepath.Join(dir, "z.bin"), small, 0o600)
	writeBankSnapshotInspectionFile(t, filepath.Join(dir, "a.bin"), large, 0o600)
	writeBankSnapshotInspectionFile(t, filepath.Join(nested, "m.bin"), small, 0o600)
	if err := os.WriteFile(filepath.Join(dir, "operator-notes.txt"), []byte("ignored"), 0o640); err != nil {
		t.Fatal(err)
	}

	report, err := inspectBankSnapshotDirectory(dir)
	if err != nil {
		t.Fatalf("inspect directory: %v", err)
	}
	if report.Version != 1 || report.ArtifactFormat != bankSnapshotInspectionArtifactFormat ||
		report.ParserVersion != bankSnapshotInspectionParser || report.ABI != bankSnapshotInspectionABI ||
		!report.RequiresOperatorReview || len(report.Records) != 3 {
		t.Fatalf("report header=%+v", report)
	}
	wantPaths := []string{"a.bin", "nested/m.bin", "z.bin"}
	for index, record := range report.Records {
		if record.SourceFile != wantPaths[index] || record.Result != "validated" || record.RootCount != 1 || record.NodeCount <= 0 {
			t.Fatalf("record[%d]=%+v", index, record)
		}
		if record.SourceSHA256 == "" || record.SourceSHA256 != record.CanonicalSHA256 || record.SourceOctets != record.CanonicalOctets {
			t.Fatalf("record[%d] digest/size=%+v", index, record)
		}
	}
	if report.Records[0].NodeCount != 2 || report.Records[1].NodeCount != 1 || report.Records[2].NodeCount != 1 {
		t.Fatalf("lexical records=%+v", report.Records)
	}

	reviewJSON, err := MarshalBankSnapshotReview(report)
	if err != nil {
		t.Fatalf("marshal review: %v", err)
	}
	if len(reviewJSON) == 0 || reviewJSON[len(reviewJSON)-1] != '\n' || strings.Contains(string(reviewJSON), "bank-a") || strings.Contains(string(reviewJSON), "payload") {
		t.Fatalf("metadata report leaked payload/name: %s", reviewJSON)
	}
	var decoded map[string]any
	if err := json.Unmarshal(reviewJSON, &decoded); err != nil {
		t.Fatalf("decode review JSON: %v", err)
	}
	if _, ok := decoded["records"]; !ok {
		t.Fatalf("records missing from report: %s", reviewJSON)
	}
}

func TestInspectBankSnapshotDesignatedFileRequiresPrivateParentAndFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "designated-artifact")
	raw := bankSnapshotInspectionFixture(t, "designated", true)
	writeBankSnapshotInspectionFile(t, path, raw, 0o600)
	report, err := inspectBankSnapshotDesignatedFile(path)
	if err != nil {
		t.Fatalf("inspect designated file: %v", err)
	}
	if len(report.Records) != 1 || report.Records[0].SourceFile != filepath.ToSlash(path) || report.Records[0].NodeCount != 2 {
		t.Fatalf("designated report=%+v", report)
	}
	if jsonRaw, err := InspectBankSnapshotReviewJSON("", path); err != nil || !bytes.HasSuffix(jsonRaw, []byte("\n")) {
		t.Fatalf("designated JSON err=%v raw=%q", err, jsonRaw)
	}

	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := inspectBankSnapshotDesignatedFile(path); !errors.Is(err, errBankSnapshotInspectionFileInvalid) {
		t.Fatalf("public file err=%v", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := inspectBankSnapshotDesignatedFile(path); !errors.Is(err, errBankSnapshotInspectionDirectoryInvalid) {
		t.Fatalf("public parent err=%v", err)
	}
}

func TestInspectBankSnapshotDirectoryFailsClosedOnMalformedPermissionsAndSymlinks(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	valid := bankSnapshotInspectionFixture(t, "valid", false)
	path := filepath.Join(dir, "bad.bin")
	tampered := append([]byte(nil), valid...)
	tampered[len(tampered)-1] ^= 1
	writeBankSnapshotInspectionFile(t, path, tampered, 0o600)
	if report, err := inspectBankSnapshotDirectory(dir); err == nil || len(report.Records) != 0 {
		t.Fatalf("tampered artifact report=%+v err=%v", report, err)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	writeBankSnapshotInspectionFile(t, path, valid, 0o640)
	if _, err := inspectBankSnapshotDirectory(dir); !errors.Is(err, errBankSnapshotInspectionFileInvalid) {
		t.Fatalf("public artifact err=%v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing", path); err != nil {
		t.Fatal(err)
	}
	if _, err := inspectBankSnapshotDirectory(dir); !errors.Is(err, errBankSnapshotInspectionFileInvalid) {
		t.Fatalf("symlink artifact err=%v", err)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	oversize := filepath.Join(dir, "oversize.bin")
	file, err := os.OpenFile(oversize, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxBankSnapshotInspectionFileBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := inspectBankSnapshotDirectory(dir); !errors.Is(err, errBankSnapshotInspectionFileTooLarge) {
		t.Fatalf("oversize artifact err=%v", err)
	}
}

func TestInspectBankSnapshotReviewVerifiesWorldDigestAndCanonicalGraph(t *testing.T) {
	raw := bankSnapshotInspectionFixture(t, "digest", false)
	record, err := inspectBankSnapshotBytes("digest.bin", raw)
	if err != nil {
		t.Fatalf("inspect bytes: %v", err)
	}
	digest := sha256.Sum256(raw)
	if record.SourceSHA256 != stringDigest(digest) || record.CanonicalSHA256 != stringDigest(digest) || record.SourceOctets != int64(len(raw)) {
		t.Fatalf("digest metadata=%+v want=%s", record, stringDigest(digest))
	}
	if _, err := inspectBankSnapshotBytes("digest.bin", raw[:len(raw)-1]); err == nil {
		t.Fatal("truncated artifact accepted")
	}
}

func stringDigest(value [sha256.Size]byte) string {
	return hex.EncodeToString(value[:])
}
