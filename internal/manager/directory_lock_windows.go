package manager

import (
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

var lockFileEx = syscall.NewLazyDLL("kernel32.dll").NewProc("LockFileEx")

func openDirectoryLockFile(path string) (*os.File, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	// Open the reparse point itself rather than following it, and do not share
	// DELETE access: a live lock file must not be removed or renamed underneath us.
	handle, err := syscall.CreateFile(name, syscall.GENERIC_READ|syscall.GENERIC_WRITE, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL|syscall.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(handle), path), nil
}

func lockDirectoryFile(file *os.File) error {
	const failImmediately = 0x1
	const exclusiveLock = 0x2
	// This is a synchronous handle and a nonblocking call. No OVERLAPPED pointer
	// survives the syscall; the kernel releases its one-byte lock on File.Close.
	var overlapped syscall.Overlapped
	result, _, err := lockFileEx.Call(file.Fd(), exclusiveLock|failImmediately, 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
	runtime.KeepAlive(file)
	runtime.KeepAlive(&overlapped)
	if result != 0 {
		return nil
	}
	if err == syscall.Errno(0) {
		return syscall.EINVAL
	}
	return err
}
