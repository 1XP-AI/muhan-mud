//go:build linux && !amd64 && !arm64

package world

import "syscall"

func legacyBankLinuxFstatAt(int, string, *syscall.Stat_t) error {
	return ErrLegacyBankFileLocatorUnsupportedOS
}
