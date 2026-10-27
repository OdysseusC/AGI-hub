package autostart

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

var startupReplaceFile = syscall.NewLazyDLL("kernel32.dll").NewProc("ReplaceFileW")

func replaceStartupJSONFile(staged, destination string, beforeExists bool) error {
	if !beforeExists {
		return os.Rename(staged, destination)
	}
	target, err := startupReplacementPath(destination)
	if err != nil {
		return err
	}
	source, err := startupReplacementPath(staged)
	if err != nil {
		return err
	}
	// MoveFileEx (used by os.Rename) can fail when a reader holds the target
	// open, even with FILE_SHARE_DELETE. ReplaceFileW supports those readers
	// and preserves the target's DACL. Flags remain zero: ACL merge errors
	// must be reported, never silently ignored or bypassed by another writer.
	result, _, callErr := startupReplaceFile.Call(uintptr(unsafe.Pointer(target)), uintptr(unsafe.Pointer(source)), 0, 0, 0, 0)
	runtime.KeepAlive(target)
	runtime.KeepAlive(source)
	if result != 0 {
		return nil
	}
	if callErr == syscall.Errno(0) {
		callErr = syscall.EINVAL
	}
	return &os.LinkError{Op: "replace", Old: staged, New: destination, Err: callErr}
}

func startupReplacementPath(path string) (*uint16, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	switch {
	case strings.HasPrefix(absolute, `\\?\`):
		// An existing extended-length drive or UNC path is already normalized.
	case strings.HasPrefix(absolute, `\\`):
		absolute = `\\?\UNC\` + strings.TrimPrefix(absolute, `\\`)
	default:
		absolute = `\\?\` + absolute
	}
	return syscall.UTF16PtrFromString(absolute)
}
