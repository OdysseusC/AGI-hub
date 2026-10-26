package autostart

import (
	"errors"
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

var startupLockFileEx = syscall.NewLazyDLL("kernel32.dll").NewProc("LockFileEx")

func openRuntimeLockFile(path string) (*os.File, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := syscall.CreateFile(name, syscall.GENERIC_READ|syscall.GENERIC_WRITE, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL|syscall.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(handle), path), nil
}

func lockRuntimeFile(file *os.File) error {
	var overlapped syscall.Overlapped
	result, _, err := startupLockFileEx.Call(file.Fd(), 0x1|0x2, 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
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

func runtimeLockHeld(err error) bool {
	return errors.Is(err, syscall.Errno(33)) // ERROR_LOCK_VIOLATION
}
