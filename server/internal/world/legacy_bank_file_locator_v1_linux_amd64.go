//go:build linux && amd64

package world

import (
	"runtime"
	"syscall"
	"unsafe"
)

const legacyBankLinuxNewFstatAt = uintptr(262)

func legacyBankLinuxFstatAt(dirfd int, name string, stat *syscall.Stat_t) error {
	path, err := syscall.BytePtrFromString(name)
	if err != nil {
		return err
	}
	_, _, errno := syscall.Syscall6(
		legacyBankLinuxNewFstatAt,
		uintptr(dirfd),
		uintptr(unsafe.Pointer(path)),
		uintptr(unsafe.Pointer(stat)),
		legacyBankATNoFollow,
		0,
		0,
	)
	runtime.KeepAlive(path)
	if errno != 0 {
		return errno
	}
	return nil
}
