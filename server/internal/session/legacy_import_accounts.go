package session

import (
	"context"
	"errors"

	"github.com/1XP-Inc/muhan-mud/server/internal/identity"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
)

// LegacyPlayerFile is one reviewed C-file mapping. PlayerID and ItemIDs come
// from the operator manifest and are never derived from the display name.
type LegacyPlayerFile struct {
	PlayerID  string
	CommandID string
	ItemIDs   []string
	Raw       []byte
}

// LegacyPlayerFiles looks up a reviewed native player file by canonical name.
type LegacyPlayerFiles interface {
	Lookup(context.Context, string) (LegacyPlayerFile, bool, error)
}

// PlayerSnapshotImporter admits a reviewed C-file snapshot into a world.
// Live composition uses the claimed world writer so import honors fencing.
type PlayerSnapshotImporter interface {
	ImportPlayerSnapshot(context.Context, storage.PlayerSnapshotImport) (storage.PlayerSnapshotImportResult, error)
	LoadWorld(context.Context, string) (storage.WorldSnapshot, error)
}

// LegacyImportAccounts is the xterm login adapter for a reviewed C player file.
// Exists reports the file so Login prompts for a password instead of signup.
// Authenticate verifies the native password, then Decode/Admit through
// ImportPlayerSnapshot. Name-only never returns a verified character.
type LegacyImportAccounts struct {
	Accounts
	files    LegacyPlayerFiles
	importer PlayerSnapshotImporter
	worldID  string
}

func NewLegacyImportAccounts(accounts Accounts, files LegacyPlayerFiles, importer PlayerSnapshotImporter, worldID string) *LegacyImportAccounts {
	return &LegacyImportAccounts{Accounts: accounts, files: files, importer: importer, worldID: worldID}
}

// NewLiveAccounts is the production xterm Accounts stack. GameHandler must
// receive this wrap so a reviewed C player file can log in with name+password
// while new characters still register through WorldAccounts. Name-only never
// grants ownership. A nil files catalog is fail-closed for C files.
func NewLiveAccounts(accounts Accounts, store WorldRegistrationStore, files LegacyPlayerFiles, importer PlayerSnapshotImporter, worldID string) Accounts {
	return NewLegacyImportAccounts(NewWorldAccounts(accounts, store, worldID), files, importer, worldID)
}

func (a *LegacyImportAccounts) Exists(ctx context.Context, name string) (bool, error) {
	if a == nil || a.Accounts == nil {
		return false, errors.New("legacy import unavailable")
	}
	exists, err := a.Accounts.Exists(ctx, name)
	if err != nil || exists {
		return exists, err
	}
	_, ok, err := a.lookup(ctx, name)
	return ok, err
}

func (a *LegacyImportAccounts) Authenticate(ctx context.Context, name string, password []byte) (storage.Character, error) {
	if a == nil || a.Accounts == nil {
		return storage.Character{}, errors.New("legacy import unavailable")
	}
	if err := ctx.Err(); err != nil {
		return storage.Character{}, err
	}
	exists, err := a.Accounts.Exists(ctx, name)
	if err != nil {
		return storage.Character{}, err
	}
	if exists {
		return a.Accounts.Authenticate(ctx, name, password)
	}
	rec, ok, err := a.lookup(ctx, name)
	if err != nil {
		return storage.Character{}, err
	}
	if !ok {
		return storage.Character{}, storage.ErrCredentials
	}
	snapshot, err := storage.SnapshotFromVerifiedLegacyPlayerFile(rec.Raw, password)
	if err != nil {
		return storage.Character{}, err
	}
	hash, err := identity.HashPassword(password)
	if err != nil {
		return storage.Character{}, err
	}
	defer clear(hash)
	canonical, err := identity.CanonicalName(name)
	if err != nil {
		return storage.Character{}, storage.ErrCredentials
	}
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		if err := ctx.Err(); err != nil {
			return storage.Character{}, err
		}
		worldSnap, err := a.importer.LoadWorld(ctx, a.worldID)
		if err != nil {
			return storage.Character{}, err
		}
		_, err = a.importer.ImportPlayerSnapshot(ctx, storage.PlayerSnapshotImport{
			WorldID:          a.worldID,
			CommandID:        rec.CommandID,
			ExpectedRevision: worldSnap.Revision,
			AccountName:      canonical,
			CredentialHash:   hash,
			PlayerID:         rec.PlayerID,
			Snapshot:         snapshot,
			ItemIDs:          rec.ItemIDs,
		})
		if err == nil || errors.Is(err, storage.ErrPlayerSnapshotImportConflict) {
			return a.Accounts.Authenticate(ctx, canonical, password)
		}
		if errors.Is(err, storage.ErrWorldConflict) {
			last = err
			continue
		}
		return storage.Character{}, err
	}
	return storage.Character{}, last
}

func (a *LegacyImportAccounts) lookup(ctx context.Context, name string) (LegacyPlayerFile, bool, error) {
	if a.files == nil || a.importer == nil || a.worldID == "" {
		return LegacyPlayerFile{}, false, nil
	}
	canonical, err := identity.CanonicalName(name)
	if err != nil {
		return LegacyPlayerFile{}, false, err
	}
	rec, ok, err := a.files.Lookup(ctx, canonical)
	if err != nil || !ok {
		return LegacyPlayerFile{}, false, err
	}
	if rec.PlayerID == "" || rec.CommandID == "" || len(rec.Raw) == 0 {
		return LegacyPlayerFile{}, false, nil
	}
	return rec, true, nil
}

var _ Accounts = (*LegacyImportAccounts)(nil)
