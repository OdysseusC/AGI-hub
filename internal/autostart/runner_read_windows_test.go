package autostart

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestStartupReadHandleAllowsAtomicRuntimeReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "startup-state.json")
	previous := RuntimeState{PID: 101, Components: []ComponentState{{ID: "hub", State: "backoff"}}}
	current := RuntimeState{PID: 202, Components: []ComponentState{{ID: "hub", State: "hub_ready"}}}
	if err := writeJSON(path, previous); err != nil {
		t.Fatal(err)
	}
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	// Reproduce the former os.Open sharing contract explicitly: without
	// SHARE_DELETE a live reader prevents replacement of the state pathname.
	legacy, err := syscall.CreateFile(name, syscall.GENERIC_READ, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	replacementErr := writeJSON(path, current)
	_ = syscall.CloseHandle(legacy)
	if !errors.Is(replacementErr, syscall.ERROR_ACCESS_DENIED) && !errors.Is(replacementErr, syscall.Errno(32)) { // ERROR_SHARING_VIOLATION
		t.Fatalf("legacy read handle did not reproduce atomic rename contention: %v", replacementErr)
	}
	reader, err := openStartupReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if err := writeJSON(path, current); err != nil {
		t.Fatalf("shared-delete reader blocked atomic runtime replacement: %v", err)
	}
	oldBytes, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	var oldSnapshot RuntimeState
	if err := json.Unmarshal(oldBytes, &oldSnapshot); err != nil || oldSnapshot.PID != previous.PID {
		t.Fatalf("open reader lost its complete old snapshot: %+v %v", oldSnapshot, err)
	}
	newSnapshot, err := ReadRuntime(dir)
	if err != nil || newSnapshot.PID != current.PID || !hasComponent(newSnapshot, "hub", "hub_ready", 0) {
		t.Fatalf("new reader did not observe complete replacement: %+v %v", newSnapshot, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
