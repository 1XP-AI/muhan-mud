package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
)

func TestValidateBackupRestoreFlagsRequiresAnExplicitBoundedMode(t *testing.T) {
	tests := []struct {
		name string
		args func() (string, string, bool, string, string, int64, bool, bool)
		want backupRestoreMode
		err  string
	}{
		{
			name: "normal server mode",
			args: func() (string, string, bool, string, string, int64, bool, bool) {
				return "", "", false, "", "", -1, false, false
			},
			want: backupRestoreNone,
		},
		{
			name: "backup requires both identity and path",
			args: func() (string, string, bool, string, string, int64, bool, bool) {
				return "world", "", false, "", "", -1, false, false
			},
			err: "backup mode requires",
		},
		{
			name: "restore requires both identity and path",
			args: func() (string, string, bool, string, string, int64, bool, bool) {
				return "", "", false, "world", "", -1, false, false
			},
			err: "restore mode requires",
		},
		{
			name: "force does not silently ignore expected revision",
			args: func() (string, string, bool, string, string, int64, bool, bool) {
				return "", "", false, "world", "backup", 4, false, true
			},
			err: "cannot be combined",
		},
		{
			name: "expected revision rejects values below sentinel",
			args: func() (string, string, bool, string, string, int64, bool, bool) {
				return "", "", false, "", "", -2, false, false
			},
			err: "must be -1 or non-negative",
		},
		{
			name: "backup and restore cannot be mixed",
			args: func() (string, string, bool, string, string, int64, bool, bool) {
				return "world", "backup", false, "world", "restore", -1, false, false
			},
			err: "mutually exclusive",
		},
		{
			name: "restore expected revision is carried through",
			args: func() (string, string, bool, string, string, int64, bool, bool) {
				return "", "", false, "world", "backup", 9, false, false
			},
			want: backupRestoreImport,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options, err := validateBackupRestoreFlags(tt.args())
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("err=%v, want substring %q", err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if options.mode != tt.want {
				t.Fatalf("mode=%d, want %d", options.mode, tt.want)
			}
			if tt.want == backupRestoreImport {
				if options.restoreExpected == nil || *options.restoreExpected != 9 {
					t.Fatalf("expected revision=%v, want 9", options.restoreExpected)
				}
			}
		})
	}
}

func TestWriteWorldBackupFileIsPrivateAndDoesNotOverwriteByDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "world.backup")
	first := []byte(`{"version":1}`)
	second := []byte(`{"version":2}`)
	if err := writeWorldBackupFile(path, first, false); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%#o, want 0600", info.Mode().Perm())
	}
	if err := writeWorldBackupFile(path, second, false); !errors.Is(err, errBackupFileExists) {
		t.Fatalf("second write err=%v, want file exists", err)
	}
	if err := writeWorldBackupFile(path, second, true); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(second) {
		t.Fatalf("contents=%q, want %q", got, second)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "world.backup" {
		t.Fatalf("temporary files remain: %+v", entries)
	}
}

func TestReadWorldBackupFileEnforcesPrivateRegularBoundAndEnvelope(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "world.backup")
	if err := os.WriteFile(path, []byte(`not-a-backup`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readWorldBackupFile(path); err == nil {
		t.Fatal("invalid backup envelope accepted")
	}

	publicPath := filepath.Join(dir, "public.backup")
	if err := os.WriteFile(publicPath, []byte(`not-a-backup`), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := readWorldBackupFile(publicPath); !errors.Is(err, errBackupFileNotPrivate) {
		t.Fatalf("public backup err=%v, want private-file error", err)
	}

	oversizePath := filepath.Join(dir, "oversize.backup")
	oversize, err := os.OpenFile(oversizePath, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := oversize.Truncate(maxWorldBackupBytes + 1); err != nil {
		_ = oversize.Close()
		t.Fatal(err)
	}
	if err := oversize.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := readWorldBackupFile(oversizePath); !errors.Is(err, errBackupFileTooLarge) {
		t.Fatalf("oversize backup err=%v, want size error", err)
	}

	linkPath := filepath.Join(dir, "link.backup")
	if err := os.Symlink(path, linkPath); err != nil {
		t.Fatal(err)
	}
	if _, err := readWorldBackupFile(linkPath); !errors.Is(err, errBackupFileNotRegular) {
		t.Fatalf("symlink backup err=%v, want regular-file error", err)
	}
}

func TestReadWorldBackupFilePreservesStorageChecksumAndFormatValidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "world.backup")
	snapshot := storage.WorldSnapshot{
		Revision: 3,
		State:    []byte(`{"Version":1,"Rooms":{},"Players":{}}`),
	}
	backup, err := storage.NewWorldBackup("cli-world", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := backup.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	decoded, err := readWorldBackupFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.WorldID != "cli-world" || decoded.Revision != snapshot.Revision {
		t.Fatalf("decoded=%+v", decoded)
	}

	raw[len(raw)-2] ^= 1
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readWorldBackupFile(path); err == nil {
		t.Fatal("checksum-tampered backup accepted")
	}
}

func TestBackupCLIRejectsMixedModeBeforeDatabaseOrListener(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "muhan")
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	run := exec.Command(binary,
		"-backup-world", "cli-world",
		"-backup-file", filepath.Join(t.TempDir(), "world.backup"),
		"-world", "runtime-world",
	)
	// The flag boundary must reject this before DATABASE_URL or any listener
	// setup is reached, so no database environment is needed in this test.
	output, err := run.CombinedOutput()
	if err == nil {
		t.Fatalf("mixed mode unexpectedly succeeded: %s", output)
	}
	if !strings.Contains(string(output), "cannot be combined") {
		t.Fatalf("mixed mode output=%s", output)
	}
	if strings.Contains(string(output), "DATABASE_URL") || strings.Contains(string(output), "listening") {
		t.Fatalf("mixed mode crossed database/listener boundary: %s", output)
	}
}
