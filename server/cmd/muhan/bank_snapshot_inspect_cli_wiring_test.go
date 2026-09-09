package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBankSnapshotInspectionCLIEmitsMetadataJSONWithoutDatabase(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(root, "bank.bin")
	writeBankSnapshotInspectionFile(t, artifact, bankSnapshotInspectionFixture(t, "cli-bank", true), 0o600)

	binaryPath := filepath.Join(t.TempDir(), "muhan")
	if output, err := exec.Command("go", "build", "-o", binaryPath, ".").CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	command := exec.Command(binaryPath,
		"-inspect-bank-snapshot-file", artifact,
		"-inspect-bank-snapshot-dry-run",
	)
	command.Env = environmentWithoutDatabaseOrListener()
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("bank snapshot inspection dry-run failed: %v\n%s", err, output)
	}
	if !json.Valid(output) || !bytes.HasSuffix(output, []byte("\n")) {
		t.Fatalf("inspection output is not JSON: %s", output)
	}
	var report bankSnapshotReviewReport
	if err := json.Unmarshal(output, &report); err != nil {
		t.Fatalf("decode inspection output: %v", err)
	}
	if report.Version != 1 || report.ArtifactFormat != bankSnapshotInspectionArtifactFormat ||
		report.ParserVersion != bankSnapshotInspectionParser || report.ABI != bankSnapshotInspectionABI ||
		!report.RequiresOperatorReview || len(report.Records) != 1 {
		t.Fatalf("inspection report=%+v", report)
	}
	if record := report.Records[0]; record.SourceOctets <= 0 || record.SourceSHA256 == "" ||
		record.SourceSHA256 != record.CanonicalSHA256 || record.RootCount != 1 || record.NodeCount != 2 ||
		record.Result != "validated" {
		t.Fatalf("inspection record=%+v", record)
	}
	if strings.Contains(string(output), "cli-bank") || strings.Contains(string(output), "DATABASE_URL") {
		t.Fatalf("inspection output leaked payload or database details: %s", output)
	}
}

func TestBankSnapshotInspectionCLIRejectsConflictsAndInvalidInputsBeforeDatabase(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "directory and file",
			args: []string{
				"-inspect-bank-snapshot-dir", "private",
				"-inspect-bank-snapshot-file", "bank.bin",
			},
			want: "mutually exclusive",
		},
		{
			name: "dry-run without source",
			args: []string{"-inspect-bank-snapshot-dry-run"},
			want: "requires",
		},
		{
			name: "inspection with world mode",
			args: []string{
				"-inspect-bank-snapshot-file", "bank.bin",
				"-world", "muhan-01",
			},
			want: "cannot be combined",
		},
	}

	binaryPath := filepath.Join(t.TempDir(), "muhan")
	if output, err := exec.Command("go", "build", "-o", binaryPath, ".").CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command := exec.Command(binaryPath, test.args...)
			command.Env = environmentWithoutDatabaseOrListener()
			output, err := command.CombinedOutput()
			if err == nil {
				t.Fatalf("invalid inspection invocation succeeded: %s", output)
			}
			if !strings.Contains(string(output), test.want) {
				t.Fatalf("error=%s, want substring %q", output, test.want)
			}
			if strings.Contains(string(output), "DATABASE_URL is required") {
				t.Fatalf("inspection validation reached database setup: %s", output)
			}
		})
	}
}

func environmentWithoutDatabaseOrListener() []string {
	env := make([]string, 0, len(os.Environ()))
	for _, value := range os.Environ() {
		if strings.HasPrefix(value, "DATABASE_URL=") || strings.HasPrefix(value, "ALLOWED_ORIGINS=") || strings.HasPrefix(value, "LISTEN_ADDR=") {
			continue
		}
		env = append(env, value)
	}
	return env
}
