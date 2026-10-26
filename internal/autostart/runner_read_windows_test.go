package autostart

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
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

func TestStartupReaderSharingControlsRunnerTransition(t *testing.T) {
	for _, shareDelete := range []bool{false, true} {
		name := "legacy-reader-blocks-transition"
		if shareDelete {
			name = "shared-delete-reader-preserves-transition"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			plan := testPlan()
			plan.Clients = []Client{testClient(t, "gated", "fail", filepath.Join(dir, "must-not-execute"))}
			gateReached, releaseGate := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(releaseGate) }) }
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				done <- runWithPolicy(ctx, dir, plan, RunOptions{
					Probe: func(context.Context) error { return nil },
					ClientGate: func(Client) error {
						close(gateReached)
						<-releaseGate
						return errors.New("controlled prerequisite rejection")
					},
				}, testRunPolicy())
			}()
			t.Cleanup(func() {
				release()
				cancel()
				select {
				case <-finished:
				case <-time.After(5 * time.Second):
					t.Error("controlled runner did not stop")
				}
			})
			select {
			case <-gateReached:
			case err := <-done:
				t.Fatalf("runner exited before the controlled transition: %v", err)
			case <-time.After(5 * time.Second):
				t.Fatal("runner did not reach the controlled transition")
			}
			path := filepath.Join(dir, "startup-state.json")
			var reader *os.File
			if shareDelete {
				var err error
				reader, err = openStartupReadFile(path)
				if err != nil {
					release()
					t.Fatal(err)
				}
			} else {
				pathName, err := syscall.UTF16PtrFromString(path)
				if err != nil {
					release()
					t.Fatal(err)
				}
				handle, err := syscall.CreateFile(pathName, syscall.GENERIC_READ, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
				if err != nil {
					release()
					t.Fatal(err)
				}
				reader = os.NewFile(uintptr(handle), path)
			}
			defer reader.Close()
			release()
			if !shareDelete {
				select {
				case err := <-done:
					if !errors.Is(err, syscall.ERROR_ACCESS_DENIED) && !errors.Is(err, syscall.Errno(32)) {
						t.Fatalf("legacy read sharing did not expose the runner write failure: %v", err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("legacy-reader failure was hidden instead of returned")
				}
				return
			}
			waitRuntimeState(t, dir, done, func(state RuntimeState) bool {
				return hasComponent(state, "client:gated", "needs_user", 0)
			})
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("shared-delete reader interrupted runner lifecycle: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("shared-delete runner did not stop")
			}
		})
	}
}
