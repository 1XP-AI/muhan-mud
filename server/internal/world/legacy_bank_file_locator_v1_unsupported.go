//go:build !darwin && !linux

package world

import "os"

const (
	legacyBankDirectoryOpenFlags = 0
	legacyBankFileOpenFlags      = 0
	legacyBankATNoFollow         = 0
)

func legacyBankOpenRoot(string) (*os.File, error) { return nil, ErrLegacyBankFileLocatorUnsupportedOS }
func legacyBankOpenAt(*os.File, string, int) (*os.File, error) {
	return nil, ErrLegacyBankFileLocatorUnsupportedOS
}
func legacyBankFstat(*os.File) (legacyBankFileStat, error) {
	return legacyBankFileStat{}, ErrLegacyBankFileLocatorUnsupportedOS
}
func legacyBankFstatAt(*os.File, string) (legacyBankFileStat, error) {
	return legacyBankFileStat{}, ErrLegacyBankFileLocatorUnsupportedOS
}
