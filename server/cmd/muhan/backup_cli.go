package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
)

// A world backup is a provisioning artifact, not a general-purpose archive.
// Keep the bound small enough that the CLI cannot be used to consume
// unbounded memory while parsing an operator-supplied file.
const maxWorldBackupBytes int64 = 64 << 20

var (
	errBackupFileExists       = errors.New("backup file already exists; pass -backup-overwrite to replace it")
	errBackupFileTooLarge     = errors.New("world backup file exceeds the size limit")
	errBackupFileNotPrivate   = errors.New("world backup file must be a private 0600 regular file")
	errBackupFileNotRegular   = errors.New("world backup file must be a regular file")
	errBackupFilePathRequired = errors.New("world backup file path is required")
)

type backupRestoreMode uint8

const (
	backupRestoreNone backupRestoreMode = iota
	backupRestoreExport
	backupRestoreImport
)

// backupRestoreOptions is the validated, explicit provisioning contract. A
// zero mode means that normal server/migrate/seed handling should continue.
type backupRestoreOptions struct {
	mode backupRestoreMode

	backupWorld     string
	backupFile      string
	backupOverwrite bool

	restoreWorld       string
	restoreFile        string
	restoreExpected    *int64
	restoreAllowCreate bool
	restoreForce       bool
}

func validateBackupRestoreFlags(
	backupWorld, backupFile string,
	backupOverwrite bool,
	restoreWorld, restoreFile string,
	restoreExpectedRevision int64,
	restoreAllowCreate, restoreForce bool,
) (backupRestoreOptions, error) {
	if restoreExpectedRevision < -1 {
		return backupRestoreOptions{}, errors.New("-restore-expected-revision must be -1 or non-negative")
	}
	backupRequested := backupWorld != "" || backupFile != "" || backupOverwrite
	restoreRequested := restoreWorld != "" || restoreFile != "" || restoreExpectedRevision >= 0 || restoreAllowCreate || restoreForce
	if backupRequested && restoreRequested {
		return backupRestoreOptions{}, errors.New("backup and restore modes are mutually exclusive")
	}
	if !backupRequested && !restoreRequested {
		return backupRestoreOptions{}, nil
	}
	if backupRequested {
		if backupWorld == "" || backupFile == "" {
			return backupRestoreOptions{}, errors.New("backup mode requires -backup-world and -backup-file")
		}
		if restoreExpectedRevision >= 0 || restoreAllowCreate || restoreForce {
			return backupRestoreOptions{}, errors.New("restore options are not valid in backup mode")
		}
		return backupRestoreOptions{
			mode:            backupRestoreExport,
			backupWorld:     backupWorld,
			backupFile:      backupFile,
			backupOverwrite: backupOverwrite,
		}, nil
	}
	if restoreWorld == "" || restoreFile == "" {
		return backupRestoreOptions{}, errors.New("restore mode requires -restore-world and -restore-file")
	}
	// A force restore deliberately ignores the target revision. Refusing the
	// ambiguous combination keeps an operator from believing that the revision
	// was checked when it was not.
	if restoreForce && restoreExpectedRevision >= 0 {
		return backupRestoreOptions{}, errors.New("-restore-force cannot be combined with -restore-expected-revision")
	}
	if restoreForce && restoreAllowCreate {
		return backupRestoreOptions{}, errors.New("-restore-force cannot be combined with -restore-allow-create")
	}
	var expected *int64
	if restoreExpectedRevision >= 0 {
		revision := restoreExpectedRevision
		expected = &revision
	}
	return backupRestoreOptions{
		mode:               backupRestoreImport,
		restoreWorld:       restoreWorld,
		restoreFile:        restoreFile,
		restoreExpected:    expected,
		restoreAllowCreate: restoreAllowCreate,
		restoreForce:       restoreForce,
	}, nil
}

func runBackupRestore(ctx context.Context, repo *storage.Postgres, options backupRestoreOptions) error {
	if options.mode == backupRestoreNone {
		return nil
	}
	if repo == nil {
		return errors.New("nil postgres store")
	}
	switch options.mode {
	case backupRestoreExport:
		return exportWorldBackup(ctx, repo, options.backupWorld, options.backupFile, options.backupOverwrite)
	case backupRestoreImport:
		return restoreWorldBackup(ctx, repo, options)
	default:
		return errors.New("invalid backup/restore mode")
	}
}

func exportWorldBackup(ctx context.Context, repo *storage.Postgres, worldID, path string, overwrite bool) error {
	snapshot, err := repo.LoadWorld(ctx, worldID)
	if err != nil {
		return fmt.Errorf("load world %q: %w", worldID, err)
	}
	backup, err := storage.NewWorldBackup(worldID, snapshot)
	if err != nil {
		return fmt.Errorf("validate world backup: %w", err)
	}
	raw, err := backup.Marshal()
	if err != nil {
		return fmt.Errorf("marshal world backup: %w", err)
	}
	if err := writeWorldBackupFile(path, raw, overwrite); err != nil {
		return fmt.Errorf("write world backup: %w", err)
	}
	return nil
}

func restoreWorldBackup(ctx context.Context, repo *storage.Postgres, options backupRestoreOptions) error {
	backup, err := readWorldBackupFile(options.restoreFile)
	if err != nil {
		return fmt.Errorf("read world backup: %w", err)
	}
	if backup.WorldID != options.restoreWorld {
		return fmt.Errorf("backup world %q does not match -restore-world %q", backup.WorldID, options.restoreWorld)
	}
	if err := repo.RestoreWorldBackup(ctx, backup, storage.WorldBackupOptions{
		ExpectedRevision: options.restoreExpected,
		AllowCreate:      options.restoreAllowCreate,
		Force:            options.restoreForce,
	}); err != nil {
		return fmt.Errorf("restore world %q: %w", options.restoreWorld, err)
	}
	return nil
}

func readWorldBackupFile(path string) (storage.WorldBackup, error) {
	if path == "" {
		return storage.WorldBackup{}, errBackupFilePathRequired
	}
	info, err := os.Lstat(path)
	if err != nil {
		return storage.WorldBackup{}, err
	}
	if !info.Mode().IsRegular() {
		if info.Mode()&os.ModeSymlink != 0 {
			return storage.WorldBackup{}, errBackupFileNotRegular
		}
		return storage.WorldBackup{}, errBackupFileNotRegular
	}
	if info.Mode().Perm() != 0o600 {
		return storage.WorldBackup{}, errBackupFileNotPrivate
	}
	if info.Size() <= 0 || info.Size() > maxWorldBackupBytes {
		return storage.WorldBackup{}, errBackupFileTooLarge
	}

	f, err := os.Open(path)
	if err != nil {
		return storage.WorldBackup{}, err
	}
	defer f.Close()
	// Read one byte over the bound so a file that grows after Lstat is still
	// rejected rather than silently truncated into a valid-looking envelope.
	raw, err := io.ReadAll(io.LimitReader(f, maxWorldBackupBytes+1))
	if err != nil {
		return storage.WorldBackup{}, err
	}
	if len(raw) == 0 || int64(len(raw)) > maxWorldBackupBytes {
		return storage.WorldBackup{}, errBackupFileTooLarge
	}
	backup, err := storage.ParseWorldBackup(raw)
	if err != nil {
		return storage.WorldBackup{}, err
	}
	return backup, nil
}

// writeWorldBackupFile writes in the destination directory so the final
// publication is atomic. Without overwrite, os.Link provides an atomic
// no-replace publication even if another process creates the destination
// after the initial existence check. With overwrite, os.Rename atomically
// replaces the old artifact with the fully synced private temporary file.
func writeWorldBackupFile(path string, raw []byte, overwrite bool) error {
	if path == "" {
		return errBackupFilePathRequired
	}
	if len(raw) == 0 || int64(len(raw)) > maxWorldBackupBytes {
		return errBackupFileTooLarge
	}
	parent := filepath.Dir(path)
	parentInfo, err := os.Stat(parent)
	if err != nil {
		return err
	}
	if !parentInfo.IsDir() {
		return fmt.Errorf("backup parent is not a directory: %s", parent)
	}
	if existing, err := os.Lstat(path); err == nil {
		if !overwrite {
			return fmt.Errorf("%w: %s", errBackupFileExists, path)
		}
		if existing.IsDir() {
			return fmt.Errorf("backup destination is a directory: %s", path)
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	tmp, err := os.CreateTemp(parent, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	if overwrite {
		if err := os.Rename(tmpName, path); err != nil {
			return err
		}
	} else {
		if err := os.Link(tmpName, path); err != nil {
			return fmt.Errorf("publish backup without overwrite: %w", err)
		}
		if err := os.Remove(tmpName); err != nil {
			return err
		}
	}
	removeTemp = false
	if dir, err := os.Open(parent); err == nil {
		syncErr := dir.Sync()
		closeErr := dir.Close()
		if syncErr != nil {
			return syncErr
		}
		if closeErr != nil {
			return closeErr
		}
	} else {
		return err
	}
	return nil
}
