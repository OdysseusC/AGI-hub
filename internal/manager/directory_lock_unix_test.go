//go:build darwin || linux

package manager

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDirectoryLockRejectsSymlinkWithoutTouchingTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, directoryLockName)); err != nil {
		t.Fatal(err)
	}
	lock, err := acquireDirectoryLock(dir)
	if err == nil {
		lock.Close()
		t.Fatal("symlink lock path accepted")
	}
	content, err := os.ReadFile(target)
	if err != nil || string(content) != "keep" {
		t.Fatal("symlink target changed")
	}
}
