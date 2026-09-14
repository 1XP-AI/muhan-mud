package main

import "github.com/1XP-Inc/muhan-mud/server/internal/session"

// newGameAccounts is the live /ws GameHandler Accounts constructor.
// LegacyImportAccounts wraps WorldAccounts so a reviewed C player file can
// authenticate by name+password while bcrypt signup/login still registers
// in-world. The catalog may be nil; lookup then fail-closes C files.
func newGameAccounts(repo session.Accounts, store session.WorldRegistrationStore, files session.LegacyPlayerFiles, importer session.PlayerSnapshotImporter, worldID string) session.Accounts {
	return session.NewLiveAccounts(repo, store, files, importer, worldID)
}
