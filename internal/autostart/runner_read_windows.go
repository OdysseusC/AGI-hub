package autostart

import (
	"os"
	"syscall"
)

func openStartupReadFile(path string) (*os.File, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	// State snapshots are replaced atomically while the UI and runner read
	// them. os.Open omits FILE_SHARE_DELETE on Windows and can therefore make
	// a concurrent atomic replacement fail with access denied. Sharing delete
	// permits replacement of the path while this handle keeps its old snapshot.
	// Open a reparse point itself; callers still validate regular-file identity.
	handle, err := syscall.CreateFile(name, syscall.GENERIC_READ, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL|syscall.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(handle), path), nil
}
