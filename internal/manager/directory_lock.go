package manager

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const directoryLockName = ".agi-hub.lock"

// The lock file is permanent. Closing the handle (including process death)
// releases kernel ownership; deleting the file could create two independent locks.
func acquireDirectoryLock(dir string) (*os.File, error) {
	path := filepath.Join(dir, directoryLockName)
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return nil, errors.New("data directory lock must be a regular file, not a symlink or directory")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect data directory lock: %w", err)
	}
	file, err := openDirectoryLockFile(path)
	if err != nil {
		return nil, fmt.Errorf("open data directory lock: %w", err)
	}
	success := false
	defer func() {
		if !success {
			_ = file.Close()
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect opened directory lock: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("opened data directory lock is not a regular file")
	}
	checkPath := func() error {
		current, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !current.Mode().IsRegular() || !os.SameFile(info, current) {
			return errors.New("data directory lock path changed or is a symlink")
		}
		return nil
	}
	if err = checkPath(); err != nil {
		return nil, fmt.Errorf("validate data directory lock: %w", err)
	}
	if err = lockDirectoryFile(file); err != nil {
		return nil, fmt.Errorf("data directory is already in use or its kernel lock is unavailable: %w", err)
	}
	if err = checkPath(); err != nil {
		return nil, fmt.Errorf("validate acquired directory lock: %w", err)
	}
	success = true
	return file, nil
}
