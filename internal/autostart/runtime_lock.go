package autostart

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const runtimeLockName = ".agi-hub-startup.lock"

// The file remains permanently. Closing its handle or process death releases
// the kernel lock; unlinking it could create independent concurrent owners.
func acquireRuntimeLock(dir string) (*os.File, error) {
	return acquireRuntimeNamedLock(dir, runtimeLockName)
}

func acquireRuntimeNamedLock(dir, name string) (*os.File, error) {
	path := filepath.Join(dir, name)
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return nil, errors.New("startup lock must be a regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect startup lock: %w", err)
	}
	file, err := openRuntimeLockFile(path)
	if err != nil {
		return nil, fmt.Errorf("open startup lock: %w", err)
	}
	success := false
	defer func() {
		if !success {
			_ = file.Close()
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	checkPath := func() error {
		current, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || !current.Mode().IsRegular() || !os.SameFile(info, current) {
			return errors.New("startup lock path changed or is not a regular file")
		}
		return nil
	}
	if err := checkPath(); err != nil {
		return nil, err
	}
	if err := lockRuntimeFile(file); err != nil {
		if runtimeLockHeld(err) {
			return nil, fmt.Errorf("%w: %v", ErrAlreadyRunning, err)
		}
		return nil, fmt.Errorf("startup kernel lock is unavailable: %w", err)
	}
	if err := checkPath(); err != nil {
		return nil, err
	}
	if err := file.Chmod(0600); err != nil {
		return nil, err
	}
	success = true
	return file, nil
}

// RuntimeActive observes current kernel ownership, not the PID or states in a
// previous snapshot. It never removes the permanent lock file. Lock failures
// other than a live owner's contention are reported instead of claiming active.
func RuntimeActive(dir string) (bool, error) {
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	lock, err := acquireRuntimeLock(dir)
	if errors.Is(err, ErrAlreadyRunning) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, lock.Close()
}
