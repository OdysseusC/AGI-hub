//go:build darwin || linux

package manager

import (
	"os"
	"syscall"
)

func openDirectoryLockFile(path string) (*os.File, error) {
	// NOFOLLOW rejects a symlink raced into place; NONBLOCK avoids blocking if a
	// nonregular file replaces the checked path before open. Regular files ignore it.
	fd, err := syscall.Open(path, syscall.O_CREAT|syscall.O_RDWR|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func lockDirectoryFile(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}
