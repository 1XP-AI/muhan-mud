//go:build darwin

package world

import (
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

const (
	legacyBankDirectoryOpenFlags = syscall.O_RDONLY | syscall.O_DIRECTORY | syscall.O_NOFOLLOW | syscall.O_CLOEXEC
	legacyBankFileOpenFlags      = syscall.O_RDONLY | syscall.O_NONBLOCK | syscall.O_NOFOLLOW | syscall.O_CLOEXEC
	legacyBankATNoFollow         = 0x0020 // AT_SYMLINK_NOFOLLOW on Darwin
	legacyBankDarwinOpenAt       = uintptr(463)
	// fstatat(469) is the legacy 32-bit-inode shape on Darwin even on a
	// 64-bit process. fstatat64(470) is the layout represented by Go's
	// syscall.Stat_t and by libc's __DARWIN_INODE64 fstatat wrapper.
	legacyBankDarwinFstatAt = uintptr(470)
)

func legacyBankOpenRoot(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func legacyBankOpenAt(parent *os.File, name string, flags int) (*os.File, error) {
	path, err := syscall.BytePtrFromString(name)
	if err != nil {
		return nil, err
	}
	rawFD, _, errno := syscall.RawSyscall6(
		legacyBankDarwinOpenAt,
		uintptr(parent.Fd()),
		uintptr(unsafe.Pointer(path)),
		uintptr(flags),
		0,
		0,
		0,
	)
	runtime.KeepAlive(path)
	if errno != 0 {
		return nil, errno
	}
	return os.NewFile(rawFD, name), nil
}

func legacyBankFstat(file *os.File) (legacyBankFileStat, error) {
	var stat syscall.Stat_t
	if err := syscall.Fstat(int(file.Fd()), &stat); err != nil {
		return legacyBankFileStat{}, err
	}
	return legacyBankDarwinStat(stat), nil
}

func legacyBankFstatAt(parent *os.File, name string) (legacyBankFileStat, error) {
	path, err := syscall.BytePtrFromString(name)
	if err != nil {
		return legacyBankFileStat{}, err
	}
	var stat syscall.Stat_t
	_, _, errno := syscall.RawSyscall6(
		legacyBankDarwinFstatAt,
		uintptr(parent.Fd()),
		uintptr(unsafe.Pointer(path)),
		uintptr(unsafe.Pointer(&stat)),
		uintptr(legacyBankATNoFollow),
		0,
		0,
	)
	runtime.KeepAlive(path)
	if errno != 0 {
		return legacyBankFileStat{}, errno
	}
	return legacyBankDarwinStat(stat), nil
}

func legacyBankDarwinStat(stat syscall.Stat_t) legacyBankFileStat {
	return legacyBankFileStat{
		dev:       uint64(uint32(stat.Dev)),
		ino:       stat.Ino,
		mode:      uint32(stat.Mode),
		uid:       uint64(stat.Uid),
		gid:       uint64(stat.Gid),
		nlink:     uint64(stat.Nlink),
		size:      stat.Size,
		mtimeSec:  stat.Mtimespec.Sec,
		mtimeNsec: int64(stat.Mtimespec.Nsec),
		ctimeSec:  stat.Ctimespec.Sec,
		ctimeNsec: int64(stat.Ctimespec.Nsec),
		isDir:     stat.Mode&syscall.S_IFMT == syscall.S_IFDIR,
		isRegular: stat.Mode&syscall.S_IFMT == syscall.S_IFREG,
	}
}
