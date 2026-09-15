//go:build linux

package world

import (
	"os"
	"syscall"
)

const (
	legacyBankDirectoryOpenFlags = syscall.O_RDONLY | syscall.O_DIRECTORY | syscall.O_NOFOLLOW | syscall.O_CLOEXEC
	legacyBankFileOpenFlags      = syscall.O_RDONLY | syscall.O_NONBLOCK | syscall.O_NOFOLLOW | syscall.O_CLOEXEC
	legacyBankATNoFollow         = 0x100 // AT_SYMLINK_NOFOLLOW on Linux
)

func legacyBankOpenRoot(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func legacyBankOpenAt(parent *os.File, name string, flags int) (*os.File, error) {
	fd, err := syscall.Openat(int(parent.Fd()), name, flags, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}

func legacyBankFstat(file *os.File) (legacyBankFileStat, error) {
	var stat syscall.Stat_t
	if err := syscall.Fstat(int(file.Fd()), &stat); err != nil {
		return legacyBankFileStat{}, err
	}
	return legacyBankLinuxStat(stat), nil
}

func legacyBankFstatAt(parent *os.File, name string) (legacyBankFileStat, error) {
	var stat syscall.Stat_t
	if err := legacyBankLinuxFstatAt(int(parent.Fd()), name, &stat); err != nil {
		return legacyBankFileStat{}, err
	}
	return legacyBankLinuxStat(stat), nil
}

func legacyBankLinuxStat(stat syscall.Stat_t) legacyBankFileStat {
	return legacyBankFileStat{
		dev:       stat.Dev,
		ino:       stat.Ino,
		mode:      stat.Mode,
		uid:       uint64(stat.Uid),
		gid:       uint64(stat.Gid),
		nlink:     uint64(stat.Nlink),
		size:      stat.Size,
		mtimeSec:  stat.Mtim.Sec,
		mtimeNsec: stat.Mtim.Nsec,
		ctimeSec:  stat.Ctim.Sec,
		ctimeNsec: stat.Ctim.Nsec,
		isDir:     stat.Mode&syscall.S_IFMT == syscall.S_IFDIR,
		isRegular: stat.Mode&syscall.S_IFMT == syscall.S_IFREG,
	}
}
